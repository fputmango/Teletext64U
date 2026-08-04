package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"

	"golang.org/x/net/html"
)


// --- DR TEKST-TV ---

func drteksttvGetTeletexPage(pageNr string) bool {
	parts := strings.Split(pageNr, "-")
	url := fmt.Sprintf("https://www.dr.dk/cgi-bin/fttx1.exe/%s/%s", parts[0], parts[1])
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

	row0 := make([]byte, 40)
	for i := range row0 {
		row0[i] = 0x20
	}
	var nav NavignationInfo
	rows, nav, err := parseDRRows(resp.Body, parts[0], parts[1])
	if err != nil {
		fmt.Println(err.Error())
		return true
	}

	pp := ""
	np := ""
	ps := ""
	ns := ""
	if nav.numberOfSubpages > 1 {
		ps, ns, _ = getPrevNextSubpage(parts[0], nav)
	}
	pp, np = buildPageNavDirectives(nav.prevPage, nav.nextPage)

	// Note: the ftl - fastext links are fixed for now; it could be made dynamic in a future release
	// Nyheder (110), Sport (200), TV (300) and Vejret (400)
	// aka: nieuws, sport, TV, weather
	var output []byte
	output = append(output, []byte(fmt.Sprintf(
		"%v%v%v%vftl=110-0\nftl=200-0\nftl=300-0\nftl=400-0\n<pre>", pp, np, ps, ns))...)

	for _, r := range rows {
		output = append(output, r...)
	}

	output = append(output, []byte("</pre>")...)
	savePage(DirDR, pageNr, output)
	return true
}


func parseDRRows(body io.ReadCloser, pageNr string, subPageNr string) ([][]byte, NavignationInfo, error) {
	defer body.Close()

	var nav NavignationInfo

	// Initialize 25x40 grid with spaces
	pageBuffer := newPageBuffer(25)

	rawData, err := io.ReadAll(body)
	if err != nil {
		return nil, nav, err
	}

	// In DR Tekst-TV every page between 100..899 always exists; we have to check this text; bail out if page is not available
	// 'Denne side er desværre ikke tilgængelig'
	if strings.Contains(string(rawData), "Denne side er") {
		return nil, nav, errors.New("page not available")
	}

	currentPageInt, _ := strconv.Atoi(pageNr)
	currentSubPageInt, _ := strconv.Atoi(subPageNr)

	z := html.NewTokenizer(strings.NewReader(string(rawData)))

	inPre := false
	colorCodeWritten := false
	dashDetected := false
	currentRow := 0
	currentCol := 0

	currentMapName := ""
	var subPages []int

	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			break
		}

		token := z.Token()

		switch tt {
		case html.StartTagToken:
			if token.Data == "pre" {
				inPre = true
			} else if token.Data == "map" {
				for _, attr := range token.Attr {
					if attr.Key == "name" {
						currentMapName = attr.Val
					}
				}
			} else if token.Data == "area" {
				var hrefVal string
				for _, attr := range token.Attr {
					if attr.Key == "href" {
						hrefVal = attr.Val
					}
				}
				if hrefVal != "" {
					segments := strings.Split(strings.Trim(hrefVal, "/"), "/")
					if currentMapName == "FPMap1" {
						// FPMap1 handles subpages (e.g., "/cgi-bin/fttx1.exe/601/3")
						if len(segments) >= 4 {
							if subIdx, err := strconv.Atoi(segments[3]); err == nil {
								subPages = append(subPages, subIdx)
							}
						}
					} else if currentMapName == "FPMap0" {
						// FPMap0 handles main pages (e.g., "/cgi-bin/fttx1.exe/600")
						if len(segments) >= 3 {
							if targetPage, err := strconv.Atoi(segments[2]); err == nil {
								if targetPage < currentPageInt {
									nav.prevPage = targetPage
								} else if targetPage > currentPageInt {
									nav.nextPage = targetPage
								}
							}
						}
					}
				}
			} else if token.Data == "a" && inPre {
				if currentCol > 0 {
					currentCol--
				}
				if dashDetected {
					dashDetected = false
					writeToBuffer(pageBuffer, &currentRow, &currentCol, '-')
				} else {
					writeToBuffer(pageBuffer, &currentRow, &currentCol, TCC_ALPHA_YELLOW)
				}
			}

		case html.EndTagToken:
			if token.Data == "pre" {
				inPre = false
			} else if token.Data == "map" {
				currentMapName = ""
			} else if token.Data == "a" && inPre {
				writeToBuffer(pageBuffer, &currentRow, &currentCol, TCC_ALPHA_WHITE)
				colorCodeWritten = true
			}

		case html.TextToken:
			if inPre {
				text := token.Data
				for _, r := range text {
					//fmt.Printf("[Debug] Row: %d Col: %d Char: %q\n", currentRow, currentCol, r)
					if r == '\n' {
						//if currentCol > 0 || currentRow > 0 {
						currentRow++
						currentCol = 0
						colorCodeWritten = false
						//}
						continue
					}
					if currentRow >= 24 {
						break
					}
					if colorCodeWritten && r == '-' {
						colorCodeWritten = false
						dashDetected = true
						continue
					}
					if colorCodeWritten && r == ' ' {
						colorCodeWritten = false
						continue
					}
					colorCodeWritten = false
					writeToBuffer(pageBuffer, &currentRow, &currentCol, encodeSVTChar(r))
				}
			}
		}
	}

	// Compute subpage fields based on collected subpages
	if len(subPages) > 0 {
		// The number of subpages equals the unique/total variants provided by the map links
		// (Usually, Teletext lists the alternative subpages here)
		nav.numberOfSubpages = len(subPages) + 1 // +1 includes the current active subpage itself

		for _, sub := range subPages {
			if sub < currentSubPageInt {
				nav.prevSubpage = sub
			} else if sub > currentSubPageInt {
				nav.nextSubpage = sub
			}
		}
	}

	// Because we parsed plain text, we didn't get any color information, except
	// the <a href> page links we made yellow
	// We have to do A LOT of post-fix color inserts on the various page styles

	insertDRTekstTVLogo := func() {
		// DR logo mosaics
		// I recreated the logo with this web based teletext editor:
		// https://zxnet.co.uk/teletext/editor/#0:QIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgL_0e9p_R62iBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECAv_4fkv9HtYIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIC6BAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECA:PS=0:RE=0:zx=EI00
		// And exported a binary dump and copied the relevant bytes into these slices
		logoRow1 := []byte{0x17, 0x7F, 0x23, 0x6F, 0x34, 0x7F, 0x23, 0x6B, 0x34, 0x07}
		logoRow2 := []byte{0x17, 0x7F, 0x70, 0x7E, 0x25, 0x7F, 0x23, 0x6D, 0x30, 0x07}
		copy(pageBuffer[2][0:], logoRow1)
		copy(pageBuffer[3][0:], logoRow2)
	}

	whiteSeperatedBoxes := func(row int) {
		// replace color control codes yellow-> red; white->black
		for i := 0; i < 39; i++ {
			if pageBuffer[row][i] == TCC_ALPHA_YELLOW {
				pageBuffer[row][i] = TCC_ALPHA_RED
			}
			if pageBuffer[row][i] == TCC_ALPHA_WHITE {
				pageBuffer[row][i] = TCC_ALPHA_BLACK
			}
		}
		pageBuffer[row][1] = TCC_NEW_BACKGROUND

		// dynamically determine vertical seperator positions
		re := regexp.MustCompile(`\b[1-8][0-9]{2}\b`)
		allPageNumbers := re.FindAllStringIndex(string(pageBuffer[row]), -1)
		count := 0
		for _, m := range allPageNumbers {
			count++
			if count > 1 && pageBuffer[row][m[0]-1] != '-' && pageBuffer[row][m[0]-2] == 0x20 {
				pageBuffer[row][m[0]-3] = TCC_MOSAIC_BLACK
				pageBuffer[row][m[0]-2] = 0x35
			}

		}
	}

	// if row == 0 it draws the big block, else a single row block
	bottomBlock := func(tccMosaicColor byte, row int) {
		if row == 0 {
			pageBuffer[21][0] = tccMosaicColor
			pageBuffer[21][1] = 0x7C
			pageBuffer[21][2] = TCC_HOLD_MOSAICS
			pageBuffer[23][0] = tccMosaicColor
			pageBuffer[23][1] = 0x2F
			pageBuffer[23][2] = TCC_HOLD_MOSAICS
			for i := 3; i < 40; i++ {
				pageBuffer[21][i] = tccMosaicColor
				pageBuffer[23][i] = tccMosaicColor
			}
		}

		var rowNr int
		if row == 0 {
			rowNr = 22
		} else {
			rowNr = row
		}
		pageBuffer[rowNr][0] = tccMosaicColor
		pageBuffer[rowNr][1] = TCC_NEW_BACKGROUND
		pageBuffer[rowNr][2] = TCC_ALPHA_WHITE

		// find all page number references on row 22 (most of the times only 1, sometimes 2)
		re := regexp.MustCompile(`\b[1-8][0-9]{2}\b`)
		matches := re.FindAllStringIndex(string(pageBuffer[22]), -1)
		for _, loc := range matches {
			pos := loc[0]
			//fmt.Printf("loc=%v page=%v\n", loc[0], string(pageBuffer[22][loc[0]:loc[1]]))
			// the '-' checks are in case the page numbers are nnn-mmm range. Then we
			// don't want to mess around with color control codes
			if pageBuffer[rowNr][pos-1] != '-' {
				// I could only pull the box effect off with this trick
				pageBuffer[rowNr][pos-2] = TCC_ALPHA_YELLOW
				pageBuffer[rowNr][pos-1] = TCC_BLACK_BACKGROUND
			}
			if pos < 36 {
				if pageBuffer[rowNr][pos+3] != '-' && pos+3 < 40 {
					pageBuffer[rowNr][pos+3] = tccMosaicColor
					if pos+4 < 40 {
						pageBuffer[rowNr][pos+4] = TCC_NEW_BACKGROUND
					}
					if pos+5 < 40 {
						pageBuffer[rowNr][pos+5] = TCC_ALPHA_WHITE
					}

				}
			}
		}
		/*
			if row == 0 {
				pageBuffer[23][0] = tccMosaicColor
				pageBuffer[23][1] = 0x2F
				pageBuffer[23][2] = TCC_HOLD_MOSAICS
				for i := 3; i < 40; i++ {
					pageBuffer[23][i] = tccMosaicColor
				}
			}*/
	}

	// post-fix: add white to the header row
	pageBuffer[0][7] = TCC_ALPHA_WHITE
	pageNum, err := strconv.Atoi(pageNr)

	if pageNum == 100 {
		insertDRTekstTVLogo()

		// post fix row 1
		pageBuffer[1][10] = TCC_ALPHA_RED
		pageBuffer[1][11] = TCC_NEW_BACKGROUND
		pageBuffer[1][12] = TCC_ALPHA_WHITE

		// If there is a blank row between two news items, it means the 1st
		// news item should be double height
		if pageBuffer[6][1] == 0x20 {
			pageBuffer[5][0] = TCC_DOUBLE_HEIGHT
		}

		// bottom 2 news items are cyan
		pageBuffer[9][0] = TCC_ALPHA_CYAN
		pageBuffer[10][0] = TCC_ALPHA_CYAN

		// DR1 and DR2 headers
		pageBuffer[12][0] = TCC_ALPHA_CYAN
		pageBuffer[15][0] = TCC_ALPHA_CYAN

		// DR shows the progress of the current TV-programming running
		// They use a line (0x2C) of length 12
		// The first part is red, the second part is yellow
		// This function calculates the time difference in minutes from the running show
		// and the next show. It also calculates the difference between the start of the
		// running show and the current system time. Based in this info it calculates how
		// low the red and yellow lines should be
		drawRedYellowProgressBar := func(row int) {
			layout := "15:04"
			time1 := string(pageBuffer[row+1][1:6])
			time2 := string(pageBuffer[row+2][1:6])
			t1, _ := time.Parse(layout, time1)
			t2, _ := time.Parse(layout, time2)
			diff := t2.Sub(t1)
			totalMinutes := diff.Minutes()
			//fmt.Printf("Difference total: %v minutes\n", totalMinutes)
			now := time.Now()
			t1Today := time.Date(
				now.Year(), now.Month(), now.Day(),
				t1.Hour(), t1.Minute(), 0, 0, now.Location(),
			)
			diff = t1Today.Sub(now)
			minutes := math.Abs(diff.Minutes())
			//fmt.Printf("Difference t1 and now:   %.0f minutes\n", minutes)
			if minutes > totalMinutes {
				minutes = totalMinutes
			}
			pageBuffer[row][5] = TCC_HOLD_MOSAICS
			pageBuffer[row][6] = TCC_MOSAIC_RED
			pageBuffer[row][7] = 0x2C
			// the red/yellow bar is 12 positions long
			numPosRed := int(math.Floor(12 * (minutes / totalMinutes)))
			//fmt.Printf("numPosRed=%v\n", numPosRed)
			for i := 2; i <= numPosRed; i++ {
				pageBuffer[row][6+i] = TCC_MOSAIC_RED // horizontal bar in the middle
			}
			numPosYellow := 12 - numPosRed
			offset := 0
			if numPosRed == 0 {
				// if there is no red bar to draw, we have to put a control code yellow
				// at the position where we put the control code red
				pageBuffer[row][6] = TCC_MOSAIC_YELLOW
				numPosYellow--
				// this offset is needed to prevent from overwriting the 0x2C mosaic at 7
				offset = 1
			}
			//fmt.Printf("numPosYellow=%v\n", numPosYellow)
			for i := 0; i < numPosYellow; i++ {
				pageBuffer[row][7+offset+numPosRed+i] = TCC_MOSAIC_YELLOW //0xAC // horizontal bar in the middle
			}
		}

		drawRedYellowProgressBar(12) // DR1
		drawRedYellowProgressBar(15) // DR2

		// block white bg; red pagelinks; black text
		// consisting if 2 rows with 3 blocks with 2 separators
		whiteSeperatedBoxes(19)
		whiteSeperatedBoxes(20)

		// bottom red block
		bottomBlock(TCC_MOSAIC_RED, 0)
	}

	DRbox := func(boxColor byte, row int, tcc byte) {
		for i := row; i < row+3; i++ {
			pageBuffer[i][0] = boxColor
			pageBuffer[i][1] = TCC_NEW_BACKGROUND
			pageBuffer[i][2] = TCC_ALPHA_WHITE
			pageBuffer[i][7] = TCC_BLACK_BACKGROUND
		}
		// overwrite hidden # characters with spaces
		pageBuffer[row][3] = ' '
		pageBuffer[row][4] = ' '
		pageBuffer[row][6] = tcc
	}

	openRect := func(rectColor byte, row int) {
		// corners mosaics
		pageBuffer[row][31] = 0xB7
		pageBuffer[row][39] = 0xEB
		pageBuffer[row+3][31] = 0xF5
		pageBuffer[row+3][39] = 0xFA
		// left and right bar mosaics
		pageBuffer[row+1][31] = 0xB5
		pageBuffer[row+2][31] = 0xB5
		pageBuffer[row+1][39] = 0xEA
		pageBuffer[row+2][39] = 0xEA
		// horizontal bar mosaics
		for i := 32; i < 39; i++ {
			pageBuffer[row][i] = 0xA3
			pageBuffer[row+3][i] = 0xF0
		}
		// color control codes
		for i := row; i < row+4; i++ {
			pageBuffer[i][30] = rectColor
			if i == row+1 || i == row+2 {
				pageBuffer[i][32] = TCC_ALPHA_WHITE
				pageBuffer[i][38] = rectColor
			}
		}

	}

	if (pageNum >= 101 && pageNum <= 104) || (pageNum >= 314 && pageNum <= 359) || (pageNum >= 502 && pageNum < 530) ||
		(pageNum >= 552 && pageNum < 570) || (pageNum >= 609 && pageNum < 630) {
		if pageNum < 600 {
			DRbox(TCC_ALPHA_RED, 1, TCC_ALPHA_CYAN)
			openRect(TCC_MOSAIC_RED, 1)
		} else {
			DRbox(TCC_ALPHA_BLUE, 1, TCC_ALPHA_CYAN)
			openRect(TCC_MOSAIC_BLUE, 1)
		}
		// make dotted lines blue
		for i := 6; i < 23; i++ {
			posDotStart := strings.Index(string(pageBuffer[i]), "..")
			posDotEnd := strings.LastIndex(string(pageBuffer[i]), "..")
			if posDotStart > 7 && posDotStart < 33 {
				pageBuffer[i][posDotStart-1] = TCC_ALPHA_BLUE
				pageBuffer[i][posDotEnd+2] = TCC_ALPHA_WHITE
			}
		}
		if pageNum > 600 {
			// these pages are not very consistant with the blue on the bottom
			if strings.TrimSpace(string(pageBuffer[23])) == "" {
				bottomBlock(TCC_MOSAIC_BLUE, 0)
			} else {
				bottomBlock(TCC_MOSAIC_BLUE, 23)
			}
		} else {
			if pageNum < 500 || pageNum > 530 {
				bottomBlock(TCC_MOSAIC_RED, 0)
			} else {
				whiteSeperatedBoxes(23)
			}
		}
	}

	// News of DR
	if pageNum == 105 {
		pageBuffer[1][10] = TCC_ALPHA_RED
		pageBuffer[1][29] = TCC_NEW_BACKGROUND
		pageBuffer[1][30] = TCC_ALPHA_WHITE
		pageBuffer[4][10] = TCC_ALPHA_RED
		pageBuffer[9][0] = TCC_ALPHA_CYAN
		pageBuffer[10][0] = TCC_ALPHA_CYAN
		pageBuffer[11][0] = TCC_ALPHA_RED
		insertDRTekstTVLogo()
		whiteSeperatedBoxes(23)
	}

	redWhiteRedBar := func(posWhite, posLastRed int) {
		pageBuffer[1][0] = TCC_ALPHA_RED
		pageBuffer[1][1] = TCC_NEW_BACKGROUND
		pageBuffer[1][2] = TCC_ALPHA_WHITE
		pageBuffer[1][posWhite] = TCC_NEW_BACKGROUND
		pageBuffer[1][posWhite+1] = TCC_ALPHA_BLACK
		if posLastRed > 0 {
			pageBuffer[1][posLastRed] = TCC_ALPHA_RED
			pageBuffer[1][posLastRed+1] = TCC_NEW_BACKGROUND
			pageBuffer[1][posLastRed+2] = TCC_ALPHA_WHITE
		}
	}

	blueWhiteBlueBar := func(posWhite, posLastRed int) {
		if posWhite > 0 {
			pageBuffer[1][0] = TCC_ALPHA_BLUE
			pageBuffer[1][1] = TCC_NEW_BACKGROUND
			pageBuffer[1][2] = TCC_ALPHA_WHITE
			pageBuffer[1][posWhite] = TCC_NEW_BACKGROUND
			pageBuffer[1][posWhite+1] = TCC_ALPHA_BLACK
		} else {
			pageBuffer[1][0] = TCC_NEW_BACKGROUND
			pageBuffer[1][1] = TCC_ALPHA_BLACK
		}
		if posLastRed > 0 {
			pageBuffer[1][posLastRed] = TCC_ALPHA_BLUE
			pageBuffer[1][posLastRed+1] = TCC_NEW_BACKGROUND
			pageBuffer[1][posLastRed+2] = TCC_ALPHA_WHITE
		}
	}

	// Index A-Z pages
	if pageNum >= 106 && pageNum <= 108 {
		// set the header background red - white - red
		redWhiteRedBar(11, 29)
		// track down index letters and apply red background bars
		var oneCharString string
		for i := 2; i < 22; i++ {
			oneCharString = string(pageBuffer[i][0:20])
			oneCharString = strings.Trim(oneCharString, " ")
			if len(oneCharString) == 1 {
				pageBuffer[i][0] = TCC_ALPHA_RED
				pageBuffer[i][1] = TCC_NEW_BACKGROUND
				pageBuffer[i][2] = TCC_ALPHA_WHITE
				pageBuffer[i][19] = TCC_BLACK_BACKGROUND
			}
			oneCharString = string(pageBuffer[i][20:39])
			oneCharString = strings.Trim(oneCharString, " ")
			if len(oneCharString) == 1 || (strings.Contains(oneCharString, "+") && len(oneCharString) < 8) {
				pageBuffer[i][20] = TCC_ALPHA_RED
				pageBuffer[i][21] = TCC_NEW_BACKGROUND
				pageBuffer[i][22] = TCC_ALPHA_WHITE
			}
		}
		whiteSeperatedBoxes(23)
	}

	detectTwoCapitals := func() {
		re := regexp.MustCompile(`\b[A-Z]{2}\b`)
		for i := 2; i < 24; i++ {
			loc := re.FindStringIndex(string(pageBuffer[i]))
			if loc != nil {
				//charCombo := pageBuffer[i][loc[0]:loc[1]]
				//fmt.Printf("Combo found: %v; loc0=%v\n", string(charCombo), loc[0])
				if loc[0] == 5 {
					pageBuffer[i][loc[0]-1] = TCC_ALPHA_CYAN
					pageBuffer[i][loc[0]+2] = TCC_ALPHA_WHITE
				}
			}
		}
	}

	detectThreeCapitals := func() {
		re := regexp.MustCompile(`\b[A-Z]{3}\b`)
		for i := 3; i < 22; i++ {
			loc := re.FindStringIndex(string(pageBuffer[i]))
			if loc != nil {
				pageBuffer[i][loc[0]-1] = TCC_ALPHA_CYAN
				pageBuffer[i][loc[0]+3] = TCC_ALPHA_WHITE
			}
		}
	}

	if pageNum == 109 {
		redWhiteRedBar(15, 0)
		// sport
		rowSport := 15
		pageBuffer[rowSport][0] = TCC_ALPHA_RED
		pageBuffer[rowSport][1] = TCC_NEW_BACKGROUND
		pageBuffer[rowSport][2] = TCC_ALPHA_WHITE
		pageBuffer[rowSport][14] = TCC_ALPHA_CYAN
		pageBuffer[rowSport][15] = TCC_NEW_BACKGROUND
		pageBuffer[rowSport][16] = TCC_ALPHA_BLACK

		// detect any 2 capital character combination, like GO, CY, FO etc. These  should
		// be made cyan.
		detectTwoCapitals()
		whiteSeperatedBoxes(22)
		whiteSeperatedBoxes(23)
	}

	if pageNum >= 110 && pageNum <= 113 {
		redWhiteRedBar(20, 0)
		whiteSeperatedBoxes(23)
	}

	// Find the headline in capitals and make it cyan
	capitalizeHeadline := func() {
		re := regexp.MustCompile(`^[^[:lower:]]*[[:upper:]][^[:lower:]]*$`)
		for i := 2; i < 23; i++ {
			if re.MatchString(string(pageBuffer[i])) {
				pageBuffer[i][0] = TCC_ALPHA_CYAN
			}
		}
	}

	// These are the news pages with the full story
	if (pageNum >= 114 && pageNum < 150) || (pageNum > 150 && pageNum < 178) {
		redWhiteRedBar(15, 31)
		capitalizeHeadline()
		whiteSeperatedBoxes(23)
	}

	// SIREN WARNING & EMERGENCY MESSAGE page
	if pageNum == 150 {
		yellowBlueBanner := func(row int) {
			pageBuffer[row][0] = TCC_ALPHA_YELLOW
			pageBuffer[row][1] = TCC_NEW_BACKGROUND
			pageBuffer[row][2] = TCC_ALPHA_BLUE
		}
		yellowBlueBanner(1)
		yellowBlueBanner(2)
		yellowBlueBanner(22)
		yellowBlueBanner(23)
	}

	if pageNum == 179 {
		redWhiteRedBar(7, 30)
		pageBuffer[1][3] = TCC_DOUBLE_HEIGHT
		copy(pageBuffer[2], pageBuffer[1])
		whiteSeperatedBoxes(23)
	}

	drawSportHeader := func(row int) {
		posCyan := 5
		posWhite := 19

		if row == 1 {
			runes := []rune(string(pageBuffer[row]))
			count := 0
			for i := 0; i < len(runes)-1; i++ {
				if !unicode.IsSpace(runes[i]) && unicode.IsSpace(runes[i+1]) {
					count++
					if count == 1 {
						posCyan = i + 1
					} else if count == 2 {
						posWhite = i + 1
					} else {
						break
					}

				}
			}
		}
		pageBuffer[row][0] = TCC_ALPHA_RED
		pageBuffer[row][1] = TCC_NEW_BACKGROUND
		pageBuffer[row][2] = TCC_ALPHA_WHITE
		pageBuffer[row][posCyan] = TCC_ALPHA_CYAN
		pageBuffer[row][posCyan+1] = TCC_NEW_BACKGROUND
		pageBuffer[row][posCyan+2] = TCC_ALPHA_BLUE
		pageBuffer[row][posWhite] = TCC_ALPHA_WHITE
		pageBuffer[row][posWhite+1] = TCC_NEW_BACKGROUND
		pageBuffer[row][posWhite+2] = TCC_ALPHA_BLACK
	}

	makeDashedLines := func(color byte) {
		for i := 1; i < 18; i++ {
			if pageBuffer[i][39] == '-' {
				pageBuffer[i][0] = color
			}
		}
	}

	// detect scores like 70-91, 5-3 etc. and make then yellow
	// also detect table headers
	detectSportScores := func() {
		reScore := regexp.MustCompile(`\b\d{1,4}-\d{1,4}\b`)
		//		reKVT := regexp.MustCompile(`K\s+V\s+T`)
		reKVT := regexp.MustCompile(`K\s+V\s`)
		for i := 2; i < 23; i++ {
			if pageBuffer[i] == nil {
				continue
			}
			s := string(pageBuffer[i])
			locScore := reScore.FindStringIndex(s)
			if locScore != nil {
				if locScore[0] > 0 {
					pageBuffer[i][locScore[0]-1] = TCC_ALPHA_YELLOW
				}
				if locScore[1] < len(pageBuffer[i]) {
					pageBuffer[i][locScore[1]] = TCC_ALPHA_WHITE
				}
			}
			pos := strings.Index(s, "Resultater")
			if pos != -1 && pos > 25 {
				pageBuffer[i][pos-3] = TCC_ALPHA_BLUE
				pageBuffer[i][pos-2] = TCC_NEW_BACKGROUND
				pageBuffer[i][pos-1] = TCC_ALPHA_CYAN
			}
			pos = strings.Index(s, "Point            ")
			if pos > 2 {
				pageBuffer[i][pos-3] = TCC_ALPHA_BLUE
				pageBuffer[i][pos-2] = TCC_NEW_BACKGROUND
				pageBuffer[i][pos-1] = TCC_ALPHA_YELLOW
			}
			locKVT := reKVT.FindStringIndex(s)
			if locKVT != nil {
				if locKVT[0] >= 3 {
					pageBuffer[i][locKVT[0]-3] = TCC_ALPHA_BLUE
					pageBuffer[i][locKVT[0]-2] = TCC_NEW_BACKGROUND
					pageBuffer[i][locKVT[0]-1] = TCC_ALPHA_CYAN
				}
			}
		}
	}

	if pageNum == 200 {
		// draw top header half height line in 3 colors
		pageBuffer[1][0] = TCC_MOSAIC_RED
		pageBuffer[1][1] = 0x70
		pageBuffer[1][2] = TCC_HOLD_MOSAICS
		for i := 3; i < 6; i++ {
			pageBuffer[1][i] = TCC_MOSAIC_RED
		}
		for i := 6; i < 20; i++ {
			pageBuffer[1][i] = TCC_MOSAIC_CYAN
		}
		for i := 20; i < 40; i++ {
			pageBuffer[1][i] = TCC_MOSAIC_WHITE
		}

		// colors for header row
		drawSportHeader(2)

		// draw top header bottom half height line in 3 colors
		pageBuffer[3][0] = TCC_MOSAIC_RED
		pageBuffer[3][1] = 0x23
		pageBuffer[3][2] = TCC_HOLD_MOSAICS
		for i := 3; i < 6; i++ {
			pageBuffer[3][i] = TCC_MOSAIC_RED
		}
		for i := 6; i < 20; i++ {
			pageBuffer[3][i] = TCC_MOSAIC_CYAN
		}
		for i := 20; i < 40; i++ {
			pageBuffer[3][i] = TCC_MOSAIC_WHITE
		}
		pageBuffer[4][0] = TCC_ALPHA_CYAN
		makeDashedLines(TCC_ALPHA_BLUE)
		detectTwoCapitals()
		whiteSeperatedBoxes(18)
		whiteSeperatedBoxes(19)
		whiteSeperatedBoxes(20)
		bottomBlock(TCC_MOSAIC_BLUE, 0)
	}

	if pageNum == 201 || (pageNum >= 660 && pageNum <= 695) || (pageNum > 530 && pageNum < 550) {
		drawSportHeader(1)
		detectTwoCapitals()
		detectSportScores()
		whiteSeperatedBoxes(23)
	}

	// VM Fodbold 2026
	if pageNum == 530 {
		drawSportHeader(1)
		pageBuffer[3][0] = TCC_ALPHA_CYAN
		detectTwoCapitals()
		detectSportScores()
		for row := 20; row < 24; row++ {
			for col := 0; col < 40; col++ {
				if pageBuffer[row][col] == TCC_ALPHA_YELLOW {
					pageBuffer[row][col] = 0x20
				}
			}
			pageBuffer[row][0] = TCC_ALPHA_WHITE
			pageBuffer[row][1] = TCC_NEW_BACKGROUND
			pageBuffer[row][2] = TCC_ALPHA_RED
			pageBuffer[row][11] = TCC_ALPHA_BLACK
			pageBuffer[row][19] = TCC_MOSAIC_BLACK
			pageBuffer[row][20] = 0x35
			pageBuffer[row][21] = TCC_ALPHA_RED
			pageBuffer[row][32] = TCC_ALPHA_BLACK
		}
	}

	// Fodbold/Ovrige Resultater/Stillinger
	if pageNum == 202 {
		drawSportHeader(1)
		for row := 3; row < 23; row++ {
			rowStr := string(pageBuffer[row])
			startColumn := strings.Index(rowStr, "Nations")
			if startColumn > 0 {
				pageBuffer[row][startColumn-1] = TCC_ALPHA_CYAN
			}
			startColumn = strings.Index(rowStr, "VM ")
			if startColumn > 0 {
				pageBuffer[row][startColumn-1] = TCC_ALPHA_CYAN
			}
			startColumn = strings.Index(rowStr, "Livekampe")
			if startColumn > 0 {
				pageBuffer[row][startColumn-1] = TCC_ALPHA_CYAN
			}
			if pageBuffer[row][3] == 0x20 && pageBuffer[row][5] != 0x20 && pageBuffer[row-1][5] == 0x20 {
				pageBuffer[row][4] = TCC_ALPHA_CYAN
				pageBuffer[row][19] = TCC_ALPHA_WHITE
			}
			if pageBuffer[row][23] == 0x20 && pageBuffer[row][25] != 0x20 {
				pageBuffer[row][24] = TCC_ALPHA_CYAN
			}
		}
		pageBuffer[23][1] = TCC_NEW_BACKGROUND
		pageBuffer[23][2] = TCC_ALPHA_BLACK
		pos := strings.Index(string(pageBuffer[23]), ">")
		if pos > 2 && pos < 39 {
			pageBuffer[23][pos-1] = TCC_ALPHA_RED
			pageBuffer[23][pos+3] = TCC_ALPHA_BLACK
		}
	}

	if pageNum >= 203 && pageNum < 300 {
		drawSportHeader(1)
		capitalizeHeadline()
		bottomBlock(TCC_MOSAIC_BLUE, 23)
	}

	if pageNum == 300 {
		insertDRTekstTVLogo()
		pageBuffer[1][0] = TCC_ALPHA_RED
		pageBuffer[1][24] = TCC_NEW_BACKGROUND
		pageBuffer[1][25] = TCC_ALPHA_WHITE
		pageBuffer[4][0] = TCC_ALPHA_RED
		for i := 5; i < 22; i++ {
			if strings.Contains(string(pageBuffer[i]), "DAGENS") ||
				strings.Contains(string(pageBuffer[i]), "UGENS") ||
				strings.Contains(string(pageBuffer[i]), "TEKSTER") {
				pageBuffer[i][0] = TCC_ALPHA_RED
				pageBuffer[i][1] = TCC_NEW_BACKGROUND
				pageBuffer[i][2] = TCC_ALPHA_WHITE
				pageBuffer[i][10] = TCC_BLACK_BACKGROUND
			}
			posDR := strings.Index(string(pageBuffer[i]), "DR1")
			if posDR > 10 {
				pageBuffer[i][posDR-1] = TCC_ALPHA_CYAN
			}
			posDR = strings.Index(string(pageBuffer[i]), "DR2")
			if posDR > 10 {
				pageBuffer[i][posDR-1] = TCC_ALPHA_CYAN
			}
		}
		whiteSeperatedBoxes(23)
	}

	// hunt for (=) and make 'm cyan
	subsAndTimesCyan := func() {
		for i := 1; i < 22; i++ {
			// and also make times cyan
			if i > 4 {
				pageBuffer[i][4] = TCC_ALPHA_CYAN
				pageBuffer[i][10] = TCC_ALPHA_WHITE
			}
			posSubSign := strings.Index(string(pageBuffer[i]), "(=)")
			if posSubSign > 0 {
				pageBuffer[i][posSubSign-1] = TCC_ALPHA_CYAN
			}
		}
	}

	redBlueStationBlock := func(DR2fromPage int) {
		bgColor = TCC_MOSAIC_RED
		num := '1'

		if pageNum >= DR2fromPage {
			bgColor = TCC_MOSAIC_BLUE
			num = '2'
		}

		pageBuffer[1][10] = bgColor
		pageBuffer[1][11] = 0x70
		pageBuffer[1][12] = TCC_HOLD_MOSAICS
		pageBuffer[4][10] = bgColor
		pageBuffer[4][11] = 0x23
		pageBuffer[4][12] = TCC_HOLD_MOSAICS
		for i := 13; i < 17; i++ {
			pageBuffer[1][i] = bgColor
			pageBuffer[4][i] = bgColor
		}
		for i := 2; i < 4; i++ {
			pageBuffer[i][10] = bgColor
			pageBuffer[i][11] = TCC_NEW_BACKGROUND
			pageBuffer[i][12] = TCC_ALPHA_WHITE
			pageBuffer[i][13] = TCC_DOUBLE_HEIGHT
			pageBuffer[i][14] = byte(num)
			pageBuffer[i][15] = TCC_NORMAL_HEIGHT
			pageBuffer[i][16] = TCC_ALPHA_WHITE
			pageBuffer[i][17] = TCC_BLACK_BACKGROUND
		}
	}

	// TV programs today for both DR1 and DR2
	if pageNum >= 301 && pageNum <= 306 {
		insertDRTekstTVLogo()
		redBlueStationBlock(304)
		subsAndTimesCyan()
		whiteSeperatedBoxes(22)
		whiteSeperatedBoxes(23)
	}

	if pageNum == 310 || pageNum == 311 {
		redWhiteRedBar(6, 28)

		// station colors
		pageBuffer[3][0] = TCC_ALPHA_YELLOW
		pageBuffer[4][0] = TCC_ALPHA_CYAN
		pageBuffer[5][0] = TCC_ALPHA_CYAN
		for i := 3; i < 6; i++ {
			pageBuffer[i][6] = TCC_ALPHA_WHITE
			posDot := strings.Index(string(pageBuffer[i]), ".")
			if posDot > 7 && posDot < 33 {
				pageBuffer[i][posDot-1] = TCC_ALPHA_BLUE
			}
		}

		// cyan time
		pageBuffer[3][33] = TCC_ALPHA_CYAN
		pageBuffer[4][33] = TCC_ALPHA_CYAN
		pageBuffer[5][33] = TCC_ALPHA_CYAN
	}

	if (pageNum >= 312 && pageNum <= 313) || pageNum == 380 {
		redWhiteRedBar(6, 29)
		// scan for DR1, DR2 and time and make them cyan
		for i := 2; i < 23; i++ {
			posDR1 := strings.Index(string(pageBuffer[i]), "DR1")
			posDR2 := strings.Index(string(pageBuffer[i]), "DR2")
			if posDR1 == 1 {
				pageBuffer[i][0] = TCC_ALPHA_CYAN
				pageBuffer[i][6] = TCC_ALPHA_WHITE
			}
			if posDR2 == 1 {
				pageBuffer[i][0] = TCC_ALPHA_CYAN
				pageBuffer[i][6] = TCC_ALPHA_WHITE
			}
			if pageBuffer[i][37] == ':' {
				pageBuffer[i][34] = TCC_ALPHA_CYAN
			}

		}
		whiteSeperatedBoxes(23)
	}

	if pageNum == 360 {
		pageBuffer[1][28] = TCC_ALPHA_RED
		pageBuffer[1][29] = TCC_NEW_BACKGROUND
		pageBuffer[1][30] = TCC_ALPHA_WHITE
		// DR1 & DR2 row cyan
		pageBuffer[6][0] = TCC_ALPHA_CYAN
		// cyan page numbers
		for i := 7; i < 18; i++ {
			pageBuffer[i][13] = TCC_ALPHA_CYAN
			pageBuffer[i][33] = TCC_ALPHA_CYAN
		}
		whiteSeperatedBoxes(18)
		whiteSeperatedBoxes(19)
		whiteSeperatedBoxes(20)

		bottomBlock(TCC_MOSAIC_RED, 0)
	}

	// TV programs next week per day for both DR1 and DR2
	if (pageNum >= 361 && pageNum <= 367) || (pageNum >= 381 && pageNum <= 387) {
		insertDRTekstTVLogo()
		redBlueStationBlock(381)
		subsAndTimesCyan()
		whiteSeperatedBoxes(23)
	}

	if (pageNum >= 371 && pageNum < 378) || (pageNum == 389) || (pageNum >= 391 && pageNum <= 396) {
		DRbox(TCC_ALPHA_RED, 1, TCC_ALPHA_YELLOW)
		DRbox(TCC_ALPHA_RED, 12, TCC_ALPHA_YELLOW)
		openRect(TCC_MOSAIC_RED, 1)
		openRect(TCC_MOSAIC_RED, 12)
		pageBuffer[1][22] = TCC_ALPHA_CYAN
		pageBuffer[12][22] = TCC_ALPHA_CYAN
		// Make 'Sendes også' cyan
		for i := 12; i < 23; i++ {
			pos := strings.Index(string(pageBuffer[i]), "Sendes")
			if pos > 20 {
				pageBuffer[i][pos-1] = TCC_ALPHA_CYAN
				break
			}
		}
		whiteSeperatedBoxes(23)
	}

	if pageNum >= 378 && pageNum <= 379 {
		redWhiteRedBar(6, 31)
		for i := 2; i < 23; i++ {
			pageBuffer[i][0] = TCC_ALPHA_CYAN
			pageBuffer[i][6] = TCC_ALPHA_WHITE
			pageBuffer[i][36] = TCC_ALPHA_CYAN
			posDot := strings.Index(string(pageBuffer[i]), " .")
			if posDot > 13 && posDot < 36 {
				pageBuffer[i][posDot] = TCC_ALPHA_BLUE
			}
		}
		whiteSeperatedBoxes(23)
	}

	if (pageNum == 390) || (pageNum == 397) || (pageNum == 398) || (pageNum == 399) {
		redWhiteRedBar(6, 21)
		pageBuffer[3][0] = TCC_ALPHA_CYAN
		whiteSeperatedBoxes(22)
	}

	openBlueRectSolidBlueBox := func(startSolidBox int) {
		row := 1
		// corners mosaics
		pageBuffer[row][1] = 0xB7
		pageBuffer[row][startSolidBox-1] = 0xEB
		pageBuffer[row+2][1] = 0xF5
		pageBuffer[row+2][startSolidBox-1] = 0xFA
		// left and right bar mosaics
		pageBuffer[row+1][1] = 0xB5
		pageBuffer[row+1][startSolidBox-1] = 0xEA
		// horizontal bar mosaics
		for i := 2; i < startSolidBox-1; i++ {
			pageBuffer[row][i] = 0xA3
			pageBuffer[row+2][i] = 0xF0
		}
		// color control codes
		for i := row; i < row+3; i++ {
			pageBuffer[i][0] = TCC_MOSAIC_BLUE
			if i == row+1 {
				pageBuffer[i][2] = TCC_ALPHA_WHITE // Verjet
				pageBuffer[i][startSolidBox-2] = TCC_MOSAIC_BLUE
			}
		}
		// solid blue block on the right
		for i := startSolidBox; i < 40; i++ {
			pageBuffer[row][i] = 0xFF
			pageBuffer[row+2][i] = 0xFF
		}
		pageBuffer[2][startSolidBox] = TCC_NEW_BACKGROUND
		pageBuffer[2][startSolidBox+1] = TCC_ALPHA_WHITE // DMI
	}

	// Weather / Vejret indland
	if pageNum == 400 {
		openBlueRectSolidBlueBox(33)
		pageBuffer[2][9] = TCC_ALPHA_CYAN // Indland
		whiteSeperatedBoxes(21)
		whiteSeperatedBoxes(22)
		whiteSeperatedBoxes(23)
		// put the missing black half width vertical bar (0x35) in the middle
		pageBuffer[23][22-3] = TCC_MOSAIC_BLACK
		pageBuffer[23][22-2] = 0x35
	}

	if (pageNum == 402) || (pageNum == 403) || (pageNum >= 411) && (pageNum <= 418) {
		// VEJR logo mosaics
		// I recreated the logo with this web based teletext editor:
		// https://zxnet.co.uk/teletext/editor/#0:QIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIC6BAgQYGG5JwQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgLoECBA9ZLkzhqr74n6HX3VoNbdF_R_UCBAgQIECBAgQIECAugQIECLN07JkCBW-QoPXfwg9NcH9n9YIECBAgQIECBAgQIC6BAgQIECBAgQIECBAgQIECJCgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECA:PS=0:RE=0:zx=EF00
		// And exported a binary dump and copied the relevant bytes into these slices
		logoRow1 := []byte{0x60, 0x30, 0x6E, 0x24, 0x70}
		logoRow2 := []byte{0x3D, 0x32, 0x2E, 0x26, 0x38, 0x35, 0x2B, 0x77, 0x62, 0x3F, 0x21,
			0x6B, 0x77, 0x2B, 0x20, 0x6B, 0x37, 0x22, 0x7F, 0x23, 0x7D,
		}
		logoRow3 := []byte{0x22, 0x66, 0x74, 0x76, 0x26, 0x20, 0x20, 0x2B, 0x3E, 0x21, 0x20,
			0x7A, 0x77, 0x78, 0x20, 0x7A, 0x35, 0x60, 0x7F, 0x33, 0x7D, 0x30,
		}
		logoRow4 := []byte{0x22, 0x21}
		copy(pageBuffer[1][3:], logoRow1)
		copy(pageBuffer[2][3:], logoRow2)
		copy(pageBuffer[3][3:], logoRow3)
		// only apply bottom part of the J if no other characters are on that place already
		if pageBuffer[4][17] == 0x20 {
			copy(pageBuffer[4][17:], logoRow4)
			pageBuffer[4][16] = TCC_MOSAIC_CYAN
		}
		for i := 1; i < 4; i++ {
			pageBuffer[i][0] = TCC_ALPHA_BLUE
			pageBuffer[i][1] = TCC_NEW_BACKGROUND
			pageBuffer[i][2] = TCC_MOSAIC_CYAN
			if i > 2 {
				pageBuffer[i][25] = TCC_ALPHA_WHITE
			} else {
				pageBuffer[i][25] = TCC_ALPHA_CYAN
			}
		}
		pageBuffer[4][0] = TCC_ALPHA_BLUE
		pageBuffer[4][1] = TCC_NEW_BACKGROUND
		pageBuffer[4][2] = TCC_ALPHA_WHITE
		pageBuffer[4][25] = TCC_ALPHA_WHITE

		pageBuffer[6][0] = TCC_ALPHA_CYAN

		for i := 8; i < 23; i++ {
			if pageBuffer[i][1] == '-' {
				pageBuffer[i+1][0] = TCC_ALPHA_CYAN
			}
		}

		pageBuffer[23][0] = TCC_ALPHA_BLUE
		pageBuffer[23][1] = TCC_NEW_BACKGROUND
		pageBuffer[23][2] = TCC_ALPHA_CYAN
	}

	if pageNum == 410 {
		// manually drawn map, not 100% accurate
		//https://zxnet.co.uk/teletext/editor/#0:QIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECDg0YsECBAgiUkCBBzaMWCBBv3c0DJqg24eSBAgYskDFkgYsgh1AXQIECBAgQIECBAgQfkKBAgAnQ9ZBFQSkFJAgQIECBAgCHS6BAgQIECBAgQINH___QIECACdD1JMxBMQQUE5BEQU0CAIdDoECBAgaMWKAvg____9qXwPCPz58-fPnz58-fPnz58-fAh0ugQIECBAgQIMH7____2qBAhAnQ8yfLgzKsSnJj1EE-OgCHUCBAgQYOHAv8_5_nbe3QIECACdDz4VOLSrQakmfOi0kCAIdBNGLIl_PJy_D5____jRAgQIAJ0PGkzokWmgoQUCBAgQIAh0kgPHv3_F_L_______tUCBAgAnQ8GdHk1p0VBTkxItJAgCHSSA9oR_9X8v_____9fzQIECJAgQIECBAgQIECBAgQIECAIdJHkGr78Qf___-V____hggQIECBAgQIECBAgQIECBAgQIAh0keQf________5X_____v9ogQIECBAgQIECBAgQIECBAgCHSR5B________K____-_170KBAgJ4EBLB-_pECBAgQIECAIdJHkGr______8r____7VWgCtGLMme_kmv_-gQIECBAgQIAh0meQav______yv____tUCBAgQICZ7-Sa__7VAgQIECBAgCHSZ5B_3______K___1kg1MEBM8gUf_5Lz_f_UAJoxboECAIdJnkCJF_____8r___0JbAwQE0CBd-_kv7dAhQIECBAgQIAh0mgQIEDbf______-lQFvz1ggJoECNFv___6xAgQIECBAgCHQbRi0JotX_____0aAtq___jVATQIEW7__fg2jFsgQIECAIdQIECAnq1f_____wMCz____pfRNAgUcP_9pwQIEAVoxcIAh0mgQIECDV______7raFka_foSk0HBBs___qdCgQFcH34wCHSaBAgQIECBAvfp16p6WWrEH4mgQf-nzX_eoECBAV1f__8IdQIECBAgQIECBAgQIAzRi1QE0CBH___9SdAgQIEBVAjVp0B0C0YMEACsgioJSCkgQIECBAgQE0CJCgVIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECBAgQIECA:PS=0:RE=0:zx=Nc00
		weatherMap := []byte{
			0x04, 0x1D, 0x20, 0x17, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x7E, 0x21, 0x20, 0x20, 0x20, 0x01, 0x1D, 0x07, 0x56, 0x20, 0x45, 0x20, 0x4A, 0x20, 0x52, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20,
			0x04, 0x1D, 0x17, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x68, 0x7F, 0x7F, 0x7F, 0x20, 0x20, 0x20, 0x20, 0x01, 0x1D, 0x07, 0x54, 0x49, 0x4C, 0x20, 0x4C, 0x20, 0x41, 0x20, 0x4E, 0x20, 0x44, 0x20, 0x53, 0x20, 0x20,
			0x04, 0x1D, 0x07, 0x20, 0x20, 0x20, 0x20, 0x20, 0x34, 0x31, 0x31, 0x20, 0x17, 0x60, 0x7F, 0x7F, 0x7F, 0x7F, 0x35, 0x17, 0x60, 0x3C, 0x11, 0x7C, 0x7C, 0x7C, 0x7C, 0x7C, 0x7C, 0x7C, 0x7C, 0x7C, 0x7C, 0x7C, 0x7C, 0x7C, 0x7C, 0x7C, 0x7C, 0x7C,
			0x04, 0x1D, 0x17, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x60, 0x7E, 0x7F, 0x7F, 0x7F, 0x7F, 0x35, 0x20, 0x20, 0x21, 0x01, 0x1D, 0x07, 0x4C, 0x4F, 0x4B, 0x41, 0x4C, 0x55, 0x44, 0x53, 0x49, 0x47, 0x54, 0x20, 0x4F, 0x47, 0x20,
			0x04, 0x1D, 0x20, 0x20, 0x20, 0x20, 0x20, 0x60, 0x70, 0x70, 0x17, 0x7C, 0x7F, 0x67, 0x7C, 0x76, 0x6F, 0x37, 0x20, 0x20, 0x20, 0x20, 0x01, 0x1D, 0x07, 0x4F, 0x42, 0x53, 0x45, 0x52, 0x56, 0x41, 0x54, 0x49, 0x4F, 0x4E, 0x45, 0x52, 0x20, 0x20,
			0x04, 0x1D, 0x02, 0x34, 0x31, 0x32, 0x12, 0x7F, 0x1E, 0x27, 0x17, 0x70, 0x7C, 0x7F, 0x7F, 0x7F, 0x7C, 0x34, 0x20, 0x20, 0x20, 0x20, 0x01, 0x1D, 0x07, 0x46, 0x49, 0x4E, 0x44, 0x45, 0x53, 0x20, 0x50, 0x41, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20,
			0x04, 0x1D, 0x12, 0x20, 0x1E, 0x1E, 0x7E, 0x7F, 0x62, 0x7F, 0x17, 0x7F, 0x7F, 0x7F, 0x7F, 0x7F, 0x7F, 0x35, 0x20, 0x20, 0x20, 0x20, 0x01, 0x1D, 0x07, 0x41, 0x4E, 0x47, 0x49, 0x56, 0x4E, 0x45, 0x20, 0x53, 0x49, 0x44, 0x45, 0x52, 0x20, 0x20,
			0x04, 0x1D, 0x12, 0x20, 0x1E, 0x68, 0x23, 0x7F, 0x6A, 0x7F, 0x17, 0x7F, 0x7F, 0x7F, 0x7F, 0x7F, 0x2F, 0x73, 0x20, 0x20, 0x20, 0x22, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20,
			0x04, 0x1D, 0x12, 0x1E, 0x20, 0x6A, 0x7D, 0x7C, 0x20, 0x7F, 0x7F, 0x7F, 0x7F, 0x15, 0x7F, 0x7F, 0x7F, 0x78, 0x30, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20,
			0x04, 0x1D, 0x12, 0x1E, 0x20, 0x7F, 0x7F, 0x7F, 0x7F, 0x7F, 0x7F, 0x7F, 0x7F, 0x15, 0x7F, 0x7F, 0x7F, 0x7F, 0x7D, 0x7F, 0x34, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20,
			0x04, 0x1D, 0x12, 0x1E, 0x20, 0x7F, 0x7F, 0x7F, 0x7F, 0x7F, 0x7F, 0x7F, 0x15, 0x7F, 0x7F, 0x7F, 0x7F, 0x3F, 0x6B, 0x6F, 0x21, 0x20, 0x20, 0x20, 0x13, 0x60, 0x20, 0x12, 0x60, 0x7E, 0x7F, 0x24, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20,
			0x04, 0x1D, 0x12, 0x1E, 0x20, 0x6A, 0x7F, 0x7F, 0x7F, 0x7F, 0x7F, 0x7F, 0x15, 0x7F, 0x7F, 0x7F, 0x7F, 0x35, 0x2B, 0x20, 0x05, 0x34, 0x31, 0x33, 0x13, 0x1E, 0x7F, 0x12, 0x35, 0x7F, 0x7F, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20,
			0x04, 0x1D, 0x13, 0x1E, 0x20, 0x6A, 0x7F, 0x7F, 0x7F, 0x7F, 0x7F, 0x7F, 0x15, 0x7F, 0x7F, 0x7F, 0x7F, 0x35, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x13, 0x1E, 0x7F, 0x12, 0x35, 0x7F, 0x7F, 0x35, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20,
			0x04, 0x1D, 0x13, 0x1E, 0x20, 0x7F, 0x6F, 0x7F, 0x7F, 0x7F, 0x7F, 0x7F, 0x15, 0x7F, 0x7F, 0x7D, 0x32, 0x20, 0x6A, 0x30, 0x13, 0x1E, 0x20, 0x28, 0x7F, 0x7F, 0x12, 0x79, 0x7F, 0x3F, 0x7D, 0x20, 0x02, 0x34, 0x31, 0x37, 0x20, 0x20, 0x20, 0x20,
			0x04, 0x1D, 0x13, 0x1E, 0x20, 0x22, 0x22, 0x7F, 0x7F, 0x7F, 0x7F, 0x7F, 0x15, 0x7F, 0x7F, 0x7F, 0x21, 0x16, 0x60, 0x30, 0x20, 0x13, 0x20, 0x20, 0x2E, 0x7E, 0x7F, 0x12, 0x7F, 0x37, 0x20, 0x21, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20,
			0x04, 0x1D, 0x13, 0x20, 0x20, 0x20, 0x20, 0x36, 0x6F, 0x7F, 0x7F, 0x7F, 0x7F, 0x7F, 0x7F, 0x25, 0x20, 0x16, 0x7E, 0x3D, 0x30, 0x20, 0x13, 0x20, 0x20, 0x23, 0x22, 0x6F, 0x7F, 0x7F, 0x7F, 0x2C, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20,
			0x04, 0x1D, 0x03, 0x34, 0x31, 0x34, 0x13, 0x22, 0x6A, 0x7F, 0x7F, 0x7F, 0x7F, 0x7F, 0x23, 0x20, 0x16, 0x6A, 0x7F, 0x7F, 0x7C, 0x35, 0x20, 0x13, 0x20, 0x20, 0x22, 0x6E, 0x7F, 0x7F, 0x3F, 0x03, 0x34, 0x31, 0x36, 0x20, 0x20, 0x20, 0x20, 0x20,
			0x04, 0x1D, 0x20, 0x20, 0x20, 0x20, 0x13, 0x6A, 0x6A, 0x7F, 0x7F, 0x7F, 0x7F, 0x7F, 0x60, 0x30, 0x16, 0x3F, 0x7F, 0x7F, 0x7F, 0x25, 0x7A, 0x13, 0x20, 0x20, 0x28, 0x70, 0x7F, 0x7F, 0x34, 0x70, 0x20, 0x20, 0x20, 0x05, 0x34, 0x31, 0x38, 0x20,
			0x04, 0x1D, 0x13, 0x20, 0x20, 0x20, 0x20, 0x20, 0x6A, 0x7F, 0x7F, 0x7F, 0x7F, 0x7F, 0x7D, 0x6B, 0x34, 0x16, 0x23, 0x2F, 0x6F, 0x68, 0x25, 0x13, 0x20, 0x70, 0x20, 0x6C, 0x7F, 0x7F, 0x7D, 0x27, 0x21, 0x20, 0x20, 0x15, 0x60, 0x7D, 0x7C, 0x30,
			0x04, 0x1D, 0x13, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x2F, 0x3F, 0x27, 0x2F, 0x2A, 0x3D, 0x16, 0x2D, 0x2C, 0x20, 0x7E, 0x13, 0x20, 0x20, 0x7F, 0x74, 0x7C, 0x6B, 0x7F, 0x3D, 0x20, 0x20, 0x20, 0x20, 0x15, 0x6A, 0x7F, 0x7F, 0x7F,
			0x04, 0x1D, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x06, 0x34, 0x31, 0x35, 0x20, 0x13, 0x20, 0x20, 0x23, 0x7F, 0x7F, 0x7F, 0x6A, 0x27, 0x20, 0x20, 0x20, 0x20, 0x20, 0x15, 0x20, 0x23, 0x2B, 0x27,
			0x04, 0x1D, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x13, 0x20, 0x22, 0x21, 0x20, 0x2A, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20,
		}
		row := 1
		for i := 0; i < len(weatherMap); i += 40 {
			end := i + 40
			if end > len(weatherMap) {
				end = len(weatherMap)
			}
			pageBuffer[row] = make([]byte, end-i)
			copy(pageBuffer[row], weatherMap[i:end])
			row++
		}
		whiteSeperatedBoxes(23)
	}
	// world time table
	// I added a concealed message in magenta (press C in Teletext64 to show the text)
	if pageNum == 438 {
		pageBuffer[1][0] = TCC_ALPHA_YELLOW
		pageBuffer[1][1] = TCC_DOUBLE_HEIGHT
		copy(pageBuffer[1][2:], "V E R D E N S U R")
		pageBuffer[3][22] = TCC_ALPHA_GREEN
		pageBuffer[21][13] = TCC_ALPHA_GREEN
		pageBuffer[22][0] = TCC_ALPHA_MAGENTA
		copy(pageBuffer[22][1:], "\x18no summer/winter time info available!")
		bottomBlock(TCC_ALPHA_RED, 23)
	}

	// sun & moon; mosaics not included here
	if pageNum == 439 {
		// remove control codes first
		for i := range 40 {
			if pageBuffer[1][i] < 0x1F {
				pageBuffer[1][i] = 0x20
			}
		}
		// red white header
		pos2ndhalf := 23
		pageBuffer[1][0] = TCC_ALPHA_RED
		pageBuffer[1][1] = TCC_NEW_BACKGROUND
		pageBuffer[1][2] = TCC_ALPHA_WHITE
		pageBuffer[1][pos2ndhalf] = TCC_NEW_BACKGROUND
		pageBuffer[1][pos2ndhalf+1] = TCC_ALPHA_BLUE
		// blue left half/black right half
		for i := 2; i < 23; i++ {
			pageBuffer[i][0] = TCC_ALPHA_BLUE
			pageBuffer[i][1] = TCC_NEW_BACKGROUND
			rowSolen := strings.Index(string(pageBuffer[i]), "SOLEN")
			if rowSolen > 0 {
				pageBuffer[i][2] = TCC_ALPHA_YELLOW

			} else {
				pageBuffer[i][2] = TCC_ALPHA_WHITE
			}
			if i < 15 {
				pageBuffer[i][pos2ndhalf-1] = TCC_ALPHA_BLACK
				pageBuffer[i][pos2ndhalf] = TCC_NEW_BACKGROUND
				pageBuffer[i][pos2ndhalf+1] = TCC_ALPHA_YELLOW
				colNymane := strings.Index(string(pageBuffer[i]), "NYM")
				if colNymane > 0 {
					pageBuffer[i][pos2ndhalf+1] = TCC_ALPHA_GREEN
				}
			} else {
				pageBuffer[i][pos2ndhalf-1] = TCC_ALPHA_WHITE
				pageBuffer[i][pos2ndhalf] = TCC_NEW_BACKGROUND
				pageBuffer[i][pos2ndhalf+1] = TCC_ALPHA_RED
			}
		}
		pageBuffer[23][0] = TCC_ALPHA_WHITE
		pageBuffer[23][1] = TCC_NEW_BACKGROUND
		pageBuffer[23][2] = TCC_ALPHA_BLUE
		pageBuffer[23][pos2ndhalf-1] = TCC_ALPHA_RED
		pageBuffer[23][pos2ndhalf] = TCC_NEW_BACKGROUND
		pageBuffer[23][pos2ndhalf+1] = TCC_ALPHA_WHITE
	}

	if pageNum == 470 {
		openBlueRectSolidBlueBox(29)
		for i := 5; i < 22; i++ {
			if pageBuffer[i][31] != 0x20 {
				pageBuffer[i][28] = TCC_ALPHA_BLUE
				pageBuffer[i][29] = TCC_NEW_BACKGROUND
				pageBuffer[i][30] = TCC_ALPHA_WHITE
			}
		}
		bottomBlock(TCC_MOSAIC_BLUE, 0)
	}

	if pageNum >= 471 && pageNum < 475 {
		openBlueRectSolidBlueBox(33)
		detectThreeCapitals()
		// detect URLs; make everything before the URL cyan
		for i := 4; i < 23; i++ {
			dkURL := strings.Index(string(pageBuffer[i]), ".dk")
			if dkURL > 0 {
				for j := dkURL; j > 3; j-- {
					if pageBuffer[i][j] == 0x20 {
						pageBuffer[i][0] = TCC_ALPHA_CYAN
						pageBuffer[i][j] = TCC_ALPHA_WHITE
						break
					}
				}
			}
		}
		if strings.TrimSpace(string(pageBuffer[23])) == "" {
			bottomBlock(TCC_MOSAIC_BLUE, 0)
		} else {
			bottomBlock(TCC_MOSAIC_BLUE, 23)
		}
	}

	// Rederier/ruter faerger
	if pageNum >= 476 && pageNum < 480 {
		openBlueRectSolidBlueBox(28)
		for i := 4; i < 23; i++ {
			if pageBuffer[i][16] == 0x20 && pageBuffer[i][17] != 0x20 {
				pageBuffer[i][0] = TCC_ALPHA_YELLOW
				pageBuffer[i][16] = TCC_ALPHA_CYAN
			}
		}
		whiteSeperatedBoxes(23)
	}

	// Lufthavne (airport info)
	if pageNum == 480 {
		for i := 1; i < 3; i++ {
			pageBuffer[i][0] = TCC_ALPHA_BLUE
			pageBuffer[i][1] = TCC_NEW_BACKGROUND
			pageBuffer[i][2] = TCC_ALPHA_WHITE
			pageBuffer[i][3] = TCC_DOUBLE_HEIGHT
			pageBuffer[i][23] = TCC_NEW_BACKGROUND
			pageBuffer[i][24] = TCC_ALPHA_BLACK
		}
		detectThreeCapitals()
		bottomBlock(TCC_MOSAIC_BLUE, 0)
	}

	// Lufthavne (airport info)
	if pageNum >= 481 && pageNum < 500 {
		for i := 1; i < 4; i++ {
			pageBuffer[i][0] = TCC_ALPHA_BLUE
			pageBuffer[i][1] = TCC_NEW_BACKGROUND
			pageBuffer[i][2] = TCC_ALPHA_WHITE
		}
		pageBuffer[4][0] = TCC_MOSAIC_BLUE
		for i := 1; i < 40; i++ {
			pageBuffer[4][i] = 0xA3
		}
		pageBuffer[5][0] = TCC_ALPHA_YELLOW
		if pageBuffer[22][1] == 0x20 {
			whiteSeperatedBoxes(22)
		}
		whiteSeperatedBoxes(23)
	}

	// Film
	if pageNum == 500 {
		redWhiteRedBar(11, 29)
		for i := 2; i < 23; i++ {
			pageBuffer[i][4] = TCC_ALPHA_CYAN
			pageBuffer[i][8] = TCC_ALPHA_WHITE
			pageBuffer[i][33] = TCC_ALPHA_CYAN
		}
		whiteSeperatedBoxes(23)
	}

	// More movies
	if pageNum == 501 {
		redWhiteRedBar(11, 27)
		for i := 2; i < 23; i++ {
			if pageBuffer[i][32] != '-' {
				pageBuffer[i][32] = TCC_ALPHA_CYAN
			} else {
				pageBuffer[i][0] = TCC_ALPHA_RED
			}
		}
		whiteSeperatedBoxes(23)
	}

	// Valuta
	if pageNum == 551 {
		pageBuffer[1][0] = TCC_ALPHA_RED
		pageBuffer[1][1] = TCC_DOUBLE_HEIGHT
		copy(pageBuffer[1][2:], "VALUTA")
		pageBuffer[2][0] = TCC_ALPHA_WHITE
		pageBuffer[2][8] = TCC_NORMAL_HEIGHT
		pos := 19
		pageBuffer[3][pos] = TCC_ALPHA_RED
		pageBuffer[3][pos+1] = TCC_NEW_BACKGROUND
		pageBuffer[3][pos+2] = TCC_ALPHA_WHITE
		for i := 4; i < 23; i++ {
			pageBuffer[i][pos] = TCC_ALPHA_CYAN
			pageBuffer[i][pos+4] = TCC_ALPHA_WHITE
			pageBuffer[i][32] = TCC_ALPHA_CYAN
		}
		pageBuffer[23][0] = TCC_ALPHA_RED
		pageBuffer[23][1] = TCC_NEW_BACKGROUND
		pageBuffer[23][2] = TCC_ALPHA_WHITE
	}

	// Folketinget
	if pageNum == 570 {
		blueWhiteBlueBar(10, 29)
	}

	// Folketinget
	if pageNum == 572 {
		blueWhiteBlueBar(0, 28)
		for i := 3; i < 9; i++ {
			pageBuffer[i][0] = TCC_ALPHA_BLUE
			pageBuffer[i][1] = TCC_NEW_BACKGROUND
		}
		pageBuffer[3][2] = TCC_ALPHA_CYAN
		pageBuffer[3][3] = TCC_DOUBLE_HEIGHT
		pageBuffer[5][2] = TCC_ALPHA_WHITE
		pageBuffer[6][2] = TCC_ALPHA_WHITE
		pageBuffer[8][2] = TCC_ALPHA_CYAN

		bottomBlock(TCC_MOSAIC_BLUE, 23)
	}

	// Folketingsvalk 2026
	if pageNum > 572 && pageNum < 587 {
		blueWhiteBlueBar(0, 28)
		if pageNum != 585 && pageNum != 586 {
			pageBuffer[3][0] = TCC_ALPHA_YELLOW
			for i := 4; i < 23; i++ {
				pageBuffer[i][0] = TCC_ALPHA_CYAN
				pageBuffer[i][7] = TCC_ALPHA_WHITE
			}
		}
		whiteSeperatedBoxes(23)
	}

	// EU-Valg 2024
	if pageNum == 587 {
		blueWhiteBlueBar(0, 20)
		for i := 3; i < 10; i++ {
			pageBuffer[i][0] = TCC_ALPHA_BLUE
			pageBuffer[i][1] = TCC_NEW_BACKGROUND
		}
		pageBuffer[3][2] = TCC_ALPHA_CYAN
		pageBuffer[3][3] = TCC_DOUBLE_HEIGHT
		pageBuffer[6][2] = TCC_ALPHA_WHITE
		pageBuffer[7][2] = TCC_ALPHA_WHITE
		pageBuffer[9][2] = TCC_ALPHA_CYAN

		bottomBlock(TCC_MOSAIC_BLUE, 23)
	}

	// EU-Valg 2024
	if pageNum == 588 {
		blueWhiteBlueBar(0, 20)
		for i := 2; i < 23; i++ {
			if pageBuffer[i][1] == 0x20 {
				pageBuffer[i+1][0] = TCC_ALPHA_CYAN
			}
		}
		bottomBlock(TCC_MOSAIC_BLUE, 23)
	}

	if pageNum == 600 {
		insertDRTekstTVLogo()
		pageBuffer[1][0] = TCC_ALPHA_BLUE
		pageBuffer[1][30] = TCC_NEW_BACKGROUND
		pageBuffer[1][31] = TCC_ALPHA_WHITE
		pageBuffer[4][0] = TCC_ALPHA_BLUE
		pageBuffer[9][0] = TCC_ALPHA_BLUE
		pageBuffer[9][30] = TCC_NEW_BACKGROUND
		pageBuffer[9][31] = TCC_ALPHA_WHITE
		for i := 10; i < 19; i++ {
			pageBuffer[i][0] = TCC_ALPHA_YELLOW
			pageBuffer[i][4] = TCC_ALPHA_CYAN
			pageBuffer[i][10] = TCC_ALPHA_WHITE

			if strings.Contains(string(pageBuffer[i]), "DAGENS") ||
				strings.Contains(string(pageBuffer[i]), "UGENS") ||
				strings.Contains(string(pageBuffer[i]), "TEKSTER") {
				pageBuffer[i][0] = TCC_ALPHA_RED
				pageBuffer[i][1] = TCC_NEW_BACKGROUND
				pageBuffer[i][2] = TCC_ALPHA_WHITE
				pageBuffer[i][10] = TCC_BLACK_BACKGROUND
			}
			posDR := strings.Index(string(pageBuffer[i]), "DR1")
			if posDR > 10 {
				pageBuffer[i][posDR-1] = TCC_ALPHA_CYAN
			}
			posDR = strings.Index(string(pageBuffer[i]), "DR2")
			if posDR > 10 {
				pageBuffer[i][posDR-1] = TCC_ALPHA_CYAN
			}
		}
		whiteSeperatedBoxes(19)
		whiteSeperatedBoxes(20)
		bottomBlock(TCC_MOSAIC_BLUE, 0)
	}

	if pageNum >= 601 && pageNum <= 608 {
		posBlack := 7
		if pageNum > 605 {
			posBlack = 11
		}
		for i := 1; i < 3; i++ {
			pageBuffer[i][0] = TCC_ALPHA_BLUE
			pageBuffer[i][1] = TCC_NEW_BACKGROUND
			pageBuffer[i][2] = TCC_ALPHA_WHITE
			pageBuffer[i][posBlack] = TCC_BLACK_BACKGROUND
			pageBuffer[i][22] = TCC_ALPHA_BLUE
			pageBuffer[i][23] = TCC_NEW_BACKGROUND
			pageBuffer[i][24] = TCC_ALPHA_WHITE
		}
		for i := range 23 {
			if i < posBlack {
				pageBuffer[1][i] = 0xFF
			} else {
				pageBuffer[1][i] = 0xA3
			}
		}
		pageBuffer[1][0] = TCC_MOSAIC_BLUE

		for i := range 40 {
			pageBuffer[3][i] = 0xA3
		}
		pageBuffer[3][0] = TCC_MOSAIC_BLUE

		for i := 4; i < 23; i++ {
			posGreater := strings.Index(string(pageBuffer[i]), ">")
			if posGreater > 0 {
				pageBuffer[i][posGreater-1] = TCC_ALPHA_RED
			}
			pageBuffer[i][4] = TCC_ALPHA_CYAN
			pageBuffer[i][10] = TCC_ALPHA_WHITE
		}
		whiteSeperatedBoxes(23)
	}

	// TV programs today for both DR1 and DR2
	if pageNum == 630 {
		insertDRTekstTVLogo()
		pageBuffer[1][0] = TCC_ALPHA_BLUE
		pageBuffer[1][25] = TCC_NEW_BACKGROUND
		pageBuffer[1][26] = TCC_ALPHA_WHITE
		for i := 4; i < 23; i++ {
			posGreater := strings.Index(string(pageBuffer[i]), ">")
			if posGreater > 0 {
				pageBuffer[i][posGreater-1] = TCC_ALPHA_RED
			}
		}
		pageBuffer[5][4] = TCC_ALPHA_CYAN
		pageBuffer[18][4] = TCC_ALPHA_CYAN
		pageBuffer[8][24] = TCC_ALPHA_CYAN
		pageBuffer[12][24] = TCC_ALPHA_CYAN

		whiteSeperatedBoxes(23)
	}

	solidBlueBoxOpenBlueRect := func(startOpenRect int) {
		// solid blue block on the left
		pageBuffer[1][0] = TCC_MOSAIC_BLUE
		pageBuffer[3][0] = TCC_MOSAIC_BLUE
		pageBuffer[2][0] = TCC_ALPHA_BLUE
		pageBuffer[2][1] = TCC_NEW_BACKGROUND
		pageBuffer[2][2] = TCC_ALPHA_WHITE
		for i := 1; i < startOpenRect; i++ {
			pageBuffer[1][i] = 0xFF
			pageBuffer[3][i] = 0xFF
		}
		pageBuffer[2][startOpenRect] = TCC_BLACK_BACKGROUND
		// corners mosaics
		pageBuffer[1][39] = 0xEB // top right
		pageBuffer[3][39] = 0xFA // bottom right
		// sometimes text runs until the end of the row; dont push a mosaic in that case
		if pageBuffer[2][38] == 0x20 {
			pageBuffer[2][38] = TCC_MOSAIC_BLUE
			pageBuffer[2][39] = 0xEA // vert bar
		}
		// horizontal bar mosaics
		for i := startOpenRect; i < 39; i++ {
			pageBuffer[1][i] = 0xA3
			pageBuffer[3][i] = 0xF0
		}
	}

	// DR Kontakt
	if (pageNum >= 631 && pageNum <= 637) || (pageNum == 641) {
		solidBlueBoxOpenBlueRect(7)
		if pageNum == 636 {
			pageBuffer[2][20] = TCC_ALPHA_YELLOW
		}
		makeDashedLines(TCC_ALPHA_BLUE)
		if pageBuffer[23][3] == 0x20 {
			bottomBlock(TCC_MOSAIC_BLUE, 0)
		} else {
			bottomBlock(TCC_MOSAIC_BLUE, 23)
		}
		for i := 3; i < 20; i++ {
			if pageBuffer[i][0] == TCC_ALPHA_BLUE || pageBuffer[i][0] == TCC_MOSAIC_BLUE {
				pageBuffer[i+1][0] = TCC_ALPHA_CYAN
			}
		}
	}

	// P1+P2
	if pageNum == 638 {
		blueWhiteBlueBar(10, 23)
		pageBuffer[22][0] = TCC_ALPHA_CYAN
		whiteSeperatedBoxes(23)
	}

	// Netradio
	if pageNum == 639 {
		blueWhiteBlueBar(12, 28)
		pageBuffer[22][0] = TCC_ALPHA_BLUE
		pageBuffer[22][1] = TCC_NEW_BACKGROUND
		pageBuffer[22][2] = TCC_ALPHA_WHITE
		pageBuffer[22][3] = TCC_DOUBLE_HEIGHT
		pageBuffer[23][0] = TCC_ALPHA_BLUE
		pageBuffer[23][1] = TCC_NEW_BACKGROUND
	}

	// Radio Digital Radiolytning
	if pageNum == 640 {
		blueWhiteBlueBar(9, 32)

		for i := 2; i < 20; i++ {
			if pageBuffer[i][1] == 0x20 {
				pageBuffer[i+1][0] = TCC_ALPHA_CYAN
			}
		}
		bottomBlock(TCC_MOSAIC_BLUE, 0)
	}

	// D A N S K T O P P E N
	if pageNum == 642 {
		pageBuffer[1][0] = TCC_ALPHA_RED
		pageBuffer[1][1] = TCC_NEW_BACKGROUND
		pageBuffer[1][2] = TCC_ALPHA_WHITE
		pageBuffer[2][0] = TCC_ALPHA_RED
		pageBuffer[2][1] = TCC_NEW_BACKGROUND
		pageBuffer[2][2] = TCC_ALPHA_WHITE
		pageBuffer[1][7] = TCC_DOUBLE_HEIGHT
		copy(pageBuffer[1][10:], "D A N S K T O P P E N")
		makeDashedLines(TCC_ALPHA_RED)
		pageBuffer[1][36] = TCC_NORMAL_HEIGHT
		for i := 4; i < 20; i++ {
			if pageBuffer[i][1] != 0x20 && pageBuffer[i][1] != '-' {
				pageBuffer[i][0] = TCC_ALPHA_CYAN
				pageBuffer[i][3] = TCC_ALPHA_WHITE
				pageBuffer[i][33] = TCC_ALPHA_YELLOW
				pageBuffer[i][37] = TCC_ALPHA_CYAN
			}
			if pageBuffer[i][1] == '-' {
				pageBuffer[i][0] = TCC_ALPHA_RED
				pageBuffer[i][24] = TCC_NEW_BACKGROUND
				pageBuffer[i][25] = TCC_ALPHA_WHITE
			}
		}
		re := regexp.MustCompile(`\b[1-8][0-9]{2}\b`)
		allPageNumbers := re.FindAllStringIndex(string(pageBuffer[23]), -1)
		if allPageNumbers == nil {
			pageBuffer[23][0] = TCC_ALPHA_RED
			pageBuffer[23][1] = TCC_NEW_BACKGROUND
			pageBuffer[23][2] = TCC_ALPHA_WHITE
		} else {
			pageBuffer[22][0] = TCC_ALPHA_RED
			pageBuffer[22][1] = TCC_NEW_BACKGROUND
			pageBuffer[22][2] = TCC_ALPHA_WHITE
			bottomBlock(TCC_MOSAIC_BLUE, 23)
		}
	}

	// P5
	if pageNum == 643 {
		redWhiteRedBar(8, 19)
		pageBuffer[3][0] = TCC_ALPHA_CYAN
		pageBuffer[14][0] = TCC_ALPHA_CYAN
		for i := 4; i < 20; i++ {
			pos := strings.Index(string(pageBuffer[i]), "point")
			if pos > 25 {
				pageBuffer[i][pos-5] = TCC_ALPHA_YELLOW
				pageBuffer[i][pos-1] = TCC_ALPHA_WHITE
			}
		}
		whiteSeperatedBoxes(23)
	}

	// DR Radio-Appen
	if pageNum >= 644 && pageNum <= 650 {
		if pageNum == 646 {
			blueWhiteBlueBar(9, 20)
		} else {
			blueWhiteBlueBar(9, 27)
		}
		for i := 2; i < 20; i++ {
			re := regexp.MustCompile(`[!:?]`)
			if re.FindStringIndex(string(pageBuffer[i])) != nil && pageBuffer[i-1][0] != TCC_ALPHA_CYAN {
				pageBuffer[i][0] = TCC_ALPHA_CYAN
			}
		}
		bottomBlock(TCC_ALPHA_BLUE, 23)
	}

	// DR Koncerthuset
	if pageNum >= 696 && pageNum <= 699 {
		solidBlueBoxOpenBlueRect(7)
		bottomBlock(TCC_MOSAIC_BLUE, 0)
	}

	// Index page Cast and Audience
	if pageNum == 700 {
		insertDRTekstTVLogo()
		pageBuffer[1][0] = TCC_ALPHA_RED
		pageBuffer[1][11] = TCC_NEW_BACKGROUND
		pageBuffer[1][12] = TCC_ALPHA_WHITE
		pageBuffer[4][0] = TCC_ALPHA_RED
	}

	if (pageNum >= 701 && pageNum < 730) || (pageNum >= 784 && pageNum < 800) {
		pos := strings.Index(string(pageBuffer[1]), "NYT")
		offset := 9
		if pos == -1 {
			pos = 8
		}
		rowWhiteSeperatedBoxes := 23
		switch pageNum {
		case 703:
			redWhiteRedBar(15, 26)
			rowWhiteSeperatedBoxes = 22
		case 718:
			redWhiteRedBar(10, 24)
		case 720:
			redWhiteRedBar(7, 21)
		case 723, 724:
			redWhiteRedBar(6, 28)
		case 727:
			redWhiteRedBar(6, 31)
		default:
			redWhiteRedBar(pos-2, pos+offset)
		}
		pageBuffer[3][0] = TCC_ALPHA_CYAN
		whiteSeperatedBoxes(rowWhiteSeperatedBoxes)
	}

	if pageNum == 730 {
		s := "D Ø V E   O G   H Ø R E H Æ M M E D E"
		pageBuffer[1][0] = TCC_ALPHA_RED
		pageBuffer[1][1] = TCC_DOUBLE_HEIGHT
		currentCol := 2
		for _, r := range s {
			pageBuffer[1][currentCol] = encodeSVTChar(r)
			currentCol++
		}
		bottomBlock(TCC_ALPHA_RED, 22)
	}

	fullOpenRect := func(rectColor byte, height int) {
		row := 1
		for i := row; i < height+3; i++ {
			pageBuffer[i][0] = rectColor
			if i > row && i < height+2 {
				// left and right bar mosaics
				pageBuffer[i][1] = 0xB5
				pageBuffer[i][39] = 0xEA
				pageBuffer[i][38] = rectColor
				pageBuffer[i][2] = TCC_ALPHA_WHITE
			}
		}
		// corners mosaics
		pageBuffer[row][1] = 0xBC           // top left
		pageBuffer[row][39] = 0xEC          // top right
		pageBuffer[row+height+1][1] = 0xAD  // bottom left
		pageBuffer[row+height+1][39] = 0xAE // bottom right

		// horizontal bar mosaics
		for i := 2; i < 39; i++ {
			pageBuffer[row][i] = 0xAC
			pageBuffer[row+height+1][i] = 0xAC
		}
	}

	if (pageNum >= 732 && pageNum <= 734) || pageNum == 738 {
		fullOpenRect(TCC_MOSAIC_RED, 1)
		pageBuffer[4][0] = TCC_ALPHA_CYAN
		for i := 4; i < 23; i++ {
			pos := strings.Index(string(pageBuffer[i]), "www")
			if pos > 0 {
				pageBuffer[i][pos-1] = TCC_ALPHA_YELLOW
			}
		}
		makeDashedLines(TCC_ALPHA_RED)
		bottomBlock(TCC_ALPHA_RED, 23)
	}

	if pageNum >= 735 && pageNum <= 737 {
		if pageNum == 735 {
			fullOpenRect(TCC_MOSAIC_BLUE, 1)
			pageBuffer[4][0] = TCC_ALPHA_CYAN
		} else {
			fullOpenRect(TCC_MOSAIC_BLUE, 2)
			pageBuffer[5][0] = TCC_ALPHA_CYAN
		}
		for i := 4; i < 23; i++ {
			pos := strings.Index(string(pageBuffer[i]), "www")
			if pos > 0 {
				pageBuffer[i][pos-1] = TCC_ALPHA_YELLOW
			}
			if pageBuffer[i][1] == 0x20 {
				pageBuffer[i+1][0] = TCC_ALPHA_CYAN
			}
		}
		bottomBlock(TCC_ALPHA_BLUE, 23)
	}

	if pageNum == 740 {
		insertDRTekstTVLogo()
		pageBuffer[1][10] = TCC_ALPHA_RED
		pageBuffer[4][10] = TCC_ALPHA_RED
		bottomBlock(TCC_ALPHA_RED, 22)
	}

	if pageNum >= 741 && pageNum <= 743 {
		pageBuffer[1][0] = TCC_ALPHA_RED
		pageBuffer[2][0] = TCC_ALPHA_RED
		pageBuffer[1][1] = TCC_NEW_BACKGROUND
		pageBuffer[2][1] = TCC_NEW_BACKGROUND
		pageBuffer[1][2] = TCC_ALPHA_WHITE
		pageBuffer[2][2] = TCC_ALPHA_WHITE
		pageBuffer[1][3] = TCC_DOUBLE_HEIGHT
		pageBuffer[2][3] = TCC_DOUBLE_HEIGHT
		pageBuffer[1][15] = TCC_NEW_BACKGROUND
		pageBuffer[2][15] = TCC_NEW_BACKGROUND
		pageBuffer[1][16] = TCC_ALPHA_BLACK
		pageBuffer[2][16] = TCC_ALPHA_BLACK
		whiteSeperatedBoxes(23)
	}

	// TEKST-TV TESTSIDE
	if pageNum == 744 {
		switch subPageNr {
		case "0", "1":
			pageBuffer[18][20] = TCC_CONCEAL

			pageBuffer[19][3] = TCC_ALPHA_WHITE
			pageBuffer[19][8] = TCC_ALPHA_YELLOW
			pageBuffer[19][12] = TCC_ALPHA_CYAN
			pageBuffer[19][17] = TCC_ALPHA_GREEN
			pageBuffer[19][22] = TCC_ALPHA_MAGENTA
			pageBuffer[19][30] = TCC_ALPHA_RED
			pageBuffer[19][34] = TCC_ALPHA_BLUE

			pageBuffer[20][3] = TCC_ALPHA_WHITE
			pageBuffer[20][4] = TCC_NEW_BACKGROUND
			pageBuffer[20][7] = TCC_ALPHA_YELLOW
			pageBuffer[20][8] = TCC_NEW_BACKGROUND
			pageBuffer[20][11] = TCC_ALPHA_CYAN
			pageBuffer[20][12] = TCC_NEW_BACKGROUND
			pageBuffer[20][16] = TCC_ALPHA_GREEN
			pageBuffer[20][17] = TCC_NEW_BACKGROUND
			pageBuffer[20][21] = TCC_ALPHA_MAGENTA
			pageBuffer[20][22] = TCC_NEW_BACKGROUND
			pageBuffer[20][29] = TCC_ALPHA_RED
			pageBuffer[20][30] = TCC_NEW_BACKGROUND
			pageBuffer[20][33] = TCC_ALPHA_BLUE
			pageBuffer[20][34] = TCC_NEW_BACKGROUND

			pageBuffer[21][2] = TCC_FLASH
			pageBuffer[22][2] = TCC_DOUBLE_HEIGHT
		case "4":
			for row := 2; row < 23; row++ {
				pageBuffer[row][0] = TCC_MOSAIC_WHITE
				for col := 1; col < 39; col++ {
					if row%2 != 0 {
						if col%3 == 0 {
							pageBuffer[row][col] = 0xFA
						} else {
							pageBuffer[row][col] = 0xF0
						}
					} else {
						if col%3 == 0 {
							pageBuffer[row][col] = 0xEA
						}
					}

				}
			}

		case "5":
			for i := 1; i < 24; i++ {
				pageBuffer[i][0] = TCC_ALPHA_RED
				pageBuffer[i][1] = TCC_NEW_BACKGROUND
			}
			pageBuffer[23][2] = TCC_ALPHA_WHITE

		case "6":
			for i := 1; i < 24; i++ {
				pageBuffer[i][0] = TCC_ALPHA_GREEN
				pageBuffer[i][1] = TCC_NEW_BACKGROUND
			}
			pageBuffer[23][2] = TCC_ALPHA_BLUE

		case "7":
			for i := 1; i < 24; i++ {
				pageBuffer[i][0] = TCC_ALPHA_BLUE
				pageBuffer[i][1] = TCC_NEW_BACKGROUND
			}
			pageBuffer[23][2] = TCC_ALPHA_WHITE
		}
	}

	if pageNum == 745 {
		fullOpenRect(TCC_MOSAIC_RED, 1)
		pageBuffer[23][1] = TCC_NEW_BACKGROUND
		pageBuffer[23][2] = TCC_ALPHA_RED
		pageBuffer[23][22] = TCC_ALPHA_BLUE
	}

	if pageNum == 746 {
		insertDRTekstTVLogo()
		for i := 4; i < 23; i++ {
			pageBuffer[i][0] = TCC_ALPHA_CYAN
			pageBuffer[i][10] = TCC_ALPHA_WHITE
		}
		whiteSeperatedBoxes(23)
	}

	if pageNum == 756 {
		pageBuffer[1][0] = TCC_ALPHA_BLUE
		pageBuffer[1][1] = TCC_NEW_BACKGROUND
		pageBuffer[1][2] = TCC_ALPHA_BLACK
		pageBuffer[1][3] = TCC_DOUBLE_HEIGHT
		pageBuffer[2][0] = TCC_ALPHA_BLUE
		pageBuffer[2][1] = TCC_NEW_BACKGROUND
		pageBuffer[2][2] = TCC_ALPHA_BLACK
		pageBuffer[2][3] = TCC_DOUBLE_HEIGHT
		copy(pageBuffer[1][4:], "L O T T E R I E R")
		whiteSeperatedBoxes(22)
		whiteSeperatedBoxes(23)
		for i := 3; i < 23; i++ {
			if pageBuffer[i][3] != 0x20 {
				pageBuffer[i][0] = TCC_DOUBLE_HEIGHT
			}
		}
	}

	if pageNum == 757 || pageNum == 758 {
		sub, _ := strconv.Atoi(subPageNr)
		if sub < 2 {
			fullOpenRect(TCC_MOSAIC_GREEN, 2)
		} else {
			fullOpenRect(TCC_MOSAIC_GREEN, 1)
		}
		pageBuffer[1][22] = TCC_NEW_BACKGROUND
		pageBuffer[1][23] = TCC_ALPHA_BLACK
		copy(pageBuffer[1][24:], "DR UDEN ANSVAR")
		pageBuffer[1][38] = TCC_ALPHA_GREEN
		for i := 2; i < 23; i++ {
			if i > 5 && sub < 2 && pageBuffer[i][4] != 0x20 {
				pageBuffer[i][0] = TCC_ALPHA_GREEN
				pageBuffer[i][1] = TCC_NEW_BACKGROUND
				pageBuffer[i][2] = TCC_ALPHA_BLACK
			}
			pos := strings.Index(string(pageBuffer[i]), "GEVINSTER")
			if pos > 2 {
				pageBuffer[i][0] = TCC_ALPHA_GREEN
				pageBuffer[i][1] = TCC_NEW_BACKGROUND
				pageBuffer[i][2] = TCC_ALPHA_BLACK
			}
			if i > 6 && sub > 2 {
				if i%2 == 0 {
					pageBuffer[i][0] = TCC_ALPHA_WHITE
				} else {
					pageBuffer[i][0] = TCC_ALPHA_GREEN
				}
			}
		}
		whiteSeperatedBoxes(23)
	}

	if pageNum == 760 {
		openBlueRectSolidBlueBox(27)
		whiteSeperatedBoxes(17)
		whiteSeperatedBoxes(18)
		whiteSeperatedBoxes(19)
		pageBuffer[20][0] = TCC_ALPHA_CYAN
		pageBuffer[20][1] = TCC_NEW_BACKGROUND
		pageBuffer[20][2] = TCC_ALPHA_BLACK
		bottomBlock(TCC_MOSAIC_BLUE, 0)
	}

	if pageNum == 761 {
		copy(pageBuffer[1], pageBuffer[2])
		pageBuffer[2] = bytes.Repeat([]byte{0x20}, 40)
		blueWhiteBlueBar(6, 29)
		whiteSeperatedBoxes(18)
		whiteSeperatedBoxes(19)
		whiteSeperatedBoxes(20)
		bottomBlock(TCC_MOSAIC_BLUE, 0)
	}

	if pageNum == 762 || pageNum == 765 || pageNum == 768 || pageNum == 771 || pageNum == 773 ||
		pageNum == 775 || pageNum == 777 || pageNum == 779 || pageNum == 783 || pageNum == 786 {
		blueWhiteBlueBar(27, 0)
		pageBuffer[3][0] = TCC_ALPHA_CYAN
		whiteSeperatedBoxes(23)
	}

	if pageNum == 764 || pageNum == 767 || pageNum == 770 || pageNum == 772 || pageNum == 774 ||
		pageNum == 776 || pageNum == 778 || pageNum == 782 || pageNum == 785 {
		copy(pageBuffer[1], pageBuffer[2])
		pageBuffer[2] = bytes.Repeat([]byte{0x20}, 40)
		blueWhiteBlueBar(6, 29)
		whiteSeperatedBoxes(18)
		whiteSeperatedBoxes(19)
		whiteSeperatedBoxes(20)
		bottomBlock(TCC_MOSAIC_BLUE, 0)
	}

	// Drømmelholdet
	if pageNum == 780 || pageNum == 781 {
		blueWhiteBlueBar(7, 21)
		copy(pageBuffer[2], pageBuffer[1])
		pageBuffer[1][3] = TCC_DOUBLE_HEIGHT
		pageBuffer[4][0] = TCC_ALPHA_CYAN
		whiteSeperatedBoxes(22)
	}

	// fastext row
	copy(pageBuffer[24][2:], "\x01Nyheder")
	copy(pageBuffer[24][14:], "\x02Sport")
	copy(pageBuffer[24][24:], "\x03TV")
	copy(pageBuffer[24][31:], "\x06Vejret")

	return pageBuffer, nav, nil
}

