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
- Chunkytext (UK)
- Webfax 1 & Webfax 1 (UK)
- SPARK (UK)
- WDR text (German)
- hr-text (German)
- SWR BW (German, Baden-Württemberg)
- SWR RP (German, Rheinland-Pfalz)
- SRF 1, zwei, SRF Info, RTS Un, RTS Deux, RSI LA 1, RSI LA 2 (Switzerland, German/French/Italian)

Next up candidates:
- RTP teletexto (Portugal) - https://www.rtp.pt/wportal/fab-txt/texto/100/100_0001.htm
- ...?

The NOS-TT file format is being used for the other teletext services:
Is set up fairly efficient: mostly around 1073 bytes; a little bit bigger if a page has subpages.
The file format is a text block with (sub)page and fastext links followed by a <pre>..</pre> block
which contains 1000 bytes of raw teletext data (control codes, text and mosiac/graphic characters).

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
	lnk=128,07,37
	lnk=110,09,37
	lnk=109,11,37
	<pre>
    ...40 columns x 25 rows = 1000 bytes of raw teletext data
    </pre>

Note: I added new parameters which are not NOS-TT native!
ct=n; cycle time is used for rotating between subpages. n is the delay in seconds.
lnk=nnn,rr,cc; these represent pagenumber references from the current page with their screen coordinate.
E.g. lnk=110,09,37 refers to page 110 on row 9 column 37 (counting from 0,0).

Why transform to the NOS-TT format? Basically to keep things simple for the Teletext64U viewer program on the C64.
- It only has to support one uniform way of communicating with this proxy program.
- It only needs to have one routine to decode teletext data.
- Adding a new teletext service within Teletext64U is just adding an item to a table.
*/

package main

import (
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Version
const pp_version = "2.5.0"

// Supported teletext services
const (
	DirNOS        = "NOS-TT"
	DirARD        = "ARD-TEXT"
	DirZDF        = "ZDF-TEXT"
	DirZDFinfo    = "ZDFINFO"
	DirZDFneo     = "ZDFNEO"
	Dir3sat       = "3SAT"
	DirWDR        = "WDR-TEXT"
	DirORF1       = "ORF1"
	DirORF2       = "ORF2"
	DirORF3       = "ORF3"
	DirORFSport   = "ORFSPORT"
	DirHR         = "HR-TEXT"
	DirSWRBW      = "SWR-BW"
	DirSWRRP      = "SWR-RP"
	DirCEEFAX     = "CEEFAX"
	DirTEEFAX     = "TEEFAX"
	DirTEKSTI     = "TEKSTI-TV"
	DirSVT        = "SVT-TEXT"
	DirDR         = "DR-TEKST-TV"
	DirCHUNKYTEXT = "CHUNKYTEXT"
	DirWEBFAX1    = "WEBFAX1"
	DirWEBFAX2    = "WEBFAX2"
	DirSPARK      = "SPARK"
	DirSRF1       = "SRF1"
	DirSRF2       = "SRF2"
	DirSRFInfo    = "SRFINFO"
	DirRTS1       = "RTS1"
	DirRTS2       = "RTS2"
	DirRSILA1     = "RSILA1"
	DirRSILA2     = "RSILA2"
	DirNOSNEWS    = "NOSNEWS" // RSS-generated NOS Nieuws pages (Dutch), see nosnews.go
	DirFORUM64    = "FORUM64" // RSS-generated forum64.de thread activity (German), see forum64.go
	DirUD         = "UD"      // User Directory where user preferences are stored for the stand alone WiC64 edition
)

// Each service has its own handler
var handlers = map[string]http.HandlerFunc{
	DirNOS:        makeHandler(DirNOS, nosttGetTeletexPage),
	DirARD:        makeHandler(DirARD, ardtextGetTeletexPage),
	DirZDF:        makeHandler(DirZDF, func(p string) bool { return zdftextGetTeletexPage(p, "zdf", DirZDF) }),
	DirZDFinfo:    makeHandler(DirZDFinfo, func(p string) bool { return zdftextGetTeletexPage(p, "zdfinfo", DirZDFinfo) }),
	DirZDFneo:     makeHandler(DirZDFneo, func(p string) bool { return zdftextGetTeletexPage(p, "zdfneo", DirZDFneo) }),
	Dir3sat:       makeHandler(Dir3sat, func(p string) bool { return zdftextGetTeletexPage(p, "3sat", Dir3sat) }),
	DirWDR:        makeHandler(DirWDR, wdrtextGetTeletexPage),
	DirHR:         makeHandler(DirHR, hrtextGetTeletexPage),
	DirSWRBW:      makeHandler(DirSWRBW, func(p string) bool { return swrGetTeletexPage(p, "bw", DirSWRBW) }),
	DirSWRRP:      makeHandler(DirSWRRP, func(p string) bool { return swrGetTeletexPage(p, "rp", DirSWRRP) }),
	DirORF1:       makeHandler(DirORF1, func(p string) bool { return orfGetTeletexPage(p, "orf1", DirORF1) }),
	DirORF2:       makeHandler(DirORF2, func(p string) bool { return orfGetTeletexPage(p, "orf2", DirORF2) }),
	DirORF3:       makeHandler(DirORF3, func(p string) bool { return orfGetTeletexPage(p, "orfiii", DirORF3) }),
	DirORFSport:   makeHandler(DirORFSport, func(p string) bool { return orfGetTeletexPage(p, "sportplus", DirORFSport) }),
	DirCEEFAX:     makeHandler(DirCEEFAX, ceefaxGetTeletexPage),
	DirTEEFAX:     makeHandler(DirTEEFAX, teefaxGetTeletexPage),
	DirCHUNKYTEXT: makeHandler(DirCHUNKYTEXT, chunkytextGetTeletexPage),
	DirWEBFAX1:    makeHandler(DirWEBFAX1, func(p string) bool { return webfaxGetTeletexPage(p, "Webfax", DirWEBFAX1) }),
	DirWEBFAX2:    makeHandler(DirWEBFAX2, func(p string) bool { return webfaxGetTeletexPage(p, "Webfax2", DirWEBFAX2) }),
	DirSPARK:      makeHandler(DirSPARK, sparkGetTeletexPage),
	DirTEKSTI:     makeHandler(DirTEKSTI, tekstiGetTeletexPage),
	DirSVT:        makeHandler(DirSVT, svttextGetTeletexPage),
	DirDR:         makeHandler(DirDR, drteksttvGetTeletexPage),
	DirSRF1:       makeHandler(DirSRF1, func(p string) bool { return srgGetTeletexPage(p, "SRF1", DirSRF1) }),
	DirSRF2:       makeHandler(DirSRF2, func(p string) bool { return srgGetTeletexPage(p, "SRFzwei", DirSRF2) }),
	DirSRFInfo:    makeHandler(DirSRFInfo, func(p string) bool { return srgGetTeletexPage(p, "SRFInfo", DirSRFInfo) }),
	DirRTS1:       makeHandler(DirRTS1, func(p string) bool { return srgGetTeletexPage(p, "RTSUn", DirRTS1) }),
	DirRTS2:       makeHandler(DirRTS2, func(p string) bool { return srgGetTeletexPage(p, "RTSDeux", DirRTS2) }),
	DirRSILA1:     makeHandler(DirRSILA1, func(p string) bool { return srgGetTeletexPage(p, "RSILA1", DirRSILA1) }),
	DirRSILA2:     makeHandler(DirRSILA2, func(p string) bool { return srgGetTeletexPage(p, "RSILA2", DirRSILA2) }),
	DirNOSNEWS:    makeHandler(DirNOSNEWS, nosnewsGetTeletexPage),
	DirFORUM64:    makeHandler(DirFORUM64, forum64GetTeletexPage),
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

type FastextLinks struct {
	ftl1 string
	ftl2 string
	ftl3 string
	ftl4 string
}

// Data to store in the log file (CSV format)
type LogEntry struct {
	Date      string
	Time      string
	IPAddress string
	Station   string
	Page      string
}

// Global channel for background logger
var logChan = make(chan LogEntry, 100) // Buffer 100 entries to handle peaks

// startCSVLogger runs in background and processes the log queue
func startCSVLogger() {
	dataDir := "./data"
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		fmt.Printf("CSV Logger Error: map %s could not be created: %v\n", dataDir, err)
		return
	}

	csvPath := filepath.Join(dataDir, "pp-history.csv")

	file, err := os.OpenFile(csvPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Printf("CSV Logger Error: could not open %s: %v\n", csvPath, err)
		return
	}
	defer file.Close()

	// Listen to incoming log data from the channel
	for entry := range logChan {
		logLine := fmt.Sprintf("%s,%s,%s,%s,%s\n",
			entry.Date,
			entry.Time,
			entry.IPAddress,
			entry.Station,
			entry.Page,
		)
		if _, err := file.WriteString(logLine); err != nil {
			fmt.Printf("CSV Logger write error: %v\n", err)
		}
	}
}

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

// logs the client IP for every incoming request
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

	// Create UD folder (=User Data) for user config storage and register handler
	// WiC64 firmware v2.1.0 uppercases the URL path, so we register both cases
	err = os.MkdirAll(DirUD, 0755)
	if err != nil {
		fmt.Printf("Could not create folder %s: %v\n", DirUD, err)
	}
	mux.HandleFunc("/UD/", udHandler)
	mux.HandleFunc("/ud/", udHandler)

	go startCSVLogger()

	syncChunkytextRepo()
	go func() {
		ticker := time.NewTicker(chunkytextSyncInterval)
		defer ticker.Stop()
		for range ticker.C {
			syncChunkytextRepo()
		}
	}()

	// NOSNEWS has no background ticker - it polls its feed lazily, only when a page is actually
	// requested and the data has gone stale (see nosNewsCategory.pollIfStale in nosnews.go).

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

	now := time.Now()
	dateStr := now.Format("2006-01-02")
	timeStr := now.Format("15:04:05")
	clientIP := getClientIP(r)

	logPageRequest(dirStation, pageName)

	// fire log data in the channel asynchronously
	select {
	case logChan <- LogEntry{
		Date:      dateStr,
		Time:      timeStr,
		IPAddress: clientIP,
		Station:   dirStation,
		Page:      pageName,
	}:
	default:
		// If buffer 100 overflow (DDOS?), prevent server from blocking
		fmt.Println("CSV Logger warning: log queue is full, entry skipped.")
	}
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

// Fetches (and caches to disk) one teletext page for a station, and reports whether the station's
// backend was reachable.
type fetchFunc func(pageNr string) bool

// If a page is older than 1 minute it will be fetched from internet, otherwise from disk cache
const freshTTL = 60 * time.Second

// Stations reported offline will be checked every 5 minutes to see if they are online again
const stationRecheckInterval = 5 * time.Minute

// This lists stations whose fetch function reads from local storage (a git mirror, a saved
// archive, ...) instead of making a live network call. Currently only Chunkeytext.
// note: syncChunkytextRepo checks if there is a newer repo.
var localOnlyStations = map[string]bool{
	DirCHUNKYTEXT: true,
	DirNOSNEWS:    true,
	DirFORUM64:    true,
}

type stationHealth struct {
	offline   bool
	lastCheck time.Time
}

var (
	healthMu sync.Mutex
	health   = map[string]*stationHealth{}
)

// returns file age
func modTime(path string) time.Time {
	info, err := os.Stat(path)
	if err != nil {
		return time.Time{}
	}
	return info.ModTime()
}

// reports whether the cached file was written within the last freshTTL
func isFresh(path string) bool {
	t := modTime(path)
	return !t.IsZero() && time.Since(t) < freshTTL
}

// reports whether it's OK to try a live fetch for this station right now. A station that has never
// failed (or isn't currently marked offline) always returns true. Once marked offline, it's only
// retried after stationRecheckInterval has passed since the last attempt.
func shouldAttemptFetch(dirStation string) bool {
	healthMu.Lock()
	defer healthMu.Unlock()
	h, ok := health[dirStation]
	if !ok || !h.offline {
		return true
	}
	return time.Since(h.lastCheck) >= stationRecheckInterval
}

// updates a station's health state after a fetch attempt
func recordFetchResult(dirStation string, reachable bool) {
	healthMu.Lock()
	defer healthMu.Unlock()
	h, ok := health[dirStation]
	if !ok {
		h = &stationHealth{}
		health[dirStation] = h
	}
	h.offline = !reachable
	h.lastCheck = time.Now()
}

// builds the HTTP handler for one station: parse the requested page name, then either serve a
// still-fresh cached copy, skip the fetch because the station is in its offline cooldown, or run
// the station's fetch function and record whether it actually reached the backend.
func makeHandler(dirStation string, fetch fetchFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		pageName := getPageName(r, dirStation)
		path := filepath.Join(dirStation, pageName)
		if localOnlyStations[dirStation] {
			fetch(pageName)
			// Always complete this request's log line here, unconditionally - fetch() may or may
			// not have triggered a lazy poll internally (see pollIfStale in nosnews.go/forum64.go),
			// and that poll logs itself separately via logBackgroundPoll when it happens. Waiting on
			// it to complete this line left it dangling on every request that didn't happen to
			// trigger one - see logFetchingPage/logBackgroundPoll in common.go for the full story.
			logFetchingPage(path)
			writeResponse(w, dirStation, pageName)
			return
		}
		if !isFresh(path) && shouldAttemptFetch(dirStation) {
			reachable := fetch(pageName)
			recordFetchResult(dirStation, reachable)
		} else {
			logFetchingPage(path)
		}
		writeResponse(w, dirStation, pageName)
	}
}
