/*****************************************************************
Teletext 64 Ultimate
WiC64 edition

- D64 image version
- Stand alone version, also hosted on the WiC64 Portal
 
Copyright (C) 2026 Frank Putman
SPDX-License-Identifier: GPL-3.0-or-later
******************************************************************/
#include <stdlib.h>
#include <conio.h>
#include <string.h>
#include <c64.h>
#include <ctype.h>
#include <peekpoke.h>
#include "wiclib/wic64.h"
#include "txtlib.h"
#include "station_picker.h"

//#define WUD_TEST        // WUD=WiC64 User Data; skip portal login/load; go straight to config screen for UI testing

#define WIC64VERSION "WiC64 edition"
#define MOUSESUPPORT

#define WIC64_RESP_MAX 2048
static char response_buf[WIC64_RESP_MAX];

#ifndef DISK_CONFIG
static void wic64_enter_portal(void);
#endif

#include "teletext_common.c"

#ifndef DISK_CONFIG
// WiC64 firmware uppercases the URL path; petsciiproxy normalises to lowercase
// User Config: stored using the MAC-address, like WiC's mechanism for the WiC64 Portal
// http://petsciiproxy.nl/ud/<mac>/<cmd>
//
// load: GET /ud/<mac>/l -> \x00\x07<7 bytes> or \x01 (not found)
// save: GET /ud/<mac>/s/<14hexchars> -> \x00 ok, \x01 error
//
// Config layout: 7 bytes, little-endian: [0..1] refreshtime; [2..3] cycletime; [4] station; [5..6] startpage
#define WUD_DATA_BYTES   7
#define WUD_HEX_LEN      14   // WUD_DATA_BYTES * 2
#define WUD_DATA_OFFSET  (41 + PETSCIIPROXY_EXTRA)

#define WUD_URL_L_SIZE (44 + PETSCIIPROXY_EXTRA)
#define WUD_URL_S_SIZE (60 + PETSCIIPROXY_EXTRA)
#define WUD_AREA   (19 + 13 + WUD_URL_L_SIZE + WUD_URL_S_SIZE)
#define WUD_MAC    (response_buf + WIC64_RESP_MAX - WUD_AREA)
#define WUD_MAC_ID (response_buf + WIC64_RESP_MAX - WUD_AREA + 19)
#define WUD_URL_L  (response_buf + WIC64_RESP_MAX - WUD_AREA + 19 + 13)
#define WUD_URL_S  (response_buf + WIC64_RESP_MAX - WUD_AREA + 19 + 13 + WUD_URL_L_SIZE)

// Everything from here till the next pragma pop runs ONLY at startup. To save memory, all this code is put at
// SCREEN_RAM/BITMAP VIC bank 2. It will be overwritten over when init_bitmap_mode() starts using the address range.
#pragma code-name (push, "OVLCODE")
#pragma rodata-name (push, "OVLRODATA")

#include "settings_screen.c"

#ifndef WUD_TEST
// fetch user preferences from server
static unsigned char wud_load(void) {
	unsigned char status;
	unsigned char *d;
	int dc;

	dc = wic64_http_get(WUD_URL_L, response_buf, WIC64_RESP_MAX);
	
	if (wic64_status != WIC64_SUCCESS || dc < 1) return 0xFF;
	
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
	unsigned char s_st, s_rb;
	
	buf[0] = config.refreshtime & 0xFF;
	buf[1] = config.refreshtime >> 8;
	buf[2] = config.cycletime & 0xFF;
	buf[3] = config.cycletime >> 8;
	buf[4] = config.station;
	buf[5] = config.startpage & 0xFF;
	buf[6] = config.startpage >> 8;
	bytes_to_hex(buf, WUD_DATA_BYTES, WUD_URL_S + WUD_DATA_OFFSET);
	wic64_http_get(WUD_URL_S, response_buf, WIC64_RESP_MAX);
	s_st = wic64_status;
	s_rb = response_buf[0];
	return (s_st == WIC64_SUCCESS) && (s_rb == 0x00);
}
#endif

#pragma rodata-name (pop)
#pragma code-name (pop)
#endif

static void build_url(char *url, const char *pageStr, int subPageNumber) {
	char numstr[6];
	
#ifdef DISK_CONFIG
	strcpy(url, "http://");
	strcat(url, host);
	if (config.port != 80) {
		strcat(url, ":");
		strcat(url, utoa(config.port, numstr, 10));
	}
	strcat(url, "/");
#else
	strcpy(url, "http://" PETSCIIPROXY "/");
#endif
	strcat(url, stations[station_index].url_name);
	strcat(url, "/");
	strcat(url, pageStr);
	strcat(url, "-");
	strcat(url, itoa(subPageNumber, numstr, 10));
}

static void build_font_url(char *url, FontFileKind kind, bool thin) {
#ifdef DISK_CONFIG
	char numstr[6];
	strcpy(url, "http://");
	strcat(url, host);
	if (config.port != 80) {
		strcat(url, ":");
		strcat(url, utoa(config.port, numstr, 10));
	}
	strcat(url, "/fonts/");
#else
	strcpy(url, "http://" PETSCIIPROXY "/fonts/");
#endif
	strcat(url, font_filenames[kind][thin ? 1 : 0]);
}

bool fetch_font_file(FontFileKind kind, bool thin, unsigned char *dest, unsigned int len) {
	char url[72];
	int dc;

	build_font_url(url, kind, thin);
	petscii_to_ascii(url);
	dc = wic64_http_get(url, response_buf, WIC64_RESP_MAX);
	if (dc < (int)len) return false;
	memcpy(dest, response_buf, len);
	return true;
}

#ifdef MOUSESUPPORT
static void build_mouse_driver_url(char *url) {
#ifdef DISK_CONFIG
	char numstr[6];
	strcpy(url, "http://");
	strcat(url, host);
	if (config.port != 80) {
		strcat(url, ":");
		strcat(url, utoa(config.port, numstr, 10));
	}
	strcat(url, "/drivers/c64-1351.mou");
#else
	strcpy(url, "http://" PETSCIIPROXY "/drivers/c64-1351.mou");
#endif
}

unsigned char *fetch_mouse_driver(unsigned int *out_len) {
	char url[64];
	int dc;

	build_mouse_driver_url(url);
	petscii_to_ascii(url);
	dc = wic64_http_get(url, response_buf, WIC64_RESP_MAX);
	if (wic64_status != WIC64_SUCCESS || dc <= 0) { *out_len = 0; return 0; }
	*out_len = dc;
	return (unsigned char *)response_buf;
}
#endif

#ifndef DISK_CONFIG
static void wic64_enter_portal(void) {
	char url[] = "http://x.wic64.net/menue.prg";
	
	petscii_to_ascii(url);
	wic64_load_and_run(url);
}
#endif

#ifndef DISK_CONFIG
#pragma code-name (push, "OVLCODE")
#pragma rodata-name (push, "OVLRODATA")
#endif
bool wic64_present(void) {
	unsigned char attempt;
	unsigned long t;

	textcolor(SCREEN_COLOR_WHITE);
	cputs("WiC64: ");
	for (attempt = 0; attempt < 5; attempt++) {
		if (wic64_detect()) {
			textcolor(SCREEN_COLOR_YELLOW);
			cputs("OK\r\n");
			return true;
		}
		cputs(".");
		t = get_jiffies();
		while ((get_jiffies() - t) < 30); // wait 0.5 sec.
	}
	cputs("not detected\r\n");
	return false;
}
#ifndef DISK_CONFIG
#pragma rodata-name (pop)
#pragma code-name (pop)
#endif

static void get_teletext_page_impl(const char *pageStr, int subPageNumber) {
	char LPageNum[5];
	char url[80];
	int datacount = 0, i = 0;
	signed char result;
	bool had_loading_screen;

	LPageNum[0] = 'P';
	strcpy(LPageNum + 1, pageStr);
	build_url(url, pageStr, subPageNumber);
	petscii_to_ascii(url);
	init_page_processor(LPageNum);

	had_loading_screen = loading_screen_active;
	loading_screen_active = false;

	// Stopwatch: record start time here so we measure the full round-trip
	// (HTTP fetch + decode + render). Only active when the user enables it with F4
	if (stopwatch_enabled) stopwatch_jiffies = get_jiffies();

	datacount = wic64_http_get(url, response_buf, WIC64_RESP_MAX);

	{
		unsigned long fetch_start = get_jiffies();
		unsigned char retry = 0;
		
		while ((get_jiffies() - fetch_start) < PAGE_FETCH_TIMEOUT_JIFFIES &&
		       (wic64_status == WIC64_DRV_TIMEOUT ||
		        wic64_status == WIC64_CONNECTION_ERROR ||
		        wic64_status == WIC64_NETWORK_ERROR)) {
			{
				unsigned long t = get_jiffies();
				
				while ((get_jiffies() - t) < 30) ;
			}
			++retry;
			datacount = wic64_http_get(url, response_buf, WIC64_RESP_MAX);
		}
	}

	if (wic64_status != WIC64_SUCCESS || datacount == 0) {
		if (had_loading_screen) clear_bitmap_screen();
		txt_draw_string(LPageNum, PAGENUMBER_X, 0, SCREEN_COLOR_RED, SCREEN_COLOR_BLACK);
		return;
	}

	// drain keyboard buffer before rendering
	while (kbhit()) cgetc();

	// Assembly fast path: process the whole buffer in one call
	result = process_page_bytes((const unsigned char *)response_buf, datacount);
	if (result == -1) return;
	finalize_page();
	// display elapsed time for WiC64 network transfer + decode + render on screen
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

#ifndef DISK_CONFIG
#pragma code-name (push, "OVLCODE")
#pragma rodata-name (push, "OVLRODATA")
#endif
static void wic64_startup_config(void) {
#ifdef DISK_CONFIG
	{
		unsigned char dev = PEEK(186);
		cbm_device = (dev >= 8 && dev <= 11) ? dev : 8;
	}
	if (!config_load()) {
		cputs("No config found; run config util\r\n");
		exit(0);
	}
	//show_wait_bar(4);
#else
#if !defined(WUD_TEST)
	unsigned char wud_status;
#endif

#ifdef WUD_TEST
	// skip login/load and go straight to the config screen
	settings_screen();
#else
	gotoxy(0,9);
	if (!wic64_get_mac(WUD_MAC)) {
		textcolor(SCREEN_COLOR_RED);
		cputs("Error reading MAC-address\r\n");
		delay_jiffies(255);
		exit(0);
	}

	// Convert MAC-address to lowercase
	{
		char *p = WUD_MAC;
		while (*p) {
			if (*p >= 'A' && *p <= 'F') {
				*p += 32;
			}
			p++;
		}
	}

	// Build no-colon MAC-address which is the id for load/save config
	{
		char *src = WUD_MAC, *dst = WUD_MAC_ID;
		while (*src) {
			if (*src != ':') {
				*dst++ = *src;
			}
			src++;
		}
		*dst = '\0';
	}

	{
		char suf[20];
		strcpy(WUD_URL_L, "http://" PETSCIIPROXY "/ud/");
		petscii_to_ascii(WUD_URL_L);
		strcat(WUD_URL_L, WUD_MAC_ID);
		strcpy(suf, "/l"); petscii_to_ascii(suf); strcat(WUD_URL_L, suf);

		strcpy(WUD_URL_S, "http://" PETSCIIPROXY "/ud/");
		petscii_to_ascii(WUD_URL_S);
		strcat(WUD_URL_S, WUD_MAC_ID);
		strcpy(suf, "/s/00000000000000"); petscii_to_ascii(suf); strcat(WUD_URL_S, suf);
	}

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
#endif // WUD_TEST
	strcpy(host, "PETSCIIPROXY.NL");
	config.port = PETSCIIPROXY_PORT;
	refresh_interval_jiffies = (unsigned long)config.refreshtime * JIFFIES_PER_SECOND;
#endif // DISK_CONFIG
}
#ifndef DISK_CONFIG
#pragma rodata-name (pop)
#pragma code-name (pop)
#endif

void main(void) {
	int currentSubpageNumber = 0, currentPageNumber;

#ifdef MOUSESUPPORT
	// mod_load() needs a C heap; the start address is computed, see heap_start.s
	{
		unsigned heap_start = get_heap_start();
		_heapadd((void *)heap_start, HEAP_TOP - heap_start);
	}
#endif

	welcome_screen();
	gotoxy(0,7);
	if (!wic64_present()) exit(0);
	show_F8_for_help();

	wic64_startup_config();

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

#ifdef DISK_CONFIG
	show_wait_bar(4);
#endif

	init_bitmap_mode();
	init_row_tables();
	init_font_ptr();
	apply_g2_font_ptr();

#ifdef MOUSESUPPORT
	mouse_cursor_start();
#endif

	get_teletext_page(currentPageNumber, currentSubpageNumber);
	teletext_main_loop(currentPageNumber);
}
