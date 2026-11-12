/*****************************************************************
Teletext 64 Ultimate
 
Copyright (C) 2026 Frank Putman
SPDX-License-Identifier: GPL-3.0-or-later
******************************************************************/
#include <conio.h>
#include "txtlib.h"
#include "station_picker.h"

#ifdef WIC64_OVERLAY
#pragma code-name (push, "OVLCODE")
#pragma rodata-name (push, "OVLRODATA")
#endif

static void picker_window(unsigned char x, unsigned char y, unsigned char w, unsigned char h) {
	unsigned char i;

	textcolor(SCREEN_COLOR_GRAY_3);
	revers(false);
	// top row of window: left corner + line + right corner
	gotoxy(x,y);
	cputc(176);
	chline(w-1);
	cputc(174);
	// left and right bars, clear middle part
	for (i=0; i<h-1; ++i) {
		gotoxy(x,y+1+i);
		cputc(125);
		cclear(w-1);
		cputc(125);
	}
	// bottom row of window: left corner + line + right corner
	gotoxy(x,y+h);
	cputc(173);
	chline(w-1);
	cputc(189);
}

void station_picker_clear(bool draw_window) {
	unsigned char i;

	if (draw_window) {
		picker_window(MENU_X, MENU_Y, MENU_WIDTH, MENU_HEIGHT);
	} else {
		textcolor(SCREEN_COLOR_GRAY_3);
		for (i=0; i<MENU_HEIGHT+2; ++i) {
			gotoxy(MENU_X-1,MENU_Y-1+i);
			cclear(MENU_WIDTH+2);
		}
	}
}

static void picker_item_draw(unsigned char i, unsigned char selected) {
	char buf[MENU_ITEM_SIZE];
	unsigned char color = SCREEN_COLOR_GRAY_3;

	if (open_folder == NO_FOLDER && menu_count[i] == 0) {
		color = SCREEN_COLOR_LIGHT_RED;		// language group header
	} else if (i == selected) {
		color = SCREEN_COLOR_GREEN;
	}
	textcolor(color);
	revers(i == selected);
	gotoxy(MENU_ITEM_COL(i), MENU_ITEM_ROW(i));
	cputs(menu_item_name(i, buf));
}

static unsigned char picker_run(unsigned char selected) {
	unsigned char item_count = menu_item_count(), i, previous, key;

	if (open_folder == NO_FOLDER) {
		picker_window(MENU_X, MENU_Y, MENU_WIDTH, MENU_HEIGHT);
	} else {
		picker_window(SUBMENU_X, submenu_row, SUBMENU_WIDTH, item_count + 1);
	}
	for (i=0; i<item_count; ++i) picker_item_draw(i, selected);

	while (1) {
		key = cgetc();
		if (key == KEY_STOP) return MENU_CANCEL;
		if (key == KEY_RETURN) return selected;
		previous = selected;
		selected = menu_navigate(selected, key);
		if (selected != previous) {
			picker_item_draw(previous, selected);
			picker_item_draw(selected, selected);
		}
	}
}

void station_picker(void) {
	unsigned char station = menu_choose(config.station, picker_run);

	revers(false);
	if (station != MENU_CANCEL) config.station = station;
}

#ifdef WIC64_OVERLAY
#pragma rodata-name (pop)
#pragma code-name (pop)
#endif
