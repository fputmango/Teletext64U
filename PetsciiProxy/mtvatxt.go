// --- MTVA Text---
//
// Hungarian Teletext service
//
// I started out with parsing this URL: www.teletext.hu/mtv1/mtext/ppp-ss.htm, but it stopped working after 31-08-2026.
// Original source is commented out.
//
// Switched to GIF image to Teletext data conversion.
// The main teletext service is on https://teletext.hu/mtv1/ppp-ss.HTM, which is based on GIF images. This is a first attempt to convert an
// image into teletext data by detecting characters and mosaics. Image esolution is 520x400. So each character is 520/40=13 by and 400/25=16 pixels.
//
// Copyright (C) 2026 Frank Putman
// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"bytes"
	"fmt"
	"image/gif"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const mtvaTxtRows = 25

// Windows-1250/ISO-8859-2 mapping for each accented Hungarian letter; it's used by mtvatxt_glyphs.go
var mtvaTxtAccentStrip = map[byte]byte{
	0xE1: 0xE1, 0xC1: 0xC1, // á Á
	0xE9: 0x60, 0xC9: 0x40, // é É
	0xED: 0xED, 0xCD: 0xCD, // í Í
	0xF3: 0xF3, 0xD3: 0xD3, // ó Ó
	0xF6: 0x7C, 0xD6: 0x5C, // ö Ö
	0xF5: 0x7C, 0xD5: 0x5C, // ő Ő
	0xFA: 0xFA, 0xDA: 0xDA, // ú Ú
	0xFC: 0x7E, 0xDC: 0x5E, // ü Ü
	0xFB: 0x7E, 0xDB: 0x5E, // ű Ű
}

/*
var mtvaTxtColorMap = map[string]byte{
	"black":   TCC_ALPHA_BLACK,
	"red":     TCC_ALPHA_RED,
	"green":   TCC_ALPHA_GREEN,
	"lime":    TCC_ALPHA_GREEN,
	"yellow":  TCC_ALPHA_YELLOW,
	"blue":    TCC_ALPHA_BLUE,
	"magenta": TCC_ALPHA_MAGENTA,
	"fuchsia": TCC_ALPHA_MAGENTA,
	"cyan":    TCC_ALPHA_CYAN,
	"aqua":    TCC_ALPHA_CYAN,
	"white":   TCC_ALPHA_WHITE,
}

const mtvaTxtMosaicFullBlock = 0x7F

func mtvaTxtMosaicColor(alpha byte) byte {
	return alpha + 0x10
}

type mtvaTxtSegment struct {
	mosaic bool
	data   []byte
}

func mtvaTxtSegmentContent(buf []byte) []mtvaTxtSegment {
	// Mosaic classification disabled for debugging (see mtvaTxtMosaicColor /
	// mtvaTxtMosaicFullBlock and the s.mosaic branch in parseMtvaTxtRows) -
	// everything is treated as plain text, so '*' renders as a literal
	// asterisk instead of a mosaic block. Revert to the commented-out logic
	// below to restore mosaic rendering.
	if len(buf) == 0 {
		return nil
	}
	return []mtvaTxtSegment{{mosaic: false, data: buf}}
}
*/

func mtvatxtGetTeletexPage(pageNr string) bool {
	parts := strings.SplitN(pageNr, "-", 2)
	page := parts[0]
	sub := 1
	if len(parts) > 1 && parts[1] != "" {
		if v, err := strconv.Atoi(parts[1]); err == nil && v > 0 {
			sub = v
		}
	}

	/*
		if !mtvaTxtHTMLBlocked() {
			url := fmt.Sprintf("https://www.teletext.hu/mtv1/mtext/%s-%02d.htm", page, sub)
			logFetchingPage(url)
			body, status, reachable := fetchMtvaTxtPage(url)
			if body != nil {
				return mtvaTxtBuildHTMLPage(body, page, sub, pageNr)
			}
			if !reachable {
				return false
			}
			if status != http.StatusForbidden {
				return true
			}
			// The HTML text pages are refused (since 13 Aug 2026): use the GIF pages for a while
			mtvaTxtBlockHTML()
		}
	*/

	return mtvaTxtGetGIFPage(page, sub, pageNr)
}

/*
func mtvaTxtBuildHTMLPage(body []byte, page string, sub int, pageNr string) bool {
	pageBuffer := parseMtvaTxtRows(body, page)
	pageBuffer[0] = mtvaTxtHeaderRow(page)
	// overwrite fastext row, fixed for now
	copy(pageBuffer[24][0:], "\x01HIREK   \x02IDOJARAS   \x03SPORT   \x06MTV-MUSOR")

	var nav NavignationInfo
	nav.prevSubpage = sub - 1
	ps, ns, _ := getPrevNextSubpage(page, nav)

	var output []byte
	output = append(output, []byte(fmt.Sprintf(
		"%v%vftl=101\nftl=175\nftl=200\nftl=300\n<pre>", ps, ns))...)
	for _, r := range pageBuffer {
		output = append(output, r...)
	}
	output = append(output, []byte("</pre>")...)

	savePage(DirMTVATXT, pageNr, output)
	return true
}
*/

func fetchMtvaTxtPage(url string) ([]byte, int, bool) {
	client := http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		fmt.Println("MTVA-TXT: connection error:", err)
		return nil, 0, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		fmt.Println("MTVA-TXT: HTTP error, status", resp.StatusCode, "for", url)
		return nil, resp.StatusCode, true
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Println("MTVA-TXT: read error:", err)
		return nil, resp.StatusCode, false
	}
	return body, resp.StatusCode, true
}

const mtvaTxtHTMLRetryAfter = time.Hour

var (
	mtvaTxtHTMLMu        sync.Mutex
	mtvaTxtHTMLBlockedAt time.Time
)

func mtvaTxtHTMLBlocked() bool {
	mtvaTxtHTMLMu.Lock()
	defer mtvaTxtHTMLMu.Unlock()
	return !mtvaTxtHTMLBlockedAt.IsZero() && time.Since(mtvaTxtHTMLBlockedAt) < mtvaTxtHTMLRetryAfter
}

func mtvaTxtBlockHTML() {
	mtvaTxtHTMLMu.Lock()
	defer mtvaTxtHTMLMu.Unlock()
	if mtvaTxtHTMLBlockedAt.IsZero() || time.Since(mtvaTxtHTMLBlockedAt) >= mtvaTxtHTMLRetryAfter {
		fmt.Println("MTVA-TXT: HTML page refused, switching to GIF conversion for the next", mtvaTxtHTMLRetryAfter)
	}
	mtvaTxtHTMLBlockedAt = time.Now()
}

/*
func parseMtvaTxtRows(body []byte, page string) [][]byte {
	pageBuffer := newPageBuffer(mtvaTxtRows)

	z := html.NewTokenizer(bytes.NewReader(body))
	inPre := false
	row := -1 // -1 to ignore of the header row because i make my own
	col := 0
	holdMosaicsSet := false
	lastMosaicEndCol := -1

	writeByte := func(b byte) {
		if row < 0 || row >= mtvaTxtRows || col >= 40 {
			return
		}
		pageBuffer[row][col] = b
		col++
	}

	var spanColor byte
	var spanActive bool
	var spanBuf []byte
	var pendingWSColor byte
	var pendingWSWidth int

	commitPendingWS := func(forContent bool, preserveWidth bool) {
		width := pendingWSWidth
		if width == 0 {
			return
		}
		if pendingWSColor != TCC_ALPHA_WHITE {
			if forContent {
				if holdMosaicsSet {
					writeByte(TCC_RELEASE_MOSAICS)
					holdMosaicsSet = false
				}
				writeByte(pendingWSColor)
				writeByte(TCC_NEW_BACKGROUND)
				if preserveWidth {
					for i := 0; i < width-2; i++ {
						writeByte(' ')
					}
				}
			} else {
				writeByte(pendingWSColor)
				for i := 0; i < width-1; i++ {
					writeByte(' ')
				}
			}
			pendingWSWidth = 0
			return
		}
		// white
		if width == 1 {
			pendingWSWidth = 0
			return
		}
		if forContent {
			if holdMosaicsSet {
				writeByte(TCC_RELEASE_MOSAICS)
				holdMosaicsSet = false
			}
			writeByte(TCC_ALPHA_WHITE)
			writeByte(TCC_NEW_BACKGROUND)
			if preserveWidth {
				for i := 0; i < width-2; i++ {
					writeByte(' ')
				}
			}
		} else {
			writeByte(TCC_ALPHA_WHITE)
			for i := 0; i < width-1; i++ {
				writeByte(' ')
			}
		}
		pendingWSWidth = 0
	}

	flushSpan := func() {
		if !spanActive {
			return
		}
		allSpace := true
		for _, b := range spanBuf {
			if b != ' ' {
				allSpace = false
				break
			}
		}
		if allSpace && len(spanBuf) > 0 {
			if pendingWSWidth > 0 && pendingWSColor == spanColor {
				pendingWSWidth += len(spanBuf)
			} else {
				if pendingWSWidth > 0 {
					commitPendingWS(true, true)
				}
				pendingWSColor = spanColor
				pendingWSWidth = len(spanBuf)
			}
		} else {
			startOfRow := col == 0

			if n := len(spanBuf); n > 0 && spanBuf[n-1] == ' ' {
				spanBuf = spanBuf[:n-1]
			}
			if startOfRow && pendingWSWidth == 0 && len(spanBuf) > 0 && spanBuf[0] == ' ' {
				spanBuf = spanBuf[1:]
			}

			needed := 0
			for _, s := range mtvaTxtSegmentContent(spanBuf) {
				needed += 1 + len(s.data)
			}
			if pendingWSWidth > 1 && pendingWSColor == TCC_ALPHA_WHITE {
				const wsCost = 2
				if over := wsCost + needed - (40 - col); over > 0 {
					lastDot := -1
					for i := len(spanBuf) - 1; i >= 0; i-- {
						if spanBuf[i] == '.' {
							lastDot = i
							break
						}
					}
					dots := 0
					dotStart := 0
					if lastDot >= 0 {
						dotStart = lastDot
						for dotStart > 0 && spanBuf[dotStart-1] == '.' {
							dotStart--
						}
						dots = lastDot - dotStart + 1
					}
					if dots >= 2 {
						trim := over
						if trim > dots {
							trim = dots
						}
						cut := lastDot - trim + 1
						spanBuf = append(spanBuf[:cut], spanBuf[lastDot+1:]...)
						over -= trim
					}
				}
			}

			commitPendingWS(true, false)

			for _, s := range mtvaTxtSegmentContent(spanBuf) {
				if s.mosaic {
					if col == lastMosaicEndCol && !holdMosaicsSet {
						writeByte(TCC_HOLD_MOSAICS)
						holdMosaicsSet = true
					}
					writeByte(mtvaTxtMosaicColor(spanColor))
					for range s.data {
						writeByte(mtvaTxtMosaicFullBlock)
					}
					lastMosaicEndCol = col
				} else {
					if holdMosaicsSet {
						writeByte(TCC_RELEASE_MOSAICS)
						holdMosaicsSet = false
					}
					writeByte(spanColor)
					for _, b := range s.data {
						writeByte(b)
					}
				}
			}
		}
		spanActive = false
		spanBuf = nil
	}

	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			break
		}
		token := z.Token()

		switch tt {
		case html.StartTagToken:
			switch token.Data {
			case "pre":
				flushSpan()
				commitPendingWS(false, false)
				inPre = true
				row = -1
				col = 0
				holdMosaicsSet = false
				lastMosaicEndCol = -1
			case "font":
				if !inPre {
					break
				}
				flushSpan()
				for _, attr := range token.Attr {
					if attr.Key != "color" {
						continue
					}
					if code, ok := mtvaTxtColorMap[strings.ToLower(attr.Val)]; ok {
						spanColor = code
						spanActive = true
						spanBuf = nil
					}
				}
			}

		case html.EndTagToken:
			switch token.Data {
			case "font":
				flushSpan()
			case "pre":
				flushSpan()
				commitPendingWS(false, false)
				inPre = false
			}

		case html.TextToken:
			if !inPre {
				continue
			}
			text := token.Data
			for i := 0; i < len(text); i++ {
				b := text[i]
				switch {
				case b == '\n':
					flushSpan()
					commitPendingWS(false, false)
					row++
					col = 0
					holdMosaicsSet = false
					lastMosaicEndCol = -1
				case b == '\r' || b < 0x20:
					// ignore
				default:
					var out byte
					switch {
					case b < 0x80:
						out = b
					default:
						if a, ok := mtvaTxtAccentStrip[b]; ok {
							out = a
						} else {
							out = '?'
						}
					}
					if spanActive {
						spanBuf = append(spanBuf, out)
					} else {
						writeByte(out)
					}
				}
			}
		}
	}
	flushSpan()
	commitPendingWS(false, false)

	// generic detection of a page that should be made double height
	page100fixed := false
	if pageBuffer[1][0] == 0x07 && pageBuffer[1][1] == 0x20 && pageBuffer[1][2] == 0x20 && pageBuffer[1][3] == 0x20 && pageBuffer[1][4] == 0x20 &&
		pageBuffer[2][0] == 0x07 && pageBuffer[2][1] == 0x20 && pageBuffer[2][2] == 0x20 && pageBuffer[2][3] == 0x20 &&
		pageBuffer[3][0] == 0x07 && pageBuffer[3][1] == 0x20 && pageBuffer[3][2] == 0x20 && pageBuffer[3][3] == 0x20 {
		page100fixed = true
		for row := 4; row < 21; row++ {
			if pageBuffer[row][0] == TCC_ALPHA_WHITE {
				pageBuffer[row][1] = TCC_DOUBLE_HEIGHT
			}
		}
	}

	// Post-fix some of the main/fastext pages
	if page == "100" && !page100fixed {
		// txt logo
		logoRow1 := []byte{0x16, 0x60, 0x70, 0x70, 0x60, 0x70, 0x20, 0x60, 0x70, 0x60, 0x70, 0x70, 0x20}
		logoRow2 := []byte{0x17, 0x22, 0x63, 0x33, 0x21, 0x2B, 0x7D, 0x3F, 0x21, 0x23, 0x73, 0x23, 0x20}
		logoRow3 := []byte{0x16, 0x20, 0x6A, 0x35, 0x60, 0x7E, 0x27, 0x6F, 0x74, 0x20, 0x7F, 0x20, 0x20}
		copy(pageBuffer[1][0:], logoRow1)
		copy(pageBuffer[2][0:], logoRow2)
		copy(pageBuffer[3][0:], logoRow3)

		copy(pageBuffer[4][13:], pageBuffer[4][12:40])
		pageBuffer[4][12] = 0x20

		// move 'IMPRESSUM' to the right
		copy(pageBuffer[20][20:], pageBuffer[20][0:20])
		copy(pageBuffer[20][0:], "                    ")

		// Make two rows double height
		r := 10
		if pageBuffer[r][3] == 0x20 {
			pageBuffer[r][3] = TCC_DOUBLE_HEIGHT
		} else if pageBuffer[r][39] == 0x20 {
			copy(pageBuffer[r][4:], pageBuffer[r][3:])
			pageBuffer[r][3] = TCC_DOUBLE_HEIGHT
		}
		r = 22
		pageBuffer[r][3] = TCC_DOUBLE_HEIGHT
	}

	// weather
	if page == "175" {
		pageBuffer[2][0] = 0x20
		pageBuffer[2][1] = 0x20
		pageBuffer[3][0] = 0x20
		pageBuffer[3][1] = 0x20
		pageBuffer[4][0] = 0x20
		pageBuffer[4][1] = 0x20

		pageBuffer[23][0] = TCC_ALPHA_WHITE
		pageBuffer[23][1] = TCC_NEW_BACKGROUND
		copy(pageBuffer[23][2:], pageBuffer[23][3:])
		pageBuffer[23][39] = ' '
	}

	// Sport
	if page == "200" {
		// correct TOTO row
		pageBuffer[20][0] = TCC_ALPHA_WHITE
		pageBuffer[20][1] = TCC_NEW_BACKGROUND
		pageBuffer[20][2] = TCC_ALPHA_MAGENTA
		copy(pageBuffer[20][3:], pageBuffer[20][4:])
		pageBuffer[20][39] = '5'
	}

	// TV
	if page == "300" {
		// fix M1 header
		pageBuffer[7][14] = TCC_ALPHA_BLUE
		pageBuffer[7][15] = TCC_NEW_BACKGROUND
		// fix M2 header
		pageBuffer[7][35] = TCC_ALPHA_GREEN
		pageBuffer[7][36] = TCC_NEW_BACKGROUND
		// remove white background in M1 block
		pageBuffer[8][2] = 0x20
		pageBuffer[8][3] = 0x20
		pageBuffer[10][2] = 0x20
		pageBuffer[10][3] = 0x20
		pageBuffer[13][2] = 0x20
		pageBuffer[13][3] = 0x20

		pageBuffer[8][20] = TCC_ALPHA_GREEN
		pageBuffer[8][21] = TCC_NEW_BACKGROUND

		pageBuffer[10][0] = TCC_ALPHA_BLUE
		pageBuffer[10][1] = TCC_NEW_BACKGROUND
		pageBuffer[10][20] = TCC_ALPHA_GREEN
		pageBuffer[10][21] = TCC_NEW_BACKGROUND

		copy(pageBuffer[15][0:], pageBuffer[8][0:])

		// M4 SPORT visible again
		pageBuffer[18][18] = TCC_ALPHA_BLUE
		pageBuffer[18][19] = TCC_NEW_BACKGROUND

		copy(pageBuffer[22][4:], pageBuffer[22][3:])
		pageBuffer[22][3] = TCC_DOUBLE_HEIGHT
	}

	return pageBuffer
}
*/

func mtvaTxtHeaderRow(page string) []byte {
	row := make([]byte, 40)
	for i := range row {
		row[i] = 0x20
	}
	row[0] = TCC_ALPHA_CYAN
	copy(row[10:], []byte("MTVA TXT"))
	row[19] = TCC_ALPHA_YELLOW
	copy(row[20:], []byte(time.Now().Format("2006.01.02.  15:04")))
	return row
}

// GIF image conversion code below

const (
	mtvaTxtGIFBaseURL = "https://teletext.hu/mtv1/"
	mtvaTxtCellW      = 13
	mtvaTxtCellH      = 16
)

// How teletext.hu draws its pages (see imagetxt.go)
var mtvaTxtProfile = &imgTxtProfile{
	name:           "MTVA-TXT",
	cellW:          mtvaTxtCellW,
	cellH:          mtvaTxtCellH,
	firstRow:       0,
	rows:           25,
	mosaicX:        [2][2]int{{0, 6}, {6, 13}},
	mosaicY:        [3][2]int{{0, 5}, {5, 11}, {11, 16}},
	separatedX:     [2][2]int{{1, 6}, {7, 12}},
	separatedY:     [3][2]int{{0, 4}, {5, 10}, {11, 15}},
	glyphs:         imgTxtBuildGlyphTable("mtvaTxtGlyphArt", mtvaTxtGlyphArt, mtvaTxtCellW, mtvaTxtCellH, mtvaTxtRuneToByte),
	glyphTableName: "mtvaTxtGlyphArt",
}

// default fastext links
var mtvaTxtDefaultFastext = []string{"101", "175", "200", "300"}

type mtvaTxtHTMInfo struct {
	prev, next       string   // "ppp-ss" or empty
	prevSub, nextSub string   // "ppp-ss" or empty
	image            string   // relative GIF path
	fastext          []string // page numbers of the fastext row, left to right
}

var (
	mtvaTxtHiddenRe = regexp.MustCompile(`(?i)id="(next|prev|nextSub|prevSub)"\s+value="([0-9-]*)"`)
	mtvaTxtImageRe  = regexp.MustCompile(`(?i)<img\s+src="([^"]+\.gif)"`)
	mtvaTxtAreaRe   = regexp.MustCompile(`(?i)coords="(\d+),(\d+),\d+,\d+"\s+href="(\d{3})-\d+\.htm"`)
)

func mtvaTxtGetGIFPage(page string, sub int, pageNr string) bool {
	name := fmt.Sprintf("%s-%02d", page, sub)
	htmURL := mtvaTxtGIFBaseURL + name + ".HTM"
	logFetchingPage(htmURL)
	htm, _, reachable := fetchMtvaTxtPage(htmURL)
	if htm == nil {
		return reachable
	}
	info := parseMtvaTxtHTM(htm, name)

	gifData, _, reachable := fetchMtvaTxtPage(mtvaTxtGIFBaseURL + info.image)
	if gifData == nil {
		return reachable
	}
	img, err := gif.Decode(bytes.NewReader(gifData))
	if err != nil {
		fmt.Println("MTVA-TXT: GIF decode error:", err, "for", name)
		return true
	}
	pageBuffer, unknown := imgTxtDecodeImage(img, mtvaTxtProfile)
	if pageBuffer == nil {
		fmt.Println("MTVA-TXT: unexpected GIF size", img.Bounds().Size(), "for", name)
		return true
	}
	mtvaTxtProfile.logUnknownCells(name, unknown)
	pageBuffer[0] = mtvaTxtHeaderRow(page)

	var nav NavignationInfo
	nav.prevPage, _ = mtvaTxtSplitPageName(info.prev)
	nav.nextPage, _ = mtvaTxtSplitPageName(info.next)
	_, nav.prevSubpage = mtvaTxtSplitPageName(info.prevSub)
	_, nav.nextSubpage = mtvaTxtSplitPageName(info.nextSub)
	pp, np := buildPageNavDirectives(nav.prevPage, nav.nextPage)
	ps, ns, _ := getPrevNextSubpage(page, nav)

	fastext := info.fastext
	if len(fastext) == 0 {
		fastext = mtvaTxtDefaultFastext
	}
	var output []byte
	output = append(output, []byte(pp+np+ps+ns)...)
	for _, f := range fastext {
		output = append(output, []byte("ftl="+f+"\n")...)
	}
	output = append(output, []byte("<pre>")...)
	for _, r := range pageBuffer {
		output = append(output, r...)
	}
	output = append(output, []byte("</pre>")...)

	savePage(DirMTVATXT, pageNr, output)
	return true
}

func parseMtvaTxtHTM(htm []byte, name string) mtvaTxtHTMInfo {
	info := mtvaTxtHTMInfo{image: "images/" + strings.ToLower(name) + ".gif"}
	for _, m := range mtvaTxtHiddenRe.FindAllSubmatch(htm, -1) {
		v := string(m[2])
		switch strings.ToLower(string(m[1])) {
		case "next":
			info.next = v
		case "prev":
			info.prev = v
		case "nextsub":
			info.nextSub = v
		case "prevsub":
			info.prevSub = v
		}
	}
	if m := mtvaTxtImageRe.FindSubmatch(htm); m != nil {
		info.image = string(m[1])
	}
	// The image map links on the fastext row (y = 384) are the fastext links
	type link struct {
		x    int
		page string
	}
	var links []link
	for _, m := range mtvaTxtAreaRe.FindAllSubmatch(htm, -1) {
		x, _ := strconv.Atoi(string(m[1]))
		y, _ := strconv.Atoi(string(m[2]))
		if y == 24*mtvaTxtCellH {
			links = append(links, link{x, string(m[3])})
		}
	}
	sort.Slice(links, func(i, j int) bool { return links[i].x < links[j].x })
	for _, l := range links {
		if len(info.fastext) < 4 {
			info.fastext = append(info.fastext, l.page)
		}
	}
	return info
}

func mtvaTxtSplitPageName(s string) (int, int) {
	parts := strings.SplitN(s, "-", 2)
	page, _ := strconv.Atoi(parts[0])
	sub := 0
	if len(parts) == 2 {
		sub, _ = strconv.Atoi(parts[1])
	}
	return page, sub
}
