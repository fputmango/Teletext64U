; Copyright (C) 2026 Frank Putman
; SPDX-License-Identifier: GPL-3.0-or-later

.export _uii_settarget

.import _uii_target

.proc _uii_settarget
    ; parameter passed in A (cc65 convention for single byte arg)
    sta _uii_target
    rts
.endproc
