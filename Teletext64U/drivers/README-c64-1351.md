# c64-1351.mou

`c64-1351.mou` (798 bytes) is the cc65 1351 mouse driver with one small patch. `c64-1351.s` is cc65's `libsrc/c64/mou/c64-1351.s` (cc65 commit `357f64e4e`); the patch is the `SetupPots` / `RestorePots` pair.

## What the patch fixes

The stock driver reads the mouse position from the SID without selecting which control port is routed to it. That selection lives in bits 6/7 of `CIA1_PRA`, the same bits the keyboard scan keeps rewriting, so the driver reads whatever the scan happened to leave behind. The result is erratic tracking on real C64s (and in VICE).

The patched driver selects port 1 once per IRQ, waits ~4 ms for the signal to settle, reads X and Y, and then restores `CIA1_PRA` for the keyboard scanner. This follows the approach of Commodore's original 1351 driver. Tested on VICE, the Commodore 64 Ultimate and a stock C64.

## Rebuilding

```
ca65 -t c64 -g -o c64-1351.o c64-1351.s
ca65 -t c64 -g -o zeropage.o <cc65-source>/libsrc/runtime/zeropage.s
ld65 -o c64-1351-v2.mou -t module c64-1351.o zeropage.o
```

Keep the output name `c64-1351-v2.mou` (ld65 stores it in the file, and this name gives the shipped 798 bytes). Copy the result to `c64-1351.mou` here and to `PetsciiProxy/drivers/`. `zeropage.s` comes from the same cc65 commit. 

**Keep `MOUSE_DRV_RAW_MAX` in sync:** `src/teletext_uci.c` defines
`MOUSE_DRV_RAW_MAX 798`, which must equal the size of `c64-1351.mou`. If a rebuild changes the size, update that constant, or the driver will fail to load on UCI.

## Licence

cc65 driver by Ullrich von Bassewitz and Greg King, zlib licence; modified as described above. See `THIRD-PARTY-NOTICES` in the repo root.
