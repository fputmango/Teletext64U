# Teletext 64 Ultimate

Teletext program showing live pages from the internet for the Commodore 64 Ultimate and other Ultimate products that have networking capabilities like the Ultimate 64 and the Ultimate II+. The WiC64 and Meatloaf devices are also supported. 

The program also runs on a stock C64/C128 in C64 mode with a WiC64, a Meatloaf or an Ultimate II+ cartridge.

Teletext 64 Ultimate has its own dedicated server at petsciiproxy.nl, so you don't need to run a local server. The program is availalble on the WiC64 Portal and also runs in VICE emulator!

### Currently supporting:

| Service | Country |
| :--- | :--- |
|NOS Teletekst |The Netherlands |
|ARD Text |Germany |
|Ceefax |UK |
|Teefax |UK |
|YLE Teksti-TV |Finland |
|SVT Text |Sweden |
|ZDFtext, info, neo |Germany |
|3sat |Germany, Austria, Switzerland |
|DR Tekst-TV |Denmark |
|ORF 1, 2, III, Sport+ |Austria |
|Chunkytext |UK |
|Webfax 1 & 2 |UK |
|SPARK TVARK teletext |UK |
|WDR Text |Germany |
|hr-text |Germany |
|SWR Baden-Württemberg |Germany |
|SWR Rheinland-Pfalz |Germany |
|SRF 1, zwei, info |Switzerland, German |
|RTS 1 & 2 |Switzerland, French |
|RSI LA 1 & 2|Switzerland, Italian |
|NOS Nieuws |The Netherlands (RSS feed) |
|Forum64 |Germany (RSS feed) |
|BMN1 Bollentekst |The Netherlands |
|HBN Teksti-TV |Finland |
|MTVA Teletext |Hungary |
|Teletekstarchief |The Netherlands |
|NDR TEXT |Germany |
|Telegazeta TVP 1, 2, 3, Kultura, Historia, Sport and Polonia |Poland |
 
My mission: add as many Teletext services as possible. So let me know if you have any wishes.

## Installation
Read the PDF in the docs folder!

## Description
100% teletext compliant by using proper teletext character sets including the special graphic (mosaic) characters and support for the most common diacrits / accent marks. It runs in hires bitmap mode to support all the required foreground/background color combinations. To make the look & feel even more authentic, it can be used with a super bright RGB palette; so no washed out default C64 colors.

It uses the same internet feed as the mobile apps and websites. To make it feel even more like on TV, the header row is modified to show the actual date with a page number top left. Entering page numbers works like as on a remote. Just type 3 digits and the requested page will be fetched, if available. It also supports fastext, those four colored words you see at the bottom row. These are mapped to the C64 functions keys. And subpages auto-rotate after a given number of seconds like on TV. 

Although the C64U has excellent networking capabilities, it unfortunately is restricted to HTTP. That does not bring us very far with basically every site running secure HTTPS these days. So I wrote a PetsciiProxy program for PC/Mac/Linux/.. that handles the secure HTTPS connections.

## Feature list
- Auto 60 second refresh - always have the latest news on you screen, handy if you have your C64 hooked up to a separate screen; refresh time can be adjusted to your liking.
- Fastext support via function keys - resembling the red, green, yellow and cyan quick access keys from the TV-remote.
- Two custom hand crafted 6x6 pixel character sets (thin & bold) - this leaves room for 1 pixel row of background color at the top and bottom of each character. This leads to a much better overall looking teletext screen imo.
- Switch between the thin and bold fonts on the fly.
- If a requested page is not available online, it will show an older previous saved version of the page, if available. Handy for archival purposes.
- Set your favorite teletext station and start page with the config utility.
- Double height text, blinking text, hidden/concealed text that can be revealed.
- Auto rotating subpages with a configurable cycle time.
- Quick navigation – Using only the cursor/WASD keys, you can quickly move back and forth through pages without having to enter page numbers.
- User's Guide as a full Teletext service!
- Mouse support - click page numbers and fastext links, use the tiny mouse menu for more navigation options. And a Amiga Workbench 1.3 style mouse pointer.
- Print a Teletext page - Only Commodore MPS printers are supported.

## Tools used for this project
- Xcode, CC65 (C/asm Teletext64U), Visual Studio Code (Go language PetsciiProxy server), CharPad (character sets), spritemate.com (for drawing the mouse pointer) 

## Bio
I grew up with the Commodore 64 as a teenager in the 1980s and always have been a big teletext fan. Professionally I developed teletext software working for a small Dutch Delft based company in the early 1990s. They developed their own teletext hardware based around the Philips SAA5243 IC. Later, commissioned by Dutch television broadcaster NOS, I developed the *NOS Teletekst Browser* and screensaver for Windows to celebrate the 20th anniversary of Teletext in the year 2000. I also developed a teletext iPhone app around 2010. And now it's time to have some fun with making software for the C64.

## Credits and Licensing

### License
Teletext64U and PetsciiProxy are copyright (C) 2026 Frank Putman and licensed under the **GNU General Public License v3.0 or later** (`GPL-3.0-or-later`). The full text is in the [`LICENSE`](LICENSE) file.

The character sets (thin, bold, mosaic) and the mouse pointer sprites are my own work, drawn in CharPad and Spritemate, and are covered by the same licence.

The Ultimate (UCI) editions of Teletext64U incorporate GPL-3.0 code (see below), as is my whole project. The source code can be found in the Teletext64U and PetsciiProxy folders.

### Third-party components
Full licence texts are in [`THIRD-PARTY-NOTICES`](THIRD-PARTY-NOTICES).

* **[ultimateii-dos-lib](https://github.com/xlar54/ultimateii-dos-lib)** by Scott Hutter (xlar54) and Francesco Sblendorio, GPL-3.0. A modified, reduced copy is used for Ultimate command interface access in the UCI editions.
* **[WiC64 library](https://github.com/WiC64-Team/wic64-library)** by Henning Liebenau, 2-clause BSD. A reduced subset, rewritten for cc65, is used in the WiC64 editions.
* **[cc65](https://github.com/cc65/cc65)**, zlib licence. Compiler and runtime library; the 1351 mouse driver and the mouse callbacks are modified versions of cc65 sources.
* **PetsciiProxy** is built with the Go standard library and several Go modules (golang.org/x/net, go-git and their dependencies; BSD, MIT, Apache-2.0 and MPL-2.0), listed in `THIRD-PARTY-NOTICES`.
