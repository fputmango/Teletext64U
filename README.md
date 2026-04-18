# Teletext 64 Ultimate

Teletext program showing live pages from the internet for the Commodore 64 Ultimate and other Ultimate products that have networking capabilities.

Currently supporting:

| Service | Country |Description |
| :--- | :--- | :--- |
|NOS Teletekst |The Netherlands | |
|ARD Text |Germany |*'Der Teletext im Ersten'* |
|Ceefax |UK |Closed by the BBC in 2012 and recreated by Nathan Dane |
|Teefax |UK |A community based service with a huge collection of fine teletext art, historical pages and other great stuff |
|YLE Teksti-TV |Finland | |
|SVT Text |Sweden | |


My mission: add as many Teletext services as possible. So let me know if you have any wishes.

## Description
100% teletext compliant by using proper teletext character sets including the special graphic (mosaic) characters and support for the most common diacrits / accent marks. It runs in hires bitmap mode to support all the required foreground/background color combinations. To make the look & feel even more authentic, it can be used with a super bright RGB palette; so no washed out default C64 colors.

It uses the same internet feed as the mobile apps or websites. To make it feel even more like on TV, the header row is modified to show the actual date with a page number top left. Entering page numbers works like as on a remote. Just type 3 digits and the requested page will be fetched, if available. Browse through sub pages with the cursor up/down keys and the Home key brings you to page 101 with the latest news. It also supports fastext, those four colored words you see at the bottom row. These are mapped to the C64U’s functions keys.

Although the C64U has excellent networking capabilities, it unfortunately is restricted to HTTP. That does not bring us very far with basically every site running secure HTTPS these days. So I wrote a PetsciiProxy program for PC/Mac/Linux/.. that handles the secure HTTPS connections. 

## Feature list
- Auto 60 second refresh - always have the latest news on you screen, handy if you have your C64U hooked up to a separate screen; refresh time can be adjusted to your liking
- Fastext support via function keys (resembling the red, green, yellow and cyan quick access keys from the TV-remote)
- Two custom hand crafted 6x6 pixel character sets (thin & bold) - this leaves toom for 1 pixel row of background color at the top and bottom of each character. This leads to a much better overall looking teletext screen imo.
- Switch between the thin and bold fonts on the fly within the program
- If a requested page is not available online, it will show an older previous saved version of the page, if available. Handy for archival purposes.
- Set your favorite teletext station and start page with the config utility.

## Tools used for this project
- Xcode, CC65, Visual Studio Code, CharPad 

## Source code
PetsciiProxy source code is provided, including makefile. Teletext64U source code will be added later.

## Bio
I grew up with the Commodore 64 as a teenager in the 1980s and always have been a big teletext fan. Professionally I developed teletext software working for a small Dutch Delft based company in the early 1990s. They developed their own teletext hardware based around the Philips SAA5243 IC. Later, commissioned by Dutch television broadcaster NOS, I developed the *NOS Teletekst Browser* and screensaver for Windows to celebrate the 20th anniversary of Teletext in the year 2000. I also developed a teletext iPhone app around 2010. And now it's time to have some fun with making software for the C64.

## Credits and Licensing

### Third-Party Libraries
This project incorporates the following third-party library:

* **[ultimateii-dos-lib](https://github.com/xlar54/ultimateii-dos-lib)** by xlar54
    * **License:** [GNU General Public License v3.0](https://github.com/xlar54/ultimateii-dos-lib/blob/master/LICENSE)
    * **Usage:** Used for Ultimate II+ DOS and Network integration.

### License
This entire project is licensed under the **GNU General Public License v3.0**. 

As required by the GPL-3.0, the source code for this project is freely available. You may find the full text of the license in the `LICENSE` file in the root directory. The original license for `ultimateii-dos-lib` can be found in the `/lib/ultimateii-dos-lib/` folder.