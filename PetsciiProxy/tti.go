package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-git/go-git/v5"
	"golang.org/x/net/html"
)

// Chunkeytext
const chunkytextRepoURL = "https://zxnet.co.uk/git/cf.git"

const chunkytextSyncInterval = 15 * time.Minute

var chunkytextMutex sync.RWMutex

// --- ChunkyText (git-mirrored teletext service) ---
// Return value is required to satisfy fetchFunc's signature but is ignored by makeHandler for
// stations in localOnlyStations (this one included) - ChunkyText's own git-sync ticker tracks
// reachability, not per-page reads.
func chunkytextGetTeletexPage(pageNr string) bool {
	parts := strings.Split(pageNr, "-")

	chunkytextMutex.RLock()
	filePath := filepath.Join(DirCHUNKYTEXT, "P"+parts[0]+".tti")
	f, err := os.Open(filePath)
	logFetchingPage(filePath)
	chunkytextMutex.RUnlock()
	if err != nil {
		fmt.Println("ChunkyText page not found:", pageNr, err)
		return false
	}
	defer f.Close()

	rows, nav := parseTTIRows(f, parts[0], parts[1], true)
	ps, ns, ct := getPrevNextSubpage(parts[0], nav)

	var output []byte
	output = append(output, []byte(fmt.Sprintf(
		"pn=p_\npn=n_\n%v%v%vftl=%v-0\nftl=%v-0\nftl=%v-0\nftl=%v-0\n<pre>",
		ps, ns, ct,
		string(ftl[0]), string(ftl[1]), string(ftl[2]), string(ftl[3])))...)

	for _, row := range rows {
		output = append(output, row...)
	}

	output = append(output, []byte("</pre>")...)
	savePage(DirCHUNKYTEXT, pageNr, output)
	return true
}

func syncChunkytextRepo() {
	chunkytextMutex.Lock()
	defer chunkytextMutex.Unlock()

	repo, err := git.PlainOpen(DirCHUNKYTEXT)
	if err != nil {
		fmt.Println("ChunkyText: cloning repository...")
		_, cloneErr := git.PlainClone(DirCHUNKYTEXT, false, &git.CloneOptions{
			URL: chunkytextRepoURL,
		})
		if cloneErr != nil {
			fmt.Println("ChunkyText clone error:", cloneErr)
		}
		return
	}

	w, err := repo.Worktree()
	if err != nil {
		fmt.Println("ChunkyText worktree error:", err)
		return
	}
	err = w.Pull(&git.PullOptions{RemoteName: "origin"})
	if err != nil && err != git.NoErrAlreadyUpToDate {
		fmt.Println("ChunkyText pull error:", err)
	}
}

// --- CEEFAX ---

var ftl [][]byte // gets filled by parseTTIRows

func ceefaxGetTeletexPage(pageNr string) bool {
	parts := strings.Split(pageNr, "-")
	url := fmt.Sprintf("https://feeds.nmsni.co.uk/svn/ceefax/Worldwide/P%s.tti", parts[0])
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

	rows, nav := parseTTIRows(resp.Body, parts[0], parts[1], true)
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
	savePage(DirCEEFAX, pageNr, output)
	return true
}

// --- TEEFAX ---

func teefaxGetTeletexPage(pageNr string) bool {
	parts := strings.Split(pageNr, "-")
	url, err := getTeefaxURL(parts[0])
	if err != nil {
		// Page not in the TEEFAX directory listing; not a connectivity problem because the directory
		// was fetched and parsed. So station's reachability issue here.
		fmt.Printf("Page %s: Error: %v\n", parts[0], err)
		return true
	}

	if strings.HasPrefix(pageNr, "100") {
		// Force 2nd subpage to be fetched(1st one has a really big banner on it)
		parts[1] = "2"
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

	rows, nav := parseTTIRows(resp.Body, parts[0], parts[1], false)
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
	savePage(DirTEEFAX, pageNr, output)
	return true
}

// --- Webfax 1 & 2 ---

func webfaxGetTeletexPage(pageNr string, station string, dirStation string) bool {
	parts := strings.Split(pageNr, "-")
	url := fmt.Sprintf("https://github.com/Webfax-Teletext/%s-Teletext/raw/refs/heads/main/P%s.tti", station, parts[0])
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

	rows, nav := parseTTIRows(resp.Body, parts[0], parts[1], true)
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
	savePage(dirStation, pageNr, output)
	return true
}

// --- SPARK ---

func sparkGetTeletexPage(pageNr string) bool {
	parts := strings.Split(pageNr, "-")
	url := fmt.Sprintf("https://raw.githubusercontent.com/spark-teletext/spark-teletext/master/P%s.tti", parts[0])
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

	rows, nav := parseTTIRows(resp.Body, parts[0], parts[1], true)
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
	savePage(DirSPARK, pageNr, output)
	return true
}

var subpage byte
var fullDoubleHeightRow bool

func parseTTIRows(r io.Reader, pageStr string, subpageStr string, isCEEFAX bool) ([][]byte, NavignationInfo) {
	subpageFound := false
	escFound := false

	var nav NavignationInfo
	nav.nextSubpage = 0
	nav.prevSubpage = 0
	nav.cycleTime = 0

	rows := newPageBuffer(25)

	data, _ := io.ReadAll(r)
	// On TEEFAX there are pages that have mixed \r\n and just \n
	normalizedData := bytes.ReplaceAll(data, []byte("\r"), []byte(""))
	lines := bytes.Split(normalizedData, []byte("\n"))

	subpage, _ := strconv.Atoi(subpageStr)

	for _, line := range lines {
		// A TTI format teletext line looks something like this: OL,23, D ] CCATCH UP WITH REGIONAL NEWS       G160
		parts := bytes.SplitN(line, []byte(","), 3)

		// Process page number and subpage number. Note: We get all the subpages at once in TTI format, so we
		// have to detect which part of the data we need to process. In TTI format, the first row of a new
		// teletextpage starts with a PN, e.g. PN,10203. Where 102 is the page number and 03 is the subpage
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
				// set default cycle time to 5 seconds; this value may be adjusted if the page has a CT command (see below)
				// note: this value is deducted by looking at NMS Ceefax and time how long each subpage is shown
				nav.cycleTime = 5
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
				// changed from break to continue because I stumbled across a TTI page that started
				// with 2 "OL, 26" rows and then the regular OL,1 OL,2...OL,24
				// source page: Chunkytext page 131 about Discord
				// the 'weird' OL,26 rows here below, any clue what this is? I could dig into it..
				// OL,26,@idPa@NjdPaCNa`CaAPbA_bMfcA~cmfdavdMgeAP
				// OL,26,AkdPa`CaCNaAPbasbmgcaQcMhda]dmheAPldPa@N
				continue //break;
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

func formatTime(timestamp int64, useTimestamp bool) string {
	var days = map[string]string{
		"Mon": "Mon", "Tue": "Tue", "Wed": "Wed", "Thu": "Thu", "Fri": "Fri", "Sat": "Sat", "Sun": "Sun",
	}
	var months = map[string]string{
		"Jan": "Jan", "Feb": "Feb", "Mar": "Mar", "Apr": "Apr", "May": "May", "Jun": "Jun",
		"Jul": "Jul", "Aug": "Aug", "Sep": "Sep", "Oct": "Oct", "Nov": "Nov", "Dec": "Dec",
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
