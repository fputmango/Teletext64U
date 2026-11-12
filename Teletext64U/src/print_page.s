; ===========================================================================
; Copyright (C) 2026 Frank Putman
; SPDX-License-Identifier: GPL-3.0-or-later
; ===========================================================================
; print_page.s
;
; turns the 320x200 screen into MPS-801 bit image columns.
;
; Output geometry:
; - 7 dot rows per band
; - Every 8-pixel screen cell row is printed as PRINT_STRETCH dot rows; the same
;		pattern for every text row. The Ultimate's virtual printer puts columns
;		4 px apart and dot rows 3 px apart.
;	  PRINT_STRETCH = 14  rows 1..6 of a cell printed twice, rows 0 and 7 once
;		Note:	the font's strokes are at rows 1..6, so every stroke is exactly 2 dots
;					thick: 350 dot rows, a bit taller than 4:3
;   PRINT_STRETCH = 16  every row twice: 400 dot row (almost square)
;
; Keep PRINT_BANDS in teletext_common.c in step (ceil(25 * PRINT_STRETCH / 7):
; 50 for 14, 58 for 16).
;
; Colours: ink follows brightness (a negative: black = paper, white = full ink).
; A pixel of the background colour prints as a dithered grey of its brightness; a
; foreground colour pixel prints at full contrast against that background.
;
; Colour print (print_cells_c): the MPS-1230 colour printer prints one ink per
; pass over the same line (see teletext_common.c). Each C64 colour is mapped to a
; set of inks (K, C, M, Y).
;
; Interface (globals instead of cc65 stack arguments, like blit_fill.s):
;   void print_rows_init(void)      once, before the first band
;   void print_band_prepare(void)   before each band: takes the next 7 dot rows
;   void print_cells(void)          print_cells_n cells (8 bytes each) of the
;                                   current band, starting where the previous
;                                   call stopped, written to *print_dest
;
; --- these below only exist if PRINT_COLOUR=1 in teletext_common.c ---
;
;   void print_band_rewind(void)    back to the start of the current band
;   void print_cells_c(void)        a whole band line (40 cells) for the ink
;                                   bit print_ink to *print_dest; print_any
;                                   becomes non-zero when it has any dot
; ===========================================================================

        .export         _print_rows_init, _print_band_prepare, _print_cells
        .export         _print_cells_n, _print_dest
        .importzp       ptr1, ptr2, tmp1, tmp2, tmp3

PRINT_STRETCH   = 14            ; dot rows per cell row: 14 or 16

; The colour print is switched off to save memory. To switch it on: set PRINT_COLOUR to 1 here
; and PRINT_COLOUR to 1 in teletext_common.c
PRINT_COLOUR    = 0

.if PRINT_COLOUR
        .export         _print_band_rewind, _print_cells_c, _print_ink, _print_any
.endif

BITMAP          = $A000

; Colour pass variables are in the unused RAM at $9FF0 next to the colour pass buffer
.if PRINT_COLOUR
_print_ink      = $9FF0         ; in:  ink bit for print_cells_c: 1 = K, 2 = C, 4 = M, 8 = Y
_print_any      = $9FF1         ; out: non-zero when print_cells_c produced at least one dot
.endif
SCREEN_RAM      = $8000

        .bss

_print_cells_n: .res 1          ; in:  number of cells to print
_print_dest:    .res 2          ; in:  where the bytes go

st_sy:          .res 1          ; screen row the next dot row comes from (0..)
st_sy7:         .res 1          ; st_sy & 7
st_k:           .res 1          ; position in the group of PRINT_STRETCH dot rows (see print_repeat)
st_j:           .res 1          ; dot row counter (only the low 2 bits matter: dither row)
st_bmq:         .res 2          ; bitmap address of the cell row holding st_sy
st_colq:        .res 2          ; screen RAM address of that cell row

row_bm_lo:      .res 7          ; per dot row: address of the next bitmap byte
row_bm_hi:      .res 7
row_col_lo:     .res 7          ; per dot row: address of the next colour byte
row_col_hi:     .res 7
row_ybay:       .res 7          ; per dot row: dither matrix row * 4
row_valid:      .res 7          ; 0 = below the screen (bare paper)

cell_bm:        .res 7          ; the 7 bitmap bytes of the current cell column
cell_fg:        .res 7          ; ink level (0..16) of a set pixel
cell_bg:        .res 7          ; ink level of a clear pixel
px_mask:        .res 1
px_idx:         .res 1


        .rodata

; VIC-II brightness (0..32) / 2 per C64 colour: blk wht red cyn pur grn blu yel org brn lrd gr1 gr2 lgr lbl gr3
print_level:
        .byte    0, 16,  5, 10,  6,  8,  4, 12,  6,  4,  8,  5,  7, 12,  7, 10

; 4x4 ordered dither thresholds
print_bayer:
        .byte    0,  8,  2, 10
        .byte   12,  4, 14,  6
        .byte    3, 11,  1,  9
        .byte   15,  7, 13,  5

bitval:
        .byte   1, 2, 4, 8, 16, 32, 64

.if PRINT_COLOUR
; Inks per C64 colour (1 = K, 2 = C, 4 = M, 8 = Y) for the colour print: a copy of the screen.
; Note: several colours share one ink set, so on paper they look identical:
; - Red, orange, brown and light red are all magenta plus yellow.
; - Dark grey and mid grey are both solid black.
; - Light grey is bare paper, the same as white.
; - Green and light green match, as do blue and light blue.
;
;        blk wht red cyn pur grn blu yel org brn lrd gr1 gr2 lgr lbl gr3
ink_mask:
        .byte  1,  0, 12,  2,  4, 10,  6,  8, 12, 12, 12,  1,  1, 10,  6,  0
.endif

; One group = one cell row (8 screen rows). 1 = this dot row repeats the screen row above it
print_repeat:
.if PRINT_STRETCH = 14
        .byte   0, 0,1, 0,1, 0,1, 0,1, 0,1, 0,1, 0
.elseif PRINT_STRETCH = 16
        .byte   0,1, 0,1, 0,1, 0,1, 0,1, 0,1, 0,1, 0,1
.else
        .error  "PRINT_STRETCH must be 14 or 16"
.endif

        .code

; ---------------------------------------------------------------------------
_print_rows_init:
        lda     #0
        sta     st_sy
        sta     st_sy7
        sta     st_k
        sta     st_j
        lda     #<BITMAP
        sta     st_bmq
        lda     #>BITMAP
        sta     st_bmq+1
        lda     #<SCREEN_RAM
        sta     st_colq
        lda     #>SCREEN_RAM
        sta     st_colq+1
        rts

; ---------------------------------------------------------------------------
; Step to the next dot row: a group of PRINT_STRETCH dot rows is one cell row; the dot rows flagged
; in print_repeat show the screen row of the previous one again.
advance:
        ldy     st_k
        iny
        cpy     #PRINT_STRETCH
        bcc     @k
        ldy     #0
@k:     sty     st_k
        lda     print_repeat,y
        bne     @done
@nextrow:
        inc     st_sy
        inc     st_sy7
        lda     st_sy7
        cmp     #8
        bne     @done
        lda     #0              ; into the next cell row: +320 bitmap bytes, +40 colour bytes
        sta     st_sy7
        clc
        lda     st_bmq
        adc     #<320
        sta     st_bmq
        lda     st_bmq+1
        adc     #>320
        sta     st_bmq+1
        clc
        lda     st_colq
        adc     #40
        sta     st_colq
        bcc     @done
        inc     st_colq+1
@done:  rts

; ---------------------------------------------------------------------------
_print_band_prepare:
        ldx     #0
@row:
        lda     st_sy
        cmp     #200            ; carry set: below the screen
        lda     #0
        bcs     @store
        lda     #1
@store: sta     row_valid,x
        clc                     ; bitmap row address = cell row address + (sy & 7)
        lda     st_bmq
        adc     st_sy7
        sta     row_bm_lo,x
        lda     st_bmq+1
        adc     #0
        sta     row_bm_hi,x
        lda     st_colq
        sta     row_col_lo,x
        lda     st_colq+1
        sta     row_col_hi,x
        lda     st_j
        and     #3
        asl     a
        asl     a
        sta     row_ybay,x
        inc     st_j
        jsr     advance
        inx
        cpx     #7
        bne     @row
        rts

; ---------------------------------------------------------------------------
_print_cells:
        lda     _print_dest
        sta     ptr2
        lda     _print_dest+1
        sta     ptr2+1

@cell:
        ; --- look at the 7 dot rows of this cell column
        ldx     #6
@gather:
        lda     row_valid,x
        bne     @real
        lda     #0
        sta     cell_bm,x
        sta     cell_fg,x
        sta     cell_bg,x
        jmp     @gnext
@real:
        lda     row_bm_lo,x     ; bitmap byte, then step to the next cell (+8)
        sta     ptr1
        lda     row_bm_hi,x
        sta     ptr1+1
        ldy     #0
        lda     (ptr1),y
        sta     cell_bm,x
        clc
        lda     row_bm_lo,x
        adc     #8
        sta     row_bm_lo,x
        bcc     @n1
        inc     row_bm_hi,x
@n1:
        lda     row_col_lo,x    ; colour byte, then step to the next cell (+1)
        sta     ptr1
        lda     row_col_hi,x
        sta     ptr1+1
        lda     (ptr1),y
        sta     tmp1
        inc     row_col_lo,x
        bne     @n2
        inc     row_col_hi,x
@n2:
        and     #$0F            ; background colour -> ink level
        tay
        lda     print_level,y
        sta     tmp2
        sta     cell_bg,x
        lda     tmp1            ; foreground colour -> ink level
        lsr     a
        lsr     a
        lsr     a
        lsr     a
        tay
        lda     print_level,y
        cmp     tmp2
        beq     @equal          ; same as background: stays hidden
        bcc     @darker
        lda     #16             ; brighter than background: full ink
        bne     @fg
@darker:
        lda     #0              ; darker than background: bare paper
        beq     @fg
@equal:
        lda     tmp2
@fg:    sta     cell_fg,x
@gnext:
        dex
        bpl     @gather

        ; --- 8 columns of 7 dots
        lda     #$80
        sta     px_mask
        lda     #0
        sta     px_idx
@px:
        lda     #$80            ; bit image bytes always have bit 7 set
        sta     tmp1
        ldx     #6
@dot:
        lda     cell_bm,x
        and     px_mask
        beq     @bgpix
        lda     cell_fg,x
        jmp     @cmp
@bgpix:
        lda     cell_bg,x
@cmp:   sta     tmp2            ; ink level
        lda     px_idx
        and     #3
        ora     row_ybay,x
        tay
        lda     print_bayer,y   ; dot if ink level > threshold
        cmp     tmp2
        bcs     @nodot
        lda     tmp1
        ora     bitval,x
        sta     tmp1
@nodot:
        dex
        bpl     @dot
        lda     tmp1
        ldy     #0
        sta     (ptr2),y
        inc     ptr2
        bne     @e
        inc     ptr2+1
@e:     lsr     px_mask
        inc     px_idx
        lda     px_idx
        cmp     #8
        bne     @px

        dec     _print_cells_n
        beq     @fin
        jmp     @cell           ; too far for a branch
@fin:   rts

.if PRINT_COLOUR
; ---------------------------------------------------------------------------
; Back to the start of the current band: undo what one pass of cells moved.
_print_band_rewind:
        ldx     #6
@row:
        sec
        lda     row_bm_lo,x     ; -320 bitmap bytes (40 cells * 8)
        sbc     #<320
        sta     row_bm_lo,x
        lda     row_bm_hi,x
        sbc     #>320
        sta     row_bm_hi,x
        sec
        lda     row_col_lo,x    ; -40 colour bytes
        sbc     #40
        sta     row_col_lo,x
        bcs     @c
        dec     row_col_hi,x
@c:     dex
        bpl     @row
        rts

; ---------------------------------------------------------------------------
; A = C64 colour -> A = $FF if it carries the ink print_ink, else 0
ink_fill:
        tay
        lda     ink_mask,y
        and     _print_ink
        beq     @z
        lda     #$FF
@z:     rts

; ---------------------------------------------------------------------------
_print_cells_c:
        lda     _print_dest
        sta     ptr2
        lda     _print_dest+1
        sta     ptr2+1
        lda     #0
        sta     _print_any
        lda     #40
        sta     _print_cells_n

@cell:
        ; --- per dot row of this cell column: which pixels carry the ink
        ldx     #6
@gather:
        lda     row_valid,x
        bne     @real
        lda     #0
        sta     cell_bm,x
        beq     @gnext          ; below the screen: always taken
@real:
        lda     row_bm_lo,x     ; bitmap byte, then step to the next cell (+8)
        sta     ptr1
        lda     row_bm_hi,x
        sta     ptr1+1
        ldy     #0
        lda     (ptr1),y
        sta     tmp1
        clc
        lda     row_bm_lo,x
        adc     #8
        sta     row_bm_lo,x
        bcc     @n1
        inc     row_bm_hi,x
@n1:
        lda     row_col_lo,x    ; colour byte, then step to the next cell (+1)
        sta     ptr1
        lda     row_col_hi,x
        sta     ptr1+1
        lda     (ptr1),y
        pha
        inc     row_col_lo,x
        bne     @n2
        inc     row_col_hi,x
@n2:
        and     #$0F            ; background colour: $FF when it has the ink
        jsr     ink_fill
        sta     tmp2
        pla
        lsr     a               ; foreground colour: $FF when it has the ink
        lsr     a
        lsr     a
        lsr     a
        jsr     ink_fill
        eor     tmp2            ; pixels = (glyph & fg) | (~glyph & bg)
        and     tmp1
        eor     tmp2
        sta     cell_bm,x
@gnext:
        dex
        bpl     @gather

        ; --- turn the 7 rows of 8 pixels into 8 columns of 7 dots
        lda     #8
        sta     px_idx
@px:
        lda     #0
        sta     tmp1
        ldx     #6
@dot:
        asl     cell_bm,x       ; left pixel of row x -> carry -> dot bit of row x
        rol     tmp1
        dex
        bpl     @dot
        lda     tmp1
        ora     _print_any
        sta     _print_any
        lda     tmp1
        ora     #$80            ; bit image bytes always have bit 7 set
        ldy     #0
        sta     (ptr2),y
        inc     ptr2
        bne     @e
        inc     ptr2+1
@e:     dec     px_idx
        bne     @px

        dec     _print_cells_n
        beq     @fin
        jmp     @cell
@fin:   rts
.endif   ; PRINT_COLOUR
