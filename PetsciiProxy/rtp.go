// --- RTP Teletexto  ---
//
// Portuguese Teletext service
//
// This service uses the FAB Teletext system, just like Antena 3, laSexta and Telegazeta. So a lot of the codebase could be re-used.
// https://www.fab-online.com/teletext/
//
// The station publish their pages like this: an HTML page per subpage holding a PNG image of the page and an image map with the page links.
//   page:  https://www.rtp.pt/wportal/fab-txt/<h>00/<ppp>_<ssss>.htm   (h = first digit of the page number, ssss = subpage)
//   image: https://www.rtp.pt/wportal/fab-txt/<h>00/<ppp>_<ssss>.png
//
// The PNG is 480x336 pixels and holds teletext rows 1-24 (cells of 12x14 pixels, no header row).
// It is converted by imagetxt.go with the glyphs of Telegazeta and Atresmedia plus some specific ones in rtp_glyphs.go.
//
// Differences with Atresmedia:
// - Previous and next page are the text links "anterior" and "próxima", not the prevpage.gif/nextpage.gif buttons
// - The image map has the page numbers of the index pages, and sometimes the number at the bottom row; there is no real fastext row
// - Portuguese has ã õ â ê ô (and capital variants): these characters are in the Portuguese G2 bank (teletext-bold-pt-g2.bin / teletext-thin-pt-g2.bin)
//
// Copyright (C) 2026 Frank Putman
// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"bytes"
	"fmt"
	"image/png"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const rtpBase = "https://www.rtp.pt/wportal/fab-txt/"

// How RTP draws its pages; same cells as Atresmedia and Telegazeta, so we add them to the glyphs
var rtpProfile = &imgTxtProfile{
	name:            "RTP",
	cellW:           atrCellW,
	cellH:           atrCellH,
	firstRow:        1, // the image has no header row!
	rows:            24,
	mosaicX:         atrProfile.mosaicX,
	mosaicY:         atrProfile.mosaicY,
	separatedX:      atrProfile.separatedX,
	separatedY:      atrProfile.separatedY,
	glyphs:          imgTxtBuildGlyphTable("rtpGlyphArt", append(append(append([]imgTxtGlyph{}, tlgzGlyphArt...), atrGlyphArt...), rtpGlyphArt...), atrCellW, atrCellH, rtpRuneToByte),
	glyphTableName:  "rtpGlyphArt",
	doubleWidthUsed: true,
}

func rtpGetTeletexPage(pageNr string, dirStation string) bool {
	parts := strings.SplitN(pageNr, "-", 2)
	page := parts[0]
	_, err := strconv.Atoi(page)
	if err != nil || len(page) != 3 {
		fmt.Println("RTP: invalid page number", pageNr)
		return true
	}
	sub := 1
	if len(parts) > 1 {
		if v, err := strconv.Atoi(parts[1]); err == nil && v > 0 && v <= atrMaxSubpageNum {
			sub = v
		}
	}

	// HTML page for navigation
	pageURL := fmt.Sprintf("%s%c00/%s_%04d", rtpBase, page[0], page, sub)
	logFetchingPage(pageURL + ".htm")
	htmData, status, reachable := imgTxtFetch(rtpProfile.name, pageURL+".htm")
	if htmData == nil {
		if status == 404 {
			fmt.Printf("RTP: page %s-%d does not exist\n", page, sub)
		}
		return reachable
	}
	htm := latin1ToUTF8(htmData)

	// PNG image
	pngData, _, reachable := imgTxtFetch(rtpProfile.name, pageURL+".png")
	if pngData == nil {
		return reachable
	}
	img, err := png.Decode(bytes.NewReader(pngData))
	if err != nil {
		fmt.Println("RTP: PNG decode error:", err, "for", pageURL+".png")
		return true
	}
	pageBuffer, unknown := imgTxtDecodeImage(img, rtpProfile)
	if pageBuffer == nil {
		fmt.Println("RTP: unexpected PNG size", img.Bounds().Size(), "for", pageURL+".png")
		return true
	}
	rtpProfile.logUnknownCells(fmt.Sprintf("%s_%04d", page, sub), unknown)
	pageBuffer[0] = rtpHeaderRow(page)

	var nav NavignationInfo
	nav.prevPage = rtpNavLink(htm, "anterior")
	nav.nextPage = rtpNavLink(htm, "próxima")
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

func latin1ToUTF8(b []byte) string {
	r := make([]rune, len(b))
	for i, v := range b {
		r[i] = rune(v)
	}
	return string(r)
}

func rtpHeaderRow(page string) []byte {
	days := []string{"dom", "seg", "ter", "qua", "qui", "sex", "sáb"}
	months := []string{"jan", "fev", "mar", "abr", "mai", "jun", "jul", "ago", "set", "out", "nov", "dez"}
	now := time.Now()
	text := fmt.Sprintf("\x02%-9s%s\x03%s %02d %s  %s",
		"RTP TEXTO", page, days[now.Weekday()], now.Day(), months[now.Month()-1], now.Format("15:04:05"))
	var header []byte
	for _, r := range text {
		if r < 0x20 {
			header = append(header, byte(r)) // control code
		} else {
			header = append(header, rtpRuneToByte(r))
		}
	}
	row := newPageBuffer(1)[0]
	if len(header) > len(row) {
		header = header[:len(row)]
	}
	copy(row[len(row)-len(header):], header) // right aligned
	return row
}

// <a href="116_0001.htm">anterior</a> ... <a href="../200/245_0001.htm">próxima</a>
var rtpNavLinkRe = regexp.MustCompile(`(?is)<a\s[^>]*href="([^"]*)"[^>]*>\s*(anterior|próxima)\s*</a>`)

// returns the page number behind the "anterior" or "próxima" link (or 0 if there is none)
func rtpNavLink(htm, text string) int {
	for _, m := range rtpNavLinkRe.FindAllStringSubmatch(htm, -1) {
		if strings.EqualFold(m[2], text) {
			if l := atrPageLinkRe.FindStringSubmatch(m[1]); l != nil {
				n, _ := strconv.Atoi(l[1])
				return n
			}
		}
	}
	return 0
}
