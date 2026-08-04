package main

import (
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

// --- hr-text ---

func hrtextGetTeletexPage(pageNr string) bool {
	var url string
	parts := strings.Split(pageNr, "-")
	subPage, _ := strconv.Atoi(parts[1])

	if subPage < 2 {
		url = fmt.Sprintf("https://www.hr-text.hr-fernsehen.de/ttxweb/?page=%s&xhr=1", parts[0])
	} else {
		subStr := strconv.Itoa(subPage)
		url = fmt.Sprintf("https://www.hr-text.hr-fernsehen.de/ttxweb/?page=%s&sub=%s&xhr=1", parts[0], subStr)
	}

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

	rows, nav := parseHRRows(resp.Body, parts[0], parts[1])

	// Optional directives for (sub)page navigation
	pp := ""
	np := ""
	ps := ""
	ns := ""
	ct := ""
	currentPage = parts[0]

	if nav.numberOfSubpages > 1 {
		ps, ns, ct = getPrevNextSubpage(parts[0], nav)
	}

	pp, np = buildPageNavDirectives(nav.prevPage, nav.nextPage)
	var ftl FastextLinks
	ftl.ftl1 = "102-0"
	ftl.ftl2 = "112-0"
	ftl.ftl3 = "170-0"
	ftl.ftl4 = "200-0"

	var output []byte
	output = append(output, []byte(fmt.Sprintf(
		"%v%v%v%v%vftl=%v\nftl=%v\nftl=%v\nftl=%v\n<pre>", pp, np, ps, ns, ct, ftl.ftl1, ftl.ftl2, ftl.ftl3, ftl.ftl4))...)

	row0 := make([]byte, 40)
	for i := range row0 {
		row0[i] = 0x20
	}
	dt := getORFDate() // yes reuse ORF date here, because why not?
	stationPage := "\x06hr-text\x07"

	copy(row0[19:], stringToLatin1Bytes(dt))
	copy(row0[11:], []byte(stationPage))
	row0[31] = 0x06

	row24 := make([]byte, 40)
	for i := range row0 {
		row24[i] = 0x20
	}
	copy(row24[0:], "\x01Inhalt A-Z \x02Nachrichten  \x03Wetter \x06Sport")

	copy(rows[24], row24)

	output = append(output, row0...)
	for _, r := range rows[1:] {
		output = append(output, r...)
	}

	output = append(output, []byte("</pre>")...)

	savePage(DirHR, pageNr, output)
	return true
}

// --- SWR Baden-Württemberg & Rheinland-Pfalz ---

func swrGetTeletexPage(pageNr string, station string, dirStation string) bool {
	var url string
	parts := strings.Split(pageNr, "-")
	subPage, _ := strconv.Atoi(parts[1])

	if subPage < 2 {
		url = fmt.Sprintf("https://wraps.swr.de/videotext/?stream=%s&page=%s&xhr=1", station, parts[0])
	} else {
		subStr := strconv.Itoa(subPage)
		url = fmt.Sprintf("https://wraps.swr.de/videotext/?stream=%s&page=%s&sub=%s&xhr=1", station, parts[0], subStr)
	}

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

	rows, nav := parseHRRows(resp.Body, parts[0], parts[1])

	// Optional directives for (sub)page navigation
	pp := ""
	np := ""
	ps := ""
	ns := ""
	ct := ""
	currentPage = parts[0]

	if nav.numberOfSubpages > 1 {
		ps, ns, ct = getPrevNextSubpage(parts[0], nav)
	}

	pp, np = buildPageNavDirectives(nav.prevPage, nav.nextPage)

	var ftl FastextLinks
	ftl.ftl1 = "101-0"
	ftl.ftl2 = "112-0"
	ftl.ftl3 = "151-0"
	ftl.ftl4 = "200-0"

	var output []byte
	output = append(output, []byte(fmt.Sprintf(
		"%v%v%v%v%vftl=%v\nftl=%v\nftl=%v\nftl=%v\n<pre>", pp, np, ps, ns, ct, ftl.ftl1, ftl.ftl2, ftl.ftl3, ftl.ftl4))...)

	row0 := make([]byte, 40)
	for i := range row0 {
		row0[i] = 0x20
	}
	dt := getORFDate() // yes reuse ORF date here, because why not?
	var stationPage string
	if station == "bw" {
		stationPage = "\x02SWR BW \x03"
	} else {
		stationPage = "\x02SWR RLP\x03"
	}

	copy(row0[19:], stringToLatin1Bytes(dt))
	copy(row0[7:], []byte(stationPage))
	row0[19] = row0[20]
	row0[20] = row0[21]
	row0[21] = ','
	row0[31] = 0x07

	row24 := make([]byte, 40)
	for i := range row0 {
		row24[i] = 0x20
	}
	copy(row24[0:], "\x01\xDCberblick  \x02Nachrichten \x03Wetter  \x06Sport")

	copy(rows[24], row24)

	output = append(output, row0...)
	for _, r := range rows[1:] {
		output = append(output, r...)
	}

	output = append(output, []byte("</pre>")...)

	savePage(dirStation, pageNr, output)
	return true
}

// hr-text runs on the ttxweb engine (https://github.com/fabianswebworld/ttxweb).
var hrRowRe = regexp.MustCompile(`^row(\d+)$`)
var hrBgRe = regexp.MustCompile(`^bg([0-7])$`)
var hrFgRe = regexp.MustCompile(`^fg([0-7])$`)
var hrMosaicRe = regexp.MustCompile(`^g1([cs])([0-7])([0-9a-fA-F]{2})$`)

func parseHRRows(body io.Reader, pageNr string, subpageStr string) ([][]byte, NavignationInfo) {
	var nav NavignationInfo

	// Initialize buffer with 25 rows, 40 spaces each
	pageBuffer := newPageBuffer(25)

	z := html.NewTokenizer(body)

	currentRow := -1
	currentCol := 0
	prevFg := byte(TCC_ALPHA_WHITE)
	prevBg := byte(TCC_ALPHA_BLACK)
	pendingFg := byte(TCC_ALPHA_WHITE)
	declaredBg := byte(TCC_ALPHA_BLACK)
	pendingBlanks := 0
	zoneBlanks := 0
	skipNextText := false
	pendingDH := false
	prevDH := false

	heldPattern := byte(0x20)   // matches client's pp_held_char default
	prevMosaicMode := byte('c') // mirrors pp_is_sep_mosaic (false = 'c' by default)
	pendingMosaicRepeats := 0
	holdActive := false   // pp_hold_graphic
	graphicsMode := false // pp_is_graphics_mode

	resetRowState := func() {
		currentCol = 0
		prevFg = TCC_ALPHA_WHITE
		prevBg = TCC_ALPHA_BLACK
		pendingFg = TCC_ALPHA_WHITE
		declaredBg = TCC_ALPHA_BLACK
		pendingBlanks = 0
		zoneBlanks = 0
		skipNextText = false
		pendingDH = false
		prevDH = false
		heldPattern = 0x20
		prevMosaicMode = 'c'
		pendingMosaicRepeats = 0
		holdActive = false
		graphicsMode = false
	}

	writeCurrent := func(b byte) {
		writeToBuffer(pageBuffer, &currentRow, &currentCol, b)
	}

	flushPendingMosaicRepeats := func() {
		for i := 0; i < pendingMosaicRepeats; i++ {
			writeCurrent(heldPattern)
		}
		pendingMosaicRepeats = 0
	}

	commitPending := func(codes []byte) {
		blanksToFlush := pendingBlanks - len(codes)
		if blanksToFlush < 0 {
			blanksToFlush = 0
		}
		for i := 0; i < blanksToFlush; i++ {
			writeCurrent(0x20)
		}
		pendingBlanks -= blanksToFlush
		for _, c := range codes {
			writeCurrent(c)
			if pendingBlanks > 0 {
				pendingBlanks--
			}
		}
	}

	commitBgIfPending := func() {
		if declaredBg == prevBg {
			return
		}
		const needed = 2 // colour code + NEW_BACKGROUND
		if pendingBlanks < needed {
			borrow := needed - pendingBlanks
			if borrow > zoneBlanks {
				borrow = zoneBlanks
			}
			pendingBlanks += borrow
			zoneBlanks -= borrow
		}
		commitPending([]byte{declaredBg, TCC_NEW_BACKGROUND})
		prevBg = declaredBg
		prevFg = declaredBg
		graphicsMode = false
		holdActive = false
		pendingBlanks += zoneBlanks
		zoneBlanks = 0
	}

	rendersPatternFor := func(attrCodes []byte, fgCodeIndex int, isMosaic bool, holdOn bool, gfxStart bool) []bool {
		out := make([]bool, len(attrCodes))
		gfx := gfxStart
		for i := range attrCodes {
			out[i] = holdOn && gfx
			if i == fgCodeIndex {
				gfx = isMosaic // alpha -> false, mosaic-colour -> true, immediately
			}
		}
		return out
	}

	resolvePending := func(isMosaic bool, mosaicColorDigit byte, mosaicMode byte) {
		commitBgIfPending()

		desiredFg := pendingFg
		if isMosaic {
			desiredFg = mosaicColorDigit
		}
		fgChanged := desiredFg != prevFg || (isMosaic && !graphicsMode)
		dhChanged := pendingDH != prevDH
		modeChanged := isMosaic && mosaicMode != prevMosaicMode

		var attrCodes []byte
		fgCodeIndex := -1
		if modeChanged {
			if mosaicMode == 's' {
				attrCodes = append(attrCodes, TCC_SEPERATED_MOSAICS)
			} else {
				attrCodes = append(attrCodes, TCC_CONTINUOUS_MOSAICS)
			}
		}
		if fgChanged {
			fgCodeIndex = len(attrCodes)
			code := desiredFg
			if isMosaic {
				code += TCC_MOSAIC_BLACK
			}
			attrCodes = append(attrCodes, code)
		}
		if dhChanged {
			if pendingDH {
				attrCodes = append(attrCodes, TCC_DOUBLE_HEIGHT)
			} else {
				attrCodes = append(attrCodes, TCC_NORMAL_HEIGHT)
			}
		}
		if modeChanged {
			prevMosaicMode = mosaicMode
		}

		hadRepeats := pendingMosaicRepeats > 0
		var finalCodes []byte
		var rendersPattern []bool
		profitable := false

		if hadRepeats {
			needHold := !holdActive
			hypRendersPattern := rendersPatternFor(attrCodes, fgCodeIndex, isMosaic, true, graphicsMode)
			patternSlots := 0
			for _, r := range hypRendersPattern {
				if r {
					patternSlots++
				}
			}
			freeSlots := patternSlots
			if needHold {
				freeSlots++
			}
			profitable = freeSlots >= 2 || (!needHold && freeSlots >= 1)
			if profitable {
				covered := pendingMosaicRepeats
				if covered > freeSlots {
					covered = freeSlots
				}
				leftover := pendingMosaicRepeats - covered
				for i := 0; i < leftover; i++ {
					writeCurrent(heldPattern)
				}
				if needHold {
					finalCodes = append(finalCodes, TCC_HOLD_MOSAICS)
					holdActive = true
				}
				finalCodes = append(finalCodes, attrCodes...)
				rendersPattern = hypRendersPattern
			} else {
				for i := 0; i < pendingMosaicRepeats; i++ {
					writeCurrent(heldPattern)
				}
				finalCodes = append(finalCodes, attrCodes...)
				rendersPattern = rendersPatternFor(attrCodes, fgCodeIndex, isMosaic, holdActive, graphicsMode)
			}
			pendingMosaicRepeats = 0
		} else {
			finalCodes = append(finalCodes, attrCodes...)
			rendersPattern = rendersPatternFor(attrCodes, fgCodeIndex, isMosaic, holdActive, graphicsMode)
		}

		anyPattern := false
		for _, r := range rendersPattern {
			if r {
				anyPattern = true
				break
			}
		}
		if holdActive && anyPattern && !(hadRepeats && profitable) {
			finalCodes = append([]byte{TCC_RELEASE_MOSAICS}, finalCodes...)
			rendersPattern = make([]bool, len(attrCodes))
			holdActive = false
		}

		if fgChanged {
			graphicsMode = isMosaic
		}
		if holdActive && !graphicsMode {
			holdActive = false
		}

		if fgChanged {
			prevFg = desiredFg
		}
		if dhChanged {
			prevDH = pendingDH
		}

		blankEligible := make([]bool, len(finalCodes))
		ptr := 0
		for i, c := range finalCodes {
			if c == TCC_HOLD_MOSAICS || c == TCC_RELEASE_MOSAICS {
				blankEligible[i] = c == TCC_RELEASE_MOSAICS // RELEASE always renders blank
				continue
			}
			r := false
			if ptr < len(rendersPattern) {
				r = rendersPattern[ptr]
			}
			blankEligible[i] = !r
			ptr++
		}

		eligibleCount := 0
		for _, e := range blankEligible {
			if e {
				eligibleCount++
			}
		}
		blanksToFlush := pendingBlanks - eligibleCount
		if blanksToFlush < 0 {
			blanksToFlush = 0
		}
		for i := 0; i < blanksToFlush; i++ {
			writeCurrent(0x20)
		}
		pendingBlanks -= blanksToFlush

		for i, c := range finalCodes {
			writeCurrent(c)
			if blankEligible[i] && pendingBlanks > 0 {
				pendingBlanks--
			}
		}
	}

	writeReal := func(b byte, isMosaic bool, mosaicColorDigit byte, mosaicMode byte) {
		if currentCol < 39 {
			resolvePending(isMosaic, mosaicColorDigit, mosaicMode)
		}
		writeCurrent(b)
	}

	navField := ""
	currentSubpage := 0
	numSubpages := 0

	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			break
		}

		token := z.Token()

		switch tt {
		case html.StartTagToken, html.SelfClosingTagToken:
			switch token.Data {

			case "pre":
				var idVal string
				for _, attr := range token.Attr {
					if attr.Key == "id" {
						idVal = attr.Val
						break
					}
				}

				navField = ""
				switch idVal {
				case "ttxPrevPageNum":
					navField = "prev"
				case "ttxNextPageNum":
					navField = "next"
				case "ttxSubpageNum":
					navField = "sub"
				case "ttxNumSubpages":
					navField = "numsub"
				default:
					if m := hrRowRe.FindStringSubmatch(idVal); m != nil {
						n, err := strconv.Atoi(m[1])
						if err == nil {
							flushPendingMosaicRepeats()
							currentRow = n
							resetRowState()
						}
					}
				}

			case "span":
				if currentRow < 0 || currentRow > 24 {
					continue
				}

				var classVal string
				for _, attr := range token.Attr {
					if attr.Key == "class" {
						classVal = attr.Val
						break
					}
				}
				if classVal == "" {
					continue
				}

				if m := hrMosaicRe.FindStringSubmatch(classVal); m != nil {
					mosaicMode := m[1][0]
					colorDigit := byte(mustAtoi(m[2]))
					if hexVal, err := strconv.ParseUint(m[3], 16, 8); err == nil {
						pattern := byte(hexVal)
						if pattern == heldPattern && colorDigit == prevFg &&
							mosaicMode == prevMosaicMode && graphicsMode {
							pendingMosaicRepeats++
						} else {
							writeReal(pattern, true, colorDigit, mosaicMode)
							heldPattern = pattern
							pendingMosaicRepeats = 0
						}
					}
					skipNextText = true
					continue
				}

				spanIsDH := false
				for _, class := range strings.Fields(classVal) {
					if class == "dh" {
						spanIsDH = true
						continue
					}
					if m := hrBgRe.FindStringSubmatch(class); m != nil {
						newBg := byte(mustAtoi(m[1]))
						if newBg != declaredBg {
							commitBgIfPending()
							declaredBg = newBg
							zoneBlanks = 0
						}
						continue
					}
					if m := hrFgRe.FindStringSubmatch(class); m != nil {
						pendingFg = byte(mustAtoi(m[1]))
					}
				}
				pendingDH = spanIsDH
			}

		case html.EndTagToken:
			if token.Data == "pre" {
				flushPendingMosaicRepeats()
				currentRow = -1
			}

		case html.TextToken:
			text := token.Data

			if navField != "" {
				if val, err := strconv.Atoi(strings.TrimSpace(text)); err == nil {
					switch navField {
					case "prev":
						nav.prevPage = val
					case "next":
						nav.nextPage = val
					case "sub":
						currentSubpage = val
					case "numsub":
						numSubpages = val
					}
				}
				navField = ""
				continue
			}

			if currentRow < 0 || currentRow > 24 || skipNextText {
				skipNextText = false
				continue
			}

			if pendingMosaicRepeats > 0 {
				for i := 0; i < pendingMosaicRepeats; i++ {
					writeCurrent(heldPattern)
				}
				pendingMosaicRepeats = 0
			}

			hasReal := false
			for _, r := range text {
				if r == ' ' || r == ' ' || r == '\n' || r == '\r' || r == '\t' {
					continue
				}
				hasReal = true
				break
			}
			if hasReal {
				resolvePending(false, 0, 'c')
			}

			for _, r := range text {
				if currentCol >= 40 {
					break
				}
				switch {
				case r == ' ': // non-breaking space character
					if declaredBg != prevBg {
						zoneBlanks++
					} else {
						pendingBlanks++
					}
				case r == ' ':
					if declaredBg != prevBg {
						zoneBlanks++
					} else {
						pendingBlanks++
					}
				case r == '\n' || r == '\r' || r == '\t':
				default:
					writeReal(zdfEncodeChar(r), false, 0, 'c')
				}
			}
		}
	}

	flushPendingMosaicRepeats()

	nav.numberOfSubpages = numSubpages
	if currentSubpage > 1 {
		nav.prevSubpage = currentSubpage - 1
	}
	if numSubpages == 0 || currentSubpage < numSubpages {
		nav.nextSubpage = currentSubpage + 1
	}

	return pageBuffer, nav
}

func mustAtoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}
