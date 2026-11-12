;==============================================================================
; Copyright (C) 2026 Frank Putman
; SPDX-License-Identifier: GPL-3.0-or-later
;==============================================================================
; process_page_bytes.s
;
; signed char process_page_bytes(const unsigned char *buf, int count);
;
; Processes a buffer of HTTP response bytes in one pass! Only used in the WiC
; and Meatloaf editions! The UCI edition uses the C function process_page_byte(),
; which is mirrored here.
;
; Returns: 0=continue, 1=page done (1000 chars), 2=key pressed, $FF=404
;
; PHASE 1  header  (pp_pre_found=0): bytes go to parse_header_byte() until '>'
; PHASE 2  setup   on '>': clear screen, drain keys, pp_pre_found=1
; PHASE 3  page    decode characters, fill blit buffers, flush rows
;
; Phase 3 inner loop: X = pp_column, ptr1 = input pointer (both saved around C calls)
;==============================================================================

    .export _process_page_bytes

; ---- C functions called from this file (defined in teletext_common.c) ------
    .import _parse_header_byte          ; signed char (int intchar)
    .import _clear_bitmap_screen        ; void (void)
    .import _map_extended_char          ; unsigned char (unsigned char c)
    .import _flush_normal_row           ; void (void)  — calls blit_row if needed
    .import _flush_dh_row               ; void (unsigned char row)
    .import _write_conceal_flash_cell   ; void (unsigned char col)
    .import _lpage_draw_row2            ; void (void)  — redraw page number / cycle indicator
    .import _lpage_draw_404             ; void (void)  — page number in red
    .import _kbhit                      ; unsigned char (void)
    .import _cgetc                      ; unsigned char (void)

; ---- Page processor state variables (teletext_common.c, non-static) --------
    .import _pp_pre_found
    .import _pp_row, _pp_column
    .import _pp_FgColor, _pp_BgColor
    .import _pp_is_graphics_mode, _pp_is_sep_mosaic
    .import _pp_hold_graphic, _pp_held_char
    .import _pp_is_dh, _pp_conceal, _pp_flash
    .import _pp_numChars                ; int (2 bytes: lo at _pp_numChars, hi at +1)

; ---- Blit buffers and row flags (teletext_common.c, non-static) ------------
    .import _blit_chars, _blit_colors, _blit_fg_packed
    .import _blit_is_dh, _blit_conc, _blit_fl
    .import _row_has_dh, _row_has_content, _force_fallback

; ---- Other shared state ----------------------------------------------------
    .import _pending_key
    .import _cg_color                   ; C64 color lookup: cg_color[0..7]
    .import _fg_shifted                 ; precomputed FgColor<<4: fg_shifted[0..15]

; ---- cc65 zero-page symbols ------------------------------------------------
    .importzp ptr1, ptr3, c_sp          ; ptr1=buf ptr, ptr3=ctrl dispatch, c_sp=stack

; ---- TCC_ control code values (must match #defines in teletext_common.c) ---
TCC_ALPHA_WHITE        = $07
TCC_FLASH              = $08
TCC_STEADY             = $09
TCC_NORMAL_HEIGHT      = $0C
TCC_DOUBLE_HEIGHT      = $0D
TCC_ALPHA_ORANGE       = $0E   ; reuses the unimplemented DOUBLE_WIDTH control code
TCC_MOSAIC_ORANGE      = $0F   ; reuses the unimplemented DOUBLE_SIZE control code
TCC_MOSAIC_BLACK       = $10
TCC_CONCEAL            = $18
TCC_CONTINUOUS_MOSAICS = $19
TCC_SEPERATED_MOSAICS  = $1A
TCC_BLACK_BACKGROUND   = $1C
TCC_NEW_BACKGROUND     = $1D
TCC_HOLD_MOSAICS       = $1E
TCC_RELEASE_MOSAICS    = $1F

; ---- SCREEN_COLOR_ values (matches #defines in txtlib.h) ----------------
SCREEN_COLOR_ORANGE    = $08

; ---- Working variables in BSS (not on the C stack) -------------------------
    .bss
ppb_buf_lo: .res 1      ; saved ptr1 lo (buffer pointer, restored after C calls)
ppb_buf_hi: .res 1      ; saved ptr1 hi
ppb_cnt_lo: .res 1      ; remaining byte count lo
ppb_cnt_hi: .res 1      ; remaining byte count hi
ppb_byte:   .res 1      ; current byte being processed (= intchar in C)
ppb_new_fg: .res 1      ; pending pp_FgColor: committed after this byte's do_blit,
                        ; so it applies from the next column
ppb_new_dh: .res 1      ; pending pp_is_dh, same timing

; ============================================================================
    .code
; ============================================================================

;------------------------------------------------------------------------------
; Entry point
; cc65 convention: last arg (count, int) arrives in AX (A=lo, X=hi)
;                  first arg (buf ptr)  is on the cc65 software stack
;------------------------------------------------------------------------------
_process_page_bytes:

    ; Save count
    sta  ppb_cnt_lo
    stx  ppb_cnt_hi

    ; Load buf pointer from cc65 software stack into ptr1
    ldy  #0
    lda  (c_sp),y
    sta  ptr1
    iny
    lda  (c_sp),y
    sta  ptr1+1

    ; Pop the 2-byte buf argument from the cc65 software stack
    clc
    lda  c_sp
    adc  #2
    sta  c_sp
    bcc  @entry_ok
    inc  c_sp+1
@entry_ok:

;==============================================================================
; MAIN LOOP — fetch one byte and process it
;==============================================================================
ppb_loop:
    ; Exit when all bytes consumed → return 0 (caller may send more chunks)
    lda  ppb_cnt_lo
    ora  ppb_cnt_hi
    bne  @ppb_has_bytes     ; count != 0: proceed
    jmp  ppb_ret0           ; count == 0: done (JMP used: beq can't reach that far)
@ppb_has_bytes:

    ; Fetch byte from buffer via ptr1
    ldy  #0
    lda  (ptr1),y           ; A = current byte
    sta  ppb_byte           ; save before the count update clobbers A

    ; Advance buffer pointer
    inc  ptr1
    bne  @buf_ok
    inc  ptr1+1
@buf_ok:

    ; Decrement count
    lda  ppb_cnt_lo
    bne  @cnt_ok
    dec  ppb_cnt_hi
@cnt_ok:
    dec  ppb_cnt_lo

;==============================================================================
; PHASE 1 — HEADER
; parse_header_byte() collects the header lines and extracts the PN/FTL navigation
; values. Returns -1 on 404, 0 otherwise.
;==============================================================================
    lda  _pp_pre_found
    bne  @phase3            ; non-zero = pre_found = true, skip to page processing

    ; --- Save ptr1 around C call (C may clobber ZP) ---
    lda  ptr1
    sta  ppb_buf_lo
    lda  ptr1+1
    sta  ppb_buf_hi

    lda  ppb_byte
    ldx  #0                 ; intchar is 0-255, high byte = 0
    jsr  _parse_header_byte ; result in A: 0=ok, $FF=-1=404
    tay                     ; save result in Y (lda/sta below don't touch Y)

    ; Restore ptr1
    lda  ppb_buf_lo
    sta  ptr1
    lda  ppb_buf_hi
    sta  ptr1+1

    tya                     ; restore result to A (ppb_byte still holds original byte)
    beq  @check_for_bracket ; A=0: ok, check if this byte is '>'

    ; A=$FF: 404 — show page number in red and return -1
    jsr  _lpage_draw_404
    lda  #$FF               ; return value -1 (signed char)
    rts

@check_for_bracket:
    lda  ppb_byte
    cmp  #$3E								; '>' = ASCII 62 = end of <pre> tag
    bne  ppb_loop						; not '>' yet, keep collecting header

;==============================================================================
; PHASE 2 — SETUP (once, at '>')
;==============================================================================
    lda  ptr1               ; save ptr1 around C call
    sta  ppb_buf_lo
    lda  ptr1+1
    sta  ppb_buf_hi

    jsr  _clear_bitmap_screen   ; C: clear_bitmap_screen()

    ; Drain keys queued while clearing, e.g. a key still held from closing a
    ; menu; otherwise the row-boundary kbhit check in phase 3 would end the page.
@phase2_drain:
    jsr  _kbhit
    beq  @phase2_drain_done
    jsr  _cgetc
    jmp  @phase2_drain
@phase2_drain_done:

    lda  ppb_buf_lo
    sta  ptr1
    lda  ppb_buf_hi
    sta  ptr1+1

    lda  #1
    sta  _pp_pre_found

    jmp  ppb_loop

;==============================================================================
; PHASE 3 — PAGE BYTES
;==============================================================================
@phase3:

    ; ++pp_numChars (16-bit)
    inc  _pp_numChars
    bne  @numchars_ok
    inc  _pp_numChars+1
@numchars_ok:

    ; if (pp_numChars > 1000) return 1
    lda  _pp_numChars+1
    beq  @count_not_done    ; high byte=0 means value <=255, never >1000
    cmp  #$04
    bcs  @count_done        ; >= 0x0400 (1024) → definitely > 1000
    cmp  #$03
    bcc  @count_not_done    ; < 0x0300 (768)  → not yet
    lda  _pp_numChars       ; high byte = 3: check low byte
    cmp  #$E9               ; 0x03E9 = 1001   → done
    bcc  @count_not_done
@count_done:
    lda  #1
    rts

@count_not_done:
    ; Load column into X for use as blit array index throughout phase 3
    ldx  _pp_column

    ; Pending fg/dh default to "no change". The colour and height handlers set
    ; ppb_new_fg/ppb_new_dh; @after_blit commits them after this column is drawn.
    lda  _pp_FgColor
    sta  ppb_new_fg
    lda  _pp_is_dh
    sta  ppb_new_dh

;==============================================================================
; CONTROL CODE DISPATCH
; Bytes < 0x20 are control codes (jump table ctrl_lo/ctrl_hi at the bottom of this
; file), bytes >= 0x20 are printable characters.
;==============================================================================
@dispatch:
    lda  ppb_byte
    cmp  #$20
    bcs  @printable         ; >= 0x20: printable character

    stx  _pp_column
    tax                     ; split lo/hi tables: the code is the index, no asl
    lda  ctrl_hi,x
    sta  ptr3+1
    lda  ctrl_lo,x
    sta  ptr3
    jmp  (ptr3)             ; jump to handler; handler ends with JMP do_blit

;==============================================================================
; PRINTABLE CHARACTER PATH (intchar >= 0x20)
;==============================================================================
@printable:

    ; Extended ASCII mapping (chars >= 0x80, or 0x40 for YLE Teksti-TV)
    lda  _pp_is_graphics_mode
    bne  @after_mapping     ; in graphics mode: skip text mapping

    lda  ppb_byte
    bmi  @do_mapping        ; >= 0x80: needs mapping
    cmp  #$40
    bne  @after_mapping     ; not 0x40: skip (0x41-0x7F need no mapping)

@do_mapping:
    stx  _pp_column         ; save X=column before C call
    lda  ptr1
    sta  ppb_buf_lo
    lda  ptr1+1
    sta  ppb_buf_hi
    lda  ppb_byte           ; A = char (cc65: last arg in register A)
    jsr  _map_extended_char ; result in A = remapped char
    sta  ppb_byte
    lda  ppb_buf_lo         ; restore ptr1 and X=column
    sta  ptr1
    lda  ppb_buf_hi
    sta  ptr1+1
    ldx  _pp_column

@after_mapping:

    ; Graphics mode: 0x20-0x3F and 0x60-0x7F become mosaic glyphs (+0x60 separated,
    ; +0x80 contiguous) and set held_char; text mode resets held_char to space.
    lda  _pp_is_graphics_mode
    beq  @text_mode_held    ; not graphics: set held_char=space

    lda  ppb_byte
    cmp  #$20
    bcc  @after_held        ; < 0x20: not a mosaic graphic char
    cmp  #$40
    bcc  @apply_offset      ; 0x20-0x3F: mosaic range
    cmp  #$60
    bcc  @after_held        ; 0x40-0x5F: not a mosaic graphic char
    cmp  #$80
    bcs  @after_held        ; >= 0x80: not a mosaic graphic char (0x60-0x7F IS)
    ; fall through: 0x60-0x7F is mosaic range

@apply_offset:
    lda  _pp_is_sep_mosaic
    beq  @offset_contiguous
    lda  ppb_byte
    clc
    adc  #$60
    jmp  @offset_done
@offset_contiguous:
    lda  ppb_byte
    clc
    adc  #$80
@offset_done:
    sta  ppb_byte
    sta  _pp_held_char
    jmp  @after_held

@text_mode_held:
    lda  #$20
    sta  _pp_held_char

@after_held:

;==============================================================================
; BLIT BUFFER FILL (from @printable and from the control code handlers)
;==============================================================================
do_blit:
    ldx  _pp_column

    lda  ppb_byte
    sta  _blit_chars,x

    lda  _pp_is_dh
    sta  _blit_is_dh,x
    beq  @dh_clear
    sta  _row_has_dh
@dh_clear:

    ; blit_fg_packed[column] = (FgColor << 4) | BgColor
    lda  _pp_FgColor
    asl
    asl
    asl
    asl
    ora  _pp_BgColor
    sta  _blit_fg_packed,x

    ; blit_colors[column] = fg_shifted[FgColor] | BgColor
    lda  _pp_FgColor
    tay
    lda  _fg_shifted,y
    ora  _pp_BgColor
    sta  _blit_colors,x

    ; Conceal / flash path
    lda  _pp_conceal
    ora  _pp_flash
    bne  @conceal_flash

    ; Normal path: clear conceal/flash flags, set row_has_content if visible
    lda  #0
    sta  _blit_conc,x
    sta  _blit_fl,x

    lda  _pp_BgColor
    bne  @set_content       ; non-black background → always has content
    lda  ppb_byte
    cmp  #$20               ; space on black → no visible content
    beq  @after_blit
@set_content:
    lda  #1
    sta  _row_has_content
    jmp  @after_blit

@conceal_flash:
    ; C helper handles SCREEN_RAM2 and the CONCEAL/FLASH arrays
    lda  _pp_conceal
    sta  _blit_conc,x
    lda  _pp_flash
    sta  _blit_fl,x
    stx  _pp_column         ; save X before C call
    lda  ptr1
    sta  ppb_buf_lo
    lda  ptr1+1
    sta  ppb_buf_hi
    lda  _pp_column         ; A = col argument
    jsr  _write_conceal_flash_cell
    lda  ppb_buf_lo
    sta  ptr1
    lda  ppb_buf_hi
    sta  ptr1+1
    ldx  _pp_column

@after_blit:

    ; Commit the pending fg/dh: active from the next column
    lda  ppb_new_fg
    sta  _pp_FgColor
    lda  ppb_new_dh
    sta  _pp_is_dh

;==============================================================================
; COLUMN INCREMENT + END-OF-ROW CHECK
;==============================================================================
    inx                     ; ++column
    stx  _pp_column

    cpx  #40
    beq  @ppb_at_40         ; column == 40: fall into end-of-row
    jmp  ppb_loop
@ppb_at_40:

;==============================================================================
; END OF ROW (column just reached 40): flush, reset state, key check
;==============================================================================

    lda  ptr1               ; save ptr1 before C calls
    sta  ppb_buf_lo
    lda  ptr1+1
    sta  ppb_buf_hi

    lda  _row_has_dh
    beq  @normal_flush
    lda  _pp_row            ; A = row argument for flush_dh_row
    jsr  _flush_dh_row
    jmp  @after_flush
@normal_flush:
    jsr  _flush_normal_row

@after_flush:
    lda  ppb_buf_lo
    sta  ptr1
    lda  ppb_buf_hi
    sta  ptr1+1

    ; force_fallback = row_has_dh && !force_fallback
    ; (a DH row right after another DH row wrote nothing to the bitmap row below it,
    ; so there is nothing to protect)
    lda  _row_has_dh          ; new DH flag
    beq  @ff_set_zero         ; row_has_dh=0 → force_fallback=0
    lda  _force_fallback      ; row_has_dh=1: check old force_fallback
    bne  @ff_set_zero         ; old=1 AND new=1: consecutive DH → force_fallback=0
    lda  #1
    sta  _force_fallback
    jmp  @ff_clear_flags
@ff_set_zero:
    lda  #0
    sta  _force_fallback
@ff_clear_flags:
    lda  #0
    sta  _row_has_dh
    sta  _row_has_content

    ; Reset per-row state: column 0, next row, white on black, flags off, held = space
    lda  #0
    sta  _pp_column
    ldx  #0                 ; X = new column = 0
    inc  _pp_row

    lda  _cg_color+TCC_ALPHA_WHITE  ; white
    sta  _pp_FgColor
    lda  #0
    sta  _pp_BgColor
    sta  _pp_is_dh
    sta  _pp_is_sep_mosaic
    sta  _pp_is_graphics_mode
    sta  _pp_hold_graphic
    sta  _pp_conceal
    sta  _pp_flash
    lda  #$20
    sta  _pp_held_char

    ; Key check at every row boundary, except rows 0-2
    lda  _pp_row
    cmp  #3
    bcc  @ppb_skip_kbhit

    lda  ptr1
    sta  ppb_buf_lo
    lda  ptr1+1
    sta  ppb_buf_hi
    jsr  _kbhit
    sta  ppb_byte           ; save result: the ptr1 restore overwrites A
    lda  ppb_buf_lo
    sta  ptr1
    lda  ppb_buf_hi
    sta  ptr1+1
    lda  ppb_byte
    bne  @ppb_key_pressed

@ppb_skip_kbhit:
    lda  _pp_row
    cmp  #2
    beq  @ppb_eor_row2
    jmp  ppb_loop               ; JMP: ppb_loop out of branch range
@ppb_eor_row2:
    ; Start of row 2: redraw the page number (the header bytes overwrote it)
    lda  ptr1
    sta  ppb_buf_lo
    lda  ptr1+1
    sta  ppb_buf_hi
    jsr  _lpage_draw_row2
    lda  ppb_buf_lo
    sta  ptr1
    lda  ppb_buf_hi
    sta  ptr1+1
    jmp  ppb_loop

@ppb_key_pressed:
    jsr  _cgetc
    sta  _pending_key
    lda  #2
    rts

ppb_ret0:
    lda  #0
    rts

;==============================================================================
; CONTROL CODE HANDLERS (reached via ctrl_lo/ctrl_hi)
; Each updates the teletext state, sets ppb_byte to the char to blit (a space, or
; held_char with Hold Mosaics) and jumps to do_blit.
; The "C:" comments name the matching case in process_page_byte().
;==============================================================================

; C: case 0x00..0x07: graphics off, conceal off, fg = cg_color[code]
; Flash is left alone: a colour change does not cancel flashing.
; held_or_space_calc runs before is_graphics_mode is overwritten (it needs the old value).
handle_alpha_color:
    lda  _pp_column
    tax
    lda  ppb_byte           ; code = cg_color index
    tay
    lda  _cg_color,y
    sta  ppb_new_fg
    jsr  held_or_space_calc
    pha                     ; stash: is_graphics_mode/conceal change next
    lda  #0
    sta  _pp_is_graphics_mode
    sta  _pp_conceal
    pla
    sta  ppb_byte
    jmp  do_blit

; C: TCC_ALPHA_ORANGE ($0E, reuses the unused DOUBLE_WIDTH code, as there is no
; standard orange code): like handle_alpha_color with fg = orange
handle_alpha_orange:
    lda  _pp_column
    tax
    lda  #SCREEN_COLOR_ORANGE
    sta  ppb_new_fg
    jsr  held_or_space_calc
    pha
    lda  #0
    sta  _pp_is_graphics_mode
    sta  _pp_conceal
    pla
    sta  ppb_byte
    jmp  do_blit

; C: case 0x10..0x17: graphics on, conceal off, fg = cg_color[code-0x10]
; held_or_space_calc runs before is_graphics_mode is set, as in handle_alpha_color.
handle_mosaic_color:
    lda  _pp_column
    tax
    lda  ppb_byte
    sec
    sbc  #$10
    tay
    lda  _cg_color,y
    sta  ppb_new_fg
    jsr  held_or_space_calc
    pha
    lda  #1
    sta  _pp_is_graphics_mode
    lda  #0
    sta  _pp_conceal
    pla
    sta  ppb_byte
    jmp  do_blit

; C: TCC_MOSAIC_ORANGE ($0F, reuses the unused DOUBLE_SIZE code): like
; handle_mosaic_color with fg = orange
handle_mosaic_orange:
    lda  _pp_column
    tax
    lda  #SCREEN_COLOR_ORANGE
    sta  ppb_new_fg
    jsr  held_or_space_calc
    pha
    lda  #1
    sta  _pp_is_graphics_mode
    lda  #0
    sta  _pp_conceal
    pla
    sta  ppb_byte
    jmp  do_blit

; held_or_space_calc: A = (pp_hold_graphic && pp_is_graphics_mode) ? pp_held_char : 0x20
; Clobbers A only. Each handler decides whether to call it before or after changing the flags.
held_or_space_calc:
    lda  _pp_hold_graphic
    beq  @hos_space
    lda  _pp_is_graphics_mode
    beq  @hos_space
    lda  _pp_held_char
    rts
@hos_space:
    lda  #$20
    rts

; C: case TCC_FLASH (0x08): should_flash=true; intchar=held_or_space
handle_flash:
    lda  _pp_column
    tax
    lda  #1
    sta  _pp_flash
    jsr  held_or_space_calc
    sta  ppb_byte
    jmp  do_blit

; C: case TCC_STEADY (0x09): should_flash=false; intchar=held_or_space
handle_steady:
    lda  _pp_column
    tax
    lda  #0
    sta  _pp_flash
    jsr  held_or_space_calc
    sta  ppb_byte
    jmp  do_blit

; C: case TCC_NORMAL_HEIGHT (0x0C): is_doubleheight=false; intchar=held_or_space
; Applies from the next column (ppb_new_dh).
handle_normal_height:
    lda  _pp_column
    tax
    lda  #0
    sta  ppb_new_dh
    jsr  held_or_space_calc
    sta  ppb_byte
    jmp  do_blit

; C: case TCC_DOUBLE_HEIGHT (0x0D): is_doubleheight=true; intchar=held_or_space
; Applies from the next column; do_blit sets row_has_dh.
handle_double_height:
    lda  _pp_column
    tax
    lda  #1
    sta  ppb_new_dh
    jsr  held_or_space_calc
    sta  ppb_byte
    jmp  do_blit

; C: case TCC_CONCEAL (0x18): should_conceal=true; intchar=held_or_space
handle_conceal:
    lda  _pp_column
    tax
    lda  #1
    sta  _pp_conceal
    jsr  held_or_space_calc
    sta  ppb_byte
    jmp  do_blit

; C: case TCC_CONTINUOUS_MOSAICS (0x19): is_sep_mosaic=false; intchar=held_or_space
handle_cont_mosaics:
    lda  _pp_column
    tax
    lda  #0
    sta  _pp_is_sep_mosaic
    jsr  held_or_space_calc
    sta  ppb_byte
    jmp  do_blit

; C: case TCC_SEPERATED_MOSAICS (0x1A): is_sep_mosaic=true; intchar=held_or_space
handle_sep_mosaics:
    lda  _pp_column
    tax
    lda  #1
    sta  _pp_is_sep_mosaic
    jsr  held_or_space_calc
    sta  ppb_byte
    jmp  do_blit

; C: case TCC_BLACK_BACKGROUND (0x1C): BgColor=cg_color[0]=black; intchar=held_or_space
handle_black_bg:
    lda  _pp_column
    tax
    lda  #0
    sta  _pp_BgColor
    jsr  held_or_space_calc
    sta  ppb_byte
    jmp  do_blit

; C: case TCC_NEW_BACKGROUND (0x1D): BgColor=FgColor; intchar=held_or_space
handle_new_bg:
    lda  _pp_column
    tax
    lda  _pp_FgColor
    sta  _pp_BgColor
    jsr  held_or_space_calc
    sta  ppb_byte
    jmp  do_blit

; C: case TCC_HOLD_MOSAICS (0x1E): hold_graphic=true; intchar = was_graphics ? held_char : 0x20
; held_or_space_calc runs after hold_graphic is set.
handle_hold_mosaics:
    lda  _pp_column
    tax
    lda  #1
    sta  _pp_hold_graphic
    jsr  held_or_space_calc
    sta  ppb_byte
    jmp  do_blit

; C: case TCC_RELEASE_MOSAICS (0x1F): hold_graphic=false; intchar=0x20
; held_or_space_calc runs after hold_graphic is cleared, so it returns 0x20.
handle_release_mosaics:
    lda  _pp_column
    tax
    lda  #0
    sta  _pp_hold_graphic
    jsr  held_or_space_calc
    sta  ppb_byte
    jmp  do_blit

; C: case TCC_ENDBOX/TCC_STARTBOX/TCC_ESC_GO_SWITCH (0x0A, 0x0B, 0x1B):
;    no state change; intchar=held_or_space
handle_passthrough_hold:
    lda  _pp_column
    tax
    jsr  held_or_space_calc
    sta  ppb_byte
    jmp  do_blit

;==============================================================================
; CONTROL CODE JUMP TABLE
; 32 entries (codes 0x00-0x1F) as split lo/hi byte arrays, indexed directly by code.
;==============================================================================
ctrl_lo:
    .byte <handle_alpha_color,  <handle_alpha_color    ; 00 ALPHA_BLACK,  01 ALPHA_RED
    .byte <handle_alpha_color,  <handle_alpha_color    ; 02 ALPHA_GREEN,  03 ALPHA_YELLOW
    .byte <handle_alpha_color,  <handle_alpha_color    ; 04 ALPHA_BLUE,   05 ALPHA_MAGENTA
    .byte <handle_alpha_color,  <handle_alpha_color    ; 06 ALPHA_CYAN,   07 ALPHA_WHITE
    .byte <handle_flash,        <handle_steady         ; 08 FLASH,        09 STEADY
    .byte <handle_passthrough_hold, <handle_passthrough_hold ; 0A ENDBOX,  0B STARTBOX
    .byte <handle_normal_height,<handle_double_height  ; 0C NORMAL_HEIGHT,0D DOUBLE_HEIGHT
    .byte <handle_alpha_orange, <handle_mosaic_orange ; 0E ALPHA_ORANGE (hack, was DOUBLE_WIDTH), 0F MOSAIC_ORANGE (hack, was DOUBLE_SIZE)
    .byte <handle_mosaic_color, <handle_mosaic_color   ; 10 MOSAIC_BLACK, 11 MOSAIC_RED
    .byte <handle_mosaic_color, <handle_mosaic_color   ; 12 MOSAIC_GREEN, 13 MOSAIC_YELLOW
    .byte <handle_mosaic_color, <handle_mosaic_color   ; 14 MOSAIC_BLUE,  15 MOSAIC_MAGENTA
    .byte <handle_mosaic_color, <handle_mosaic_color   ; 16 MOSAIC_CYAN,  17 MOSAIC_WHITE
    .byte <handle_conceal,      <handle_cont_mosaics   ; 18 CONCEAL,      19 CONT_MOSAICS
    .byte <handle_sep_mosaics,  <handle_passthrough_hold ; 1A SEP_MOSAICS,1B ESC_GO_SWITCH
    .byte <handle_black_bg,     <handle_new_bg         ; 1C BLACK_BG,     1D NEW_BG
    .byte <handle_hold_mosaics, <handle_release_mosaics; 1E HOLD_MOSAICS, 1F RELEASE_MOSAICS

ctrl_hi:
    .byte >handle_alpha_color,  >handle_alpha_color
    .byte >handle_alpha_color,  >handle_alpha_color
    .byte >handle_alpha_color,  >handle_alpha_color
    .byte >handle_alpha_color,  >handle_alpha_color
    .byte >handle_flash,        >handle_steady
    .byte >handle_passthrough_hold, >handle_passthrough_hold
    .byte >handle_normal_height,>handle_double_height
    .byte >handle_alpha_orange, >handle_mosaic_orange
    .byte >handle_mosaic_color, >handle_mosaic_color
    .byte >handle_mosaic_color, >handle_mosaic_color
    .byte >handle_mosaic_color, >handle_mosaic_color
    .byte >handle_mosaic_color, >handle_mosaic_color
    .byte >handle_conceal,      >handle_cont_mosaics
    .byte >handle_sep_mosaics,  >handle_passthrough_hold
    .byte >handle_black_bg,     >handle_new_bg
    .byte >handle_hold_mosaics, >handle_release_mosaics
