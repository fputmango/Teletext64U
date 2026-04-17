# **Teletext64U** changelog

## [1.4.0] - 2026-04-17

### Added
- SVE Text (Sweden)
- The station selection list ('M' key) now wraps from the last item back to the first (or vice versa).
- 'R' - Instant refresh of the current teletext page.
- '←' - Go to previous teletext page (max. 20 steps back). Resets when changing stations.

### Notes on SVE Text
- Some of the teletext pages seem to a real mess under the hood. Could also be my parsing skills. Manual intervention was nessecary on several pages to correct teletext control codes in code to be able to display them correctly. I do not know if this is due to the API, or the source the API is using. Also, the way the API handles mosaic characters is very strange, with referal to GIF images with a unclear numeric coding system and baked in colors. I tried to be complete, but could still could have missed something. The weather page (401) graphic is not accurate and needs some work.
- Bottom row: currently shows fixed fastext links.
- Also bottom row: A subpage indicator will be shown if needed for user convenience. For some reason SVE Text does not supply this most of the time. E.g. https://www.svt.se/text-tv/331 The official SVE Text on the web shows all the subpages at once. Maybe a subpage indicator is available on SVE Text on an actual TV.
- The actual publishing date and time info from the teletext page is used! For some reason older pages are still being hosted. E.g. https://texttv.nu/221 ('Hem › Ekonomi › 221'). If a page is really old, the date with year (DD-MM-YYYY) is being shown in the header.



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