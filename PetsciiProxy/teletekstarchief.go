package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// --- Teletekst Archief (teletekstarchief.nl) ---
//
// An archive of reconstructed NOS Teletekst pages.
//
//   GET https://teletekstarchief.nl/archive-json/{page}[-{subpage}]
//
// returns JSON with prev/next page and subpage IDs (as strings, e.g. "114", "111-2") and
// "binaryDisplay": 960 bytes (40x24, rows 0..23) of Level-1 teletext, base64 encoded.
// Row 0 is the original header as it was captured (e.g. "P111  NOS-TT  111 vr 12 mei 17:32:03"),
// so it is kept as-is instead of being replaced by a generated header.
// Note: /json/{id} returns the same data but with a blank header row, hence /archive-json.
//
//   GET https://teletekstarchief.nl/meta-json/{page}[-{subpage}]
//
// returns optional metadata that the archive's web viewer shows below the page: snapshot date,
// source, notes and rows lost in the original capture. It is put on row 24 (normally fastext,
// which this archive doesn't have). The subpage interval is used as cycle time.
//
// Subpage numbers can have gaps (e.g. 101-4 is followed by 101-10), so navigation is taken
// verbatim from the JSON rather than computed.

type teletekstarchiefPage struct {
	PrevPage      string `json:"prevPage"`
	NextPage      string `json:"nextPage"`
	PrevSubPage   string `json:"prevSubPage"`
	NextSubPage   string `json:"nextSubPage"`
	BinaryDisplay string `json:"binaryDisplay"`
}

type teletekstarchiefMeta struct {
	Date            string  `json:"date"`
	Source          string  `json:"source"`
	Notes           string  `json:"notes"`
	SubPageInterval float64 `json:"subPageInterval"`
	CaptureDefects  struct {
		MissingRows []int `json:"missingRows"`
	} `json:"captureDefects"`
}

const teletekstarchiefBaseURL = "https://teletekstarchief.nl"

// The archive is static, so a cached page is kept much longer than the default freshTTL
const teletekstarchiefTTL = 24 * time.Hour

// Default subpage rotation time of the archive's own viewer (seconds)
const teletekstarchiefCycleTime = 10

var reTeletekstarchiefPage = regexp.MustCompile(`^[1-8][0-9]{2}(-(0|[1-9][0-9]{0,2}))?$`)

func teletekstarchiefGetTeletexPage(pageNr string) bool {
	if !reTeletekstarchiefPage.MatchString(pageNr) {
		fmt.Println("Invalid page number:", pageNr)
		return true
	}

	// Serve from disk cache as long as it's younger than the archive TTL
	if t := modTime(filepath.Join(DirTELETEKSTARCHIEF, pageNr)); !t.IsZero() && time.Since(t) < teletekstarchiefTTL {
		return true
	}

	// The client asks for "-0" when no subpage was entered, and page navigation directives always
	// point to subpage "-1" (see buildPageNavDirectives). Pages without subpages only exist as a
	// bare number in the archive, while a bare number returns the first subpage for pages that do
	// have them, so both "-0" and "-1" can safely be dropped.
	archiveID := strings.TrimSuffix(strings.TrimSuffix(pageNr, "-0"), "-1")

	url := fmt.Sprintf("%s/archive-json/%s", teletekstarchiefBaseURL, archiveID)
	logFetchingPage(url)

	client := http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		fmt.Println("Connection Error:", err)
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		fmt.Println("HTTP Error: Could not retrieve page", pageNr, "Status:", resp.StatusCode)
		return true
	}

	var ttPage teletekstarchiefPage
	if err := json.NewDecoder(resp.Body).Decode(&ttPage); err != nil {
		fmt.Println("JSON decode error:", err)
		return true
	}

	body, err := base64.StdEncoding.DecodeString(ttPage.BinaryDisplay)
	if err != nil {
		fmt.Println("Base64 decode error:", err)
		return true
	}

	// Rows 0..23 (960 bytes), padded with spaces just in case
	fullPage := bytes.Repeat([]byte{0x20}, 24*40)
	copy(fullPage, body[:min(len(body), 24*40)])
	for i := range fullPage {
		fullPage[i] &= 0x7F // strip parity bit if present
	}

	// Metadata is optional: a failure here must never prevent the page itself from being shown
	meta := teletekstarchiefGetMeta(&client, archiveID)
	row24 := teletekstarchiefInfoRow(meta)

	// Only the page number part is used, in case the archive ever returns a subpage ID here
	prevPage, _ := strconv.Atoi(strings.SplitN(ttPage.PrevPage, "-", 2)[0])
	nextPage, _ := strconv.Atoi(strings.SplitN(ttPage.NextPage, "-", 2)[0])
	pp, np := buildPageNavDirectives(prevPage, nextPage)

	ps, ns, ct := "", "", ""
	if ttPage.PrevSubPage != "" {
		ps = "pn=ps" + ttPage.PrevSubPage + "\n"
	}
	if ttPage.NextSubPage != "" {
		ns = "pn=ns" + ttPage.NextSubPage + "\n"
	}
	if ps != "" || ns != "" {
		cycleTime := teletekstarchiefCycleTime
		if meta.SubPageInterval > 0 {
			cycleTime = max(5, int(meta.SubPageInterval+0.5))
		}
		ct = "ct=" + strconv.Itoa(cycleTime) + "\n"
	}

	var output []byte
	output = append(output, []byte(pp+np+ps+ns+ct+"<pre>")...)
	output = append(output, fullPage...)
	output = append(output, row24...)
	output = append(output, []byte("</pre>")...)

	savePage(DirTELETEKSTARCHIEF, pageNr, output)
	return true
}

// Fetches the page's metadata; returns an empty struct if there is none or on any error
func teletekstarchiefGetMeta(client *http.Client, archiveID string) teletekstarchiefMeta {
	var meta teletekstarchiefMeta
	url := fmt.Sprintf("%s/meta-json/%s", teletekstarchiefBaseURL, archiveID)
	resp, err := client.Get(url)
	if err != nil {
		fmt.Println("Metadata connection error:", err)
		return meta
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return meta
	}
	if err := json.NewDecoder(resp.Body).Decode(&meta); err != nil {
		fmt.Println("Metadata JSON decode error:", err)
		return teletekstarchiefMeta{}
	}
	return meta
}

// Builds row 24 from the metadata the archive's web viewer shows below the page, e.g.
// "Snapshot do 25-12-2014 Bron nos.nl". Labels are white, values cyan (missing rows red).
// Order of importance: snapshot date, missing rows, source, notes; cut off at 40 characters.
// Each colour code also acts as the separating space.
func teletekstarchiefInfoRow(meta teletekstarchiefMeta) []byte {
	row := bytes.Repeat([]byte{0x20}, 40)

	var line []byte
	add := func(colour byte, value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		line = append(line, colour)
		line = append(line, stringToLatin1Bytes(value)...)
	}

	shortDate := getYear(meta.Date)
	if shortDate != "" {
		//add(TCC_ALPHA_WHITE, "Jaar:")
		add(TCC_ALPHA_MAGENTA, shortDate)
		if meta.Source != "" {
			add(TCC_ALPHA_WHITE, "/")
			add(TCC_ALPHA_MAGENTA, meta.Source)
		}
		if meta.Notes != "" {
			add(TCC_ALPHA_WHITE, "-")
			add(TCC_ALPHA_MAGENTA, meta.Notes)
		}
		copy(row, line)
	}
	//add("Notities", TCC_ALPHA_CYAN, meta.Notes)
	/*	fmt.Printf("Snapshot %v\n", makeShortDate(meta.Date))
		fmt.Printf("Opname %v\n", teletekstarchiefMissingRows(meta.CaptureDefects.MissingRows))
		fmt.Printf("Bron %v\n", meta.Source)
		fmt.Printf("Notities %v\n", meta.Notes)
	*/
	return row
}

// Formats the valid missing row numbers (1..24) as compact ranges, e.g. "r.3-5,9 ontbreekt"
func teletekstarchiefMissingRows(rows []int) string {
	seen := map[int]bool{}
	var valid []int
	for _, r := range rows {
		if r >= 1 && r <= 24 && !seen[r] {
			seen[r] = true
			valid = append(valid, r)
		}
	}
	if len(valid) == 0 {
		return ""
	}
	for i := 1; i < len(valid); i++ { // tiny list, insertion sort is fine
		for j := i; j > 0 && valid[j] < valid[j-1]; j-- {
			valid[j], valid[j-1] = valid[j-1], valid[j]
		}
	}

	var parts []string
	start, end := valid[0], valid[0]
	flush := func() {
		if start == end {
			parts = append(parts, strconv.Itoa(start))
		} else {
			parts = append(parts, strconv.Itoa(start)+"-"+strconv.Itoa(end))
		}
	}
	for _, r := range valid[1:] {
		if r == end+1 {
			end = r
			continue
		}
		flush()
		start, end = r, r
	}
	flush()
	return "r." + strings.Join(parts, ",") + " ontbreekt"
}

func makeShortDate(longDate string) string {
	var dayStr, monthStr, yearStr string
	monthNum := map[string]string{"jan": "1", "feb": "2", "maa": "3", "apr": "4", "mei": "5", "jun": "6",
		"jul": "7", "aug": "8", "sep": "9", "okt": "10", "nov": "11", "dec": "12"}

	parts := strings.Fields(longDate)
	switch len(parts) {
	case 3:
		dayStr = parts[0]
		monthStr = monthNum[parts[1]]
		yearStr = parts[2]
	case 4:
		dayStr = parts[1]
		monthStr = monthNum[parts[2]]
		yearStr = parts[3]
	default:
		return ""
	}

	return fmt.Sprintf("%s-%s-%s", dayStr, monthStr, yearStr)
}

func getYear(longDate string) string {
	var yearStr string
	parts := strings.Fields(longDate)
	switch len(parts) {
	case 3:
		yearStr = parts[2]
	case 4:
		yearStr = parts[3]
	default:
		return ""
	}
	return yearStr
}
