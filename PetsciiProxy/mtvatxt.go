package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"
)

// --- MTVA TXT (teletext.hu) ---

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

// mtvaTxtAccentStrip maps the Windows-1250/ISO-8859-2 byte for each accented Hungarian letter to
// what gets sent in its place.
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

const mtvaTxtRows = 25

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

	/*
		var segs []mtvaTxtSegment
		i := 0
		for i < len(buf) {
			if buf[i] == '*' {
				j := i
				for j < len(buf) && buf[j] == '*' {
					j++
				}
				if j-i >= 2 {
					segs = append(segs, mtvaTxtSegment{mosaic: true, data: buf[i:j]})
					i = j
					continue
				}
			}
			j := i
			for j < len(buf) {
				if buf[j] == '*' {
					k := j
					for k < len(buf) && buf[k] == '*' {
						k++
					}
					if k-j >= 2 {
						break
					}
				}
				j++
			}
			segs = append(segs, mtvaTxtSegment{mosaic: false, data: buf[i:j]})
			i = j
		}
		return segs
	*/
}

func mtvatxtGetTeletexPage(pageNr string) bool {
	parts := strings.SplitN(pageNr, "-", 2)
	page := parts[0]
	sub := 1
	if len(parts) > 1 && parts[1] != "" {
		if v, err := strconv.Atoi(parts[1]); err == nil && v > 0 {
			sub = v
		}
	}
	url := mtvaTxtPageURL(page, sub)
	logFetchingPage(url)
	body, reachable := fetchMtvaTxtPage(url)
	if body == nil {
		return reachable
	}

	pageBuffer := parseMtvaTxtRows(body, page)
	pageBuffer[0] = mtvaTxtHeaderRow(page)
	// overwrite fastext row, fixed for now
	copy(pageBuffer[24][0:], "\x01HIREK   \x02IDOJARAS   \x03SPORT   \x06MTV-MUSOR")

	var nav NavignationInfo
	nav.prevSubpage = sub - 1
	ps, ns, _ := getPrevNextSubpage(page, nav)

	var output []byte
	output = append(output, []byte(fmt.Sprintf(
		"%v%vftl=101-0\nftl=175-0\nftl=200-0\nftl=300-0\n<pre>", ps, ns))...)
	for _, r := range pageBuffer {
		output = append(output, r...)
	}
	output = append(output, []byte("</pre>")...)

	savePage(DirMTVATXT, pageNr, output)
	return true
}

func mtvaTxtPageURL(page string, sub int) string {
	return fmt.Sprintf("https://www.teletext.hu/mtv1/mtext/%s-%02d.htm", page, sub)
}

func fetchMtvaTxtPage(url string) ([]byte, bool) {
	client := http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		fmt.Println("MTVA-TXT: connection error:", err)
		return nil, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		fmt.Println("MTVA-TXT: HTTP error, status", resp.StatusCode, "for", url)
		return nil, true
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Println("MTVA-TXT: read error:", err)
		return nil, false
	}
	return body, true
}

func parseMtvaTxtRows(body []byte, page string) [][]byte {
	pageBuffer := newPageBuffer(mtvaTxtRows)

	z := html.NewTokenizer(bytes.NewReader(body))
	inPre := false
	// start at -1 to force a drop of the provided header row; i make my own
	row := -1
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

	// commitPendingWS writes out a pending run of pure-whitespace spans.
	//
	// forContent is true when this run is immediately followed by real
	// (non-whitespace) content, or by another differently-colored blank run,
	// later in the same row - i.e. there's a reason to believe the run was
	// deliberately setting up a background. When it's false (end of row, end
	// of <pre>, or the row simply has nothing but blanks after this point),
	// there's nothing to justify an explicit background change, so white
	// always falls back to the safe literal form below, regardless of width.
	//
	// preserveWidth only matters when forContent is true (the non-forContent
	// path below always writes the run's full width, regardless of this
	// flag). It distinguishes two different situations flushSpan commits
	// through this function:
	//
	//   - The LAST pending run right before real content: preserveWidth is
	//     false, so it's always just the [color, NEW_BACKGROUND] pair (2
	//     bytes), regardless of the run's original source width. This is a
	//     deliberate, empirically-verified simplification (see the git log
	//     for the "3-space lead-in" bug this fixed) - it keeps a page's
	//     ordinary left margin at a fixed width across rows, rather than
	//     varying between rows depending on how much room the rest of each
	//     row's content happened to need.
	//   - An EARLIER pending run that a later color change is about to
	//     supersede (see flushSpan below): preserveWidth is true, so its
	//     full source width is kept via literal padding after the pair.
	//     These runs are typically deliberate column separators inside a
	//     multi-column layout (e.g. a wide gap between a left and right
	//     menu column) where the exact width is what creates the layout -
	//     collapsing them to a fixed 2 bytes destroys that alignment.
	//
	// A lone single white space (width == 1) is ordinary inter-word/inter-
	// item spacing, not evidence of an intended background change - even at
	// forContent - so it's dropped entirely rather than spent on a byte of
	// any kind (matches how a single leading space embedded directly in real
	// content already gets trimmed away for free elsewhere in flushSpan).
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
					// A differently-colored blank run is superseding this one - commit
					// it now, preserving its full width (see commitPendingWS above).
					// This can only happen mid-row (more content still follows, whether
					// this new blank run or eventual real content), so forContent=true.
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
				// width == 1 is dropped for free by commitPendingWS below, so it costs
				// nothing here either. Any width >= 2 now always costs exactly 2 bytes -
				// the [color, NEW_BACKGROUND] pair, with no padding beyond it (see
				// commitPendingWS) - regardless of the pending run's original source
				// width, so that fixed cost (not pendingWSWidth) is what the row budget
				// check below needs to use.
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
					// Anything still over after trimming decorative dots just means the row
					// is genuinely too full for the rest of this content - writeByte's own
					// bounds check safely drops whatever doesn't fit rather than panicking.
				}
			}

			// The final pending run right before this real content: fixed 2-byte cost,
			// no width preservation (see commitPendingWS's doc comment above).
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
	if pageBuffer[1][0] == 0x07 && pageBuffer[1][1] == 0x20 && pageBuffer[1][2] == 0x20 && pageBuffer[1][3] == 0x20 &&
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

		// correct mosaic block top right
		/*
			for i := 1; i < 5; i++ {
				pageBuffer[i][32] = TCC_MOSAIC_WHITE
				pageBuffer[i][33] = 0x7F
				pageBuffer[i][34] = 0x7F
			}
		*/
		//pageBuffer[3][15] = TCC_ALPHA_RED

		//pageBuffer[13][0] = TCC_ALPHA_GREEN
		//pageBuffer[13][1] = TCC_NEW_BACKGROUND

		// move 'IMPRESSUM' to the right
		copy(pageBuffer[20][20:], pageBuffer[20][0:20])
		copy(pageBuffer[20][0:], "                    ")

		r := 2
		//c := 13
		/*
			pageBuffer[r][c] = TCC_ALPHA_WHITE
			pageBuffer[r][c+1] = TCC_NEW_BACKGROUND
			r++
			pageBuffer[r][c] = TCC_ALPHA_WHITE
			pageBuffer[r][c+1] = TCC_NEW_BACKGROUND
			r++
			pageBuffer[r][c] = TCC_ALPHA_GREEN
			pageBuffer[r][c+1] = TCC_NEW_BACKGROUND
		*/
		// Make two rows double height
		r = 10
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
