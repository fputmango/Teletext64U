// Copyright (C) 2026 Frank Putman
//
// SPDX-License-Identifier: GPL-3.0-or-later
// Settings screen called via F2 in the standalone editions

#define T_CYCLE   "Minimum cycle time:"
#define T_HELP0   "The active teletext page will be\r\nrefreshed after the given number of\r\nseconds."
#define T_HELP2   "Teletext 64 will start with your\r\nfavorite station."
#define T_HELP3   "Teletext 64 will start with your\r\nfavorite page."
#define T_KEYS    "UP/DN=NAV  RETURN=EDIT  F1=SAVE&CONTINUE"
#define T_BADPAGE "Start page must be between 100..899     "
#define T_TITLE   "*** TELETEXT 64 CONFIG ***"

#ifdef SETTINGS_OVL
static const char s_cycle[] = T_CYCLE;
static const char s_help0[] = T_HELP0;
static const char s_help2[] = T_HELP2;
static const char s_help3[] = T_HELP3;
static const char s_keys[] = T_KEYS;
static const char s_badpage[] = T_BADPAGE;
static const char s_title[] = T_TITLE;
#define S_CYCLE   s_cycle
#define S_HELP0   s_help0
#define S_HELP2   s_help2
#define S_HELP3   s_help3
#define S_KEYS    s_keys
#define S_BADPAGE s_badpage
#define S_TITLE   s_title
#else
#define S_CYCLE   T_CYCLE
#define S_HELP0   T_HELP0
#define S_HELP2   T_HELP2
#define S_HELP3   T_HELP3
#define S_KEYS    T_KEYS
#define S_BADPAGE T_BADPAGE
#define S_TITLE   T_TITLE
#endif

static void bytes_to_hex(const unsigned char *src, unsigned char count, char *dst) {
	static const char hex[] = "0123456789abcdef";
	unsigned char i;

	for (i=0; i<count; i++) {
		*dst++ = hex[src[i] >> 4];
		*dst++ = hex[src[i] & 0x0F];
	}
}

#define CFG_NUM_MENU_ITEMS	4
#define CFG_MSG_ROW					24
#define CFG_X_POS_VALUES		21
#define CFG_MENU_YPOS				2

static const char *cfg_menu_items[CFG_NUM_MENU_ITEMS] = {
	"Auto refresh time:",
	S_CYCLE,
	"Favorite station:",
	"Favorite page:",
};

static const char *cfg_menu_help[CFG_NUM_MENU_ITEMS] = {
	S_HELP0,
	"Number of seconds to wait before auto-\r\nrotating to the next subpage.\r\n\nLeave 0 to accept the default cycle\r\ntime (10 seconds) or a given cycle\r\ntime by the teletext service.",
	S_HELP2,
	S_HELP3,
};

static unsigned char cfg_int_len(unsigned int val) {
	if (val < 10) return 1;
	if (val < 100) return 2;
	if (val < 1000) return 3;
	if (val < 10000) return 4;
	return 5;
}

// Left-justified; note: this functoin prevents the need of pulling cprintf()-lib in / saves memory
static void cfg_print_padded_str(const char *s, unsigned char width) {
	unsigned char len = strlen(s);

	cputs(s);
	while (len++ < width) cputc(' ');
}

static void cfg_display_menu_help(void) {
	gotoxy(0, CFG_MSG_ROW);
	textcolor(SCREEN_COLOR_WHITE);
	cputs(S_KEYS);
}

static void cfg_display_menu_items(char index) {
	char i;

	for (i=0; i<CFG_NUM_MENU_ITEMS; ++i) {
		gotoxy(0,CFG_MENU_YPOS+i);
		textcolor((i == index) ? SCREEN_COLOR_WHITE : SCREEN_COLOR_GRAY_2);
		cputs(cfg_menu_items[i]);
		if (i == index) {
			gotoxy(0,9);
			cclear(240);
			gotoxy(0,9);
			textcolor(SCREEN_COLOR_GRAY_2);
			cputs(cfg_menu_help[i]);
		}
	}
}

static void cfg_display_config_items(void) {
	char numbuf[6];
	textcolor(SCREEN_COLOR_YELLOW);
	gotoxy(CFG_X_POS_VALUES, CFG_MENU_YPOS); cfg_print_padded_str(utoa(config.refreshtime, numbuf, 10), 6);
	gotoxy(CFG_X_POS_VALUES, CFG_MENU_YPOS+1); cfg_print_padded_str(utoa(config.cycletime, numbuf, 10), 6);
	gotoxy(CFG_X_POS_VALUES, CFG_MENU_YPOS+2); cfg_print_padded_str(stations[config.station].url_name, 13);
	gotoxy(CFG_X_POS_VALUES, CFG_MENU_YPOS+3); cfg_print_padded_str(utoa(config.startpage, numbuf, 10), 4);
}

static void cfg_edit_time(unsigned char x, unsigned char y, unsigned int *field) {
	char buf[6];
	unsigned char pos;
	char ch;

	textcolor(SCREEN_COLOR_GREEN);
	utoa(*field, buf, 10);
	pos = cfg_int_len(*field);
	while (1) {
		gotoxy(x,y);
		cfg_print_padded_str(buf, 6);
		gotoxy(x + pos, y);
		cursor(1);
		ch = cgetc();
		cursor(0);
		if (ch == KEY_RETURN)
			break;
		if (ch == 20 && pos > 0) {
			--pos;
			buf[pos] = '\0';
		} else if (pos < 5 && ch >= '0' && ch <= '9') {
			buf[pos++] = ch;
			buf[pos] = '\0';
		}
	}
	*field = atoi(buf);
}

static void cfg_edit_startpage(unsigned char x, unsigned char y) {
	char buf[4];
	unsigned char pos;
	char ch;

	textcolor(SCREEN_COLOR_GREEN);
	utoa(config.startpage, buf, 10);
	pos = cfg_int_len(config.startpage);
	while (1) {
		gotoxy(x, y);
		cfg_print_padded_str(buf, 4);
		gotoxy(x + pos, y);
		cursor(1);
		ch = cgetc();
		cursor(0);
		if (ch == KEY_RETURN) break;
		if (ch == 20 && pos > 0) {
			--pos;
			buf[pos] = '\0';
		} else if (pos < 3 && ch >= '0' && ch <= '9') {
			buf[pos++] = ch;
			buf[pos] = '\0';
		}
	}
	config.startpage = atoi(buf);
	if (config.startpage < 100 || config.startpage > 899) {
		config.startpage = 101;
		gotoxy(0, CFG_MSG_ROW);
		textcolor(SCREEN_COLOR_GRAY_2);
		cclear(40);
		gotoxy(0, CFG_MSG_ROW);
		cputs(S_BADPAGE);
		delay_jiffies(240);
		cfg_display_menu_help();
	}
}

// runs in text mode before bitmap init
static void settings_screen(void) {
	char menu_index = 0;
	char c;
	unsigned char k;

	clrscr();
	POKEW(0xD020, 0x00);
	gotoxy(7,0);
	textcolor(SCREEN_COLOR_CYAN);
	cputs(S_TITLE);
	textcolor(SCREEN_COLOR_GRAY_2);
	gotoxy(0,23);
	for (k=0; k<40; k++) cputc(64+128);

	cfg_display_menu_items(menu_index);
	cfg_display_menu_help();
	cfg_display_config_items();

	while (1) {
		if (kbhit()) {
			c = cgetc();
			switch (c) {
				case KEY_RETURN:
					if (menu_index == 0)
						cfg_edit_time(CFG_X_POS_VALUES, CFG_MENU_YPOS, &config.refreshtime);
					else if (menu_index == 1)
						cfg_edit_time(CFG_X_POS_VALUES, CFG_MENU_YPOS+1, &config.cycletime);
					else if (menu_index == 2) {
						station_picker();
						station_picker_clear(false);
						cfg_display_menu_items(menu_index);
						cfg_display_config_items();
					} else if (menu_index == 3)
						cfg_edit_startpage(CFG_X_POS_VALUES, CFG_MENU_YPOS+3);
					cfg_display_config_items();
					break;
				case KEY_CRSR_UP:
					if (menu_index > 0) --menu_index;
					cfg_display_menu_items(menu_index);
					break;
				case KEY_CRSR_DOWN:
					if (menu_index < (CFG_NUM_MENU_ITEMS - 1)) ++menu_index;
					cfg_display_menu_items(menu_index);
					break;
				case KEY_F1:
					return;
			}
		}
	}
}
