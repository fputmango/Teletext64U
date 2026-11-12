// --- Telegazeta TVP ---
//
// Polish Teletext services
//
// Telegazeta serves its pages as PNG images only
//
// The PNG is 480x336 pixels and holds teletext rows 1-24 (no header row!), so each cell is 480/40=12 by 336/24=14 pixels.
// It is converted into teletext data by imagetxt.go; the character pictures are in telegazeta_glyphs.go.
//
// Copyright (C) 2026 Frank Putman
// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image/png"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	tlgzBaseURL       = "https://www.telegazeta.pl/sync/ncexp/"
	tlgzCellW         = 12
	tlgzCellH         = 14
	tlgzStatusMaxAge  = time.Minute
	tlgzFolderPrefix  = "TLGZ-"
	tlgzMaxSubpageNum = 9999
)

// How Telegazeta draws its pages (see imagetxt.go)
var tlgzProfile = &imgTxtProfile{
	name:            "TLGZ",
	cellW:           tlgzCellW,
	cellH:           tlgzCellH,
	firstRow:        1, // the image has no header row!
	rows:            24,
	mosaicX:         [2][2]int{{0, 6}, {6, 12}},
	mosaicY:         [3][2]int{{0, 4}, {4, 10}, {10, 14}},
	separatedX:      [2][2]int{{1, 5}, {7, 11}},
	separatedY:      [3][2]int{{1, 4}, {5, 9}, {10, 13}},
	glyphs:          imgTxtBuildGlyphTable("tlgzGlyphArt", tlgzGlyphArt, tlgzCellW, tlgzCellH, tlgzRuneToByte),
	glyphTableName:  "tlgzGlyphArt",
	doubleWidthUsed: true,
}

// Short name for the header row
var tlgzServiceNames = map[string]string{
	"TG1": "TVP1",
	"TG2": "TVP2",
	"OTV": "TVP3",
	"KUL": "KULTURA",
	"HIS": "HISTORIA",
	"SPO": "SPORT",
	"SAT": "POLONIA",
}

func tlgzGetTeletexPage(pageNr string, dirStation string) bool {
	service := strings.TrimPrefix(dirStation, tlgzFolderPrefix)
	parts := strings.SplitN(pageNr, "-", 2)
	page := parts[0]
	pageNum, err := strconv.Atoi(page)
	if err != nil || len(page) != 3 {
		fmt.Println("TLGZ: invalid page number", pageNr)
		return true
	}
	sub := 1
	if len(parts) > 1 {
		if v, err := strconv.Atoi(parts[1]); err == nil && v > 0 && v <= tlgzMaxSubpageNum {
			sub = v
		}
	}

	status, reachable := tlgzGetStatus(service)
	if status == nil {
		return reachable
	}
	subCount := status.subpages[pageNum]
	if subCount == 0 || sub > subCount {
		fmt.Printf("TLGZ: page %s-%d does not exist on %s\n", page, sub, service)
		return true
	}

	url := fmt.Sprintf("%s%s/%c00/%s_%04d.png", tlgzBaseURL, service, page[0], page, sub)
	logFetchingPage(url)
	pngData, _, reachable := imgTxtFetch(tlgzProfile.name, url)
	if pngData == nil {
		return reachable
	}
	img, err := png.Decode(bytes.NewReader(pngData))
	if err != nil {
		fmt.Println("TLGZ: PNG decode error:", err, "for", url)
		return true
	}
	pageBuffer, unknown := imgTxtDecodeImage(img, tlgzProfile)
	if pageBuffer == nil {
		fmt.Println("TLGZ: unexpected PNG size", img.Bounds().Size(), "for", url)
		return true
	}
	tlgzProfile.logUnknownCells(fmt.Sprintf("%s %s_%04d", service, page, sub), unknown)
	pageBuffer[0] = tlgzHeaderRow(service, page)

	var nav NavignationInfo
	nav.prevPage, nav.nextPage = status.prevPage(pageNum), status.nextPage(pageNum)
	nav.numberOfSubpages = subCount
	nav.prevSubpage = sub - 1
	if sub < subCount {
		nav.nextSubpage = sub + 1
	}
	pp, np := buildPageNavDirectives(nav.prevPage, nav.nextPage)
	ps, ns, _ := getPrevNextSubpage(page, nav)

	var output []byte
	output = append(output, []byte(pp+np+ps+ns)...)
	for _, f := range tlgzFastext(pageBuffer[24], status, pageNum) {
		output = append(output, []byte("ftl="+f+"\n")...)
	}
	output = append(output, []byte("<pre>")...)
	for _, r := range pageBuffer {
		output = append(output, r...)
	}
	output = append(output, []byte("</pre>")...)

	savePage(dirStation, pageNr, output)
	return true
}

func tlgzHeaderRow(service, page string) []byte {
	days := []string{"nie", "pon", "wto", "śro", "czw", "pią", "sob"}
	months := []string{"sty", "lut", "mar", "kwi", "maj", "cze", "lip", "sie", "wrz", "paź", "lis", "gru"}
	now := time.Now()
	name := tlgzServiceNames[service]
	if name == "" {
		name = service
	}
	text := fmt.Sprintf("\x02%-9s%s\x03%s %02d %s  %s",
		name, page, days[now.Weekday()], now.Day(), months[now.Month()-1], now.Format("15:04:05"))
	var header []byte
	for _, r := range text {
		if r < 0x20 {
			header = append(header, byte(r)) // control code
		} else {
			header = append(header, tlgzRuneToByte(r))
		}
	}
	row := newPageBuffer(1)[0]
	if len(header) > len(row) {
		header = header[:len(row)]
	}
	copy(row[len(row)-len(header):], header) // right aligned
	return row
}

// pagenumbers in the fastext row
var tlgzFastextNumberRe = regexp.MustCompile(`\b[1-8][0-9][0-9]\b`)

// tlgzFastext returns the 4 fastext links. Page numbers in the fastext row are used in order. When there are fewer
// than 4, the others follow the TOP system: red = previous page, green = next page, yellow = next group,
// cyan = next block.
func tlgzFastext(row24 []byte, status *tlgzStatus, pageNum int) []string {
	// the row holds control codes and Polish letters; only digits matter here
	clean := make([]byte, len(row24))
	for i, b := range row24 {
		if b >= '0' && b <= '9' {
			clean[i] = b
		} else {
			clean[i] = ' '
		}
	}
	links := tlgzFastextNumberRe.FindAllString(string(clean), 4)
	top := []int{status.prevPage(pageNum), status.nextPage(pageNum), status.nextOfType(pageNum, "group", "block"), status.nextOfType(pageNum, "block")}
	for i := len(links); i < 4; i++ {
		p := top[i]
		if p == 0 {
			p = 100
		}
		links = append(links, strconv.Itoa(p))
	}
	return links
}

// --- status.json ---

type tlgzStatus struct {
	fetched  time.Time
	pages    []int          // existing pages, ascending
	subpages map[int]int    // page -> number of subpages
	topType  map[int]string // page -> "block", "group" or "normal"
}

var (
	tlgzStatusMu    sync.Mutex
	tlgzStatusCache = map[string]*tlgzStatus{}
)

type tlgzStatusJSON struct {
	Teletext struct {
		Pages map[string]struct {
			Page struct {
				TopType      string `json:"toptype"`
				SubpageCount string `json:"subpagecount"`
			} `json:"page"`
		} `json:"pages"`
	} `json:"teletext"`
}

// tlgzGetStatus returns the page list of a service, fetched at most once per tlgzStatusMaxAge. When fetching fails,
// an older copy is used if there is one.
func tlgzGetStatus(service string) (*tlgzStatus, bool) {
	tlgzStatusMu.Lock()
	cached := tlgzStatusCache[service]
	tlgzStatusMu.Unlock()
	if cached != nil && time.Since(cached.fetched) < tlgzStatusMaxAge {
		return cached, true
	}

	data, _, reachable := imgTxtFetch(tlgzProfile.name, tlgzBaseURL+service+"/status.json")
	if data == nil {
		return cached, reachable
	}
	var js tlgzStatusJSON
	if err := json.Unmarshal(data, &js); err != nil {
		fmt.Println("TLGZ: status.json error:", err, "for", service)
		return cached, true
	}
	status := &tlgzStatus{fetched: time.Now(), subpages: map[int]int{}, topType: map[int]string{}}
	for key, p := range js.Teletext.Pages {
		num, err1 := strconv.Atoi(strings.TrimPrefix(key, "p"))
		count, err2 := strconv.Atoi(p.Page.SubpageCount)
		if err1 != nil || err2 != nil || count < 1 {
			continue // a page with 0 subpages has no image
		}
		status.pages = append(status.pages, num)
		status.subpages[num] = count
		status.topType[num] = p.Page.TopType
	}
	sort.Ints(status.pages)

	tlgzStatusMu.Lock()
	tlgzStatusCache[service] = status
	tlgzStatusMu.Unlock()
	return status, true
}

func (s *tlgzStatus) prevPage(pageNum int) int {
	i := sort.SearchInts(s.pages, pageNum)
	if i > 0 {
		return s.pages[i-1]
	}
	return 0
}

func (s *tlgzStatus) nextPage(pageNum int) int {
	i := sort.SearchInts(s.pages, pageNum+1)
	if i < len(s.pages) {
		return s.pages[i]
	}
	return 0
}

func (s *tlgzStatus) nextOfType(pageNum int, types ...string) int {
	matches := func(p int) bool {
		for _, t := range types {
			if s.topType[p] == t {
				return true
			}
		}
		return false
	}
	start := sort.SearchInts(s.pages, pageNum+1)
	for i := 0; i < len(s.pages); i++ {
		p := s.pages[(start+i)%len(s.pages)]
		if p != pageNum && matches(p) {
			return p
		}
	}
	return 0
}
