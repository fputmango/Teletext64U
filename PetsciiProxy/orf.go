package main

import (
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"golang.org/x/net/html"
)

// --- ORF text---

func orfGetTeletexPage(pageNr string, station string, dirStation string) bool {
	var url string
	parts := strings.Split(pageNr, "-")
	subPage, _ := strconv.Atoi(parts[1])

	if subPage < 2 {
		url = fmt.Sprintf("https://text.orf.at/channel/%s/page/%s/1.html", station, parts[0])
	} else {
		subStr := strconv.Itoa(subPage)
		url = fmt.Sprintf("https://text.orf.at/channel/%s/page/%s/%s.html", station, parts[0], subStr)
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

	var nav NavignationInfo
	rows, nav := parseORFRows(resp.Body, station, parts[0])

	// Optional directives for (sub)page navigation
	pp := ""
	np := ""
	ps := ""
	ns := ""
	ct := ""
	currentPage = parts[0]
	ps, ns, ct = getPrevNextSubpage(parts[0], nav)
	pp, np = buildPageNavDirectives(nav.prevPage, nav.nextPage)

	ftl1 := "100-0" // ORF 1, 2 and 3 - Übersicht
	ftl2 := "111-0" // ORF 1, 2 and 3 - Schlagzeilen
	ftl3 := "200-0" // ORF 1, 2 - Sport
	ftl4 := "600-0" // ORF 1, 2 and 3 - Wetter
	if strings.Contains(station, "iii") {
		ftl2 = "300-0" // Fernsehen
		ftl3 = "400-0" // Kultur
	}
	if strings.Contains(station, "sport") {
		ftl2 = "200-0" // Sport
		ftl3 = "300-0" // Fernsehen
	}
	var output []byte
	output = append(output, []byte(fmt.Sprintf(
		"%v%v%v%v%vftl=%v\nftl=%v\nftl=%v\nftl=%v\n<pre>", pp, np, ps, ns, ct, ftl1, ftl2, ftl3, ftl4))...)

	row0 := make([]byte, 40)
	for i := range row0 {
		row0[i] = 0x20
	}
	dt := getORFDate()
	stationPage := "\x07" + parts[0]
	switch station {
	case "orf1":
		stationPage = stationPage + "\x06ORF1"
	case "orf2":
		stationPage = stationPage + "\x06ORF2"
	case "orfiii":
		stationPage = stationPage + "\x06ORF III"
	case "sportplus":
		stationPage = stationPage + "\x03ORF SPORT+\x07"
	}
	// first write date + time
	copy(row0[19:], stringToLatin1Bytes(dt))
	// then write pagenumber and station; the reason for this order is because of ORF SPORT+.
	// This text overwrites the day name on purpose to mimick the header row on TV
	copy(row0[7:], []byte(stationPage))

	output = append(output, row0...)

	for _, r := range rows[1:] {
		output = append(output, r...)
	}

	output = append(output, []byte("</pre>")...)
	savePage(dirStation, pageNr, output)
	return true
}

var controlCodeMap = map[string]byte{
	"Black":    TCC_ALPHA_BLACK,
	"Red":      TCC_ALPHA_RED,
	"Green":    TCC_ALPHA_GREEN,
	"Yellow":   TCC_ALPHA_YELLOW,
	"Blue":     TCC_ALPHA_BLUE,
	"Magenta":  TCC_ALPHA_MAGENTA,
	"Cyan":     TCC_ALPHA_CYAN,
	"White":    TCC_ALPHA_WHITE,
	"GBlack":   TCC_MOSAIC_BLACK,
	"GRed":     TCC_MOSAIC_RED,
	"GGreen":   TCC_MOSAIC_GREEN,
	"GYellow":  TCC_MOSAIC_YELLOW,
	"GBlue":    TCC_MOSAIC_BLUE,
	"GMagenta": TCC_MOSAIC_MAGENTA,
	"GCyan":    TCC_MOSAIC_CYAN,
	"GWhite":   TCC_MOSAIC_WHITE,
	"BB":       TCC_BLACK_BACKGROUND,
	"NB":       TCC_NEW_BACKGROUND,
	"Hold":     TCC_HOLD_MOSAICS,
	"Release":  TCC_RELEASE_MOSAICS,
	"DH":       TCC_DOUBLE_HEIGHT,
}

// Extract page or subpage index from url: "/channel/orf1/page/652/1.html" -> returns (652, 1)
func extractPageInfoFromURL(href string) (int, int) {
	parts := strings.Split(href, "/")
	if len(parts) >= 6 {
		p, _ := strconv.Atoi(parts[4])
		subStr := strings.TrimSuffix(parts[5], ".html")
		s, _ := strconv.Atoi(subStr)
		return p, s
	}
	return 0, 0
}

func parseORFRows(body io.Reader, station string, pageNr string) ([][]byte, NavignationInfo) {
	var nav NavignationInfo

	if pageNr == "100" {
		nav.cycleTime = 6
	}

	// Initialize buffer with 25 rows, 40 spaces each
	pageBuffer := newPageBuffer(25)

	z := html.NewTokenizer(body)
	currentRow := 0 //-1
	currentCol := 0
	inTeletextBlock := false
	skipTextToken := false

	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			break
		}

		token := z.Token()

		switch tt {
		case html.TextToken:
			if skipTextToken {
				skipTextToken = false
				continue
			}
			if currentRow < 0 || currentRow >= 25 {
				continue
			}
			text := token.Data
			for _, r := range text {
				if currentCol >= 40 {
					break
				}
				if r == '\u00a0' {
					pageBuffer[currentRow][currentCol] = 0x20
				} else {
					pageBuffer[currentRow][currentCol] = zdfEncodeChar(r)
				}
				currentCol++
			}

		case html.StartTagToken, html.SelfClosingTagToken:
			var classVal, dataLengthVal, dataCharcodeVal, dataInfo, dataPagenumber string
			for _, attr := range token.Attr {
				switch attr.Key {
				case "class":
					classVal = attr.Val
				case "data-length":
					dataLengthVal = attr.Val
				case "data-charcode":
					dataCharcodeVal = attr.Val
				case "data-info":
					dataInfo = attr.Val
				case "data-pagenumber":
					dataPagenumber = attr.Val
				}
			}

			// 1. Navigation Parser Part
			if token.Data == "div" && classVal == "menu" {
				for {
					innerTT := z.Next()
					if innerTT == html.ErrorToken {
						break
					}
					innerToken := z.Token()
					if innerTT == html.EndTagToken && innerToken.Data == "div" {
						break
					}
					if innerTT == html.StartTagToken && innerToken.Data == "a" {
						var subClass, subHref string
						for _, a := range innerToken.Attr {
							if a.Key == "class" {
								subClass = a.Val
							} else if a.Key == "href" {
								subHref = a.Val
							}
						}
						p, s := extractPageInfoFromURL(subHref)
						switch subClass {
						case "pp":
							nav.prevPage = p
						case "ps":
							nav.prevSubpage = s
						case "ns":
							nav.nextSubpage = s
						case "np":
							nav.nextPage = p
						}
					}
				}
				continue
			}

			// 2. Track entrance into teletext content region
			if token.Data == "div" && classVal == "teletext" {
				inTeletextBlock = true
			}

			if !inTeletextBlock {
				continue
			}

			// Handle layout row steps inside Teletext
			if token.Data == "div" && classVal == "line" {
				currentRow++
				currentCol = 0
				continue
			}

			// Process individual data runs
			if token.Data == "div" && classVal == "run" {
				if currentRow < 0 || currentRow >= 25 {
					continue
				}

				length := 1
				if dataLengthVal != "" {
					length, _ = strconv.Atoi(dataLengthVal)
				}
				if length <= 0 {
					continue
				}

				if dataInfo != "" {
					codeName := strings.Trim(dataInfo, "{}")
					if strings.Contains(codeName, "PN") {
						skipTextToken = true
						continue
					}
					if codeByte, found := controlCodeMap[codeName]; found {
						if currentCol < 40 {
							pageBuffer[currentRow][currentCol] = codeByte
							currentCol += length
						}
						skipTextToken = true
						continue
					}
				}

				// If hardcoded mosaic character code is present via hex pattern (e.g., "7Ch")
				if dataCharcodeVal != "" {
					hexStr := strings.TrimSuffix(dataCharcodeVal, "h")
					if val, err := strconv.ParseUint(hexStr, 16, 8); err == nil {
						if currentCol < 40 {
							pageBuffer[currentRow][currentCol] = byte(val)
							currentCol += length
						}
					}
					skipTextToken = true
					continue
				}

				if dataPagenumber != "" {
					skipTextToken = true
				}
			}
		}
	}

	// Static Footer Fastext generation defaults overrides
	switch station {
	case "orf1", "orf2":
		copy(pageBuffer[24][0:], "\x01\xDCbersicht \x02Schlagzeilen \x03Sport \x06Wetter")
	case "orfiii":
		copy(pageBuffer[24][0:], "\x01\xDCbersicht\x02Fernsehen\x03Kultur+Show \x06Wetter")
	case "sportplus":
		copy(pageBuffer[24][0:], "\x01\xDCbersicht  \x02Sport   \x03Fernsehen  \x06Wetter")
	}

	return pageBuffer, nav
}
