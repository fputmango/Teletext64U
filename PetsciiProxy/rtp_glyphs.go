// Glyph table for rtp.go
//
// RTP PNG images are the same as Telegazeta, Antena 3 and La Sexta (all FAB Teletext system based), so we can reuse a lot of existing code
//
// The decoder uses:
// - tlgzGlyphArt (telegazeta_glyphs.go)
// - atrGlyphArt (atresmedia_glyphs.go)
// - and these additional characters here below
//
// Copyright (C) 2026 Frank Putman
// SPDX-License-Identifier: GPL-3.0-or-later

package main

var rtpGlyphArt = []imgTxtGlyph{
	// a with tilde
	{'ã', `
............
...###...#..
..##.##.##..
..#...###...
............
..#######...
.###...###..
........##..
..########..
.##.....##..
.###...####.
..#########.
............
............`},
	// A with tilde
	{'Ã', `
...####..##.
..##.#####..
.....###....
...######...
..########..
.###....###.
.##......##.
.##########.
.##########.
.##......##.
.##......##.
.##......##.
............
............`},
	// A with grave
	{'À', `
...###......
....###.....
............
...######...
..########..
.###....###.
.##......##.
.##########.
.##########.
.##......##.
.##......##.
.##......##.
............
............`},
	// o with tilde
	{'õ', `
............
...###...#..
..##.##.##..
..#...###...
............
...#######..
..#########.
..##.....##.
..##.....##.
..##.....##.
..#########.
...#######..
............
............`},
	// arrow right (replaced by > for the client)
	{'→', `
............
............
.....#......
.....###....
.....####...
......####..
###########.
###########.
......####..
.....####...
.....##.....
.....#......
............
............`},
	// E with circumflex
	{'Ê', `
....####....
...##..##...
..#########.
..#########.
..##........
..##........
..#######...
..#######...
..##........
..##........
..#########.
..#########.
............
............`},
	// a with circumflex
	{'â', `
............
.....#......
....###.....
...##.##....
............
..#######...
.###...###..
........##..
..########..
.##.....##..
.###...####.
..#########.
............
............`},
	// o with circumflex
	{'ô', `
............
......#.....
.....###....
....##.##...
............
...#######..
..#########.
..##.....##.
..##.....##.
..##.....##.
..#########.
...#######..
............
............`},
	// O with tilde
	{'Õ', `
...####..##.
..##..####..
............
...#######..
..#########.
..##.....##.
..##.....##.
..##.....##.
..##.....##.
..##.....##.
..#########.
...#######..
............
............`},
	// A with circumflex
	{'Â', `
...######...
..###..###..
............
...######...
..########..
.###....###.
.##......##.
.##########.
.##########.
.##......##.
.##......##.
.##......##.
............
............`},
}

// Portuguese and Spanish share the same national option subset (ETS 300 706, table 36). Info on how this works:
// https://www.etsi.org/deliver/etsi_i_ets/300700_300799/300706/01_60/ets_300706e01p.pdf
var rtpSubset = map[rune]byte{
	'ç': 0x23, '¡': 0x40, 'á': 0x5B, 'é': 0x5C, 'í': 0x5D, 'ó': 0x5E, 'ú': 0x5F, '¿': 0x60, 'ü': 0x7B, 'ñ': 0x7C, 'è': 0x7D, 'à': 0x7E,
}

// Characters outside the subset are sent as ISO-8859-1 (like the other services do). Teletext 64 Ultimate maps each of these bytes
// to a slot of the Portuguese G2 bank (teletext-bold-pt-g2.bin / teletext-thin-pt-g2.bin).
var rtpLatin1 = map[rune]byte{
	'Á': 0xC1, 'É': 0xC9, 'Í': 0xCD, 'Ó': 0xD3, 'Ú': 0xDA, 'Ã': 0xC3, 'Ç': 0xC7, 'Õ': 0xD5, 'À': 0xC0, 'Â': 0xC2, 'Ê': 0xCA, 'Ô': 0xD4,
	'ã': 0xE3, 'õ': 0xF5, 'â': 0xE2, 'ê': 0xEA, 'ô': 0xF4,
	'º': 0xBA, 'ª': 0xAA, '°': 0xB0,
	'€': 0x80, // the client has the euro in the same slot of the default G2!
	'@': 0xA4, // 0x40 is ¡ in the national subset, so the '@' gets its own byte
}

// These characters can't be shown. They are mapped to something similar
var rtpReplacements = map[rune]byte{
	'Ñ': 'N', 'Ü': 'U', 'Ä': 'A', 'Ö': 'O', 'ä': 'a', 'ö': 'o',
	'“': '"', '”': '"', '„': '"', '«': '"', '»': '"', '‘': '\'', '’': '\'', '–': '-', '—': '-', '…': '.', '•': '*', '→': '>',
	// ASCII characters whose position holds a national letter
	'#': '*', '_': '-', '[': '(', ']': ')', '{': '(', '}': ')', '|': '/', '\\': '/', '~': '-', '`': '\'', '^': '\'',
}

func rtpRuneToByte(r rune) byte {
	if b, ok := rtpSubset[r]; ok {
		return b
	}
	if b, ok := rtpLatin1[r]; ok {
		return b
	}
	if b, ok := rtpReplacements[r]; ok {
		return b
	}
	if b, ok := atrReplacements[r]; ok { // Polish letters and other look-alikes
		return b
	}
	if r >= 0x20 && r < 0x7F {
		return byte(r)
	}
	return '?'
}
