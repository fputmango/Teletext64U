# **Teletext64U** changelog

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