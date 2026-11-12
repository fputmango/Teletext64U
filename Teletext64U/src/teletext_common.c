/*****************************************************************
Teletext 64 Ultimate
 
Copyright (C) 2026 Frank Putman
SPDX-License-Identifier: GPL-3.0-or-later
******************************************************************/

// used in the toggle conceal function
#define MIRROR_NIBBLE(v) (((v) & 0x0f) | ((v) << 4))
// the special 2-character green icon that will be dispayed next to the page number top left when there is hidden info on the page
#define TCC_CONCEAL_GRAPHIC 0x12	// and 0x13 is the other half

#define ASCII_LINE_FEED	0x0A

// used to detect a C= + other key combination
#define KBD_MODIFIERS PEEK(653)

// Teletext Control Characters; I support Teletext Level 1.5 (which most services still use on TV)
#define TCC_ALPHA_BLACK					0x00
#define TCC_ALPHA_RED						0x01
#define TCC_ALPHA_GREEN					0x02
#define TCC_ALPHA_YELLOW				0x03
#define TCC_ALPHA_BLUE					0x04
#define TCC_ALPHA_MAGENTA				0x05
#define TCC_ALPHA_CYAN					0x06
#define TCC_ALPHA_WHITE					0x07
#define TCC_FLASH								0x08
#define TCC_STEADY							0x09
#define TCC_ENDBOX							0x0A
#define TCC_STARTBOX						0x0B
#define TCC_NORMAL_HEIGHT				0x0C
#define TCC_DOUBLE_HEIGHT				0x0D
#define TCC_DOUBLE_WIDTH				0x0E	// double with is Teletext Level 2.5 and is not implemented: doubles as TCC_ALPHA_ORANGE, see below
#define TCC_DOUBLE_SIZE					0x0F	// double size is Teletext Level 2.5 and is not implemented: doubles as TCC_MOSAIC_ORANGE
#define TCC_MOSAIC_BLACK				0x10
#define TCC_MOSAIC_RED					0x11
#define TCC_MOSAIC_GREEN				0x12
#define TCC_MOSAIC_YELLOW				0x13
#define TCC_MOSAIC_BLUE					0x14
#define TCC_MOSAIC_MAGENTA			0x15
#define TCC_MOSAIC_CYAN					0x16
#define TCC_MOSAIC_WHITE				0x17
#define TCC_CONCEAL							0x18
#define TCC_CONTINUOUS_MOSAICS	0x19
#define TCC_SEPERATED_MOSAICS		0x1A
#define TCC_ESC_GO_SWITCH				0x1B
#define TCC_BLACK_BACKGROUND		0x1C
#define TCC_NEW_BACKGROUND			0x1D
#define TCC_HOLD_MOSAICS				0x1E
#define TCC_RELEASE_MOSAICS			0x1F

// These codes are misused/reused. Why? Because I needed orange for the Forum64 logo, the Meatloaf logo and the Dutch Royals pages on NOS Nieuws
#define TCC_ALPHA_ORANGE				0x0E
#define TCC_MOSAIC_ORANGE				0x0F


#define CBM_LOGICAL_FILE_NUMBER	1
#define CBM_SEC_ADDR						2

#define VIC_D011					(*(unsigned char*)0xD011)
#define VIC_D018					(*(unsigned char*)0xD018)
#define VIC_BORDER_COLOR	(*(unsigned char*)0xD020)

// VIC bank 2 ($8000-$BFFF)
#define BITMAP					((unsigned char*)0xA000)
#define SCREEN_RAM			((unsigned char*)0x8000)
#define SCREEN_RAM2			((unsigned char*)0x8400)
#define SCREEN_CONCEAL	((unsigned char*)0x8800)
#define SCREEN_FLASH		((unsigned char*)0x8C00)

#define FONT_LANG_ADDR	0x9000
#define FONT_TEXT_ADDR	0x9400
#define FONT_LANG_TABLE	((unsigned char*)FONT_LANG_ADDR)	// currently 896 bytes, must end before FONT_TEXT_BOLD
#define FONT_TEXT_BOLD	((unsigned char*)FONT_TEXT_ADDR)	// 1K or 128 characters
#define FONT_MOSAIC			((unsigned char*)0x9800) 					// 1K or 128 characters

#define FONT_SIZE_1K		1024
#define FONT_SIZE_LANG	896		// 7 languages x 16 char slots x 8 bytes/character (I added Polish language support in release 2.9.0)
// characters per language (see Latin National Option Sub-sets, p115, https://www.etsi.org/deliver/etsi_i_ets/300700_300799/300706/01_60/ets_300706e01p.pdf)
#define NAT_CHAR_COUNT	13

// Compile time check - language table cannot run into text font (negative array size if it does)
typedef char lang_table_fits[(FONT_LANG_ADDR + FONT_SIZE_LANG <= FONT_TEXT_ADDR) ? 1 : -1];

// G2 supplementary font: backs a subset of the 0x00-0x1F index range instead of the main font, for extended-ASCII characters that no
// longer fit in the "free" slots of the main font.
#define FONT_SIZE_G2			224
#define FONT_G2_ADDR			0x9D00
#define FONT_G2_BOLD_BASE	((unsigned char*)FONT_G2_ADDR)

#define HEAP_TOP				0xD000
#define MLF_HEAP1_ADDR	0x9C00
#define MLF_HEAP1_SIZE	256
#define MLF_HEAP2_ADDR	(FONT_G2_ADDR + FONT_SIZE_G2)   // right after the G2 font
#define MLF_HEAP2_SIZE	(0xA000 - MLF_HEAP2_ADDR)       // up to the end of VIC bank 2

typedef char mlf_heap1_fits[(MLF_HEAP1_ADDR + MLF_HEAP1_SIZE <= FONT_G2_ADDR) ? 1 : -1];

#define FLASH_SPEED	30	// jiffies = 0.5 sec

#define PAGE_FETCH_TIMEOUT_JIFFIES		(20UL * JIFFIES_PER_SECOND)
#define LOAD_INDICATOR_DELAY_JIFFIES	3

// I used to put the Pnnn pagenumber on 0,0. 1 is visually alignes better because most services use column 0 for setting a control code
#define PAGENUMBER_X	1

static unsigned char stopwatch_enabled = false;

static bool font_is_thin = false;

typedef enum { FONT_FILE_TEXT, FONT_FILE_G2, FONT_FILE_LANG, FONT_FILE_MOSAIC } FontFileKind;

const char * const font_filenames[4][2] = {
	{ "teletext-bold.bin",						"teletext-thin.bin" },
	{ "teletext-bold-g2.bin",					"teletext-thin-g2.bin" },
	{ "teletext-languages-bold.bin",	"teletext-languages-thin.bin" },
	{ "teletext-mosaic.bin",					"teletext-mosaic.bin" },
};

// Implemented per build
extern bool fetch_font_file(FontFileKind kind, bool thin, unsigned char *dest, unsigned int len);

// These need to be loaded when swapping fonts
bool load_font_set(bool thin) {
	if (!fetch_font_file(FONT_FILE_TEXT, thin, FONT_TEXT_BOLD, FONT_SIZE_1K)) return false;
	if (!fetch_font_file(FONT_FILE_G2, thin, FONT_G2_BOLD_BASE, FONT_SIZE_G2)) return false;
	if (!fetch_font_file(FONT_FILE_LANG, thin, FONT_LANG_TABLE, FONT_SIZE_LANG)) return false;
	return true;
}

// mosaic font is loading one time at startup only and stays the same with each font toggle
bool load_fonts_at_startup(void) {
	return load_font_set(false) && fetch_font_file(FONT_FILE_MOSAIC, false, FONT_MOSAIC, FONT_SIZE_1K);
}

// Lookup tables pre-calculated at init for performance
unsigned int row_bitmap_offset[25];	// exported: used by blit_row.s
unsigned int row_screen_offset[25];	// exported: used by blit_row.s
unsigned char* font_ptr[256];				// exported: used by blit_row.s
unsigned int blit_screen_base;			// exported: SCREEN_RAM address, set by init_row_tables()

// foreground colour speed optimization
const unsigned char fg_shifted[16] = { 0x00, 0x10, 0x20, 0x30, 0x40, 0x50, 0x60, 0x70, 0x80, 0x90, 0xA0, 0xB0, 0xC0, 0xD0, 0xE0, 0xF0 };

// C64 color codes; exported: used by process_page_bytes.s
unsigned char cg_color[] = {
	SCREEN_COLOR_BLACK,
	SCREEN_COLOR_RED,
	SCREEN_COLOR_GREEN,
	SCREEN_COLOR_YELLOW,
	SCREEN_COLOR_BLUE,
	SCREEN_COLOR_PURPLE,
	SCREEN_COLOR_CYAN,
	SCREEN_COLOR_WHITE
};

// TARGET_DEV is used in build-all-dev.sh
#define PETSCIIPROXY_HOST "petsciiproxy.nl"
#ifdef TARGET_DEV
	#define PETSCIIPROXY_PORT 8081
	#define PETSCIIPROXY PETSCIIPROXY_HOST ":8081"
#else
	#define PETSCIIPROXY_PORT 80
	#define PETSCIIPROXY PETSCIIPROXY_HOST
#endif
#define PETSCIIPROXY_EXTRA (sizeof(PETSCIIPROXY) - sizeof(PETSCIIPROXY_HOST))

static char host[16];	// IP-address of host server or URL

// Fastext links: 3 chars+NUL for red/green/yellow/cyan bottom-row links
static char FTL[4][4];

// Optional mouse-hover highlight geometry for each FTL[] slot, parsed from the header "ftl=nnn,cc,ww"
// nnn=pagenumber 100..899
// cc=start column 01..37
// ww=width
static unsigned char FTL_col[4];
static unsigned char FTL_width[4];

// A page link parameter from the NOS-TT header: lnk=nnn,rr,cc,h
// rr=row 01..23
// h=double-height row? (0=no, 1=yes)
#define LNK_MAX 50
static unsigned int LNK_page[LNK_MAX];	// page number
static unsigned char LNK_row[LNK_MAX];	// row of the link's first digit (1-23)
static unsigned char LNK_col[LNK_MAX];	// column of the link's first digit (1-37)
static unsigned char LNK_dh[LNK_MAX];		// 1 for double-height, else 0
static unsigned char LNK_count;					// number of links parsed for the current page
static signed char LNK_selected;				// -1 = nothing highlighted yet; else index into the arrays above
static unsigned char LNK_saved[6];			// saved SCREEN_RAM bytes under the current highlight (3 top-half + 3 bottom-half for DH), for restore

static unsigned int PN_P;		// PageNumber_Previous
static unsigned int PN_N;		// PageNumber_Next
static unsigned int PN_PS;	// PageNumber_PreviousSubpage
static unsigned int PN_NS;	// PageNumber_NextSubpage

static unsigned int lastSubPageNumber;
static bool subpage_input_mode = false;
// important: the '= 0' assignment keeps them in DATA (not BSS)
static int newPageNumber = 0;
static int digitsEntered = 0;

#define DEFAULT_CYCLE_TIME 10 				// default value in seconds
static unsigned char cycle_time = 0;	// 0 = disabled; set to a value is a page has subpages
static bool cycle_paused = false;			// toggled by SPACE bar

// used to time page refreshes and flashing text
static unsigned long last_jiffies  = 0;
static unsigned long flash_jiffies = 0;
static unsigned long cycle_jiffies = 0;
static unsigned long stopwatch_jiffies = 0;

static unsigned char station_index = 0;

// Not static: referenced as external symbols from toggle_conceal.s and toggle_flash.s
unsigned char is_conceal = true;
unsigned char is_flash = true;

// The stack is used for pushing and popping page numbers and stepping back (↑/Q key or right mouse button)
#define STACK_SIZE 20
static int stack[STACK_SIZE];
static signed char stack_pointer = -1;

unsigned char push(int value) {
	if (stack_pointer < STACK_SIZE-1) {
		stack[++stack_pointer] = value;
		return true;
	}
	return false;
}

unsigned char pop(int* result) {
	if (stack_pointer >= 0) {
		*result = stack[stack_pointer--];
		return true;
	}
	return false;
}

// PETSCII <-> ASCII conversion functions
// Thanks to Gregory Naçu for his research
// https://c64os.com/post/petsciiasciiconversion
static char petscii_char_to_ascii(char c) {
	// flip CR & LF
	if (c == 0x0A)
		return 0x0D;
	if (c == 0x0D)
		return 0x0A;
	if (c == 0xC0)
		return 0x60;
	// lower case
	if (c >= 0x41 && c <= 0x7A)
		return c + 0x20;
	// upper case
	if (c >= 0xC1 && c <= 0xDA)
		return c - 0x80;
	// ` { } Symbols
	if (c >= 0xDB && c <= 0xDF)
		return c - 0x60;
	return c;
}

#if !defined(MLFVERSION)
void petscii_to_ascii(char *p) {
	while (*p) {
		*p = petscii_char_to_ascii(*p);
		p++;
	}
}
#endif

void ascii_to_petscii(char *p) {
	while (*p) {
		if (*p == 0x0A)
			*p = 0x0D;
		else if (*p == 0x0D)
			*p = 0x0A;
		else if (*p == 0x60)
			*p = 0xC0;
		else if (*p >= 0x61 && *p <= 0x9A)
			*p -= 0x20;
		else if (*p >= 0x41 && *p <= 0x5A)
			*p += 0x80;
		else if (*p >= 0x7B && *p <= 0x7F)
			*p += 0x60;
		p++;
	}
}

void welcome_screen(void) {
	clrscr();
	POKEW(0xD020, 0x00);
	textcolor(SCREEN_COLOR_CYAN);
	cputs("      *** Teletext 64 Ultimate ***");
#if defined(WIC64VERSION)
	gotoxy(5,2);
	cputs("Version ");
	cputs(TELETEXT64_VERSION);
	cputs(" - ");
	cputs(WIC64VERSION);
#elif defined(MLFVERSION)
	gotoxy(4,2);
	cputs("Version ");
	cputs(TELETEXT64_VERSION);
	cputs(" - ");
	cputs(MLFVERSION);
#elif defined(UCI_STANDALONE)
	gotoxy(6,2);
	cputs("Version ");
	cputs(TELETEXT64_VERSION);
	cputs(" - ");
	cputs(UCIEDITION);
#else
	gotoxy(13,2);
	cputs("Version ");
//	gotoxy(21,2);
	cputs(TELETEXT64_VERSION);
#endif
	textcolor(SCREEN_COLOR_GRAY_2);
	gotoxy(3,4);
	textcolor(SCREEN_COLOR_GREEN);
	cputs("Developed by Frank Putman in 2026");
	gotoxy(4,5);
	textcolor(SCREEN_COLOR_GRAY_3);
	cputs("github.com/fputmango/Teletext64U");
}

void show_F8_for_help(void) {
	gotoxy(0,23);
	textcolor(SCREEN_COLOR_WHITE);
	cputs("Press ");
	textcolor(SCREEN_COLOR_YELLOW);
	cputs("F8");
	textcolor(SCREEN_COLOR_WHITE);
	cputs(" for help in teletext screen");
	gotoxy(0,7);
}

void config_show(void) {
	char *p = host;
	char temp[6];

	textcolor(SCREEN_COLOR_WHITE);
	cputs("Auto refresh: ");
	textcolor(SCREEN_COLOR_YELLOW);
	cputs(utoa(config.refreshtime, temp, 10)); cputs("\r\n");
	textcolor(SCREEN_COLOR_WHITE);
	cputs("Cycle time  : ");
	textcolor(SCREEN_COLOR_YELLOW);
	cputs(utoa(config.cycletime, temp, 10)); cputs("\r\n");
	textcolor(SCREEN_COLOR_WHITE);
	cputs("Station     : ");
	textcolor(SCREEN_COLOR_YELLOW);
	cputs(stations[config.station].url_name); cputs("\r\n");
	textcolor(SCREEN_COLOR_WHITE);
	cputs("Start page  : ");
	textcolor(SCREEN_COLOR_YELLOW);
	cputs(utoa(config.startpage, temp, 10)); cputs("\r\n\n");
}

#if (!defined(WIC64VERSION) || defined(DISK_CONFIG)) && !defined(UCI_STANDALONE) && !defined(MLFVERSION)
bool config_load(void) {
	char *p;
	char temp[6];
	
	cputs("Loading config...");
	if (cbm_open(CBM_LOGICAL_FILE_NUMBER, cbm_device, CBM_SEC_ADDR, CONFIG_FILENAME) != 0) {
		cputs("failed to open\r\n");
		return false;
	}
	if (cbm_read(CBM_LOGICAL_FILE_NUMBER, &config, sizeof(Config)) != sizeof(Config)) {
		cbm_close(CBM_LOGICAL_FILE_NUMBER);
		cputs("read error\r\n");
		return false;
	}
	cbm_close(CBM_LOGICAL_FILE_NUMBER);

	if (config.magic[0] != MAGIC[0] || config.magic[1] != MAGIC[1] || config.magic[2] != MAGIC[2] || config.magic[3] != MAGIC[3]) {
		return false;
	}

	refresh_interval_jiffies = config.refreshtime * JIFFIES_PER_SECOND;

	cputs("done\r\n\n");
	
	if (config.dedicatedserver) {
		strcpy(host, PETSCIIPROXY_HOST);
		config.port = PETSCIIPROXY_PORT;
	} else {
		// Format IP address
		p = host;
		utoa(config.ip[0], p, 10); p += strlen(p); *p++ = '.';
		utoa(config.ip[1], p, 10); p += strlen(p); *p++ = '.';
		utoa(config.ip[2], p, 10); p += strlen(p); *p++ = '.';
		utoa(config.ip[3], p, 10);
	}

	textcolor(SCREEN_COLOR_WHITE);
	cputs("Server      : ");
	textcolor(SCREEN_COLOR_YELLOW);
	cputs(host);
	if (!config.dedicatedserver) {
		cputc(':');
		cputs(utoa(config.port, temp, 10));
	}
	cputs("\r\n");

	config_show();

	return true;
}
#endif

// https://www.c64-wiki.com/wiki/Jiffy_Clock
#define JIFFY_CLOCK_HIGH_BYTE		PEEK(160)		// Changes every 18.2 minutes
#define JIFFY_CLOCK_MIDDLE_BYTE	PEEK(161)		// Changes every 4.26 seconds
#define JIFFY_CLOCK_LOW_BYTE 		PEEK(162)		// Changes every 16.67 ms

unsigned long get_jiffies(void) {
	// using a 4 byte union/struct to overlay 3 bytes directly onto a long
	static union {
		unsigned long value;
		unsigned char bytes[4];
	} j;
	asm("sei");
	j.bytes[0] = JIFFY_CLOCK_LOW_BYTE;
	j.bytes[1] = JIFFY_CLOCK_MIDDLE_BYTE;
	j.bytes[2] = JIFFY_CLOCK_HIGH_BYTE;
	j.bytes[3] = 0;
	asm("cli");
	return j.value;
}

// note: can only be used for very short delays (255 * 16.67ms)
void delay_jiffies(unsigned char count) {
	unsigned char start = JIFFY_CLOCK_LOW_BYTE;
	while ((unsigned char)(JIFFY_CLOCK_LOW_BYTE - start) < count);
}

bool refresh_txtpage(void) {
	if (config.refreshtime == 0) return false;
	{
		unsigned long current_jiffies = get_jiffies();
		if ((current_jiffies - last_jiffies) >= (refresh_interval_jiffies)) {
			last_jiffies = current_jiffies;
			return true;
		}
	}
	return false;
}

void reset_autorefresh_timer(void) {
	last_jiffies = get_jiffies();
}

bool cycle_subpage_due(void) {
	if (PN_NS == 0xFF) return false; // no subpages to cycle?
	if (cycle_time == 0 || cycle_paused) return false;
	return (get_jiffies() - cycle_jiffies) >= ((unsigned long)cycle_time * JIFFIES_PER_SECOND);
}

#if defined(UCIVERSION)
bool is_bracket_close(char c) {
	return c == 62; // ASCII code for >
}
#endif

#if 0
bool is_bracket_open(char c) {
	return (c == 60); // ASCII code for <
}
#endif

// clear memory just before displaying a new teletext page
void clear_bitmap_screen(void) {
	memset(BITMAP,         0x00, 8000);	// 320 * 200 / 8 bytes
	memset(SCREEN_RAM,     0x00, 1000);	// 40 columns * 25 rows
	memset(SCREEN_RAM2,    0x00, 1000);	// holds a copy of color values; used for reveal
	memset(SCREEN_CONCEAL, 0x00, 1000);	// conceal and flash could be combined later but also introduces new logic to handle it
	memset(SCREEN_FLASH,   0x00, 1000);
}

bool loading_screen_active = false;

#define LOADING_MESSAGE "Loading...please wait"

// Switch off BASIC, select VIC bank 2 and enable bitmap mode
void init_bitmap_mode(void) {
	clear_bitmap_screen();
	*((unsigned char*)0x01) = *((unsigned char*)0x01) & 0xFE;
	*((unsigned char*)0xDD00) = (*((unsigned char*)0xDD00) & 0xFC) | 0x01;
	VIC_D011 = VIC_D011 | 0x20;
	VIC_D018 = 0x08;
	VIC_BORDER_COLOR = SCREEN_COLOR_BLACK;
}

// precalculate offsets to increase performance
void init_row_tables(void) {
	static unsigned char r;
	unsigned int screen = 0;
	unsigned int bitmap = (unsigned int)BITMAP;

	blit_screen_base = (unsigned int)SCREEN_RAM;   // used by blit_row.s
	for (r = 0; r < 25; ++r) {
		row_screen_offset[r] = screen;
		row_bitmap_offset[r] = bitmap;
		screen += 40;
		bitmap += 320;
	}
}

// precalculate font pointers to increase performance
// $00-$7F -> text font (thin or bold), $80-$FF -> shared mosaic font
void init_font_ptr(void) {
	static unsigned int i;
	
	for (i = 0; i < 0x80; ++i) {
		font_ptr[i] = FONT_TEXT_BOLD + (i << 3);
	}
	for (i = 0x80; i < 0x100; ++i) {
		font_ptr[i] = FONT_MOSAIC + ((i - 0x80) << 3);
	}
}

// Most indices -not all- within 0x00-0x1F get their glyph from the G2 supplementary font. This is every extended-ASCII output value from
// map_extended_ascii_to_txt_charset().
static const unsigned char g2_indices[] = {
	0x00, 0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08,	// 0x09 and 0x10 are mouse menu icons, and drwan from the main font! (same for 0x1C-1F)
	0x0B, 0x0C, 0x0D, 0x0E, 0x0F, 0x10, 0x11,							// 0x12 and 0x13 together form the conceal icon
	0x14, 0x15, 0x16, 0x17, 0x18, 0x19, 0x1A, 0x1B
};
#define G2_INDEX_COUNT (sizeof(g2_indices) / sizeof(g2_indices[0]))

// Overwrites font_ptr[] for the indices in g2_indices[]. Is called AFTER init_font_ptr(), since that function unconditionally rebuilds the
// full 0x00-0xFF range first.
void apply_g2_font_ptr(void) {
	static unsigned int i;
	
	for (i=0; i<G2_INDEX_COUNT; ++i) {
		font_ptr[g2_indices[i]] = FONT_G2_BOLD_BASE + (g2_indices[i] << 3);
	}
}

// Latin National Option Sub-sets. These positions in the characterset will be overwritten with the language specific characters.
// more info here on page 115:
// https://www.etsi.org/deliver/etsi_i_ets/300700_300799/300706/01_60/ets_300706e01p.pdf
static const unsigned char nat_pos[NAT_CHAR_COUNT] = {
	0x23, 0x24, 0x40, 0x5B, 0x5C, 0x5D, 0x5E, 0x5F, 0x60, 0x7B, 0x7C, 0x7D, 0x7E
};

static unsigned char current_lang = 0xFF;

// Applies National Option Sub-set in current font
void apply_language(unsigned char lang_offset) {
	const unsigned char *src;
	unsigned char i;

	if (lang_offset == current_lang) return;

	src = FONT_LANG_TABLE + lang_offset * 8;
	for (i=0; i<NAT_CHAR_COUNT; ++i) {
		memcpy(FONT_TEXT_BOLD + (nat_pos[i] << 3), src + (i << 3), 8);
	}
	current_lang = lang_offset;
}

bool toggle_font(void) {
	bool want_thin = !font_is_thin;
	
	if (!load_font_set(want_thin)) return false;
	font_is_thin = want_thin;
	init_font_ptr();
	apply_g2_font_ptr();
	// Force the current_lang guard open first, since lang_offset may be unchanged from before the toggle.
	current_lang = 0xFF;
	apply_language(stations[station_index].lang);
	return true;
}

// copy8.s
void copy8(unsigned char *dst, const unsigned char *src);

// heap_start.s
// Used by platform main() to hand _heapadd() a heap region that always starts right after BSS
// returns __BSS_RUN__ + __BSS_SIZE__
unsigned get_heap_start(void);

// blit_row.s
void blit_row(unsigned char row, const unsigned char *chars, const unsigned char *colors);

void blit_fill_rect_asm(void);

// Args for blit_fill_rect_asm
unsigned char blit_f_col;
unsigned char blit_f_row;
unsigned char blit_f_w;
unsigned char blit_f_h;
unsigned char blit_f_color;

void txt_draw_char(unsigned char ch, unsigned char col, unsigned char row, unsigned char fg, unsigned char bg,
                   unsigned char doubleheight, unsigned char conceal, unsigned char flash) {
	static unsigned int screen_index;
	static unsigned char *src, *dst, i;

	dst = (unsigned char*)row_bitmap_offset[row] + (col << 3);

	// Double height detect
	if (row > 0) {
		for (i = 0; i < 8; ++i)
			if (dst[i] != 0) return;
	}

	screen_index = row_screen_offset[row] + col;
	// Double height detect
	if (row > 0 && SCREEN_RAM[screen_index] != 0x00) return;

	src = font_ptr[ch];

	// commbine this in 1 map file? instead of using 0/1 we can set bits in a byte; would save 1000 bytes (but adds logic)
	if (conceal) SCREEN_CONCEAL[screen_index] = true;
	if (flash) SCREEN_FLASH[screen_index] = true;

	if (doubleheight) {
		// draws first half of the character on row
		/*dst[0] = src[0];
		 dst[1] = src[0];
		 dst[2] = src[1];
		 dst[3] = src[1];
		 dst[4] = src[2];
		 dst[5] = src[2];
		 dst[6] = src[3];
		 dst[7] = src[3];*/
		// for loop translates to less code compared to above
		// what happens here is that we draw the source byte on the current and next line
		for (i=0; i<4; ++i) {
			dst[i*2] = src[i];
			dst[i*2+1] = src[i];
		}
	} else {
		copy8(dst, src);
	}

	if (conceal || flash) {
		// Character will be shown with same foreground and background color aka not visible
		SCREEN_RAM[screen_index] = MIRROR_NIBBLE(bg);
		// Store the color value in RAM2 for later use
		SCREEN_RAM2[screen_index] = fg_shifted[fg] | bg;
	} else {
		SCREEN_RAM[screen_index] = fg_shifted[fg] | bg;
	}

	if (!doubleheight) return;

	dst = (unsigned char*)row_bitmap_offset[row+1] + (col << 3);
	screen_index = row_screen_offset[row+1] + col;

	if (conceal) SCREEN_CONCEAL[screen_index] = true;
	if (flash)   SCREEN_FLASH[screen_index]   = true;

	// draws second half of the character on row+1
	for (i=0; i<4; ++i) {
		dst[i*2] = src[i+4];
		dst[i*2+1] = src[i+4];
	}

	if (conceal || flash) {
		SCREEN_RAM[screen_index] = MIRROR_NIBBLE(bg);
		SCREEN_RAM2[screen_index] = fg_shifted[fg] | bg;
	} else {
		SCREEN_RAM[screen_index]  = fg_shifted[fg] | bg;
	}
}

void txt_draw_string(const char *str, unsigned char col, unsigned char row, unsigned char fg, unsigned char bg) {
	static char c;
	unsigned int screen_index;
	unsigned char *src, *dst;
	while (*str) {
		c = petscii_char_to_ascii(*str++);
		src = font_ptr[c];
		dst = (unsigned char*)row_bitmap_offset[row] + (col << 3);
		screen_index = row_screen_offset[row] + col++;
		copy8(dst, src);
		SCREEN_RAM[screen_index] = fg_shifted[fg] | bg;
	}
}

void show_loading_screen(void) {
	clear_bitmap_screen();
	txt_draw_string(LOADING_MESSAGE, PAGENUMBER_X, 0, SCREEN_COLOR_WHITE, SCREEN_COLOR_BLACK);
	loading_screen_active = true;
}

void update_cycle_stop_cont_indicator(void) {
	if (cycle_paused) {
		txt_draw_string("HOLD", PAGENUMBER_X, 0, SCREEN_COLOR_GREEN, SCREEN_COLOR_BLACK);
	} else {
		cycle_jiffies = get_jiffies();
		txt_draw_string("CONT", PAGENUMBER_X, 0, SCREEN_COLOR_GREEN, SCREEN_COLOR_BLACK);
	}
}

static void menu_put_char(unsigned char ch, unsigned char col, unsigned char row, unsigned char colour) {
	unsigned int  screen_index = row_screen_offset[row] + col;
	unsigned char *src = font_ptr[ch];
	unsigned char *dst = (unsigned char*)row_bitmap_offset[row] + (col << 3);
	
	copy8(dst, src);
	SCREEN_RAM[screen_index] = colour;
}

void menu_draw_char(unsigned char ch, unsigned char col, unsigned char row, unsigned char fg, unsigned char bg) {
	menu_put_char(ch, col, row, fg_shifted[fg] | bg);
}

void reset_prev_next(void) {
	PN_P = PN_N = PN_PS = 0;
	PN_NS = 0xFF; // dont presume a page has subpages
	FTL[0][0] = FTL[1][0] = FTL[2][0] = FTL[3][0] = '\0';
	FTL_width[0] = FTL_width[1] = FTL_width[2] = FTL_width[3] = 0;
	lastSubPageNumber = 0;
	subpage_input_mode = false;
	newPageNumber = digitsEntered = 0; // abort any half-typed number
	cycle_paused = false;
	cycle_time = 0;
}

void time_elapsed(void) {
	unsigned long elapsed = get_jiffies() - stopwatch_jiffies;
	char strElapsed[40];
	
	ultoa(elapsed, strElapsed, 10);
	txt_draw_string(strElapsed, 0, 24, SCREEN_COLOR_WHITE, SCREEN_COLOR_BLUE);
}

// Flash / conceal (loop bodies in asm)
void toggle_conceal();
void toggle_flash_loop(void);

void toggle_flash(void) {
	is_flash = !is_flash;
	flash_jiffies = get_jiffies();
	toggle_flash_loop();
}

void clear_popup_canvas(unsigned char x, unsigned char y, unsigned char w, unsigned char h, unsigned char fg, unsigned char bg) {
	// Clear inside; see blit_fill.s
	blit_f_col   = x+1;
	blit_f_row   = y+1;
	blit_f_w     = w-1;
	blit_f_h     = h-1;
	blit_f_color = fg_shifted[fg] | bg;
	blit_fill_rect_asm();
}

void draw_popup_window(unsigned char x, unsigned char y, unsigned char w, unsigned char h, unsigned char fg, unsigned char bg) {
	// border characters: top row (left, bar, right), middle rows (left, -, right), bottom row
	static const unsigned char border[9] = { 0xB7, 0xA3, 0xEB,  0xB5, 0, 0xB5,  0xF5, 0xF0, 0xFA };
	unsigned char row, col, part, step, c1 = fg_shifted[fg] | bg, c2 = fg_shifted[bg] | fg;

	clear_popup_canvas(x,y,w,h,fg,bg);

	// The top and bottom row are drawn completely, the rows in between only at the left and right side (right reversed colours)
	for (row = 0; row <= h; ++row) {
		part = (row == 0) ? 0 : (row == h) ? 6 : 3;
		step = (part == 3) ? w : 1;
		for (col = 0; col <= w; col += step) {
			menu_put_char(border[part + ((col == 0) ? 0 : (col == w) ? 2 : 1)], x+col, y+row, (part == 3 && col == w) ? c2 : c1);
		}
	}
	// draw right and bottom shadow by changing colour value
	for (row = 0; row <= h + w + 1; ++row) {
		if (row <= h) {
			col = x + w + 1;
			part = y + 1 + row;
		} else {
			col = x + row - h;
			part = y + h + 1;
		}
		SCREEN_RAM[row_screen_offset[part] + col] = MENU_SHADOW_COLOR;
	}
}

static void item_draw(unsigned char i, unsigned char selected) {
	char buf[MENU_ITEM_SIZE];
	unsigned char fg = (open_folder == NO_FOLDER && menu_count[i] == 0) ? SCREEN_COLOR_BLUE : SCREEN_COLOR_BLACK;
	unsigned char bg = (i == selected) ? SCREEN_COLOR_LIGHT_GREEN : SCREEN_COLOR_GRAY_3;

	txt_draw_string(menu_item_name(i, buf), MENU_ITEM_COL(i), MENU_ITEM_ROW(i), fg, bg);
}

#if defined(MOUSESUPPORT)
// Returns the click edges in out_clicked: the CLICK_ bits that went down since the previous call
#define CLICK_LEFT	1
#define CLICK_RIGHT	2
extern bool mouse_raw_poll(unsigned char *out_row, unsigned char *out_col, unsigned char *out_clicked);
#endif
extern unsigned char pending_key;

static unsigned char menu_run(unsigned char selected) {
	unsigned char item_count = menu_item_count(), i, previous;
	char key;

	if (open_folder == NO_FOLDER) {
		draw_popup_window(MENU_X, MENU_Y, MENU_WIDTH, MENU_HEIGHT, SCREEN_COLOR_GRAY_1, SCREEN_COLOR_GRAY_3);
	} else {
		draw_popup_window(SUBMENU_X, submenu_row, SUBMENU_WIDTH, item_count+1, SCREEN_COLOR_GRAY_1, SCREEN_COLOR_GRAY_3);
	}
	for (i=0; i<item_count; ++i) item_draw(i, selected);

	while (1) {
		previous = selected;
		key = 0;
#if defined(MOUSESUPPORT)
		{
			unsigned char mouse_row, mouse_col, click_edges, hover_item, hover_col;
			static unsigned char last_mouse_row = 0xFF, last_mouse_col = 0xFF;
			
			if (mouse_raw_poll(&mouse_row, &mouse_col, &click_edges)) {
				// The item under the pointer (NO_ITEM = none). Right column continues the numbering of the left one.
				if (open_folder == NO_FOLDER) {
					hover_item = mouse_row - (MENU_Y+1);
					hover_col = mouse_col - (MENU_X+1);
					if (hover_item >= MENU_ROWS) {
						hover_item = NO_ITEM;
					} else if (hover_col >= MENU_ITEM_WIDTH + MENU_COLUMN_GAP) {	// right column (the gap column in between is no item)
						hover_col -= MENU_ITEM_WIDTH + MENU_COLUMN_GAP;
						hover_item += MENU_COL1_COUNT;
					} else if (hover_item >= MENU_COL1_COUNT) {
						hover_item = NO_ITEM;
					}
					if (hover_item < item_count && menu_count[hover_item] == 0) hover_item = NO_ITEM;
				} else {
					hover_item = mouse_row - (submenu_row+1);
					hover_col = mouse_col - (SUBMENU_X+1);
				}
				if (hover_col >= MENU_ITEM_WIDTH || hover_item >= item_count) hover_item = NO_ITEM;
				if (hover_item != NO_ITEM) {
					// Hover moves the selection; only when pointer moved, so the keys still work
					if (mouse_row != last_mouse_row || mouse_col != last_mouse_col) selected = hover_item;
					if (click_edges & CLICK_LEFT) {
						selected = hover_item;
						key = KEY_RETURN;
					}
				}
				last_mouse_row = mouse_row;
				last_mouse_col = mouse_col;
				if (click_edges & CLICK_RIGHT) key = KEY_STOP;
			}
		}
#endif
		if (key == 0) {
			if (pending_key) {
				key = pending_key;
				pending_key = 0;
			} else if (kbhit()) {
				key = cgetc();
			}
		}
		switch (key) {
			case 0:
				break;
			case KEY_STOP:
				return MENU_CANCEL;
			case KEY_RETURN:
				return selected;
			default:
				// cursor keys, letters and digits
				selected = menu_navigate(selected, key);
				break;
		}
		if (selected != previous) {
			item_draw(previous, selected);
			item_draw(selected, selected);
		}
	}
}

#if !defined(MLFVERSION) && (!defined(WIC64VERSION) || defined(DISK_CONFIG))
void show_wait_bar(unsigned char seconds) {
	static unsigned char number_of_jiffies;
	unsigned long t;
	unsigned char x;
	
	number_of_jiffies = seconds + ((seconds + 1) >> 1);
	textcolor(SCREEN_COLOR_GREEN);
	for (x=0; x<40; x++) {
		gotoxy(x,24);
		cputc(164); // horizontal stripe
	}
	for (x=0; x<40; x++) {
		gotoxy(x,24);
		cputc(32);
		t = get_jiffies();
		while ((get_jiffies() - t) < number_of_jiffies) {} // wait to draw next character
	}
}
#endif

// Maps extended ASCII characters to teletext character set 0x00..0x1F
// see https://www.ascii-code.com/ for the extended ASCII codes (128-255)
void map_extended_ascii_to_txt_charset(int *p, unsigned char i) {
	switch (*p) {
		case 0x80: *p = 0x0E; break; // €
		case 0xE7: *p = 0x08; break; // ç
		case 0xE8: *p = 0x03; break; // è
		case 0xE9: *p = 0x01; break; // é
		case 0xEA: *p = 0x02; break; // ê
		case 0xEB: *p = 0x00; break; // ë
		case 0xED: *p = 0x04; break; // í
		case 0xEE: *p = 0x0C; break; // î
		case 0xEF: *p = 0x0D; break; // ï
		case 0xE1: *p = 0x05; break; // á
		case 0xE2: *p = 0x06; break; // â
		case 0xDF: *p = 0x07; break; // ß
		case 0xF3: *p = 0x0B; break; // ó
		case 0xD3: *p = 0x0B; break; // Ó
		case 0xF4: *p = 0x0F; break; // ô
		case 0xFB: *p = 0x11; break; // û

		// Hungarian
		case 0xC1: *p = 0x18; break; // Á
		case 0xCD: *p = 0x19; break; // Í
		case 0xFA: *p = 0x1A; break; // ú
		case 0xDA: *p = 0x1B; break; // Ú

		// Danish
		case 0xC6: *p = 0x15; break; // Æ
		case 0xE6: *p = 0x14; break; // æ
		case 0xF8: *p = 0x16; break; // ø
		case 0xD8: *p = 0x17; break; // Ø
			
		// language specific - mostly (only?) used in German/Dutch - time will tell
		case 0xC4: *p = 0x5B; break; // Ä
		case 0xD6: *p = 0x5C; break; // Ö
		case 0xDC: *p = 0x5D; break; // Ü
		case 0xE4: *p = 0x7B; break; // ä
		case 0xF6: *p = 0x7C; break; // ö
		case 0xFC: *p = 0x7D; break; // ü
			
		// Special case: YLE Teksti-TV (É), ZDF (§), ORF (§), Forum64(§) 0x40-> 0x10 (@)
		case 0x40:
			if ((i == STATION_YLETEKSTITV)
					| (i == STATION_HBNTEKSTITV)
					|| (i == STATION_FORUM64)
					|| (i == STATION_TELETEKSTARCHIEF)
					|| (i >= STATION_ZDFTEXT && i <= STATION_3SAT)
					|| (i >= STATION_ORF1 && i <= STATION_ORFSPORT)) {
					*p = 0x10; break;
			}
	}
}

// Parses one byte of the header (=everything before <PRE>).
// NOS-TT header structure:
// pn=p_	-> previous page
// pn=n_	-> next page
// pn=ps_	-> previous subpage
// pn=ns_	-> next subpage
// ct=n		-> cycle time: auto-advance to the next subpage after n seconds
// ftl=		-> Fast Text Link 1 - red
// ftl=		-> Fast Text Link 2 - green
// ftl=		-> Fast Text Link 3 - yellow
// ftl=		-> Fast Text Link 4 - cyan
// lnk=		-> linked pagenumber
static char phb_buffer[40];
static char phb_buffer_index = 0;

// exported: used by process_page_bytes.s
signed char parse_header_byte(int intchar) {
	char *p;
	int tempVal;

	if (intchar != ASCII_LINE_FEED) {
		if (phb_buffer_index < (sizeof(phb_buffer) - 1)) {
			phb_buffer[phb_buffer_index++] = intchar;
		} else {
			phb_buffer_index = 0;
		}
		return 0;
	}

	phb_buffer[phb_buffer_index] = '\0';
	phb_buffer_index = 0;
	ascii_to_petscii(phb_buffer);

	// check for not found response
	p = strstr(phb_buffer, "404");
	if (p != NULL) {
		// 404 can also be a valid pagenumber and occur in pn=/ftl=/lnk=, so we have to rule these out
		if (strncmp(phb_buffer, "pn=", 3) != 0 && strncmp(phb_buffer, "ftl", 3) != 0 && strncmp(phb_buffer, "lnk=", 4) != 0) {
			return -1;
		}
	}

	// 0x5F -> underscore
	// pn = pagenumber; p_ = previous page / n_ = next page
	p = strstr(phb_buffer, "pn=p\x5F"); if (p) { p += 5; PN_P = atoi(p); }
	p = strstr(phb_buffer, "pn=n\x5F"); if (p) { p += 5; PN_N = atoi(p); }
	// previous subpages (ps)
	p = strstr(phb_buffer, "pn=ps");
	if (p) {
		p += 9;
		PN_PS = atoi(p);
		// override default cycle time if the user set a value other than 0
		cycle_time = config.cycletime > 0 ? config.cycletime : DEFAULT_CYCLE_TIME;
	}
	// next subpages (ns)
	p = strstr(phb_buffer, "pn=ns");
	if (p) {
		p += 9;
		tempVal = atoi(p);
		if (PN_NS == 0xFF) PN_NS = 0;// there actually is a subpage; so set the FF to 0 one time
		if (tempVal > PN_NS ) {
			PN_NS = tempVal;
			lastSubPageNumber = PN_NS;
			// override default cycle time if the user set a value other than 0
			cycle_time = config.cycletime > 0 ? config.cycletime : DEFAULT_CYCLE_TIME;
		}
	}
	// ftl = fastext link (always 4 or none)
	p = strstr(phb_buffer, "ftl");
	if (p) {
		char *slot = NULL;
		char *q;
		unsigned char idx = 0;
		p += 4;
		if      (FTL[0][0] == '\0') { slot = FTL[0]; idx = 0; }
		else if (FTL[1][0] == '\0') { slot = FTL[1]; idx = 1; }
		else if (FTL[2][0] == '\0') { slot = FTL[2]; idx = 2; }
		else if (FTL[3][0] == '\0') { slot = FTL[3]; idx = 3; }
		if (slot) {
			strncpy(slot, p, 3); slot[3] = '\0';
			for (q = slot; *q; q++) {
				// trim at first non-alphanumeric (space, newline, CR, ...)
				if ((*q < '0' || *q > '9') && (*q < 'A' || *q > 'Z') && (*q < 'a' || *q > 'z')) {
					*q = '\0'; break;
				}
				// petscii/ascii lower/uppercase conversion
				if (*q >= 'a' && *q <= 'z') *q += 0x80;
			}
			// Optional "ftl=xxx,cc,ww" position suffix
			if (p[3] == ',') {
				FTL_col[idx]   = (p[4] - '0') * 10 + (p[5] - '0');
				FTL_width[idx] = (p[7] - '0') * 10 + (p[8] - '0');
			}
		}
	}
	// lnk=nnn,rr,cc,h where nnn = page number (100..899), rr = row (00-24), cc = column (00-39), h = double-height flag (0/1)
	p = strstr(phb_buffer, "lnk=");
	if (p && LNK_count < LNK_MAX) {
		p += 4;
		LNK_page[LNK_count] = atoi(p);
		LNK_row[LNK_count] = (p[4] - '0') * 10 + (p[5] - '0');
		LNK_col[LNK_count] = (p[7] - '0') * 10 + (p[8] - '0');
		LNK_dh[LNK_count] = (p[10] - '0');
		LNK_count++;
	}
	// ct = cycle time for auto rotating subpages
	p = strstr(phb_buffer, "ct=");
	if (p) {
		p += 3;
		cycle_time = atoi(p);
		// override minimum cycle time is user set a value other than 0
		if (cycle_time > 0 && config.cycletime > 0) {
			cycle_time = MAX(cycle_time, config.cycletime);
		}
	}
	return 0;
}

static char pp_LPageNum[5];	// static: assembly uses C helpers to access

// all exported and used by process_page_bytes.s
char pp_pre_found;
char pp_row, pp_column;
unsigned char pending_key = 0;	// keypress captured during rendering
char pp_FgColor, pp_BgColor;
char pp_is_graphics_mode, pp_is_sep_mosaic;
char pp_hold_graphic, pp_held_char;
char pp_is_dh, pp_first_zero, pp_conceal, pp_flash;
int  pp_numChars;

// all exported and used by process_page_bytes.s
unsigned char blit_chars[40];
unsigned char blit_colors[40];
unsigned char blit_fg_packed[40];
unsigned char blit_is_dh[40];
unsigned char blit_conc[40];
unsigned char blit_fl[40];
unsigned char row_has_dh;
unsigned char row_has_content;
unsigned char force_fallback;

void init_page_processor(const char *lPageNum) {
	strcpy(pp_LPageNum, lPageNum);
	pp_pre_found = false;
	phb_buffer_index = 0;
	pp_row = 0;
	pp_column = 0;
	pp_FgColor = 0;
	pp_BgColor = 0;
	pp_is_graphics_mode = false;
	pp_is_sep_mosaic = false;
	pp_hold_graphic = false;
	pp_held_char = 0;
	pp_is_dh = false;
	pp_first_zero = false;
	pp_conceal = false;
	pp_flash = false;
	pp_numChars = 0;
	FTL[0][0] = FTL[1][0] = FTL[2][0] = FTL[3][0] = '\0'; // FTL holds fastext linked pagenumbers
	FTL_width[0] = FTL_width[1] = FTL_width[2] = FTL_width[3] = 0;
	LNK_count = 0;
	LNK_selected = -1;	// no LNK highlighted until CURSOR RIGHT is pressed
	cycle_jiffies = get_jiffies();
	reset_autorefresh_timer();
	row_has_dh = false;
	row_has_content = false;
	force_fallback = false;
}

#ifdef UCIVERSION
// Returns: 0=continue, 1=page complete (1000 chars), -1=404
signed char process_page_byte(int intchar) {

	if (!pp_pre_found) {
		if (parse_header_byte(intchar) == -1) {
			txt_draw_string(pp_LPageNum, PAGENUMBER_X, 0, SCREEN_COLOR_RED, SCREEN_COLOR_BLACK);
			return -1;
		}
	}

	if (!pp_pre_found && is_bracket_close(intchar)) {
		pp_pre_found = true;
		clear_bitmap_screen();
		return 0;
	}

	if (!pp_pre_found) return 0;
	++pp_numChars;
	if (pp_numChars > 1000) return 1;
	if (pp_column == 0 && intchar == 0x00) pp_first_zero = true;
	if (pp_first_zero) intchar = 0x20;
	
	{
		// Snapshot of state as it stood before this byte's own case runs
		char was_graphics  = pp_is_graphics_mode;
		char held_or_space = (pp_hold_graphic && was_graphics) ? pp_held_char : 0x20;
		// applies new FG colour / double-height 'set-after'
		char new_fg = pp_FgColor;
		char new_dh = pp_is_dh;
		char is_control_code = true;

		switch (intchar) {
			// I have put the statements on one line for readability
			case TCC_BLACK_BACKGROUND: pp_BgColor = cg_color[TCC_ALPHA_BLACK]; intchar = held_or_space; break;
			case TCC_NEW_BACKGROUND: pp_BgColor = pp_FgColor; intchar = held_or_space; break;
			case TCC_HOLD_MOSAICS: pp_hold_graphic = true;  intchar = was_graphics ? pp_held_char : 0x20; break;
			case TCC_RELEASE_MOSAICS: pp_hold_graphic = false; intchar = 0x20; break;
			case TCC_CONTINUOUS_MOSAICS: pp_is_sep_mosaic = false; intchar = held_or_space; break;
			case TCC_SEPERATED_MOSAICS: pp_is_sep_mosaic = true;  intchar = held_or_space; break;
			case TCC_NORMAL_HEIGHT: new_dh = false; intchar = held_or_space; break;
			case TCC_DOUBLE_HEIGHT: new_dh = true;  intchar = held_or_space; break;
			//case TCC_DOUBLE_WIDTH: intchar = held_or_space; break;
			//case TCC_DOUBLE_SIZE: intchar = held_or_space; break;
			case TCC_FLASH: pp_flash = true;  intchar = held_or_space; break;
			case TCC_STEADY: pp_flash = false; intchar = held_or_space; break;
			case TCC_CONCEAL: pp_conceal = true; intchar = held_or_space; break;
			case TCC_STARTBOX: intchar = held_or_space; break;
			case TCC_ENDBOX: intchar = held_or_space; break;
			case TCC_ESC_GO_SWITCH: intchar = held_or_space; break;
			default:
				// alpha
				if (intchar < 0x08 || (intchar == TCC_ALPHA_ORANGE)) {
					if (intchar == TCC_ALPHA_ORANGE) {
						new_fg = SCREEN_COLOR_ORANGE;
					} else {
						new_fg = cg_color[intchar];
					}
					pp_is_graphics_mode = false;
					pp_conceal = false;
					intchar = held_or_space;
				} else if ((intchar > 0x0F) && (intchar < 0x18) || (intchar == TCC_MOSAIC_ORANGE)) { // mosaic
					if (intchar == TCC_MOSAIC_ORANGE) {
						new_fg = SCREEN_COLOR_ORANGE;
					} else {
						new_fg = cg_color[intchar - 0x10];
					}
					pp_conceal = false;
					pp_is_graphics_mode = true;
					intchar = held_or_space;
				} else {
					// genuinely a literal page-content byte, not a control code
					is_control_code = false;
				}
		}

		if (!is_control_code) {
			if (!pp_is_graphics_mode && (intchar >= 0x80 || intchar == 0x40)) {
				map_extended_ascii_to_txt_charset(&intchar, station_index);
			}
			// determine if intchar is a (seperated) mosaic and set offset accordingly
			if (intchar > 0x1F) {
				if (pp_is_graphics_mode) {
					if (((intchar >= 0x20) && (intchar <= 0x3F)) || ((intchar >= 0x60) && (intchar <= 0x7F))) {
						intchar += pp_is_sep_mosaic ? 0x60 : 0x80;
						pp_held_char = intchar;
					}
				} else {
					pp_held_char = 0x20;
				}
			}
		}

		blit_chars[pp_column] = intchar;
		blit_fg_packed[pp_column] = (pp_FgColor << 4) | pp_BgColor;
		blit_is_dh[pp_column] = pp_is_dh;
		blit_conc[pp_column] = pp_conceal;
		blit_fl[pp_column] = pp_flash;
		if (pp_is_dh) row_has_dh = true;

		if (pp_conceal || pp_flash) {
			unsigned int _si = row_screen_offset[pp_row] + pp_column;
			blit_colors[pp_column] = MIRROR_NIBBLE(pp_BgColor);
			SCREEN_RAM2[_si] = fg_shifted[pp_FgColor] | pp_BgColor;
			if (pp_conceal) SCREEN_CONCEAL[_si] = true;
			if (pp_flash) SCREEN_FLASH[_si] = true;
			row_has_content = true;
		} else {
			blit_colors[pp_column] = fg_shifted[pp_FgColor] | pp_BgColor;
			if (!((intchar == 0x20) && (pp_BgColor == 0x00))) row_has_content = true;
		}

		// Commit pending set after attributes, so new_fg/new_dh become active starting with the next column
		pp_FgColor = new_fg;
		pp_is_dh = new_dh;
	}

	++pp_column;

	if (pp_column == 40) {
		if (row_has_dh) {
			unsigned char _c;
			for (_c=0; _c<40; _c++) {
				if (!((blit_chars[_c] == 0x20) && ((blit_fg_packed[_c] & 0x0F) == 0x00)))
					txt_draw_char(blit_chars[_c], _c, pp_row,
					              blit_fg_packed[_c] >> 4,
					              blit_fg_packed[_c] & 0x0F,
					              blit_is_dh[_c], blit_conc[_c], blit_fl[_c]);
				if (!blit_is_dh[_c]) {
					unsigned int _si = row_screen_offset[pp_row+1] + _c;
					if (blit_conc[_c] || blit_fl[_c]) {
						unsigned char _bg = blit_fg_packed[_c] & 0x0F;
						SCREEN_RAM[_si] = MIRROR_NIBBLE(_bg);
						SCREEN_RAM2[_si] = blit_fg_packed[_c];
						if (blit_conc[_c]) SCREEN_CONCEAL[_si] = true;
						if (blit_fl[_c]) SCREEN_FLASH[_si] = true;
					} else {
						SCREEN_RAM[_si] = blit_fg_packed[_c];
					}
				}
			}
		} else if (!force_fallback && row_has_content) {
			blit_row(pp_row, blit_chars, blit_colors);
		}
		
		force_fallback = row_has_dh && !force_fallback;
		row_has_dh = false;
		row_has_content = false;
		pp_column = 0;
		++pp_row;
		pp_FgColor = cg_color[TCC_ALPHA_WHITE];
		pp_BgColor = TCC_ALPHA_BLACK;
		pp_is_dh = false;
		pp_is_sep_mosaic = false;
		pp_is_graphics_mode = false;
		pp_hold_graphic = false;
		pp_held_char = 0x20;
		pp_first_zero = false;
		pp_conceal = false;
		pp_flash = false;
		
		// Check for keypress at each row boundary (~25x per page, ~4 jiffies max lag)
		if (kbhit()) {
			pending_key = cgetc();
			return 2;
		}
	}

	if ((pp_row == 2) && (pp_column == 0)) {
		if (cycle_paused) {
			update_cycle_stop_cont_indicator();
		} else {
			txt_draw_string(pp_LPageNum, PAGENUMBER_X, 0, 1, 0);
		}
	}

	return 0;
}
#endif

void finalize_page(void) {
	int i;
	for (i=0; i<1000; ++i) {
		if (SCREEN_CONCEAL[i]) {
			txt_draw_char(TCC_CONCEAL_GRAPHIC, 5, 0, SCREEN_COLOR_GREEN, SCREEN_COLOR_BLACK, false, false, false);
			txt_draw_char(TCC_CONCEAL_GRAPHIC+1, 6, 0, SCREEN_COLOR_GREEN, SCREEN_COLOR_BLACK, false, false, false);
			break;
		}
	}
	is_conceal = true;
}

// C helpers called from process_page_bytes.s
#if !defined(UCIVERSION)
unsigned char map_extended_char(unsigned char c) {
	int tmp = c;
	
	map_extended_ascii_to_txt_charset(&tmp, station_index);
	return tmp;
}
#endif

#if !defined(UCIVERSION)
void flush_normal_row(void) {
	if (!force_fallback && row_has_content)
		blit_row(pp_row, blit_chars, blit_colors);
}
#endif

#if !defined(UCIVERSION)
void flush_dh_row(unsigned char row) {
	unsigned char _c;
	
	for (_c=0; _c<40; _c++) {
		if (!((blit_chars[_c] == 0x20) && ((blit_fg_packed[_c] & 0x0F) == 0x00)))
			txt_draw_char(blit_chars[_c], _c, row,
			              blit_fg_packed[_c] >> 4,
			              blit_fg_packed[_c] & 0x0F,
			              blit_is_dh[_c], blit_conc[_c], blit_fl[_c]);
		if (!blit_is_dh[_c]) {
			unsigned int _si = row_screen_offset[row+1] + _c;
			if (blit_conc[_c] || blit_fl[_c]) {
				unsigned char _bg = blit_fg_packed[_c] & 0x0F;
				SCREEN_RAM[_si] = MIRROR_NIBBLE(_bg);
				SCREEN_RAM2[_si] = blit_fg_packed[_c];
				if (blit_conc[_c]) SCREEN_CONCEAL[_si] = true;
				if (blit_fl[_c]) SCREEN_FLASH[_si] = true;
			} else {
				SCREEN_RAM[_si] = blit_fg_packed[_c];
			}
		}
	}
}
#endif

#if !defined(UCIVERSION)
void write_conceal_flash_cell(unsigned char col) {
	unsigned int _si = row_screen_offset[pp_row] + col;
	
	blit_colors[col] = MIRROR_NIBBLE(pp_BgColor);
	SCREEN_RAM2[_si] = fg_shifted[pp_FgColor] | pp_BgColor;
	if (pp_conceal) SCREEN_CONCEAL[_si] = true;
	if (pp_flash) SCREEN_FLASH[_si] = true;
	row_has_content = 1;
}
#endif

#if !defined(UCIVERSION)
void lpage_draw_row2(void) {
	if (cycle_paused) {
		update_cycle_stop_cont_indicator();
	} else {
		txt_draw_string(pp_LPageNum, PAGENUMBER_X, 0, 1, 0);
	}
}
#endif

#if !defined(UCIVERSION)
void lpage_draw_404(void) {
	txt_draw_string(pp_LPageNum, PAGENUMBER_X, 0, SCREEN_COLOR_RED, SCREEN_COLOR_BLACK);
}
#endif

unsigned char distance(signed char x, signed char x1) {
	return (x > x1) ? (x - x1) : (x1 - x);
}

// dir: +1 = next (down/right), -1 = prev (up/left)
unsigned char lnk_next(signed char idx, bool vertical, signed char dir) {
	unsigned char i, best_idx, row_diff, col_diff, wrap_row, min_col_dist;
	unsigned int min_dist, total_dist;

	// none selected -> choose first item (dir>0) or last item (dir<0)
	if (idx == -1) return (dir > 0) ? 0 : LNK_count - 1;

	best_idx = idx;
	min_dist = 0xFFFFU;

	if (vertical) {
		// Find closest item in the given row direction. I use Weighted Manhattan Distance; calculating a real
		// vector is the better and more precise method but also a bit more complicated.
		if (dir > 0) {
			for (i=idx+1; i<LNK_count; i++) {
				if (LNK_row[i] > LNK_row[idx]) {
					row_diff = LNK_row[i] - LNK_row[idx];
					col_diff = distance(LNK_col[idx], LNK_col[i]);
					total_dist = (row_diff * 2) + col_diff;
					if (total_dist < min_dist) {
						min_dist = total_dist;
						best_idx = i;
					}
				}
			}
		} else {
			for (i=idx; i>0; ) {
				i--;
				if (LNK_row[i] < LNK_row[idx]) {
					row_diff = LNK_row[idx] - LNK_row[i];
					col_diff = distance(LNK_col[idx], LNK_col[i]);
					total_dist = (row_diff * 2) + col_diff;
					if (total_dist < min_dist) {
						min_dist = total_dist;
						best_idx = i;
					}
				}
			}
		}
		if (min_dist != 0xFFFFU) {
			return best_idx;
		}
		// Wrap around to the first/last row depending on direction. If nothing was found find the closest match to the current column on that row.
		wrap_row = (dir > 0) ? LNK_row[0] : LNK_row[LNK_count-1];
		min_col_dist = 0xFF;
		for (i = 0; i < LNK_count; i++) {
			if (LNK_row[i] == wrap_row) {
				col_diff = distance(LNK_col[idx], LNK_col[i]);
				if (col_diff < min_col_dist) {
					min_col_dist = col_diff;
					best_idx = i;
				}
			}
		}
		return best_idx;
	} else {
		if (dir > 0) {
			// if we can pick the pagenumber to the right on the same row
			if (idx+1 < LNK_count && LNK_row[idx] == LNK_row[idx+1]) return idx+1;
			// find the first pagenumber below and to the right
			for (i=idx+1; i < LNK_count; i++) {
				if (LNK_row[i] > LNK_row[idx] && LNK_col[i] > LNK_col[idx]) return i;
			}
		} else {
			// if we can pick the pagenumber to the left on the same row
			if (idx > 0 && LNK_row[idx] == LNK_row[idx-1]) return idx-1;
			// find the first pagenumber above and to the left
			for (i=idx; i>0; ) {
				i--;
				if (LNK_row[i] < LNK_row[idx] && LNK_col[i] < LNK_col[idx]) return i;
			}
		}
		return idx; // stay put if no match in that direction was found
	}
}

static void lnk_paint_cells(unsigned int si, unsigned char *saved, unsigned char width, unsigned char on) {
	unsigned char i;

	if (on) {
		for (i=0; i<width; i++) {
			saved[i] = SCREEN_RAM[si+i];
			// The highlight colour combo depends on the background colour; default white on purple. If the background colour is red
			// then it is reverseD to purple on white
			if ((SCREEN_RAM[si+i] & 0x0F) == SCREEN_COLOR_RED) {
				SCREEN_RAM[si+i] = fg_shifted[SCREEN_COLOR_PURPLE] | SCREEN_COLOR_WHITE;
			} else {
				SCREEN_RAM[si+i] = fg_shifted[SCREEN_COLOR_WHITE] | SCREEN_COLOR_PURPLE;
			}
		}
	} else {
		for (i=0; i<width; i++) SCREEN_RAM[si + i] = saved[i];
	}
}

// Linked pages paint: highlights pagenumbers
static void lnk_paint(unsigned char idx, unsigned char on) {
	unsigned int si = row_screen_offset[LNK_row[idx]] + LNK_col[idx];

	lnk_paint_cells(si, LNK_saved, 3, on);
	// double height number? also paint bottom half
	if (LNK_dh[idx]) {
		unsigned int si2 = row_screen_offset[LNK_row[idx] + 1] + LNK_col[idx];
		lnk_paint_cells(si2, LNK_saved + 3, 3, on);
	}
}

static void ftl_jump(unsigned char idx, int *currentPageNumber, int *currentSubpageNumber) {
	char ftl[4];
	char *q;
	bool is_num = true;

	strcpy(ftl, FTL[idx]);
	if (ftl[0] == '\0') return;
	for (q=ftl; *q; q++) {
		if (*q < '0' || *q > '9') {
			is_num = false;
			break;
		}
	}
	push(*currentPageNumber);
	*currentSubpageNumber = 0;
	reset_prev_next();
	if (is_num) {
		*currentPageNumber = atoi(ftl);
		get_teletext_page(*currentPageNumber, 0);
	} else {
		get_teletext_page_str(ftl, 0); // alphanumeric FTL, something like 10A: fetch directly, leaves currentPageNumber intact
	}
}

#ifdef MOUSESUPPORT
#include <mouse.h>
#include <modload.h>

// mouse_cb.s
extern const struct mouse_callbacks mouse_tt_callbacks;
extern void mouse_tt_cursor_init(void);

extern unsigned char *fetch_mouse_driver(unsigned int *out_len);

static bool mouse_driver_buffer_owned = false;
static bool mouse_available = false;
static bool mouse_btn_was_down = false;
static signed char FTL_hover = -1;
static unsigned char FTL_saved[40];	// saved SCREEN_RAM bytes under the current highlight, for restore full row width

static void ftl_paint(unsigned char idx, unsigned char on) {
	unsigned int si = row_screen_offset[24] + FTL_col[idx]; // Fastext links always on row 24
	lnk_paint_cells(si, FTL_saved, FTL_width[idx], on);
}

static unsigned char *mouse_drv_stage;
static unsigned int mouse_drv_stage_len;
static unsigned int mouse_drv_stage_pos;

static int __fastcall__ mouse_drv_read(int callerdata, void *buffer, unsigned count) {
	unsigned int avail = mouse_drv_stage_len - mouse_drv_stage_pos;
	(void)callerdata;
	if (count > avail) count = avail;
	memcpy(buffer, mouse_drv_stage + mouse_drv_stage_pos, count);
	mouse_drv_stage_pos += count;
	return count;
}

static bool mouse_driver_load(void) {
	struct mod_ctrl ctrl;

	mouse_drv_stage = fetch_mouse_driver(&mouse_drv_stage_len);
	if (!mouse_drv_stage) return false;
	mouse_drv_stage_pos = 0;
	ctrl.read = mouse_drv_read;
	ctrl.callerdata = 0;

	if (mod_load(&ctrl) == MLOAD_OK) {
		if (mouse_install((const struct mouse_callbacks*)&mouse_tt_callbacks, ctrl.module) == MOUSE_ERR_OK) {
			mouse_available = true;
		} else {
			mod_free(ctrl.module);
		}
	}

	if (mouse_driver_buffer_owned) {
		free(mouse_drv_stage);
		mouse_drv_stage = NULL;
	}

	return mouse_available;
}

#if defined(MLFVERSION)
void mouse_hide_if_active(void) { if (mouse_available) mouse_hide(); }
#endif
#if defined(MLFVERSION)
void mouse_show_if_active(void) { if (mouse_available) mouse_show(); }
#endif

bool mouse_raw_poll(unsigned char *out_row, unsigned char *out_col, unsigned char *out_clicked) {
	struct mouse_info info;
	static unsigned char buttons_prev = 0;
	unsigned char buttons;

	if (!mouse_available) return false;

	mouse_info(&info);
	if (info.pos.x < 0) info.pos.x = 0;
	if (info.pos.y < 0) info.pos.y = 0;
	*out_col = info.pos.x >> 3;
	*out_row = info.pos.y >> 3;
	if (*out_col > 39) *out_col = 39;
	if (*out_row > 24) *out_row = 24;

	buttons = ((info.buttons & MOUSE_BTN_LEFT) ? CLICK_LEFT : 0) | ((info.buttons & MOUSE_BTN_RIGHT) ? CLICK_RIGHT : 0);
	*out_clicked = buttons & ~buttons_prev;
	buttons_prev = buttons;
	return true;
}

static void mouse_cursor_start(void) {
	static const struct mouse_box box = {0, 0, 319, 199};

	if (!mouse_available) return;

	mouse_tt_cursor_init();
	mouse_setbox(&box);
	mouse_move(160, 100);
}

// MM=Mouse Menu: 1 menu icon top left + 5 clickable menu icons
#define MM_IS_TOGGLE(r,c) ((r) == 0 && (c) == 0)
// Slot number (1..5) if (r,c) is one of the item cells, else -1.
#define MM_ITEM_SLOT(r,c) (((c) == 0 && (r) >= 1 && (r) <= 5) ? (signed char)(r) : (signed char)-1)

// Blank h cells of column 0, starting at row
static void mm_blank(unsigned char row, unsigned char h) {
	blit_f_col   = 0;
	blit_f_row   = row;
	blit_f_w     = 1;
	blit_f_h     = h;
	blit_f_color = fg_shifted[SCREEN_COLOR_BLACK] | SCREEN_COLOR_BLACK;
	blit_fill_rect_asm();
}

static void mouse_poll(int *currentPageNumber, int *currentSubpageNumber) {
	struct mouse_info info;
	unsigned char row, col, i;
	bool btn_down, rbtn_down;
	static unsigned char last_row = 0xFF, last_col = 0xFF;
	static bool mouse_rbtn_was_down = false;
	static bool mouse_cursor_shown = false;
	static bool mouse_menu_open = false;
	// Where the pointer was at the first poll; only the low byte of x is kept (y is always < 200, so 0xFF=not taken yet)
	static unsigned char rest_x = 0, rest_y = 0xFF;
	static int prev_x = -1, prev_y = -1;
	static unsigned int last_move_jiffies;
	static bool mouse_icon_hidden = false;
	
	if (!mouse_available) return;
	
	mouse_info(&info);
	
	// The pointer stays hidden until the mouse is moved
	if (!mouse_cursor_shown) {
		if (rest_y == 0xFF) {
			rest_x = info.pos.x;
			rest_y = info.pos.y;
			return;
		}
		if ((unsigned char)info.pos.x == rest_x && (unsigned char)info.pos.y == rest_y) return;
		mouse_cursor_shown = true;
		mouse_show();
	}
	
	// mouse menu icon hides after 3 seconds without pointer movement
	if (info.pos.x != prev_x || info.pos.y != prev_y) {
		prev_x = info.pos.x;
		prev_y = info.pos.y;
		last_move_jiffies = (unsigned int)get_jiffies();
		mouse_icon_hidden = false;
	} else if (!mouse_icon_hidden && !mouse_menu_open && (unsigned int)get_jiffies() - last_move_jiffies >= 3 * JIFFIES_PER_SECOND) {
		mouse_icon_hidden = true;
		mm_blank(0, 1);
	}
	if (!mouse_icon_hidden) menu_draw_char(0x09, 0, 0, SCREEN_COLOR_BLACK, SCREEN_COLOR_GRAY_3);
	if (mouse_menu_open) {
		static const unsigned char item_glyph[5] = { 0x0A, 0x1C, 0x1D, 0x1E, 0x1F }; // references to positions of the mouse menu icons in the font
		signed char hover = MM_ITEM_SLOT(last_row, last_col);
		unsigned char s;
		for (s=1; s<=5; s++) {
			if (s == hover)
				menu_draw_char(item_glyph[s-1], 0, s, SCREEN_COLOR_BLACK, SCREEN_COLOR_LIGHT_GREEN);
			else
				menu_draw_char(item_glyph[s-1], 0, s, SCREEN_COLOR_BLACK, SCREEN_COLOR_GRAY_3);
		}
	}
	
	if (info.pos.x < 0) info.pos.x = 0;
	if (info.pos.y < 0) info.pos.y = 0;
	col = info.pos.x >> 3;
	row = info.pos.y >> 3;
	if (col > 39) col = 39;
	if (row > 24) row = 24;
	
	if (row != last_row || col != last_col) {
		signed char hit = -1;
		bool new_menu_open;
		
		last_row = row;
		last_col = col;
		
		// Menu opens on hover over the toggle, stays open while the cursor
		// is anywhere on the item strip, and closes the moment it leaves
		// both - no click needed. Item icons just draw over whatever's
		// there on open (like the toggle icon always has); closing blanks
		// them back out.
		new_menu_open = mouse_cursor_shown &&
		(MM_IS_TOGGLE(row, col) || (mouse_menu_open && MM_ITEM_SLOT(row, col) >= 0));
		if (mouse_menu_open && !new_menu_open) mm_blank(1, 5);
		mouse_menu_open = new_menu_open;
		
		// Hit-test (row,col) against the current page's link table
		if (!mouse_menu_open) {
			for (i=0; i<LNK_count; i++) {
				unsigned char link_row = LNK_row[i];
				if (row != link_row && !(LNK_dh[i] && row == (unsigned char)(link_row + 1))) continue;
				if (col >= LNK_col[i] && col < (unsigned char)(LNK_col[i] + 3)) { hit = i; break; }
			}
		}
		
		if (hit != LNK_selected) {
			if (LNK_selected >= 0) lnk_paint(LNK_selected, false);
			LNK_selected = hit;
			if (LNK_selected >= 0) lnk_paint(LNK_selected, true);
		}
		
		// Fastext hit-test: entirely separate from the LNK
		{
			signed char ftl_hit = -1;
			if (row == 24) {
				for (i=0; i<4; i++) {
					if (FTL_width[i] == 0) continue;
					if (col >= FTL_col[i] && col < (unsigned char)(FTL_col[i] + FTL_width[i])) { ftl_hit = i; break; }
				}
			}
			if (ftl_hit != FTL_hover) {
				if (FTL_hover >= 0) ftl_paint(FTL_hover, false);
				FTL_hover = ftl_hit;
				if (FTL_hover >= 0) ftl_paint(FTL_hover, true);
			}
		}
	}
	
	btn_down = (info.buttons & MOUSE_BTN_LEFT) != 0;
	if (btn_down && !mouse_btn_was_down) {
		if (LNK_selected >= 0 && LNK_selected < LNK_count) {
			push(*currentPageNumber);
			*currentPageNumber = LNK_page[LNK_selected];
			*currentSubpageNumber = 0;
			reset_prev_next();
			get_teletext_page(*currentPageNumber, *currentSubpageNumber);
		} else if (FTL_hover >= 0) {
			ftl_jump(FTL_hover, currentPageNumber, currentSubpageNumber);
		} else if (mouse_menu_open) {
			signed char slot = MM_ITEM_SLOT(row, col);
			if (slot >= 1 && slot <= 5) {
				static const unsigned char menu_keys[5] = { KEY_M, KEY_MINUS, KEY_PLUS, KEY_COMMA, KEY_DOT };
				pending_key = menu_keys[slot-1];
			}
		} else if (info.pos.x == 319 || info.pos.y == 199) {
			pending_key = KEY_DOT;
		}
	}
	mouse_btn_was_down = btn_down;

	// Right button: step back, same as Q/arrow up
	rbtn_down = (info.buttons & MOUSE_BTN_RIGHT) != 0;
	if (rbtn_down && !mouse_rbtn_was_down) {
		if (pop(currentPageNumber)) {
			*currentSubpageNumber = 0;
			reset_prev_next();
			get_teletext_page(*currentPageNumber, *currentSubpageNumber);
		}
	}
	mouse_rbtn_was_down = rbtn_down;
}
#endif

// Print feature excluded from the Meatloaf edition (for now). Meatloaf itself creates a printer as IEC device #4 which is not working as far as I know
#ifndef MLFVERSION
#define PRINT_SUPPORT
#endif

#ifdef PRINT_SUPPORT
// Notes:
// - Only supports Commodore MPS-801 compatible printers on IEC device 4
// - Prints black and white with dithering
// - Every print ends with a form feed
// - Meatloaf edition currently has no print support, here is why:
// 		jamie idolpx on discord:
// 		"It was working at some point. The printout was saved to the sd card as a PDF. It has been a while since I touched it. Check the console for info
// 		while printing. That will give more info as to what is happening. Or not happening."
//		-> the console didn't report anything on my device

#include <cbm.h>

// Colour printing is switched off to save memory (around 500 bytes)
// To switch it on: set PRINT_COLOUR to 1 here and PRINT_COLOUR to 1 in print_page.s
#define PRINT_COLOUR				0

#define PRINTER_LFN					4
#define PRINTER_DEVICE			4
#define PRINT_BANDS					50		// ceil(25 * 14 / 7); 58 when PRINT_STRETCH in print_page.s is 16
#define PRINT_BUF						((unsigned char*)0x9E00)	// free CPU only RAM ($9E00-$9EFF)
#define PRINT_BUF_SIZE			256
#define VIC_SPRITE_ENABLE		(*(unsigned char*)0xD015)
#define PRINT_PASS_SIZE			317		// ESC header (3) + 39 cells x 8 columns + CHR$(15) CHR$(141)
#define PRINT_SKIP_COLUMNS	8			// screen column 0 = 8 printed columns: computed, but not sent in the colour print

#define PRINT_CBUF	((unsigned char*)0x9E00)

extern unsigned char print_cells_n;
extern unsigned char *print_dest;
void print_rows_init(void);
void print_band_prepare(void);
void print_cells(void);
#if PRINT_COLOUR
extern unsigned char print_ink;
extern unsigned char print_any;
void print_band_rewind(void);
void print_cells_c(void);
#endif
// see print_page.s

static bool print_send(unsigned char n) {
	return cbm_write(PRINTER_LFN, PRINT_BUF, n) == n;
}

static bool print_band(void) {
	print_band_prepare();
	PRINT_BUF[0] = 8;							// CHR$(8): bit image mode
	print_dest = PRINT_BUF + 1;
	print_cells_n = 31;						// 31 cell columns (248 bytes) ...
	print_cells();
	if (!print_send(1 + 31 * 8)) return false;
	print_dest = PRINT_BUF;
	print_cells_n = 9;						// ... and the last 9 (72 bytes)
	print_cells();
	PRINT_BUF[9 * 8] = 13;				// CHR$(13): print the line
	if (!print_send(9 * 8 + 1)) return false;
	if (kbhit()) {
		cgetc();
		return false;
	}
	return true;
}

#if PRINT_COLOUR
// one pass per ink colour
static bool print_band_colour(void) {
	static const unsigned char ink_code[4] = { 0x62, 0x63, 0x6D, 0x79 };	// ASCII b, c, m, y (not PETSCII)
	unsigned char ink;

	print_band_prepare();
	print_dest = PRINT_CBUF + 3;
	for (ink=0; ink<4; ++ink) {
		if (ink) print_band_rewind();
		print_ink = 1 << ink;
		print_cells_c();
		if (!print_any) continue;
		// All 40 cells were computed into PRINT_CBUF+3.., but the header goes directly in front of cell 1, so the 8 columns of cell 0 are left out of what is sent
		PRINT_CBUF[PRINT_SKIP_COLUMNS] = 8;										// CHR$(8): bit image mode
		PRINT_CBUF[PRINT_SKIP_COLUMNS + 1] = 27;							// ESC b/c/m/y: ink
		PRINT_CBUF[PRINT_SKIP_COLUMNS + 2] = ink_code[ink];
		PRINT_CBUF[323] = 15;																	// CHR$(15): leave bit image mode
		PRINT_CBUF[324] = 141;																// CHR$(141): back to the line start, no line feed
		if (cbm_write(PRINTER_LFN, PRINT_CBUF + PRINT_SKIP_COLUMNS, PRINT_PASS_SIZE) != PRINT_PASS_SIZE) return false;
	}
	PRINT_CBUF[0] = 8;																			// bit image line pitch, then the line feed
	PRINT_CBUF[1] = 13;
	if (cbm_write(PRINTER_LFN, PRINT_CBUF, 2) != 2) return false;
	if (kbhit()) {
		cgetc();
		return false;
	}
	return true;
}
#endif

#if PRINT_COLOUR
static void print_page(bool colour) {
#else
static void print_page(void) {
#endif
	unsigned char band;
	unsigned char old_border = VIC_BORDER_COLOR;
	unsigned char old_sprites = VIC_SPRITE_ENABLE;
	bool ok = true;

	VIC_SPRITE_ENABLE = 0;	// sprite DMA would disturb IEC bus timing
	VIC_BORDER_COLOR = SCREEN_COLOR_YELLOW;		// let user know data is being sent to the printer
	if (cbm_open(PRINTER_LFN, PRINTER_DEVICE, 0, "") != 0) {
		ok = false;
	} else {
		print_rows_init();
#if PRINT_COLOUR
		for (band=0; band<PRINT_BANDS && ok; ++band) ok = colour ? print_band_colour() : print_band();
#else
		for (band=0; band<PRINT_BANDS && ok; ++band) ok = print_band();
#endif
		PRINT_BUF[0] = 12;												// CHR$(12): form feed
		memset(PRINT_BUF + 1, 0, 255);						// 255 NULs is enough to fill the chunk wherever the form feed lands in it
		cbm_write(PRINTER_LFN, PRINT_BUF, 256);
		cbm_close(PRINTER_LFN);
	}
	VIC_SPRITE_ENABLE = old_sprites;
	if (!ok) {		// printer not found / error / aborted
		VIC_BORDER_COLOR = SCREEN_COLOR_RED;
		delay_jiffies(40);
	}
	VIC_BORDER_COLOR = old_border;
}
#endif

void teletext_main_loop(int startPageNumber) {
	unsigned char c = 0;
	unsigned char station;
	int currentPageNumber = startPageNumber;
	int currentSubpageNumber = 0;
	int digit = 0;
	char str[5] = "p---";

	while (1) {
		if (refresh_txtpage()) get_teletext_page(currentPageNumber, currentSubpageNumber);
		if (cycle_subpage_due()) {
			if (currentSubpageNumber < PN_NS || PN_NS == 0) {
				currentSubpageNumber = (currentSubpageNumber == 0) ? 2 : currentSubpageNumber + 1;
			} else if (currentSubpageNumber == lastSubPageNumber) {
				currentSubpageNumber = 0;
			}
			get_teletext_page(currentPageNumber, currentSubpageNumber);
		}
		if ((get_jiffies() - flash_jiffies) > FLASH_SPEED) toggle_flash();
#ifdef MOUSESUPPORT
		mouse_poll(&currentPageNumber, &currentSubpageNumber);
#endif
		if (pending_key) {
			c = pending_key;
			pending_key = 0;
		} else {
			c = kbhit();
			if (c == 0) continue;
			c = cgetc();
		}
		if (c >= KEY_0 && c <= KEY_9) {
			digit = c - KEY_0;
			if (subpage_input_mode) {
				// re-use newPageNumber for entering subpage number
				if (newPageNumber < 10) {
					newPageNumber = newPageNumber * 10 + digit;
					++digitsEntered;
				}
				str[digitsEntered] = c;
				txt_draw_string(str, PAGENUMBER_X+4, 0, 1, 0); // subpage number comes after pagenumber
				if (digitsEntered == 2) {
					txt_draw_string("   ", PAGENUMBER_X+4, 0, 1, 0);
					currentSubpageNumber = newPageNumber;
					newPageNumber = 0;
					digitsEntered = 0;
					subpage_input_mode = false;
					str[1] = '-'; str[2] = '-'; str[3] = '-';
					get_teletext_page(currentPageNumber, currentSubpageNumber);
				}
			} else {
				if (digitsEntered == 0 && (c < '1' || c > '8')) continue;
				cycle_paused = true;
				if (digitsEntered == 0) strcpy(str, "P---");
				if (newPageNumber < 100) {
					newPageNumber = newPageNumber * 10 + digit;
					++digitsEntered;
				}
				str[digitsEntered] = c;
				txt_draw_string(str, PAGENUMBER_X, 0, 1, 0);
				if (newPageNumber > 99) {
					push(currentPageNumber);
					currentPageNumber = newPageNumber;
					currentSubpageNumber = 0;
					newPageNumber = 0;
					digitsEntered = 0;
					str[1] = '-'; str[2] = '-'; str[3] = '-';
					reset_prev_next();
					get_teletext_page(currentPageNumber, currentSubpageNumber);
				}
			}
		} else {
			switch (c) {
				case KEY_CLRHOME:
					push(currentPageNumber);
					currentPageNumber = config.startpage;
					currentSubpageNumber = 0;
					reset_prev_next();
					get_teletext_page(currentPageNumber, currentSubpageNumber);
					break;
				case KEY_MINUS:
					if (currentPageNumber > 100) {
						push(currentPageNumber);
						currentPageNumber = (PN_P == 0) ? currentPageNumber - 1 : PN_P;
						currentSubpageNumber = 0;
						reset_prev_next();
						get_teletext_page(currentPageNumber, currentSubpageNumber);
					}
					break;
				case KEY_EQUAL:
				case KEY_PLUS:
					if (currentPageNumber < 899) {
						push(currentPageNumber);
						currentPageNumber = (PN_N == 0) ? currentPageNumber + 1 : PN_N;
						currentSubpageNumber = 0;
						reset_prev_next();
						get_teletext_page(currentPageNumber, currentSubpageNumber);
					}
					break;
				case KEY_POUND:
					subpage_input_mode = true;
					strcpy(str, "/--");
					txt_draw_string(str, PAGENUMBER_X+4, 0, 1, 0); // subpage number comes after pagenumber
					break;
				case KEY_COMMA: //prev subpage
					if (currentSubpageNumber > 1) {
						--currentSubpageNumber;
						get_teletext_page(currentPageNumber, currentSubpageNumber);
					} else {
						currentSubpageNumber = lastSubPageNumber;
						get_teletext_page(currentPageNumber, currentSubpageNumber);
					}
					break;
				case KEY_DOT: // next subpage
					if (currentSubpageNumber < PN_NS || PN_NS == 0) {
						currentSubpageNumber = (currentSubpageNumber == 0) ? 2 : currentSubpageNumber + 1;
						get_teletext_page(currentPageNumber, currentSubpageNumber);
					} else if (currentSubpageNumber == lastSubPageNumber) {
						currentSubpageNumber = 0;
						get_teletext_page(currentPageNumber, currentSubpageNumber);
					}
					break;
				case KEY_W:
				case KEY_CRSR_UP:	// go to the row above and find the nearest pagelink number
					if (LNK_count > 0) {
						if (LNK_selected >= 0) lnk_paint(LNK_selected, false);
						LNK_selected = lnk_next(LNK_selected, true, -1);
						lnk_paint(LNK_selected, true);
					}
					break;
				case KEY_A:
				case KEY_CRSR_LEFT: 	// go to pagelink number to te left (if any)
					if (LNK_count > 0) {
						if (LNK_selected >= 0) lnk_paint(LNK_selected, false);
						LNK_selected = lnk_next(LNK_selected, false, -1);
						lnk_paint(LNK_selected, true);
					}
					break;
				case KEY_S:
				case KEY_CRSR_DOWN:	// go to the row below and find the nearest pagelink number
					if (LNK_count > 0) {
						if (LNK_selected >= 0) lnk_paint(LNK_selected, false);
						LNK_selected = lnk_next(LNK_selected, true, 1);
						lnk_paint(LNK_selected, true);
					}
					break;
				case KEY_D:
				case KEY_CRSR_RIGHT:	// go to pagelink number to te right (if any)
					if (LNK_count > 0) {
						if (LNK_selected >= 0) lnk_paint(LNK_selected, false);
						LNK_selected = lnk_next(LNK_selected, false, 1);
						lnk_paint(LNK_selected, true);
					}
					break;
				case KEY_E:
				case KEY_RETURN:	// Go to selected pagenumber
					if (LNK_selected >= 0 && LNK_selected < LNK_count) {
						push(currentPageNumber);
						currentPageNumber = LNK_page[LNK_selected];
						currentSubpageNumber = 0;
						reset_prev_next();
						get_teletext_page(currentPageNumber, currentSubpageNumber);
					}
					break;
#if defined(WIC64VERSION) && !defined(DISK_CONFIG)
				case KEY_ARROW_LEFT:
					wic64_enter_portal();
					break;
#endif
				case KEY_Q:
				case KEY_ARROW_UP:	// step back page
					if (pop(&currentPageNumber)) {
						currentSubpageNumber = 0;
						reset_prev_next();
						get_teletext_page(currentPageNumber, currentSubpageNumber);
					}
					break;
				case KEY_C:
					toggle_conceal();
					break;
				case KEY_SPACE:
					cycle_paused = !cycle_paused;
					update_cycle_stop_cont_indicator();
					break;
				case KEY_R: // refresh page
					get_teletext_page(currentPageNumber, currentSubpageNumber);
					break;
				case KEY_SLASH:			// select next station
				case KEY_QUESTION:	// select prev station
					if (c == KEY_SLASH) {
						station_index = (station_index < MAX_STATION_INDEX) ? ++station_index : 0;
					} else {
						station_index = (station_index > 0) ? --station_index : MAX_STATION_INDEX;
					}
					currentPageNumber = 100;
					currentSubpageNumber = 0;
					stack_pointer = -1;
					reset_prev_next();
					apply_language(stations[station_index].lang);
#if defined(WIC64VERSION)
					show_loading_screen();
#else
					clear_bitmap_screen();
#endif
					get_teletext_page(currentPageNumber, currentSubpageNumber);
					break;
				case KEY_M:
					station = menu_choose(station_index, menu_run);
					if (station != MENU_CANCEL) {
						station_index = station;
						currentPageNumber = 100;
						currentSubpageNumber = 0;
						stack_pointer = -1;
						reset_prev_next();
						apply_language(stations[station_index].lang);
					}
#if defined(WIC64VERSION)
					show_loading_screen();
#else
					clear_bitmap_screen();
#endif
					get_teletext_page(currentPageNumber, currentSubpageNumber);
					break;
				case KEY_T:
					show_loading_screen();
					toggle_font();
					get_teletext_page(currentPageNumber, currentSubpageNumber);
					break;
#ifdef PRINT_SUPPORT
#if PRINT_COLOUR
				case KEY_P:	 // b&w print
					print_page(false);
					break;
				case KEY_SHIFT_P: // colour print
					print_page(true);
					break;
#else
				case KEY_P:	 // b&w print
					print_page();
					break;
#endif
#endif
				case KEY_F1:
				case KEY_F3:
				case KEY_F5:
				case KEY_F7:
					ftl_jump(c - KEY_F1, &currentPageNumber, &currentSubpageNumber);
					break;
				case KEY_F4:
					stopwatch_enabled = !stopwatch_enabled;
					txt_draw_string(stopwatch_enabled ? "Stopwatch: ON  " : "Stopwatch: OFF ", 0, 24, SCREEN_COLOR_WHITE, SCREEN_COLOR_BLUE);
					break;
				case KEY_F8:
					station_index = STATION_TELETEXT64;
					currentPageNumber = 100;
					currentSubpageNumber = 0;
					stack_pointer = -1;
					reset_prev_next();
					apply_language(stations[station_index].lang);
#if defined(WIC64VERSION)
					show_loading_screen();
#else
					clear_bitmap_screen();
#endif
					get_teletext_page(currentPageNumber, currentSubpageNumber);
					break;
			}
		}
	}
}
