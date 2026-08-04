package main

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"
)

// SVT Text
var svtColorMap = map[string]byte{
	"Bl": 0, // Black
	"R":  1, // Red
	"G":  2, // Green
	"Y":  3, // Yellow
	"Bx": 4, // Blue
	"M":  5, // Magenta
	"C":  6, // Cyan
	"W":  7, // White
}

// This date/time stamp will be fetched from within the HTML page; it is more accurate than using the current date/time from the system
var dateAdded string

func svttextGetTeletexPage(pageNr string) bool {
	parts := strings.Split(pageNr, "-")
	currentPage = parts[0]
	//url := fmt.Sprintf("https://api.texttv.nu/api/get/%s?app=teletext64u", parts[0])
	url := fmt.Sprintf("https://l.texttv.nu/db/%s?app=teletext64u", currentPage)

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

	// parse all rows; also gives information about the number of subpages
	rows, nav, err := parseSVTRows(resp.Body, parts[1])
	if err != nil {
		fmt.Println(err.Error())
		return true
	}
	if len(rows) > 24 {
		rows = rows[:24]
	}

	// optional directives for subpage navigation
	ps := ""
	ns := ""
	subPageIndicator := ""
	nav.nextSubpage = nav.prevSubpage + 2
	if nav.numberOfSubpages > 1 {
		subPageIndicator = "(" + strconv.Itoa(nav.prevSubpage+1) + "/" + strconv.Itoa(nav.numberOfSubpages) + ")"
		ps, ns, _ = getPrevNextSubpage(parts[0], nav)
	}

	var output []byte
	output = append(output, []byte(fmt.Sprintf(
		"pn=p_\npn=n_\n%v%vftl=100-0\nftl=300-0\nftl=400-0\nftl=700-0\n<pre>",
		ps, ns))...)

	// create row 0 / header line
	row0 := make([]byte, 40)
	for i := range row0 {
		row0[i] = 0x20
	}
	dt := getSwedishDate()
	start := 6
	row0[start] = byte(TCC_ALPHA_WHITE)
	stationPage := "SVT Text " + currentPage
	copy(row0[start+1:], []byte(stationPage))
	copy(row0[start+15:], stringToLatin1Bytes(dt))
	row0[start+25] = byte(TCC_ALPHA_YELLOW)

	rows[23][0] = TCC_ALPHA_RED
	// 2 variants of the fastext layout: If we have subpages, we need some room for the
	// subpage indicator bottom right
	if nav.numberOfSubpages > 1 {
		copy(rows[23][1:], "Nyheter  Sport  V\x7Bder  Inneh\x7Dll")
		rows[23][9] = TCC_ALPHA_GREEN
		rows[23][16] = TCC_ALPHA_YELLOW
		rows[23][22] = TCC_ALPHA_CYAN
		rows[23][32] = TCC_ALPHA_WHITE
	} else {
		rows[23][0] = TCC_ALPHA_RED
		copy(rows[23][1:], "Nyheter    Sport     V\x7Bder     Inneh\x7Dll")
		rows[23][11] = TCC_ALPHA_GREEN
		rows[23][21] = TCC_ALPHA_YELLOW
		rows[23][28] = TCC_ALPHA_CYAN
	}

	if subPageIndicator != "" && len(rows) > 0 {
		copy(rows[23][40-len(subPageIndicator):], []byte(subPageIndicator))
	}

	// add teletext page
	output = append(output, row0...)
	for _, r := range rows {
		output = append(output, r...)
	}
	output = append(output, []byte("</pre>")...)

	savePage(DirSVT, pageNr, output)
	return true
}

// Every line starts with a <span class="bgBl"> </span>, indicating an empty black space; we don't need this
var ignoreFirst bool
var checkText bool
var doubleHeight bool

// If we encounter this: <span class=\"bgB\">  <\/span>, we to translate this to 2 control codes: 0x04 (blue) and 0x1D (new background color)
// this already takes to positions in telext, so we need to skipt the 2 spaces between the spans and set skipCount to 2
var skipCount int
var prevBgCode byte
var prevFgCode byte
var prevFGMosaicCode byte
var huidigeRij int // aka currentRow

const MAXRIJ = 25

func parseSVTRows(body io.ReadCloser, subpageStr string) ([][]byte, NavignationInfo, error) {
	defer body.Close()

	var nav NavignationInfo

	ignoreFirst = true
	checkText = false
	doubleHeight = false
	skipCount = 0
	prevBgCode = TCC_ALPHA_BLACK
	prevFgCode = 0xFF
	prevFGMosaicCode = 0xFF
	huidigeRij = 0

	// Convert subpage string to 0-indexed count
	targetSub, _ := strconv.Atoi(subpageStr)
	if targetSub > 0 {
		targetSub-- // "1" becomes index 0
	}

	// Initialize empty teletext page
	pageBuffer := newPageBuffer(24)

	rawData, err := io.ReadAll(body)
	if err != nil {
		return nil, nav, err
	}
	// Turns \" into " and \/ into /
	cleanHTML := strings.ReplaceAll(string(rawData), "\\", "")

	// In SVT Text every pages between 100..899 always exists; we have to check this text; bail out if page is not available
	if strings.Contains(cleanHTML, "Sidan ej") {
		return nil, nav, errors.New("page not available")
	}

	z := html.NewTokenizer(strings.NewReader(cleanHTML))

	rootCount := -1
	currentRow := -1
	currentCol := 0
	inTargetSubpage := false

	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			if z.Err() == io.EOF {
				break
			}
			return nil, nav, z.Err()
		}

		token := z.Token()

		switch tt {
		case html.StartTagToken:
			if token.Data == "p" {
				z.Next()
				text := string(z.Text())
				if strings.Contains(text, "Date_added:") {
					dateAdded = strings.TrimPrefix(text, "Date_added: ")
				}
			}
			// Find the subpage we need
			if token.Data == "div" {
				for _, attr := range token.Attr {
					if attr.Key == "class" && attr.Val == "root" {
						rootCount++
						if rootCount == targetSub {
							inTargetSubpage = true
							nav.prevSubpage = rootCount
							break
						} else {
							inTargetSubpage = false
						}
					}
				}
				nav.numberOfSubpages = rootCount + 1
			}

			if !inTargetSubpage {
				continue
			}

			// find a line (row)
			if token.Data == "span" {
				isLine := false
				var classes string
				var styles string
				//var styles string
				for _, attr := range token.Attr {
					if attr.Key == "class" {
						classes = attr.Val
						// Skip toprow; we always create our own
						if strings.Contains(classes, "line") && !strings.Contains(classes, "toprow") {
							isLine = true
						}
					}
					if attr.Key == "style" {
						styles = attr.Val
					}
				}

				if isLine {
					currentRow++
					huidigeRij = currentRow
					currentCol = 0
					ignoreFirst = true
					doubleHeight = false
					skipCount = 0
					prevBgCode = TCC_ALPHA_BLACK
					prevFgCode = 0xFF
					prevFGMosaicCode = 0xFF
					if currentRow >= 24 {
						return pageBuffer, nav, nil
					}
				}

				// handle background, foreground colors and double height
				if currentRow >= 0 && currentRow < 24 {
					handleSVTStyles(classes, pageBuffer[currentRow], &currentCol)
					handleMosaics(styles, pageBuffer[currentRow], &currentCol)
				}
			}

		case html.TextToken:
			if inTargetSubpage && currentRow >= 0 && currentRow < 24 {
				text := token.Data

				// Swedish unicode replacements
				text = strings.ReplaceAll(text, "u00a0", " ")
				text = strings.ReplaceAll(text, "u00c4", "Ä")
				text = strings.ReplaceAll(text, "u00e4", "ä")
				text = strings.ReplaceAll(text, "u00c5", "Å")
				text = strings.ReplaceAll(text, "u00e5", "å")
				text = strings.ReplaceAll(text, "u00d6", "Ö")
				text = strings.ReplaceAll(text, "u00f6", "ö")
				text = strings.ReplaceAll(text, "u00e9", "é")

				// Text to display? Check if we need a color control code
				if strings.TrimSpace(text) != "" {
					// previous character was a mosaic
					if prevFGMosaicCode != 0xFF {
						// force use text color control code
						prevFgCode = 0xFF
					}
				}

				if checkText {
					checkText = false
					if strings.TrimSpace(text) == "" {
						if doubleHeight {
							doubleHeight = false
							skipCount = 2
						} else {
							skipCount = 1
						}
					} else {
						// If text is not empty, we have to insert a TCC_ALPHA_WHITE
						// If we don't do this the text will not be visible
						pageBuffer[currentRow][currentCol] = TCC_ALPHA_WHITE
						currentCol++
						prevFgCode = TCC_ALPHA_WHITE
					}
				}

				for _, r := range text {
					if currentCol < 40 {
						if skipCount == 0 {
							pageBuffer[currentRow][currentCol] = encodeSVTChar(r)
							currentCol++
						} else {
							skipCount--
						}
					}
				}
			}
		}
	}

	return pageBuffer, nav, nil
}

func handleSVTStyles(classes string, row []byte, col *int) {
	parts := strings.Fields(classes)
	var fg, bg, fgMosaic string

	for _, p := range parts {
		// Double Height
		if p == "DH" {
			doubleHeight = true
			row[*col] = TCC_DOUBLE_HEIGHT
			*col += 1
			skipCount = 1
		}
		if strings.HasPrefix(p, "bg") {
			bg = strings.TrimPrefix(p, "bg")
			if bg == "B" {
				bg = "Bx"
			}
			if p == "bgImg" {
				fgMosaic = fg
			}
		} else if len(p) == 1 { // SVT uses single chars for FG colors
			fg = p
			if huidigeRij < MAXRIJ {
				if fg == "B" {
					fg = "Bx"
				}
			}
		}
	}

	// If background is defined (e.g., bgBl)
	if ignoreFirst { //&& bg == "Bl"
		ignoreFirst = false
		prevBgCode = svtColorMap["Bl"]
		return
	} else {
		if bgCode, ok := svtColorMap[bg]; ok {
			if *col < 39 {
				// e.g. <span class=\"bgB\">  <\/span> followed by a <span class=\"bgB W\">
				// in this situation we alread have the bg set; we only need to set the new fg
				// so ignore if equal
				if bgCode != prevBgCode {
					row[*col-1] = bgCode
					row[*col] = TCC_NEW_BACKGROUND
					*col++
					if doubleHeight {
						row[*col] = TCC_DOUBLE_HEIGHT
						*col++
					}
					//skipCount = 1
					checkText = true
					prevBgCode = bgCode
				}
			}
		}
	}

	// The mosaic header on these pages is wild
	// E.g. page 100: the 2nd row (line=1) alternates continuesly between blue and white; this has to be ignored to display the proper graphics
	switch currentPage {
	case "100", "101", "102", "103", "104", "105", "400", "700", "701":
		if *col > 3 && *col < 27 && huidigeRij < 4 {
			// weather page needs some minor corrections
			if currentPage == "400" {
				if huidigeRij == 1 {
					row[18] = 0xF3
					row[19] = 0xFF
					row[22] = 0xA1
					row[23] = 0x20
				}
			}
			return
		}
	// The football player color, mosaic and contrl codes on these sport pages are a total mess. We have to manually reconstruct them
	case "300", "301", "302":
		if *col > 3 && *col < 25 && huidigeRij == 0 {
			return
		}
		if huidigeRij == 0 && *col >= 25 {
			row[20] = TCC_MOSAIC_YELLOW
			row[21] = 0xE0
			row[22] = 0xFF
			row[23] = 0xF0
			row[24] = 0xF0
			row[25] = 0xF0
			row[26] = 0x07
		}
		if *col > 3 && huidigeRij >= 1 && huidigeRij < 5 {
			if huidigeRij == 1 {
				row[19] = TCC_MOSAIC_YELLOW
				row[22] = 0xEF
				row[23] = 0xFF
				row[24] = 0xF4
				row[25] = 0x20
			}
			if huidigeRij == 2 {
				row[19] = TCC_MOSAIC_YELLOW
				row[22] = TCC_MOSAIC_YELLOW
			}
			if huidigeRij == 3 {
				row[24] = TCC_MOSAIC_YELLOW
			}
			return
		}
	case "401":
		if huidigeRij == 1 {
			row[19] = TCC_MOSAIC_YELLOW
			return
		}
		if huidigeRij == 2 {
			row[16] = TCC_MOSAIC_BLUE
			row[17] = 0xEA
			row[18] = 0xFF
			row[19] = 0x07
			return
		}
		if huidigeRij == 8 {
			row[7] = TCC_NEW_BACKGROUND
			row[8] = TCC_ALPHA_BLUE
		}
		if huidigeRij == 12 {
			row[4] = TCC_NEW_BACKGROUND
			row[5] = TCC_ALPHA_WHITE
			row[9] = TCC_MOSAIC_WHITE
			row[10] = TCC_ALPHA_BLACK
			row[11] = TCC_NEW_BACKGROUND
			row[12] = 0x20
			row[16] = TCC_MOSAIC_RED
		}
		if huidigeRij == 13 {
			row[3] = TCC_MOSAIC_RED
		}
		if huidigeRij == 16 {
			row[2] = TCC_MOSAIC_RED
		}
		if huidigeRij == 17 {
			row[16] = TCC_MOSAIC_YELLOW
			row[17] = 0xEA
			row[18] = 0xFF
			row[19] = 0x07
			return
		}
	// corrections for the Italian flag
	case "500":
		if huidigeRij == 0 && *col > 11 {
			// reconstruct the italian flag
			row[0] = TCC_ALPHA_GREEN
			row[1] = TCC_NEW_BACKGROUND
			row[2] = TCC_ALPHA_WHITE
			row[3] = TCC_NEW_BACKGROUND
			row[4] = TCC_ALPHA_RED
			row[5] = TCC_NEW_BACKGROUND
			row[6] = TCC_ALPHA_BLUE
			row[7] = TCC_NEW_BACKGROUND
			row[8] = TCC_DOUBLE_HEIGHT
			row[9] = TCC_ALPHA_CYAN
			row[11] = 'S'
			return
		}
		if huidigeRij == 1 {
			// reconstruct the italian flag
			row[0] = TCC_ALPHA_GREEN
			row[1] = TCC_NEW_BACKGROUND
			row[2] = TCC_ALPHA_WHITE
			row[3] = TCC_NEW_BACKGROUND
			row[4] = TCC_ALPHA_RED
			row[5] = TCC_NEW_BACKGROUND
			row[6] = TCC_ALPHA_BLUE
			row[7] = TCC_NEW_BACKGROUND
			row[9] = TCC_ALPHA_CYAN
			row[11] = 'S'
			return
		}
	// A fix for 'UTBILDNINGSRADION' (aka EDUCATIONAL RADIO?)
	// Note: this is a mess on https://texttv.nu/801
	// I made it look like: https://www.svt.se/text-tv/801
	case "801":
		if huidigeRij == 1 && *col > 11 {
			row[9] = TCC_ALPHA_RED
			row[10] = TCC_NEW_BACKGROUND
			return
		}
		if huidigeRij == 2 && *col > 11 {
			row[8] = 0x20
			row[9] = TCC_ALPHA_RED
			row[10] = TCC_NEW_BACKGROUND
			row[16] = byte('U')
			copy(row[15:], "\x07UTBILDNINGSRADION       ")
		}
		if huidigeRij == 3 {
			row[9] = TCC_ALPHA_RED
			row[10] = TCC_NEW_BACKGROUND
			row[14] = TCC_MOSAIC_WHITE
			for i := 15; i < 34; i++ {
				row[i] = 0xA3
			}
			for i := 34; i < 40; i++ {
				row[i] = 0x20
			}
		}
	}
	currentPageInt, _ := strconv.Atoi(currentPage)
	if currentPageInt > 500 && currentPageInt <= 550 {
		if huidigeRij == 0 && *col > 11 {
			// reconstruct the italian flag
			row[0] = TCC_ALPHA_GREEN
			row[1] = TCC_NEW_BACKGROUND
			row[2] = TCC_ALPHA_WHITE
			row[3] = TCC_NEW_BACKGROUND
			row[4] = TCC_ALPHA_RED
			row[5] = TCC_NEW_BACKGROUND
			row[6] = TCC_ALPHA_BLUE
			row[7] = TCC_NEW_BACKGROUND
			row[9] = TCC_ALPHA_CYAN
			row[11] = 'S'
			return
		}
	}

	// If foreground is defined (e.g., class="bgB W")
	if fgCode, ok := svtColorMap[fg]; ok {
		// Apply color control code only when there is a color change OR there is a switch from mosaic to text mode
		if fgCode != prevFgCode || (fgMosaic == "" && prevFGMosaicCode != 0xFF) {
			if *col > 0 && *col < 40 {
				row[*col-1] = fgCode
				prevFgCode = fgCode
			}
		}
		if fgMosaic != "" {
			prevFGMosaicCode = fgCode
		}
	}
}

func handleMosaics(classes string, row []byte, col *int) {
	parts := strings.Fields(classes)
	var gifStr string
	var mosaic byte = 0x00

	// No fun! I had to manually determince the mosiac charachter code for each .GIF image
	// (after doing this for a while I wrote a helper program for this)
	var mosaicMap = map[string]byte{
		"4166044020": 0xA2, "207576990": 0xA3, "2267014944": 0xA5,
		"1460303617": 0xA7, "3987931972": 0xAA, "1227236920": 0xAA,
		"723504262": 0xAC, "4249453864": 0xAF, "299620102": 0xAF,
		"2030688620": 0xB0, "3713433556": 0xB0, "2754943555": 0xB4,
		"2015754887": 0xB5, "2964044975": 0xB5, "2862847544": 0xBF,
		"2335531887": 0xE0, "693852549": 0xE8, "1270603014": 0xEA,
		"2201328430": 0xEA, "2594562150": 0xEB, "282174899": 0xED,
		"2762748738": 0xEF, "2218724507": 0xAC, "294742777": 0xF0,
		"2327991958": 0xF5, "1760051201": 0xFA, "2413702233": 0xFC,
		"167497510": 0xFE, "1074033251": 0xFF, "1254105466": 0xF0,
		"2681114375": 0xA7, "750680978": 0xA1, "3298983629": 0xEE,
		"2308811616": 0xBD, "3771534768": 0xA3, "15963642": 0xEF,
		"3288266310": 0xA5, "3188198897": 0xA8, "3618463797": 0xA4,
		"2881270998": 0xAD, "872158518": 0xAC, "4082209591": 0xA6,
		"880409429": 0xFD, "3931275958": 0xBE, "3547727352": 0xF7,
		"1559180511": 0xF3, "925899746": 0xB7, "4244846807": 0xF0,
		"1028566380": 0xA2, "2296503594": 0xA1, "1739010369": 0xE0,
		"2790421332": 0xF0, "2353048447": 0xAB, "2140796170": 0xEB,
		"3785335171": 0xE8, "999369151": 0xA7, "3965831124": 0xEF,
		"3838981461": 0xF4, "1118560998": 0xB3, "610948841": 0xA2,
		"3147580979": 0xA3, "3896730824": 0xFD, "2509998914": 0xB0,
		"1840924899": 0xE8, "1091112751": 0xB4, "3772511681": 0xAB,
		"739691859": 0xF4, "1087885570": 0xF8, "1056054768": 0xE5,
		"225196657": 0xBA, "1954418500": 0xFF, "1665957495": 0xFC,
		"2913233310": 0xFE, "4050100045": 0xFD, "251408512": 0xA7,
		"2185071352": 0xFC, "1326555685": 0xF0, "3037313580": 0xFD,
		"3215696164": 0xF4, "3387636925": 0xFA, "1994053858": 0xB4,
		"2287478073": 0xE8, "1219799629": 0xF5, "2642197907": 0xEA,
		"2934086162": 0xB5, "1625865678": 0xA7, "1164105659": 0xE0,
		"3806973766": 0xA1, "2190446388": 0xAA, "2156528839": 0xEA,
		"2537420265": 0xFC, "3585010416": 0xB0, "3826504151": 0xFA,
		"3150678580": 0xB7, "3352595016": 0xEA, "3609107780": 0xA7,
		"3782488817": 0xA2, "3287848953": 0xE0, "3138777730": 0xB7,
		"2693613557": 0xAA, "4098534857": 0xB5, "1685294852": 0xA1,
		"1250598021": 0xF0, "1339760422": 0xFA, "1460540445": 0xA3,
	}

	for _, p := range parts {
		// Mosaic character lookup via .gif image filename
		if strings.HasPrefix(p, "url(https://l.texttv.nu/storage/chars/") {
			gifStr = strings.TrimPrefix(p, "url(https://l.texttv.nu/storage/chars/")
			gifStr = strings.TrimSuffix(gifStr, ".gif)")
			if val, ok := mosaicMap[gifStr]; ok {
				mosaic = val
			}

			if mosaic > 0x00 && *col < 40 {
				if row[*col-1] == 0x20 {
					row[*col-1] = 0x17
				}
				if row[*col-1] < 0x08 {
					row[*col-1] = row[*col-1] + 0x10
					prevFGMosaicCode = prevFgCode
				}
				row[*col] = mosaic
				*col++
				skipCount = 1
			}
		}
	}
}

func encodeSVTChar(r rune) byte {
	switch r {
	case 'Ä':
		return 0x5B
	case 'Ö':
		return 0x5C
	case 'Å':
		return 0x5D
	case 'ä':
		return 0x7B
	case 'ö':
		return 0x7C
	case 'å':
		return 0x7D
	case 'é':
		return 0xE9
	// Below: Denmark
	case 'æ':
		return 0xE6
	case 'Æ':
		return 0xC6
	case 'ø':
		return 0xF8
	case 'Ø':
		return 0xD8
	case 'Ü':
		return 0xDC
	default:
		if r < 128 {
			return byte(r)
		}
		return 0x20
	}
}

func getSwedishDate() string {
	months := map[string]string{"Jan": "jan", "Feb": "feb", "Mar": "mar", "Apr": "apr", "May": "maj", "Jun": "jun", "Jul": "jul", "Aug": "aug", "Sep": "sep", "Oct": "okt", "Nov": "nov", "Dec": "dec"}
	days := map[string]string{
		"Sun": "S\x7Cn", // Sön
		"Mon": "M\x7Dn", // Mån
		"Tue": "Tis",
		"Wed": "Ons",
		"Thu": "Tor",
		"Fri": "Fre",
		"Sat": "L\x7Cr", // Lör
	}

	// we need the system time to compare the date from the html against the current date
	now := time.Now()

	layout := "2006-01-02 15:04:05"
	// we get something like this: 2022-08-23 13:30:24. We have to remove the . (dot)
	dateAdded = dateAdded[:len(dateAdded)-1]
	t, err := time.Parse(layout, dateAdded)
	if err != nil {
		fmt.Println("getSwedishDate: could not read the date; using the systems date/time")
		return fmt.Sprintf("%s %02d %s %s", days[now.Format("Mon")], now.Day(), months[now.Format("Jan")], now.Format("15:04:05"))
	}

	year, _ := strconv.Atoi(dateAdded[:4])
	// The return value depends of the age
	if now.Year() == year {
		return fmt.Sprintf("%s %02d %s %s",
			days[t.Format("Mon")],
			t.Day(),
			months[t.Format("Jan")],
			dateAdded[len(dateAdded)-8:],
		)
	} else {
		// A date from a previous year: use the DD-MM-YYYY format
		return fmt.Sprintf("%s %s", dateAdded[8:10]+"-"+dateAdded[5:7]+"-"+dateAdded[:4], dateAdded[len(dateAdded)-8:])
	}
}
