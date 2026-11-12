; Copyright (C) 2026 Frank Putman
; SPDX-License-Identifier: GPL-3.0-or-later

CONTROL_REG = $DF1C
STATUS_REG  = $DF1C

.export _uii_accept

.proc _uii_accept
    lda STATUS_REG
    ora #$02
    sta CONTROL_REG
wait:
    lda STATUS_REG
    and #$02
    bne wait
    rts
.endproc
