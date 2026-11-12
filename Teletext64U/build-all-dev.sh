#!/bin/bash
# Same as build-all.sh, but TARGET_DEV is defined: every edition talks to petsciiproxy.nl:8081 (the dev instance).
# The output goes to target/ with the usual names, so do NOT release what this script builds.
rm -rf target/
mkdir target

cd src

echo cl1: UCI+WIC
cl65 -O -t c64 -DTARGET_DEV -C ../teletext64.cfg -m teletext64ultimate.map \
	lib/ultimate_lib_small.c teletext_uci.c txtlib.c copy8.s blit_row.s blit_fill.s print_page.s toggle_conceal.s toggle_flash.s heap_start.s mouse_cb.s lib/uii_accept.s \
	lib/uii_readstatus.s lib/uii_readdata.s lib/uii_settarget.s -o ../target/teletext64u.prg

echo cl1a: UCI stand alone
cl65 -O -t c64 -DTARGET_DEV -C ../teletext64-wic64.cfg -m teletext64uci.map -DUCI_STANDALONE -DWIC64_OVERLAY \
	lib/ultimate_lib_small.c teletext_uci.c station_picker.c txtlib.c copy8.s blit_row.s blit_fill.s print_page.s toggle_conceal.s toggle_flash.s heap_start.s mouse_cb.s lib/uii_accept.s \
	lib/uii_readstatus.s lib/uii_readdata.s lib/uii_settarget.s -o ../target/teletext64-uci.prg

cl65 -O -t c64 -DTARGET_DEV -C ../teletext64-wic64.cfg -m teletext64wic64.map -DWIC64_OVERLAY \
	teletext_wic.c station_picker.c txtlib.c process_page_bytes.s \
	copy8.s blit_row.s blit_fill.s print_page.s toggle_conceal.s toggle_flash.s heap_start.s mouse_cb.s \
	wiclib/wic64.s \
	 -o ../target/teletext64-wic64.prg

cl65 -O -t c64 -DTARGET_DEV -C ../teletext64.cfg -m teletext64wic64-disk.map \
	-DDISK_CONFIG \
	teletext_wic.c txtlib.c process_page_bytes.s \
	copy8.s blit_row.s blit_fill.s print_page.s toggle_conceal.s toggle_flash.s heap_start.s mouse_cb.s \
	wiclib/wic64.s \
	 -o ../target/teletext64-wic64-disk.prg

echo cl1b: Meatloaf
cl65 -O -t c64 -DTARGET_DEV -C ../teletext64-wic64.cfg -m teletext64mlf.map -DWIC64_OVERLAY \
	teletext_mlf.c station_picker.c txtlib.c process_page_bytes.s \
	copy8.s blit_row.s blit_fill.s toggle_conceal.s toggle_flash.s heap_start.s mouse_cb.s \
	 -o ../target/teletext64-mlf.prg

echo cl2: config
cl65 -O -t c64 -DTARGET_DEV config.c station_picker.c txtlib.c -o ../target/config.prg

#echo cl3: txthires
#cl65 -O -t c64 txthires.c copy8.s blit_row.s -o ../target/txthires.prg

rm -f *.o

cd ../config
cp RunMeFirst-C64U.cfg ../target
cp RunMeFirst-U64.cfg ../target
cp teletext.vpl ../target

cd ../target

DISK_IMAGE=Teletext64U.d64

c1541 -format "teletext64u,sh" d64 $DISK_IMAGE \
-write teletext64u.prg teletext64u \
-write config.prg config \
-write ../config/ut64.cfg.prg ut64.cfg \

DISK_IMAGE=Teletext64W.d64

c1541 -format "teletext64w,sh" d64 $DISK_IMAGE \
-write teletext64-wic64-disk.prg teletext64w \
-write config.prg config \
-write ../config/ut64.cfg.prg ut64.cfg \

rm -f teletext64u.prg
rm -f teletext64-wic64-disk.prg

cd ..
