package main

import (
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"
)

// --- NDR Text ---

type ndrCell struct {
	ch        rune // text character (when !mosaic)
	sext      byte // sextant bits 0-63 (when mosaic)
	mosaic    bool
	separated bool // separated mosaic (when mosaic)
	fg        byte // teletext colour 0-7
	bg        byte // teletext colour 0-7
}

func (c ndrCell) blank() bool {
	if c.mosaic {
		return c.sext == 0
	}
	return c.ch == ' ' || c.ch == ' '
}

func ndrtextGetTeletexPage(pageNr string) bool {
	parts := strings.Split(pageNr, "-")
	subpage := 1
	if len(parts) > 1 {
		if s, err := strconv.Atoi(parts[1]); err == nil && s > 1 {
			subpage = s
		}
	}
	url := fmt.Sprintf("https://www.ndr.de/public/teletext/%s_%02d.htm", parts[0], subpage)

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

	cells, nav, err := parseNDRPage(resp.Body)
	if err != nil {
		fmt.Println("NDR Text page could not be parsed:", pageNr, "-", err)
		return true
	}

	ps := ""
	ns := ""
	ct := ""
	currentPage = parts[0]
	subPageIndicator := ""
	if nav.numberOfSubpages > 1 {
		subPageIndicator = "\x07(" + strconv.Itoa(subpage) + "/" + strconv.Itoa(nav.numberOfSubpages) + ")"
		ps, ns, ct = getPrevNextSubpage(parts[0], nav)
	}
	pp, np := buildPageNavDirectives(nav.prevPage, nav.nextPage)

	var output []byte
	output = append(output, []byte(fmt.Sprintf(
		"%v%v%v%v%vftl=101\nftl=200\nftl=300\nftl=650\n<pre>", pp, np, ps, ns, ct))...)

	row0 := newPageBuffer(1)[0]
	copy(row0[21:], stringToLatin1Bytes(getNDRDate()))
	copy(row0[7:], []byte("\x07"+parts[0]+"\x06NDR TEXT"))
	output = append(output, row0...)

	for r := 0; r < 23; r++ {
		var rowCells []ndrCell
		if r < len(cells) {
			rowCells = cells[r]
		}
		output = append(output, ndrEncodeRow(rowCells)...)
	}

	row24 := newPageBuffer(1)[0]
	if subPageIndicator == "" {
		copy(row24[0:], "\x01Inhalt    \x02Sport   \x03Fernsehen   \x06Wetter")
	} else {
		copy(row24[0:], "\x01Inhalt \x02Sport \x03Fernsehen \x06Wetter")
		copy(row24[40-len(subPageIndicator):], []byte(subPageIndicator))
	}
	output = append(output, row24...)

	output = append(output, []byte("</pre>")...)
	savePage(DirNDR, pageNr, output)
	return true
}

// Matches the "n/m" subpage counter in header
var ndrSubpageCountRe = regexp.MustCompile(`(\d+)/(\d+)`)

// Parses the "NNN_SS.htm" href of a navigation link
func ndrParseHref(href string) (int, int) {
	name := strings.TrimSuffix(href, ".htm")
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	p, s, found := strings.Cut(name, "_")
	if !found {
		return 0, 0
	}
	page, _ := strconv.Atoi(p)
	sub, _ := strconv.Atoi(s)
	return page, sub
}

func parseNDRPage(body io.Reader) ([][]ndrCell, NavignationInfo, error) {
	var nav NavignationInfo
	var rows [][]ndrCell
	var hdrText strings.Builder

	z := html.NewTokenizer(body)
	inHdr := false
	inTxt := false
	foundTxt := false
	seenText := false
	fg, bg := byte(TCC_ALPHA_WHITE), byte(TCC_ALPHA_BLACK)
	row := 0

	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			break
		}
		token := z.Token()

		switch tt {
		case html.StartTagToken:
			var idVal, classVal, hrefVal string
			for _, attr := range token.Attr {
				switch attr.Key {
				case "id":
					idVal = attr.Val
				case "class":
					classVal = attr.Val
				case "href":
					hrefVal = attr.Val
				}
			}

			switch token.Data {
			case "pre":
				inHdr = classVal == "hdr"
				inTxt = classVal == "txt"
				if inTxt {
					foundTxt = true
				}
			case "a":
				if !inHdr {
					continue
				}
				page, sub := ndrParseHref(hrefVal)
				switch idVal {
				case "kbd_left":
					nav.prevPage = page
				case "kbd_right":
					nav.nextPage = page
				case "kbd_minus":
					nav.prevSubpage = sub
				case "kbd_plus":
					nav.nextSubpage = sub
				}
			case "b":
				if !inTxt {
					continue
				}
				fg, bg = TCC_ALPHA_WHITE, TCC_ALPHA_BLACK
				for _, c := range strings.Fields(classVal) {
					if len(c) != 2 || c[1] < '0' || c[1] > '7' {
						continue
					}
					switch c[0] {
					case 'f':
						fg = c[1] - '0'
					case 'b':
						bg = c[1] - '0'
					}
				}
			}

		case html.EndTagToken:
			switch token.Data {
			case "pre":
				inHdr = false
				inTxt = false
			case "b":
				fg, bg = TCC_ALPHA_WHITE, TCC_ALPHA_BLACK
			}

		case html.TextToken:
			if inHdr {
				hdrText.WriteString(token.Data)
			}
			if !inTxt {
				continue
			}
			for _, r := range token.Data {
				if r == '\r' {
					continue
				}
				if r == '\n' {
					if seenText {
						row++
					}
					continue
				}
				seenText = true
				for len(rows) <= row {
					rows = append(rows, nil)
				}
				cell := ndrCell{ch: r, fg: fg, bg: bg}
				if r >= 0xE000 && r <= 0xE07F {
					cell.mosaic = true
					cell.separated = r >= 0xE040
					cell.sext = byte(r-0xE000) & 0x3F
				}
				rows[row] = append(rows[row], cell)
			}
		}
	}

	if m := ndrSubpageCountRe.FindStringSubmatch(hdrText.String()); m != nil {
		nav.numberOfSubpages, _ = strconv.Atoi(m[2])
	}

	if !foundTxt {
		return nil, nav, fmt.Errorf("no <pre class=\"txt\"> block found")
	}
	return rows, nav, nil
}

/*
the teletext mosaic, which is a 2x3 block like below, can be represented as a byte
[b0][b1]
[b2][b3]
[b4][b5]
in the output byte bits b7 and b5 are always 1 because of the 0x80 and 0x20
bits b0..b4 are passed unchanged (v & 0x1F)
b5 is shifted 1 left to put it in output bit 6
for example:

	input v = 0000 0000 (0x00) outputs as 1111 1111 (0xFF)
	input v = 0011 1111 (0x3F) outputs as 1010 0000 (0xA0)
*/
func ndrSextantToTeletext(v byte) byte {
	return (0x20 | (v & 0x1F) | ((v & 0x20) << 1)) | 0x80
}

// Turns one row of cells into 40 bytes of teletext data. Control codes are placed in blank cells:
//  1. a blank cell that must show a different background gets New Background (or Black Background);
//  2. a blank cell right before a background change gets the colour code that New Background copies;
//  3. otherwise a blank cell gets the colour / mosaic code the next visible character needs,
//     or the Separated / Contiguous Graphics code a mosaic character needs.
//
// The weather maps (665/666) change colour between adjacent mosaics without a blank cell in between
// (the broadcast uses Hold Graphics for that). There one mosaic cell is given up for the control code,
// preferably one that repeats its left neighbour, so colours don't bleed into the next region.
func ndrEncodeRow(cells []ndrCell) []byte {
	out := newPageBuffer(1)[0]

	// Pad short rows with the attributes of the last cell, so the padding doesn't need control codes
	for len(cells) < 40 {
		pad := ndrCell{ch: ' ', fg: TCC_ALPHA_WHITE, bg: TCC_ALPHA_BLACK}
		if len(cells) > 0 {
			pad.fg, pad.bg = cells[len(cells)-1].fg, cells[len(cells)-1].bg
		}
		cells = append(cells, pad)
	}

	fg, bg, mosaic, separated := byte(TCC_ALPHA_WHITE), byte(TCC_ALPHA_BLACK), false, false

	// Does cell n need a control code with the current state?
	needsCode := func(n ndrCell) bool {
		return n.fg != fg || n.mosaic != mosaic || (n.mosaic && n.separated != separated)
	}
	// Returns the control code cell n needs and updates the state accordingly
	codeFor := func(n ndrCell) byte {
		if n.fg != fg || n.mosaic != mosaic {
			fg, mosaic = n.fg, n.mosaic
			if n.mosaic {
				return TCC_MOSAIC_BLACK + n.fg
			}
			return TCC_ALPHA_BLACK + n.fg
		}
		separated = n.separated
		if n.separated {
			return TCC_SEPERATED_MOSAICS
		}
		return TCC_CONTINUOUS_MOSAICS
	}

	for i := 0; i < 40; i++ {
		c := cells[i]

		if !c.blank() {
			if c.mosaic && c.bg == bg {
				// No blank cell was left for the code this mosaic needs: use this cell for it
				if needsCode(c) {
					out[i] = codeFor(c)
					continue
				}
				// The next mosaic will need a code but has no blank cell before it: if this cell
				// repeats its left neighbour, give this one up for the code (hardly noticeable)
				if i+1 < 40 && i > 0 {
					n, prev := cells[i+1], cells[i-1]
					if !n.blank() && n.mosaic && n.bg == bg && needsCode(n) &&
						prev.mosaic && prev.sext == c.sext && prev.separated == c.separated && prev.fg == c.fg {
						out[i] = codeFor(n)
						continue
					}
				}
			}
			if c.mosaic {
				out[i] = ndrSextantToTeletext(c.sext)
			} else {
				out[i] = zdfEncodeChar(c.ch)
			}
			continue
		}

		// 1. This cell has to show a new background
		if c.bg != bg {
			switch {
			case c.bg == TCC_ALPHA_BLACK:
				out[i] = TCC_BLACK_BACKGROUND
				bg = c.bg
			case fg == c.bg:
				out[i] = TCC_NEW_BACKGROUND
				bg = c.bg
			default:
				// No free cell was left before this one: set the colour now, New Background
				// follows in the next blank cell (one cell late, but better than nothing)
				out[i] = TCC_ALPHA_BLACK + c.bg
				fg, mosaic = c.bg, false
			}
			continue
		}

		// 2. The next cell needs a new (non-black) background: set the colour New Background copies
		if i+1 < 40 {
			next := cells[i+1]
			if next.bg != bg && next.bg != TCC_ALPHA_BLACK && fg != next.bg {
				out[i] = TCC_ALPHA_BLACK + next.bg
				fg, mosaic = next.bg, false
				continue
			}
		}

		// 3. Set colour / mode for the next visible character, unless a background change comes first.
		//    Colour goes first; if it also needs Separated/Contiguous Graphics, the next blank cell gets that.
		j := i + 1
		for j < 40 && cells[j].blank() && cells[j].bg == bg {
			j++
		}
		if j < 40 && !cells[j].blank() && cells[j].bg == bg && needsCode(cells[j]) {
			out[i] = codeFor(cells[j])
		}
	}
	return out
}

func getNDRDate() string {
	now := time.Now()
	days := map[string]string{"Sun": "So", "Mon": "Mo", "Tue": "Di", "Wed": "Mi", "Thu": "Do", "Fri": "Fr", "Sat": "Sa"}
	return fmt.Sprintf("\x07%s %02d.%02d.\x02%s", days[now.Format("Mon")], now.Day(), now.Month(), now.Format("15:04:05"))
}
