; Copyright (C) 2026 Frank Putman
; SPDX-License-Identifier: GPL-3.0-or-later

STATUS_REG      = $DF1C
STATUS_DATA_REG = $DF1F

.export _uii_readstatus

.importzp ptr1
.import _uii_status

.proc _uii_readstatus
    ; uii_status[0] = 0
    lda #0
    sta _uii_status

    ; count = 0, use X as counter
    ldx #0

loop:
    ; check uii_isstatusdataavailable: bit 6 of STATUS_REG
    lda STATUS_REG
    and #$40
    beq done

    ; uii_status[count++] = *statusdatareg
    lda STATUS_DATA_REG
    sta _uii_status, x
    inx
    bne loop       ; safety: stop at 256

done:
    ; uii_status[count] = 0
    lda #0
    sta _uii_status, x

    ; return count in AX (cc65 convention: A = low byte, X = high byte)
    txa
    ldx #0
    rts
.endproc
