/*****************************************************************
Teletext 64 Ultimate config program
This is a seperate program shipped on the D64 image to configure user preferences
 
Copyright (C) 2026 Frank Putman
SPDX-License-Identifier: GPL-3.0-or-later
******************************************************************/
#include <stdlib.h>
#include <conio.h>
#include <string.h>
#include <c64.h>
#include <peekpoke.h>
#include <cbm.h>
#include <ctype.h>
#include "txtlib.h"
#include "station_picker.h"

#define CONFIG_FILENAME "ut64.cfg"
#define MAGIC						"ut64"
#define LFN 						2

#define MSG_ROW 						24
#define X_POS_CONFIG_ITEMS	21
#define MENU_YPOS						2
#define NUM_MENU_ITEMS			7

const char* menu_items[] = {
	"Use PetsciiProxy.nl:",
	"Server IP-address:",
	"Server port:",
	"Auto refresh time:",
	"Minimum cycle time:",
	"Favorite station:",
	"Favorite page:",
};

const char* menu_help[] = {
	"Enabled is the recommended setting. Usesthe superfast public PetsciiProxy.nl\r\nserver.\r\n\nSet to Disabled if you want to run a\r\nlocal PetsciiProxy server.",
	"IP-address of the machine where the\r\nPetsciiProxy server is running.",
	"Listening port of the PetsciiProxy\r\nserver. 8080 is the default value.",
	"The active teletext page will be\r\nrefreshed after the given number of\r\nseconds.",
	"Number of seconds to wait before auto-\r\nrotating to the next subpage.\r\n\nLeave 0 to accept the default cycle\r\ntime (10 seconds) or a given cycle\r\ntime by the teletext service.",
	"Teletext 64 will start with your\r\nfavorite station.",
	"Teletext 64 will start with your\r\nfavorite page.",
};

const char* use_dedicatedserver[] = {
	"Disabled",
	"Enabled ",
};

unsigned long get_jiffies(void) {
	unsigned long j;
	// Jiffy clock at 0xA0, 0xA1, 0xA2 (160, 161, 162)
	// Disable interrupts briefly to get an atomic read
	asm("sei");
	j = (unsigned long)PEEK(162) |
			((unsigned long)PEEK(161) << 8) |
			((unsigned long)PEEK(160) << 16);
	asm("cli");
	return j;
}

void wait(char tenth_seconds) {
	unsigned long last_jiffies = get_jiffies();
	while ((get_jiffies() - last_jiffies) < (tenth_seconds * 6));
}

unsigned char get_segment_len(unsigned char val) {
	if (val >= 100) return 3;
	if (val >= 10) return 2;
	return 1;
}

unsigned char get_int_len(unsigned int val) {
	if (val < 10) return 1;
	if (val < 100) return 2;
	if (val < 1000) return 3;
	if (val < 10000) return 4;
	return 5;
}

void display_menu_help(void) {
	gotoxy(0,24);
	textcolor(SCREEN_COLOR_WHITE);
	cprintf("UP/DN=NAV  RETURN=EDIT  F1=SAVE  F3=EXIT");
}

unsigned char get_ip_len(void) {
	unsigned char len = 3; // 3 dots
	unsigned char i;
	
	for (i=0; i<4; ++i) {
		len += get_segment_len(config.ip[i]);
	}
	return len;
}

void print_ip_aligned(unsigned char y) {
	unsigned char len = 3; // The three dots: "." "." "."
	unsigned char i;
	
	for (i=0; i<4; ++i) {
		len += get_segment_len(config.ip[i]);
	}
	gotoxy(X_POS_CONFIG_ITEMS,y);
	cprintf("%u.%u.%u.%u", config.ip[0], config.ip[1], config.ip[2], config.ip[3]);
}

unsigned char save_config(void) {
	textcolor(SCREEN_COLOR_GRAY_2);
	gotoxy(0, MSG_ROW);
	cclear(40);
	gotoxy(0,MSG_ROW);
	cprintf("Saving... ");
	// using @0: prefix to overwrite existing file
	if (cbm_open(LFN, cbm_device, 1, "@0:" CONFIG_FILENAME) != 0) {
		cprintf("open failed");
		wait(10);
		return 1;
	}
	if (cbm_write(LFN, &config, sizeof(Config)) != sizeof(Config)) {
		cbm_close(LFN);
		cprintf("write failed");
		wait(10);
		return 2;
	}
	cbm_close(LFN);
	cprintf("done");
	wait(10);
	return 0;
}

unsigned char load_config(void) {
	textcolor(SCREEN_COLOR_GRAY_2);
	gotoxy(0, MSG_ROW);
	cprintf("Loading... ");
	if (cbm_open(LFN, cbm_device, 0, CONFIG_FILENAME) != 0) {
		cprintf("File not found or disk error");
		wait(10);
		return 1;
	}
	if (cbm_read(LFN, &config, sizeof(Config)) != sizeof(Config)) {
		cbm_close(LFN);
		cprintf("Read error or short file");
		wait(10);
		return 2;
	}
	cbm_close(LFN);
	if (config.magic[0] != MAGIC[0] || config.magic[1] != MAGIC[1] || config.magic[2] != MAGIC[2] || config.magic[3] != MAGIC[3]) {
		wait(10);
		cprintf("Invalid config data");
		return 3;
	}
	cprintf("done");
	wait(5);
	return 0;
}

void config_set_defaults(void) {
	config.magic[0] = MAGIC[0];
	config.magic[1] = MAGIC[1];
	config.magic[2] = MAGIC[2];
	config.magic[3] = MAGIC[3];

	config.dedicatedserver = true;
	
	config.ip[0] = 192;
	config.ip[1] = 168;
	config.ip[2] = 178;
	config.ip[3] = 128;

	config.port = 8080; 			// default petsciiproxy server, unless user has changed that value there
	config.refreshtime = 60;	// refresh page every 60 seconds
	config.cycletime = 0;			// accept default cycle times
	
	config.station = STATION_NOSTT;	// because I'm Dutch
	config.startpage = 101;
}

void parse_ip(char* s) {
	unsigned char i;
	
	for (i = 0; i < 4; ++i) {
		config.ip[i] = atoi(s);
		// Find the next dot and skip it
		s = strchr(s, '.');
		if (s == NULL) break;
		s++; // Move past the dot
	}
}

void build_ip_string(char* dest) {
	char tmp[4];
	unsigned char i;

	dest[0] = '\0';
	for (i=0; i<4; ++i) {
		utoa(config.ip[i], tmp, 10);
		strcat(dest, tmp);
		if (i < 3) // Only add the inbetween dots
			strcat(dest, ".");
	}
}

void edit_ip_string(unsigned char x, unsigned char y) {
	char buf[16]; // Max: "255.255.255.255" + \0
	unsigned char pos;
	char ch;

	textcolor(SCREEN_COLOR_GREEN);
	build_ip_string(buf);
	pos = get_ip_len();
	while (1) {
		gotoxy(x, y);
		cprintf("%-15s", buf);
		gotoxy(x + pos, y);
		cursor(1);
		ch = cgetc();
		cursor(0);

		if (ch == KEY_RETURN) break;

		if (ch == KEY_INSTDEL && pos > 0) {
			--pos;
			buf[pos] = '\0';
		} else if (pos < 15) {
			if (isdigit(ch) || ch == '.') {
				buf[pos++] = ch;
				buf[pos] = '\0';
			}
		}
	}
	parse_ip(buf);
}

void edit_port(unsigned char x, unsigned char y) {
	char buf[6]; // "12345" + \0
	unsigned char pos;
	char ch;

	textcolor(SCREEN_COLOR_GREEN);
	utoa(config.port, buf, 10);
	pos = get_int_len(config.port);
	while (1) {
		gotoxy(x, y);
		cprintf("%-6s", buf);
		gotoxy(x + pos, y);
		cursor(1);
		ch = cgetc();
		cursor(0);
		
		if (ch == KEY_RETURN) break;

		if (ch == KEY_INSTDEL && pos > 0) {
			--pos;
			buf[pos] = '\0';
		} else if (pos < 5) {
			if (isdigit(ch)) {
				buf[pos++] = ch;
				buf[pos] = '\0';
			}
		}
	}
	config.port = atoi(buf);
}

void edit_startpage(unsigned char x, unsigned char y) {
	char buf[4]; // "123" + \0
	unsigned char pos;
	char ch;
	
	textcolor(SCREEN_COLOR_GREEN);
	utoa(config.startpage, buf, 10);
	pos = get_int_len(config.startpage);
	while (1) {
		gotoxy(x, y);
		cprintf("%-4s", buf);
		gotoxy(x + pos, y);
		cursor(1);
		ch = cgetc();
		cursor(0);
		
		if (ch == KEY_RETURN) break;
		
		if (ch == KEY_INSTDEL && pos > 0) {
			--pos;
			buf[pos] = '\0';
		} else if (pos < 3) {
			if (isdigit(ch)) {
				buf[pos++] = ch;
				buf[pos] = '\0';
			}
		}
	}
	config.startpage = atoi(buf);
	if (config.startpage < 100 || config.startpage > 899) {
		config.startpage = 100;
		textcolor(SCREEN_COLOR_GRAY_2);
		gotoxy(0, MSG_ROW);
		cclear(40);
		gotoxy(0, MSG_ROW);
		cprintf("Start page must be between 100..899");
		wait(10);
		display_menu_help();
	}
}

void edit_time(unsigned char x, unsigned char y, unsigned int *field) {
	char buf[6]; // "12345" + \0
	unsigned char pos;
	char ch;

	textcolor(SCREEN_COLOR_GREEN);
	utoa(*field, buf, 10);
	pos = get_int_len(*field);
	while (1) {
		gotoxy(x, y);
		cprintf("%-6s", buf);
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

void display_menu_items(char index) {
	char i;
	
	for (i=0; i<NUM_MENU_ITEMS; ++i) {
		gotoxy(0,MENU_YPOS+i);
		if (i == index)
			textcolor(SCREEN_COLOR_WHITE); // Highlight active menu
		else {
			if (config.dedicatedserver && (i==1 || i==2)) {
				textcolor(SCREEN_COLOR_BLUE);	// disabled menu items
			} else {
				textcolor(SCREEN_COLOR_GRAY_2);
			}
		}
		cputs(menu_items[i]);
		// show help text for active menu
		if (i == index) {
			gotoxy(0,12);
			cclear(240);
			gotoxy(0,12);
			textcolor(SCREEN_COLOR_GRAY_2);
			cputs(menu_help[i]);
		}
	}
}

void display_config_items(void) {
	textcolor(SCREEN_COLOR_YELLOW);
	gotoxy(X_POS_CONFIG_ITEMS, MENU_YPOS); cprintf("%s", use_dedicatedserver[config.dedicatedserver]);
	if (config.dedicatedserver) {
		textcolor(SCREEN_COLOR_BLUE);
	}
	print_ip_aligned(MENU_YPOS+1);
	gotoxy(X_POS_CONFIG_ITEMS, MENU_YPOS+2); cprintf("%d", config.port);
	textcolor(SCREEN_COLOR_YELLOW);
	gotoxy(X_POS_CONFIG_ITEMS, MENU_YPOS+3); cprintf("%d", config.refreshtime);
	gotoxy(X_POS_CONFIG_ITEMS, MENU_YPOS+4); cprintf("%d", config.cycletime);
	gotoxy(X_POS_CONFIG_ITEMS, MENU_YPOS+5); cprintf("%s", stations[config.station].menu_name);
	gotoxy(X_POS_CONFIG_ITEMS, MENU_YPOS+6); cprintf("%d", config.startpage);
}

void clear_petsciiproxylist(unsigned char x, unsigned char y) {
	unsigned char max_len = 0;
	unsigned char i, current_len;
	
	// determine length of the longest station name
	for (i = 0; i < 2; ++i) {
		current_len = strlen(use_dedicatedserver[i]);
		if (current_len > max_len) {
			max_len = current_len;
		}
	}
	// clear display area list
	revers(false);
	for (i=0; i<2; ++i) {
		gotoxy(x,y+i);
		cclear(max_len);
	}
}

void display_petsciiproxylist(unsigned char x, unsigned char y, char index) {
	char i;
	
	for (i=0; i<2; ++i) {
		gotoxy(x,y+i);
		if (i == index) {
			textcolor(SCREEN_COLOR_GREEN);
			revers(true);
		} else {
			textcolor(SCREEN_COLOR_GRAY_3);
			revers(false);
		}
		cputs(use_dedicatedserver[i]);
	}
}

void select_petsciiproxy(unsigned char x, unsigned char y) {
	static char c, old_value;

	old_value = config.dedicatedserver;
	clear_petsciiproxylist(x, y);
	display_petsciiproxylist(x, y, config.dedicatedserver);
	while (1) {
		c = kbhit();
		if (c != 0) {
			c = cgetc();
			switch (c) {
				case KEY_CRSR_UP:
					if (config.dedicatedserver > 0) display_petsciiproxylist(x, y, --config.dedicatedserver);
					break;
				case KEY_CRSR_DOWN:
					if (config.dedicatedserver < (2-1)) display_petsciiproxylist(x, y, ++config.dedicatedserver);
					break;
			}
			if (c == KEY_RETURN) break;
		}
	}
	clear_petsciiproxylist(x,y);
	if (old_value != config.dedicatedserver) {
		display_menu_items(0);
	}
}

void redraw_everything(char index) {
	station_picker_clear(false);
	display_menu_items(index);
	display_config_items();
	textcolor(SCREEN_COLOR_GRAY_2);
	gotoxy(0,23);
	chline(40);
}

void main(void) {
	char menu_index = 0, c;
	
	// determine from whice device the program was loaded
	cbm_device = PEEK(186);
	if (cbm_device < 8 || cbm_device > 11) {
		cbm_device = 8;
	}

	clrscr();
	POKEW(0xD020, 0); // POKEW also sets 0xD021 to 0
	gotoxy(3,0);
	textcolor(SCREEN_COLOR_CYAN);
	cprintf("*** TELETEXT 64 CONFIG UTILITY ***");
	textcolor(SCREEN_COLOR_GRAY_2);
	gotoxy(0,23);
	chline(40);
	display_menu_items(0);
	if (load_config() > 0) {
		config_set_defaults();
	}
	display_menu_items(0);
	display_menu_help();
	display_config_items();
	while (1) {
		c = kbhit();
		if (c != 0) {
			c = cgetc();
			switch (c) {
				case KEY_RETURN:
					if (menu_index == 0) {
						select_petsciiproxy(X_POS_CONFIG_ITEMS, MENU_YPOS);
					} else if (menu_index == 1) {
						edit_ip_string(X_POS_CONFIG_ITEMS, MENU_YPOS+1);
					} else if (menu_index == 2) {
						edit_port(X_POS_CONFIG_ITEMS, MENU_YPOS+2);
					} else if (menu_index == 3) {
						edit_time(X_POS_CONFIG_ITEMS, MENU_YPOS+3, &config.refreshtime);
					} else if (menu_index == 4) {
						edit_time(X_POS_CONFIG_ITEMS, MENU_YPOS+4, &config.cycletime);
					} else if (menu_index == 5) {
						station_picker();
						redraw_everything(menu_index);
					} else if (menu_index == 6) {
						edit_startpage(X_POS_CONFIG_ITEMS, MENU_YPOS+6);
					}
					display_config_items();
					break;
				case KEY_CRSR_UP:
					if (menu_index > 0)
						if (menu_index == 3 && config.dedicatedserver) {
							menu_index =  0;
						} else {
							--menu_index;
						}

					display_menu_items(menu_index);
					break;
				case KEY_CRSR_DOWN:
					if (menu_index < (NUM_MENU_ITEMS-1)) {
						if (menu_index == 0 && config.dedicatedserver) {
							menu_index =  3;
						} else {
							++menu_index;
						}
					}
					display_menu_items(menu_index);
					break;
				case KEY_F1:
					save_config();
					display_menu_help();
					break;
			}
			if (c == KEY_F3) break;
		}
	}
	clrscr();
	exit(0);
}
