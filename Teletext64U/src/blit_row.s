; Copyright (C) 2026 Frank Putman
; SPDX-License-Identifier: GPL-3.0-or-later
; blit_row.s
;
; void blit_row(unsigned char row, const unsigned char *chars, const unsigned char *colors);
;
; Draws the 40 characters of one teletext row in one pass.
;
; cc65 calling convention:
;   colors (ptr, 2 bytes)  -> AX (last arg, in registers: A=lo, X=hi)
;   chars  (ptr, 2 bytes)  -> top of cc65 software stack  (c_sp[0..1])
;   row    (1 byte)        -> c_sp[2]
;
; ZP usage (cc65 symbols):
;   ptr1  = font glyph source (set per char)
;   ptr2  = bitmap dest       (advances by 8 each char)
;   ptr3  = char buffer       (read with Y = col index)
;   ptr4  = color buffer      (read with Y = col index)
;
; blit_scr (BSS) = running screen RAM pointer, +1 per char
;
; font_ptr: unsigned char*[256]; chars 0..127 are the first 256 bytes, chars 128..255
; (mosaic) the second 256. A single asl/bcs selects the half.

    .export _blit_row

    .importzp ptr1, ptr2, ptr3, ptr4, c_sp
    .import  _row_bitmap_offset
    .import  _row_screen_offset
    .import  _font_ptr
    .import  _blit_screen_base   ; unsigned int: absolute address of SCREEN_RAM

COLS = 40

    .bss
blit_scr_lo:  .res 1    ; screen RAM running pointer
blit_scr_hi:  .res 1    ; ,,
blit_color:   .res 1    ; color byte current char
blit_col:     .res 1    ; column counter (0-39)

    .code

_blit_row:
    ; ---- Store colors pointer (AX = last arg) ----
    sta  ptr4
    stx  ptr4+1

    ; ---- Read chars pointer and row from software stack ----
    ldy  #0
    lda  (c_sp),y
    sta  ptr3           ; chars lo
    iny
    lda  (c_sp),y
    sta  ptr3+1         ; chars hi
    iny
    lda  (c_sp),y       ; row (0-24)
    tax                 ; X = row

    ; ---- Advance software stack by 3 (pop chars ptr + row) ----
    clc
    lda  c_sp
    adc  #3
    sta  c_sp
    bcc  @sp_ok
    inc  c_sp+1
@sp_ok:

    ; ---- row_bitmap_offset[row] → ptr2 ----
    ; unsigned int[25]: each element 2 bytes → byte index = row*2
    txa
    asl  a              ; A = row*2
    tay
    lda  _row_bitmap_offset,y
    sta  ptr2
    lda  _row_bitmap_offset+1,y
    sta  ptr2+1

    ; ---- row_screen_offset[row] + blit_screen_base → blit_scr_lo/hi ----
    lda  _row_screen_offset,y
    clc
    adc  _blit_screen_base
    sta  blit_scr_lo
    lda  _row_screen_offset+1,y
    adc  _blit_screen_base+1
    sta  blit_scr_hi

    ; ---- column loop ----
    lda  #0
    sta  blit_col

@col_loop:
    ; -- Load char code and look up font pointer --
    ldy  blit_col
    lda  (ptr3),y       ; A = char code

    asl  a
    bcs  @mosaic_font
    tax
    lda  _font_ptr,x
    sta  ptr1
    lda  _font_ptr+1,x
    sta  ptr1+1
    jmp  @font_done
@mosaic_font:
    tax
    lda  _font_ptr+256,x
    sta  ptr1
    lda  _font_ptr+256+1,x
    sta  ptr1+1
@font_done:

    ; -- Color byte --
    ldy  blit_col
    lda  (ptr4),y
    sta  blit_color

    ; -- Unrolled 8-byte copy: font glyph (ptr1) → bitmap (ptr2) --
    ldy  #0
    lda  (ptr1),y
    sta  (ptr2),y
    iny
    lda  (ptr1),y
    sta  (ptr2),y
    iny
    lda  (ptr1),y
    sta  (ptr2),y
    iny
    lda  (ptr1),y
    sta  (ptr2),y
    iny
    lda  (ptr1),y
    sta  (ptr2),y
    iny
    lda  (ptr1),y
    sta  (ptr2),y
    iny
    lda  (ptr1),y
    sta  (ptr2),y
    iny
    lda  (ptr1),y
    sta  (ptr2),y

    ; -- Write color to screen RAM --
    ; ptr1 is free after the glyph copy; borrow it for the indirect write
    lda  blit_scr_lo
    sta  ptr1
    lda  blit_scr_hi
    sta  ptr1+1
    ldy  #0
    lda  blit_color
    sta  (ptr1),y

    ; -- Advance bitmap pointer by 8 --
    clc
    lda  ptr2
    adc  #8
    sta  ptr2
    bcc  @no_bmp_carry
    inc  ptr2+1
@no_bmp_carry:

    ; -- Advance screen RAM pointer by 1 --
    inc  blit_scr_lo
    bne  @no_scr_carry
    inc  blit_scr_hi
@no_scr_carry:

    ; -- Next column --
    inc  blit_col
    lda  blit_col
    cmp  #COLS
    bne  @col_loop

    rts
