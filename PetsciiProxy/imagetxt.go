// --- Image to Teletext data conversion ---
//
// Currently used by:
// - mtvatxt.go, serves GIF-images
// - telegazeta.go, serves PNG-images
//
// Each cell in the image has the same size, and consits of 2 colours. Each cell can be recognised exactly:
//   - text characters through a glyph table (see the specific <station>_glyphs.go files)
//   - mosaics are not in there; the are detected by sampling their 2x3 blocks
//   - double height characters by reading the cell over two rows, taking every other pixel line
//   - double width characters by reading two cells, taking every other pixel column
//   - double size characters by doing both
//
// Note:
// - Double width and double size are Teletext level 2.5 spec; I currently support level 1.5, which is the default level for
//   most Teletext services. Double width becomes normal text and double size becomes double height!
// - Teletext 64 Ultimate misuses double width and double size control codes for orange text/mosiacs (also not level 1.5 off course)
//   The reason for this initially was the representation of the forum64.de logo, which has orange in it.
//
// Copyright (C) 2026 Frank Putman
// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	imgTxtCols     = 40
	imgTxtRows     = 25
	imgTxtMaxCellW = 16
	imgTxtMaxCellH = 16
)

// describes how a service draws its Teletext pages
type imgTxtProfile struct {
	name         string // name used for logging
	cellW, cellH int    // pixels per cell
	firstRow     int    // teletext row of the first pixel row (0, or 1 if the image has no header row)
	rows         int    // number of teletext rows in the image

	// Pixel ranges of the 2x3 mosaic blocks: x for left/right, y for top/middle/bottom
	mosaicX, separatedX [2][2]int
	mosaicY, separatedY [3][2]int

	glyphs          map[imgTxtGlyphKey]byte // shape -> byte sent to the client
	glyphTableName  string                  // name used for logging lines
	doubleWidthUsed bool                    // also look for double width and double size characters

	loggedMu sync.Mutex
	logged   map[string]bool // unknown cell pictures already logged
}

func imgTxtFetch(name, url string) ([]byte, int, bool) {
	client := http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		fmt.Println(name+": connection error:", err)
		return nil, 0, false
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		fmt.Println(name+": HTTP error, status", resp.StatusCode, "for", url)
		return nil, resp.StatusCode, true
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Println(name+": read error:", err)
		return nil, resp.StatusCode, false
	}
	return body, resp.StatusCode, true
}

// --- Glyph table ---

// this is a picture of a single character: cellH lines of cellW '#' or '.' characters
type imgTxtGlyph struct {
	char rune
	art  string
}

// this is a cell shape: a number per pixel line, the leftmost pixel in the highest bit used
type imgTxtGlyphKey [imgTxtMaxCellH]uint16

// Turns the pictures into a lookup table
// It stops the program at startup when a picture is malformed or when two characters have the same picture.
func imgTxtBuildGlyphTable(tableName string, glyphs []imgTxtGlyph, w, h int, toByte func(rune) byte) map[imgTxtGlyphKey]byte {
	table := make(map[imgTxtGlyphKey]byte, len(glyphs))
	seen := make(map[imgTxtGlyphKey]rune, len(glyphs))
	for _, g := range glyphs {
		key, err := imgTxtParseGlyphArt(g.art, w, h)
		if err != nil {
			panic(fmt.Sprintf("%s: character %q: %v", tableName, g.char, err))
		}
		if other, dup := seen[key]; dup {
			panic(fmt.Sprintf("%s: characters %q and %q have the same picture!", tableName, other, g.char))
		}
		seen[key] = g.char
		table[key] = toByte(g.char)
	}
	return table
}

// converts a picture of h lines of w '#' or '.' chars into a key
func imgTxtParseGlyphArt(art string, w, h int) (imgTxtGlyphKey, error) {
	var key imgTxtGlyphKey
	lines := strings.Split(strings.TrimSpace(art), "\n")
	if len(lines) != h {
		return key, fmt.Errorf("picture has %d lines instead of %d", len(lines), h)
	}
	for y, line := range lines {
		line = strings.TrimSpace(line)
		if len(line) != w {
			return key, fmt.Errorf("line %d has %d characters instead of %d", y+1, len(line), w)
		}
		for x := 0; x < w; x++ {
			switch line[x] {
			case '#':
				key[y] |= 1 << (w - 1 - x)
			case '.':
			default:
				return key, fmt.Errorf("line %d has %q; only '#' and '.' are allowed", y+1, line[x])
			}
		}
	}
	return key, nil
}

// --- Pixels ---

// holds the image as teletext colour numbers, one byte per pixel
type imgTxtPixels struct {
	w, h int
	p    []byte
}

// one cell's pixels as colour numbers; only the first cellH lines and cellW columns are used!
type imgTxtCellPixels [imgTxtMaxCellH][imgTxtMaxCellW]byte

// Teletext colour control codes: 0 black, 1 red, 2 green, 3 yellow, 4 blue, 5 magenta, 6 cyan, 7 white
func imgTxtColourNumber(c color.Color) byte {
	r, g, b, _ := c.RGBA()
	var n byte
	if r >= 0x8000 {
		n |= 1
	}
	if g >= 0x8000 {
		n |= 2
	}
	if b >= 0x8000 {
		n |= 4
	}
	return n
}

func imgTxtImagePixels(img image.Image) imgTxtPixels {
	bounds := img.Bounds()
	px := imgTxtPixels{w: bounds.Dx(), h: bounds.Dy()}
	px.p = make([]byte, px.w*px.h)
	if pal, ok := img.(*image.Paletted); ok {
		// check how the decoded image stores its pixels, with each pixel pointing to the palette colour table
		// always the case for GIFs and PNGs in palette mode; lookup table turns it into the Teletext colour
		// MTVA + Telegazeta use this fast route
		lut := make([]byte, 256)
		for i, c := range pal.Palette {
			lut[i] = imgTxtColourNumber(c)
		}
		for y := 0; y < px.h; y++ {
			for x := 0; x < px.w; x++ {
				px.p[y*px.w+x] = lut[pal.ColorIndexAt(bounds.Min.X+x, bounds.Min.Y+y)]
			}
		}
		return px
	}
	// the slow route (>100k conversions) for images saved in full colour (currently not used)
	for y := 0; y < px.h; y++ {
		for x := 0; x < px.w; x++ {
			// img.At returns pixel colour and gets converted
			px.p[y*px.w+x] = imgTxtColourNumber(img.At(bounds.Min.X+x, bounds.Min.Y+y))
		}
	}
	return px
}

// returns the pixels of the cell in pixel row ir, column col. Double height reads over this row and the next one,
// skipping every other pixel line; double width reads this cell and the next one, skipping every other pixel column.
// NB: Both give back the normal size shape
func (p *imgTxtProfile) cell(px *imgTxtPixels, ir, col int, dh, dw bool) imgTxtCellPixels {
	var c imgTxtCellPixels
	stepY, stepX := 1, 1
	if dh {
		stepY = 2
	}
	if dw {
		stepX = 2
	}
	for y := 0; y < p.cellH; y++ {
		line := (ir*p.cellH + y*stepY) * px.w
		for x := 0; x < p.cellW; x++ {
			c[y][x] = px.p[line+col*p.cellW+x*stepX]
		}
	}
	return c
}

// --- Decoding ---

const (
	imgCellSolid     = iota // one colour cell (fg == bg)
	imgCellText             // character from the glyph table
	imgCellMosaic           // contiuous mosaic
	imgCellSeparated        // separated mosaic
	imgCellUnknown          // will be handled as a solid cell in its main colour
)

type imgTxtCell struct {
	kind   int
	fg, bg byte
	ch     byte // text: the output byte; mosaics: the sextant bits (bit 0 top left .. bit 5 bottom right)
	dh     bool // double height?
}

// a cell the decoder could not recognise
type imgTxtUnknownCell struct {
	row, col int
	pixels   imgTxtCellPixels // at normal size, also for a double height cell
}

// turns the image into a 25 row teletext page buffer
func imgTxtDecodeImage(img image.Image, p *imgTxtProfile) ([][]byte, []imgTxtUnknownCell) {
	px := imgTxtImagePixels(img)
	if px.w != imgTxtCols*p.cellW || px.h != p.rows*p.cellH {
		return nil, nil
	}
	pageBuffer := newPageBuffer(imgTxtRows)
	var unknown []imgTxtUnknownCell
	for ir := 0; ir < p.rows; ir++ {
		row := ir + p.firstRow
		cells := make([]imgTxtCell, imgTxtCols)
		for col := range cells {
			cells[col] = p.classifyCell(p.cell(&px, ir, col, false, false))
		}
		// Double height: these are only rows 1-22
		dhStart := -1
		if row >= 1 && row <= imgTxtRows-3 && ir+1 < p.rows {
			dhStart = p.doubleHeightStart(&px, ir, cells)
		}
		for col := 0; col < imgTxtCols; col++ {
			if dhStart >= 0 && col >= dhStart {
				c := p.classifyCell(p.cell(&px, ir, col, true, false))
				if c.kind != imgCellUnknown {
					c.dh = true
					cells[col] = c
					continue
				}
				// double size: shown as a double height character plus space
				if c, ok := p.doubleWidthCell(&px, ir, col, true); ok {
					c.dh = true
					cells[col] = c
					cells[col+1] = imgTxtCell{kind: imgCellSolid, fg: c.bg, bg: c.bg, dh: true}
					col++
					continue
				}
				// otherwise it may be a normal height cell after a normal height code: keep that
				if cells[col].kind == imgCellUnknown {
					unknown = append(unknown, imgTxtUnknownCell{row, col, p.cell(&px, ir, col, true, false)})
					continue
				}
			}
			if cells[col].kind == imgCellUnknown {
				// double width: shown as a normal character plus space
				if c, ok := p.doubleWidthCell(&px, ir, col, false); ok {
					cells[col] = c
					cells[col+1] = imgTxtCell{kind: imgCellSolid, fg: c.bg, bg: c.bg}
					col++
					continue
				}
				unknown = append(unknown, imgTxtUnknownCell{row, col, p.cell(&px, ir, col, false, false)})
			}
		}
		copy(pageBuffer[row], p.encodeRow(cells))
		if dhStart >= 0 {
			// The row below a double height row is hidden by the teletext decoder; left empty
			ir++
		}
	}
	return pageBuffer, unknown
}

// tries to read a double width character (or with dh set: a double size character) from this cell and the next one
func (p *imgTxtProfile) doubleWidthCell(px *imgTxtPixels, ir, col int, dh bool) (imgTxtCell, bool) {
	if !p.doubleWidthUsed || col+1 >= imgTxtCols {
		return imgTxtCell{}, false
	}
	c := p.classifyCell(p.cell(px, ir, col, dh, true))
	// Double width mosaics are already recognised as two normal mosaics, so only text counts here
	return c, c.kind == imgCellText
}

// first column of a double height row, or -1 for a normal row. A cell is double height (or double size)
// when it can't be recognised at normal size, but it can be when read over this row and the next one.
func (p *imgTxtProfile) doubleHeightStart(px *imgTxtPixels, ir int, cells []imgTxtCell) int {
	for col, c := range cells {
		if c.kind != imgCellUnknown {
			continue
		}
		if _, ok := p.doubleWidthCell(px, ir, col, false); ok {
			continue // double width, normal height
		}
		dh := p.classifyCell(p.cell(px, ir, col, true, false))
		if dh.kind != imgCellUnknown && dh.kind != imgCellSolid {
			return col
		}
		if _, ok := p.doubleWidthCell(px, ir, col, true); ok {
			return col // double size
		}
	}
	return -1
}

func (p *imgTxtProfile) classifyCell(c imgTxtCellPixels) imgTxtCell {
	var count [8]int
	for y := 0; y < p.cellH; y++ {
		for x := 0; x < p.cellW; x++ {
			count[c[y][x]&7]++
		}
	}
	var colours []byte
	main := byte(0)
	for n := byte(0); n < 8; n++ {
		if count[n] > 0 {
			colours = append(colours, n)
		}
		if count[n] > count[main] {
			main = n
		}
	}
	if len(colours) == 1 {
		return imgTxtCell{kind: imgCellSolid, fg: main, bg: main}
	}
	unknown := imgTxtCell{kind: imgCellUnknown, fg: main, bg: main}
	if len(colours) > 2 {
		return unknown
	}
	a, b := colours[0], colours[1]

	// Text: try both colours as foreground
	for _, fg := range [2]byte{a, b} {
		var key imgTxtGlyphKey
		for y := 0; y < p.cellH; y++ {
			for x := 0; x < p.cellW; x++ {
				if c[y][x] == fg {
					key[y] |= 1 << (p.cellW - 1 - x)
				}
			}
		}
		if ch, ok := p.glyphs[key]; ok {
			bg := a
			if fg == a {
				bg = b
			}
			return imgTxtCell{kind: imgCellText, fg: fg, bg: bg, ch: ch}
		}
	}

	// Continuous mosaic: every block one colour
	if bits, ok := p.sextants(c, a, p.mosaicX, p.mosaicY); ok {
		return imgTxtCell{kind: imgCellMosaic, fg: a, bg: b, ch: bits}
	}

	// separated: gaps around the blocks in the background colour
	for _, bg := range [2]byte{a, b} {
		fg := a
		if bg == a {
			fg = b
		}
		bits, ok := p.sextants(c, fg, p.separatedX, p.separatedY)
		if !ok {
			continue
		}
		inBlock := func(x, y int) bool {
			inX := (x >= p.separatedX[0][0] && x < p.separatedX[0][1]) || (x >= p.separatedX[1][0] && x < p.separatedX[1][1])
			inY := false
			for _, yr := range p.separatedY {
				inY = inY || (y >= yr[0] && y < yr[1])
			}
			return inX && inY
		}
		gapsOK := true
		for y := 0; y < p.cellH && gapsOK; y++ {
			for x := 0; x < p.cellW; x++ {
				if !inBlock(x, y) && c[y][x] != bg {
					gapsOK = false
					break
				}
			}
		}
		if gapsOK {
			return imgTxtCell{kind: imgCellSeparated, fg: fg, bg: bg, ch: bits}
		}
	}
	return unknown
}

// check that each block is single colour, and return a bit per block that has colour fg
func (p *imgTxtProfile) sextants(c imgTxtCellPixels, fg byte, xs [2][2]int, ys [3][2]int) (byte, bool) {
	var bits byte
	// outer loops is only 3 block rows * 2 block columns
	for by, yr := range ys {
		for bx, xr := range xs {
			// inner loops walks pixels inside the block
			v := c[yr[0]][xr[0]]
			for y := yr[0]; y < yr[1]; y++ {
				for x := xr[0]; x < xr[1]; x++ {
					if c[y][x] != v {
						return 0, false
					}
				}
			}
			if v == fg {
				bits |= 1 << (by*2 + bx)
			}
		}
	}
	return bits, true
}

// sextant bits to the teletext mosaic character (0x20-0x3F, 0x60-0x7F)
func imgTxtMosaicChar(bits byte) byte {
	return 0x20 | (bits & 0x1F) | (bits&0x20)<<1
}

// --- Unrecognised cells logging---

// a two colour cell is logged as a ready to use glyph table entry
func (p *imgTxtProfile) logUnknownCells(name string, cells []imgTxtUnknownCell) {
	if len(cells) == 0 {
		return
	}
	fmt.Println(p.name+":", len(cells), "unrecognised cell(s) on", name)
	p.loggedMu.Lock()
	defer p.loggedMu.Unlock()
	if p.logged == nil {
		p.logged = map[string]bool{}
	}
	for _, c := range cells {
		entry, ok := p.glyphArtEntry(c.pixels)
		if !ok || p.logged[entry] {
			continue
		}
		p.logged[entry] = true
		fmt.Printf("%s: unrecognised cell on %s, row %d column %d. If it is a character, add it to %s with the right character instead of '?':\n%s\n",
			p.name, name, c.row, c.col, p.glyphTableName, entry)
	}
}

// format the cell as a glyph table entry. The less frequent colour is drawn as '#'
func (p *imgTxtProfile) glyphArtEntry(c imgTxtCellPixels) (string, bool) {
	var count [8]int
	for y := 0; y < p.cellH; y++ {
		for x := 0; x < p.cellW; x++ {
			count[c[y][x]&7]++
		}
	}
	var colours []byte
	for n := byte(0); n < 8; n++ {
		if count[n] > 0 {
			colours = append(colours, n)
		}
	}
	if len(colours) != 2 {
		// cell doesn't have exactly two colours!
		return "", false
	}
	fg := colours[0]
	if count[colours[1]] < count[fg] {
		fg = colours[1]
	}
	var sb strings.Builder
	sb.WriteString("\t{'?', `\n")
	for y := 0; y < p.cellH; y++ {
		for x := 0; x < p.cellW; x++ {
			if c[y][x] == fg {
				sb.WriteByte('#')
			} else {
				sb.WriteByte('.')
			}
		}
		if y < p.cellH-1 {
			sb.WriteByte('\n')
		}
	}
	sb.WriteString("`},")
	return sb.String(), true
}

// --- Row encoding ---

// A cell holds either a character or a control code. The encoder walks the row left to right and puts the control codes needed
// for the next character in the blank cells just before it.
// Those cells are where the original page had its control codes as well, so normally everything should fit. And if not, the least
// important codes are left out.

type imgTxtEncoder struct {
	out    []byte
	fg, bg byte
	mosaic bool
	sep    bool
	dh     bool
	free   []int // blank cells since the last character or control code, usable for control codes
	nbAt   int   // last New Background placed among the free cells: cells before it can't take colour codes
}

// control codes needed before a character
type imgTxtCodes struct {
	fg, bg, size, sep []byte
	fgKeepBg          []byte // foreground code needed when the background isn't changed (no room)
}

// put control codes in blank cells if possible
func (p *imgTxtProfile) encodeRow(cells []imgTxtCell) []byte {
	e := &imgTxtEncoder{out: bytes.Repeat([]byte{' '}, imgTxtCols), fg: 7, nbAt: -1}
	for i := 0; i < len(cells); i++ {
		c := cells[i]
		if c.kind != imgCellSolid && c.kind != imgCellUnknown {
			nextIsMosaic := i+1 < len(cells) && (cells[i+1].kind == imgCellMosaic || cells[i+1].kind == imgCellSeparated)
			e.put(i, c, nextIsMosaic)
			continue
		}
		u := c.bg
		switch {
		// same colour as the current background? a blank cell, kept free for later control codes
		case u == e.bg:
			e.free = append(e.free, i)
		// Mosaic mode in this colour and no text on it follows?
		case e.mosaic && !e.sep && u == e.fg && !imgTxtTextOnRun(cells, i, u):
			e.out[i] = 0x7F // write full block
			e.free = e.free[:0]
		// part of a mosaic graphic, or the background returns after it?
		case imgTxtSolidIsMosaic(cells, i, u, e.bg):
			e.put(i, imgTxtCell{kind: imgCellMosaic, fg: u, bg: e.bg, ch: 0x3F, dh: c.dh}, false)
		// foreground already has this colour?
		case u == e.fg && u != 0:
			e.out[i] = TCC_NEW_BACKGROUND // New Background alone switches the background here
			e.bg = u
			e.nbAt = i
		// black?
		case u == 0:
			e.out[i] = TCC_BLACK_BACKGROUND // Black Background alone switches the background here
			e.bg = 0
			e.free = e.free[:0]
		// other colour with a free cell before it?
		case len(e.free) > 0:
			e.out[e.free[len(e.free)-1]] = e.colourCode(u, e.mosaic) // apply colour code in prev.  cell
			e.out[i] = TCC_NEW_BACKGROUND                            // and New Background here
			e.fg, e.bg = u, u
			e.free = e.free[:0]
		// // no free cell, but next one has the same colour?
		case i+1 < len(cells) && cells[i+1].kind == imgCellSolid && cells[i+1].bg == u:
			// colour code here, New Background there
			e.out[i] = e.colourCode(u, e.mosaic)
			e.out[i+1] = TCC_NEW_BACKGROUND
			e.fg, e.bg = u, u
			e.free = e.free[:0]
			i++
		// lone cell of another colour with no room for codes?
		default:
			e.free = append(e.free, i) // left in the current background
		}
	}
	return e.out
}

// tells if a run of solid cells in colour u is followed by text on that colour. Then the run is the start of that
// background, and is better written as a background change than as full mosaic blocks.
func imgTxtTextOnRun(cells []imgTxtCell, i int, u byte) bool {
	j := i + 1
	for j < len(cells) && cells[j].kind == imgCellSolid && cells[j].bg == u {
		j++
	}
	return j < len(cells) && cells[j].kind == imgCellText && cells[j].bg == u
}

// this decides if a run of solid cells in colour u is better written as full mosaic blocks than as a background change.
// That's the case when the run belongs to a mosaic graphic in that colour, or when the current background returns right
// after it. Note: a background change would then need codes before and after the run, a mosaic colour only before it.
func imgTxtSolidIsMosaic(cells []imgTxtCell, i int, u, bg byte) bool {
	for j := i + 1; j < len(cells); j++ {
		c := cells[j]
		if c.kind == imgCellSolid && c.bg == u {
			continue
		}
		if c.kind == imgCellSolid || c.kind == imgCellUnknown {
			return c.bg == bg
		}
		return c.kind == imgCellMosaic && ((c.fg == u && c.bg == bg) || (c.bg == u && c.fg == bg))
	}
	return true
}

func (e *imgTxtEncoder) colourCode(colour byte, mosaic bool) byte {
	if mosaic {
		return TCC_MOSAIC_BLACK + colour
	}
	return TCC_ALPHA_BLACK + colour
}

// returns control codes needed before cell c
func (e *imgTxtEncoder) codesFor(c imgTxtCell, nextIsMosaic bool) imgTxtCodes {
	var codes imgTxtCodes
	alpha := c.kind == imgCellText
	if c.dh && !e.dh {
		codes.size = []byte{TCC_DOUBLE_HEIGHT}
	} else if !c.dh && e.dh {
		codes.size = []byte{TCC_NORMAL_HEIGHT}
	}
	// Chars 0x40-0x5F show as text in mosaic mode too. Use that for such a character between mosaics, or when there is no room to switch to text mode
	mosaicMode := !alpha
	if alpha && c.ch >= 0x40 && c.ch <= 0x5F && (nextIsMosaic || (e.mosaic && len(e.free) == 0)) {
		mosaicMode = true
	}
	curFg, curMosaic := e.fg, e.mosaic
	if c.bg != e.bg {
		switch {
		case c.bg == 0:
			codes.bg = []byte{TCC_BLACK_BACKGROUND}
		case c.bg == e.fg:
			codes.bg = []byte{TCC_NEW_BACKGROUND} // the foreground already has the right colour
		default:
			codes.bg = []byte{e.colourCode(c.bg, mosaicMode), TCC_NEW_BACKGROUND}
			curFg, curMosaic = c.bg, mosaicMode
		}
	}
	if c.fg != curFg || curMosaic != mosaicMode {
		codes.fg = []byte{e.colourCode(c.fg, mosaicMode)}
	}
	codes.fgKeepBg = codes.fg
	if len(codes.bg) > 0 {
		codes.fgKeepBg = nil
		if c.fg != e.fg || e.mosaic != mosaicMode {
			codes.fgKeepBg = []byte{e.colourCode(c.fg, mosaicMode)}
		}
	}
	if !alpha && e.sep != (c.kind == imgCellSeparated) {
		if c.kind == imgCellSeparated {
			codes.sep = []byte{TCC_SEPERATED_MOSAICS}
		} else {
			codes.sep = []byte{TCC_CONTINUOUS_MOSAICS}
		}
	}
	return codes
}

// A cell that is left half white and right half blue can be a white block on blue background or vice versa (inverted); this
// makes no difference in how it looks on screen. Here we calculate how many control code each a variant needs.
func (e *imgTxtEncoder) cost(c imgTxtCell) int {
	codes := e.codesFor(c, false)
	return len(codes.fg) + len(codes.bg) + len(codes.size) + len(codes.sep)
}

// writes character cell c at position i, preceded by needed control codes
func (e *imgTxtEncoder) put(i int, c imgTxtCell, nextIsMosaic bool) {
	if c.kind == imgCellMosaic {
		// A contiguous mosaic can be drawn either way round
		inverse := c
		inverse.fg, inverse.bg, inverse.ch = c.bg, c.fg, c.ch^0x3F
		if e.cost(inverse) < e.cost(c) {
			c = inverse
		}
	}
	codes := e.codesFor(c, nextIsMosaic)

	// Keep the most important codes that fit in the free cells: foreground, background, size, separation.
	// Colour codes can't go before a New Background that's among the free cells.
	room := len(e.free)
	colourRoom := 0
	for _, f := range e.free {
		if f > e.nbAt {
			colourRoom++
		}
	}
	var keep imgTxtCodes
	used, usedColour := 0, 0
	try := func(group []byte, colour bool) []byte {
		if len(group) == 0 || used+len(group) > room || (colour && usedColour+len(group) > colourRoom) {
			return nil
		}
		used += len(group)
		if colour {
			usedColour += len(group)
		}
		return group
	}
	keep.fg = try(codes.fg, true)
	keep.bg = try(codes.bg, true)
	if len(codes.bg) > 0 && keep.bg == nil {
		// No room to change the background: the foreground code (if any) must then suit the current state,
		// so the next characters don't need it again
		used, usedColour = 0, 0
		keep.fg = try(codes.fgKeepBg, true)
		codes.fg = codes.fgKeepBg
	}
	keep.size = try(codes.size, false)
	keep.sep = try(codes.sep, false)

	if used == 0 && len(codes.fg) > 0 {
		if c.kind == imgCellText && e.mosaic && nextIsMosaic {
			// A character that can't be shown in mosaic mode, squeezed between mosaics: switching to
			// text and back would cost two cells, so only this one is left blank
			e.free = append(e.free, i)
			return
		}
		// No room for the colour code: it takes the place of this character
		e.apply(codes.fg[0])
		e.out[i] = codes.fg[0]
		e.free = e.free[:0]
		return
	}
	// Size and separation codes first, the colour codes in the last free cells (after any New Background)
	var ordered []byte
	ordered = append(ordered, keep.size...)
	ordered = append(ordered, keep.sep...)
	ordered = append(ordered, keep.bg...)
	ordered = append(ordered, keep.fg...)
	slots := e.free[len(e.free)-len(ordered):]
	for k, code := range ordered {
		e.out[slots[k]] = code
		e.apply(code)
	}
	if c.kind == imgCellText {
		e.out[i] = c.ch
	} else {
		e.out[i] = imgTxtMosaicChar(c.ch)
	}
	e.free = e.free[:0]
}

// updates encoder state for a control code
func (e *imgTxtEncoder) apply(code byte) {
	switch {
	case code <= TCC_ALPHA_WHITE:
		e.fg, e.mosaic = code, false
	case code >= TCC_MOSAIC_BLACK && code <= TCC_MOSAIC_WHITE:
		e.fg, e.mosaic = code-TCC_MOSAIC_BLACK, true
	case code == TCC_NEW_BACKGROUND:
		e.bg = e.fg
	case code == TCC_BLACK_BACKGROUND:
		e.bg = 0
	case code == TCC_DOUBLE_HEIGHT:
		e.dh = true
	case code == TCC_NORMAL_HEIGHT:
		e.dh = false
	case code == TCC_CONTINUOUS_MOSAICS:
		e.sep = false
	case code == TCC_SEPERATED_MOSAICS:
		e.sep = true
	}
}
