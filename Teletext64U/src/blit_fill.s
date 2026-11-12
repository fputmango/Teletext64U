; Copyright (C) 2026 Frank Putman
; SPDX-License-Identifier: GPL-3.0-or-later
; blit_fill.s
;
; void blit_fill_rect_asm(void);
;
; Fills a col..col+w-1 x row..row+h-1 rectangle with the blanksand a single packed color byte
;
; Arguments are not passed via the cc65 stack. The callers, clear_popup_canvas(), mm_blank() in
; teletext_common.c) write these BSS globals before calling:
;   _blit_f_col   (1 byte)  — start column
;   _blit_f_row   (1 byte)  — start row
;   _blit_f_w     (1 byte)  — width
;   _blit_f_h     (1 byte)  — height
;   _blit_f_color (1 byte)  — color byte
;
; ZP usage:  ptr1=bitmap dest, ptr2=screen RAM dest

    .export _blit_fill_rect_asm

    .importzp ptr1, ptr2
    .import   _row_bitmap_offset, _row_screen_offset, _blit_screen_base
    .import   _blit_f_col, _blit_f_row, _blit_f_w, _blit_f_h, _blit_f_color

    .bss
fill_rows_left: .res 1     ; rows to do
fill_cols_left: .res 1     ; columns left in current row
fill_tmp_hi:    .res 1     ; scratch for col*8 hi byte

    .code

_blit_fill_rect_asm:
    lda  _blit_f_h
    sta  fill_rows_left
    bne  @have_rows           ; if h is 0 -> nothing to do
    jmp  @done
@have_rows:

    lda  _blit_f_row

@row_loop:
    pha                       ; keep row, reused at the end to compute row+1

    ; ---- ptr1 = row_bitmap_offset[row] + col*8 ----
    asl                       ; row*2 -> Y (uint16 table index)
    tay
    lda  _row_bitmap_offset,y
    sta  ptr1
    lda  _row_bitmap_offset+1,y
    sta  ptr1+1

    lda  _blit_f_col
    lsr
    lsr
    lsr
    lsr
    lsr
    sta  fill_tmp_hi

    lda  _blit_f_col
    asl
    asl
    asl
    clc
    adc  ptr1
    sta  ptr1
    lda  ptr1+1
    adc  fill_tmp_hi
    sta  ptr1+1

    ; ---- ptr2 = blit_screen_base + row_screen_offset[row] + col ----
    lda  _row_screen_offset,y
    clc
    adc  _blit_screen_base
    sta  ptr2
    lda  _row_screen_offset+1,y
    adc  _blit_screen_base+1
    sta  ptr2+1
    clc
    lda  ptr2
    adc  _blit_f_col
    sta  ptr2
    bcc  @scr_ok
    inc  ptr2+1
@scr_ok:

    ; ---- clear w glyph cells + write w color bytes ----
    lda  _blit_f_w
    sta  fill_cols_left
    beq  @next_row            ; if w is 0 -> nothing to do this row

@col_loop:
    ldy  #7
    lda  #0
@zero_glyph:
    sta  (ptr1),y
    dey
    bpl  @zero_glyph

    ldy  #0
    lda  _blit_f_color
    sta  (ptr2),y

    clc
    lda  ptr1
    adc  #8
    sta  ptr1
    bcc  @no_bmp_carry
    inc  ptr1+1
@no_bmp_carry:

    inc  ptr2
    bne  @no_scr_carry
    inc  ptr2+1
@no_scr_carry:

    dec  fill_cols_left
    bne  @col_loop

@next_row:
    pla                        ; restore row
    clc
    adc  #1                    ; next iteration
    dec  fill_rows_left
    bne  @row_loop

@done:
    rts
