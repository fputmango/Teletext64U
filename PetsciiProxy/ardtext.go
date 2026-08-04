package main

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ARD Text
// These characters are used in ARD-TEXT html classes, e.g. class='fgy bgb' means yellow character on a black background
var ardColorMap = map[string]byte{
	"b ": 0, // black, note: I have added black twice with an explicit space and single quote to prevent
	"b'": 0, // black        the function that processes colors to pick blue accidently
	"r":  1, // red
	"g":  2, // green
	"y":  3, // yellow
	"bl": 4, // blue
	"m":  5, // magenta
	"c":  6, // cyan
	"w":  7, // white
}

// html acccent marks with corresponding teletext values and other entities (far from complete, but all we need for now)
var entityMap = map[string]byte{
	"nbsp":   0x20,
	"gt":     '>',
	"lt":     '<',
	"euml":   0xEB, // ë
	"eacute": 0xE9, // é
	"ecirc":  0xEA, // ê
	"egrave": 0xE8, // è
	"iacute": 0xED, // í
	"aacute": 0xE1, // á
	"acirc":  0xE2, // â
	"szlig":  0xDF, // ß
	"Auml":   0xC4, // Ä
	"Ouml":   0xD6, // Ö
	"Uuml":   0xDC, // Ü
	"auml":   0xE4, // ä
	"ouml":   0xF6, // ö
	"uuml":   0xFC, // ü
	"iuml":   0xEF, // ï
}

// Used to determine mosaic/graphic character in ARD-TEXT (seems also being used at text.orf.at)
var mosaicRe = regexp.MustCompile(`g1[a-z]([0-9a-fA-F]{2})\.gif`)

// --- ARD-TEXT ---

func ardtextHasNextSubpage(page string, subpage string) int {
	subpageNumber, err := strconv.Atoi(subpage)
	if err != nil {
		return -1
	}
	if subpageNumber < 2 {
		subpageNumber = 2
	} else {
		subpageNumber++
	}

	url := fmt.Sprintf("https://www.ard-text.de/page_only.php?page=%s&sub=%v", page, subpageNumber)
	resp, err := http.Get(url)
	if err != nil {
		return -1
	}
	defer resp.Body.Close()

	reader := bufio.NewReader(resp.Body)

	line, err := reader.ReadString('\n')
	if err != nil && err != io.EOF {
		return -1
	}
	if len(line) < 150 {
		return -1
	}
	return subpageNumber
}

func ardtextGetTeletexPage(pageNr string) bool {
	parts := strings.Split(pageNr, "-")
	url := fmt.Sprintf("https://www.ard-text.de/page_only.php?page=%s&sub=%s", parts[0], parts[1])
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

	// determine number of subpages
	var nav NavignationInfo
	nav.nextSubpage = ardtextHasNextSubpage(parts[0], parts[1])
	ps, ns, ct := getPrevNextSubpage(parts[0], nav)

	// Note: the ftl - fastext links are fixed for now; it could be made dynamic in a future release
	// Startseite (100), Sport (200), Wetter (171) and Börse (711)
	// aka: start page, sport, weather, stocks
	// Note: to support prev/next subpage numbers; the full site must be parsed: e.g. https://www.ard-text.de/index.php?page=620
	// and detect something like this: <div id="output_unterseite" class="subpageCounter">1/3</div>
	var output []byte
	output = append(output, []byte(fmt.Sprintf(
		"%v%v%vftl=100-0\nftl=200-0\nftl=171-0\nftl=710-0\n<pre>",
		ps, ns, ct))...)

	row0 := make([]byte, 40)
	for i := range row0 {
		row0[i] = 0x20
	}
	dt := getArdDate()
	start := 5
	row0[start] = byte(TCC_ALPHA_GREEN)
	stationPage := "ARD-TEXT  " + parts[0]
	copy(row0[start+1:], []byte(stationPage))
	row0[start+14] = byte(TCC_ALPHA_YELLOW)
	copy(row0[start+15:], stringToLatin1Bytes(dt))

	output = append(output, row0...)

	rows := parseARDRows(resp.Body, parts[0] != "100")

	if len(rows) > 24 {
		rows = rows[:24]
	}

	for _, r := range rows {
		output = append(output, r...)
	}

	output = append(output, []byte("</pre>")...)
	savePage(DirARD, pageNr, output)
	return true
}

var bgColor = byte(0)
var skipNextSpace = false
var colorPos = byte(0xFF)
var currentRow = 1 // headerline = 0
var colCorrected = false

func parseARDRows(r io.Reader, correctFirstRows bool) [][]byte {
	data, _ := io.ReadAll(r)
	i := 0

	currentRow = 1
	colCorrected = false

	var rows [][]byte
	row := make([]byte, 40)

	col := 0
	currentColor := byte(TCC_ALPHA_WHITE)

	resetRow := func() {
		row = make([]byte, 40)
		for i := range row {
			row[i] = 0x20
		}
		col = 0
		currentColor = TCC_ALPHA_WHITE
		colCorrected = false
	}

	writeChar := func(b byte) {
		if col >= 40 {
			return
		}

		// The ARD-TEXT website pulls off some trick that seems not possible
		// We have to correct some weird html behaviour on row 1, 2 and 3 (every page after 100)
		// Handle code for line 2 (text) and line 1 + 3 (only mosaic)

		if correctFirstRows {
			if !colCorrected && currentRow < 4 {
				if currentRow == 1 || currentRow == 3 {
					if col == 11 {
						// we have to swap and shuffle some bytes here
						var saveValue byte = row[8]
						row[8] = row[9]
						row[10] = row[9]
						row[9] = saveValue
						colCorrected = true
					}
				} else {
					// detect first space
					if col == 15 {
						row[9] = row[8]
						// we need to set a text color, not a mosiac color, so correct this if needed
						if row[9] > 0x10 {
							row[9] -= 0x10
						}
						// extra fully filled mosaic
						row[8] = 0xFF
						row[10] = 0x1D
						row[11] = TCC_ALPHA_WHITE
						row[12] = 0x20
						row[13] = 0x20
						col = 12
						colCorrected = true
					}
				}
			}
		}
		row[col] = b
		col++
	}

	parseEntity := func() {
		start := i
		//for i < len(data) && data[i] != ';' && data[i] != '<' && data[i] != '>' && (i-start) < 8 {
		for i < len(data) && data[i] != ';' && data[i] != '<' && data[i] != '>' && data[i] != '&' && (i-start) < 8 {
			i++
		}

		if i >= len(data) || data[i] != ';' {
			// Not a valid entity — treat '&' as a literal ampersand
			i = start
			writeChar('&')
			return
		}

		entityName := string(data[start:i])

		if b, ok := entityMap[entityName]; ok {

			if b == 0x20 {
				if skipNextSpace && !(col == 1) {
					skipNextSpace = false
				} else {
					skipNextSpace = false
					writeChar(b)
				}
			} else {
				skipNextSpace = false
				writeChar(b)
			}
		}

		// Move past the ';'
		if i < len(data) {
			i++
		}
	}

	parseSpan := func(tag string) {
		for name, val := range ardColorMap {
			// fg and bg same color? Then ok return value will be true -> set bg color!
			tmpVal, ok := ExtractColor(tag)
			if ok {
				bgColor = tmpVal
				currentColor = bgColor
				writeChar(currentColor)
				writeChar(byte(TCC_NEW_BACKGROUND))
				skipNextSpace = true
				return
			}
			if strings.Contains(tag, "fg"+name) && !strings.Contains(tag, ":0px") {
				currentColor = val
				colorPos = byte(col) // save the column to add 0x10 if we encounter a mosaic
				skipNextSpace = true
				writeChar(currentColor)
				return
			}
		}
	}

	parseImg := func(tag string) {
		m := mosaicRe.FindStringSubmatch(tag)

		if len(m) != 2 {
			return
		}

		var v byte
		fmt.Sscanf(m[1], "%x", &v)
		mosaic := byte(v + 0x80)
		writeChar(mosaic)
		skipNextSpace = false
		// correct color control code offset if needed
		if colorPos != 0xFF {
			row[colorPos] += 0x10
			colorPos = 0xFF
		}
	}

	parseTag := func() {
		start := i

		for i < len(data) && data[i] != '>' {
			i++
		}

		tag := string(data[start:i])
		i++

		if strings.HasPrefix(tag, "div") {
			if col > 0 {
				rows = append(rows, row)
			}
			resetRow()
			return
		}

		if strings.HasPrefix(tag, "/div") {
			return
		}

		if strings.HasPrefix(tag, "span") {
			parseSpan(tag)
			return
		}

		if strings.HasPrefix(tag, "img") {
			parseImg(tag)
			return
		}
	}

	resetRow()

	for i < len(data) {
		c := data[i]
		i++

		switch c {
		case '<':
			parseTag()
		case '&':
			parseEntity()
		case '\n', '\r', '\t':
			currentRow++
			continue
		default:
			if c < 32 {
				continue
			}
			skipNextSpace = false
			writeChar(c)
		}
	}

	if col > 0 {
		rows = append(rows, row)
	}

	/*
		Added an extra FastextLinks row to the teletext page.
		Note: ARD-TEXT doesn't have this in their TV-station nor on the internet service.
		What I did (for now): provide some fixed FTL links. I think it's better than nothing.
		Of course, this could be made more fancy with dynamic info from the HTML page in the future.
	*/
	resetRow()
	row[0] = TCC_ALPHA_RED
	copy(row[1:], "Startseite    Sport     Wetter    B\xF6rse") // Börse
	row[12] = TCC_ALPHA_GREEN
	row[22] = TCC_ALPHA_YELLOW
	row[32] = TCC_ALPHA_CYAN
	rows = append(rows, row)

	/*	if correctFirstRows {
			rows[0][2] = TCC_HOLD_MOSAICS
			rows[0][9] = 0x30
			rows[0][10] = 0x14
			rows[1][9] = 0x35
			rows[2][9] = 0x21
		}
	*/
	return rows
}

func ExtractColor(tag string) (byte, bool) {
	// ignore
	if !strings.Contains(tag, ":10px") {
		return 0, false
	}

	fgIdx := strings.Index(tag, "fg")
	bgIdx := strings.Index(tag, "bg")

	if fgIdx == -1 || bgIdx == -1 {
		return 0, false
	}

	extract := func(start int) string {
		// Move pointer past "fg" or "bg"
		ptr := start + 2
		res := ""
		for ptr < len(tag) {
			char := tag[ptr]
			// Stop if we hit a non-lowercase letter (like ':', ' ', or '"')
			if char < 'a' || char > 'z' {
				break
			}
			res += string(char)
			ptr++
		}
		return res
	}

	fgName := extract(fgIdx)
	bgName := extract(bgIdx)

	// detect if both fg and bg are set to the same color => if we encounter this we have to set the background color
	if fgName != "" && fgName == bgName {
		if val, ok := ardColorMap[fgName]; ok {
			return val, true
		}
	}

	return 0, false
}

func getArdDate() string {
	now := time.Now()
	months := map[string]string{"Jan": "Jan", "Feb": "Feb", "Mar": "Mär", "Apr": "Apr", "May": "Mai", "Jun": "Jun", "Jul": "Jul", "Aug": "Aug", "Sep": "Sep", "Oct": "Okt", "Nov": "Nov", "Dec": "Dez"}
	days := map[string]string{"Sun": "Son", "Mon": "Mon", "Tue": "Die", "Wed": "Mit", "Thu": "Don", "Fri": "Fre", "Sat": "Sam"}
	return fmt.Sprintf("%s %02d %s  %s", days[now.Format("Mon")], now.Day(), months[now.Format("Jan")], now.Format("15:04:05"))
}
