package main

// NOS Nieuws: RSS-generated teletext pages
//
// Templates:
// - in folder assets/nosnews/
// - nosnews.bin is for page 100
// - <category>_index.bin and <category>_story.bin
// Each is a 24-row (960 byte) raw teletext buffer, made with https://zxnet.co.uk/teletext/editor.
// Row 0 (the "NOS NIEUWS <date> <time>" is generated and not part of the template
//
// Layout convention inside the per-category templates:
// <category>_index.bin:
// - <topic>
// - <shortdate> / <headline> / <p> : one row, dynamically repeated
// <category>_story.bin
// - <topic>
// - <datetime>
// - <headline> / <heading>
// - <story> : each its own row
//
// Every token is expected to be preceded by one teletext colour control byte
//
// <heading> is a colour declaration for <h2> sub-headings within the story body
// (NOS's bold in-article headers)
//
// <shortdate> is optional, index-template only: placed  before <headline> on that same row to print
// a compact date ahead of the headline text.

import (
	"bytes"
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

// Note: the 'go:embed' below are not comments, they are needed to include the bin files in the executables

//go:embed assets/nosnews/nieuws_index.bin
var nosnieuwsIndexTpl []byte

//go:embed assets/nosnews/nieuws_story.bin
var nosnieuwsStoryTpl []byte

//go:embed assets/nosnews/sport_index.bin
var nossportIndexTpl []byte

//go:embed assets/nosnews/sport_story.bin
var nossportStoryTpl []byte

//go:embed assets/nosnews/nieuwsuur_index.bin
var nieuwsuurIndexTpl []byte

//go:embed assets/nosnews/nieuwsuur_story.bin
var nieuwsuurStoryTpl []byte

//go:embed assets/nosnews/koningshuis_index.bin
var koningshuisIndexTpl []byte

//go:embed assets/nosnews/koningshuis_story.bin
var koningshuisStoryTpl []byte

//go:embed assets/nosnews/jeugdjournaal_index.bin
var jeugdjournaalIndexTpl []byte

//go:embed assets/nosnews/jeugdjournaal_story.bin
var jeugdjournaalStoryTpl []byte

//go:embed assets/nosnews/nosnews.bin
var nosnewsMasterTpl []byte

const nosNewsPollInterval = 1 * time.Minute

const nosNewsMasterPage = 100

// --- data model ---

type nosNewsParagraph struct {
	text    string
	heading bool // true for <h2>/<h3> blocks
}

type nosNewsArticle struct {
	pageNr     int
	guid       string
	title      string
	pubTime    time.Time
	dateTime   string
	paragraphs []nosNewsParagraph
}

// nosNewsStore holds one category's currently-visible articles and the pool of page numbers they can be assigned. Same as
// Chunkytext's pattern of a mutex-guarded shared state refreshed by a background ticker and read by the per-request handler.
type nosNewsStore struct {
	mu        sync.RWMutex
	articles  []*nosNewsArticle // newest first
	freePages []int
}

func newNosNewsStore(firstPage, lastPage int) *nosNewsStore {
	free := make([]int, 0, lastPage-firstPage+1)
	for p := firstPage; p <= lastPage; p++ {
		free = append(free, p)
	}
	return &nosNewsStore{freePages: free}
}

// merges freshly-fetched articles (newest-first) into the store: unseen guids get the next free page number (or, once
// the pool is empty, recycle the number from whichever article is about to age out).
func (s *nosNewsStore) update(fresh []*nosNewsArticle, maxItems int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	known := make(map[string]bool, len(s.articles))
	for _, a := range s.articles {
		known[a.guid] = true
	}

	var newOnes []*nosNewsArticle
	for _, it := range fresh {
		if it.guid == "" || known[it.guid] {
			continue
		}
		newOnes = append(newOnes, it)
		known[it.guid] = true
	}
	if len(newOnes) == 0 {
		return
	}
	// The pool only ever holds maxItems slots and keeps only the newest maxItems
	if len(newOnes) > maxItems {
		newOnes = newOnes[:maxItems]
	}

	for _, it := range newOnes {
		switch {
		case len(s.freePages) > 0:
			it.pageNr = s.freePages[0]
			s.freePages = s.freePages[1:]
		case len(s.articles) > 0:
			oldest := s.articles[len(s.articles)-1]
			s.articles = s.articles[:len(s.articles)-1]
			it.pageNr = oldest.pageNr
		default:
			// pool empty: skip
			continue
		}
	}

	s.articles = append(newOnes, s.articles...)

	for len(s.articles) > maxItems {
		oldest := s.articles[len(s.articles)-1]
		s.articles = s.articles[:len(s.articles)-1]
		s.freePages = append(s.freePages, oldest.pageNr)
	}
}

func (s *nosNewsStore) list() []*nosNewsArticle {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*nosNewsArticle, len(s.articles))
	copy(out, s.articles)
	return out
}

func (s *nosNewsStore) findByPage(pageNr int) *nosNewsArticle {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, a := range s.articles {
		if a.pageNr == pageNr {
			return a
		}
	}
	return nil
}

type nosNewsCategory struct {
	name        string
	displayName string // as shown on page 100, next to NOS logo on index page and in the story page
	feedURL     string
	indexPage   int
	firstPage   int
	lastPage    int
	maxItems    int
	indexTpl    []byte
	storyTpl    []byte
	store       *nosNewsStore

	pollMu     sync.Mutex
	lastPolled time.Time
}

// maxItems 20 is also the max number of items on the actual RSS feed
// the trailing dots in the displayName are removed on the index and story pages.
var nosNewsCategories = []*nosNewsCategory{
	{
		name:        "algemeen",
		displayName: "Laatste nieuws.",
		feedURL:     "https://feeds.nos.nl/nosnieuwsalgemeen",
		indexPage:   101,
		firstPage:   102,
		lastPage:    121,
		maxItems:    20,
		indexTpl:    nosnieuwsIndexTpl,
		storyTpl:    nosnieuwsStoryTpl,
	},
	{
		name:        "binnenland",
		displayName: "Binnenland.....",
		feedURL:     "https://feeds.nos.nl/nosnieuwsbinnenland",
		indexPage:   130,
		firstPage:   131,
		lastPage:    150,
		maxItems:    20,
		indexTpl:    nosnieuwsIndexTpl,
		storyTpl:    nosnieuwsStoryTpl,
	},
	{
		name:        "buitenland",
		displayName: "Buitenland.....",
		feedURL:     "https://feeds.nos.nl/nosnieuwsbuitenland",
		indexPage:   160,
		firstPage:   161,
		lastPage:    180,
		maxItems:    20,
		indexTpl:    nosnieuwsIndexTpl,
		storyTpl:    nosnieuwsStoryTpl,
	},
	{
		name:        "politiek",
		displayName: "Politiek.......",
		feedURL:     "https://feeds.nos.nl/nosnieuwspolitiek",
		indexPage:   200,
		firstPage:   201,
		lastPage:    220,
		maxItems:    20,
		indexTpl:    nosnieuwsIndexTpl,
		storyTpl:    nosnieuwsStoryTpl,
	},
	{
		name:        "economie",
		displayName: "Economie.......",
		feedURL:     "https://feeds.nos.nl/nosnieuwseconomie",
		indexPage:   230,
		firstPage:   231,
		lastPage:    250,
		maxItems:    20,
		indexTpl:    nosnieuwsIndexTpl,
		storyTpl:    nosnieuwsStoryTpl,
	},
	{
		name:        "opmerkelijk",
		displayName: "Opmerkelijk....",
		feedURL:     "https://feeds.nos.nl/nosnieuwsopmerkelijk",
		indexPage:   260,
		firstPage:   261,
		lastPage:    280,
		maxItems:    20,
		indexTpl:    nosnieuwsIndexTpl,
		storyTpl:    nosnieuwsStoryTpl,
	},
	{
		name:        "koningshuis",
		displayName: "Koningshuis....",
		feedURL:     "https://feeds.nos.nl/nosnieuwskoningshuis",
		indexPage:   300,
		firstPage:   301,
		lastPage:    320,
		maxItems:    20,
		indexTpl:    koningshuisIndexTpl,
		storyTpl:    koningshuisStoryTpl,
	},
	{
		name:        "cultuur",
		displayName: "Cultuur & media",
		feedURL:     "https://feeds.nos.nl/nosnieuwscultuurenmedia",
		indexPage:   330,
		firstPage:   331,
		lastPage:    350,
		maxItems:    20,
		indexTpl:    nosnieuwsIndexTpl,
		storyTpl:    nosnieuwsStoryTpl,
	},
	{
		name:        "tech",
		displayName: "Tech...........",
		feedURL:     "https://feeds.nos.nl/nosnieuwstech",
		indexPage:   360,
		firstPage:   361,
		lastPage:    380,
		maxItems:    20,
		indexTpl:    nosnieuwsIndexTpl,
		storyTpl:    nosnieuwsStoryTpl,
	},
	{
		name:        "sportalgemeen",
		displayName: "Algemeen.......",
		feedURL:     "https://feeds.nos.nl/nossportalgemeen",
		indexPage:   600,
		firstPage:   601,
		lastPage:    620,
		maxItems:    20,
		indexTpl:    nossportIndexTpl,
		storyTpl:    nossportStoryTpl,
	},
	{
		name:        "voetbal",
		displayName: "Voetbal........",
		feedURL:     "https://feeds.nos.nl/nosvoetbal",
		indexPage:   630,
		firstPage:   631,
		lastPage:    650,
		maxItems:    20,
		indexTpl:    nossportIndexTpl,
		storyTpl:    nossportStoryTpl,
	},
	{
		name:        "wielrennen",
		displayName: "Wielrennen.....",
		feedURL:     "https://feeds.nos.nl/nossportwielrennen",
		indexPage:   660,
		firstPage:   661,
		lastPage:    680,
		maxItems:    20,
		indexTpl:    nossportIndexTpl,
		storyTpl:    nossportStoryTpl,
	},
	{
		name:        "tennis",
		displayName: "Tennis.........",
		feedURL:     "https://feeds.nos.nl/nossporttennis",
		indexPage:   700,
		firstPage:   701,
		lastPage:    720,
		maxItems:    20,
		indexTpl:    nossportIndexTpl,
		storyTpl:    nossportStoryTpl,
	},
	{
		name:        "schaatsen",
		displayName: "Schaatsen......",
		feedURL:     "https://feeds.nos.nl/nossportschaatsen",
		indexPage:   730,
		firstPage:   731,
		lastPage:    750,
		maxItems:    20,
		indexTpl:    nossportIndexTpl,
		storyTpl:    nossportStoryTpl,
	},
	{
		name:        "formule1",
		displayName: "Formule 1......",
		feedURL:     "https://feeds.nos.nl/nossportformule1",
		indexPage:   760,
		firstPage:   761,
		lastPage:    780,
		maxItems:    20,
		indexTpl:    nossportIndexTpl,
		storyTpl:    nossportStoryTpl,
	},
	{
		name:        "nieuwsuur",
		displayName: "Nieuwsuur......",
		feedURL:     "https://feeds.nos.nl/nieuwsuuralgemeen",
		indexPage:   400,
		firstPage:   401,
		lastPage:    420,
		maxItems:    20,
		indexTpl:    nieuwsuurIndexTpl,
		storyTpl:    nieuwsuurStoryTpl,
	},
	{
		name:        "nosop3",
		displayName: "NOS op 3.......",
		feedURL:     "https://feeds.nos.nl/nosop3",
		indexPage:   800,
		firstPage:   801,
		lastPage:    820,
		maxItems:    20,
		indexTpl:    nosnieuwsIndexTpl,
		storyTpl:    nosnieuwsStoryTpl,
	},
	{
		name:        "jeugdjournaal",
		displayName: "Jeugdjournaal..",
		feedURL:     "https://feeds.nos.nl/jeugdjournaal",
		indexPage:   850,
		firstPage:   851,
		lastPage:    870,
		maxItems:    20,
		indexTpl:    jeugdjournaalIndexTpl,
		storyTpl:    jeugdjournaalStoryTpl,
	},
}

func init() {
	for _, c := range nosNewsCategories {
		c.store = newNosNewsStore(c.firstPage, c.lastPage)
	}
}

// Refreshes the category's feed, but only if it's actually been requested and the last poll is older than
// nosNewsPollInterval. No requests means no network traffic. The mutex makes sure a burst of concurrent
// requests only triggers one live fetch, not one each.
func (c *nosNewsCategory) pollIfStale() {
	c.pollMu.Lock()
	stale := time.Since(c.lastPolled) >= nosNewsPollInterval
	if stale {
		c.lastPolled = time.Now()
	}
	c.pollMu.Unlock()
	if !stale {
		return
	}
	c.poll()
}

func (c *nosNewsCategory) poll() {
	items, err := fetchNOSFeed(c.feedURL)
	if err != nil {
		fmt.Println("NOSNEWS: fetch error for", c.name, err)
		return
	}
	c.store.update(items, c.maxItems)
}

// --- feed fetching / parsing ---

type nosRSS struct {
	XMLName xml.Name `xml:"rss"`
	Channel struct {
		Items []struct {
			Title       string `xml:"title"`
			Link        string `xml:"link"`
			Description string `xml:"description"`
			PubDate     string `xml:"pubDate"`
			GUID        string `xml:"guid"`
		} `xml:"item"`
	} `xml:"channel"`
}

func fetchNOSFeed(url string) ([]*nosNewsArticle, error) {
	logBackgroundPoll(DirNOSNEWS, url)
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
	var feed nosRSS
	if err := xml.Unmarshal(body, &feed); err != nil {
		return nil, err
	}

	out := make([]*nosNewsArticle, 0, len(feed.Channel.Items))
	for _, it := range feed.Channel.Items {
		pubTime := parseNOSPubDate(it.PubDate)
		out = append(out, &nosNewsArticle{
			guid:       strings.TrimSpace(it.GUID),
			title:      strings.TrimSpace(it.Title),
			pubTime:    pubTime,
			dateTime:   formatNOSPubDate(pubTime, it.PubDate),
			paragraphs: parseNOSDescription(it.Description),
		})
	}
	return out, nil
}

var nosNewsMonths = []string{"jan", "feb", "mrt", "apr", "mei", "jun", "jul", "aug", "sep", "okt", "nov", "dec"}

func parseNOSPubDate(s string) time.Time {
	t, err := time.Parse("Mon, 2 Jan 2006 15:04:05 -0700", strings.TrimSpace(s))
	if err != nil {
		return time.Time{}
	}
	return t
}

// <datetime> on the story page
func formatNOSPubDate(t time.Time, raw string) string {
	if t.IsZero() {
		return strings.TrimSpace(raw)
	}
	return t.Format("2 Jan 2006 15:04:05")
}

// <shortdate> on the index page
func formatNOSShortDate(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return fmt.Sprintf("%d %s,%d:%02d", t.Day(), nosNewsMonths[t.Month()-1], t.Hour(), t.Minute())
}

// strips the HTML in a <description> CDATA block down to plain paragraphs. <h2>/<h3> blocks are flagged as
// headings; everything else (<p>, stray text) is a normal paragraph. Links and emphasis markup are made plain text
func parseNOSDescription(descHTML string) []nosNewsParagraph {
	var paras []nosNewsParagraph
	z := html.NewTokenizer(strings.NewReader(descHTML))
	var buf strings.Builder
	heading := false
	inBlock := false

	flush := func() {
		text := strings.Join(strings.Fields(buf.String()), " ")
		if text != "" {
			paras = append(paras, nosNewsParagraph{text: text, heading: heading})
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
			case "p":
				inBlock = true
				heading = false
			case "h2", "h3":
				inBlock = true
				heading = true
			case "br":
				buf.WriteString(" ")
			}
		case html.EndTagToken:
			switch tok.Data {
			case "p", "h2", "h3":
				flush()
				inBlock = false
			}
		case html.TextToken:
			if inBlock {
				buf.WriteString(tok.Data)
				buf.WriteString(" ")
			}
		}
	}
	flush()
	return paras
}

// --- text -> teletext rows ---

// nosNewsEncode maps a Dutch string onto the single-byte teletext charset
func nosNewsEncode(s string) string {
	b := make([]byte, 0, len(s))
	for _, r := range s {
		b = append(b, zdfEncodeChar(r))
	}
	return string(b)
}

// wrapTeletext  word-wraps text to width columns, hard-splitting any single "word" that doesn't fit
// on its own line
func wrapTeletext(s string, width int) []string {
	s = nosNewsEncode(strings.TrimSpace(s))
	if s == "" || width <= 0 {
		return nil
	}
	words := strings.Fields(s)
	var lines []string
	cur := ""
	for _, w := range words {
		for len(w) > width {
			if cur != "" {
				lines = append(lines, cur)
				cur = ""
			}
			lines = append(lines, w[:width])
			w = w[width:]
		}
		switch {
		case cur == "":
			cur = w
		case len(cur)+1+len(w) <= width:
			cur += " " + w
		default:
			lines = append(lines, cur)
			cur = w
		}
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return lines
}

func wrapTeletextVariable(s string, firstWidth, restWidth int) []string {
	s = nosNewsEncode(strings.TrimSpace(s))
	if s == "" || firstWidth <= 0 {
		return nil
	}
	words := strings.Fields(s)
	var lines []string
	cur := ""
	width := firstWidth
	for _, w := range words {
		for len(w) > width {
			if cur != "" {
				lines = append(lines, cur)
				cur = ""
				width = restWidth
				if len(w) <= width {
					break
				}
			}
			lines = append(lines, w[:width])
			w = w[width:]
		}
		switch {
		case cur == "":
			cur = w
		case len(cur)+1+len(w) <= width:
			cur += " " + w
		default:
			lines = append(lines, cur)
			cur = w
			width = restWidth
		}
	}
	if cur != "" {
		lines = append(lines, cur)
	}
	return lines
}

func blankTeletextRow() []byte {
	r := make([]byte, 40)
	for i := range r {
		r[i] = 0x20
	}
	return r
}

func isBlankTeletextRow(row []byte) bool {
	for _, b := range row {
		if b != 0x20 {
			return false
		}
	}
	return true
}

// scans a `rows`-row template for the first row containing the literal token text, returning the row it's on, the
// start column, and the single control byte
func findMarker(tpl []byte, rows int, token string) (row, col int, prefix byte, found bool) {
	return findMarkerInRows(tpl, 0, rows, token)
}

// findMarker restricted to rows [fromRow, toRow) - used for standalone markers like <heading> and <shortdate> that
// live outside their own dynamic content region, so they can't be confused with it
func findMarkerInRows(tpl []byte, fromRow, toRow int, token string) (row, col int, prefix byte, found bool) {
	tb := []byte(token)
	for r := fromRow; r < toRow; r++ {
		line := tpl[r*40 : (r+1)*40]
		idx := bytes.Index(line, tb)
		if idx < 0 {
			continue
		}
		p := byte(TCC_ALPHA_WHITE)
		if idx > 0 && line[idx-1] < 0x20 {
			p = line[idx-1]
		}
		return r, idx, p, true
	}
	return 0, 0, 0, false
}

// searches a single row for the first occurrence of token at or after fromCol. Used on page 100 to find the <p> belonging
// to a specific category token when a row holds more than one <name>/<p> pair - searching from just after that token's own
// end means it can only ever match its own nearest <p>, not one that belongs to a neighbour further along the row
func findMarkerInRowFrom(tpl []byte, row, fromCol int, token string) (col int, prefix byte, found bool) {
	if fromCol >= 40 {
		return 0, 0, false
	}
	line := tpl[row*40 : (row+1)*40]
	tb := []byte(token)
	idx := bytes.Index(line[fromCol:], tb)
	if idx < 0 {
		return 0, 0, false
	}
	idx += fromCol
	p := byte(TCC_ALPHA_WHITE)
	if idx > 0 && line[idx-1] < 0x20 {
		p = line[idx-1]
	}
	return idx, p, true
}

func findContentEnd(tpl []byte, rows int, startRow int) int {
	for r := startRow + 1; r < rows; r++ {
		if !isBlankTeletextRow(tpl[r*40 : (r+1)*40]) {
			return r
		}
	}
	return rows
}

// chunks a flowable stream of pre-rendered rows into pages: the first page holds up to firstCap rows, every following
// page holds up to restCap rows
func paginateRows(rows [][]byte, firstCap, restCap int) [][][]byte {
	if len(rows) == 0 || firstCap <= 0 {
		return nil
	}
	var pages [][][]byte
	remaining := rows
	cap := firstCap
	for len(remaining) > 0 {
		if cap <= 0 {
			break
		}
		n := cap
		if n > len(remaining) {
			n = len(remaining)
		}
		pages = append(pages, remaining[:n])
		remaining = remaining[n:]
		cap = restCap
	}
	return pages
}

// drops any blank separator row(s) left stranded at the very top of a page. Dropping it there  means the page ends
// with one extra blank row at the bottom instead of starting with an empty line.
func trimLeadingBlanks(pages [][][]byte) {
	for i := range pages {
		for len(pages[i]) > 0 && isBlankTeletextRow(pages[i][0]) {
			pages[i] = pages[i][1:]
		}
	}
}

// like paginateRows but keeps each group's rows together: if a group doesn't fit in whatever's left of the current
// page, the whole group moves to the next page rather than being split across the two.
func paginateGroups(groups [][][]byte, firstCap, restCap int) [][][]byte {
	if firstCap <= 0 {
		return nil
	}
	var pages [][][]byte
	var current [][]byte
	left := firstCap

	flush := func() {
		pages = append(pages, current)
		current = nil
		left = restCap
	}

	for _, g := range groups {
		if len(g) == 0 {
			continue
		}
		if len(g) > left && len(current) > 0 {
			flush()
		}
		if len(g) <= left {
			current = append(current, g...)
			left -= len(g)
			continue
		}
		// doesn't fit on a page - forced split as a last resort
		rem := g
		for len(rem) > 0 {
			if left <= 0 {
				flush()
			}
			n := left
			if n > len(rem) {
				n = len(rem)
			}
			current = append(current, rem[:n]...)
			left -= n
			rem = rem[n:]
		}
	}
	if len(current) > 0 || len(pages) == 0 {
		pages = append(pages, current)
	}
	return pages
}

func nosNewsHeaderRow() []byte {
	days := []string{"maa", "din", "woe", "don", "vri", "zat", "zon"}
	now := time.Now()
	dutchDate := fmt.Sprintf("%s %02d %s", days[(int(now.Weekday())+6)%7], now.Day(), nosNewsMonths[now.Month()-1])

	row := blankTeletextRow()
	text := fmt.Sprintf("\x02NOS NIEUWS \x03%s  %s", dutchDate, now.Format("15:04:05"))
	copy(row[7:], []byte(text))
	return row
}

func nosnewsBuildOutput(filledTpl []byte, ps, ns string) []byte {
	var out []byte
	out = append(out, []byte(ps)...)
	out = append(out, []byte(ns)...)
	out = append(out, []byte("<pre>")...)
	out = append(out, nosNewsHeaderRow()...)
	out = append(out, filledTpl...)
	out = append(out, []byte("</pre>")...)
	return out
}

func stampSubpageIndicator(buf []byte, contentEnd, sub, total int) {
	if total <= 1 {
		return
	}
	indicator := fmt.Sprintf("(%d/%d) ", sub, total)
	rowStart := contentEnd * 40
	copy(buf[rowStart+40-len(indicator):rowStart+40], []byte(indicator))
}

// --- page renderers ---

const nosNewsTemplateRows = 24
const nosNewsTopicToken = "<topic>"

func nosNewsTopicLabel(cat *nosNewsCategory) string {
	if cat.displayName != "" {
		return cat.displayName
	}
	return cat.name
}

// renders e.g. "Algemeen" as "A L G E M E E N" style used for <topic> on index pages.
// trailing dots (used on page 100) are removed first
func spreadCapsLabel(name string) string {
	name = strings.TrimRight(name, ".")
	if len(name) > 12 {
		return name
	}
	runes := []rune(strings.ToUpper(name))
	var b strings.Builder
	for i, r := range runes {
		if i > 0 {
			b.WriteByte(' ')
		}
		b.WriteRune(r)
	}
	return b.String()
}

func renderIndexPage(cat *nosNewsCategory, sub int) []byte {
	tpl := cat.indexTpl
	hRow, hCol, hPrefix, hFound := findMarker(tpl, nosNewsTemplateRows, "<headline>")
	_, pCol, pPrefix, pFound := findMarker(tpl, nosNewsTemplateRows, "<p>")
	if !hFound || !pFound {
		fmt.Println("NOSNEWS: index template for", cat.name, "is missing <headline> or <p>")
		return nosnewsBuildOutput(tpl, "", "")
	}
	contentEnd := findContentEnd(tpl, nosNewsTemplateRows, hRow)
	pColorCol := pCol - 1
	capacity := contentEnd - hRow

	// <shortdate> is optional and placed before <headline> on that same row
	const sdToken = "<shortdate>"
	sdCol, sdFound := 0, false
	if _, c, _, f := findMarkerInRows(tpl, hRow, hRow+1, sdToken); f {
		sdCol, sdFound = c, true
	}

	// The first line has less room when <shortdate> shares its row; every line after it starts
	// flush at column 1 instead of hCol, so it gets the (wider) rest-of-line width instead.
	firstWidth := pColorCol - hCol
	if firstWidth < 5 {
		firstWidth = 5
	}
	restWidth := pColorCol - 1
	if restWidth < 5 {
		restWidth = 5
	}

	// Each article is its own group: its headline lines plus the page-number line stay together as one atomic unit
	var groups [][][]byte
	for _, a := range cat.store.list() {
		lines := wrapTeletextVariable(a.title, firstWidth, restWidth)
		if len(lines) == 0 {
			continue
		}
		var block [][]byte
		for i, line := range lines {
			row := blankTeletextRow()
			switch {
			case i == 0 && sdFound:
				copy(row[:hCol], tpl[hRow*40:hRow*40+hCol])
				tokenEnd := sdCol + len(sdToken)
				if tokenEnd > hCol {
					tokenEnd = hCol
				}
				decoStart := tokenEnd
				for decoStart < hCol && tpl[hRow*40+decoStart] == 0x20 {
					decoStart++
				}
				decoration := append([]byte{}, tpl[hRow*40+decoStart:hRow*40+hCol]...)
				for k := sdCol; k < hCol; k++ {
					row[k] = 0x20
				}
				copy(row[decoStart:hCol], decoration)
				dateBytes := []byte(formatNOSShortDate(a.pubTime))
				dateEnd := sdCol + len(dateBytes)
				if dateEnd > decoStart {
					dateEnd = decoStart
				}
				copy(row[sdCol:dateEnd], dateBytes)
				copy(row[hCol:], []byte(line))
			case i == 0:
				row[0] = hPrefix
				copy(row[hCol:], []byte(line))
			default:
				// continuation lines: no <shortdate> to make room for, so start flush at column 1
				row[0] = hPrefix
				copy(row[1:], []byte(line))
			}
			if i == len(lines)-1 {
				row[pColorCol] = pPrefix
				copy(row[pCol:], []byte(fmt.Sprintf("%3d", a.pageNr)))
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

	if tRow, tCol, _, tFound := findMarker(tpl, nosNewsTemplateRows, nosNewsTopicToken); tFound {
		label := spreadCapsLabel(nosNewsTopicLabel(cat))
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

	return nosnewsBuildOutput(out, ps, ns)
}

// builds page 100
func renderMasterIndexPage(sub int) []byte {
	tpl := nosnewsMasterTpl
	out := make([]byte, len(tpl))
	copy(out, tpl)

	for _, c := range nosNewsCategories {
		token := "<" + c.name + ">"
		row, col, prefix, found := findMarker(tpl, nosNewsTemplateRows, token)
		if !found {
			fmt.Println("NOSNEWS: nosnews.bin has no", token, "row for category", c.name, "- skipping on page 100")
			continue
		}

		pCol, pPrefix, pFound := findMarkerInRowFrom(tpl, row, col+len(token), "<p>")

		label := c.displayName
		if label == "" {
			label = c.name
		}
		width := 40 - col
		if pFound {
			width = pCol - 1 - col
		}
		if width < 5 {
			width = 5
		}
		lines := wrapTeletext(label, width)

		// build on the row's current state, so a category sharing this row with an earlier one in the loop layers
		// its edit on top instead of erasing it
		line := make([]byte, 40)
		copy(line, out[row*40:(row+1)*40])

		for k := col; k < col+len(token) && k < 40; k++ {
			line[k] = 0x20 // erase the placeholder text
		}
		if len(lines) > 0 {
			copy(line[col:], []byte(lines[0]))
		}
		if col > 0 {
			line[col-1] = prefix
		}
		if pFound {
			line[pCol-1] = pPrefix
			copy(line[pCol:], []byte(fmt.Sprintf("%3d", c.indexPage)))
		}
		copy(out[row*40:(row+1)*40], line)
	}

	ps, ns, _ := getPrevNextSubpage(strconv.Itoa(nosNewsMasterPage), NavignationInfo{})
	return nosnewsBuildOutput(out, ps, ns)
}

func renderStoryPage(cat *nosNewsCategory, a *nosNewsArticle, sub int) []byte {
	tpl := cat.storyTpl
	dtRow, dtCol, dtPrefix, dtFound := findMarker(tpl, nosNewsTemplateRows, "<datetime>")
	hRow, hCol, hPrefix, hFound := findMarker(tpl, nosNewsTemplateRows, "<headline>")
	sRow, sCol, sPrefix, sFound := findMarker(tpl, nosNewsTemplateRows, "<story>")
	if !dtFound || !hFound || !sFound {
		fmt.Println("NOSNEWS: story template for", cat.name, "is missing <datetime>/<headline>/<story>")
		return nosnewsBuildOutput(tpl, "", "")
	}
	contentEnd := findContentEnd(tpl, nosNewsTemplateRows, sRow)

	const headingToken = "<heading>"
	headingPrefix := sPrefix
	hcRow, hcCol, hcPrefix, hcFound := findMarkerInRows(tpl, 0, sRow, headingToken)
	if hcFound {
		headingPrefix = hcPrefix
	}

	headlineWidth := 40 - hCol
	storyWidth := 40 - sCol

	var headlineRows [][]byte
	for _, line := range wrapTeletext(a.title, headlineWidth) {
		row := blankTeletextRow()
		row[0] = hPrefix
		copy(row[hCol:], []byte(line))
		headlineRows = append(headlineRows, row)
	}
	headlineRows = append(headlineRows, blankTeletextRow())

	buildParaRows := func(p nosNewsParagraph) [][]byte {
		prefix := sPrefix
		if p.heading {
			prefix = headingPrefix
		}
		var rows [][]byte
		for _, line := range wrapTeletext(p.text, storyWidth) {
			row := blankTeletextRow()
			row[0] = prefix
			copy(row[sCol:], []byte(line))
			rows = append(rows, row)
		}
		return rows
	}

	// Each group stays together on one subpage (paginateGroups only splits a group across subpages as a last
	// resort). A <heading> paragraph is grouped with the paragraph right after it.
	groups := [][][]byte{headlineRows}
	for i := 0; i < len(a.paragraphs); i++ {
		p := a.paragraphs[i]
		group := append(buildParaRows(p), blankTeletextRow())
		if p.heading && i+1 < len(a.paragraphs) {
			i++
			group = append(group, buildParaRows(a.paragraphs[i])...)
			group = append(group, blankTeletextRow())
		}
		groups = append(groups, group)
	}

	firstCap := contentEnd - hRow // page 1 starts at the headline row
	restCap := contentEnd - dtRow // continuation pages reuse the datetime row's slot too
	pages := paginateGroups(groups, firstCap, restCap)
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
	if hcFound {
		start := hcRow*40 + hcCol
		copy(out[start:start+len(headingToken)], bytes.Repeat([]byte{0x20}, len(headingToken)))
	}

	// <topic> on the story page: right-aligned Title Case (e.g. "Algemeen"), always ending one
	// column short of the row's true right edge (leaving column 39 blank)
	const nosNewsTopicRightMargin = 1 // trailing blank columns reserved at the row's right edge
	if tRow, tCol, _, tFound := findMarker(tpl, nosNewsTemplateRows, nosNewsTopicToken); tFound {
		label := nosNewsTopicLabel(cat)
		label = strings.TrimRight(label, ".")
		rightEnd := 40 - nosNewsTopicRightMargin - 1
		startCol := rightEnd - len(label) + 1
		if startCol < 0 {
			startCol = 0
		}
		eraseFrom := min(tCol, startCol)
		rowStart := tRow * 40
		for k := eraseFrom; k < 40; k++ {
			out[rowStart+k] = 0x20
		}
		end := rightEnd + 1
		if end > 40 {
			end = 40
		}
		copy(out[rowStart+startCol:rowStart+end], []byte(label))
	}

	for r := dtRow; r < contentEnd; r++ {
		copy(out[r*40:(r+1)*40], blankTeletextRow())
	}

	startRow := hRow
	if sub == 1 {
		dtLine := blankTeletextRow()
		dtLine[0] = dtPrefix
		copy(dtLine[dtCol:], []byte(a.dateTime))
		copy(out[dtRow*40:(dtRow+1)*40], dtLine)
	} else {
		startRow = dtRow
	}
	for i, row := range pages[sub-1] {
		r := startRow + i
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
	ps, ns, _ := getPrevNextSubpage(strconv.Itoa(a.pageNr), nav)

	return nosnewsBuildOutput(out, ps, ns)
}

// --- HTTP entry point ---

func nosnewsFindCategory(base string) (cat *nosNewsCategory, isIndex bool, pageNr int) {
	n, err := strconv.Atoi(base)
	if err != nil {
		return nil, false, 0
	}
	for _, c := range nosNewsCategories {
		if n == c.indexPage {
			return c, true, n
		}
		if n >= c.firstPage && n <= c.lastPage {
			return c, false, n
		}
	}
	return nil, false, 0
}

func nosnewsGetTeletexPage(pageNr string) bool {
	parts := strings.SplitN(pageNr, "-", 2)
	base := parts[0]
	sub := 1
	if len(parts) > 1 && parts[1] != "" {
		if v, err := strconv.Atoi(parts[1]); err == nil && v > 0 {
			sub = v
		}
	}

	if base == strconv.Itoa(nosNewsMasterPage) {
		savePage(DirNOSNEWS, pageNr, renderMasterIndexPage(sub))
		return true
	}

	cat, isIndex, n := nosnewsFindCategory(base)
	if cat == nil {
		return false
	}
	cat.pollIfStale()

	var output []byte
	if isIndex {
		output = renderIndexPage(cat, sub)
	} else {
		a := cat.store.findByPage(n)
		if a == nil {
			return false
		}
		output = renderStoryPage(cat, a, sub)
	}
	savePage(DirNOSNEWS, pageNr, output)
	return true
}
