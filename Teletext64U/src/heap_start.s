; Copyright (C) 2026 Frank Putman
; SPDX-License-Identifier: GPL-3.0-or-later
; heap_start.s
; unsigned get_heap_start(void);
;
; Returns __BSS_RUN__ + __BSS_SIZE__ (first free byte after BSS) in A (lo) / X (hi);
; main() passes it to _heapadd()
; Not readable from C: cc65 adds an underscore to C externs (__BSS_RUN__ becomes ___BSS_RUN__).

    .export _get_heap_start

    .import __BSS_RUN__
    .import __BSS_SIZE__

    .code

_get_heap_start:
    lda #<(__BSS_RUN__ + __BSS_SIZE__)
    ldx #>(__BSS_RUN__ + __BSS_SIZE__)
    rts
