package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// --- SRG (SRF 1, SRF 2, SRF Info / RTS 1, RTS 2 / RSI LA 1, RSI LA 2) ---
// SRG because these stations are all part of the Swiss Broadcasting Corporation (SRG SSR)
//
// Like NOS-TT it doesn't need a parser function because we get the data in raw teletext binary
//
// A small SPA that fetches page data as JSON from:
//   GET https://api.teletext.ch/channels/{channel}/pages/{pageNumber}
// Every subpage entry contains "ep1Info.data.ep1Format.content": the raw teletext row data, base64 encoded.

type ttchEp1Format struct {
	Content string `json:"content"`
}

type ttchData struct {
	Ep1Format ttchEp1Format `json:"ep1Format"`
}

type ttchEp1Info struct {
	Data ttchData `json:"data"`
}

type ttchSubpage struct {
	SubpageNumber int         `json:"subpageNumber"`
	Ep1Info       ttchEp1Info `json:"ep1Info"`
}

type ttchPage struct {
	PageNumber   int           `json:"pageNumber"`
	PreviousPage int           `json:"previousPage"`
	NextPage     int           `json:"nextPage"`
	Subpages     []ttchSubpage `json:"subpages"`
}

func srgGetTeletexPage(pageNr string, channel string, dirStation string) bool {
	parts := strings.Split(pageNr, "-")
	page := parts[0]

	uiSub := 1
	if len(parts) > 1 {
		if v, err := strconv.Atoi(parts[1]); err == nil && v > 0 {
			uiSub = v
		}
	}

	url := fmt.Sprintf("https://api.teletext.ch/channels/%s/pages/%s", channel, page)
	logFetchingPage(url)

	client := http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		fmt.Println("HTTP Error: Could not retrieve page", pageNr, "Status:", resp.StatusCode)
		return true
	}

	var ttPage ttchPage
	if err := json.NewDecoder(resp.Body).Decode(&ttPage); err != nil {
		fmt.Println("JSON decode error:", err)
		return true
	}

	if len(ttPage.Subpages) == 0 {
		fmt.Println("No subpages returned for", pageNr)
		return true
	}

	if uiSub > len(ttPage.Subpages) {
		uiSub = 1
	}
	subIdx := uiSub - 1

	rawContent := ttPage.Subpages[subIdx].Ep1Info.Data.Ep1Format.Content
	body, err := base64.StdEncoding.DecodeString(rawContent)
	if err != nil {
		fmt.Println("Base64 decode error:", err)
		return true
	}

	row0 := make([]byte, 40)
	for i := range row0 {
		row0[i] = 0x20
	}
	row24 := make([]byte, 40)
	for i := range row24 {
		row0[i] = 0x20
	}
	dt := getORFDate() // yes reuse ORF date here, because why not?
	var stationPage string

	// trailing spaces are needed to overwrite the 2-letter Weekday abbreviation
	switch channel {
	case "SRF1":
		stationPage = "\x07" + parts[0] + "  SRF 1    "
	case "SRFzwei":
		stationPage = "\x07" + parts[0] + "  SRF zwei "
	case "SRFInfo":
		stationPage = "\x07" + parts[0] + "  SRF info "
	case "RTSUn":
		stationPage = "\x07" + parts[0] + "  RTS 1    "
	case "RTSDeux":
		stationPage = "\x07" + parts[0] + "  RTS 2    "
	case "RSILA1":
		stationPage = "\x07" + parts[0] + "  RSI LA 1 "
	case "RSILA2":
		stationPage = "\x07" + parts[0] + "  RSI LA 2 "
	}

	if strings.Contains(channel, "SRF") {
		copy(row24[0:], "\x01Kurz\xDCbersicht  \x02Sport  \x03Meteo \x06TV&Radio")
	}
	if strings.Contains(channel, "RTS") {
		copy(row24[0:], "\x01Index    \x02Sport     \x03Meteo    \x06TV&Radio")
	}
	if strings.Contains(channel, "RSI") {
		copy(row24[0:], "\x01Indice    \x02Sport    \x03Meteo    \x06TV&Radio")
	}

	copy(row0[19:], stringToLatin1Bytes(dt))
	copy(row0[7:], []byte(stationPage))

	// Pages don't always fill all 24 rows; pad out to the full 920-byte grid with spaces.
	// This excludes to header and fastext rows
	fullPage := bytes.Repeat([]byte{0x20}, 920)
	copy(fullPage, body)

	var nav NavignationInfo
	nav.numberOfSubpages = len(ttPage.Subpages)
	if uiSub > 1 {
		nav.prevSubpage = uiSub - 1
	}
	if uiSub < nav.numberOfSubpages {
		nav.nextSubpage = uiSub + 1
	}
	if nav.numberOfSubpages > 1 {
		nav.cycleTime = 8
	}

	ps, ns, ct := "", "", ""
	pp, np := buildPageNavDirectives(ttPage.PreviousPage, ttPage.NextPage)
	if nav.numberOfSubpages > 1 {
		ps, ns, ct = getPrevNextSubpage(page, nav)
	}

	var ftl FastextLinks
	ftl.ftl1 = "101-0"
	ftl.ftl2 = "180-0"
	ftl.ftl3 = "500-0"
	ftl.ftl4 = "700-0"

	var output []byte
	output = append(output, []byte(fmt.Sprintf(
		"%s%s%s%s%sftl=%s\nftl=%s\nftl=%s\nftl=%s\n<pre>",
		pp, np, ps, ns, ct, ftl.ftl1, ftl.ftl2, ftl.ftl3, ftl.ftl4))...)
	output = append(output, row0...)
	output = append(output, fullPage...)
	output = append(output, row24...)
	output = append(output, []byte("</pre>")...)

	savePage(dirStation, pageNr, output)
	return true
}
