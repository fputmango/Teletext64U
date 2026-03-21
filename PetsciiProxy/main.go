/*
Petscii Proxy server
Developed by Frank Putman, 2026

This program acts as a middle man between the Commodore 64 Ultimate / other Ultimate products with networking
capabilities. Note: The Ultimate does not support HTTPS (yet), so direct connections to any modern secure
website is not possible.

[Teletext services]  <--HTTPS--> [PetsciiProxy] <--HTTP--> [C64U/Ultimate product]

Functionality:
- HTTPS/HTTP middle man proxy
- Default listening port is 8080; user can override this by starting to program with a parameter
- Parser/transformer

Supported teletext services:
- NOS Teletekst / NOS-TT (Dutch teletext)
- ARD-TEXT (German: 'Der Teletext im Ersten')
- NMS CEEFAX (British teletext, closed by the BBC in 2012 and recreated by Nathan Dane)
- TEEFAX (British teletext, a community based service with a huge collection of fine teletext art, historical pages and other great stuff)

Next up:
- other services which can be parsed

The NOS-TT file format is being used for the other teletext services:
Is set up fairly efficient: mostly around 1073 bytes; a little bit bigger if a page has sub pages.
The file format is a text block with (sub)page and fastext links followed by a <pre>..</pre> block
which contains 1000 bytes of raw teletext data (control codes, text and mosiac/graphic characters)

It looks like this:
    pn=p_503-1
    pn=n_521-1
    pn=ps520-1
    pn=ns520-3
    ftl=101-0
    ftl=102-0
    ftl=103-0
    ftl=601-0
    <pre>
    ...40 columns x 25 rows = 1000 bytes of raw teletext data
    </pre>

Why transform to the NOS-TT format? Basically to keep things simple for the Teletext64U viewer program on the C64.
- It only has to support one uniform way of communicating with this proxy program.
- It only needs to have one routine to decode teletext data.
- Adding a new teletext service within Teletext64U is just adding an item to a table.
*/

package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"
)

// Supported teletext services
const (
	DirNOS    = "NOS-TT"
	DirARD    = "ARD-TEXT"
	DirCEEFAX = "CEEFAX"
	DirTEEFAX = "TEEFAX"
)

// Each service has its own handler
var handlers = map[string]http.HandlerFunc{
	DirNOS:    nosttHandler,
	DirARD:    ardtextHandler,
	DirCEEFAX: ceefaxHandler,
	DirTEEFAX: teefaxHandler,
}

// Teletext control codes (range 0x00..0x1F); Alpha is a regular character; a mosaic is a graphics character
// Note: not every value is used yet in this program; I just added all to be complete here
const (
	TCC_ALPHA_BLACK        = 0x00
	TCC_ALPHA_RED          = 0x01
	TCC_ALPHA_GREEN        = 0x02
	TCC_ALPHA_YELLOW       = 0x03
	TCC_ALPHA_BLUE         = 0x04
	TCC_ALPHA_MAGENTA      = 0x05
	TCC_ALPHA_CYAN         = 0x06
	TCC_ALPHA_WHITE        = 0x07
	TCC_FLASH              = 0x08
	TCC_STEADY             = 0x09
	TCC_ENDBOX             = 0x0A
	TCC_STARTBOX           = 0x0B
	TCC_NORMAL_HEIGHT      = 0x0C
	TCC_DOUBLE_HEIGHT      = 0x0D
	TCC_DOUBLE_WIDTH       = 0x0E
	TCC_DOUBLE_SIZE        = 0x0F
	TCC_MOSAIC_BLACK       = 0x10
	TCC_MOSAIC_RED         = 0x11
	TCC_MOSAIC_GREEN       = 0x12
	TCC_MOSAIC_YELLOW      = 0x13
	TCC_MOSAIC_BLUE        = 0x14
	TCC_MOSAIC_MAGENTA     = 0x15
	TCC_MOSAIC_CYAN        = 0x16
	TCC_MOSAIC_WHITE       = 0x17
	TCC_CONCEAL            = 0x18
	TCC_CONTINUOUS_MOSAICS = 0x19
	TCC_SEPERATED_MOSAICS  = 0x1A
	TCC_ESC_GO_SWITCH      = 0x1B
	TCC_BLACK_BACKGROUND   = 0x1C
	TCC_NEW_BACKGROUND     = 0x1D
	TCC_HOLD_MOSAICS       = 0x1E
	TCC_RELEASE_MOSAICS    = 0x1F
)

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

// Used to determine mosaic/graphic character in ARD-TEXT
var mosaicRe = regexp.MustCompile(`g1[a-z]([0-9a-fA-F]{2})\.gif`)

func main() {
	var port int = 8080 // default listening port
	var err error

	// User can override default port with a command line parameter
	if len(os.Args) > 1 {
		port, err = strconv.Atoi(os.Args[1])
		if err != nil {
			fmt.Println("Error: The argument provided is not a valid. Provide a port number.")
			return
		}
		if port < 0 || port > 65535 {
			fmt.Println("Error: Invalid port number (shoud be in range 0-65535)")
			return
		}
	}

	mux := http.NewServeMux()

	// Create folders if needed and assign handler functions for each station
	for name, handler := range handlers {
		err = os.MkdirAll(name, 0755)
		if err != nil {
			fmt.Printf("Could not create folder %s: %v\n", name, err)
		}
		mux.HandleFunc(fmt.Sprintf("/%s/{id}", name), handler)
	}

	fmt.Printf("Teletext PetsciiProxy Go server, serving on port %d\n", port)

	address := fmt.Sprintf(":%d", port)
	err = http.ListenAndServe(address, mux)
	if err != nil {
		fmt.Println("Server error:", err)
	}
}

func nosttHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	// used this for a quick test; actually not needed (for now)
	if id == "/" || id == "/index.html" {
		handleStaticFile(w, "test.html")
		return
	}

	pageName := strings.TrimPrefix(id, "/")
	logPageRequest(DirNOS, pageName)
	nosttGetTeletexPage(pageName)

	path := filepath.Join(DirNOS, pageName)
	if _, err := os.Stat(path); err == nil {
		content, err := os.ReadFile(path)
		if err != nil {
			sendErrorMsg(w, 500, "Internal error reading file")
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=ISO-8859-1")
		w.WriteHeader(200)
		w.Write(content)
	} else {
		sendErrorMsg(w, 404, "Teletext page "+pageName+" not found.")
	}
}

func nosttGetTeletexPage(pageNr string) {
	urlData := fmt.Sprintf("https://teletekst-data.nos.nl/page/%s", pageNr)
	logFetchingPage(urlData)
	client := http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(urlData)
	if err != nil {
		fmt.Println("Connection Error:", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		fmt.Println("HTTP Error: Could not retrieve page", pageNr, "Status:", resp.StatusCode)
		return
	}

	rawData, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Println("Read error:", err)
		return
	}

	txtContent := string(rawData)

	// Build a header that resembles what you see when watching teletext on a regular TV
	days := []string{"maa", "din", "woe", "don", "vri", "zat", "zon"}
	months := []string{"jan", "feb", "mrt", "apr", "mei", "jun", "jul", "aug", "sep", "okt", "nov", "dec"}

	now := time.Now()
	dutchDate := fmt.Sprintf("%s %02d %s",
		days[(int(now.Weekday())+6)%7],
		now.Day(),
		months[now.Month()-1],
	)

	reTime := regexp.MustCompile(`(\d{2}:\d{2}:\d{2})`)
	headerTime := now.Format("15:04:05")

	match := reTime.FindStringSubmatch(txtContent)
	if len(match) > 1 {
		headerTime = match[1]
	}

	cleanNr := strings.Split(pageNr, "-")[0]

	headerText := fmt.Sprintf("\x02NOS-TT  %s\x03%s  %s", cleanNr, dutchDate, headerTime)
	newPreLine := fmt.Sprintf("<pre>%40s", headerText)

	lowerContent := strings.ToLower(txtContent)
	startIndex := strings.Index(lowerContent, "<pre>")

	modifiedContent := txtContent

	if startIndex != -1 {
		reBreak := regexp.MustCompile(`[\r\n]`)
		loc := reBreak.FindStringIndex(txtContent[startIndex:])

		if loc != nil {
			endOfLine := startIndex + loc[0]
			before := txtContent[:startIndex]
			after := txtContent[endOfLine:]
			modifiedContent = before + newPreLine + after
		} else if len(txtContent) >= startIndex+45 {
			modifiedContent = txtContent[:startIndex] + newPreLine + txtContent[startIndex+45:]
		}
	}

	finalBytes := []byte(modifiedContent)
	filePath := filepath.Join(DirNOS, pageNr)

	err = os.WriteFile(filePath, finalBytes, 0644)
	if err != nil {
		fmt.Println("File write error:", err)
		return
	}
}

// --- ARD-TEXT ---

func ardtextHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	pageName := strings.TrimPrefix(id, "/")
	logPageRequest(DirARD, pageName)
	ardtextGetTeletexPage(pageName)

	path := filepath.Join(DirARD, pageName)
	if _, err := os.Stat(path); err == nil {
		content, err := os.ReadFile(path)
		if err != nil {
			sendErrorMsg(w, 500, "Internal error reading file")
			return
		}

		w.Header().Set("Content-Type", "text/plain; charset=ISO-8859-1")
		w.WriteHeader(200)
		w.Write(content)

	} else {
		sendErrorMsg(w, 404, "Teletext page "+pageName+" not found.")
	}
}

func ardtextGetTeletexPage(pageNr string) {
	parts := strings.Split(pageNr, "-")
	url := fmt.Sprintf("https://www.ard-text.de/page_only.php?page=%s&sub=%s", parts[0], parts[1])
	logFetchingPage(url)
	resp, err := http.Get(url)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		fmt.Println("HTTP Error: Could not retrieve page", pageNr, "Status:", resp.StatusCode)
		return
	}

	// Note: the ftl - fast text links are fixed for now; it could be made dynamic in a future release
	// Startseite (100), Sport (200), Wetter (171) and Börse (711)
	// aka: start page, sport, weather, stocks
	var output []byte
	output = append(output, []byte(fmt.Sprintf(
		"pn=p_%s\npn=n_102-1\nftl=100-0\nftl=200-0\nftl=171-0\nftl=711-0\n<pre>",
		pageNr))...)

	row0 := make([]byte, 40)
	for i := range row0 {
		row0[i] = 0x20
	}
	dt := getGermanDate()
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
	os.WriteFile(filepath.Join(DirARD, pageNr), output, 0644)
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
		for i < len(data) && data[i] != ';' {
			i++
		}

		entityName := string(data[start:i])

		if b, ok := entityMap[entityName]; ok {

			if b == 0x20 {
				if skipNextSpace && !(col == 1) {
					skipNextSpace = false
				} else {
					writeChar(b)
				}
			} else {
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
		// correct color control code offset if neededd
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
		Added an extra FastTextLinks row to the teletext page.
		Note: ARD-TEXT doesn't have this in their TV-station nor on the internet service.
		What I did (for now): provide some fixed FTL links. I think it's better than nothing.
		Of course, this could be made more fancy with dynamic info from the HTML page in the future.
	*/
	resetRow()
	row[0] = TCC_ALPHA_RED
	copy(row[1:], "Startseite    Sport     Wetter    Borse")
	row[12] = TCC_ALPHA_GREEN
	row[22] = TCC_ALPHA_YELLOW
	row[32] = TCC_ALPHA_CYAN
	row[36] = 0xF6 // ö
	rows = append(rows, row)

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

func stringToLatin1Bytes(s string) []byte {
	var res []byte
	// Teletext/ISO-8859-1 mapping for German
	for _, r := range s {
		switch r {
		case 'ä':
			res = append(res, 0xE4)
		case 'ö':
			res = append(res, 0xF6)
		case 'ü':
			res = append(res, 0xFC)
		case 'Ä':
			res = append(res, 0xC4)
		case 'Ö':
			res = append(res, 0xD6)
		case 'Ü':
			res = append(res, 0xDC)
		case 'ß':
			res = append(res, 0xDF)
		case '\u00a0':
			res = append(res, 0x20) // Non-breaking space to space
		default:
			if r <= 255 {
				res = append(res, byte(r))
			} else {
				res = append(res, '?')
			}
		}
	}
	return res
}

func getGermanDate() string {
	now := time.Now()
	months := map[string]string{"Jan": "Jan", "Feb": "Feb", "Mar": "Mär", "Apr": "Apr", "May": "Mai", "Jun": "Jun", "Jul": "Jul", "Aug": "Aug", "Sep": "Sep", "Oct": "Okt", "Nov": "Nov", "Dec": "Dez"}
	days := map[string]string{"Sun": "Son", "Mon": "Mon", "Tue": "Die", "Wed": "Mit", "Thu": "Don", "Fri": "Fre", "Sat": "Sam"}
	return fmt.Sprintf("%s %02d %s  %s", days[now.Format("Mon")], now.Day(), months[now.Format("Jan")], now.Format("15:04:05"))
}

// --- CEEFAX ---

func ceefaxHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	pageName := strings.TrimPrefix(id, "/")
	logPageRequest(DirCEEFAX, pageName)
	ceefaxGetTeletexPage(pageName)

	path := filepath.Join(DirCEEFAX, pageName)
	if _, err := os.Stat(path); err == nil {
		content, err := os.ReadFile(path)
		if err != nil {
			sendErrorMsg(w, 500, "Internal error reading file")
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=ISO-8859-1")
		w.WriteHeader(200)
		w.Write(content)
	} else {
		sendErrorMsg(w, 404, "Teletext page "+pageName+" not found.")
	}
}

// --- TEEFAX ---

func teefaxHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	pageName := strings.TrimPrefix(id, "/")
	logPageRequest(DirTEEFAX, pageName)
	teefaxGetTeletexPage(pageName)

	path := filepath.Join(DirTEEFAX, pageName)
	if _, err := os.Stat(path); err == nil {
		content, err := os.ReadFile(path)
		if err != nil {
			sendErrorMsg(w, 500, "Internal error reading file")
			return
		}
		w.Header().Set("Content-Type", "text/plain; charset=ISO-8859-1")
		w.WriteHeader(200)
		w.Write(content)
	} else {
		sendErrorMsg(w, 404, "Teletext page "+pageName+" not found.")
	}
}

var ftl [][]byte // gets filled by parseTTIRows

func ceefaxGetTeletexPage(pageNr string) {
	parts := strings.Split(pageNr, "-")
	url := fmt.Sprintf("https://feeds.nmsni.co.uk/svn/ceefax/Worldwide/P%s.tti", parts[0])
	logFetchingPage(url)
	resp, err := http.Get(url)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		fmt.Println("HTTP Error: Could not retrieve page", pageNr, "Status:", resp.StatusCode)
		return
	}

	rows := parseTTIRows(resp.Body, parts[0], parts[1], true) // parts[1] = subpagenumber

	var output []byte
	output = append(output, []byte(fmt.Sprintf(
		"pn=p_\npn=n_\nftl=%v-0\nftl=%v-0\nftl=%v-0\nftl=%v-0\n<pre>",
		string(ftl[0]), string(ftl[1]), string(ftl[2]), string(ftl[3])))...)

	for _, r := range rows {
		output = append(output, r...)
	}

	output = append(output, []byte("</pre>")...)
	os.WriteFile(filepath.Join(DirCEEFAX, pageNr), output, 0644)
}

func teefaxGetTeletexPage(pageNr string) {
	parts := strings.Split(pageNr, "-")
	url, err := getTeefaxURL(parts[0])
	if err != nil {
		fmt.Printf("Page %s: Error: %v\n", parts[0], err)
	} else {
		//fmt.Printf("Page %s: %s\n", parts[0], url)
	}

	if strings.HasPrefix(pageNr, "100") {
		// Force 2nd subpage to be fetched(1st one has a really big banner on it)
		parts[1] = "2"
	}

	logFetchingPage(url)
	resp, err := http.Get(url)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		fmt.Println("HTTP Error: Could not retrieve page", pageNr, "Status:", resp.StatusCode)
		return
	}

	rows := parseTTIRows(resp.Body, parts[0], parts[1], false) // parts[1] = subpagenumber

	var output []byte
	output = append(output, []byte(fmt.Sprintf(
		"pn=p_\npn=n_\nftl=%v-0\nftl=%v-0\nftl=%v-0\nftl=%v-0\n<pre>",
		string(ftl[0]), string(ftl[1]), string(ftl[2]), string(ftl[3])))...)

	for _, r := range rows {
		output = append(output, r...)
	}

	output = append(output, []byte("</pre>")...)
	os.WriteFile(filepath.Join(DirTEEFAX, pageNr), output, 0644)
}

var subpage byte

func parseTTIRows(r io.Reader, pageStr string, subpageStr string, isCEEFAX bool) [][]byte {
	subpageFound := false
	escFound := false

	// create an empty teletext page, fill it with spaces.
	// The reason why I do this is because in the TTI format only the rows which have actual data are
	// supplied. And where that row needs to be stored is also given.
	rows := make([][]byte, 25)
	spaceRow := bytes.Repeat([]byte{0x00}, 40) //
	for i := range rows {
		rows[i] = make([]byte, 40)
		copy(rows[i], spaceRow)
	}

	data, _ := io.ReadAll(r)
	//	lines := bytes.Split(data, []byte("\r\n"))
	// On TEEFAX there are pages that have mixed \r\n and just \n; fixed
	normalizedData := bytes.ReplaceAll(data, []byte("\r"), []byte(""))
	lines := bytes.Split(normalizedData, []byte("\n"))

	subpage, _ := strconv.Atoi(subpageStr)

	for _, line := range lines {
		// A TTI format teletext line looks something like this: OL,23, D ] CCATCH UP WITH REGIONAL NEWS       G160
		parts := bytes.SplitN(line, []byte(","), 3)

		/*
			Process page number and subpage number. Note: We get all the subpages at once in TTI format, so we
			have to detect which part of the data we need to process. In TTI format, the first row of a new
			teletextpage starts with a PN, e.g. PN,10203. Where 102 is the page number and 03 is the subpage
		*/
		if bytes.HasPrefix(parts[0], []byte("PN")) {
			if subpageFound {
				// Stop processing data if we encounter the next subpage
				break
			}
			// format XXXYY; subpage is last two YY digits
			subpageNumber := parts[1][3:]
			s := string(subpageNumber)
			val, _ := strconv.Atoi(s)
			if (subpage == 0 || subpage == 1) && val == 1 {
				subpageFound = true
			}
			if val == 0 || val == subpage {
				subpageFound = true
			}
		}

		// Actual teletext lines start with an OL
		if subpageFound && bytes.HasPrefix(parts[0], []byte("OL")) {
			numberStr := string(parts[1])
			lineNumber, _ := strconv.Atoi(numberStr)

			col := 0
			for _, c := range parts[2] {
				if c == TCC_ESC_GO_SWITCH {
					escFound = true
					continue
				}
				// If we have found an escape character we have to subtract 0x40 from the next character
				if escFound {
					escFound = false
					c -= 0x40
				}
				//fmt.Printf("row:%v;col:%v\n", lineNumber, col)
				if col < 40 {
					rows[lineNumber][col] = c
				}
				col++
			}

			if lineNumber == 0 {
				if isCEEFAX {
					// We need to modify the header from something like this: ECIMS^BCeefax Worl^F102^A1773576080
					// To what is displayed on a TV (and https://nmsceefax.co.uk/): CEEFAX 1 100 Sun 15 Mar 13:17/09
					// Large number on the right is a unix time stamp
					copy(rows[0][7:], fmt.Sprintf("\x07CEEFAX 1 %s ", pageStr)) // Text is white (0x07)
					unixtime := bytes.Split(rows[0], []byte{0x01})
					timestampStr := string(unixtime[1])
					unixInt64, err := strconv.ParseInt(timestampStr, 10, 64)
					if err != nil {
						fmt.Printf("timeStampStr:%v error strconv: %v\n", timestampStr, err)
					}
					timeStr := formatTime(unixInt64, true)
					copy(rows[0][21:], timeStr)
				}
			}
		}

		// process fast text line if we encounter a FL
		if subpageFound && bytes.HasPrefix(parts[0], []byte("FL")) {
			ftl = bytes.Split(line, []byte(","))
			ftl = ftl[1:5] // we need ftl 1, 2, 3 and 4. Note ftl[1:5] in Go is equal to math notation [1:5)

			//break
		}
	}
	// TEEFAX: always force the default header row with current date/time
	if !isCEEFAX {
		rows[0] = bytes.Repeat([]byte{0x20}, 40)
		copy(rows[0][7:], fmt.Sprintf("\x07TEEFAX 1 %s ", pageStr)) // Text is white (0x07)
		timeStr := formatTime(0, false)
		copy(rows[0][21:], timeStr)
	}
	return rows
}

func bytesToLatin1String(b []byte) string {
	r := make([]rune, len(b))
	for i, v := range b {
		r[i] = rune(v) // Force each byte to be its own Unicode point
	}
	return string(r)
}

func formatTime(timestamp int64, useTimestamp bool) string {
	var days = map[string]string{
		"Mon": "Mon", "Tue": "Tue", "Wed": "Wed", "Thu": "Thu",
		"Fri": "Fri", "Sat": "Sat", "Sun": "Sun",
	}
	var months = map[string]string{
		"Jan": "Jan", "Feb": "Feb", "Mar": "Mar", "Apr": "Apr",
		"May": "May", "Jun": "Jun", "Jul": "Jul", "Aug": "Aug",
		"Sep": "Sep", "Oct": "Oct", "Nov": "Nov", "Dec": "Dec",
	}
	var now time.Time

	if useTimestamp {
		now = time.Unix(timestamp, 0)
	} else {
		now = time.Now()
	}

	// 0x03 is yellow control character
	return fmt.Sprintf("%s %02d %s\x03%s",
		days[now.Format("Mon")],
		now.Day(),
		months[now.Format("Jan")],
		now.Format("15:04/05"),
	)
}

// TEEFAX works a little different compared to CEEFAX. We can't just request pages with a fixed URL. Every
// page can have a unique URL. These are listed in the URL below. So when we want a certain page, we first
// lookup what the URL is in the directory list.
const baseURL = "http://teastop.plus.com/svn/teletext/"

var directoryData []byte
var fetchedDirectoryListing bool = false

func getTeefaxURL(pageID string) (string, error) {
	// Fetch directory listing only at first use; after that we directoryData for reuse
	if !fetchedDirectoryListing {
		// Fetch directory listing
		resp, err := http.Get(baseURL)
		if err != nil {
			return "", err
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return "", fmt.Errorf("failed to fetch directory: %s", resp.Status)
		}
		directoryData, err = io.ReadAll(resp.Body)
		if err != nil {
			return "", err
		}
		fetchedDirectoryListing = true
	}

	// Parse HTML and find the URL of the page to fetch
	z := html.NewTokenizer(bytes.NewReader(directoryData))
	searchPrefix := "P" + pageID
	for {
		tt := z.Next()
		switch tt {
		case html.ErrorToken:
			// End of document
			if z.Err() == io.EOF {
				return "", fmt.Errorf("page %s not found in directory", pageID)
			}
			return "", z.Err()

		case html.StartTagToken:
			t := z.Token()
			// Look for anchor tags <a>
			if t.Data == "a" {
				for _, a := range t.Attr {
					if a.Key == "href" {
						// Check if filename starts with Pxxx
						// Matches "P171.tti", "P171-Index.tti", etc.
						if strings.HasPrefix(a.Val, searchPrefix) {
							// Return the absolute URL
							return baseURL + a.Val, nil
						}
					}
				}
			}
		}
	}
}

func handleStaticFile(w http.ResponseWriter, filename string) {
	data, err := os.ReadFile(filename)
	if err != nil {
		http.Error(w, "Static file not found.", 404)
		return
	}
	w.WriteHeader(200)
	w.Write(data)
}

func sendErrorMsg(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(code)
	w.Write([]byte(message))
}

func logPageRequest(station string, page string) {
	now := time.Now()
	fmt.Printf("%v [%v:%v] - ", now.Format("2006-01-02 15:04:05"), station, page)
}

func logFetchingPage(url string) {
	fmt.Println(url)
}
