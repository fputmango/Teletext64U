; Copyright (C) 2026 Frank Putman
; SPDX-License-Identifier: GPL-3.0-or-later
;
; toggle_flash.s
; void toggle_flash_loop(void)
;
; Colour loop of toggle_flash(), called after the C wrapper has toggled is_flash.
; Only cells with a non-zero mask are touched.
;   is_flash non-zero: dest = low nibble of src in both nibbles (fg = bg -> text hidden)
;   is_flash zero:     dest = src (text visible)
;
; SCREEN_RAM $8000 = dest (fg<<4 | bg), SCREEN_RAM2 $8400 = src (saved colours),
; SCREEN_FLASH $8C00 = mask
;
; 1000 cells = 3 pages of 256 + 1 partial page of 232.

    .export  _toggle_flash_loop
    .import  _is_flash          ; not static in teletext_common.c
    .importzp tmp1              ; ZP scratch

SCREEN_DST = $8000
SCREEN_SRC = $8400
SCREEN_MSK = $8C00

; ── visible, full page ──
.macro flash_on_page offset
    .local loop, skip
    LDY #0
loop:
    LDA SCREEN_MSK + offset, Y
    BEQ skip
    LDA SCREEN_SRC + offset, Y
    STA SCREEN_DST + offset, Y
skip:
    INY
    BNE loop
.endmacro

; ── visible, partial page ──
.macro flash_on_partial offset, count
    .local loop, skip
    LDY #0
loop:
    LDA SCREEN_MSK + offset, Y
    BEQ skip
    LDA SCREEN_SRC + offset, Y
    STA SCREEN_DST + offset, Y
skip:
    INY
    CPY #count
    BNE loop
.endmacro

; ── hidden, full page ──
.macro flash_off_page offset
    .local loop, skip
    LDY #0
loop:
    LDA SCREEN_MSK + offset, Y
    BEQ skip
    LDA SCREEN_SRC + offset, Y
    AND #$0F
    STA tmp1
    ASL A
    ASL A
    ASL A
    ASL A
    ORA tmp1                    ; low nibble in both nibbles
    STA SCREEN_DST + offset, Y
skip:
    INY
    BNE loop
.endmacro

; ── hidden, partial page ──
.macro flash_off_partial offset, count
    .local loop, skip
    LDY #0
loop:
    LDA SCREEN_MSK + offset, Y
    BEQ skip
    LDA SCREEN_SRC + offset, Y
    AND #$0F
    STA tmp1
    ASL A
    ASL A
    ASL A
    ASL A
    ORA tmp1
    STA SCREEN_DST + offset, Y
skip:
    INY
    CPY #count
    BNE loop
.endmacro

    .code

.proc _toggle_flash_loop

    LDA _is_flash
    BNE do_flash_off

do_flash_on:
    flash_on_page    $000
    flash_on_page    $100
    flash_on_page    $200
    flash_on_partial $300, 232  ; 3*256 + 232 = 1000
    RTS

do_flash_off:
    flash_off_page    $000
    flash_off_page    $100
    flash_off_page    $200
    flash_off_partial $300, 232
    RTS

.endproc
