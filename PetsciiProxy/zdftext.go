package main

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"
)


var tagRegex = regexp.MustCompile(`\{([A-Za-z0-9]+)\}`)


// --- ZDF-TEXT ---

func evalUnary(jsSnippet string) int {
	// The JS challenge builds digits like this: (+!+[]+!![]+!![]) = 3
	// We count the occurrences of "!![]" (which is 1) and "!+[]" (which is 1)
	ones := strings.Count(jsSnippet, "!![]") + strings.Count(jsSnippet, "!+[]")
	return ones
}


func extractNumber(body, variableName string) int {
	// Finds the pattern: s=+((...)+(...)...)
	re := regexp.MustCompile(variableName + `=\+\((.*?)\),`)
	match := re.FindStringSubmatch(body)
	if len(match) < 2 {
		return 0
	}

	// Split the parts like (+!+[])+(+!+[]+!![])
	parts := strings.Split(match[1], ")+(")
	resultStr := ""
	for _, part := range parts {
		digit := evalUnary(part)
		resultStr += strconv.Itoa(digit)
	}

	val, _ := strconv.Atoi(resultStr)
	return val
}


func setHeaders(req *http.Request, referer string) {
	h := req.Header
	h.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36")
	h.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8")
	h.Set("Accept-Language", "de-DE,de;q=0.9,en-US;q=0.8,en;q=0.7")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("Upgrade-Insecure-Requests", "1")

	// Security headers (The "Sec-" group)
	h.Set("Sec-Fetch-Dest", "document")
	h.Set("Sec-Fetch-Mode", "navigate")
	h.Set("Sec-Fetch-Site", "none") // Change to "same-origin" for the 2nd and 3rd request
	h.Set("Sec-Fetch-User", "?1")

	// If we have a referer (for step 2 and 3 of the handshake), use it!
	if referer != "" {
		h.Set("Referer", referer)
		h.Set("Sec-Fetch-Site", "same-origin")
	}
}


func zdftextGetTeletexPage(pageNr string, zdfStation string, dirStation string) bool {
	var url string
	parts := strings.Split(pageNr, "-")
	subPage, _ := strconv.Atoi(parts[1])

	if subPage < 2 {
		url = fmt.Sprintf("https://teletext.zdf.de/teletext/%s/seiten/klassisch/%s.html", zdfStation, parts[0])
	} else {
		subPage--
		subStr := strconv.Itoa(subPage)
		url = fmt.Sprintf("https://teletext.zdf.de/teletext/%s/seiten/klassisch/%s_%s.html", zdfStation, parts[0], subStr)
	}

	logFetchingPage(url)

	// ZDF added some abuse checks / bot detection
	// We have to solve a javascript puzzle to be able to continue
	// But not always, sometime we get lucky and the initial call works straight away

	//  Setup a CookieJar to catch the verification cookie
	jar, _ := cookiejar.New(nil)

	// Configure custom TLS transport settings to ignore expired certificates
	customTransport := &http.Transport{
		TLSClientConfig: &tls.Config{
			InsecureSkipVerify: true, // Bypasses the x509 validation check
		},
	}

	client := &http.Client{
		Jar:       jar,
		Transport: customTransport, // Inject our bypass settings
		Timeout:   15 * time.Second,
	}

	req, _ := http.NewRequest("GET", url, nil)
	setHeaders(req, "")

	resp, err := client.Do(req)
	if err != nil {
		fmt.Println(">>err: client.Do(req):", err)
		return false
	}
	if resp.StatusCode != 200 {
		fmt.Println(">>err: unexpected status:", resp.StatusCode)
		return true
	}
	bodyBytes, _ := io.ReadAll(resp.Body)
	body := string(bodyBytes)
	resp.Body.Close()

	gotLucky := false
	var reader io.ReadCloser
	// CHECK: Did we get lucky and bypass the challenge entirely?
	if strings.Contains(body, "ZDFtext - Seite") || strings.Contains(body, "id=\"headline\"") {
		//		fmt.Println("Bypassed challenge completely! Parsing page directly...")
		reader = io.NopCloser(bytes.NewReader(bodyBytes))
		gotLucky = true
	}

	if !gotLucky {
		fmt.Println(">>Starting challenge...")

		// Solve the Challenge
		//tsMatch := regexp.MustCompile(`'ts','(\d+)'`).FindStringSubmatch(body)
		//oMatch := regexp.MustCompile(`o='(/z[^']*)'`).FindStringSubmatch(body)

		// New relaxed regex allows single quotes, double quotes, and spaces
		reTS := regexp.MustCompile(`['"]ts['"]\s*,\s*['"](\d+)['"]`)
		tsMatch := reTS.FindStringSubmatch(body)

		// New relaxed regex captures the endpoint path regardless of variable name or quotes
		reAction := regexp.MustCompile(`(?:o|path|action)\s*=\s*['"](/z[^'"]*)['"]`)
		oMatch := reAction.FindStringSubmatch(body)

		if len(tsMatch) < 2 || len(oMatch) < 2 {
			//fmt.Println("Failed to find challenge tokens")
			fmt.Println(">>err: Bot detected or format changed; please report to author of Teletext64U")
			fmt.Println(">>     Status Code received:", resp.StatusCode)
			// dump some content
			if len(body) > 1000 {
				fmt.Println(body[:1000])
			} else {
				fmt.Println(body)
			}
			return true
		}

		sVal := extractNumber(body, "s")
		yVal := extractNumber(body, "Y")
		wsidchk := sVal + yVal

		// Send validation request; "proves" to the server we ran the JS, and sets the cookie in our jar
		verifyURL := fmt.Sprintf("https://teletext.zdf.de%s?ts=%s&wsidchk=%d&pdata=https%%3A%%2F%%2Fteletext.zdf.de%%2Fteletext%%2Fzdf%%2Fseiten%%2Fklassisch%%2F100.html",
			oMatch[1], tsMatch[1], wsidchk)

		verifyReq, _ := http.NewRequest("GET", verifyURL, nil)
		setHeaders(verifyReq, url)
		verifyResp, err := client.Do(verifyReq)
		if err != nil {
			return false
		}
		verifyResp.Body.Close()

		// Fetch the actual teletext page, now with cookies!
		finalReq, _ := http.NewRequest("GET", url, nil)
		setHeaders(finalReq, verifyURL)
		finalResp, err := client.Do(finalReq)
		if err != nil {
			return false
		}
		defer finalResp.Body.Close()

		finalBody, _ := io.ReadAll(finalResp.Body)
		reader = io.NopCloser(bytes.NewReader(finalBody))
		//fmt.Println("Page content:", string(finalBody))
		fmt.Println(">>Challenge completed")
	}

	var nav NavignationInfo
	rows, nav := parseZDFRows(reader, zdfStation, parts[0])

	// Optional directives for (sub)page navigation
	pp := ""
	np := ""
	ps := ""
	ns := ""
	subPage, _ = strconv.Atoi(parts[1])
	nav.prevSubpage = subPage - 1
	if subPage+2 <= nav.numberOfSubpages {
		nav.nextSubpage = subPage + 2
	}
	currentPage = parts[0]
	if nav.numberOfSubpages > 1 {
		ps, ns, _ = getPrevNextSubpage(parts[0], nav)
	}
	pp, np = buildPageNavDirectives(nav.prevPage, nav.nextPage)

	// Note: the ftl - fastext links are fixed for now; it could be made dynamic in a future release
	// Übersicht (100), Nachrichten (112), Sport (200), Wetter (170)
	// aka: Overview, News, Sport, Weather
	ftl2 := "112-0"
	ftl3 := "200-0"
	if strings.Contains(zdfStation, "info") || strings.Contains(zdfStation, "neo") {
		ftl3 = "300-0"
	}
	ftl4 := "170-0"
	if strings.Contains(zdfStation, "3sat") {
		ftl2 = "500-0"
		ftl3 = "300-0"
		ftl4 = "400-0"
	}
	var output []byte
	output = append(output, []byte(fmt.Sprintf(
		"%v%v%v%vftl=100-0\nftl=%v\nftl=%v\nftl=%v\n<pre>", pp, np, ps, ns, ftl2, ftl3, ftl4))...)

	for _, r := range rows {
		output = append(output, r...)
	}

	output = append(output, []byte("</pre>")...)
	savePage(dirStation, pageNr, output)
	return true
}


func parseZDFRows(body io.ReadCloser, zdfStation string, pageNr string) ([][]byte, NavignationInfo) {
	defer body.Close()

	var nav NavignationInfo

	pageBuffer := newPageBuffer(25)

	rawData, err := io.ReadAll(body)
	if err != nil {
		return pageBuffer, nav
	}

	z := html.NewTokenizer(strings.NewReader(string(rawData)))

	currentRow := -1
	currentCol := 0
	prevFgCode := byte(TCC_ALPHA_WHITE)
	prevBgCode := byte(TCC_ALPHA_BLACK)
	isMosaic := false
	// A span whose fg is black and has no bc attribute is a black-filler span.
	// &nbsp; content must be suppressed; otherwise every leading filler span
	// writes a 0x20 space and pushes all row content 20+ columns to the right.
	skipNbsp := false
	spaceCounter := 0
	//new
	bgTransitionCol := -1

	resetRowState := func() {
		currentCol = 0
		prevFgCode = TCC_ALPHA_WHITE
		prevBgCode = TCC_ALPHA_BLACK
		isMosaic = false
		skipNbsp = false
		spaceCounter = 0
		// new
		bgTransitionCol = -1
	}

	writeAt := func(pos int, b byte) {
		if currentRow >= 0 && currentRow < 24 && pos >= 0 && pos < 40 {
			if pos == 39 && pageBuffer[currentRow][39] != 0x20 {
				return
			}
			pageBuffer[currentRow][pos] = b
		}
	}

	writeCurrent := func(b byte) {
		if spaceCounter < 20 {
			spaceCounter++
			return
		}
		if currentRow >= 0 && currentRow < 24 && currentCol < 40 {
			pageBuffer[currentRow][currentCol] = b
			currentCol++
		}
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

			case "body":
				for _, attr := range token.Attr {
					if attr.Key == "subpages" {
						valInt, err := strconv.Atoi(attr.Val)
						if err == nil {
							nav.numberOfSubpages = valInt
						}
						continue
					}
					if attr.Key == "prevpg" {
						valInt, err := strconv.Atoi(attr.Val)
						if err == nil {
							nav.prevPage = valInt
						}
						continue
					}
					if attr.Key == "nextpg" {
						valInt, err := strconv.Atoi(attr.Val)
						if err == nil {
							nav.nextPage = valInt
						}
						continue
					}
				}

			case "div":
				for _, attr := range token.Attr {
					if attr.Key != "id" {
						continue
					}
					if attr.Val == "headline" {
						currentRow = 0
						resetRowState()
					} else if strings.HasPrefix(attr.Val, "row_") {
						n, err := strconv.Atoi(strings.TrimPrefix(attr.Val, "row_"))
						if err == nil {
							currentRow = n + 1
							resetRowState()
						}
					}
				}

			case "span", "a":
				if currentRow < 0 || currentRow > 24 {
					continue
				}

				fgHex, bgHex, mosaic := zdfExtractColors(token)
				fgCode := zdfHexToTCC(fgHex)
				bgCode := zdfHexToTCC(bgHex)
				isMosaic = mosaic

				if isMosaic {
					// turn a TCC_ALPHA_xxx in a TCC_MOSAIC_xxx
					fgCode += 0x10
				}

				skipNbsp = (fgCode == TCC_ALPHA_BLACK && bgHex == "")

				// new background colour?
				if fgHex != "" && bgHex != "" && fgCode == bgCode {
					if bgCode != prevBgCode {
						if currentCol > 0 {
							writeAt(currentCol-1, fgCode)
						}
						writeCurrent(TCC_NEW_BACKGROUND)
						prevFgCode = fgCode
						prevBgCode = bgCode
						skipNbsp = true
						// new
						bgTransitionCol = currentCol - 1
					}
					continue
				}

				// New foreground colour?
				if fgHex != "" && fgCode != prevFgCode {
					// newif currentCol > 0 && (fgCode != TCC_ALPHA_BLACK || bgHex != "") {
					if currentCol > 0 && (fgCode != TCC_ALPHA_BLACK || bgHex != "") && currentCol-1 != bgTransitionCol {
						writeAt(currentCol-1, fgCode)
					}
					prevFgCode = fgCode
				}

				if bgHex != "" && bgCode != prevBgCode {
					if pageNr != "100" && currentRow > 2 && currentCol > 0 {
						if true && fgCode == TCC_ALPHA_WHITE && bgCode == TCC_ALPHA_BLACK {
							writeAt(currentCol, TCC_BLACK_BACKGROUND)

						} else {
							writeAt(currentCol-1, bgCode)
						}
					}
					writeCurrent(TCC_NEW_BACKGROUND)
					prevBgCode = bgCode
					prevFgCode = bgCode
					skipNbsp = true
					// new
					bgTransitionCol = currentCol - 1
				}
			}

		case html.TextToken:
			if currentRow < 0 || currentRow >= 24 {
				continue
			}
			text := token.Data

			writeZdfRune := func(r rune) {
				if currentCol >= 40 {
					return
				}
				switch {
				case r == '\u00a0': // is a &nbsp;
					if skipNbsp {
						skipNbsp = false
					} else {
						writeCurrent(0x20)
					}
				case r < 0x20:
					// Skip control characters
				default:
					var b byte
					if r <= 0x7E {
						b = byte(r)
					} else {
						b = zdfEncodeChar(r)
					}
					if isMosaic {
						b = byte(r)
					}
					// fix letter A should be a 0xFF (solid mosaic block)
					if isMosaic && r == 'A' {
						writeCurrent(0xFF)
					} else {
						writeCurrent(b)
					}
				}
			}

			// 3sat testpage 898 embed teletext control codes as {TAG} placeholders instead of real formatting
			// like {DW}/{DH}/{NH} convention used for YLE Teksti-TV
			if tagRegex.MatchString(text) {
				last := 0
				for _, m := range tagRegex.FindAllStringSubmatchIndex(text, -1) {
					for _, r := range text[last:m[0]] {
						writeZdfRune(r)
					}
					tagName := text[m[2]:m[3]]
					if code, ok := controlMap[tagName]; ok {
						writeCurrent(code)
					}
					last = m[1]
				}
				text = text[last:]
			}
			for _, r := range text {
				writeZdfRune(r)
			}
		}
	}

	// post-fix weather map; update 27-06-2026 v1.8.0
	if pageNr == "171" || pageNr == "172" {
		for j := 6; j < 21; j++ {
			pageBuffer[j][0] = TCC_SEPERATED_MOSAICS
			pageBuffer[j][1] = TCC_HOLD_MOSAICS
			pageBuffer[j][19] = TCC_RELEASE_MOSAICS
			for i := 2; i < 20; i++ {
				if pageBuffer[j][i] >= 0xA0 {
					pageBuffer[j][i] -= 0x80
				}
			}
		}
	}

	// post-fix A-Z index pages
	pageNum, _ := strconv.Atoi(pageNr)

	if zdfStation == "zdfinfo" {
		excludedPages := []int{100, 111, 171, 333}
		if !(slices.Contains(excludedPages, pageNum) || (pageNum > 555 && pageNum < 600)) {
			pageBuffer[2][12] = TCC_BLACK_BACKGROUND
		}
	}

	if zdfStation == "zdfneo" {
		excludedPages := []int{100, 111, 333}
		if !(slices.Contains(excludedPages, pageNum) || (pageNum > 555 && pageNum < 600)) {
			pageBuffer[2][12] = TCC_BLACK_BACKGROUND
		}
	}

	if pageNum > 101 && pageNum < 107 {
		// ZDFtext
		if zdfStation == "zdf" {
			for j := 3; j < 20; j++ {
				if pageBuffer[j][0] == TCC_ALPHA_BLUE && pageBuffer[j][1] == TCC_NEW_BACKGROUND {
					// The forced TCC_BLACK_BACKGROUND stops the blue background be drawn further to the right
					pageBuffer[j][20] = TCC_BLACK_BACKGROUND
					// If there is another index letter on the same row: shift them 1 position to the right
					if pageBuffer[j][21] == TCC_NEW_BACKGROUND {
						pageBuffer[j][24] = pageBuffer[j][23]
						pageBuffer[j][23] = pageBuffer[j][22]
						pageBuffer[j][22] = pageBuffer[j][21]
						pageBuffer[j][21] = TCC_ALPHA_BLUE
					}
				}
			}
		} else {
			// ZDFinfo & ZDFneo
			if strings.Contains(zdfStation, "info") || strings.Contains(zdfStation, "neo") {
				for j := 3; j < 22; j++ {
					if pageBuffer[j][0] == TCC_ALPHA_BLUE && pageBuffer[j][1] == TCC_NEW_BACKGROUND {
						pageBuffer[j][6] = TCC_BLACK_BACKGROUND
					}
				}
			}
			// 3sat
			if strings.Contains(zdfStation, "3sat") {
				for j := 3; j < 22; j++ {
					if pageBuffer[j][0] == TCC_ALPHA_RED && pageBuffer[j][1] == TCC_NEW_BACKGROUND {
						pageBuffer[j][10] = TCC_BLACK_BACKGROUND
					}
					// some weird shit on page 106; they start with a ALPHA_RED followed with A MOSAIC_RED
					if pageBuffer[j][0] == TCC_ALPHA_RED && pageBuffer[j][1] == TCC_MOSAIC_RED {
						pageBuffer[j][1] = TCC_NEW_BACKGROUND
						pageBuffer[j][10] = TCC_BLACK_BACKGROUND
					}
					if pageBuffer[j][20] == TCC_ALPHA_RED && pageBuffer[j][21] == TCC_NEW_BACKGROUND {
						pageBuffer[j][30] = TCC_BLACK_BACKGROUND
					}
				}
			}
		}
	}

	// post-fix row 1+2
	if strings.Contains(zdfStation, "3sat") {
		if pageNum != 100 && pageNum != 111 && pageNum != 300 && pageNum != 898 && pageNum != 899 {
			pageBuffer[1][4] = TCC_NEW_BACKGROUND
			pageBuffer[1][5] = TCC_ALPHA_WHITE
			pageBuffer[2][4] = TCC_NEW_BACKGROUND
			pageBuffer[2][5] = TCC_ALPHA_WHITE
		}
		if pageNum == 300 {
			pageBuffer[1][2] = 0x20
			pageBuffer[1][4] = TCC_ALPHA_BLACK
			pageBuffer[1][5] = 'a'
			pageBuffer[2][4] = 0x20
			pageBuffer[2][5] = 0x20
		}
	}

	// move header 4 positions to the right
	headerSlice := make([]byte, 40)
	copy(headerSlice, pageBuffer[0][5:])
	copy(pageBuffer[0][5:10], bytes.Repeat([]byte{0x20}, 5))
	copy(pageBuffer[0][9:], headerSlice)
	// overwrite data/time from html with system date/time
	copy(pageBuffer[0][18:], []byte(getZdfDate()))

	if strings.Contains(zdfStation, "info") {
		copy(pageBuffer[0][9:], "ZDFinfo")
	}
	if strings.Contains(zdfStation, "neo") {
		copy(pageBuffer[0][9:], "ZDFneo ")
	}
	if strings.Contains(zdfStation, "3sat") {
		copy(pageBuffer[0][9:], "3sat   ")
	}

	// Fixed fastest row
	if zdfStation == "zdf" {
		copy(pageBuffer[24][0:], "\x01\xDCbersicht \x02Nachrichten  \x03Sport  \x06Wetter")
	} else {
		if strings.Contains(zdfStation, "3sat") {
			copy(pageBuffer[24][0:], "\x01\xDCbersicht  \x02Kultur   \x03Programm  \x06Wetter")
		} else {
			copy(pageBuffer[24][0:], "\x01\xDCbersicht\x02Nachrichten \x03Programm \x06Wetter")
		}
	}

	return pageBuffer, nav
}


func zdfExtractColors(token html.Token) (fg, bg string, isMosaic bool) {
	for _, attr := range token.Attr {
		if attr.Key != "class" {
			continue
		}
		isMosaic = strings.Contains(attr.Val, "teletextlinedrawregular")
		parts := strings.Fields(attr.Val)
		for _, p := range parts {
			if strings.HasPrefix(p, "bc") {
				bg = strings.TrimPrefix(p, "bc")
			} else if strings.HasPrefix(p, "c") {
				fg = strings.TrimPrefix(p, "c")
			}
		}
	}
	return
}


func zdfHexToTCC(hex string) byte {
	if len(hex) < 6 {
		return TCC_ALPHA_WHITE
	}

	var r, g, b byte
	fmt.Sscanf(hex[0:2], "%x", &r)
	fmt.Sscanf(hex[2:4], "%x", &g)
	fmt.Sscanf(hex[4:6], "%x", &b)

	rOn := r == 0xFF
	gOn := g == 0xFF
	bOn := b == 0xFF

	switch {
	case !rOn && !gOn && !bOn:
		return TCC_ALPHA_BLACK
	case rOn && !gOn && !bOn:
		return TCC_ALPHA_RED
	case !rOn && gOn && !bOn:
		return TCC_ALPHA_GREEN
	case rOn && gOn && !bOn:
		return TCC_ALPHA_YELLOW
	case !rOn && !gOn && bOn:
		return TCC_ALPHA_BLUE
	case rOn && !gOn && bOn:
		return TCC_ALPHA_MAGENTA
	case !rOn && gOn && bOn:
		return TCC_ALPHA_CYAN
	default: // rOn && gOn && bOn
		return TCC_ALPHA_WHITE
	}
}


func getZdfDate() string {
	now := time.Now()
	days := map[string]string{"Sun": "So", "Mon": "Mo", "Tue": "Di", "Wed": "Mi", "Thu": "Do", "Fri": "Fr", "Sat": "Sa"}
	yearStr := strconv.Itoa(now.Year())
	return fmt.Sprintf("\x02%s %02d.%02d.%s \x03%s", days[now.Format("Mon")], now.Day(), now.Month(), yearStr[2:], now.Format("15:04:05"))
}

