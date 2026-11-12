/*****************************************************************
Teletext 64 Ultimate
 
Copyright (C) 2026 Frank Putman
SPDX-License-Identifier: GPL-3.0-or-later
******************************************************************/

typedef unsigned char bool;
#define true 1
#define false 0

// process_page_bytes.s
signed char process_page_bytes(const unsigned char *buf, int count);
signed char parse_header_byte(int intchar);

// Implemented in all edtions. Each get_teletext_page_impl() has a platform specific network fetch
void get_teletext_page(int pageNumber, int subPageNumber);
void get_teletext_page_str(const char *pageStr, int subPageNumber);

#define JIFFIES_PER_SECOND 60

#define TELETEXT64_VERSION "2.9.0"

#define MAX(a, b) ((a) > (b) ? (a) : b)

// C64 keyboard codes, appendix C ASCII AND CHR$ CODES, p379 Programmer's Reference Guide
// Note: letter keys may have a double function: as a quick select in the station select menu
#define KEY_RETURN			13		// Select highlighted quick navigation pagenumber
#define KEY_CLRHOME			19		// Teletext homepage - set by user in config
#define KEY_INSTDEL			20		// Deletes typed character (backspace)
#define KEY_COMMA				44		// Subpage up
#define KEY_DOT					46		// Subpage down
#define KEY_0						48
#define KEY_9						57
#define KEY_PLUS				43		// Next teletext page (+1)
#define KEY_MINUS 			45		// Previous teletext page (-1)
#define KEY_SLASH 			47		// Switch station forward
#define KEY_EQUAL				61		// alternative for the + (I use this when I have a PC-keyboard attached to my C64U)
#define KEY_QUESTION		63		// Switch station backward
#define KEY_A						65		// WASD Quick Navigation
#define KEY_B						66
#define KEY_C						67		// Toggle between revealed / concealed text
#define KEY_D						68		// WASD Quick Navigation
#define KEY_E						69		// Go to selected pagenumber
#define KEY_F						70
#define KEY_H						72
#define KEY_I						73
#define KEY_M						77		// menu station select
#define KEY_N						78
#define KEY_O						79
#define KEY_P						80		// Print Teletext page in B&W
#define KEY_SHIFT_P			208		// Print Teletext page in colour (MPS-1230 colour inks)
#define KEY_STOP				3			// RUN/STOP: cancel/step back in station select menu
#define KEY_Q						81		// Visit previous page (like on a web browser)
#define KEY_R						82		// refresh current page
#define KEY_S						83		// WASD Quick Navigation
#define KEY_T						84		// Toggle font
#define KEY_W						87		// WASD Quick Navigation
#define KEY_Y						89
#define KEY_Z						90
#define KEY_POUND				92		// Used for entering subpage number
#define KEY_ARROW_UP		94		// Step back
#define KEY_ARROW_LEFT	95		// Key left to the 1 key. Go back to the WiC64 Portal (only wic edition)
#define KEY_CRSR_DOWN		17		// Quick navigation LNK page numbers
#define KEY_CRSR_UP			145		// Quick navigation LNK page numbers
#define KEY_CRSR_LEFT		157		// Quick navigation LNK page numbers
#define KEY_CRSR_RIGHT	29		// Quick navigation LNK page numbers
#define KEY_F1					133		// Fast text red
#define KEY_F2       		137   // Open settings on startup (only on single PRG versions)
#define KEY_F3					134		// Fast text green
#define KEY_F4					138		// Stopwatch on/off
#define KEY_F5					135		// Fast text yellow
#define KEY_F6					139		// not assigned right now
#define KEY_F7					136		// Fast text cyan
#define KEY_F8					140		// Help screen
#define KEY_SPACE				32		// Hold/resume subpage auto-cycle

// C64 screen colors
#define SCREEN_COLOR_BLACK				0x00
#define SCREEN_COLOR_WHITE				0x01
#define SCREEN_COLOR_RED					0x02
#define SCREEN_COLOR_CYAN					0x03
#define SCREEN_COLOR_PURPLE				0x04
#define SCREEN_COLOR_GREEN				0x05
#define SCREEN_COLOR_BLUE					0x06
#define SCREEN_COLOR_YELLOW				0x07
#define SCREEN_COLOR_ORANGE				0x08
#define SCREEN_COLOR_BROWN				0x09
#define SCREEN_COLOR_LIGHT_RED		0x0A
#define SCREEN_COLOR_GRAY_1				0x0B
#define SCREEN_COLOR_GRAY_2				0x0C
#define SCREEN_COLOR_LIGHT_GREEN	0x0D
#define SCREEN_COLOR_LIGHT_BLUE		0x0E
#define SCREEN_COLOR_GRAY_3				0x0F

#define FONT_FILENAME_TEXT_THIN  "txt-font-thin"
#define FONT_FILENAME_TEXT_BOLD  "txt-font-bold"
#define FONT_FILENAME_LANG_THIN  "txt-font-thin-l"
#define FONT_FILENAME_LANG_BOLD  "txt-font-bold-l"
#define FONT_FILENAME_MOSAIC     "txt-font-mosaic"

// Config (user preferences)
#define CONFIG_FILENAME "ut64.cfg"
#define MAGIC						"ut64"
#if !defined(WIC64VERSION) || defined(DISK_CONFIG)
extern unsigned char cbm_device;
#endif

typedef struct {
	char magic[4];							// always: UT64
	char dedicatedserver;				// 0 = disabled; 1 = enabled
	char ip[4];									// 4 byte address like 192.168.0.64
	unsigned int port;					// server port number
	unsigned int refreshtime; 	// in seconds; 0=no refresh
	unsigned int cycletime; 		// in seconds; 0=accept default cycle time
	unsigned char station; 			// favorite station; default 0 (currently ARD-TEXT)
	unsigned int startpage;			// favorite startpage (100..899)
} Config;

extern Config config;
extern unsigned long refresh_interval_jiffies;

// language offset per station, see defines. Used by apply_language() for national special characters.
// In real teletext this info can be in each Teletext page. Not implemented to save memory.
// More info: ETS 300 706 reference guide, page 115, Table 36: Latin National Option Sub-sets
// https://www.etsi.org/deliver/etsi_i_ets/300700_300799/300706/01_60/ets_300706e01p.pdf
#define TXT_LANG_ENGLISH					0x00
#define TXT_LANG_FRENCH						0x10
#define TXT_LANG_SWEDISH_FINNISH	0x20
#define TXT_LANG_GERMAN						0x30
#define TXT_LANG_SPANISH					0x40
#define TXT_LANG_ITALIAN					0x50
#define TXT_LANG_POLISH						0x60

typedef struct {
	const char* url_name;
	const char* menu_name;
	unsigned char lang;			// see note above
} Station;

// Edit/add new station: always edit this file, txtlib.c and station_menu.h
enum {
	// Group 1: German
	STATION_ARDTEXT,
	STATION_ZDFTEXT,
	STATION_ZDFINFO,
	STATION_ZDFNEO,
	STATION_WDRTEXT,
	STATION_NDR,
	STATION_HR,
	STATION_SWR_BW,
	STATION_SWR_RP,
	STATION_3SAT,
	STATION_ORF1,
	STATION_ORF2,
	STATION_ORF3,
	STATION_ORFSPORT,
	STATION_SRF1,
	STATION_SRF2,
	STATION_SRFINFO,
	STATION_FORUM64,
	// Group 2: English
	STATION_CEEFAX,
	STATION_TEEFAX,
	STATION_CHUNKYTEXT,
	STATION_SPARK,
	STATION_WEBFAX1,
	STATION_WEBFAX2,
	// Group 3: Nordic
	STATION_DRTEKSTTV,
	STATION_SVTTEXT,
	STATION_YLETEKSTITV,
	STATION_HBNTEKSTITV,
	// Group 4: Other
	STATION_BMN1,	// aka Bollentekst
	STATION_MTVATEXT,
	STATION_NOSTT,
	STATION_NOSNEWS,
	STATION_TELETEKSTARCHIEF,
	STATION_RTS1,
	STATION_RTS2,
	STATION_RSILA1,
	STATION_RSILA2,
	STATION_TLGZ_TVP1,	// Telegazeta
	STATION_TLGZ_TVP2,
	STATION_TLGZ_TVP3,
	STATION_TLGZ_KULTURA,
	STATION_TLGZ_HISTORIA,
	STATION_TLGZ_SPORT,
	STATION_TLGZ_POLONIA,
	STATION_TELETEXT64,
	STATION_ENUM_END	// the number of stations; txtlib.c checks if stations[] has exactly this many entries
};

extern const Station stations[];

#define STATION_COUNT STATION_ENUM_END
#define MAX_STATION_INDEX	(STATION_COUNT-2)	// -2 because STATION_TELETEXT64 is hidden in the station select menu

// station menu window coords, used by bitmap mode (teletext_common.c) and character mode (station_picker.c) pickers
#define MENU_X			6
#define MENU_Y			3
#define MENU_WIDTH	28	// 2 station names (each 13 wide) + 1 gap column + 1 border

#include "station_menu.h"

#define MENU_ITEM_WIDTH			13
#define MENU_ITEM_SIZE			(MENU_ITEM_WIDTH + 1)	// +terminator
#define MENU_COLUMN_GAP			1
#define MENU_COL2_X					(MENU_X + 1 + MENU_ITEM_WIDTH + MENU_COLUMN_GAP)
#define MENU_SUBMENU_INDENT	7
#define SUBMENU_X						(MENU_X + MENU_SUBMENU_INDENT)
#define SUBMENU_WIDTH				(MENU_ITEM_WIDTH + 1)	// the +1 for the right border
#define NO_ITEM							0xFF
#define NO_FOLDER						NO_ITEM
#define MENU_CANCEL					NO_ITEM

extern const unsigned char menu_first[MENU_ENTRY_COUNT];
extern const unsigned char menu_count[MENU_ENTRY_COUNT];
extern unsigned char open_folder;
extern unsigned char submenu_row;

// Number of items in the window that is shown
#define menu_item_count() (open_folder == NO_FOLDER ? MENU_ENTRY_COUNT : menu_count[open_folder])

#define MENU_ITEM_COL(i) (open_folder != NO_FOLDER ? SUBMENU_X + 1 : \
	((i) < MENU_COL1_COUNT ? MENU_X + 1 : MENU_COL2_X))
#define MENU_ITEM_ROW(i) (open_folder != NO_FOLDER ? submenu_row + 1 + (i) : \
	MENU_Y + 1 + ((i) < MENU_COL1_COUNT ? (i) : (i) - MENU_COL1_COUNT))

const char *menu_item_name(unsigned char i, char *buf);
// Handles cursor and key presses and returns selected item
unsigned char menu_navigate(unsigned char selected, unsigned char key);
// Returns chosen station or MENU_CANCEL
unsigned char menu_choose(unsigned char current_station, unsigned char (*run)(unsigned char selected));

// fg_shifted[GRAY_1] | BLACK to change screen character color to dark gray for shadow/floating window effect (like Borland Pascal DOS IDE)
#define MENU_SHADOW_COLOR		0xB0
