; Copyright (C) 2026 Frank Putman
; SPDX-License-Identifier: GPL-3.0-or-later
;
; toggle_conceal.s
; void toggle_conceal(void)
;
; Toggles concealed text on/off; only cells with a non-zero mask are touched.
;   conceal: dest = low nibble of src in both nibbles (fg = bg -> text invisible)
;   reveal:  dest = src
;
; SCREEN_RAM $8000 = dest (fg<<4 | bg), SCREEN_RAM2 $8400 = src (saved colours),
; SCREEN_CONCEAL $8800 = mask
;
; 1000 cells = 3 pages of 256 + 1 partial page of 232.

    .export  _toggle_conceal
    .import  _is_conceal        ; not static in teletext_common.c
    .importzp tmp1              ; ZP scratch

SCREEN_DST = $8000
SCREEN_SRC = $8400
SCREEN_MSK = $8800

; ── reveal, full page ──
.macro reveal_page offset
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

; ── reveal, partial page ──
.macro reveal_partial offset, count
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

; ── conceal, full page ──
.macro conceal_page offset
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

; ── conceal, partial page ──
.macro conceal_partial offset, count
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

.proc _toggle_conceal

    LDA _is_conceal
    EOR #$01
    STA _is_conceal
    BNE do_conceal              ; is_conceal = !is_conceal; non-zero: conceal

do_reveal:
    reveal_page    $000
    reveal_page    $100
    reveal_page    $200
    reveal_partial $300, 232    ; 3*256 + 232 = 1000
    RTS

do_conceal:
    conceal_page    $000
    conceal_page    $100
    conceal_page    $200
    conceal_partial $300, 232
    RTS

.endproc
