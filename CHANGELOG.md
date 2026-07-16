# **Teletext64U** changelog

## [2.1.1] - 2026-07-16

### Added
- New station: SPARK - TVARK teletext, more info on https://tvark.org/
- PetsciiProxy new target: raspberrypi-1B - specifically for the Pi 1 and Pi Zero (W) models.

### Changed Teletext64U
- Loading / saving the config was bound to device 8. Now the device number is read at program start and used for loading and saving. Device range supported: 8 to 11. Thanks to Arndt for pointing me on this issue.
- Pagenumber top left is shifted 1 position to the right; aligns better with the teletext page.

### Fixed
- The WiC64 D64 version now stores the user preferences on disk again, like it used to. Note: the WiC64 stand alone PRG version is the one that is used on the WiC64 Portal and can also be run locally with going to the portal first.

### Notes 
- Updated Teletext64U Notes.pdf in docs folder.
- Teletext64WiC64.d64 is renamed to Teletext64W.d64.


## [2.1.0] - 2026-07-11

### Added
- Chunkytext, Webfax 1, Webfax 2 (UK)
- Added support for weird pagenumbers (like 10A) on Chunkytext. This means you can play Fun and Games on page 150! Note: I am aware that Teefax also uses weird pagenumbers; I have to look into that.


## [2.0.2] - 2026-07-09

### Changed Teletext64U
- WiC edition: backarrow key Loads and runs the WiC64 portal program. Implemented using https://github.com/WiC64-Team/wic64-library#wic64_enter_portal
- Uparrow key now steps back through previously visited pages (the key right to the *-key).
- Updated the help screen.


## [2.0.1] - 2026-07-07

### News
- Teletext64 is now hosted on the WiC portal under 'Apps'.

### Fixed
- The WiC64 edition now also runs within VICE emulating the WiC64.
- ZDF parser improved, thanks to feedback from a user on Forum64. 


## [2.0.0] - 2026-07-04

### New!
- Teletext64 now has its own dedicated, super-fast PetsciiProxy server at petsciiproxy.nl! No local proxy installation required.

### Changed Teletext64U
- WiC64 edition: This is a PRG-only version that always connects to petsciiproxy.nl. The CONFIG program and the UT64.CFG file have been removed from the D64 image and are no longer required. User settings are stored on the PetsciiProxy server.
- Ultimate edition: This version still includes the complete D64 image and stores user settings locally. The CONFIG program now includes a new option that lets users choose between the petsciiproxy.nl server and a locally running PetsciiProxy instance by entering its IP address and port.

### Changed PetsciiProxy
- DR Tekst-TV: applied cyan header/highlights on page 202

### Notes on the petsciiproxy.nl server
- Time zone on the server set to Europe/Amsterdam (CEST, +0200).
- YLE Teksti-TV is not supported yet, because of the personal API key. I'll have to implement a solution for this.


## [1.8.1] - 2026-06-30

### Added
- CONFIG program: Added the option to set the minimum cycle time (in seconds); 0 uses the default cycle time.
- CONFIG program: Added descriptive help text for each configuration item.


## [1.8.0] - 2026-06-27

### Added
- ORF 1, ORF 2, ORF III and ORF Sport+ (Austria), including auto rotating subpage support.
- Auto rotating subpage support for ARD (Germany), YLE Teksti-TV (Finland) and DR Tekst-TV (Denmark).

### Fixed Teletext64U
- Auto rotating pages is paused while typing a pagenumber.  
- Auto rotating pages: STOP indicator in header row always visible after manually going up / down a subpage.
- Adding ORF exposed a number of issues in the teletext engine. These have been fixed. 
- Local language character $40 (YLE: É, ZDF/ORF: §) now maps to @.
- Pressing fastext keys F1-F7 after a page not found (red pagenumber top left), no longer request page 000.

### Fixed PetsciiProxy
- ZDF: Fixed the weather map on pages 171 and 172: mosaics are now converted back to proper teletext spec (ETS 300 706) with seperated mosaics and hold mosaic control codes. This results in a map exactly like on TV. So no more black open gaps in the map.
- ZDF: Fixed row 2 issue banner / section.


## [1.7.0] - 2026-06-16

### Added
- Carousel support: subpages automatically rotate every 10 seconds, or after a specific cycle time set for a page (Ceefax/Teefax). Use the space bar to toggle STOP/CONTinue rotating (like on a TV-remote).
- Subpage navigation support is available for Ceefax, Teefax, NOS-TT, ZDF, 3SAT & SVT.
- ô character 
- C= + 'S' toggles stopwatch ON/OFF. It's meant for testing purposes. When ON, you'll see the jiffy count bottom left (1 jiffy = 1/60 of a second) after each page is fetched and rendered.

### Changed Teletext64U
- Start screen shows version number and is more informative.
- No more font loading at startup; all the fonts are part of the the prg now.
- Fetching teletext pages is now significantly faster. Stock C64 users with a WiC64 will definitely notice the difference; pages load about 2.5 times faster, bringing the total fetch-and-render time to under one second! (on my system that is) Note: Stock C64 users with an Ultimate-II+ cartridge won't see this performance boost yet, as the UCI version of Teletext64U has not yet been updated with the optimized code.
- The station select menu (M) reacts slightly faster. In the previous version the entire menu list was drawn at every key press. Now only the newly and previous selected menu items are redrawn.
- Navigation keys are now checked while rendering a teletext page. This means that the user can change pages while the page is being fetched, resulting in faster navigation.
- Navigating subpages wraps around now; from last subpage back to first (and vice versa if the last page is known).

### Fixed Teletext64U
- The teletext End Box control code ($0A) is handled now; in previous versions 'å' characters were displayed instead of an empty space.

### Changed PetsciiProxy
- DR Tekst-TV: Add support for VM Fodbold 2026 (world cup soccer) pages from 530 and up.
- Ceefax/Teefax: Cycle time info from TTI data is used and stored in the new ct=n field in the teletext header part. Cycle time will be used when rotating between subpages. n is the delay in seconds.


## [1.6.4] - 2026-05-27

### Added
- Help screen (F8)

### Fixed
- YLE Teksti-TV: é and € will be shown now

### Changed
- RunMeFirst.cfg: Turbo Control changed from Manual to C64U Turbo Registers. CPU Speed=40Mhz is removed.
- Teletext64: Sets CPU speed to 40Mhz at runtime.
- Teletext64: Using VIC-II bank 2 for teletext bitmap -> more available memory for extending the program with new features.  
- Teletext64: The fonts are split up into smaller parts. Now there are seperate font files for characters and mosaics. I also created a seperate font for 'Latin National Option Sub-sets' (ETSI EN 300 706 V1.2.1), currently supporting: English, French, Swedish/Finnish, German, Spanish/Portugese and Italian. This set could be extended in the future when adding new teletext services. Visible characters start at $20, everything below is used for extended ASCII character support.
- Teletext64 Wic64 version: removed beta debug screen, so no more change in border color with error codes when a page doesn't exist. Several people have confirmed it works on both original C64s and Ultimates.


## [1.6.3] - 2026-05-20

### Added Teletext64U (both Ultimate and WiC64 edition)
- menu 'M': station select list - new feature: quick select item by pressing the 1st letter of the station. Cycles through stations if there are multiple stations starting with the same letter like.


## [1.6.2] - 2026-05-18

### Fixed
- Teletext64 WiC64 edition: beta #2 should run on a stock C64 and C64 Ultimate and can handle every CPU speed: set Turbo Control to C64U Turbo Registers in the TURBO BOOST menu

### Changed Teletext64U
- Now checks the Turbo Control menu setting; gives message when not set to C64U Turbo Registers. When set, it sets the CPU to 40Mhz at startup


## [1.6.1] - 2026-05-17

### Added
- Teletext64 WiC64 edition - for BETA testing

### Fixed PetsciiProxy
- ZDF: bypasses the x509 validation check


## [1.6.0] - 2026-05-16

### Added
- DR Tekst-TV (Danish teletext)
- Æ, æ, Ø, and ø character support

### Fixed PetsciiProxy
- NOS TT: fine tuned double height / normal height post-fix on pages 703..705, 710, 711 and 713.
- ZDFtext / ZDFinfo / ZDF_Neo / 3sat: These services stopped working on May 15. ZDF added an abuse check (but not always) before the actual teletext page is being served. I added some functions to handle and interact with these checks, if needed. With this fix these teletext services work again (for now).

### Fixed Teletext64U
- Prev/next page numbers now reset after changing stations with 'M'-key.
- The green conceal icon in the header row is steady now (previously flashing).

### Notes on DR Tekst-TV
- This time no html/xml/tti or other format available for this teletext service to work with, only generated teletext images (https://www.dr.dk/cgi-bin/fttv1.exe/100) or plain text (https://www.dr.dk/cgi-bin/fttx2.exe/100) without any color information.
- For this service I used the plain text service to parse the data. That was the easy part. Using plain text mode and applying colors and mosaics in post-fix was a lot of manual labour. Most -but not all- of the colors per page or page group were reconstructed.
- https://zxnet.co.uk/teletext/editor/ was used to reconstruct some of the mosaics, like the DR-logo, DMI-logo and weather map. 
- Because layout differs per page, and the content is not always fixed, I had to write algoritms to dynamically determine where to apply color control codes. We have to see over time how stable this is.
-  A note about the red/yellow progress bars for DR1 and DR2 on page 100: They are accurate and calculated based on the start times of the tv-shows and the current time.
- Page 438 has a concealed message regarding summer/winter time (press 'C' to show the message).

### WiC64 support coming in the near future
- It took some effort and it works now. Have to do some more testing before releasing. So stay tuned...


## [1.5.1] - 2026-04-30

### Fixed
- PetsciiProxy, SVT Text: +/- keys not working in Teletext64U due to fixed page numbers in the teletext page header (pn=p_ and pn=n_ values)


## [1.5.0] - 2026-04-25

### Added
- ZDFtext, ZDFinfo, ZDFneo & 3sat (all German language)
- SVT Text: App-id now provided when fetching pages from texttv (?app=teletext64u) - thanks to Pär Thernström
- +/- behaviour: if a teletext service provides info about the previous and next pages on the current page, the +/- keys will use this info to navigate.

### Notes on the newly added teletext services
- ZDF(text): German national public service television
- ZDFinfo: TV station with a focus on documentaries, reports and portraits
- ZDF_neo: TV station for a younger audience (25-49)
- 3sat: provided by ARD, ZDF (both Germany), ORF (Austria) and SRG (Switserland). It is a free-to-air, commercial-free German-language public service television channel focusing on culture, science, and education.


## [1.4.0] - 2026-04-17

### Added
- SVT Text (Sweden)
- The station selection list ('M' key) now wraps from the last item back to the first (or vice versa).
- 'R' - Instant refresh of the current teletext page.
- '←' - Go to previous teletext page (max. 20 steps back). Resets when changing stations.

### Notes on SVT Text
- Some of the teletext pages appear to be quite messy "under the hood"—though that may also be down to my parsing skills. Manual intervention was required on several pages to correct control codes within the code to ensure they display properly. I’m unsure if these issues stem from the API itself or its underlying source. Furthermore, the API’s handling of mosaic characters is unusual; it references GIF images using an obscure numeric coding system with hard-coded colors. While I’ve aimed for completeness, some edge cases may remain. Specifically, the graphic on the weather page (401) is currently inaccurate and requires further work.
- Bottom row: Displays fixed Fastext links and a subpage indicator for user convenience when applicable. For some reason SVT Text rarely provides this most of the time. E.g. https://www.svt.se/text-tv/331 The official SVT Text on the web shows all the subpages at once. Maybe a subpage indicator is available on SVT Text on an actual TV.
- Dates: The header reflects the original publishing date and time from the Teletext page. Since older pages are occasionally still hosted, any "stale" content will display the full date including the year (DD-MM-YYYY). E.g. https://texttv.nu/221 ('Hem › Ekonomi › 221').



## [1.3.1] - 2026-04-06

### Added
- Station list menu. Press 'M' to display, use cursor up/down and RETURN to select.
- ó and î character support.
- Improved character draw speed by 50%. Note: because the data is also being fetched from the server and processed while displaying the teletext page, the practical increase is around 10%. The improvement will be noticable on original 1 Mhz C64 machines with an Ultimate cartridge.


## [1.3.0] - 2026-03-28

### Added
- YLE TEKSTI-TV (Finland)
- Å and å character support for Swedish pages on Teksti-TV (pages 700-799).
- Subpage range limitation (currently only NOS Teletekst). For example: if a page has 5 subpages, the cursor-down key will no longer allow requests beyond subpage number 5. Most teletext services provide subpage count information, so support for additional services is expected in the future.
- PetsciiProxy commandline parameters: -p [listening port] -k [Yle Teksti-TV API-key]. Both parameters are optional. Use --help to display all available options.
- petsciiproxy-linux-64bit executable (amd64 architecture).

### Notes on Teksti-TV
- I wasn't aware, but the Finnish language (Suomi) is very intriguing.
- Check page 403 - the lighthouse has a real blinking light!
- Also worth mentioning is index page 670, which lists major European soccer leagues by country.
- They have some pages in English starting from page 190.
- Ex
- Some nice colorful pages: 811, 890-

### Note on Teletext64U
- With the growing number of teletext services, pressing 'S' to switch stations all the time is not the best way. I will look into implementing a list for quick selection.



## [1.2.3] - 2026-03-24

### Added
- *Flashing text* support added in Teletext64U. The green conceal indicator in the top row will also blink on and off now. 

### Notes on flashing and blinking text
On TEEFAX page 532/8 is a really cool subpage! Go check it out, you will find a really great and familiar recreated boot screen of a certain 8-bit computer. Some more (sub)pages to check out nice flashing effects (all TEEFAX): 411/2, 411/17, 411/20, 501/2, 510/4, 510/7, 510/23, 551/4, 794/3.


## [1.2.2] - 2026-03-23

### Added
- *Conceal* support added in Teletext64U. When a page has concealed (hidden) text it won't be shown until you press the 'C' key. It acts like a toggle switch. And how to know if a page contains concealed text? Normally you won't, but I created a special green graphic that will pop up in the top header row after the page number. Only TEEFAX has pages that make use of the conceal feature. Maybe CEEFAX has them too, but so far I couldn't find any.

### Fixed
- PetsciiProxy: CEEFAX page 101 suddenly had a OL,26,.. line in the TTI file resulting in a panic; all Ol's greater than 24 are ignored now.


## [1.2.1] - 2026-03-22

### PetsciiProxy minor update
- The headers on NOS-TT pages 703 and up are now displayed in double height again, like in the old days.


## [1.2.0] - 2026-03-21

### Added
- CEEFAX
- TEEFAX
- Double height character support

* Many thanks to Giancarlo for the suggestions below:
- Config utility: your favorite start up station and page can now be configured. 
- PetsciiProxy executables added for Linux 386 32 bit and Intel Mac

### Note
- Both CEEFAX and TEEFAX use the TTI Page file format. If you know any other teletext source that makes use of TTI it would be fairly easy to support it. So let me know, and I'll look into it.
- TEEFAX showed me I still have some work to do to achieve complete teletext compliancy. I have yet to implement conceal, flashing text, add support country specific characters and optimize and fix a few quirks. Stay tuned for further releases.


## [1.1.0] - 2026-03-10

### Added
- German ARD-TEXT aka *'Der Teletext im Ersten'*
- Press 'S' to switch (alternate) stations and show start page 100
- PetsciiProxy is rewritten in Go, which creates stand alone target executables for Windows, Mac and Linux. 

### Removed
- PetsciiProxy written in Python. Lot's of people had problems getting it up and running or having network security issues, or simply didn't want to give Python full network access (understandable). For me personally -being completely new to Go- I really like the Go language and it's features.

### Remarks ARD-TEXT
- It works almost 100%. There are some minor spacing situations to address. For example: At page 403 you will see famous people with their birth date's in yellow and below in white text a little biography. This block of text should be idented one space to the right.
- I had to fix the 3 rows on the top on each page (except page 100). The html pulled off some weird tricks that had to be corrected when parsing the data. This has to do with how teletext works. When you want to change color for example, the control character needed to do this takes 1 position on screen and won't be visible. A control character will be replaced by a space on screen.
- Fast Text Links (those 4 colored words on the bottom row): ARD-TEXT does not provide these (not online, nor on TV), so I made up then myself. They are always the same for every page. I could make this smarter in a future release to make them dynamic.


## [1.0.2] - 2026-03-07

### Added
Users reporting a black teletext screen at startup now have more information about what could be the problem:
- _Ultimate Command Interface_ detection added at startup; when not detected, the program instructs the user how to enable it.
- PetsciiProxy detection at startup.
- RunMeFirst.cfg. This config file enables the Command Interface and sets the CPU speed to 40Mhz. It's in the Teletext64U/target folder next to the .d64 image. 


## [1.0.1] - 2026-03-05

### Fixed
- Auto refresh timer was off by 3 seconds when running on 1Mhz, because of the time needed to display the teletext page. It now resets before displaying the page.


## [1.0.0] - 2026-03-04
- Initial release of Teletext64U.

### Purpose
- Get it tested by users on various Ultimate products with networking capabilities.