// --- Atresmedia: Antena 3 and La Sexta ---
//
// Spanish Teletext services
//
// This service uses the FAB Teletext system, just like Telegazeta. So a lot of the codebase could be re-used.
// https://www.fab-online.com/teletext/
//
// Both stations publish their pages like this: an HTML page per subpage holding a PNG image of the page and an image map with the page links.
//   page:  <base><h>00/<ppp>_<ssss>.htm   (h = first digit of the page number, ssss = subpage, starting at 0001)
//   image: <base><h>00/<ppp>_<ssss>.png
//
// The PNG is 480x336 pixels and holds teletext rows 1-24 (no header row!), so each cell is 12 by 14 pixels; this is
// exactly the same font and layout as Telegazeta. It is converted into teletext data by imagetxt.go; the character
// pictures are in telegazeta_glyphs.go and atresmedia_glyphs.go.
//
// The HTML page gives:
// - previous/next page: the links around prevpage.gif and nextpage.gif
// - number of subpages: the highest <ppp>_<ssss> it refers to (subpage list, prevsub/nextsub links)
// - fastext: the image map areas on row 24
//
// Copyright (C) 2026 Frank Putman
// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"bytes"
	"fmt"
	"image/png"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const (
	atrCellW         = 12
	atrCellH         = 14
	atrMaxSubpageNum = 9999
)

type atrStation struct {
	name string // shown in the header row
	base string // URL of the folder that holds the 100, 200, ... folders
}

var atrStations = map[string]atrStation{
	DirAntena3: {name: "ANTENA 3", base: "https://www.antena3.com/teletexto/"},
	DirLaSexta: {name: "LA SEXTA", base: "https://www.lasexta.com/teletexto/datos/"},
}

// How Antena 3 and La Sexta draw their pages; the same as Telegazeta so we append them to the glyphs
var atrProfile = &imgTxtProfile{
	name:            "ATR",
	cellW:           atrCellW,
	cellH:           atrCellH,
	firstRow:        1, // the image has no header row!
	rows:            24,
	mosaicX:         [2][2]int{{0, 6}, {6, 12}},
	mosaicY:         [3][2]int{{0, 4}, {4, 10}, {10, 14}},
	separatedX:      [2][2]int{{1, 5}, {7, 11}},
	separatedY:      [3][2]int{{1, 4}, {5, 9}, {10, 13}},
	glyphs:          imgTxtBuildGlyphTable("atrGlyphArt", append(append([]imgTxtGlyph{}, tlgzGlyphArt...), atrGlyphArt...), atrCellW, atrCellH, atrRuneToByte),
	glyphTableName:  "atrGlyphArt",
	doubleWidthUsed: true,
}

func atrGetTeletexPage(pageNr string, dirStation string) bool {
	station, ok := atrStations[dirStation]
	if !ok {
		fmt.Println("ATR: unknown station", dirStation)
		return true
	}
	parts := strings.SplitN(pageNr, "-", 2)
	page := parts[0]
	_, err := strconv.Atoi(page)
	if err != nil || len(page) != 3 {
		fmt.Println("ATR: invalid page number", pageNr)
		return true
	}
	sub := 1
	if len(parts) > 1 {
		if v, err := strconv.Atoi(parts[1]); err == nil && v > 0 && v <= atrMaxSubpageNum {
			sub = v
		}
	}

	// HTML page for navigation and fastext
	pageURL := fmt.Sprintf("%s%c00/%s_%04d", station.base, page[0], page, sub)
	logFetchingPage(pageURL + ".htm")
	htmData, status, reachable := imgTxtFetch(atrProfile.name, pageURL+".htm")
	if htmData == nil {
		if status == 404 {
			fmt.Printf("ATR: page %s-%d does not exist on %s\n", page, sub, dirStation)
		}
		return reachable
	}
	htm := string(htmData)

	// PNG image
	pngData, _, reachable := imgTxtFetch(atrProfile.name, pageURL+".png")
	if pngData == nil {
		return reachable
	}
	img, err := png.Decode(bytes.NewReader(pngData))
	if err != nil {
		fmt.Println("ATR: PNG decode error:", err, "for", pageURL+".png")
		return true
	}
	pageBuffer, unknown := imgTxtDecodeImage(img, atrProfile)
	if pageBuffer == nil {
		fmt.Println("ATR: unexpected PNG size", img.Bounds().Size(), "for", pageURL+".png")
		return true
	}
	atrProfile.logUnknownCells(fmt.Sprintf("%s %s_%04d", dirStation, page, sub), unknown)
	pageBuffer[0] = atrHeaderRow(station.name, page)

	var nav NavignationInfo
	nav.prevPage = atrNavLink(htm, "prevpage")
	nav.nextPage = atrNavLink(htm, "nextpage")
	nav.numberOfSubpages = atrSubpageCount(htm, page, sub)
	nav.prevSubpage = sub - 1
	if sub < nav.numberOfSubpages {
		nav.nextSubpage = sub + 1
	}
	pp, np := buildPageNavDirectives(nav.prevPage, nav.nextPage)
	ps, ns, _ := getPrevNextSubpage(page, nav)

	var output []byte
	output = append(output, []byte(pp+np+ps+ns)...)
	for _, f := range atrFastext(htm, nav) {
		output = append(output, []byte("ftl="+f+"\n")...)
	}
	output = append(output, []byte("<pre>")...)
	for _, r := range pageBuffer {
		output = append(output, r...)
	}
	output = append(output, []byte("</pre>")...)

	savePage(dirStation, pageNr, output)
	return true
}

func atrHeaderRow(name, page string) []byte {
	days := []string{"dom", "lun", "mar", "mié", "jue", "vie", "sáb"}
	months := []string{"ene", "feb", "mar", "abr", "may", "jun", "jul", "ago", "sep", "oct", "nov", "dic"}
	now := time.Now()
	text := fmt.Sprintf("\x02%-9s%s\x03%s %02d %s  %s",
		name, page, days[now.Weekday()], now.Day(), months[now.Month()-1], now.Format("15:04:05"))
	var header []byte
	for _, r := range text {
		if r < 0x20 {
			header = append(header, byte(r)) // control code
		} else {
			header = append(header, atrRuneToByte(r))
		}
	}
	row := newPageBuffer(1)[0]
	if len(header) > len(row) {
		header = header[:len(row)]
	}
	copy(row[len(row)-len(header):], header) // right aligned
	return row
}

// a link to a page: "101_0001.htm" or "../200/200_0001.htm"
var atrPageLinkRe = regexp.MustCompile(`(\d{3})_(\d{4})\.htm`)

// <a href="102_0001.htm" accesskey="2"><img src="../gif/nextpage.gif" ...>
var atrNavLinkRe = regexp.MustCompile(`(?is)<a\s[^>]*href="([^"]*)"[^>]*>\s*<img\s[^>]*src="[^"]*/(prevpage|nextpage)\.gif"`)

// returns the page number behind the prevpage or nextpage button, or 0 when there is none
func atrNavLink(htm, button string) int {
	for _, m := range atrNavLinkRe.FindAllStringSubmatch(htm, -1) {
		if strings.EqualFold(m[2], button) {
			if l := atrPageLinkRe.FindStringSubmatch(m[1]); l != nil {
				n, _ := strconv.Atoi(l[1])
				return n
			}
		}
	}
	return 0
}

// returns the highest subpage of this page that the HTML refers to (subpage list, prevsub/nextsub, image list of the slide show)
func atrSubpageCount(htm, page string, sub int) int {
	count := sub
	re := regexp.MustCompile(page + `_(\d{4})\.(?:htm|png)`)
	for _, m := range re.FindAllStringSubmatch(htm, -1) {
		if n, _ := strconv.Atoi(m[1]); n > count && n <= atrMaxSubpageNum {
			count = n
		}
	}
	return count
}

// <img id="FABTTXImage" src="101_0001.png" ... usemap="#M101_0001" ...>
var atrUsemapRe = regexp.MustCompile(`(?is)<img\s[^>]*src="[^"]*\.png"[^>]*usemap="#([^"]+)"`)

// <area shape="rect" coords="12,322,107,336" href="101_0001.htm" alt="101_0001.htm">
var atrAreaRe = regexp.MustCompile(`(?is)<area\s[^>]*coords="(\d+),(\d+),(\d+),(\d+)"[^>]*href="([^"]*)"`)

func atrFastext(htm string, nav NavignationInfo) []string {
	// the page has a map for every subpage (slide show); use the one of the image shown
	mapHTML := htm
	if m := atrUsemapRe.FindStringSubmatch(htm); m != nil {
		start := strings.Index(htm, `name="`+m[1]+`"`)
		if start >= 0 {
			end := strings.Index(strings.ToLower(htm[start:]), "</map>")
			if end >= 0 {
				mapHTML = htm[start : start+end]
			}
		}
	}

	type area struct{ x, page int }
	row24Top := (24 - atrProfile.firstRow) * atrCellH
	var areas []area
	for _, m := range atrAreaRe.FindAllStringSubmatch(mapHTML, -1) {
		y1, _ := strconv.Atoi(m[2])
		if y1 != row24Top {
			continue
		}
		l := atrPageLinkRe.FindStringSubmatch(m[5])
		if l == nil {
			continue
		}
		x1, _ := strconv.Atoi(m[1])
		p, _ := strconv.Atoi(l[1])
		areas = append(areas, area{x1, p})
	}
	sort.Slice(areas, func(i, j int) bool { return areas[i].x < areas[j].x })

	var links []string
	for _, a := range areas {
		if len(links) == 4 {
			break
		}
		links = append(links, strconv.Itoa(a.page))
	}
	extra := []int{nav.prevPage, nav.nextPage, 100}
	for i := 0; len(links) < 4; i++ {
		p := 100
		if i < len(extra) && extra[i] > 0 {
			p = extra[i]
		}
		links = append(links, strconv.Itoa(p))
	}
	return links
}
