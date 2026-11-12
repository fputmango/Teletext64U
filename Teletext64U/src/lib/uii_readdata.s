; Copyright (C) 2026 Frank Putman
; SPDX-License-Identifier: GPL-3.0-or-later

STATUS_REG    = $DF1C
RESP_DATA_REG = $DF1E

.export _uii_readdata

.importzp ptr1
.import _uii_data

.proc _uii_readdata
    ; uii_data[0] = 0
    lda #0
    sta _uii_data

    ; use ptr1 as 16-bit index (low/high)
    lda #<_uii_data
    sta ptr1
    lda #>_uii_data
    sta ptr1+1

    ldx #0          ; X = high byte of count
    ldy #0          ; Y = low byte of count

loop:
    lda STATUS_REG
    and #$80
    beq done

    lda RESP_DATA_REG
    sta (ptr1), y
    iny
    bne loop
    inc ptr1+1      ; crossed 256 byte boundary
    inx             ; increment high byte of count
    bne loop

done:
    ; uii_data[count] = 0
    lda #0
    sta (ptr1), y

    ; return count: A = low byte (Y), X = high byte
    tya
    rts
.endproc
