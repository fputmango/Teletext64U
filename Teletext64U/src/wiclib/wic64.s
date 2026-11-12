; ===================================================================
; wic64.s - WiC64 driver for cc65 (C64 target)
; -------------------------------------------------------------------
; Note: this is a subset of the WiC64 library by Henning Liebenau.
; https://github.com/WiC64-Team/wic64-library
;
; Copyright 2023 Henning Liebenau <henning.liebenau@protonmail.com>
;
; Redistribution and use in source and binary forms, with or without
; modification, are permitted provided that the following conditions are met:
;
; 1. Redistributions of source code must retain the above copyright notice,
; this list of conditions and the following disclaimer.
;
; 2. Redistributions in binary form must reproduce the above copyright
; notice, this list of conditions and the following disclaimer in the
; documentation and/or other materials provided with the distribution.
;
; THIS SOFTWARE IS PROVIDED BY THE COPYRIGHT HOLDERS AND CONTRIBUTORS "AS IS"
; AND ANY EXPRESS OR IMPLIED WARRANTIES, INCLUDING, BUT NOT LIMITED TO, THE
; IMPLIED WARRANTIES OF MERCHANTABILITY AND FITNESS FOR A PARTICULAR PURPOSE
; ARE DISCLAIMED. IN NO EVENT SHALL THE COPYRIGHT HOLDER OR CONTRIBUTORS BE
; LIABLE FOR ANY DIRECT, INDIRECT, INCIDENTAL, SPECIAL, EXEMPLARY, OR
; CONSEQUENTIAL DAMAGES (INCLUDING, BUT NOT LIMITED TO, PROCUREMENT OF
; SUBSTITUTE GOODS OR SERVICES; LOSS OF USE, DATA, OR PROFITS; OR BUSINESS
; INTERRUPTION) HOWEVER CAUSED AND ON ANY THEORY OF LIABILITY, WHETHER IN
; CONTRACT, STRICT LIABILITY, OR TORT (INCLUDING NEGLIGENCE OR OTHERWISE)
; ARISING IN ANY WAY OUT OF THE USE OF THIS SOFTWARE, EVEN IF ADVISED OF THE
; POSSIBILITY OF SUCH DAMAGE.
; ===================================================================

.export _wic64_detect
.export _wic64_http_get
.export _wic64_get_mac
.export _wic64_status
.export _wic64_response_size
.export _wic64_load_and_run

.import popax
.importzp ptr1, ptr2

; --- WiC64 command codes used here --------------------------------
WIC64_GET_VERSION_STRING = $00
WIC64_HTTP_GET           = $01
WIC64_GET_MAC            = $14

; --- CIA #2 (userport) --------------------------------------------
CIA2_PRA  = $DD00              ; PA2 (bit 2) signals direction to ESP
CIA2_PRB  = $DD01              ; userport data byte
CIA2_DDRA = $DD02
CIA2_DDRB = $DD03              ; $00 = input, $FF = output
CIA2_ICR  = $DD0D              ; FLAG2 (bit 4) = handshake from ESP

; --- VIC-II raster MSB: speed-independent clock ------------------------
; Bit 7 of $D011 flips about every 10 ms (video clock rate), independent
; of CPU/CIA turbo speed.
; $D012 is NOT used: on some turbo boards it reads as a random value.
VIC_CTRL1  = $D011             ; bit 7 = raster MSB

; --- Ultimate 64 turbo control ----------------------------------------
; We need to drop to 1Mhz for each WiC64 transfer or else the ESP misses
; the ack (CIA clock scales at turbo speeds)
U64_TURBO_SLOW = $D07A          ; write-only: any value → 1 MHz
U64_TURBO_FAST = $D07B          ; write-only: any value → firmware turbo speed

; --- Sentinel status used by this driver only ---------------------
WIC64_DRV_TIMEOUT = $FF        ; userport handshake never completed

; ===================================================================
; Storage
; ===================================================================

.segment "DATA"
wic64_timeout_val:    .byte $02   ; default timeout: ~2 s per FLAG2 wait

; -------------------------------------------------------------------
; wic64_tapebuf_stub: receive-and-run code assembled to run from $0334
; (C64 tape buffer, $0334-$03FB), stored here in DATA as initialized bytes.
; Size 123 bytes. wic64_load_and_run patches it before copying it:
;   offsets 32,33 ($0354/$0355):   destination page pointer, reset to $0801
;   offsets 121,122 ($03AD/$03AE): response_size = PRG body size (lo, hi)
; -------------------------------------------------------------------
wic64_tapebuf_stub:
        .byte $A9,$37           ; LDA #$37  (bank in KERNAL)
        .byte $85,$01           ; STA $01
        .byte $A9,$47           ; LDA #$47  (NMI lo)
        .byte $8D,$18,$03       ; STA $0318
        .byte $A9,$FE           ; LDA #$FE  (NMI hi)
        .byte $8D,$19,$03       ; STA $0319
        .byte $AE,$AE,$03       ; LDX $03AE  (response_size hi = page count)
        .byte $F0,$18           ; BEQ $035F  (no pages → recv_bytes)
        .byte $A0,$00           ; LDY #$00   (ldy_loop)
        .byte $AD,$0D,$DD       ; LDA $DD0D  (recv_pages_wait: poll FLAG2)
        .byte $29,$10           ; AND #$10
        .byte $F0,$F9           ; BEQ $0349  (not ready)
        .byte $AD,$01,$DD       ; LDA $DD01  (read byte, asserts /PC2)
        .byte $99,$01,$08       ; STA $0801,Y [lo@offset32=$0354, hi@offset33=$0355]
        .byte $C8               ; INY
        .byte $D0,$F0           ; BNE $0349  (loop for 256 bytes)
        .byte $EE,$55,$03       ; INC $0355  (advance page pointer)
        .byte $CA               ; DEX
        .byte $D0,$E8           ; BNE $0347  (next page → ldy_loop)
        .byte $AE,$AD,$03       ; LDX $03AD  (response_size lo = remaining bytes)
        .byte $F0,$1F           ; BEQ $0383  (no remainder)
        .byte $AD,$54,$03       ; LDA $0354  (dst_pages lo, still $01)
        .byte $8D,$7D,$03       ; STA $037D  (→ bytes dst lo)
        .byte $AD,$55,$03       ; LDA $0355  (dst_pages hi, advanced by pages)
        .byte $8D,$7E,$03       ; STA $037E  (→ bytes dst hi)
        .byte $A0,$00           ; LDY #$00
        .byte $AD,$0D,$DD       ; LDA $DD0D  (recv_bytes_wait: poll FLAG2)
        .byte $29,$10           ; AND #$10
        .byte $F0,$F9           ; BEQ $0372  (not ready)
        .byte $AD,$01,$DD       ; LDA $DD01  (read byte)
        .byte $99,$00,$00       ; STA $0000,Y [lo@offset73=$037D, hi@offset74=$037E]
        .byte $C8               ; INY
        .byte $CA               ; DEX
        .byte $D0,$EF           ; BNE $0372  (loop)
        .byte $A9,$01           ; LDA #$01   (++ BEQ target: set BASIC end ptr)
        .byte $85,$2D           ; STA $2D
        .byte $A9,$08           ; LDA #$08
        .byte $85,$2E           ; STA $2E
        .byte $AD,$AD,$03       ; LDA $03AD  (response_size lo)
        .byte $18               ; CLC
        .byte $65,$2D           ; ADC $2D
        .byte $85,$2D           ; STA $2D
        .byte $A5,$2E           ; LDA $2E
        .byte $6D,$AE,$03       ; ADC $03AE  (response_size hi)
        .byte $85,$2E           ; STA $2E
        .byte $A2,$FF           ; LDX #$FF
        .byte $9A               ; TXS        (reset stack)
        .byte $A9,$00           ; LDA #$00
        .byte $85,$C6           ; STA $C6    (clear keyboard buffer)
        .byte $20,$A3,$FD       ; JSR $FDA3  (KERNAL init_io)
        .byte $20,$8A,$FF       ; JSR $FF8A  (KERNAL reset_vectors)
        .byte $20,$8E,$A6       ; JSR $A68E  (BASIC reset_program_pointer)
        .byte $4C,$AE,$A7       ; JMP $A7AE  (BASIC perform_run — no return)
        .byte $00,$00           ; response_size word [offset121=$03AD, offset122=$03AE]

.segment "BSS"
_wic64_status:        .res 1
_wic64_response_size: .res 2
wic64_user_irq:       .res 1
wic64_count_l:        .res 1
wic64_count_m:        .res 1
wic64_count_h:        .res 1
wic64_xfer_lo:        .res 1
wic64_xfer_hi:        .res 1
wic64_max_lo:         .res 1
wic64_max_hi:         .res 1
wic64_actual_lo:      .res 1
wic64_actual_hi:      .res 1
wic64_request_buf:    .res 4    ; 'R', cmd, sz_lo, sz_hi

; ===================================================================
; Code
; ===================================================================

.segment "CODE"

; -------------------------------------------------------------------
; wait for FLAG2 (bit 4 of $dd0d).
; out: C clear = handshake received, C set = timed out
; clobbers A, the wic64_count_* counters
;
; The timeout counts raster-MSB flips (see VIC_CTRL1), so it lasts the
; same real time at any CPU speed:
;   timeout = timeout_val x 100 flips x ~10 ms = timeout_val x ~1 s
;   default (timeout_val=$02): ~2 s
; -------------------------------------------------------------------
wic64_wait_flag2:
        lda     #$10
        bit     CIA2_ICR                ; fast path: FLAG2 already latched?
        bne     @ok
        ; Slow path: count VIC raster MSB transitions as clock reference
        lda     VIC_CTRL1               ; snapshot current raster MSB (bit 7)
        and     #$80
        sta     wic64_count_l           ; wic64_count_l = $00 or $80
        lda     wic64_timeout_val
        sta     wic64_count_h           ; outer counter: timeout_val units
        lda     #100                    ; flips per timeout unit
        sta     wic64_count_m
@wait:  lda     CIA2_ICR                ; poll FLAG2 (read clears latch)
        and     #$10
        bne     @ok                     ; FLAG2 arrived — done
        lda     VIC_CTRL1               ; check raster MSB
        and     #$80
        cmp     wic64_count_l           ; same as stored?
        beq     @wait                   ; yes → keep polling
        ; Raster MSB flipped
        sta     wic64_count_l           ; update stored MSB
        dec     wic64_count_m
        bne     @wait                   ; still counting transitions
        ; unit done: reload count_m, decrement outer counter
        lda     #100
        sta     wic64_count_m
        dec     wic64_count_h
        bne     @wait                   ; more outer units remaining
        sec                             ; timed out
        rts
@ok:    clc
        rts

; -------------------------------------------------------------------
; wic64_begin: prepare CIA #2 and disable IRQs around a transfer.
;
; Forces the CIA to 1 MHz with an explicit fast->slow edge ($D07B, then
; $D07A), waits for the speed change to settle (one raster-MSB flip) and
; clears any stale FLAG2 latch.
;
; Why $D07B first: with "Fast CIA" on, a lone $D07A from an idle 1 MHz
; state can be a no-op for the CIA. $D07B arms fast mode, so the following
; $D07A is a genuine fast->slow transition (this also covers the first
; call). With Fast CIA off in the U64 BIOS both writes are harmless.
;
; /PC2 is NOT fired to clear FLAG2: after a completed recv_data the ESP32
; is idle, and an extra /PC2 shifts the R-protocol handshake by one, giving
; a send_header timeout on every request after the first. A single
; CIA2_ICR read does it, as in the reference library (wic64lib.lib);
; wic64_send_header reads CIA2_ICR again before sending 'R'.
; -------------------------------------------------------------------
wic64_begin:
        lda     CIA2_DDRA               ; PA2 must be output
        ora     #$04
        sta     CIA2_DDRA
        ; --- U64 turbo: force CIA to 1 MHz (see header) ---
        lda     #$00
        sta     U64_TURBO_FAST          ; → turbo (arms Fast CIA if enabled)
        sta     U64_TURBO_SLOW          ; → 1 MHz; CIA /PC2 now ~1 µs
        ; settling delay: one raster-MSB flip
        lda     VIC_CTRL1
        and     #$80
        sta     wic64_count_l
:       lda     VIC_CTRL1
        and     #$80
        cmp     wic64_count_l
        beq     :-
        lda     CIA2_ICR                ; clear stale FLAG2 (no /PC2, see header)
        php                             ; remember current IRQ flag
        pla
        and     #$04
        sta     wic64_user_irq
        sei
        clc
        rts

; -------------------------------------------------------------------
; wic64_end: restore CIA #2, the turbo speed and the IRQ flag.
;
; PA2 is driven LOW. This is essential after a failed send_header: it
; raised PA2 HIGH without completing the exchange, and the next call's
; "ora #$04" on an already-HIGH PA2 gives no LOW→HIGH edge, so the ESP
; never re-arms and every later call times out at send_header. After a
; successful transaction it is a no-op (recv_header already drove it LOW).
;
; $D07B restores the firmware-configured turbo speed; without it the CPU
; stays at 1 MHz between requests. wic64_begin does its own $D07B→$D07A
; pair, so this write needs no special ordering.
;
; Turbo modes: "U64 Turbo Registers" mode works fully. In "Manual"
; (hardware-forced) mode $D07A/$D07B are ignored and the CIA follows the
; Fast CIA BIOS setting: fine with Fast CIA OFF; with Fast CIA ON /PC2 is
; ~25 ns, below the ESP32 detection threshold, so Fast CIA must be
; disabled in the U64 BIOS.
; -------------------------------------------------------------------
wic64_end:
        lda     #$00                    ; userport back to input
        sta     CIA2_DDRB
        lda     CIA2_PRA                ; ensure PA2 LOW for clean edge on next command
        and     #$FB
        sta     CIA2_PRA
        lda     CIA2_ICR                ; clear any stale FLAG2 latch
        lda     #$00
        sta     U64_TURBO_FAST          ; restore firmware turbo speed
        lda     wic64_user_irq
        bne     :+
        cli
:       rts

; -------------------------------------------------------------------
; wic64_send_header: send the 4-byte "R"-protocol request header
; (wic64_request_buf must have been filled).
;
; PA2 is LOW on entry (left so by wic64_recv_header or wic64_end). The
; "ora #$04" gives the LOW→HIGH edge that puts the ESP in command-receive
; mode, as in the official reference. No LOW pulse is done here: it makes
; the ESP raise an extra direction-change FLAG2 and shifts the handshake
; by one for all later requests.
; -------------------------------------------------------------------
wic64_send_header:
        lda     CIA2_PRA
        ora     #$04                    ; PA2 HIGH — command mode (LOW→HIGH edge)
        sta     CIA2_PRA
        ; Arming delay: one raster-MSB flip, far more than the ESP32 needs
        ; to arm its /PC2 interrupt handler.
        lda     VIC_CTRL1               ; snapshot raster MSB
        and     #$80
        sta     wic64_count_l           ; (reinitialised by wait_flag2 on entry)
:       lda     VIC_CTRL1
        and     #$80
        cmp     wic64_count_l           ; wait until bit 7 flips
        beq     :-
        lda     #$ff                    ; userport -> output
        sta     CIA2_DDRB
        lda     CIA2_ICR                ; clear any FLAG2 latch from previous op
        ldy     #$00
@loop:  lda     wic64_request_buf,y
        sta     CIA2_PRB
        jsr     wic64_wait_flag2
        bcs     @to
        iny
        cpy     #$04
        bne     @loop
        clc
@to:    rts

; -------------------------------------------------------------------
; wic64_send_data: send (wic64_xfer_hi:wic64_xfer_lo) bytes from (ptr1)
; ptr1 may be modified.
; -------------------------------------------------------------------
wic64_send_data:
        ldx     wic64_xfer_hi
        beq     @rem
@page:  ldy     #$00
@pl:    lda     (ptr1),y
        sta     CIA2_PRB
        jsr     wic64_wait_flag2
        bcs     @to
        iny
        bne     @pl
        inc     ptr1+1
        dex
        bne     @page
@rem:   ldx     wic64_xfer_lo
        beq     @done
        ldy     #$00
@rl:    lda     (ptr1),y
        sta     CIA2_PRB
        jsr     wic64_wait_flag2
        bcs     @to
        iny
        dex
        bne     @rl
@done:  clc
@to:    rts

; -------------------------------------------------------------------
; wic64_recv_header: read the 3-byte "R"-protocol response header
; into _wic64_status / _wic64_response_size.
; -------------------------------------------------------------------
wic64_recv_header:
        lda     #$00                    ; userport -> input
        sta     CIA2_DDRB
        ; Same arming delay as send_header: the ESP32 must arm its PA2
        ; interrupt before PA2 goes LOW below.
        lda     VIC_CTRL1
        and     #$80
        sta     wic64_count_l
:       lda     VIC_CTRL1
        and     #$80
        cmp     wic64_count_l
        beq     :-
        lda     CIA2_PRA                ; signal ready: PA2 low
        and     #$fb
        sta     CIA2_PRA
        jsr     wic64_wait_flag2        ; ESP confirms direction change
        bcs     @to
        lda     CIA2_PRB                ; dummy read - asserts /PC2, lets ESP start sending
        jsr     wic64_wait_flag2        ; status byte
        bcs     @to
        lda     CIA2_PRB
        sta     _wic64_status
        jsr     wic64_wait_flag2        ; size lo
        bcs     @to
        lda     CIA2_PRB
        sta     _wic64_response_size
        jsr     wic64_wait_flag2        ; size hi
        bcs     @to
        lda     CIA2_PRB
        sta     _wic64_response_size+1
        clc
@to:    rts

; -------------------------------------------------------------------
; wic64_recv_data: receive (wic64_xfer_hi:wic64_xfer_lo) bytes into (ptr2)
; ptr2 may be modified.
; -------------------------------------------------------------------
wic64_recv_data:
        ldx     wic64_xfer_hi
        beq     @rem
@page:  ldy     #$00
@pl:    jsr     wic64_wait_flag2
        bcs     @to
        lda     CIA2_PRB
        sta     (ptr2),y
        iny
        bne     @pl
        inc     ptr2+1
        dex
        bne     @page
@rem:   ldx     wic64_xfer_lo
        beq     @done
        ldy     #$00
@rl:    jsr     wic64_wait_flag2
        bcs     @to
        lda     CIA2_PRB
        sta     (ptr2),y
        iny
        dex
        bne     @rl
@done:  clc
@to:    rts

; -------------------------------------------------------------------
; wic64_discard_data: receive and throw away wic64_xfer_* bytes
; -------------------------------------------------------------------
wic64_discard_data:
        ldx     wic64_xfer_hi
        beq     @rem
@page:  ldy     #$00
@pl:    jsr     wic64_wait_flag2
        bcs     @to
        lda     CIA2_PRB
        iny
        bne     @pl
        dex
        bne     @page
@rem:   ldx     wic64_xfer_lo
        beq     @done
@rl:    jsr     wic64_wait_flag2
        bcs     @to
        lda     CIA2_PRB
        dex
        bne     @rl
@done:  clc
@to:    rts

; -------------------------------------------------------------------
; wic64_strlen_ptr1: store strlen() of C string at (ptr1) into
; wic64_xfer_lo/hi (saturates at 255). ptr1 is preserved.
; -------------------------------------------------------------------
wic64_strlen_ptr1:
        ldy     #$00
@loop:  lda     (ptr1),y
        beq     @done
        iny
        bne     @loop
        dey                             ; saturate at 255
@done:  sty     wic64_xfer_lo
        lda     #$00
        sta     wic64_xfer_hi
        rts

; -------------------------------------------------------------------
; wic64_compute_recv_size:
;   xfer = min(_wic64_response_size, wic64_max), and remember actual
; -------------------------------------------------------------------
wic64_compute_recv_size:
        lda     _wic64_response_size+1
        cmp     wic64_max_hi
        bcc     @use_resp
        bne     @use_max
        lda     _wic64_response_size
        cmp     wic64_max_lo
        bcc     @use_resp
@use_max:
        lda     wic64_max_lo
        sta     wic64_xfer_lo
        sta     wic64_actual_lo
        lda     wic64_max_hi
        sta     wic64_xfer_hi
        sta     wic64_actual_hi
        rts
@use_resp:
        lda     _wic64_response_size
        sta     wic64_xfer_lo
        sta     wic64_actual_lo
        lda     _wic64_response_size+1
        sta     wic64_xfer_hi
        sta     wic64_actual_hi
        rts

; -------------------------------------------------------------------
; wic64_compute_discard_size: xfer = response_size - actual
; -------------------------------------------------------------------
wic64_compute_discard_size:
        sec
        lda     _wic64_response_size
        sbc     wic64_actual_lo
        sta     wic64_xfer_lo
        lda     _wic64_response_size+1
        sbc     wic64_actual_hi
        sta     wic64_xfer_hi
        rts

; ===================================================================
; unsigned int __fastcall__
;   wic64_http_get(const char *url, void *buf, unsigned int max);
;
;   A/X = max          (passed in registers - last fastcall arg)
;   stack:  url (pushed first), buf (pushed second)
;   popax pops buf first, then url.
;
; Returns: bytes actually written into buf (in A/X).
; Sets globals: wic64_status, wic64_response_size.
; ===================================================================
_wic64_http_get:
        sta     wic64_max_lo
        stx     wic64_max_hi
        jsr     popax                   ; buf
        sta     ptr2
        stx     ptr2+1
        jsr     popax                   ; url
        sta     ptr1
        stx     ptr1+1
        jsr     wic64_strlen_ptr1
        ; build request header: 'R', HTTP_GET, sz_lo, sz_hi
        lda     #$52                    ; 'R' as raw ASCII - do NOT use #'R', ca65 with -t c64
        sta     wic64_request_buf       ; maps char literals through PETSCII, giving $D2 not $52
        lda     #WIC64_HTTP_GET
        sta     wic64_request_buf+1
        lda     wic64_xfer_lo
        sta     wic64_request_buf+2
        lda     wic64_xfer_hi
        sta     wic64_request_buf+3
        ; reset state
        lda     #$00
        sta     _wic64_status
        sta     _wic64_response_size
        sta     _wic64_response_size+1
        sta     wic64_actual_lo
        sta     wic64_actual_hi

        jsr     wic64_begin
        jsr     wic64_send_header
        bcs     @timeout
        jsr     wic64_send_data
        bcs     @timeout
        jsr     wic64_recv_header
        bcs     @timeout
        jsr     wic64_compute_recv_size
        jsr     wic64_recv_data
        bcs     @timeout
        jsr     wic64_compute_discard_size
        jsr     wic64_discard_data      ; ignore timeout on discard
        ; wic64_end's CIA2_ICR read clears any trailing FLAG2 latch, as in the
        ; reference library (no explicit poll after the last byte).
        jsr     wic64_end
        lda     wic64_actual_lo
        ldx     wic64_actual_hi
        rts
@timeout:
        jsr     wic64_end
        lda     #WIC64_DRV_TIMEOUT
        sta     _wic64_status
        lda     #$00
        ldx     #$00
        rts

; ===================================================================
; unsigned char __fastcall__ wic64_get_mac(char *buf);
;
; Fetches the WiC64 MAC address (WIC64_GET_MAC = $14): "AA:BB:CC:DD:EE:FF"
; (17 chars) plus the NUL sent by the firmware, at most 19 bytes.
;
; A/X = buf pointer (fastcall, single argument)
; Returns: 1 = success, 0 = timeout (wic64_status = WIC64_DRV_TIMEOUT)
; ===================================================================
_wic64_get_mac:
        sta     ptr2                    ; buf lo
        stx     ptr2+1                  ; buf hi
        ; build request: 'R', WIC64_GET_MAC, $00, $00  (no payload)
        lda     #$52                    ; 'R' raw ASCII
        sta     wic64_request_buf
        lda     #WIC64_GET_MAC
        sta     wic64_request_buf+1
        lda     #$00
        sta     wic64_request_buf+2
        sta     wic64_request_buf+3
        ; reset state
        sta     _wic64_status
        sta     _wic64_response_size
        sta     _wic64_response_size+1
        sta     wic64_actual_lo
        sta     wic64_actual_hi
        ; reply is at most 19 bytes (MAC string + NUL)
        lda     #19
        sta     wic64_max_lo
        lda     #$00
        sta     wic64_max_hi
        jsr     wic64_begin
        jsr     wic64_send_header
        bcs     @gmac_timeout
        ; no payload — skip send_data
        jsr     wic64_recv_header
        bcs     @gmac_timeout
        jsr     wic64_compute_recv_size
        jsr     wic64_recv_data
        bcs     @gmac_timeout
        jsr     wic64_end
        lda     #$01                    ; return 1 = success
        ldx     #$00
        rts
@gmac_timeout:
        jsr     wic64_end
        lda     #WIC64_DRV_TIMEOUT
        sta     _wic64_status
        lda     #$00                    ; return 0 = timeout
        ldx     #$00
        rts

; ===================================================================
; unsigned char wic64_detect(void);
; Returns 1 if a WiC64 responds, 0 otherwise
; ===================================================================
_wic64_detect:
        ; build a zero-payload GET_VERSION_STRING request
        lda     #$52                    ; 'R' protocol byte (raw ASCII, not PETSCII)
        sta     wic64_request_buf
        lda     #WIC64_GET_VERSION_STRING
        sta     wic64_request_buf+1
        lda     #$00
        sta     wic64_request_buf+2
        sta     wic64_request_buf+3
        sta     _wic64_status
        sta     _wic64_response_size
        sta     _wic64_response_size+1
        ; force a short probe timeout
        lda     wic64_timeout_val
        pha
        lda     #$01
        sta     wic64_timeout_val
        jsr     wic64_begin
        jsr     wic64_send_header
        bcs     @nope               ; send_header timeout = no device at all
        ; recv_header timeout is expected with legacy firmware and does NOT
        ; mean "device absent": if send_header succeeded the device is present
        ; (same logic as the official library).
        jsr     wic64_recv_header   ; ignore carry
        ; Drain the version string with raw reads like the ACME reference
        ; library: no FLAG2 wait, the nop;nop gives the ESP time to queue
        ; the next byte. The bytes are ignored; we only advance through
        ; response_size so the ESP's send state machine reaches "done".
        ; response_size is 0 if recv_header timed out.
        ldy     _wic64_response_size    ; version strings are always <256 bytes
        beq     @drain_done
@drain: lda     CIA2_PRB                ; raw read — fires /PC2, no FLAG2 wait
        nop
        nop
        dey
        bne     @drain
@drain_done:
        ; wic64_end's LDA CIA2_ICR clears any stale trailing FLAG2 latch.
        jsr     wic64_end
        pla
        sta     wic64_timeout_val
        lda     #$01
        ldx     #$00
        rts
@nope:
        jsr     wic64_end
        pla
        sta     wic64_timeout_val
        lda     #WIC64_DRV_TIMEOUT
        sta     _wic64_status
        lda     #$00
        ldx     #$00
        rts

; ===================================================================
; void __fastcall__ wic64_load_and_run(const char *url);
;
; Fetches a PRG from the given URL via WIC64_HTTP_GET ($01) and
; runs it from $0801 using the standard BASIC/KERNAL mechanism.
;
; Protocol:
;   1. Sends a normal HTTP GET request (same wire format as wic64_http_get).
;   2. Receives the 3-byte response header (status + size).
;   3. Reads and discards the 2-byte PRG load address from the stream.
;   4. Patches wic64_tapebuf_stub with the remaining body size.
;   5. Copies wic64_tapebuf_stub (123 bytes) to the tape buffer ($0334).
;   6. Jumps to $0334 — the stub receives the PRG body directly from
;      CIA2, stores it at $0801, adjusts BASIC pointers, and JMPs to
;      BASIC perform_run.  Does NOT return on success.
;
; On error (timeout or firmware error) sets wic64_status and returns.
;
; A/X = url pointer (fastcall, single argument).
; ===================================================================

; Constants for patching wic64_tapebuf_stub before copying to $0334
TAPEBUF_ADDR            = $0334
TAPEBUF_SIZE            = 123
TAPEBUF_DST_LO_OFFSET   = 32   ; byte offset of destination_pointer_pages lo in stub
TAPEBUF_DST_HI_OFFSET   = 33   ; byte offset of destination_pointer_pages hi in stub
TAPEBUF_RSPSIZE_OFFSET  = 121  ; byte offset of response_size word in stub

_wic64_load_and_run:
        sta     ptr1                    ; url lo
        stx     ptr1+1                  ; url hi
        jsr     wic64_strlen_ptr1       ; wic64_xfer_lo/hi = strlen(url)
        ; Build 'R'-protocol request header: 'R', HTTP_GET, sz_lo, sz_hi
        lda     #$52                    ; 'R' raw ASCII (NOT #'R': ca65 maps to PETSCII $D2)
        sta     wic64_request_buf
        lda     #WIC64_HTTP_GET
        sta     wic64_request_buf+1
        lda     wic64_xfer_lo
        sta     wic64_request_buf+2
        lda     wic64_xfer_hi
        sta     wic64_request_buf+3
        ; Reset state
        lda     #$00
        sta     _wic64_status
        sta     _wic64_response_size
        sta     _wic64_response_size+1
        sta     wic64_actual_lo
        sta     wic64_actual_hi

        jsr     wic64_begin
        jsr     wic64_send_header
        bcs     @lar_timeout
        jsr     wic64_send_data         ; send URL payload
        bcs     @lar_timeout
        jsr     wic64_recv_header       ; fills wic64_status, wic64_response_size
        bcs     @lar_timeout
        ; Check firmware-level error (e.g. HTTP 404 → WIC64_SERVER_ERROR)
        lda     _wic64_status
        bne     @lar_err

        ; discard the 2-byte PRG load address at the start of the stream
        jsr     wic64_wait_flag2
        bcs     @lar_timeout
        lda     CIA2_PRB                ; load address lo (discarded)
        jsr     wic64_wait_flag2
        bcs     @lar_timeout
        lda     CIA2_PRB                ; load address hi (discarded)

        ; body size = response_size - 2  (subtract the discarded load address bytes)
        lda     _wic64_response_size
        sec
        sbc     #$02
        sta     wic64_actual_lo         ; reuse actual_lo/hi for body size
        lda     _wic64_response_size+1
        sbc     #$00
        sta     wic64_actual_hi

        ; Patch wic64_tapebuf_stub before copying:
        ;   - response_size word at offset TAPEBUF_RSPSIZE_OFFSET
        ;   - reset destination pointer bytes to $0801 (may be dirty from prev call)
        lda     wic64_actual_lo
        sta     wic64_tapebuf_stub + TAPEBUF_RSPSIZE_OFFSET
        lda     wic64_actual_hi
        sta     wic64_tapebuf_stub + TAPEBUF_RSPSIZE_OFFSET + 1
        lda     #$01
        sta     wic64_tapebuf_stub + TAPEBUF_DST_LO_OFFSET  ; $0801 lo
        lda     #$08
        sta     wic64_tapebuf_stub + TAPEBUF_DST_HI_OFFSET  ; $0801 hi

        ; Copy stub to tape buffer ($0334–$03AE)
        ; CIA2 is still configured from wic64_begin/recv_header (DDRB=input,
        ; PA2 LOW).  The stub polls CIA2 directly for the remaining PRG bytes.
        ldx     #TAPEBUF_SIZE - 1
@copy:  lda     wic64_tapebuf_stub,x
        sta     TAPEBUF_ADDR,x
        dex
        bpl     @copy

        jmp     TAPEBUF_ADDR            ; hand off to stub — does not return on success

@lar_timeout:
        jsr     wic64_end
        lda     #WIC64_DRV_TIMEOUT
        sta     _wic64_status
        rts

@lar_err:
        jsr     wic64_end               ; wic64_status already set by recv_header
        rts
