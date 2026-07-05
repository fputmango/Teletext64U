/*
Petscii Proxy server
Developed by Frank Putman, 2026

This program acts as a middle man between the Commodore 64 Ultimate / other Ultimate products with networking
capabilities / Original C64 with a WiC64.

[Teletext services]  <--HTTPS--> [PetsciiProxy] <--HTTP--> [C64 Ultimate/WiC64]

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
- ZDF Text, ZDF Info, ZDF Neo (German)
- 3SAT (German)
- DR Tekst-TV (Danish teletext)
- ORF 1, ORF 2, ORF III, ORF Sport+ (Austria)

Next up candidates:
- RTP teletexto (Portugal) - https://www.rtp.pt/wportal/fab-txt/texto/100/100_0001.htm
- HR-text (German) https://www.hr-text.hr-fernsehen.de/ttxweb/?page=100
- ...?

The NOS-TT file format is being used for the other teletext services:
Is set up fairly efficient: mostly around 1073 bytes; a little bit bigger if a page has sub pages.
The file format is a text block with (sub)page and fastext links followed by a <pre>..</pre> block
which contains 1000 bytes of raw teletext data (control codes, text and mosiac/graphic characters)

It looks like this:
    pn=p_503-1
    pn=n_521-1
    pn=ps520-1
    pn=ns520-3
	ct=20
    ftl=101-0
    ftl=102-0
    ftl=103-0
    ftl=601-0
    <pre>
    ...40 columns x 25 rows = 1000 bytes of raw teletext data
    </pre>

Note: the ct=n parameter was added; it's not NOS-TT native. Cycle time is used when rotating between subpages. n is the delay in seconds.

Why transform to the NOS-TT format? Basically to keep things simple for the Teletext64U viewer program on the C64.
- It only has to support one uniform way of communicating with this proxy program.
- It only needs to have one routine to decode teletext data.
- Adding a new teletext service within Teletext64U is just adding an item to a table.
*/

package main

import (
	"bufio"
	"bytes"
	"crypto/tls"
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"

	"net/http/cookiejar"

	"golang.org/x/net/html"
)

// Version
const pp_version = "2.0.0"

// Supported teletext services
const (
	DirNOS      = "NOS-TT"
	DirARD      = "ARD-TEXT"
	DirZDF      = "ZDF-TEXT"
	DirZDFinfo  = "ZDFINFO"
	DirZDFneo   = "ZDFNEO"
	Dir3sat     = "3SAT"
	DirORF1     = "ORF1"
	DirORF2     = "ORF2"
	DirORF3     = "ORF3"
	DirORFSport = "ORFSPORT"
	DirCEEFAX   = "CEEFAX"
	DirTEEFAX   = "TEEFAX"
	DirTEKSTI   = "TEKSTI-TV"
	DirSVT      = "SVT-TEXT"
	DirDR       = "DR-TEKST-TV"
	DirUD       = "UD"
)

// Each service has its own handler
var handlers = map[string]http.HandlerFunc{
	DirNOS:      nosttHandler,
	DirARD:      ardtextHandler,
	DirZDF:      zdftextHandler,
	DirZDFinfo:  zdfinfoHandler,
	DirZDFneo:   zdfneoHandler,
	Dir3sat:     zdf3satHandler,
	DirORF1:     orf1Handler,
	DirORF2:     orf2Handler,
	DirORF3:     orf3Handler,
	DirORFSport: orfSportHandler,
	DirCEEFAX:   ceefaxHandler,
	DirTEEFAX:   teefaxHandler,
	DirTEKSTI:   tekstiHandler,
	DirSVT:      svttextHandler,
	DirDR:       drteksttvHandler,
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

// moved these vars to a struct; I found out that using global vars is very tricky because a
// HTTP request is executed concurrently and global var assignments might lead to variable shadowing
// The nav info gets passed around now the ensure they hold their values
type NavignationInfo struct {
	prevPage         int
	nextPage         int
	prevSubpage      int
	nextSubpage      int
	numberOfSubpages int
	cycleTime        int
}

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

// Used to determine mosaic/graphic character in ARD-TEXT (seems also being used at text.orf.at)
var mosaicRe = regexp.MustCompile(`g1[a-z]([0-9a-fA-F]{2})\.gif`)

// getClientIP extracts the real IP address from the request
func getClientIP(r *http.Request) string {
	// Check for X-Forwarded-For proxy header first
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		ips := strings.Split(xff, ",")
		return strings.TrimSpace(ips[0]) // First IP is always the original client
	}

	// Fallback to X-Real-IP
	if realIP := r.Header.Get("X-Real-IP"); realIP != "" {
		return realIP
	}

	// Ultimate fallback to RemoteAddr (and strip the port)
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr // return unparsed if splitting fails
	}
	return ip
}

// ipLoggingMiddleware logs the client IP for every incoming request
func ipLoggingMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		now := time.Now()
		clientIP := getClientIP(r)
		// Log the IP along with the request method and path
		//		fmt.Printf("%v [CONN] Client IP: %-15s | Request: %s %s", now.Format("2006-01-02 15:04:05"), clientIP, r.Method, r.URL.Path)
		fmt.Printf("%v Client IP: %-15s\n", now.Format("2006-01-02 15:04:05"), clientIP)
		// Pass the request along to the actual handler
		next.ServeHTTP(w, r)
	})
}

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

	// Create UD folder for user config storage and register handler
	// WiC64 firmware v2.1.0 uppercases the URL path, so register both cases
	err = os.MkdirAll(DirUD, 0755)
	if err != nil {
		fmt.Printf("Could not create folder %s: %v\n", DirUD, err)
	}
	mux.HandleFunc("/UD/", udHandler)
	mux.HandleFunc("/ud/", udHandler)
	fmt.Printf("Teletext PetsciiProxy server v%v, serving on port %d\n", pp_version, port)

	address := fmt.Sprintf(":%d", port)
	err = http.ListenAndServe(address, ipLoggingMiddleware(mux))
	if err != nil {
		fmt.Println("Server error:", err)
	}
}

func getPageName(r *http.Request, dirStation string) string {
	id := r.PathValue("id")
	pageName := strings.TrimPrefix(id, "/")
	logPageRequest(dirStation, pageName)
	return pageName
}

func writeResponse(w http.ResponseWriter, dirStation string, pageName string) {
	path := filepath.Join(dirStation, pageName)
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

// --- NOS Teletekst ---

func nosttHandler(w http.ResponseWriter, r *http.Request) {
	pageName := getPageName(r, DirNOS)
	nosttGetTeletexPage(pageName)
	writeResponse(w, DirNOS, pageName)
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

	// post-fix double height
	// These pages used to have a double heigth row on top. At some point NOS-TT decided (probably when
	// they migrated to a new teletext system) to make it normal height and the row below became black.
	if (cleanNrInt > 702 && cleanNrInt < 733) || (cleanNrInt > 750 && cleanNrInt < 763) {
		startIndex += 5 // put startIndex after the '<pre>' closing bracket
		for x := 0; x < 39; x++ {
			if finalBytes[startIndex+2*40+x] == 0x20 {
				finalBytes[startIndex+2*40+x] = 0x0D
				break
			}
		}
		// fix 3rd row
		finalBytes[startIndex+3*40+0] = 0x02 // Green
		finalBytes[startIndex+3*40+1] = 0x1D // New Background Color
		// restore normal height on some pages where the second part of the text is not written with spaces in between
		// overzicht, vooruitzichten, nederland hh:mm, windwaarschuwing, actuele luchtkwaliteit, waarschuwing
		if (cleanNrInt >= 703 && cleanNrInt <= 705) || cleanNrInt == 710 || cleanNrInt == 711 || cleanNrInt == 713 {
			finalBytes[startIndex+13+2*40] = TCC_NORMAL_HEIGHT
		}
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
	pageName := getPageName(r, DirARD)
	ardtextGetTeletexPage(pageName)
	writeResponse(w, DirARD, pageName)
}

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
	pageName := getPageName(r, DirZDF)
	zdftextGetTeletexPage(pageName, "zdf", DirZDF)
	writeResponse(w, DirZDF, pageName)
}

func zdfinfoHandler(w http.ResponseWriter, r *http.Request) {
	pageName := getPageName(r, DirZDFinfo)
	zdftextGetTeletexPage(pageName, "zdfinfo", DirZDFinfo)
	writeResponse(w, DirZDFinfo, pageName)
}

func zdfneoHandler(w http.ResponseWriter, r *http.Request) {
	pageName := getPageName(r, DirZDFneo)
	zdftextGetTeletexPage(pageName, "zdfneo", DirZDFneo)
	writeResponse(w, DirZDFneo, pageName)
}

func zdf3satHandler(w http.ResponseWriter, r *http.Request) {
	pageName := getPageName(r, Dir3sat)
	zdftextGetTeletexPage(pageName, "3sat", Dir3sat)
	writeResponse(w, Dir3sat, pageName)
}

// solveChallenge parses the JS challenge and returns the verification URL
func solveChallenge(body string) (string, error) {
	reTS := regexp.MustCompile(`'ts','(\d+)'`)
	matchTS := reTS.FindStringSubmatch(body)

	// 2. Find the Action URL (o)
	reAction := regexp.MustCompile(`o='(/z[^']*)'`)
	matchAction := reAction.FindStringSubmatch(body)

	if len(matchTS) < 2 || len(matchAction) < 2 {
		return "", fmt.Errorf("could not find challenge parameters")
	}

	verificationURL := fmt.Sprintf("https://teletext.zdf.de%s?ts=%s&wsidchk=%d",
		matchAction[1], matchTS[1], 9922147) // Example total

	return verificationURL, nil
}

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

func setHeaders2(req *http.Request) {
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/avif,image/webp,image/apng,*/*;q=0.8")
	req.Header.Set("Accept-Language", "de-DE,de;q=0.9,en-US;q=0.8,en;q=0.7")
	req.Header.Set("Cache-Control", "no-cache")
	req.Header.Set("Sec-Fetch-Dest", "document")
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	req.Header.Set("Sec-Fetch-Site", "none")

	req.Header.Set("Connection", "keep-alive")
	req.Header.Set("Sec-Fetch-Dest", "document")
	req.Header.Set("Sec-Fetch-Mode", "navigate")
	req.Header.Set("Sec-Fetch-Site", "none")
	req.Header.Set("Sec-Fetch-User", "?1")
	req.Header.Set("Upgrade-Insecure-Requests", "1")
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
	if err != nil || resp.StatusCode != 200 {
		fmt.Println(">>err: client.Do(req):", err)
		return
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
			return
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
			return
		}
		verifyResp.Body.Close()

		// Fetch the actual teletext page, now with cookies!
		finalReq, _ := http.NewRequest("GET", url, nil)
		setHeaders(finalReq, verifyURL)
		finalResp, err := client.Do(finalReq)
		if err != nil {
			return
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
	if nav.prevPage > 0 {
		pp = "pn=p_" + strconv.Itoa(nav.prevPage) + "-1\n"
	}
	if nav.nextPage > 0 {
		np = "pn=n_" + strconv.Itoa(nav.nextPage) + "-1\n"
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

func parseZDFRows(body io.ReadCloser, zdfStation string, pageNr string) ([][]byte, NavignationInfo) {
	defer body.Close()

	var nav NavignationInfo

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

// also used for ORF stations
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

// --- ORF text---

func orf1Handler(w http.ResponseWriter, r *http.Request) {
	pageName := getPageName(r, DirORF1)
	orfGetTeletexPage(pageName, "orf1", DirORF1)
	writeResponse(w, DirORF1, pageName)
}

func orf2Handler(w http.ResponseWriter, r *http.Request) {
	pageName := getPageName(r, DirORF2)
	orfGetTeletexPage(pageName, "orf2", DirORF2)
	writeResponse(w, DirORF2, pageName)
}

func orf3Handler(w http.ResponseWriter, r *http.Request) {
	pageName := getPageName(r, DirORF3)
	orfGetTeletexPage(pageName, "orfiii", DirORF3)
	writeResponse(w, DirORF3, pageName)
}

func orfSportHandler(w http.ResponseWriter, r *http.Request) {
	pageName := getPageName(r, DirORFSport)
	orfGetTeletexPage(pageName, "sportplus", DirORFSport)
	writeResponse(w, DirORFSport, pageName)
}

func orfGetTeletexPage(pageNr string, station string, dirStation string) {
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
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		fmt.Println("HTTP Error: Could not retrieve page", pageNr, "Status:", resp.StatusCode)
		return
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
	if nav.prevPage > 0 {
		pp = "pn=p_" + strconv.Itoa(nav.prevPage) + "-1\n"
	}
	if nav.nextPage > 0 {
		np = "pn=n_" + strconv.Itoa(nav.nextPage) + "-1\n"
	}

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
	os.WriteFile(filepath.Join(dirStation, pageNr), output, 0644)
}

func getORFDate() string {
	now := time.Now()
	days := map[string]string{"Sun": "So", "Mon": "Mo", "Tue": "Di", "Wed": "Mi", "Thu": "Do", "Fri": "Fr", "Sat": "Sa"}
	yearStr := strconv.Itoa(now.Year())
	return fmt.Sprintf("\x07%s %02d.%02d.%s %s", days[now.Format("Mon")], now.Day(), now.Month(), yearStr[2:], now.Format("15:04:05"))
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
	pageBuffer := make([][]byte, 25)
	for i := range pageBuffer {
		line := make([]byte, 40)
		for j := range line {
			line[j] = 0x20
		}
		pageBuffer[i] = line
	}

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

// --- SVT Text ---

func svttextHandler(w http.ResponseWriter, r *http.Request) {
	pageName := getPageName(r, DirSVT)
	svttextGetTeletexPage(pageName)
	writeResponse(w, DirSVT, pageName)
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
	rows, nav, err := parseSVTRows(resp.Body, parts[1])
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

// --- CEEFAX ---

func ceefaxHandler(w http.ResponseWriter, r *http.Request) {
	pageName := getPageName(r, DirCEEFAX)
	ceefaxGetTeletexPage(pageName)
	writeResponse(w, DirCEEFAX, pageName)
}

// --- TEEFAX ---

func teefaxHandler(w http.ResponseWriter, r *http.Request) {
	pageName := getPageName(r, DirTEEFAX)
	teefaxGetTeletexPage(pageName)
	writeResponse(w, DirTEEFAX, pageName)
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

	rows, nav := parseTTIRows(resp.Body, parts[0], parts[1], true) // parts[1] = subpagenumber
	ps, ns, ct := getPrevNextSubpage(parts[0], nav)

	var output []byte
	output = append(output, []byte(fmt.Sprintf(
		"pn=p_\npn=n_\n%v%v%vftl=%v-0\nftl=%v-0\nftl=%v-0\nftl=%v-0\n<pre>",
		ps, ns, ct,
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
		fmt.Printf("Page %s: Error: %v", parts[0], err)
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

	rows, nav := parseTTIRows(resp.Body, parts[0], parts[1], false) // parts[1] = subpagenumber
	ps, ns, ct := getPrevNextSubpage(parts[0], nav)

	var output []byte
	output = append(output, []byte(fmt.Sprintf(
		"pn=p_\npn=n_\n%v%v%vftl=%v-0\nftl=%v-0\nftl=%v-0\nftl=%v-0\n<pre>",
		ps, ns, ct,
		string(ftl[0]), string(ftl[1]), string(ftl[2]), string(ftl[3])))...)

	for _, r := range rows {
		output = append(output, r...)
	}

	output = append(output, []byte("</pre>")...)
	os.WriteFile(filepath.Join(DirTEEFAX, pageNr), output, 0644)
}

// currently used by ceefax, teefax, zdf, svt
func getPrevNextSubpage(pageNr string, nav NavignationInfo) (string, string, string) {
	prev := ""
	next := ""
	cycletime := ""
	//	if prevSubpage > 0 && nextSubpage > 1 && prevSubpage < nextSubpage {
	if nav.prevSubpage > 0 {
		prev = "pn=ps" + pageNr + "-" + strconv.Itoa(nav.prevSubpage) + "\n"
	}
	if nav.nextSubpage > 0 {
		if nav.numberOfSubpages == 0 || nav.nextSubpage <= nav.numberOfSubpages {
			next = "pn=ns" + pageNr + "-" + strconv.Itoa(nav.nextSubpage) + "\n"
		}
	}
	if nav.cycleTime > 0 {
		// Force cycle time to be at least 5 seconds. Below seems not very useful to me.
		nav.cycleTime = max(5, nav.cycleTime)
		cycletime = "ct=" + strconv.Itoa(nav.cycleTime) + "\n"
	}
	return prev, next, cycletime
}

var subpage byte
var fullDoubleHeightRow bool

func parseTTIRows(r io.Reader, pageStr string, subpageStr string, isCEEFAX bool) ([][]byte, NavignationInfo) {
	subpageFound := false
	escFound := false
	//subpageCarouselFound := false

	var nav NavignationInfo
	nav.nextSubpage = 0
	nav.prevSubpage = 0
	nav.cycleTime = 0

	// Create an empty teletext page, fill it with spaces.
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
		//fmt.Println(string(line))
		// A TTI format teletext line looks something like this: OL,23, D ] CCATCH UP WITH REGIONAL NEWS       G160
		parts := bytes.SplitN(line, []byte(","), 3)

		/*
			Process page number and subpage number. Note: We get all the subpages at once in TTI format, so we
			have to detect which part of the data we need to process. In TTI format, the first row of a new
			teletextpage starts with a PN, e.g. PN,10203. Where 102 is the page number and 03 is the subpage
		*/
		if bytes.HasPrefix(parts[0], []byte("PN")) {
			// format XXXYY; subpage is last two YY digits
			subpageNumber := parts[1][3:]
			s := string(subpageNumber)
			val, _ := strconv.Atoi(s)
			if subpageFound {
				nav.nextSubpage = val
				break
			}
			if (subpage == 0 || subpage == 1) && val == 1 {
				subpageFound = true
			}
			if val == 0 || val == subpage {
				if val > 1 {
					nav.prevSubpage = val - 1
				}
				subpageFound = true
			}
		}

		// SC=Subpage Carousel indicator; if a page has subpages the subcode value is > 0
		if nav.cycleTime == 0 && bytes.HasPrefix(parts[0], []byte("SC")) {
			// parts[1] = subcode
			subcodeStr := string(parts[1])
			subcode, _ := strconv.Atoi(subcodeStr)
			if subcode > 0 {
				//subpageCarouselFound = true
				// set default cycle time to 5 seconds; this value may be adjusted if the page has a CT command (see below)
				// note: this value is deducted by looking at NMS Ceefax and time how long each subpage is shown
				nav.cycleTime = 5
				//fmt.Printf("SC/Subpage Carousel indicator encountered:%v cycleTime set to:%v\n", subcode, nav.cycleTime)
			}
		}

		if bytes.HasPrefix(parts[0], []byte("CT")) {
			// parts[1] = cycle time in seconds
			// parts[2] = T=text; C=Clear/Erase previous page from memory; S=Subtitle
			cycletimeStr := string(parts[1])
			cycletime, _ := strconv.Atoi(cycletimeStr)
			nav.cycleTime = cycletime
			//fmt.Printf("CT; cycleTime set to:%v\n", nav.cycleTime)
			// 199: CT,2,C
			// 528: CT,20,T
			// 100: No CT indicator -> cycle time is the default value of 5s
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
	return rows, nav
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
	// Fetch directory listing only at first use; after that we reuse directoryData
	if !fetchedDirectoryListing {
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
			if z.Err() == io.EOF {
				return "", fmt.Errorf("page %s not found in directory", pageID)
			}
			return "", z.Err()

		case html.StartTagToken:
			t := z.Token()
			if t.Data == "a" {
				for _, a := range t.Attr {
					if a.Key == "href" {
						// Check if filename starts with Pxxx
						// Matches "P171.tti", "P171-Index.tti", etc.
						if strings.HasPrefix(a.Val, searchPrefix) {
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
	pageName := getPageName(r, DirTEKSTI)
	tekstiGetTeletexPage(pageName)
	writeResponse(w, DirTEKSTI, pageName)
}

func tekstiGetTeletexPage(pageNr string) {
	parts := strings.Split(pageNr, "-")
	var rows [][]byte
	var nav NavignationInfo

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

		rows, nav, err = parseTEKSTIRows(resp.Body, parts[1]) // parts[1] = subpagenumber
		if err != nil {
			fmt.Println("xml.Unmarshal error")
			return
		}
	}
	ps := ""
	ns := ""
	if nav.numberOfSubpages > 1 {
		ps, ns, _ = getPrevNextSubpage(parts[0], nav)
	}

	var output []byte
	output = append(output, []byte(fmt.Sprintf(
		"%v%vftl=%v-0\nftl=%v-0\nftl=%v-0\nftl=%v-0\n<pre>", ps, ns,
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

func parseTEKSTIRows(body io.ReadCloser, subpageStr string) ([][]byte, NavignationInfo, error) {
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

	var pageSubpageCount int
	var nav NavignationInfo

	for {
		t, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nav, err
		}

		switch se := t.(type) {
		case xml.StartElement:
			if se.Name.Local == "page" {
				for _, attr := range se.Attr {
					switch attr.Name.Local {
					case "subpagecount":
						fmt.Sscanf(attr.Value, "%d", &pageSubpageCount)
						nav.numberOfSubpages = pageSubpageCount
						//fmt.Printf("Page has %d subpages\n", pageSubpageCount)
					case "number":
						//fmt.Printf("Page number: %s\n", attr.Value)
					}
				}
			}

			if se.Name.Local == "subpage" {
				// Check if this subpage matches the requested number
				for _, attr := range se.Attr {
					if attr.Name.Local == "number" && attr.Value == subpageStr {
						inTargetSubpage = true
						subpageInt, err := strconv.Atoi(subpageStr)
						if err == nil {
							if subpageInt > 1 {
								nav.prevSubpage = subpageInt - 1
							}
							if subpageInt < nav.numberOfSubpages {
								nav.nextSubpage = subpageInt + 1
							}
						}
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
						return nil, nav, err
					}
					return pageBuffer, nav, nil // Found and processed the target
				}
			}

		case xml.EndElement:
			if se.Name.Local == "subpage" {
				inTargetSubpage = false
			}
		}
	}
	return pageBuffer, nav, nil
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
	case 'é':
		return 0xE9
	case '€':
		return 0x80
	default:
		if r < 128 {
			return byte(r)
		}
		return 0x20
	}
}

// --- DR TEKST-TV ---

func drteksttvHandler(w http.ResponseWriter, r *http.Request) {
	pageName := getPageName(r, DirDR)
	drteksttvGetTeletexPage(pageName)
	writeResponse(w, DirDR, pageName)
}

func drteksttvGetTeletexPage(pageNr string) {
	parts := strings.Split(pageNr, "-")
	url := fmt.Sprintf("https://www.dr.dk/cgi-bin/fttx1.exe/%s/%s", parts[0], parts[1])
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

	row0 := make([]byte, 40)
	for i := range row0 {
		row0[i] = 0x20
	}
	var nav NavignationInfo
	rows, nav, err := parseDRRows(resp.Body, parts[0], parts[1])
	if err != nil {
		fmt.Println(err.Error())
		return
	}

	pp := ""
	np := ""
	ps := ""
	ns := ""
	if nav.numberOfSubpages > 1 {
		ps, ns, _ = getPrevNextSubpage(parts[0], nav)
	}
	if nav.prevPage > 0 {
		pp = "pn=p_" + strconv.Itoa(nav.prevPage) + "-1\n"
	}
	if nav.nextPage > 0 {
		np = "pn=n_" + strconv.Itoa(nav.nextPage) + "-1\n"
	}

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
	os.WriteFile(filepath.Join(DirDR, pageNr), output, 0644)
}

func parseDRRows(body io.ReadCloser, pageNr string, subPageNr string) ([][]byte, NavignationInfo, error) {
	defer body.Close()

	var nav NavignationInfo

	// Initialize 25x40 grid with spaces
	pageBuffer := make([][]byte, 25)
	for i := range pageBuffer {
		pageBuffer[i] = bytes.Repeat([]byte{0x20}, 40)
	}

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

// Helper to ensure we don't write out of bounds
func writeToBuffer(buffer [][]byte, row *int, col *int, b byte) {
	if *row >= 0 && *row < 25 && *col >= 0 && *col < 40 {
		buffer[*row][*col] = b
		*col++
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

// --- User Config (UD)
//
// Stores per-user config as a 7-byte binary file in the UD/ folder, keyed by MAC address.
//
// Endpoints (firmware sends uppercase; server accepts both):
//   GET /ud/{mac}/r           → check: \x00 if config exists, \x01 if not
//   GET /ud/{mac}/l           → load:  \x00\x07<7 bytes> on success, \x01 if not found
//   GET /ud/{mac}/s/{hexdata} → save:  \x00 on success, \x01 on error
//
// Config layout (7 bytes, little-endian):
//   [0-1] refreshtime  [2-3] cycletime  [4] station  [5-6] startpage

func udHandler(w http.ResponseWriter, r *http.Request) {
	// Normalise to lowercase (firmware uppercases the entire path)
	parts := strings.Split(strings.ToLower(strings.Trim(r.URL.Path, "/")), "/")
	// Expected: ud / mac / cmd  (and optionally ud / mac / s / hexdata)
	if len(parts) < 3 {
		http.Error(w, "Bad request", 400)
		return
	}

	mac := parts[1]
	cmd := parts[2]

	// Validate MAC: exactly 12 lowercase hex characters
	if len(mac) != 12 {
		http.Error(w, "Invalid MAC", 400)
		return
	}
	for _, c := range mac {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			http.Error(w, "Invalid MAC", 400)
			return
		}
	}

	cfgPath := filepath.Join(DirUD, mac+".cfg")

	writeBinaryResponse := func(data []byte) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(200)
		w.Write(data)
	}

	switch cmd {

	case "r": // Check — does a config exist for this MAC?
		fmt.Printf("Request: %-12s %s/r | ", DirUD, mac)
		if _, err := os.Stat(cfgPath); err == nil {
			fmt.Println("found")
			writeBinaryResponse([]byte{0x00})
		} else {
			fmt.Println("not found")
			writeBinaryResponse([]byte{0x01})
		}

	case "l": // Load — return the 7 config bytes
		fmt.Printf("Request: %-12s %s/l | ", DirUD, mac)
		data, err := os.ReadFile(cfgPath)
		if err != nil || len(data) != 7 {
			fmt.Println("not found")
			writeBinaryResponse([]byte{0x01})
			return
		}
		fmt.Println("loaded")
		resp := make([]byte, 9) // status(1) + length(1) + data(7)
		resp[0] = 0x00
		resp[1] = 7
		copy(resp[2:], data)
		writeBinaryResponse(resp)

	case "s": // Save — hex data is the next path segment
		fmt.Printf("Request: %-12s %s/s | ", DirUD, mac)
		if len(parts) < 4 {
			fmt.Println("missing hex data")
			writeBinaryResponse([]byte{0x01})
			return
		}
		hexdata := parts[3]
		if len(hexdata) != 14 { // 7 bytes × 2 hex digits
			fmt.Printf("invalid hex length: %d\n", len(hexdata))
			writeBinaryResponse([]byte{0x01})
			return
		}
		data := make([]byte, 7)
		for i := 0; i < 7; i++ {
			v, err := strconv.ParseUint(hexdata[i*2:i*2+2], 16, 8)
			if err != nil {
				fmt.Println("hex decode error:", err)
				writeBinaryResponse([]byte{0x01})
				return
			}
			data[i] = byte(v)
		}
		if err := os.WriteFile(cfgPath, data, 0644); err != nil {
			fmt.Println("write error:", err)
			writeBinaryResponse([]byte{0x01})
			return
		}
		fmt.Println("saved")
		writeBinaryResponse([]byte{0x00})

	default:
		http.Error(w, "Unknown command", 400)
	}
}

func sendErrorMsg(w http.ResponseWriter, code int, message string) {
	w.Header().Set("Content-Type", "text/plain")
	w.WriteHeader(code)
	w.Write([]byte(message))
}

func logPageRequest(station string, page string) {
	//	now := time.Now()
	//	fmt.Printf("%v [%v:%v] - ", now.Format("2006-01-02 15:04:05"), station, page)
	fmt.Printf("Request: %-12s %s | ", station, page)
}

func logFetchingPage(url string) {
	fmt.Println(url)
}
