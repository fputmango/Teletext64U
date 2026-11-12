/*****************************************************************
Teletext 64 Ultimate
 
Copyright (C) 2026 Frank Putman
SPDX-License-Identifier: GPL-3.0-or-later

station menu layout, 2 columns
 - MENU_HEADER:		language group label
 - MENU_FOLDER:		shows the first part (till the 1st space) of the station name
									the number tells how many stations are in the submenu
 - MENU_STATION:		station name, directly selectable
 
Edit/add new station: always edit this file, txtlib.c and txtlib.h
******************************************************************/

#ifndef STATION_MENU_H
#define STATION_MENU_H

#define MENU_LEFT(ITEM) \
	ITEM(MENU_HEADER(1)) \
	ITEM(MENU_STATION(STATION_ARDTEXT, KEY_A)) \
	ITEM(MENU_FOLDER(STATION_ZDFTEXT, 3, KEY_Z)) \
	ITEM(MENU_STATION(STATION_WDRTEXT, KEY_W)) \
	ITEM(MENU_STATION(STATION_NDR, KEY_N)) \
	ITEM(MENU_STATION(STATION_HR, KEY_H)) \
	ITEM(MENU_FOLDER(STATION_SWR_BW, 2, KEY_S)) \
	ITEM(MENU_STATION(STATION_3SAT, KEY_0+3)) \
	ITEM(MENU_FOLDER(STATION_ORF1, 4, KEY_O)) \
	ITEM(MENU_FOLDER(STATION_SRF1, 3, KEY_S)) \
	ITEM(MENU_STATION(STATION_FORUM64, KEY_F)) \
	ITEM(MENU_HEADER(2)) \
	ITEM(MENU_STATION(STATION_CEEFAX, KEY_C)) \
	ITEM(MENU_STATION(STATION_TEEFAX, KEY_T)) \
	ITEM(MENU_STATION(STATION_CHUNKYTEXT, KEY_C)) \
	ITEM(MENU_STATION(STATION_SPARK, KEY_S)) \
	ITEM(MENU_FOLDER(STATION_WEBFAX1, 2, KEY_W))

#define MENU_RIGHT(ITEM) \
	ITEM(MENU_HEADER(3)) \
	ITEM(MENU_STATION(STATION_DRTEKSTTV, KEY_D)) \
	ITEM(MENU_STATION(STATION_SVTTEXT, KEY_S)) \
	ITEM(MENU_STATION(STATION_YLETEKSTITV, KEY_Y)) \
	ITEM(MENU_STATION(STATION_HBNTEKSTITV, KEY_H)) \
	ITEM(MENU_HEADER(4)) \
	ITEM(MENU_STATION(STATION_BMN1, KEY_B)) \
	ITEM(MENU_STATION(STATION_MTVATEXT, KEY_M)) \
	ITEM(MENU_FOLDER(STATION_NOSTT, 3, KEY_N)) \
	ITEM(MENU_FOLDER(STATION_RTS1, 2, KEY_R)) \
	ITEM(MENU_FOLDER(STATION_RSILA1, 2, KEY_R)) \
	ITEM(MENU_FOLDER(STATION_TLGZ_TVP1, 7, KEY_T))

#define MENU_HEADER(number)								(number), 0, 0
#define MENU_STATION(station, key)				(station), 1, (key)
#define MENU_FOLDER(station, count, key)	(station), (count), (key)

// counts follow from the table, every item adds 1. coount incl. headers
#define MENU_ONE_ITEM(item)	+ 1
#define MENU_COL1_COUNT		(0 MENU_LEFT(MENU_ONE_ITEM))
#define MENU_COL2_COUNT		(0 MENU_RIGHT(MENU_ONE_ITEM))
#define MENU_ENTRY_COUNT	(MENU_COL1_COUNT + MENU_COL2_COUNT)

// determine longest column and menu height
enum {
	MENU_ROWS = (MENU_COL1_COUNT > MENU_COL2_COUNT ? MENU_COL1_COUNT : MENU_COL2_COUNT),
	MENU_HEIGHT = MENU_ROWS + 1		// window height: number of rows between borders + 1
};

#endif
