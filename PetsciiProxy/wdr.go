package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

// --- WDR Text ---
// Every WDR mobiltext cell is its own <div class="bg_X Y vt_col colN">, where
// X is the background color name, Y is either a plain foreground color name
// (e.g. "white") or a mosaic descriptor "g<color>_<hex>h" (e.g. "gblue_7Fh" -
// same hex-mosaic-shape convention as ARD-TEXT's g1x##.gif images, just
// spelled as a CSS class instead of an image filename), and colN is how many
// character cells that run occupies.
var wdrMosaicRe = regexp.MustCompile(`^g([a-z]+)_([0-9A-Fa-f]{2})h$`)

// Matches a run of whitespace that contains at least one tab/CR/LF (plain
// space-only runs, e.g. deliberate multi-space padding, are left alone) -
// used to collapse pretty-printing indentation embedded inside a single
// text node down to the single space a real browser would render it as.
var wdrInterTagWhitespaceRe = regexp.MustCompile(`[ \t]*[\t\r\n][ \t\r\n]*`)

var wdrAlphaColorCode = map[string]byte{
	"black":   TCC_ALPHA_BLACK,
	"red":     TCC_ALPHA_RED,
	"green":   TCC_ALPHA_GREEN,
	"yellow":  TCC_ALPHA_YELLOW,
	"blue":    TCC_ALPHA_BLUE,
	"magenta": TCC_ALPHA_MAGENTA,
	"cyan":    TCC_ALPHA_CYAN,
	"white":   TCC_ALPHA_WHITE,
}

var wdrMosaicColorCode = map[string]byte{
	"black":   TCC_MOSAIC_BLACK,
	"red":     TCC_MOSAIC_RED,
	"green":   TCC_MOSAIC_GREEN,
	"yellow":  TCC_MOSAIC_YELLOW,
	"blue":    TCC_MOSAIC_BLUE,
	"magenta": TCC_MOSAIC_MAGENTA,
	"cyan":    TCC_MOSAIC_CYAN,
	"white":   TCC_MOSAIC_WHITE,
}

// --- WDR Text ---

func wdrtextGetTeletexPage(pageNr string) bool {
	var url string
	parts := strings.Split(pageNr, "-")
	url = fmt.Sprintf("https://mobiltext.wdr.de/%s.html", parts[0])

	logFetchingPage(url)
	resp, err := http.Get(url)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		fmt.Println("HTTP Error: Could not retrieve page", pageNr, "Status:", resp.StatusCode)
		return true
	}

	var nav NavignationInfo
	var ftl FastextLinks
	rows, nav, ftl, err := parseWDRRows(resp.Body, parts[0], parts[1])
	if err != nil {
		fmt.Println("WDR Text page not found:", pageNr, "-", err)
		return true
	}

	// Optional directives for (sub)page navigation
	pp := ""
	np := ""
	ps := ""
	ns := ""
	ct := ""
	currentPage = parts[0]
	subPageIndicator := ""

	if nav.numberOfSubpages > 1 {
		subPageIndicator = "\x07(" + strconv.Itoa(nav.prevSubpage+1) + "/" + strconv.Itoa(nav.numberOfSubpages) + ")"
		ps, ns, ct = getPrevNextSubpage(parts[0], nav)
	}

	pp, np = buildPageNavDirectives(nav.prevPage, nav.nextPage)

	switch parts[0] {
	case "100":
		ftl.ftl2 = ftl.ftl1
		ftl.ftl1 = "100"
	case "899":
		ftl.ftl2 = "899"
	}

	var output []byte
	output = append(output, []byte(fmt.Sprintf(
		"%v%v%v%v%vftl=%v\nftl=%v\nftl=%v\nftl=%v\n<pre>", pp, np, ps, ns, ct, ftl.ftl1, ftl.ftl2, ftl.ftl3, ftl.ftl4))...)

	row0 := make([]byte, 40)
	for i := range row0 {
		row0[i] = 0x20
	}
	dt := getORFDate() // yes reuse ORF date here, because why not?
	stationPage := "\x07" + parts[0] + " WDR Text"

	copy(row0[19:], stringToLatin1Bytes(dt))
	copy(row0[5:], []byte(stationPage))

	// reconstruct row[24] based on FTL info; if there is a blue text on a yellow background it should be saved to insert it later again
	ByteIndex := bytes.IndexByte(rows[24], byte(TCC_ALPHA_BLUE)) + 1
	blueString := ""
	if ByteIndex > 0 {
		blueString = strings.TrimRight(string(rows[24][ByteIndex:]), " ")
	}
	row24 := make([]byte, 40)
	for i := range row0 {
		row24[i] = 0x20
	}

	if ftl.ftl1 != "" {
		row24[0] = TCC_ALPHA_RED
		row24[1] = TCC_NEW_BACKGROUND
		row24[2] = TCC_ALPHA_WHITE
		row24[3] = byte('-')
		row24[4] = 0x20
		copy(row24[5:], []byte(ftl.ftl1))
	}
	if ftl.ftl2 != "" {
		row24[8] = TCC_ALPHA_GREEN
		row24[9] = TCC_NEW_BACKGROUND
		row24[10] = TCC_ALPHA_WHITE
		row24[11] = byte('+')
		row24[12] = 0x20
		copy(row24[13:], []byte(ftl.ftl2))
	}
	if ByteIndex > 0 {
		row24[16] = TCC_ALPHA_YELLOW
		row24[17] = TCC_NEW_BACKGROUND
		row24[18] = TCC_ALPHA_BLUE
		copy(row24[19:], []byte(blueString))
		row24[20+len(blueString)] = TCC_BLACK_BACKGROUND
	} else {
		row24[18] = TCC_BLACK_BACKGROUND
	}
	if subPageIndicator != "" {
		copy(row24[40-len(subPageIndicator):], []byte(subPageIndicator))
	}

	copy(rows[24], row24)

	output = append(output, row0...)
	for _, r := range rows[1:] {
		output = append(output, r...)
	}

	output = append(output, []byte("</pre>")...)

	savePage(DirWDR, pageNr, output)
	return true
}

func parseWDRRows(body io.Reader, pageNr string, subpageStr string) ([][]byte, NavignationInfo, FastextLinks, error) {
	var nav NavignationInfo
	var ftl FastextLinks

	// Initialize buffer with 25 rows, 40 spaces each
	pageBuffer := newPageBuffer(25)

	// "0" (no subpage chosen yet) and "1" both mean the first subpage.
	targetSubpage := subpageStr
	if targetSubpage == "0" {
		targetSubpage = "1"
	}
	targetSubpageNum, _ := strconv.Atoi(targetSubpage)

	z := html.NewTokenizer(body)

	inTargetPage := false
	foundTargetPage := false
	subpageCount := 0
	currentRow := 0
	currentCol := 0

	lastFg := "white"
	lastBg := "black"

	lastFgIsMosaic := false

	prevBlankStart := -1
	prevBlankBgBefore := ""
	prevBlankFgBefore := ""
	prevBlankFgIsMosaicBefore := false

	linkTarget := ""

	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			break
		}
		if tt != html.StartTagToken && tt != html.SelfClosingTagToken {
			continue
		}
		token := z.Token()

		var idVal, classVal, actionVal string
		for _, attr := range token.Attr {
			switch attr.Key {
			case "id":
				idVal = attr.Val
			case "class":
				classVal = attr.Val
			case "action":
				actionVal = attr.Val
			}
		}

		if token.Data == "form" {
			switch idVal {
			case "vt_navigation_prev":
				nav.prevPage, _ = strconv.Atoi(strings.TrimSuffix(actionVal, ".html"))
			case "vt_navigation_next":
				nav.nextPage, _ = strconv.Atoi(strings.TrimSuffix(actionVal, ".html"))
			}
			continue
		}

		if token.Data == "div" && strings.HasPrefix(idVal, "seite_") {
			subpageCount++
			inTargetPage = strings.TrimPrefix(idVal, "seite_") == targetSubpage
			if inTargetPage {
				foundTargetPage = true
			}
			continue
		}

		if !inTargetPage {
			continue
		}

		if token.Data == "div" && strings.HasPrefix(idVal, "vt_row_") {
			rowNum, _ := strconv.Atoi(strings.TrimPrefix(idVal, "vt_row_"))
			// vt_row_24 is always an empty wrapper around a nested vt_row_25 - both land on our own row 24
			if rowNum == 25 {
				rowNum = 24
			}
			currentRow = rowNum
			currentCol = 0
			lastFg = "white"
			lastBg = "black"
			lastFgIsMosaic = false
			prevBlankStart = -1
			continue
		}

		classes := strings.Fields(classVal)
		isCell := len(classes) == 4 && classes[2] == "vt_col" && strings.HasPrefix(classes[3], "col")
		if !isCell {
			continue
		}

		width, _ := strconv.Atoi(strings.TrimPrefix(classes[3], "col"))
		bgName := strings.TrimPrefix(classes[0], "bg_")
		fgDescriptor := classes[1]

		var text []rune
		if tt == html.StartTagToken {
			depth := 1
			for depth > 0 {
				itt := z.Next()
				if itt == html.ErrorToken {
					break
				}
				itok := z.Token()
				switch itt {
				case html.StartTagToken:
					if itok.Data == "div" {
						depth++
					} else if itok.Data == "span" {
						invisible := false
						for _, a := range itok.Attr {
							if a.Key == "class" && strings.Contains(a.Val, "invisible") {
								invisible = true
							}
						}
						if invisible {
							skipDepth := 1
							for skipDepth > 0 {
								sitt := z.Next()
								if sitt == html.ErrorToken {
									break
								}
								sitok := z.Token()
								if sitt == html.StartTagToken && sitok.Data == "span" {
									skipDepth++
								} else if sitt == html.EndTagToken && sitok.Data == "span" {
									skipDepth--
								}
							}
						}
					} else if itok.Data == "a" {
						for _, a := range itok.Attr {
							if a.Key == "href" {
								linkTarget = strings.TrimSuffix(a.Val, ".html")
								if currentRow == 24 {
									//fmt.Printf("Row=%v; linkTarget=%v\n", currentRow, linkTarget)
									if ftl.ftl1 == "" {
										ftl.ftl1 = linkTarget
									} else if ftl.ftl2 == "" {
										ftl.ftl2 = linkTarget
									} else if ftl.ftl3 == "" {
										ftl.ftl3 = linkTarget
									} else if ftl.ftl4 == "" {
										ftl.ftl4 = linkTarget
									}
								}
							}
						}
					}
				case html.EndTagToken:
					if itok.Data == "div" {
						depth--
					}
				case html.TextToken:
					if trimmed := strings.TrimSpace(itok.Data); trimmed == "" && strings.ContainsAny(itok.Data, "\n\r") {
						continue
					}
					text = append(text, []rune(wdrInterTagWhitespaceRe.ReplaceAllString(itok.Data, " "))...)
				}
			}
		}

		if currentRow < 1 || currentRow > 24 {
			continue
		}
		if currentCol >= 40 {
			continue
		}
		if currentCol+width > 40 {
			width = 40 - currentCol
		}

		contentStart := currentCol
		remaining := width
		reclaimPos := currentCol

		emitControlSequence := func(lastMustStealFront bool, codes ...byte) bool {
			n := len(codes)
			reclaimEligible := n
			if lastMustStealFront {
				reclaimEligible--
			}
			reclaimable := 0
			for reclaimable < reclaimEligible && reclaimPos-reclaimable > 0 && pageBuffer[currentRow][reclaimPos-reclaimable-1] == 0x20 {
				reclaimable++
			}
			neededFromFront := n - reclaimable
			if remaining < neededFromFront {
				return false
			}
			for i := 0; i < reclaimable; i++ {
				pageBuffer[currentRow][reclaimPos-reclaimable+i] = codes[i]
			}
			reclaimPos -= reclaimable
			for i := reclaimable; i < n; i++ {
				pageBuffer[currentRow][contentStart] = codes[i]
				contentStart++
				remaining--
			}
			return true
		}

		isMosaic := false
		mosaicColorName := ""
		var mosaicByte byte
		if m := wdrMosaicRe.FindStringSubmatch(fgDescriptor); m != nil {
			isMosaic = true
			mosaicColorName = m[1]
			hv, _ := strconv.ParseUint(m[2], 16, 8)
			mosaicByte = byte(hv) + 0x80
		}

		cellIsBlank := !isMosaic && strings.TrimSpace(string(text)) == ""
		bgBeforeCell := lastBg
		fgBeforeCell := lastFg
		fgIsMosaicBeforeCell := lastFgIsMosaic

		effectiveStart := currentCol

		if bgName != lastBg {
			if code, ok := wdrAlphaColorCode[bgName]; ok {
				attempt := func() bool {
					if lastFg == bgName && !lastFgIsMosaic {
						return emitControlSequence(true, TCC_NEW_BACKGROUND)
					}
					return emitControlSequence(true, code, TCC_NEW_BACKGROUND)
				}
				sequenceOK := attempt()
				if !sequenceOK && prevBlankStart >= 0 {
					for p := prevBlankStart; p < currentCol; p++ {
						pageBuffer[currentRow][p] = 0x20
					}
					remaining = (currentCol - prevBlankStart) + width
					contentStart = prevBlankStart
					reclaimPos = prevBlankStart
					lastBg = prevBlankBgBefore
					lastFg = prevBlankFgBefore
					lastFgIsMosaic = prevBlankFgIsMosaicBefore
					bgBeforeCell = prevBlankBgBefore
					effectiveStart = prevBlankStart
					sequenceOK = attempt()
				}
				if sequenceOK {
					lastBg = bgName
					lastFg = bgName
					lastFgIsMosaic = false
					prevBlankStart = -1
				}
			}
		}

		if !cellIsBlank {
			logicalColor := fgDescriptor
			if isMosaic {
				logicalColor = mosaicColorName
			}
			if logicalColor != lastFg || isMosaic != lastFgIsMosaic {
				var code byte
				var ok bool
				if isMosaic {
					code, ok = wdrMosaicColorCode[mosaicColorName]
				} else {
					code, ok = wdrAlphaColorCode[fgDescriptor]
				}
				if ok && emitControlSequence(false, code) {
					lastFg = logicalColor
					lastFgIsMosaic = isMosaic
				}
			}
		}

		if cellIsBlank && prevBlankStart < 0 {
			prevBlankStart = effectiveStart
			prevBlankBgBefore = bgBeforeCell
			prevBlankFgBefore = fgBeforeCell
			prevBlankFgIsMosaicBefore = fgIsMosaicBeforeCell
		} else if !cellIsBlank {
			prevBlankStart = -1
		}

		if isMosaic {
			for i := 0; i < remaining; i++ {
				pageBuffer[currentRow][contentStart+i] = mosaicByte
			}
		} else {
			deficit := len(text) - remaining
			if deficit < 0 {
				deficit = 0
			}
			for i := 0; i < remaining; i++ {
				src := deficit + i
				if src < len(text) {
					r := text[src]
					if r == ' ' {
						pageBuffer[currentRow][contentStart+i] = 0x20
					} else {
						pageBuffer[currentRow][contentStart+i] = zdfEncodeChar(r)
					}
				} else {
					pageBuffer[currentRow][contentStart+i] = 0x20
				}
			}
		}

		currentCol += width
	}

	nav.numberOfSubpages = subpageCount
	if targetSubpageNum > 1 {
		nav.prevSubpage = targetSubpageNum - 1
	}
	if subpageCount == 0 || targetSubpageNum < subpageCount {
		nav.nextSubpage = targetSubpageNum + 1
	}

	if !foundTargetPage {
		return pageBuffer, nav, ftl, fmt.Errorf("WDR page %s: subpage %s not found (page has %d subpage(s))", pageNr, targetSubpage, subpageCount)
	}

	return pageBuffer, nav, ftl, nil
}
