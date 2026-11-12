; Copyright (C) 2026 Frank Putman
; SPDX-License-Identifier: GPL-3.0-or-later
; copy8.s
; void copy8(unsigned char *dst, const unsigned char *src);
;
; cc65 convention: src (last arg) in AX, dst (first arg) on the software stack.

    .export  _copy8
    .importzp ptr1, ptr2, c_sp

    .code

_copy8:
    sta  ptr1               ; src lo
    stx  ptr1+1             ; src hi

    ldy  #0
    lda  (c_sp),y           ; dst lo
    sta  ptr2
    iny
    lda  (c_sp),y           ; dst hi
    sta  ptr2+1

    ; pop dst
    clc
    lda  c_sp
    adc  #2
    sta  c_sp
    bcc  @skip
    inc  c_sp+1
@skip:

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

    rts
