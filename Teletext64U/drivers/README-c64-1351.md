# c64-1351.mou — patched, not stock

`c64-1351.mou` (798 bytes) is **not** the unmodified cc65 driver. It's built from
`c64-1351.s` in this folder, which is cc65's own `libsrc/c64/mou/c64-1351.s`
(from cc65 `V2.19 - Git 357f64e4e`) with a deliberate patch applied, now on its
second revision.

## The bug this fixes

The stock driver reads `SID_ADConv1`/`SID_ADConv2` (the mouse's X/Y position)
without ever selecting which control port's signal should be routed there.
`CIA1_PRA` bits 6/7 do that selection (`$40` = port 1, `$80` = port 2) - but
those same bits are also the keyboard scan's own column-select lines, which
get rewritten on every keyboard scan. The stock driver just reads whatever
bit pattern the keyboard scan happened to leave behind, which is inconsistent
tick-to-tick and never guaranteed to mean "port 1."

That's why the mouse tracks erratically on real hardware and in VICE (both
faithfully reproduce this keyboard-scan/paddle-mux interference), while
working fine on the C64 Ultimate/Ultimate 64 (an FPGA reimplementation that
doesn't share a real analog multiplexer between the two).

`c64-pot.s`, in the same cc65 codebase, already does this correctly for its
own device - it explicitly writes `$40` to `CIA1_PRA` and waits before
reading. `c64-1351.s` never did.

## Revision history

**v1 (778 bytes, 2026-09-20):** added a `SelectPot1` subroutine (select $40,
delay ~2.6ms) called before *each* of the two `SID_ADConv` reads. Confirmed
this fixed jitter in VICE. It also appeared not to fix the first real stock
C64 tested, but that machine turned out to be hardware-faulty (see Status),
so v1 was never actually tested on healthy real hardware. v2 superseded it.

**v2 (798 bytes, 2026-09-21 - current):** rewritten to match Commodore's own
official 1351 driver source exactly (`M1351.64`, by Hedley Davis and Fred
Bowen, 27-Oct-1986, shipped on the 1351 demo disk - see
`m1351.64.src.seq`/`mouse64.src.seq` in this folder), instead of re-deriving
the fix from first principles. Two concrete differences from v1 that v2
fixes:

1. **Select once per IRQ tick, not once per axis.** Selecting a port routes
   *both* the X and Y analog signals to the SID at once, so there's no need
   to reselect/redelay between reading POTX and POTY - v1 did this
   redundantly (and wastefully - double the delay for no benefit).
2. **Save and restore `CIA1_PRA` around the read.** v1 permanently pinned
   `CIA1_PRA` to `$40` after every single IRQ tick and never gave it back to
   the keyboard scanner. The official driver explicitly saves the CIA1_PRA
   value the keyboard scan left there *before* overwriting it, and restores
   that exact value once both pot reads are done. This is the more likely
   fix for the real-hardware-only symptom: VICE's CIA/SID emulation may not
   reproduce whatever real-silicon side effect of leaving that register
   pinned actually caused the erratic tracking.

Also increased the settling delay from ~2.6ms to ~4ms (`LDX #4 / LDY #199`),
matching the official driver's `SETPOT` timing exactly, including its
non-obvious loop structure (Y is not reloaded between outer passes, so the
first pass is short and the remaining three are near-maximum-length - this
is replicated deliberately, not a bug).

```asm
SetupPots:
        lda     CIA1_PRA                ; save whatever the keyboard scan
        sta     CIASave                 ; left CIA1_PRA set to
        lda     #%01000000              ; select control port 1's paddles/mouse
        sta     CIA1_PRA
        ldx     #4                      ; ~4ms delay, matches the official
        ldy     #199                    ; 1986 Commodore driver's SETPOT
@Outer: dey
        bne     @Outer
        dex
        bne     @Outer
        rts

RestorePots:
        lda     CIASave                 ; hand CIA1_PRA back to the keyboard
        sta     CIA1_PRA                ; scanner exactly as it left it
        rts
```

`SetupPots` is called once, right before the X read; `RestorePots` is called
once, right before `CDRAW`, after both X and Y have been fully processed -
mirroring the official driver's single select/single restore per tick.

**Status: tested and working** on VICE, the C64 Ultimate (UCI), and a healthy
real stock C64 (2026-09-21).

Note: the first stock C64 tried showed erratic tracking with *every* driver
version and with Commodore's own 1351 demo disk. A raw diagnostic
(`../tools/mouse_raw_diag.s`, which reads the SID POT registers directly)
showed both control ports pegged at ~$FE/$FF regardless of the mouse - a
hardware fault in that machine (likely SID POT section or the 4066 analog
switch), not a driver bug. Use that diagnostic to check a machine's pot lines
before suspecting the driver.

## Rebuilding

```
ca65 -t c64 -g -o c64-1351.o c64-1351.s
ca65 -t c64 -g -o zeropage.o <cc65-source>/libsrc/runtime/zeropage.s
ld65 -o c64-1351-v2.mou -t module c64-1351.o zeropage.o
```

Link it as `c64-1351-v2.mou`: ld65 stores the output name in the file, and this
name reproduces the shipped 798 bytes (only the build timestamp differs). Then
copy the result to `c64-1351.mou` here and to `PetsciiProxy/drivers/`.

`zeropage.s` isn't vendored here - fetch it from the matching cc65 commit
(357f64e4e) if rebuilding, or grab it from a local cc65 checkout.

## If this needs reverting

The stock cc65 driver is `libsrc/c64/mou/c64-1351.s` in cc65 commit `357f64e4e`
(752 bytes when built). Older copies are in `backup/mouse_cleanup/`.

## Licensing

`c64-1351.s` is cc65's driver (zlib licence, Ullrich von Bassewitz and Greg King)
with the patch described above. `m1351.64.src.seq` and `mouse64.src.seq` are
Commodore's 1986 source: reference only, never publish them.

## Important: keep `MOUSE_DRV_RAW_MAX` in sync

`Teletext64U/src/teletext_uci.c` has `#define MOUSE_DRV_RAW_MAX 798` which
**must** exactly match this file's byte size - UCI's fetch stops reading at
exactly that many bytes. If this driver is rebuilt and its size changes,
that constant needs updating to match, or the mouse driver will silently
fail to load on UCI (truncated fetch).
