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
- ARD TEXT (German: 'Der Teletext im Ersten')
- NMS CEEFAX (British teletext, closed by the BBC in 2012 and recreated by Nathan Dane)
- TEEFAX (British teletext, a community based service with a huge collection of fine teletext art, historical pages and other great stuff)
- YLE Teksti-TV (Finnish / Suomi)
- SVT Text (Swedish teletext)

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
	"encoding/xml"
	"errors"
	"flag"
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
	DirNOS     = "NOS-TT"
	DirARD     = "ARD-TEXT"
	DirZDF     = "ZDF-TEXT"
	DirZDFinfo = "ZDFINFO"
	DirZDFneo  = "ZDFNEO"
	Dir3sat    = "3SAT"
	DirCEEFAX  = "CEEFAX"
	DirTEEFAX  = "TEEFAX"
	DirTEKSTI  = "TEKSTI-TV"
	DirSVT     = "SVT-TEXT"
)

// Each service has its own handler
var handlers = map[string]http.HandlerFunc{
	DirNOS:     nosttHandler,
	DirARD:     ardtextHandler,
	DirZDF:     zdftextHandler,
	DirZDFinfo: zdfinfoHandler,
	DirZDFneo:  zdfneoHandler,
	Dir3sat:    zdf3satHandler,
	DirCEEFAX:  ceefaxHandler,
	DirTEEFAX:  teefaxHandler,
	DirTEKSTI:  tekstiHandler,
	DirSVT:     svttextHandler,
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

// General vars
var prevPage int
var nextPage int
var numberOfSubpages int
var prevSubpage int
var nextSubpage int

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

// END ARD Text

// TEKSTI-TV: XML based

// gets filled with command line parameter
var tekstiAPIkey string = ""

// https://developer.yle.fi/en/api/index.html
// Note: not every control code is listed here
var controlMap = map[string]byte{
	"Black":    TCC_ALPHA_BLACK,
	"Red":      TCC_ALPHA_RED,
	"Green":    TCC_ALPHA_GREEN,
	"Yellow":   TCC_ALPHA_YELLOW,
	"Blue":     TCC_ALPHA_BLUE,
	"Magenta":  TCC_ALPHA_MAGENTA,
	"Cyan":     TCC_ALPHA_CYAN,
	"White":    TCC_ALPHA_WHITE,
	"Flash":    TCC_FLASH,
	"Steady":   TCC_STEADY,
	"GBlack":   TCC_MOSAIC_BLACK,
	"GRed":     TCC_MOSAIC_RED,
	"GGreen":   TCC_MOSAIC_GREEN,
	"GYellow":  TCC_MOSAIC_YELLOW,
	"GBlue":    TCC_MOSAIC_BLUE,
	"GMagenta": TCC_MOSAIC_MAGENTA,
	"GCyan":    TCC_MOSAIC_CYAN,
	"GWhite":   TCC_MOSAIC_WHITE,
	"CG":       TCC_CONTINUOUS_MOSAICS,
	"SG":       TCC_SEPERATED_MOSAICS,
	"NB":       TCC_NEW_BACKGROUND,
	"Hold":     TCC_HOLD_MOSAICS,
	"NH":       TCC_NORMAL_HEIGHT,
	"DH":       TCC_DOUBLE_HEIGHT,
	"BB":       TCC_BLACK_BACKGROUND,
	"Conceal":  TCC_CONCEAL,
	"SB":       TCC_STARTBOX,
}

var tagRegex = regexp.MustCompile(`\{([A-Za-z0-9]+)\}`)

type TeletextLine struct {
	Number int    `xml:"number,attr"`
	Value  string `xml:",chardata"`
}

type Content struct {
	Type  string         `xml:"type,attr"`
	Lines []TeletextLine `xml:"line"`
}

type Subpage struct {
	Number   int       `xml:"number,attr"`
	Contents []Content `xml:"content"`
}

type TeletextPage struct {
	Subpages []Subpage `xml:"subpage"`
}

// END TEKSTI-TV

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

// END SVT Text

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
	var err error

	// Command line parameter flags: name, default value, description
	portPtr := flag.Int("p", 8080, "Listening port number (0-65535)")
	keyPtr := flag.String("k", "", `Yle Teksti-TV API key string. Mandatory if you want to use the Finnish teletext service. 

For the Finnish Yle Teksti-TV service to work you have to use your personal API-key. 
If you do not have one, you can request one here: https://developer.yle.fi/en/index.html`)

	flag.Parse()
	port := *portPtr
	tekstiAPIkey = *keyPtr

	if port < 0 || port > 65535 {
		fmt.Println("Error: Invalid port number (should be in range 0-65535)")
		os.Exit(1)
	}

	if tekstiAPIkey == "" {
		fmt.Printf(">> No Yle Teksti-TV API key provided. Select Teksti-TV in Teletext64U for more information.\n")
	} else {
		if !strings.Contains(tekstiAPIkey, "app_id") || !strings.Contains(tekstiAPIkey, "app_key") {
			fmt.Println("The Teksti-TV API key should contain an app_id and an app_key.")
			fmt.Printf("\r\nStart PetsciiProxy like the example below. Use quotes around the whole API key!\r\n")
			fmt.Printf("petsciiproxy-mac-silicon -k \"app_id=123a456b789c0&app_key=0abc1def2\"\r\n")
			os.Exit(1)
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

// --- NOS Teletekst ---

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
	cleanNrInt, _ := strconv.Atoi(cleanNr)

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

	// post-fix
	// These pages used to have a double heigth row on top. At some point NOS-TT decided
	// to make it normal height and the row below became black. This fix restores double height.
	if (cleanNrInt > 702 && cleanNrInt < 733) || (cleanNrInt > 750 && cleanNrInt < 763) {
		for x := 0; x < 39; x++ {
			if finalBytes[startIndex+5+2*40+x] == 0x20 {
				finalBytes[startIndex+5+2*40+x] = 0x0D
				break
			}
		}
		// fix 3rd row
		finalBytes[startIndex+5+3*40+0] = 0x02 // Green
		finalBytes[startIndex+5+3*40+1] = 0x1D // New Background Color
	}

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

	// Note: the ftl - fastext links are fixed for now; it could be made dynamic in a future release
	// Startseite (100), Sport (200), Wetter (171) and Börse (711)
	// aka: start page, sport, weather, stocks
	var output []byte
	output = append(output, []byte(fmt.Sprintf(
		"pn=p_\npn=n_\nftl=100-0\nftl=200-0\nftl=171-0\nftl=711-0\n<pre>"))...)

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

func getArdDate() string {
	now := time.Now()
	months := map[string]string{"Jan": "Jan", "Feb": "Feb", "Mar": "Mär", "Apr": "Apr", "May": "Mai", "Jun": "Jun", "Jul": "Jul", "Aug": "Aug", "Sep": "Sep", "Oct": "Okt", "Nov": "Nov", "Dec": "Dez"}
	days := map[string]string{"Sun": "Son", "Mon": "Mon", "Tue": "Die", "Wed": "Mit", "Thu": "Don", "Fri": "Fre", "Sat": "Sam"}
	return fmt.Sprintf("%s %02d %s  %s", days[now.Format("Mon")], now.Day(), months[now.Format("Jan")], now.Format("15:04:05"))
}

// --- ZDF-TEXT ---

func zdftextHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	pageName := strings.TrimPrefix(id, "/")
	logPageRequest(DirZDF, pageName)
	zdftextGetTeletexPage(pageName, "zdf", DirZDF)

	path := filepath.Join(DirZDF, pageName)
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

func zdfinfoHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	pageName := strings.TrimPrefix(id, "/")
	logPageRequest(DirZDFinfo, pageName)
	zdftextGetTeletexPage(pageName, "zdfinfo", DirZDFinfo)

	path := filepath.Join(DirZDFinfo, pageName)
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

func zdfneoHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	pageName := strings.TrimPrefix(id, "/")
	logPageRequest(DirZDFneo, pageName)
	zdftextGetTeletexPage(pageName, "zdfneo", DirZDFneo)

	path := filepath.Join(DirZDFneo, pageName)
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

func zdf3satHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	pageName := strings.TrimPrefix(id, "/")
	logPageRequest(Dir3sat, pageName)
	zdftextGetTeletexPage(pageName, "3sat", Dir3sat)

	path := filepath.Join(Dir3sat, pageName)
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

func zdftextGetTeletexPage(pageNr string, zdfStation string, dirStation string) {
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
	resp, err := http.Get(url)
	if err != nil {
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		fmt.Println("HTTP Error: Could not retrieve page", pageNr, "Status:", resp.StatusCode)
		return
	}

	numberOfSubpages = 0
	prevPage = 0
	nextPage = 0
	rows := parseZDFRows(resp.Body, zdfStation, parts[0])

	// optional directives for (sub)page navigation
	pp := ""
	np := ""
	ps := ""
	ns := ""
	subPage, _ = strconv.Atoi(parts[1])
	prevSubpage = subPage - 1
	nextSubpage = subPage + 1
	currentPage = parts[0]
	if numberOfSubpages > 1 {
		if prevSubpage > 0 {
			ps = "pn=ps" + currentPage + "-" + strconv.Itoa(prevSubpage) + "\n"
		}
		if nextSubpage <= numberOfSubpages {
			ns = "pn=ns" + currentPage + "-" + strconv.Itoa(nextSubpage) + "\n"
		}
	}
	if prevPage > 0 {
		pp = "pn=p_" + strconv.Itoa(prevPage) + "-1\n"
	}
	if nextPage > 0 {
		np = "pn=n_" + strconv.Itoa(nextPage) + "-1\n"
	}

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
	os.WriteFile(filepath.Join(dirStation, pageNr), output, 0644)
}

func parseZDFRows(body io.ReadCloser, zdfStation string, pageNr string) [][]byte {
	defer body.Close()

	pageBuffer := make([][]byte, 25)
	for i := range pageBuffer {
		line := make([]byte, 40)
		for j := range line {
			line[j] = 0x20
		}
		pageBuffer[i] = line
	}

	rawData, err := io.ReadAll(body)
	if err != nil {
		return pageBuffer
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

	resetRowState := func() {
		currentCol = 0
		prevFgCode = TCC_ALPHA_WHITE
		prevBgCode = TCC_ALPHA_BLACK
		isMosaic = false
		skipNbsp = false
		spaceCounter = 0
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
							numberOfSubpages = valInt
						}
						continue
					}
					if attr.Key == "prevpg" {
						valInt, err := strconv.Atoi(attr.Val)
						if err == nil {
							prevPage = valInt
						}
						continue
					}
					if attr.Key == "nextpg" {
						valInt, err := strconv.Atoi(attr.Val)
						if err == nil {
							nextPage = valInt
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
					}
					continue
				}

				// New foreground colour?
				if fgHex != "" && fgCode != prevFgCode {
					if currentCol > 0 && (fgCode != TCC_ALPHA_BLACK || bgHex != "") {
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
				}
			}

		case html.TextToken:
			if currentRow < 0 || currentRow >= 24 {
				continue
			}
			text := token.Data
			for _, r := range text {
				if currentCol >= 40 {
					break
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
		}
	}

	// post-fix weather map mosaics
	if pageNr == "171" || pageNr == "172" {
		for j := 0; j < 24; j++ {
			for i := 0; i < 22; i++ {
				if pageBuffer[j][i] == 0x60 {
					pageBuffer[j][i] = 0xDF
				} else {
					if pageBuffer[j][i] >= 0xA0 {
						pageBuffer[j][i] -= 0x20
					}
				}
			}
		}
	}

	// post-fix A-Z index pages
	pageNum, _ := strconv.Atoi(pageNr)
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

	return pageBuffer
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

func zdfEncodeChar(r rune) byte {
	switch r {
	case 'ä':
		return 0xE4
	case 'ö':
		return 0xF6
	case 'ü':
		return 0xFC
	case 'Ä':
		return 0xC4
	case 'Ö':
		return 0xD6
	case 'Ü':
		return 0xDC
	case 'ß':
		return 0xDF
	case 'é':
		return 0xE9
	case 'è':
		return 0xE8
	case 'ê':
		return 0xEA
	case 'ë':
		return 0xEB
	case 'î':
		return 0xEE
	case 'ï':
		return 0xEF
	case 'à':
		return 0xE0
	case 'â':
		return 0xE2
	case 'ç':
		return 0xE7
	case '°':
		return 0x60
	default:
		if r >= 0x20 && r <= 0x7E {
			return byte(r)
		}
		return 0x20
	}
}

func getZdfDate() string {
	now := time.Now()
	days := map[string]string{"Sun": "So", "Mon": "Mo", "Tue": "Di", "Wed": "Mi", "Thu": "Do", "Fri": "Fr", "Sat": "Sa"}
	yearStr := strconv.Itoa(now.Year())
	return fmt.Sprintf("\x02%s %02d.%02d.%s \x03%s", days[now.Format("Mon")], now.Day(), now.Month(), yearStr[2:], now.Format("15:04:05"))
}

// --- SVT Text ---

func svttextHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	pageName := strings.TrimPrefix(id, "/")
	logPageRequest(DirSVT, pageName)
	svttextGetTeletexPage(pageName)

	path := filepath.Join(DirSVT, pageName)
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

var currentPage string

// This date/time stamp will be fetched from within the HTML page; it is more accurate than using the current date/time from the system
var dateAdded string

func svttextGetTeletexPage(pageNr string) {
	parts := strings.Split(pageNr, "-")
	currentPage = parts[0]
	//url := fmt.Sprintf("https://api.texttv.nu/api/get/%s?app=teletext64u", parts[0])
	url := fmt.Sprintf("https://l.texttv.nu/db/%s?app=teletext64u", currentPage)

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

	// parse all rows; also gives information about the number of subpages
	rows, err := parseSVTRows(resp.Body, parts[1])
	if err != nil {
		fmt.Println(err.Error())
		return
	}
	if len(rows) > 24 {
		rows = rows[:24]
	}

	// optional directives for subpage navigation
	ps := ""
	ns := ""
	subPageIndicator := ""
	nextSubpage = prevSubpage + 2
	if numberOfSubpages > 1 {
		subPageIndicator = "(" + strconv.Itoa(prevSubpage+1) + "/" + strconv.Itoa(numberOfSubpages) + ")"
		if prevSubpage > 0 {
			ps = "pn=ps" + currentPage + "-" + strconv.Itoa(prevSubpage) + "\n"
		}
		if nextSubpage <= numberOfSubpages {
			ns = "pn=ns" + currentPage + "-" + strconv.Itoa(nextSubpage) + "\n"
		}
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
	if numberOfSubpages > 1 {
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

	os.WriteFile(filepath.Join(DirSVT, pageNr), output, 0644)
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

func parseSVTRows(body io.ReadCloser, subpageStr string) ([][]byte, error) {
	defer body.Close()

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
	pageBuffer := make([][]byte, 24)
	for i := range pageBuffer {
		line := make([]byte, 40)
		for j := range line {
			line[j] = 0x20
		}
		pageBuffer[i] = line
	}

	rawData, err := io.ReadAll(body)
	if err != nil {
		return nil, err
	}
	// Turns \" into " and \/ into /
	cleanHTML := strings.ReplaceAll(string(rawData), "\\", "")

	// In SVT Text every pages between 100..899 always exists; we have to check of this text; bail out if page is not available
	if strings.Contains(cleanHTML, "Sidan ej") {
		return pageBuffer, errors.New("page not available")
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
			return nil, z.Err()
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
							prevSubpage = rootCount
							break
						} else {
							inTargetSubpage = false
						}
					}
				}
				numberOfSubpages = rootCount + 1
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
						return pageBuffer, nil
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

	return pageBuffer, nil
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
var fullDoubleHeightRow bool

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
			if lineNumber > 24 {
				break
			}

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
				if col == 3 && c == 0x0D && lineNumber < 24 {
					// we found a full row double height; copy color and new background to next line (apply to Teksti-TV too?)
					rows[lineNumber+1][0] = rows[lineNumber][0]
					rows[lineNumber+1][1] = rows[lineNumber][1]
				}
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
					copy(rows[0][7:], fmt.Sprintf("\x07CEEFAX 1 %s ", pageStr))
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

		// process fastext line if we encounter a FL
		if subpageFound && bytes.HasPrefix(parts[0], []byte("FL")) {
			ftl = bytes.Split(line, []byte(","))
			ftl = ftl[1:5] // we need ftl 1, 2, 3 and 4. Note ftl[1:5] in Go is equal to math notation [1:5)
		}
	}
	// TEEFAX: always force the default header row with current date/time
	if !isCEEFAX {
		rows[0] = bytes.Repeat([]byte{0x20}, 40)
		copy(rows[0][7:], fmt.Sprintf("\x07TEEFAX 1 %s ", pageStr))
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

// --- YLE TEKSTI-TV  ---

func tekstiHandler(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	pageName := strings.TrimPrefix(id, "/")
	logPageRequest(DirTEKSTI, pageName)
	tekstiGetTeletexPage(pageName)

	path := filepath.Join(DirTEKSTI, pageName)
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

func tekstiGetTeletexPage(pageNr string) {
	parts := strings.Split(pageNr, "-")
	var rows [][]byte

	if tekstiAPIkey == "" {
		// show the user a teletext page with instructions how to obtain an API key
		rows = make([][]byte, 24)
		rows[0] = []byte{0x14, 0x1D, 0x17, 0x68, 0x20, 0x68, 0x68, 0x20, 0x70, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20}
		rows[1] = []byte{0x04, 0x1D, 0x17, 0x22, 0x64, 0x26, 0x6A, 0x6A, 0x2C, 0x25, 0x07, 0x54, 0x65, 0x6B, 0x73, 0x74, 0x69, 0x2D, 0x54, 0x56, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20}
		rows[2] = []byte{0x04, 0x1D, 0x17, 0x20, 0x2A, 0x20, 0x2A, 0x22, 0x2C, 0x21, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20}
		rows[3] = []byte{0x06, 0x1D, 0x20, 0x04, 0x79, 0x6C, 0x65, 0x2E, 0x66, 0x69, 0x2F, 0x74, 0x65, 0x6B, 0x73, 0x74, 0x69, 0x74, 0x76, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20}
		rows[4] = []byte{0x14, 0x23, 0x23, 0x23, 0x23, 0x23, 0x23, 0x23, 0x23, 0x23, 0x23, 0x23, 0x23, 0x23, 0x23, 0x23, 0x23, 0x23, 0x23, 0x23, 0x23, 0x23, 0x23, 0x23, 0x23, 0x23, 0x23, 0x23, 0x23, 0x23, 0x23, 0x23, 0x23, 0x23, 0x23, 0x23, 0x23, 0x23, 0x23, 0x23}
		rows[5] = []byte{0x07, 0x20, 0x46, 0x6F, 0x72, 0x20, 0x74, 0x68, 0x65, 0x20, 0x46, 0x69, 0x6E, 0x6E, 0x69, 0x73, 0x68, 0x20, 0x59, 0x6C, 0x65, 0x20, 0x54, 0x65, 0x6B, 0x73, 0x74, 0x69, 0x2D, 0x54, 0x56, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20}
		rows[6] = []byte{0x07, 0x20, 0x73, 0x65, 0x72, 0x76, 0x69, 0x63, 0x65, 0x20, 0x74, 0x6F, 0x20, 0x77, 0x6F, 0x72, 0x6B, 0x2C, 0x20, 0x79, 0x6F, 0x75, 0x20, 0x68, 0x61, 0x76, 0x65, 0x20, 0x74, 0x6F, 0x20, 0x75, 0x73, 0x65, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20}
		rows[7] = []byte{0x07, 0x20, 0x79, 0x6F, 0x75, 0x72, 0x20, 0x70, 0x65, 0x72, 0x73, 0x6F, 0x6E, 0x61, 0x6C, 0x20, 0x41, 0x50, 0x49, 0x2D, 0x6B, 0x65, 0x79, 0x2E, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20}
		rows[8] = []byte{0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20}
		rows[9] = []byte{0x07, 0x20, 0x49, 0x66, 0x20, 0x79, 0x6F, 0x75, 0x20, 0x64, 0x6F, 0x20, 0x6E, 0x6F, 0x74, 0x20, 0x68, 0x61, 0x76, 0x65, 0x20, 0x6F, 0x6E, 0x65, 0x2C, 0x20, 0x79, 0x6F, 0x75, 0x20, 0x63, 0x61, 0x6E, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20}
		rows[10] = []byte{0x07, 0x20, 0x72, 0x65, 0x71, 0x75, 0x65, 0x73, 0x74, 0x20, 0x6F, 0x6E, 0x65, 0x20, 0x68, 0x65, 0x72, 0x65, 0x3A, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20}
		rows[11] = []byte{0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20}
		rows[12] = []byte{0x06, 0x0D, 0x64, 0x65, 0x76, 0x65, 0x6C, 0x6F, 0x70, 0x65, 0x72, 0x2E, 0x79, 0x6C, 0x65, 0x2E, 0x66, 0x69, 0x2F, 0x65, 0x6E, 0x2F, 0x69, 0x6E, 0x64, 0x65, 0x78, 0x2E, 0x68, 0x74, 0x6D, 0x6C, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20}
		rows[13] = []byte{0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20}
		rows[14] = []byte{0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20}
		rows[15] = []byte{0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20}
		rows[16] = []byte{0x12, 0x70, 0x70, 0x70, 0x70, 0x70, 0x70, 0x70, 0x70, 0x70, 0x70, 0x70, 0x70, 0x70, 0x70, 0x70, 0x70, 0x70, 0x70, 0x70, 0x70, 0x70, 0x70, 0x70, 0x70, 0x70, 0x70, 0x70, 0x70, 0x70, 0x70, 0x70, 0x70, 0x70, 0x70, 0x70, 0x70, 0x70, 0x70, 0x70}
		rows[17] = []byte{0x02, 0x1D, 0x04, 0x53, 0x74, 0x61, 0x72, 0x74, 0x20, 0x50, 0x65, 0x74, 0x73, 0x63, 0x69, 0x69, 0x50, 0x72, 0x6F, 0x78, 0x79, 0x20, 0x77, 0x69, 0x74, 0x68, 0x20, 0x74, 0x68, 0x69, 0x73, 0x20, 0x63, 0x6F, 0x6D, 0x6D, 0x61, 0x6E, 0x64, 0x20}
		rows[18] = []byte{0x02, 0x1D, 0x04, 0x6C, 0x69, 0x6E, 0x65, 0x20, 0x70, 0x61, 0x72, 0x61, 0x6D, 0x65, 0x74, 0x65, 0x72, 0x3A, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20}
		rows[19] = []byte{0x02, 0x1D, 0x04, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20}
		rows[20] = []byte{0x02, 0x1D, 0x04, 0x70, 0x65, 0x74, 0x73, 0x63, 0x69, 0x69, 0x70, 0x72, 0x6F, 0x78, 0x79, 0x20, 0x2D, 0x6B, 0x20, 0x22, 0x79, 0x6F, 0x75, 0x72, 0x20, 0x41, 0x50, 0x49, 0x20, 0x6B, 0x65, 0x79, 0x22, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20}
		rows[21] = []byte{0x02, 0x1D, 0x07, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20}
		rows[22] = []byte{0x04, 0x1D, 0x07, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x54, 0x65, 0x6C, 0x65, 0x74, 0x65, 0x78, 0x74, 0x36, 0x34, 0x55, 0x20}
		rows[23] = []byte{0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20, 0x20}
		logFetchingPage("Yle Teksti-TV info screen")
	} else {
		url := fmt.Sprintf("https://external.api.yle.fi/v1/teletext/pages/%s.xml?%s", parts[0], tekstiAPIkey)
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

		if strings.HasPrefix(parts[1], "0") {
			parts[1] = "1"
		}

		rows, err = parseTEKSTIRows(resp.Body, parts[1]) // parts[1] = subpagenumber
		if err != nil {
			fmt.Println("xml.Unmarshal error")
			return
		}
	}

	var output []byte
	/*
		output = append(output, []byte(fmt.Sprintf(
				"pn=p_\npn=n_\nftl=%v-0\nftl=%v-0\nftl=%v-0\nftl=%v-0\n<pre>",
				string(ftl[0]), string(ftl[1]), string(ftl[2]), string(ftl[3])))...)
	*/
	output = append(output, []byte(fmt.Sprintf(
		"pn=p_\npn=n_\nftl=%v-0\nftl=%v-0\nftl=%v-0\nftl=%v-0\n<pre>",
		"100", "200", "300", "400"))...)

	headerRow := bytes.Repeat([]byte{0x20}, 40)
	now := time.Now()
	copy(headerRow[7:], fmt.Sprintf("\x07%s YLE TEKSTI-TV %02d.%02d.%s", parts[0], now.Day(), 3, now.Format("15:04:05")))
	output = append(output, headerRow...)

	for _, r := range rows {
		output = append(output, r...)
	}

	output = append(output, []byte("</pre>")...)
	os.WriteFile(filepath.Join(DirTEKSTI, pageNr), output, 0644)
}

func parseTEKSTIRows(body io.ReadCloser, subpageStr string) ([][]byte, error) {
	defer body.Close()

	// Initialize empty 24x40 grid with spaces (0x20)
	pageBuffer := make([][]byte, 24)
	for i := range pageBuffer {
		line := make([]byte, 40)
		for j := range line {
			line[j] = 0x20
		}
		pageBuffer[i] = line
	}

	decoder := xml.NewDecoder(body)

	// Track state during streaming
	inTargetSubpage := false

	for {
		t, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		switch se := t.(type) {
		case xml.StartElement:
			if se.Name.Local == "subpage" {
				// Check if this subpage matches the requested number
				for _, attr := range se.Attr {
					if attr.Name.Local == "number" && attr.Value == subpageStr {
						inTargetSubpage = true
					}
				}
			}

			// If inside correct subpage, look for <content type="all">
			if inTargetSubpage && se.Name.Local == "content" {
				isAllType := false
				for _, attr := range se.Attr {
					if attr.Name.Local == "type" && attr.Value == "all" {
						isAllType = true
					}
				}

				if isAllType {
					// We are inside the correct block, parse the lines
					if err := decodeTekstiLinesIntoBuffer(decoder, pageBuffer); err != nil {
						return nil, err
					}
					return pageBuffer, nil // Found and processed the target
				}
			}

		case xml.EndElement:
			if se.Name.Local == "subpage" {
				inTargetSubpage = false
			}
		}
	}
	return pageBuffer, nil
}

// Helper to handle the internal line decoding
func decodeTekstiLinesIntoBuffer(decoder *xml.Decoder, buffer [][]byte) error {
	for {
		t, err := decoder.Token()
		if err != nil {
			return err
		}
		switch se := t.(type) {
		case xml.StartElement:
			if se.Name.Local == "line" {
				var lineNum int
				for _, attr := range se.Attr {
					if attr.Name.Local == "number" {
						fmt.Sscanf(attr.Value, "%d", &lineNum)
					}
				}
				content, err := decoder.Token()
				if err != nil {
					return err
				}
				if cd, ok := content.(xml.CharData); ok {
					if lineNum >= 1 && lineNum <= 24 {
						buffer[lineNum-1] = processTekstiLine(string(cd))
					}
				}
			}
		case xml.EndElement:
			if se.Name.Local == "content" {
				return nil
			}
		}
	}
}

func processTekstiLine(input string) []byte {
	output := make([]byte, 0, 40)
	runes := []rune(input)

	for i := 0; i < len(runes); i++ {
		if len(output) >= 40 {
			break
		}
		if runes[i] == '{' {
			end := -1
			for j := i + 1; j < len(runes); j++ {
				if runes[j] == '}' {
					end = j
					break
				}
			}
			if end != -1 {
				tagName := string(runes[i+1 : end])
				if code, ok := controlMap[tagName]; ok {
					output = append(output, code)
					i = end // Move pointer to the '}'
					continue
				}
			}
		}
		output = append(output, encodeTekstiChar(runes[i]))
		//output = append(output, byte(runes[i]))
	}
	for len(output) < 40 {
		output = append(output, 0x20)
	}
	return output[:40]
}

func encodeTekstiChar(r rune) byte {
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
	default:
		if r < 128 {
			return byte(r)
		}
		return 0x20
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
