/*****************************************************************
Teletext 64 Ultimate
 
Copyright (C) 2026 Frank Putman
SPDX-License-Identifier: GPL-3.0-or-later
******************************************************************/
#include <string.h>
#include "txtlib.h"

Config config;
unsigned long refresh_interval_jiffies;

#if !defined(WIC64VERSION) || defined(DISK_CONFIG)
unsigned char cbm_device = 8; // the actual device number will be determinde at program start
#endif

// Edit/add new station: always edit this file, txtlib.h and station_menu.h
// 1st item:	id used in the GET <url>
// 2nd item:	menu name
// 3rd item:	language, see https://www.etsi.org/deliver/etsi_i_ets/300700_300799/300706/01_60/ets_300706e01p.pdf page 115
//						the national option sub-sets are stored in these files: teletext-languages-bold.bin/teletext-languages-thin.bin
//						note: in real Teletext each page can have a language code; this is not implemented in this program to save memory
const Station stations[] = {
	// Group 1: German
	{"ARD-TEXT",					"ARD Text     ", TXT_LANG_GERMAN},
	{"ZDF-TEXT",					"ZDF Text     ", TXT_LANG_GERMAN},
	{"ZDFINFO",						"ZDF Info     ", TXT_LANG_GERMAN},
	{"ZDFNEO",						"ZDF Neo      ", TXT_LANG_GERMAN},
	{"WDR-TEXT",					"WDR Text     ", TXT_LANG_GERMAN},
	{"NDR",								"NDR Text     ", TXT_LANG_GERMAN},
	{"HR-TEXT",						"HR-text      ", TXT_LANG_GERMAN},
	{"SWR-BW",						"SWR Baden-Wb ", TXT_LANG_GERMAN},
	{"SWR-RP",						"SWR Rhein-Pf ", TXT_LANG_GERMAN},
	{"3SAT",							"3SAT         ", TXT_LANG_GERMAN},
	{"ORF1",							"ORF 1        ", TXT_LANG_GERMAN},
	{"ORF2",							"ORF 2        ", TXT_LANG_GERMAN},
	{"ORF3",							"ORF III      ", TXT_LANG_GERMAN},
	{"ORFSPORT",					"ORF Sport+   ", TXT_LANG_GERMAN},
	{"SRF1",							"SRF 1        ", TXT_LANG_GERMAN},
	{"SRF2",							"SRF zwei     ", TXT_LANG_GERMAN},
	{"SRFINFO",						"SRF Info     ", TXT_LANG_GERMAN},
	{"FORUM64",						"Forum64      ", TXT_LANG_GERMAN},
	// Group 2: English
	{"CEEFAX",						"Ceefax       ", TXT_LANG_ENGLISH},
	{"TEEFAX",   	 				"Teefax       ", TXT_LANG_ENGLISH},
	{"CHUNKYTEXT",	 			"Chunkytext   ", TXT_LANG_ENGLISH},
	{"SPARK",    	  			"SPARK        ", TXT_LANG_ENGLISH},
	{"WEBFAX1",     			"Webfax 1     ", TXT_LANG_ENGLISH},
	{"WEBFAX2",   	  		"Webfax 2     ", TXT_LANG_ENGLISH},
	// Group 3: Nordic
	{"DR-TEKST-TV",				"DR Tekst-TV  ", TXT_LANG_SWEDISH_FINNISH},	// Danish (Swedish/Finnish fallback)
	{"SVT-TEXT",  	 			"SVT Text     ", TXT_LANG_SWEDISH_FINNISH},
	{"TEKSTI-TV",   			"YLE Teksti-TV", TXT_LANG_SWEDISH_FINNISH},
	{"HBN-TEKSTI-TV",			"HBN Teksti-TV", TXT_LANG_SWEDISH_FINNISH},
	// Group 4: Other
	{"BMN1", 							"Bollentekst  ", TXT_LANG_GERMAN},
	{"MTVA-TXT",   		    "MTVA Teletext", TXT_LANG_SWEDISH_FINNISH},	// Hungarian (Swedish/Finnish fallback)
	{"NOS-TT",    	  		"NOS Teletekst", TXT_LANG_GERMAN},					// Dutch (German fallback)
	{"NOSNEWS",  	   			"NOS Nieuws   ", TXT_LANG_GERMAN},
	{"TELETEKSTARCHIEF",	"NOS Archief  ", TXT_LANG_GERMAN},
	{"RTS1", 	   					"RTS 1        ", TXT_LANG_FRENCH},
	{"RTS2", 	    				"RTS 2        ", TXT_LANG_FRENCH},
	{"RSILA1", 	   				"RSI LA 1     ", TXT_LANG_ITALIAN},
	{"RSILA2", 	   		  	"RSI LA 2     ", TXT_LANG_ITALIAN},
	{"TLGZ-TG1", 					"Telegaz TVP 1", TXT_LANG_POLISH},
	{"TLGZ-TG2", 					"Telegaz TVP 2", TXT_LANG_POLISH},
	{"TLGZ-OTV", 					"Telegaz TVP 3", TXT_LANG_POLISH},
	{"TLGZ-KUL", 					"Telegaz Kult.", TXT_LANG_POLISH},
	{"TLGZ-HIS", 					"Telegaz Hist.", TXT_LANG_POLISH},
	{"TLGZ-SPO", 					"Telegaz Sport", TXT_LANG_POLISH},
	{"TLGZ-SAT", 					"Telegaz Polon", TXT_LANG_POLISH},
	{"TELETEXT64",   			"Teletext 64  ", TXT_LANG_ENGLISH},					// Hidden in the menu. This is (F8) User's Guide as Teletext service
};

// Compile time check: gets negative if the number of stations[] items not equal to the STATION_ list in txtlib.h
typedef char stations_table_matches_station_list[(sizeof(stations) / sizeof(stations[0]) == STATION_ENUM_END) ? 1 : -1];

// Station menu: data and logic are shared by the bitmap menu in teletext_common.c and the character mode picker in station_picker.c
// The menu arrays are made from the MENU_LEFT / MENU_RIGHT (txtlib.h) table:
// - menu_first[i]		station or the number of header label
// - menu_count[i]		0=header, 1=station, #=folder with that # of stations
// - menu_initial[i]	key that jumps to it
#define FIRST_OF(station, count, key)		(station),
#define COUNT_OF(station, count, key)		(count),
#define KEY_OF(station, count, key)			(key),
#define AS_FIRST(item)	FIRST_OF(item)
#define AS_COUNT(item)	COUNT_OF(item)
#define AS_KEY(item)		KEY_OF(item)
const unsigned char menu_first[MENU_ENTRY_COUNT] = { MENU_LEFT(AS_FIRST) MENU_RIGHT(AS_FIRST) };
const unsigned char menu_count[MENU_ENTRY_COUNT] = { MENU_LEFT(AS_COUNT) MENU_RIGHT(AS_COUNT) };
static const unsigned char menu_initial[MENU_ENTRY_COUNT] = { MENU_LEFT(AS_KEY) MENU_RIGHT(AS_KEY) };

// labels dont need padding, station names do
static const char *const menu_headers[4] = {
	"==German==",
	"==English==",
	"==Nordic==",
	"==Other==" };

unsigned char open_folder;		// item whose second window is open; NO_FOLDER while only the main window is on
unsigned char submenu_row;

const char *menu_item_name(unsigned char i, char *buf) {
	const char *name;
	unsigned char station = menu_first[i], count = menu_count[i], pos;

	if (open_folder != NO_FOLDER) {
		station = menu_first[open_folder] + i;
		count = 1;
	}
	if (count == 0) return menu_headers[station - 1];
	name = stations[station].menu_name;
	if (count > 1) {	// item is a folder: take the first part of the station name and add a '>' submenu indicator
		strcpy(buf, name);
		for (pos=0; buf[pos] != ' '; ++pos) ;
		memset(buf + pos, ' ', (MENU_ITEM_WIDTH - 1) - pos);
		buf[MENU_ITEM_WIDTH - 1] = '>';
		name = buf;
	}
	return name;
}

unsigned char menu_navigate(unsigned char selected, unsigned char key) {
	unsigned char last_item = menu_item_count() - 1, previous = selected, step, candidate;

	switch (key) {
		case KEY_CRSR_LEFT:
		case KEY_CRSR_RIGHT:
			// Same row in the other column; header is skipped by taking the entry below
			if (open_folder == NO_FOLDER) {
				selected = (selected < MENU_COL1_COUNT) ? selected + MENU_COL1_COUNT : selected - MENU_COL1_COUNT;
				// other column can be shorter: if it has no such row, take last row
				if (selected > last_item) selected = last_item;
				if (selected >= MENU_COL1_COUNT && previous >= MENU_COL1_COUNT) selected = MENU_COL1_COUNT - 1;
				if (menu_count[selected] == 0) ++selected;
			}
			break;
		case KEY_CRSR_UP:
		case KEY_CRSR_DOWN:
			do {
				if (key == KEY_CRSR_DOWN) selected = (selected == last_item) ? 0 : selected + 1;
				else selected = (selected == 0) ? last_item : selected - 1;
			} while (open_folder == NO_FOLDER && menu_count[selected] == 0);
			break;
		default:
			// Letter or digit: jump to the next main window entry that starts with it
			if (open_folder == NO_FOLDER) {
				for (step = 1; step <= last_item; ++step) {
					candidate = previous + step;
					if (candidate > last_item) candidate -= last_item + 1;
					if (menu_initial[candidate] == key) {
						selected = candidate;
						break;
					}
				}
			}
			break;
	}
	return selected;
}

unsigned char menu_choose(unsigned char current_station, unsigned char (*run)(unsigned char selected)) {
	unsigned char selected = 1, i, offset, folder_item, first, count, limit;

	// Find current station
	for (i=0; i<MENU_ENTRY_COUNT; ++i) {
		offset = current_station - menu_first[i];
		if (offset < menu_count[i]) {
			selected = i;
			break;
		}
	}
	
	while (1) {
		open_folder = NO_FOLDER;
		selected = run(selected);
		if (selected == MENU_CANCEL) return MENU_CANCEL;
		first = menu_first[selected];
		count = menu_count[selected];
		if (count == 1) return first;
		// 2nd window y-position is matched with the underlying station group name
		open_folder = selected;
		submenu_row = MENU_Y + ((selected < MENU_COL1_COUNT) ? selected : selected - MENU_COL1_COUNT);
		limit = MENU_Y + MENU_HEIGHT - count - 2;	// with -2 we keep the 2nd window including it's shadow inside the main window
		if (submenu_row > limit) submenu_row = limit;
		folder_item = current_station - first;
		if (folder_item >= count) folder_item = 0;
		folder_item = run(folder_item);
		if (folder_item != MENU_CANCEL) return first + folder_item;
	}
}
