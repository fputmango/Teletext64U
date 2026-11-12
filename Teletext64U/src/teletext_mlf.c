/*****************************************************************
Teletext 64 Ultimate
Meatloaf edition
First released August 2026

Copyright (C) 2026 Frank Putman
SPDX-License-Identifier: GPL-3.0-or-later
******************************************************************/
#include <stdlib.h>
#include <conio.h>
#include <string.h>
#include <c64.h>
#include <cbm.h>
#include <peekpoke.h>
#include "txtlib.h"
#include "station_picker.h"

#define MLFVERSION "Meatloaf edition"
#define MOUSESUPPORT

#include "teletext_common.c"

#define MLF_RESP_MAX 2048
static char response_buf[MLF_RESP_MAX];

// cbm_read() reads in chunks so we can update the loading spinner (2 alternating dots)
#define MLF_CHUNK 256 // 128

// the device number will be used where the program was started from
static unsigned char mlf_http_device = 8;

// User Config: stored using the MAC-address, same as WiC's mechanism
// http://petsciiproxy.nl/ud/<mac>/<cmd>
//
// load: GET /ud/<mac>/l -> \x00\x07<7 bytes> or \x01 (not found)
// save: GET /ud/<mac>/s/<14hexchars> -> \x00 ok, \x01 error
//
// Config layout: 7 bytes, little-endian: [0..1] refreshtime; [2..3] cycletime; [4] station; [5..6] startpage
#define WUD_DATA_BYTES   7
#define WUD_HEX_LEN      14   // WUD_DATA_BYTES*2
#define WUD_DATA_OFFSET  (41 + PETSCIIPROXY_EXTRA)

static char wud_mac[19];		// full MAC address, format: "aa:bb:cc:dd:ee:ff" + NUL
static char wud_mac_id[13];	// no-colon MAC, format: "aabbccddeeff" + NUL

static char wud_url_l[44 + PETSCIIPROXY_EXTRA];	// load URL (26 prefix + 12 MAC + "/l" + NUL)
static char wud_url_s[60 + PETSCIIPROXY_EXTRA];	// save URL; 14-char hex field at offset 41

// Everything from here till the next pragma pop runs ONLY at startup. To save memory, all this code is put at
// SCREEN_RAM/BITMAP VIC bank 2. It will be overwritten over when init_bitmap_mode() starts using the address range.
#pragma code-name (push, "OVLCODE")
#pragma rodata-name (push, "OVLRODATA")

#define MLF_MAC_DEVICE    30
#define MLF_MAC_SEC_ADDR  15
static bool meatloaf_get_mac(char *buf) {
	int n;
	unsigned int total = 0;
	unsigned char i;

	if (cbm_open(CBM_LOGICAL_FILE_NUMBER, MLF_MAC_DEVICE, MLF_MAC_SEC_ADDR, "macaddress") != 0) {
		return false;
	}
	do {
		n = cbm_read(CBM_LOGICAL_FILE_NUMBER, buf + total, 18 - total);
		if (n > 0) total += n;
	} while (n > 0 && total < 18);
	cbm_close(CBM_LOGICAL_FILE_NUMBER);
	if (total < 1) return false;

	{
		unsigned char colons = 0, digits_in_group = 0;
		char c;

		for (i=0; i<(unsigned char)total; i++) {
			c = buf[i];
			if (c == ':') {
				if (colons >= 5) break;
				colons++;
				digits_in_group = 0;
				continue;
			}
			if (!((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F'))) {
				break;
			}
			if (colons == 5) {
				if (digits_in_group >= 2) break;
				digits_in_group++;
			}
		}
	}
	buf[i] = '\0';
	return i > 0;
}

#include "settings_screen.c"

static int meatloaf_url_get(const char *url, char *buf, unsigned int max) {
	int n;
	unsigned int total = 0;

	if (cbm_open(CBM_LOGICAL_FILE_NUMBER, mlf_http_device, CBM_SEC_ADDR, url) != 0) {
		return -1;
	}
	do {
		n = cbm_read(CBM_LOGICAL_FILE_NUMBER, buf + total, max - total);
		if (n > 0) total += n;
	} while (n > 0 && total < max);
	cbm_close(CBM_LOGICAL_FILE_NUMBER);
	return total;
}

// fetch user preferences from server
static unsigned char wud_load(void) {
	unsigned char status;
	unsigned char *d;
	int dc;

	dc = meatloaf_url_get(wud_url_l, response_buf, MLF_RESP_MAX);

	if (dc < 1) return 0xFF;

	status = response_buf[0];
	if (status == 0x00 && dc >= (2 + WUD_DATA_BYTES) && response_buf[1] == WUD_DATA_BYTES) {
		d = (unsigned char *)response_buf + 2;

		config.refreshtime = (unsigned int)d[0] | ((unsigned int)d[1] << 8);
		config.cycletime = (unsigned int)d[2] | ((unsigned int)d[3] << 8);

		config.station = d[4];
		if (config.station >= STATION_COUNT) config.station = 0;

		config.startpage = (unsigned int)d[5] | ((unsigned int)d[6] << 8);
		if (config.startpage < 100 || config.startpage > 899) config.startpage = 101;
	}
	return status;
}

static bool wud_save(void) {
	unsigned char buf[WUD_DATA_BYTES];
	int dc;

	buf[0] = config.refreshtime & 0xFF;
	buf[1] = config.refreshtime >> 8;
	buf[2] = config.cycletime & 0xFF;
	buf[3] = config.cycletime >> 8;
	buf[4] = config.station;
	buf[5] = config.startpage & 0xFF;
	buf[6] = config.startpage >> 8;
	bytes_to_hex(buf, WUD_DATA_BYTES, wud_url_s + WUD_DATA_OFFSET);
	dc = meatloaf_url_get(wud_url_s, response_buf, MLF_RESP_MAX);
	return (dc >= 1) && (response_buf[0] == 0x00);
}

#pragma rodata-name (pop)
#pragma code-name (pop)

static void build_url(char *url, const char *pageStr, int subPageNumber) {
	char numstr[6];

	strcpy(url, "http://" PETSCIIPROXY "/");
	strcat(url, stations[station_index].url_name);
	strcat(url, "/");
	strcat(url, pageStr);
	strcat(url, "-");
	strcat(url, itoa(subPageNumber, numstr, 10));
}

// fetch and display teletext page in one run
static void get_teletext_page_impl(const char *pageStr, int subPageNumber) {
	char LPageNum[5], url[80];
	int datacount;
	signed char result;
	bool had_loading_screen;
	unsigned char spinner_col, spinner_shown_frame = 0xFF;

	LPageNum[0] = 'P';
	strcpy(LPageNum + 1, pageStr);
	spinner_col = PAGENUMBER_X + strlen(LPageNum);
	build_url(url, pageStr, subPageNumber);
	init_page_processor(LPageNum);

	had_loading_screen = loading_screen_active;
	loading_screen_active = false;
	txt_draw_string(LPageNum, PAGENUMBER_X, 0, 1, 0);

	if (stopwatch_enabled) stopwatch_jiffies = get_jiffies();

#ifdef MOUSESUPPORT
	// prevents sprite flashing during data transfers
	mouse_hide_if_active();
#endif
	if (cbm_open(CBM_LOGICAL_FILE_NUMBER, mlf_http_device, CBM_SEC_ADDR, url) != 0) {
#ifdef MOUSESUPPORT
		mouse_show_if_active();
#endif
		if (had_loading_screen) clear_bitmap_screen();
		txt_draw_string(LPageNum, PAGENUMBER_X, 0, SCREEN_COLOR_RED, SCREEN_COLOR_BLACK);
		return;
	}

	{
		unsigned long fetch_start = get_jiffies();
		int n;

		datacount = 0;
		do {
			n = cbm_read(CBM_LOGICAL_FILE_NUMBER, response_buf + datacount, MLF_CHUNK);
			if (n > 0) datacount += n;
			{
				unsigned long waited = get_jiffies() - fetch_start;
				if (waited >= LOAD_INDICATOR_DELAY_JIFFIES) {
					unsigned char frame = (waited / LOAD_INDICATOR_DELAY_JIFFIES) % 4;
					if (frame != spinner_shown_frame) {
						spinner_shown_frame = frame;
						if (frame==0) txt_draw_string(". ", spinner_col, 0, SCREEN_COLOR_WHITE, SCREEN_COLOR_BLACK);
						else if (frame==1) txt_draw_string("..", spinner_col, 0, SCREEN_COLOR_WHITE, SCREEN_COLOR_BLACK);
						else if (frame==2) txt_draw_string(" .", spinner_col, 0, SCREEN_COLOR_WHITE, SCREEN_COLOR_BLACK);
						else if (frame==3) txt_draw_string("  ", spinner_col, 0, SCREEN_COLOR_WHITE, SCREEN_COLOR_BLACK);
					}
				}
			}
		} while (n == MLF_CHUNK && datacount + MLF_CHUNK <= MLF_RESP_MAX);
	}
	cbm_close(CBM_LOGICAL_FILE_NUMBER);
#ifdef MOUSESUPPORT
	mouse_show_if_active();
#endif

	if (datacount <= 0) {
		if (had_loading_screen) clear_bitmap_screen();
		txt_draw_string(LPageNum, PAGENUMBER_X, 0, SCREEN_COLOR_RED, SCREEN_COLOR_BLACK);
		return;
	}

	while (kbhit()) cgetc();

	result = process_page_bytes((const unsigned char *)response_buf, datacount);
	if (result == -1) return;

	finalize_page();
	if (stopwatch_enabled) time_elapsed();
}

void get_teletext_page(int pageNumber, int subPageNumber) {
	char numstr[4];

	itoa(pageNumber, numstr, 10);
	get_teletext_page_impl(numstr, subPageNumber);
}

void get_teletext_page_str(const char *pageStr, int subPageNumber) {
	get_teletext_page_impl(pageStr, subPageNumber);
}

// At startup read MAC-address, load or create config from serrver
#pragma code-name (push, "OVLCODE")
#pragma rodata-name (push, "OVLRODATA")
static void meatloaf_startup_config(void) {
	unsigned char wud_status;

	gotoxy(0,7);
	textcolor(SCREEN_COLOR_WHITE);
	cputs("Meatloaf: ");
	if (!meatloaf_get_mac(wud_mac)) {
		textcolor(SCREEN_COLOR_RED);
		cputs("could not read MAC-address\r\n");
		textcolor(SCREEN_COLOR_WHITE);
		cputs("\r\nPossible solution: flash your Meatloaf\r\nwith the lastest firmware\r\n");
		delay_jiffies(255);
		exit(0);
	}
	textcolor(SCREEN_COLOR_YELLOW);
	cputs("OK\r\n"); //cputs(wud_mac); // don't show MAC-address anymore
	cputs("\r\n");

	// Convert MAC-address to lowercase
	{
		char *p = wud_mac;
		while (*p) {
			if (*p >= 'A' && *p <= 'F') {
				*p += 32;
			}
			p++;
		}
	}

	// Build no-colon MAC-address which is the id for load/save config
	{
		char *src = wud_mac;
		char *dst = wud_mac_id;
		char octet[2];
		unsigned char oi;

		while (*src) {
			oi = 0;
			while (*src && *src != ':') {
				if (oi < 2) octet[oi] = *src;
				oi++;
				src++;
			}
			if (*src == ':') src++;
			if (oi == 1) {
				*dst++ = '0';
				*dst++ = octet[0];
			} else if (oi >= 2) {
				*dst++ = octet[0];
				*dst++ = octet[1];
			}
		}
		*dst = '\0';
	}

	strcpy(wud_url_l, "http://" PETSCIIPROXY "/ud/");
	strcat(wud_url_l, wud_mac_id);
	strcat(wud_url_l, "/l");

	strcpy(wud_url_s, "http://" PETSCIIPROXY "/ud/");
	strcat(wud_url_s, wud_mac_id);
	strcat(wud_url_s, "/s/00000000000000");

	textcolor(SCREEN_COLOR_WHITE);
	cputs("Connecting to " PETSCIIPROXY "...");
	wud_status = wud_load();
	if (wud_status == 0x00) {
		cputs("ok\r\n\n");
		config_show();
		{
			unsigned char secs;
			bool go_settings = false;
			bool just_go_on_with_it = false;
			textcolor(SCREEN_COLOR_WHITE);
			gotoxy(0,17);
			cputs("(or press cursor right to continue)");
			for (secs = 10; secs > 0 && !(go_settings || just_go_on_with_it); --secs) {
				unsigned long t;
				gotoxy(0,16);
				textcolor(SCREEN_COLOR_WHITE);
				cputs("Press ");
				textcolor(SCREEN_COLOR_YELLOW);
				cputs("F2");
				textcolor(SCREEN_COLOR_WHITE);
				{
					char secsbuf[4];
					cputs(" to change config (");
					cputs(utoa(secs, secsbuf, 10));
					cputs(") ");
				}
				t = get_jiffies();
				while ((get_jiffies() - t) < JIFFIES_PER_SECOND) {
					if (kbhit()) {
						unsigned char k = cgetc();
						if (k == KEY_F2) { go_settings = true; break; }
						if (k == KEY_CRSR_RIGHT) { just_go_on_with_it = true; break; }
					}
				}
			}
			if (go_settings) {
				settings_screen();
				gotoxy(0,24);
				cclear(40);
				gotoxy(0,24);
				textcolor(SCREEN_COLOR_GRAY_2);
				cputs("Saving settings to server...");
				if (!wud_save()) {
					textcolor(SCREEN_COLOR_RED);
					cputs("failed!\r\n");
					delay_jiffies(30);
				} else {
					cputs("done\r\n");
					delay_jiffies(30);
				}
			} else {
				cputs("\r\n\n\n");
			}
		}
	} else {
		textcolor(SCREEN_COLOR_YELLOW);
		cputs("not found\r\n"); // user preferences
		delay_jiffies(60);

		// set defaults
		config.refreshtime = 60;
		config.cycletime = 15;
		config.station = STATION_NOSTT; // Why NOS-TT? Because I'm Dutch!
		config.startpage = 101;

		settings_screen();
		clrscr();
		POKEW(0xD020, 0x00);
		gotoxy(0,0);
		textcolor(SCREEN_COLOR_WHITE);
		cputs("Saving settings to server...");
		if (!wud_save()) {
			textcolor(SCREEN_COLOR_RED);
			cputs("failed!\r\n");
			delay_jiffies(30);
		} else {
			cputs("done\r\n");
			delay_jiffies(30);
		}
	}
	strcpy(host, "PETSCIIPROXY.NL");
	config.port = PETSCIIPROXY_PORT;
	refresh_interval_jiffies = (unsigned long)config.refreshtime * JIFFIES_PER_SECOND;
}
#pragma rodata-name (pop)
#pragma code-name (pop)

bool fetch_font_file(FontFileKind kind, bool thin, unsigned char *dest, unsigned int len) {
	char url[64]; // has to fit urls like these "http://petsciiproxy.nl/fonts/teletext-languages-bold.bin"
	int n;
	unsigned int total = 0;

	strcpy(url, "http://" PETSCIIPROXY "/fonts/");
	strcat(url, font_filenames[kind][thin ? 1 : 0]);

	if (cbm_open(CBM_LOGICAL_FILE_NUMBER, mlf_http_device, CBM_SEC_ADDR, url) != 0) {
		return false;
	}
	do {
		n = cbm_read(CBM_LOGICAL_FILE_NUMBER, dest + total, len - total);
		if (n > 0) total += n;
	} while (n > 0 && total < len);
	cbm_close(CBM_LOGICAL_FILE_NUMBER);
	return total == len;
}

#ifdef MOUSESUPPORT
// runs before init_bitmap_mode() overwrites the memory
unsigned char *fetch_mouse_driver(unsigned int *out_len) {
	int dc = meatloaf_url_get("http://" PETSCIIPROXY "/drivers/c64-1351.mou", response_buf, MLF_RESP_MAX);
	if (dc <= 0) { *out_len = 0; return 0; }
	*out_len = dc;
	return (unsigned char *)response_buf;
}
#endif

void main(void) {
	int currentSubpageNumber = 0, currentPageNumber;

	mlf_http_device = PEEK(186);
	if (mlf_http_device < 4 || mlf_http_device > 30) mlf_http_device = 8;

#ifdef MOUSESUPPORT
	// mod_load() needs a C heap; the start address is computed, see heap_start.s
	{
		unsigned heap_start = get_heap_start();
		_heapadd((void *)heap_start, HEAP_TOP - heap_start);
	}
	// The BSS window (~490 B) is barely enough for mod_load() (~465-487 B), so two more regions are needed
	_heapadd((void *)MLF_HEAP1_ADDR, MLF_HEAP1_SIZE);
	_heapadd((void *)MLF_HEAP2_ADDR, MLF_HEAP2_SIZE);
#endif

	welcome_screen();
	show_F8_for_help();

	meatloaf_startup_config();

	station_index = config.station;
	currentPageNumber = config.startpage;

	textcolor(SCREEN_COLOR_WHITE);
	cputs("Loading fonts...");
	if (!load_fonts_at_startup()) {
		cputs("Failed to load fonts\r\n");
		exit(0);
	}
	apply_language(stations[station_index].lang);
	cputs("done\r\n");

#ifdef MOUSESUPPORT
	textcolor(SCREEN_COLOR_WHITE);
	cputs("Loading mouse driver....");
	if (mouse_driver_load()) {
		cputs("done\r\n");
	} else {
		cputs("not available\r\n");
	}
#endif

	init_bitmap_mode();
	init_row_tables();
	init_font_ptr();
	apply_g2_font_ptr();

#ifdef MOUSESUPPORT
	mouse_cursor_start();
#endif

	// Immediatly load page at startup
	get_teletext_page(currentPageNumber, currentSubpageNumber);
	teletext_main_loop(currentPageNumber);
}
