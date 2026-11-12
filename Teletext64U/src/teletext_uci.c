/*****************************************************************
Teletext 64 Ultimate
Ultimate Command Interface edition

The UCI D64 image version was the first edition of the program
First public release 1.0.0: March 4, 2026

The standalone edition was published on October 9, 2026
 
Copyright (C) 2026 Frank Putman
SPDX-License-Identifier: GPL-3.0-or-later

Originially based on ultimate_dos-1.2.docx and command interface.docx
https://github.com/markusC64/1541ultimate2/tree/master/doc
******************************************************************/
#include <stdlib.h>
#include <conio.h>
#include <string.h>
#include <c64.h>
#include <peekpoke.h>
#include "lib/ultimate_lib_small.h"
#include "txtlib.h"

#define UCIVERSION
#define MOUSESUPPORT

#ifdef UCI_STANDALONE
#include "station_picker.h"
#define UCIEDITION "UCI edition"
#endif

// Ultimate Command Interface register
#define UCI_ID (*(unsigned char*)0xDF1D)

// Ultimate speed control
#define U64_TURBO_SLOW  (*(volatile unsigned char *)0xD07A)
#define U64_TURBO_FAST  (*(volatile unsigned char *)0xD07B)
#define U64_TURBO_CTRL  (*(volatile unsigned char *)0xD031)

// Ultimate speed indices
#define U64_SPEED_1MHZ   0
#define U64_SPEED_4MHZ   3
#define U64_SPEED_8MHZ   5
#define U64_SPEED_20MHZ  9
#define U64_SPEED_32MHZ  11
#define U64_SPEED_40MHZ  12
#define U64_SPEED_48MHZ  13

#include "teletext_common.c"

bool UCI_enabled(void) {
	return UCI_ID == 201;
}

unsigned char detect_u64_turbo_mode(void) {
	volatile unsigned char *ctrl = (volatile unsigned char *)0xD031;
	unsigned char saved, probe, readback;
	
	saved = *ctrl;
	probe = (saved == 0x05) ? 0x06 : 0x05;
	*ctrl = probe;
	readback = *ctrl;
	*ctrl = saved;
	return (readback == probe);
}

#if !defined(UCI_STANDALONE)
bool proxy_running(void) {
	static unsigned char socketnr = 0;
	
	textcolor(SCREEN_COLOR_WHITE);
	cputs("PetsciiProxy: ");
	uii_settarget(TARGET_NETWORK);
	uii_getinterfacecount();
	socketnr = uii_tcpconnect(host, config.port);
	if (socketnr == 0) {
		cputs("could not connect!\r\n\n");
	} else {
		cputs("detected\r\n\n");
		uii_socketclose(socketnr);
	}
	return socketnr > 0;
}
#endif

// 256 byte network buffer in the free gap of VIC bank 2 (a static array costs BSS):
// - offset 0		HTTP request (get_teletext_page_impl() & uci_http_get())
// - offset 80	status_line: header being parsed
// - offset 120	path: input of uci_http_get(), read while the request is built
#define UCI_NETBUF         0x9C00
#define UCI_NETBUF_SIZE    256
#define UCI_STATUS_OFFSET  80
#define UCI_STATUS_LEN     40
#define UCI_PATH_OFFSET    (UCI_STATUS_OFFSET + UCI_STATUS_LEN)
#define UCI_PATH_MAX       40	// longest now is "/fonts/teletext-languages-bold.bin" = 35 + NUL
#define UCI_REQUEST_MAX    (4 + UCI_PATH_MAX + 17 + sizeof(host) + 4)	// "GET " path " HTTP/1.1\r\nHost: " host "\r\n\r\n"
#define shared_request_buf ((char*)UCI_NETBUF)

// compile time checks: if array size is negative then the layout above is no longer valid
typedef char uci_netbuf_below_g2[(UCI_NETBUF + UCI_NETBUF_SIZE <= FONT_G2_ADDR) ? 1 : -1];
typedef char uci_path_fits[(UCI_PATH_OFFSET + UCI_PATH_MAX <= UCI_NETBUF_SIZE) ? 1 : -1];
typedef char uci_request_fits[(UCI_REQUEST_MAX <= UCI_NETBUF_SIZE) ? 1 : -1];
typedef char uci_path_copied_before_overwritten[(4 + UCI_PATH_MAX <= UCI_PATH_OFFSET) ? 1 : -1];
typedef char uci_socketread_fits[(2 + 512 + 1 <= sizeof(uii_data)) ? 1 : -1];

// fetch and display teletext page in one run; separating the networking and displaying part seems more elegant,
// but decreases the performance significantly (i tried)
static void get_teletext_page_impl(const char *pageStr, int subPageNumber) {
	char *p;
	unsigned char socketnr = 0;
	int datacount = 0;
	int i = 0;
	char LPageNum[5];
#define myrequest shared_request_buf
	char done = false;
	signed char result;
	unsigned long fetch_start;
	unsigned char spinner_col;
	unsigned char spinner_shown_frame = 0xFF;

	LPageNum[0] = 'P';
	strcpy(LPageNum + 1, pageStr);
	spinner_col = PAGENUMBER_X + strlen(LPageNum);

	// Build the HTTP GET request string
	// e.g. "GET /NOS-TT/705-2 HTTP/1.1\r\nHost: 192.168.0.1\r\n\r\n"
	strcpy(myrequest, "GET /");
	p = myrequest + 5;
	strcpy(p, stations[station_index].url_name);
	p += strlen(p); *p++ = '/';
	strcpy(p, pageStr);
	p += strlen(p); *p++ = '-';
	itoa(subPageNumber, p, 10);
	p += strlen(p);
	strcpy(p, " HTTP/1.1\r\nHost: ");
	p += strlen(p);
	strcpy(p, host);
	p += strlen(p);
	strcpy(p, "\r\n\r\n");

	init_page_processor(LPageNum);

	// drain any stale keys
	while (kbhit()) cgetc();

	txt_draw_string(LPageNum, PAGENUMBER_X, 0, 1, 0);

	if (stopwatch_enabled) stopwatch_jiffies = get_jiffies();

	uii_settarget(TARGET_NETWORK);

	uii_getinterfacecount();
	socketnr = uii_tcpconnect(host, config.port);

	if (socketnr == 0) {
		// Couldn't connect
		txt_draw_string(LPageNum, PAGENUMBER_X, 0, SCREEN_COLOR_RED, SCREEN_COLOR_BLACK);
		return;
	}

	petscii_to_ascii(myrequest);
	uii_socketwrite(socketnr, myrequest);

	fetch_start = get_jiffies();

	while (uii_success() && !done) {
		datacount = uii_socketread(socketnr, 512);
		while (datacount == -1 && (get_jiffies() - fetch_start) < PAGE_FETCH_TIMEOUT_JIFFIES) {
			{
				// animated dots will be shown if it take more time to fetch a page (>0.5s)
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
			datacount = uii_socketread(socketnr, 512);
		}
		if (datacount < 0) {
			txt_draw_string(LPageNum, PAGENUMBER_X, 0, SCREEN_COLOR_RED, SCREEN_COLOR_BLACK); // '404'
			uii_socketclose(socketnr);
			return;
		}

		while (kbhit()) cgetc();

		// Note: I dont use the ways faster process_page_bytes.s in the UCI edition. Not linking this is saves me 1.1KB
		for (i=0; i<datacount; ++i) {
			result = process_page_byte(uii_data[2+i]);
			if (result == -1) { uii_socketclose(socketnr); return; }
			if (result == 1 || result == 2) { done = true; break; }
		}
	}
	uii_socketclose(socketnr);
	finalize_page();

	// Measures: TCP connect + HTTP request/response + decode + render.
	if (stopwatch_enabled) time_elapsed();
}
#undef myrequest

void get_teletext_page(int pageNumber, int subPageNumber) {
	char numstr[4];
	
	itoa(pageNumber, numstr, 10);
	get_teletext_page_impl(numstr, subPageNumber);
}

void get_teletext_page_str(const char *pageStr, int subPageNumber) {
	get_teletext_page_impl(pageStr, subPageNumber);
}

#ifdef UCI_STANDALONE
// "content-length:" in ASCII
static const unsigned char content_length_lc[15] = {
	0x63,0x6F,0x6E,0x74,0x65,0x6E,0x74,0x2D,0x6C,0x65,0x6E,0x67,0x74,0x68,0x3A
};
#endif

static unsigned int uci_http_get(const char *path, unsigned char *dest, unsigned int maxlen) {
	unsigned char socketnr;
	int datacount, i;
#ifdef UCI_STANDALONE
	unsigned int content_len = 0;
	unsigned char have_len = false;
	unsigned char k;
#endif

#define myrequest shared_request_buf
#define status_line ((char*)(UCI_NETBUF + UCI_STATUS_OFFSET))
	unsigned char header_done = false;
	unsigned int received = 0;
	unsigned long fetch_start;
	unsigned char last4[4] = {0, 0, 0, 0};

	unsigned char status_len = 0;
	unsigned char status_line_done = false;

	strcpy(myrequest, "GET ");
	strcat(myrequest, path);
	strcat(myrequest, " HTTP/1.1\r\nHost: ");
	strcat(myrequest, host);
	strcat(myrequest, "\r\n\r\n");

	uii_settarget(TARGET_NETWORK);
	uii_getinterfacecount();
	socketnr = uii_tcpconnect(host, config.port);
	if (socketnr == 0) return 0;

	petscii_to_ascii(myrequest);
	uii_socketwrite(socketnr, myrequest);

	fetch_start = get_jiffies();
	while (uii_success() && received < maxlen) {
		datacount = uii_socketread(socketnr, 512);
		while (datacount == -1 && (get_jiffies() - fetch_start) < PAGE_FETCH_TIMEOUT_JIFFIES) {
			datacount = uii_socketread(socketnr, 512);
		}
		if (datacount <= 0) break;

		for (i=0; i<datacount; ++i) {
			unsigned char b = uii_data[2+i];

			if (!header_done) {
				if (!status_line_done) {
					if (b == '\n') {
						status_line_done = true;
						status_line[status_len] = '\0';
						if (!strstr(status_line, "200")) {
							uii_socketclose(socketnr);
							return 0;
						}
#ifdef UCI_STANDALONE
						status_len = 0;
#endif
					} else if (b != '\r' && status_len < UCI_STATUS_LEN - 1) {
						status_line[status_len++] = b;
					}
				}
#ifdef UCI_STANDALONE
				else {
					// A header line after the status line: remember its Content-Length
					if (b == '\n') {
						status_line[status_len] = '\0';
						if (status_len > 15) {
							for (k=0; k<15; ++k) {
								if ((status_line[k] | 0x20) != content_length_lc[k]) break;
							}
							if (k == 15) {
								content_len = 0;
								for (k=15; k < status_len; ++k) {
									if (status_line[k] >= '0' && status_line[k] <= '9') {
										content_len = content_len * 10 + (status_line[k] - '0');
									}
								}
								have_len = true;
							}
						}
						status_len = 0;
					} else if (b != '\r' && status_len < UCI_STATUS_LEN - 1) {
						status_line[status_len++] = b;
					}
				}
#endif
				last4[0] = last4[1];
				last4[1] = last4[2];
				last4[2] = last4[3];
				last4[3] = b;
				if (last4[0] == 13 && last4[1] == 10 && last4[2] == 13 && last4[3] == 10) {
					header_done = true;
#ifdef UCI_STANDALONE
					if (have_len && content_len < maxlen) maxlen = content_len;
#endif
				}
				continue;
			}
			if (received < maxlen) dest[received++] = b;
		}
	}
	uii_socketclose(socketnr);
	return received;
}
#undef myrequest
#undef status_line

bool fetch_font_file(FontFileKind kind, bool thin, unsigned char *dest, unsigned int len) {
#define path ((char*)(UCI_NETBUF + UCI_PATH_OFFSET))
	strcpy(path, "/fonts/");
	strcat(path, font_filenames[kind][thin ? 1 : 0]);
	return uci_http_get(path, dest, len) == len;
#undef path
}

#ifdef MOUSESUPPORT
#define MOUSE_DRV_RAW_MAX 798	// must be the EXACT filesize
#ifdef UCI_STANDALONE
#define MOUSE_DRV_STAGE ((unsigned char*)0x8000)
unsigned char *fetch_mouse_driver(unsigned int *out_len) {
	*out_len = uci_http_get("/drivers/c64-1351.mou", MOUSE_DRV_STAGE, MOUSE_DRV_RAW_MAX);
	return *out_len ? MOUSE_DRV_STAGE : 0;
}
#else
unsigned char *fetch_mouse_driver(unsigned int *out_len) {
	static unsigned char *buf = NULL;

	if (!buf) buf = malloc(MOUSE_DRV_RAW_MAX);
	if (!buf) { *out_len = 0; return 0; }
	*out_len = uci_http_get("/drivers/c64-1351.mou", buf, MOUSE_DRV_RAW_MAX);
	if (*out_len == 0) {
		free(buf);
		buf = NULL;
		return 0;
	}
	// let mouse_driver_load() know it's safe to free buffer after reading
	mouse_driver_buffer_owned = true;
	return buf;
}
#endif // UCI_STANDALONE
#endif // MOUSESUPPORT

#ifdef UCI_STANDALONE
#define NET_CMD_GET_NETADDR	0x04
#define WUD_DATA_BYTES			7
#define WUD_STATUS_OK				0x00
#define WUD_STATUS_NONE			0x01	// server has no profile for this MAC-address yet (=first run)
#define WUD_STATUS_ERROR		0xFF	// no connection/bad HTTP status/unexpected reply

static char wud_key[13];	// MAC+NUL

// Everything from here till uci_startup_config() runs once before init_bitmap_mode() takes over $8000-$BFFF.
//To save MAIN space it is placed in the OVERLAY area (see teletext64-wic64.cfg)
#pragma code-name (push, "OVLCODE")
#pragma rodata-name (push, "OVLRODATA")
static const char ovl_msg8[] = "Saving settings to server...";
static const char ovl_msg9[] = "Could not read the network MAC-address.\r\nCheck that the Ultimate is connected\r\nto your network, and update its\r\nfirmware if the problem remains.\r\n";
static const char ovl_msg10[] = "Connecting to " PETSCIIPROXY "...";
static const char ovl_msg11[] = "Could not reach " PETSCIIPROXY ".\r\nCheck the network settings of your\r\nUltimate and try again.\r\n";
static const char ovl_msg12[] = "(or press cursor right to continue)";
static const char ovl_msg13[] = " to change config (";

#define SETTINGS_OVL
#include "settings_screen.c"

static bool uci_get_mac_key(char *key) {
	unsigned char cmd[3];
	int n;

	uii_settarget(TARGET_NETWORK);
	uii_getinterfacecount();
	if (!uii_success() || uii_data[0] == 0) return false;

	cmd[0] = 0x00;
	cmd[1] = NET_CMD_GET_NETADDR;
	cmd[2] = 0;
	uii_settarget(TARGET_NETWORK);
	uii_sendcommand(cmd, 3);
	n = uii_readdata();
	uii_readstatus();
	uii_accept();
	if (!uii_success() || n < 6) return false;

	bytes_to_hex((const unsigned char *)uii_data, 6, key);
	key[12] = '\0';
	return true;
}

static unsigned char wud_load(void) {
	char path[24];	// "/ud/" + 12 + "/l" + NUL = 19
	unsigned char reply[2+WUD_DATA_BYTES];
	unsigned int n;

	strcpy(path, "/ud/");
	strcat(path, wud_key);
	strcat(path, "/l");
	n = uci_http_get(path, reply, sizeof(reply));
	if (n == 1 && reply[0] == 0x01) return WUD_STATUS_NONE;
	if (n >= 2 + WUD_DATA_BYTES && reply[0] == 0x00 && reply[1] == WUD_DATA_BYTES) {
		config.refreshtime = (unsigned int)reply[2] | ((unsigned int)reply[3] << 8);
		config.cycletime = (unsigned int)reply[4] | ((unsigned int)reply[5] << 8);
		config.station = reply[6];
		if (config.station >= STATION_COUNT) config.station = 0;
		config.startpage = (unsigned int)reply[7] | ((unsigned int)reply[8] << 8);
		if (config.startpage < 100 || config.startpage > 899) config.startpage = 101;
		return WUD_STATUS_OK;
	}
	return WUD_STATUS_ERROR;
}

static bool wud_save(void) {
	char path[36];	// "/ud/" + 12 + "/s/" + 14 + NUL = 34
	unsigned char buf[WUD_DATA_BYTES];
	unsigned char reply;

	buf[0] = config.refreshtime & 0xFF;
	buf[1] = config.refreshtime >> 8;
	buf[2] = config.cycletime & 0xFF;
	buf[3] = config.cycletime >> 8;
	buf[4] = config.station;
	buf[5] = config.startpage & 0xFF;
	buf[6] = config.startpage >> 8;

	strcpy(path, "/ud/");
	strcat(path, wud_key);
	strcat(path, "/s/");
	bytes_to_hex(buf, WUD_DATA_BYTES, path+4+12+3);
	path[4+12+3+2*WUD_DATA_BYTES] = '\0';
	return uci_http_get(path, &reply, 1) == 1 && reply == 0x00;
}

static void clear_startup_rows(void) {
	unsigned char r;

	for (r=7; r<23; ++r) {
		gotoxy(0,r);
		cclear(40);
	}
	gotoxy(0,7);
}

static void save_settings_message(void) {
	clrscr();
	POKEW(0xD020,0);
	gotoxy(0,0);
	textcolor(SCREEN_COLOR_WHITE);
	cputs(ovl_msg8);
	if (!wud_save()) {
		textcolor(SCREEN_COLOR_RED);
		cputs("failed!\r\n");
	} else {
		cputs("done\r\n");
	}
	delay_jiffies(30);
}

static void uci_startup_config(void) {
	unsigned char wud_status;

	strcpy(host, PETSCIIPROXY_HOST);
	config.port = PETSCIIPROXY_PORT;

	textcolor(SCREEN_COLOR_WHITE);
	cputs("Network adapter: ");
	if (!uci_get_mac_key(wud_key)) {
		textcolor(SCREEN_COLOR_RED);
		cputs("failed\r\n\n");
		textcolor(SCREEN_COLOR_WHITE);
		cputs(ovl_msg9);
		exit(0);
	}
	textcolor(SCREEN_COLOR_YELLOW);
	cputs("OK\r\n");

	textcolor(SCREEN_COLOR_WHITE);
	cputs(ovl_msg10);
	wud_status = wud_load();
	if (wud_status == WUD_STATUS_ERROR) {
		textcolor(SCREEN_COLOR_RED);
		cputs("failed\r\n\n");
		textcolor(SCREEN_COLOR_WHITE);
		cputs(ovl_msg11);
		exit(0);
	}

	if (wud_status == WUD_STATUS_OK) {
		unsigned char secs, row;
		bool go_settings = false;
		bool just_go_on_with_it = false;

		textcolor(SCREEN_COLOR_YELLOW);
		cputs("ok\r\n\n");
		// make room if turbo advise was put on screen
		if (wherey() > 13) clear_startup_rows();
		config_show();
		row = wherey();
		textcolor(SCREEN_COLOR_WHITE);
		gotoxy(0,row+1);
		cputs(ovl_msg12);
		for (secs = 10; secs > 0 && !(go_settings || just_go_on_with_it); --secs) {
			unsigned long t;
			char secsbuf[4];

			gotoxy(0,row);
			textcolor(SCREEN_COLOR_WHITE);
			cputs("Press ");
			textcolor(SCREEN_COLOR_YELLOW);
			cputs("F2");
			textcolor(SCREEN_COLOR_WHITE);
			cputs(ovl_msg13);
			cputs(utoa(secs, secsbuf, 10));
			cputs(") ");
			t = get_jiffies();
			while ((get_jiffies() - t) < JIFFIES_PER_SECOND) {
				if (kbhit()) {
					unsigned char key = cgetc();
					if (key == KEY_F2) { go_settings = true; break; }
					if (key == KEY_CRSR_RIGHT) { just_go_on_with_it = true; break; }
				}
			}
		}
		if (go_settings) {
			settings_screen();
			save_settings_message();
		} else {
			gotoxy(0,row+2); // loading fonts... message
		}
	} else {
		textcolor(SCREEN_COLOR_YELLOW);
		cputs("not found\r\n"); // user preferences
		delay_jiffies(60);

		config.refreshtime = 60;
		config.cycletime = 15;
		config.station = STATION_NOSTT; // Why NOS-TT? Because I'm Dutch!
		config.startpage = 101;

		settings_screen();
		save_settings_message();
	}
	refresh_interval_jiffies = (unsigned long)config.refreshtime * JIFFIES_PER_SECOND;
}
#pragma rodata-name (pop)
#pragma code-name (pop)
#endif // UCI_STANDALONE

void main(void) {
	int currentSubpageNumber = 0, currentPageNumber;
	unsigned heap_start = get_heap_start();

	_heapadd((void *)heap_start, HEAP_TOP - heap_start);

#ifndef UCI_STANDALONE
	cbm_device = PEEK(186);
	if (cbm_device < 8 || cbm_device > 11) cbm_device = 8;
#endif
	
	welcome_screen();

	if (!UCI_enabled()) {
		gotoxy(0,7);
		textcolor(SCREEN_COLOR_YELLOW);
		cputs("No Command Interface detected!\r\n\n");
		textcolor(SCREEN_COLOR_WHITE);
		cputs("This program requires: a C64 Ultimate,\r\nUltimate 64 or a stock C64 with an\r\nUltimate II+ cartridge.\r\n\n");
		cputs("* Activate the menu (C= + RESTORE)\r\n* Select MEMORY & ROMS\r\n");
		cputs("* Set Command Interface to Enabled\r\n\n>> Reboot and run program again\r\n");
		exit(0);
	}

	show_F8_for_help();

	if (detect_u64_turbo_mode()) {
		U64_TURBO_CTRL = U64_SPEED_40MHZ; // for near instant displaying Teletext pages
		cputs("C64U/U64 Turbo Registers enabled\r\n");
		cputs("Setting CPU speed to 40Mhz\r\n\n");
	} else {
		textcolor(SCREEN_COLOR_YELLOW);
		cputs("To enjoy full speed:\r\n\n");
		textcolor(SCREEN_COLOR_WHITE);
		cputs("* Activate menu\r\n* Select TURBO BOOST\r\n");
		cputs("* Set Turbo Control to:\r\n  C64U Turbo Registers\r\n\n");
	}

#ifdef UCI_STANDALONE
	uci_startup_config();
#else
	if (!config_load()) {
		cputs("No config found; run config util\r\n");
		exit(0);
	}
#endif
	station_index = config.station;
	currentPageNumber = config.startpage;

#ifndef UCI_STANDALONE
	if (!proxy_running()) exit(0);
#endif

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

	show_wait_bar(3);
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
