package main

// Forum64: RSS-generated teletext pages
//
// Single merged category: forum64.de only exposes one public RSS feed - recent thread activity across the
// whole forum, no per-board filtering :-(
//
// Templates in assets/forum64/index.bin and assets/forum64/story.bin (similar to NOSNEWS)

import (
	_ "embed"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/html"
)

//go:embed assets/forum64/index.bin
var forum64IndexTpl []byte

//go:embed assets/forum64/story.bin
var forum64StoryTpl []byte

const forum64PollInterval = 1 * time.Minute
const forum64TemplateRows = 24

type forum64Thread struct {
	pageNr     int
	guid       string
	title      string
	creator    string // author of the LATEST reply
	comments   int
	pubTime    time.Time
	paragraphs []string // the opening post, plain paragraphs
	lastReply  []string // the latest reply's own text (content:encoded), plain paragraphs
}

type forum64Store struct {
	mu        sync.RWMutex
	threads   []*forum64Thread // newest first
	freePages []int
}

func newForum64Store(firstPage, lastPage int) *forum64Store {
	free := make([]int, 0, lastPage-firstPage+1)
	for p := firstPage; p <= lastPage; p++ {
		free = append(free, p)
	}
	return &forum64Store{freePages: free}
}

func (s *forum64Store) update(fresh []*forum64Thread, maxItems int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	byGUID := make(map[string]*forum64Thread, len(s.threads))
	for _, t := range s.threads {
		byGUID[t.guid] = t
	}

	var newOnes []*forum64Thread
	seenNew := make(map[string]bool, len(fresh))
	for _, it := range fresh {
		if it.guid == "" {
			continue
		}
		// A thread's GUID never changes when someone posts a new reply to it - only its
		// <dc:creator>/<slash:comments>/<content:encoded> do, since those describe the LATEST
		// reply, not the thread itself
		if existing, ok := byGUID[it.guid]; ok {
			existing.title = it.title
			existing.creator = it.creator
			existing.comments = it.comments
			existing.pubTime = it.pubTime
			existing.paragraphs = it.paragraphs
			existing.lastReply = it.lastReply
			continue
		}
		if seenNew[it.guid] {
			continue
		}
		newOnes = append(newOnes, it)
		seenNew[it.guid] = true
	}
	if len(newOnes) == 0 {
		return
	}
	if len(newOnes) > maxItems {
		newOnes = newOnes[:maxItems]
	}

	for _, it := range newOnes {
		switch {
		case len(s.freePages) > 0:
			it.pageNr = s.freePages[0]
			s.freePages = s.freePages[1:]
		case len(s.threads) > 0:
			oldest := s.threads[len(s.threads)-1]
			s.threads = s.threads[:len(s.threads)-1]
			it.pageNr = oldest.pageNr
		default:
			continue
		}
	}

	s.threads = append(newOnes, s.threads...)

	for len(s.threads) > maxItems {
		oldest := s.threads[len(s.threads)-1]
		s.threads = s.threads[:len(s.threads)-1]
		s.freePages = append(s.freePages, oldest.pageNr)
	}
}

func (s *forum64Store) list() []*forum64Thread {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*forum64Thread, len(s.threads))
	copy(out, s.threads)
	return out
}

func (s *forum64Store) findByPage(pageNr int) *forum64Thread {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, t := range s.threads {
		if t.pageNr == pageNr {
			return t
		}
	}
	return nil
}

type forum64Category struct {
	name        string
	displayName string
	feedURL     string
	indexPage   int
	firstPage   int
	lastPage    int
	maxItems    int
	indexTpl    []byte
	storyTpl    []byte
	store       *forum64Store

	pollMu     sync.Mutex
	lastPolled time.Time
}

var forum64Categories = []*forum64Category{
	{
		name:        "forum",
		displayName: "Forum64",
		feedURL:     "https://www.forum64.de/index.php?thread-list-rss-feed/",
		indexPage:   100,
		firstPage:   101,
		lastPage:    120,
		maxItems:    20,
		indexTpl:    forum64IndexTpl,
		storyTpl:    forum64StoryTpl,
	},
}

func init() {
	for _, c := range forum64Categories {
		c.store = newForum64Store(c.firstPage, c.lastPage)
	}
}

// pollIfStale refreshes the category's feed, but only if it's actually been requested and the last poll is
// older than forum64PollInterval - no requests means no network traffic, same no-background-ticker convention as NOSNEWS.
func (c *forum64Category) pollIfStale() {
	c.pollMu.Lock()
	stale := time.Since(c.lastPolled) >= forum64PollInterval
	if stale {
		c.lastPolled = time.Now()
	}
	c.pollMu.Unlock()
	if !stale {
		return
	}
	c.poll()
}

func (c *forum64Category) poll() {
	items, err := fetchForum64Feed(c.feedURL)
	if err != nil {
		fmt.Println("FORUM64: fetch error for", c.name, err)
		return
	}
	c.store.update(items, c.maxItems)
}

// --- feed fetching / parsing ---

type forum64RSS struct {
	XMLName xml.Name `xml:"rss"`
	Channel struct {
		Items []struct {
			Title       string `xml:"title"`
			Link        string `xml:"link"`
			Description string `xml:"description"`
			PubDate     string `xml:"pubDate"`
			GUID        string `xml:"guid"`
			Creator     string `xml:"http://purl.org/dc/elements/1.1/ creator"`
			Comments    int    `xml:"http://purl.org/rss/1.0/modules/slash/ comments"`
			Content     string `xml:"http://purl.org/rss/1.0/modules/content/ encoded"`
		} `xml:"item"`
	} `xml:"channel"`
}

func fetchForum64Feed(url string) ([]*forum64Thread, error) {
	logBackgroundPoll(DirFORUM64, url)
	client := http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	var feed forum64RSS
	if err := xml.Unmarshal(body, &feed); err != nil {
		return nil, err
	}

	out := make([]*forum64Thread, 0, len(feed.Channel.Items))
	for _, it := range feed.Channel.Items {
		out = append(out, &forum64Thread{
			guid:       strings.TrimSpace(it.GUID),
			title:      forum64UnsafePunctuation.Replace(strings.TrimSpace(it.Title)),
			creator:    forum64UnsafePunctuation.Replace(strings.TrimSpace(it.Creator)),
			comments:   it.Comments,
			pubTime:    parseForum64PubDate(it.PubDate),
			paragraphs: parseForum64Text(it.Description),
			lastReply:  parseForum64Text(it.Content),
		})
	}
	return out, nil
}

func parseForum64PubDate(s string) time.Time {
	t, err := time.Parse("Mon, 2 Jan 2006 15:04:05 -0700", strings.TrimSpace(s))
	if err != nil {
		return time.Time{}
	}
	return t
}

// square to round brackets, because teletext doesn't these
var forum64UnsafePunctuation = strings.NewReplacer("[", "(", "]", ")")

// Any paragraph containing this is dropped entirely
var forum64NoisePhrases = []string{
	"Bitte melde Dich an, um diesen Anhang zu sehen",
}

func parseForum64Text(descHTML string) []string {
	var paras []string
	z := html.NewTokenizer(strings.NewReader(descHTML))
	var buf strings.Builder
	var skipStack []bool
	skipDepth := 0
	brRun := 0

	flush := func() {
		text := strings.Join(strings.Fields(buf.String()), " ")
		text = forum64UnsafePunctuation.Replace(text)
		for _, noise := range forum64NoisePhrases {
			if strings.Contains(text, noise) {
				text = ""
				break
			}
		}
		if text != "" {
			paras = append(paras, text)
		}
		buf.Reset()
	}

	for {
		tt := z.Next()
		if tt == html.ErrorToken {
			break
		}
		tok := z.Token()
		switch tt {
		case html.StartTagToken, html.SelfClosingTagToken:
			switch tok.Data {
			case "div":
				isSkip := skipDepth > 0
				if !isSkip {
					for _, a := range tok.Attr {
						if a.Key == "class" && strings.Contains(a.Val, "contentNotVisible") {
							isSkip = true
						}
					}
				}
				skipStack = append(skipStack, isSkip)
				if isSkip {
					skipDepth++
				}
			case "br":
				if skipDepth == 0 {
					brRun++
					if brRun >= 2 {
						flush()
						brRun = 0
					} else {
						buf.WriteString(" ")
					}
				}
			}
		case html.EndTagToken:
			if tok.Data == "div" && len(skipStack) > 0 {
				wasSkip := skipStack[len(skipStack)-1]
				skipStack = skipStack[:len(skipStack)-1]
				if wasSkip {
					skipDepth--
				}
			}
		case html.TextToken:
			if skipDepth == 0 && strings.TrimSpace(tok.Data) != "" {
				brRun = 0
				buf.WriteString(tok.Data)
				buf.WriteString(" ")
			}
		}
	}
	flush()
	return paras
}

// --- rendering ---

var forum64Days = []string{"Mo", "Di", "Mi", "Do", "Fr", "Sa", "So"}
var forum64Months = []string{"Jan", "Feb", "Mär", "Apr", "Mai", "Jun", "Jul", "Aug", "Sep", "Okt", "Nov", "Dez"}

func forum64HeaderRow() []byte {
	now := time.Now()
	dateStr := fmt.Sprintf("%s %02d %s", forum64Days[(int(now.Weekday())+6)%7], now.Day(), forum64Months[now.Month()-1])

	row := blankTeletextRow()
	text := fmt.Sprintf("\x07FORUM64   \x07%s \x06%s", dateStr, now.Format("15:04:05"))
	copy(row[9:], []byte(text))
	return row
}

func forum64BuildOutput(filledTpl []byte, ps, ns string) []byte {
	var out []byte
	out = append(out, []byte(ps)...)
	out = append(out, []byte(ns)...)
	out = append(out, []byte("<pre>")...)
	out = append(out, forum64HeaderRow()...)
	out = append(out, filledTpl...)
	out = append(out, []byte("</pre>")...)
	return out
}

func forum64TopicLabel(cat *forum64Category) string {
	if cat.displayName != "" {
		return cat.displayName
	}
	return cat.name
}

func writeTokenValue(out, tpl []byte, rows int, token, value string, rowOffset int) {
	for row := 0; row < rows; row++ {
		_, col, _, found := findMarkerInRows(tpl, row, row+1, token)
		if !found {
			continue
		}
		outRow := row + rowOffset
		if outRow < 0 || outRow >= rows {
			continue
		}
		rowStart := outRow * 40
		eraseEnd := col + len(token)
		if eraseEnd > 40 {
			eraseEnd = 40
		}
		for k := col; k < eraseEnd; k++ {
			out[rowStart+k] = 0x20
		}
		writeEnd := rowStart + col + len(value)
		if writeEnd > rowStart+40 {
			writeEnd = rowStart + 40
		}
		copy(out[rowStart+col:writeEnd], []byte(value))
	}
}

func renderForum64IndexPage(cat *forum64Category, sub int) []byte {
	tpl := cat.indexTpl
	hRowOdd, hColOdd, hPrefixOdd, hFoundOdd := findMarker(tpl, forum64TemplateRows, "<headlineodd>")
	hRowEven, _, hPrefixEven, hFoundEven := findMarker(tpl, forum64TemplateRows, "<headlineeven>")
	_, pCol, pPrefix, pFound := findMarker(tpl, forum64TemplateRows, "<p>")
	if !hFoundOdd || !hFoundEven || !pFound {
		fmt.Println("FORUM64: index template for", cat.name, "is missing <headlineodd>, <headlineeven> or <p>")
		return forum64BuildOutput(tpl, "", "")
	}

	hCol := hColOdd
	hRow := hRowOdd
	anchorEnd := hRowOdd
	if hRowEven < hRow {
		hRow = hRowEven
	}
	if hRowEven > anchorEnd {
		anchorEnd = hRowEven
	}
	contentEnd := findContentEnd(tpl, forum64TemplateRows, anchorEnd)
	pColorCol := pCol - 1
	capacity := contentEnd - hRow

	width := pColorCol - hCol
	if width < 5 {
		width = 5
	}

	var groups [][][]byte
	for i, t := range cat.store.list() {
		prefix := hPrefixOdd
		if i%2 == 1 {
			prefix = hPrefixEven
		}
		lines := wrapTeletext(t.title, width)
		if len(lines) == 0 {
			continue
		}
		var block [][]byte
		for j, line := range lines {
			row := blankTeletextRow()
			row[0] = prefix
			copy(row[hCol:], []byte(line))
			if j == len(lines)-1 {
				row[pColorCol] = pPrefix
				copy(row[pCol:], []byte(fmt.Sprintf("%3d", t.pageNr)))
			}
			block = append(block, row)
		}
		groups = append(groups, block)
		groups = append(groups, [][]byte{blankTeletextRow()})
	}

	pages := paginateGroups(groups, capacity, capacity)
	trimLeadingBlanks(pages)
	if len(pages) == 0 {
		pages = [][][]byte{{}}
	}
	if sub < 1 {
		sub = 1
	}
	if sub > len(pages) {
		sub = len(pages)
	}

	out := make([]byte, len(tpl))
	copy(out, tpl)

	if tRow, tCol, _, tFound := findMarker(tpl, forum64TemplateRows, nosNewsTopicToken); tFound {
		label := spreadCapsLabel(forum64TopicLabel(cat))
		rowStart := tRow * 40
		for k := tCol; k < 40; k++ {
			out[rowStart+k] = 0x20
		}
		copy(out[rowStart+tCol:rowStart+40], []byte(label))
	}

	for r := hRow; r < contentEnd; r++ {
		copy(out[r*40:(r+1)*40], blankTeletextRow())
	}
	for i, row := range pages[sub-1] {
		r := hRow + i
		if r >= contentEnd {
			break
		}
		copy(out[r*40:(r+1)*40], row)
	}
	stampSubpageIndicator(out, contentEnd, sub, len(pages))

	var nav NavignationInfo
	nav.numberOfSubpages = len(pages)
	if sub > 1 {
		nav.prevSubpage = sub - 1
	}
	if sub < len(pages) {
		nav.nextSubpage = sub + 1
	}
	ps, ns, _ := getPrevNextSubpage(strconv.Itoa(cat.indexPage), nav)

	return forum64BuildOutput(out, ps, ns)
}

func renderForum64StoryPage(cat *forum64Category, t *forum64Thread, sub int) []byte {
	tpl := cat.storyTpl
	hRow, hCol, hPrefix, hFound := findMarker(tpl, forum64TemplateRows, "<headline>")
	sRow, sCol, sPrefix, sFound := findMarker(tpl, forum64TemplateRows, "<story>")
	if !hFound || !sFound {
		fmt.Println("FORUM64: story template for", cat.name, "is missing <headline> or <story>")
		return forum64BuildOutput(tpl, "", "")
	}

	// <lastreply> is optional
	lrRow, lrCol, lrPrefix, lrFound := findMarker(tpl, forum64TemplateRows, "<lastreply>")
	anchorRow := sRow
	if lrFound && lrRow > anchorRow {
		anchorRow = lrRow
	}
	contentEnd := findContentEnd(tpl, forum64TemplateRows, anchorRow)

	headlineWidth := 40 - hCol
	storyWidth := 40 - sCol
	lastReplyWidth := 40 - lrCol

	var headlineRows [][]byte
	for _, line := range wrapTeletext(t.title, headlineWidth) {
		row := blankTeletextRow()
		row[0] = hPrefix
		copy(row[hCol:], []byte(line))
		headlineRows = append(headlineRows, row)
	}

	// first add <lastreply> comes first in the flow
	var stream [][]byte
	if lrFound && len(t.lastReply) > 0 {
		for _, p := range t.lastReply {
			for _, line := range wrapTeletext(p, lastReplyWidth) {
				row := blankTeletextRow()
				row[0] = lrPrefix
				copy(row[lrCol:], []byte(line))
				stream = append(stream, row)
			}
			stream = append(stream, blankTeletextRow())
		}
	}

	// add seperator
	row := blankTeletextRow()
	copy(row, "\x03Erster Beitrag:")
	stream = append(stream, row)
	stream = append(stream, blankTeletextRow())

	// add original first post <story>
	for _, p := range t.paragraphs {
		for _, line := range wrapTeletext(p, storyWidth) {
			row := blankTeletextRow()
			row[0] = sPrefix
			copy(row[sCol:], []byte(line))
			stream = append(stream, row)
		}
		stream = append(stream, blankTeletextRow())
	}

	headlineExtra := 0
	if n := len(headlineRows); n > 1 {
		headlineExtra = n - 1
	}

	firstCap := contentEnd - sRow - headlineExtra
	restCap := contentEnd - (hRow + 1)
	pages := paginateRows(stream, firstCap, restCap)
	trimLeadingBlanks(pages)
	if len(pages) == 0 {
		pages = [][][]byte{{}}
	}
	if sub < 1 {
		sub = 1
	}
	if sub > len(pages) {
		sub = len(pages)
	}

	out := make([]byte, len(tpl))
	copy(out, tpl)

	if tRow, tCol, _, tFound := findMarker(tpl, forum64TemplateRows, nosNewsTopicToken); tFound {
		label := forum64TopicLabel(cat)
		rowStart := tRow * 40
		for k := tCol; k < 40; k++ {
			out[rowStart+k] = 0x20
		}
		copy(out[rowStart+tCol:rowStart+40], []byte(label))
	}

	// shift is the row-position push applied to everything between <headline> and the footer, on subpage 1 only
	shift := 0
	if sub == 1 {
		shift = headlineExtra
	}
	if shift > 0 {
		// Rebuild every row between <headline> and the footer at its shifted position
		for r := hRow + 1; r < contentEnd; r++ {
			rowStart := r * 40
			src := r - shift
			if src <= hRow {
				copy(out[rowStart:rowStart+40], blankTeletextRow())
				continue
			}
			copy(out[rowStart:rowStart+40], tpl[src*40:(src+1)*40])
		}
	}
	sRowOut := sRow + shift
	lrRowOut := lrRow + shift

	if lrFound && lrRowOut < contentEnd {
		start := lrRowOut*40 + lrCol
		end := start + len("<lastreply>")
		if end > len(out) {
			end = len(out)
		}
		for k := start; k < end; k++ {
			out[k] = 0x20
		}
	}

	startRow := sRowOut
	if sub == 1 {
		for i, row := range headlineRows {
			r := hRow + i
			if r >= sRowOut || r >= contentEnd {
				break
			}
			copy(out[r*40:(r+1)*40], row)
		}
	} else {
		startRow = hRow + 1
		for r := hRow; r < startRow; r++ {
			copy(out[r*40:(r+1)*40], blankTeletextRow())
		}
	}

	for r := startRow; r < contentEnd; r++ {
		copy(out[r*40:(r+1)*40], blankTeletextRow())
	}
	for i, row := range pages[sub-1] {
		r := startRow + i
		if r >= contentEnd {
			break
		}
		copy(out[r*40:(r+1)*40], row)
	}

	if sub == 1 {
		if t.creator != "" {
			writeTokenValue(out, tpl, forum64TemplateRows, "<creator>", t.creator, shift)
		}
		writeTokenValue(out, tpl, forum64TemplateRows, "<comments>", strconv.Itoa(t.comments), shift)
	}

	stampSubpageIndicator(out, contentEnd, sub, len(pages))

	var nav NavignationInfo
	nav.numberOfSubpages = len(pages)
	if sub > 1 {
		nav.prevSubpage = sub - 1
	}
	if sub < len(pages) {
		nav.nextSubpage = sub + 1
	}
	ps, ns, _ := getPrevNextSubpage(strconv.Itoa(t.pageNr), nav)

	return forum64BuildOutput(out, ps, ns)
}

func forum64FindCategory(base string) (cat *forum64Category, isIndex bool, pageNr int) {
	n, err := strconv.Atoi(base)
	if err != nil {
		return nil, false, 0
	}
	for _, c := range forum64Categories {
		if n == c.indexPage {
			return c, true, n
		}
		if n >= c.firstPage && n <= c.lastPage {
			return c, false, n
		}
	}
	return nil, false, 0
}

func forum64GetTeletexPage(pageNr string) bool {
	parts := strings.SplitN(pageNr, "-", 2)
	base := parts[0]
	sub := 1
	if len(parts) > 1 && parts[1] != "" {
		if v, err := strconv.Atoi(parts[1]); err == nil && v > 0 {
			sub = v
		}
	}

	cat, isIndex, n := forum64FindCategory(base)
	if cat == nil {
		return false
	}
	cat.pollIfStale()

	var output []byte
	if isIndex {
		output = renderForum64IndexPage(cat, sub)
	} else {
		t := cat.store.findByPage(n)
		if t == nil {
			return false
		}
		output = renderForum64StoryPage(cat, t, sub)
	}
	savePage(DirFORUM64, pageNr, output)
	return true
}
