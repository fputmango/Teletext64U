package main

import (
	"bytes"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

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
	"DW":       TCC_DOUBLE_WIDTH,
	"DS":       TCC_DOUBLE_SIZE,
	"BB":       TCC_BLACK_BACKGROUND,
	"Conceal":  TCC_CONCEAL,
	"SB":       TCC_STARTBOX,
}

// matches 3-digit candidates from 100 to 899
var candidateBytesRegex = regexp.MustCompile(`[1-8]\d{2}`)

// This looks for embedded page-number references / page links and returns a list of 'lnk=nnn,rr,cc,h' strings (if any)
func scanPageLinks(body []byte) string {
	var pageLinks strings.Builder
	rowCount := len(body) / 40

	for row := 1; row < rowCount; row++ { // skip row 0 because no page links in the header/date row
		rowBytes := body[row*40 : row*40+40]
		indexes := candidateBytesRegex.FindAllIndex(rowBytes, -1)

		// Double-height tracking: This info will be added as a 0 or 1 value in the lnk= line at the end. This info is
		// used by Teletext64U to determine if it has to highlight a single or double row pagenumber.
		doubleheightAtCol := [40]bool{}
		doubleheight := false
		for col := 0; col < 40; col++ {
			doubleheightAtCol[col] = doubleheight
			switch rowBytes[col] {
			case TCC_NORMAL_HEIGHT:
				doubleheight = false
			case TCC_DOUBLE_HEIGHT:
				doubleheight = true
			}
		}

		for _, loc := range indexes {
			startIdx := loc[0]
			endIdx := loc[1]
			col := startIdx

			// No valid page numbers beyond column 37
			if col > 37 {
				continue
			}

			candidate := rowBytes[startIdx:endIdx]

			validPrefix := false
			if col == 0 {
				validPrefix = true
			} else {
				prev := rowBytes[startIdx-1]
				if prev == ' ' || prev == 'p' || prev == 'P' || prev == '>' || prev == '-' || prev == ',' || prev == '.' || prev == '/' || prev < 32 {
					validPrefix = true
				}
			}

			if !validPrefix {
				continue
			}

			validSuffix := false
			if col == 37 {
				validSuffix = true
			} else {
				next := rowBytes[endIdx]
				if next == ' ' || next == ',' || next == '.' || next == '-' || next == '/' || next == '\n' || next < 32 {
					validSuffix = true
				}
				if next == ',' || next == '.' {
					// a space or teletext control code after the . / , is valid
					if rowBytes[endIdx+1] == ' ' || rowBytes[endIdx+1] < 0x20 {
						validSuffix = true
					} else {
						if endIdx+4 > len(rowBytes) {
							validSuffix = false
						} else {
							followingBytes := rowBytes[endIdx+1 : endIdx+4]
							val, err := strconv.Atoi(string(followingBytes))
							if err != nil || val < 100 || val > 899 {
								validSuffix = false
							} else {
								// Ensure the 3 digits are NOT part of a longer number like 2590 in 363,2590
								if endIdx+4 < len(rowBytes) {
									charAfterNext := rowBytes[endIdx+4]
									if charAfterNext >= '0' && charAfterNext <= '9' {
										validSuffix = false
									}
								}
							}
						}
					}
				}
				if next == '/' {
					validSlash := false
					if endIdx+1 < len(rowBytes) && rowBytes[endIdx+1] >= '0' && rowBytes[endIdx+1] <= '9' {
						// Count every consecutive digit after the slash (no cap).
						digitEnd := endIdx + 1
						for digitEnd < len(rowBytes) && rowBytes[digitEnd] >= '0' && rowBytes[digitEnd] <= '9' {
							digitEnd++
						}
						digitCount := digitEnd - (endIdx + 1)
						val, err := strconv.Atoi(string(rowBytes[endIdx+1 : digitEnd]))
						if err == nil {
							switch digitCount {
							case 1, 2:
								// subpage reference, e.g. /1 or /12
								validSlash = val >= 1 && val <= 99
							case 3:
								// full page-number reference, e.g. /600
								validSlash = val >= 100 && val <= 899
							}
							// 4+ digits: not a clean subpage or page reference, reject
						}
					}
					if !validSlash {
						validSuffix = false
					}
				}
			}

			if validSuffix {
				h := 0
				if doubleheightAtCol[col] {
					h = 1
				}
				fmt.Fprintf(&pageLinks, "lnk=%s,%02d,%02d,%d\n", string(candidate), row, col, h)
			}
		}
	}

	//return pageLinks
	return pageLinks.String()
}

// insertPageLinks scans the rendered page body for teletext page number references and, if any are found,
// splices "lnk=nnn,rr,cc" lines into the header block right before <pre>.
func insertPageLinks(output []byte) []byte {
	preTag := []byte("<pre>")
	postTag := []byte("</pre>")

	preIdx := bytes.Index(output, preTag)
	if preIdx == -1 {
		return output
	}
	bodyStart := preIdx + len(preTag)

	postIdx := bytes.Index(output[bodyStart:], postTag)
	if postIdx == -1 {
		return output
	}
	body := output[bodyStart : bodyStart+postIdx]

	pageLinks := scanPageLinks(body)
	if pageLinks == "" {
		return output
	}

	result := make([]byte, 0, len(output)+len(pageLinks))
	result = append(result, output[:preIdx]...)
	result = append(result, []byte(pageLinks)...)
	result = append(result, output[preIdx:]...)
	return result
}

// The one place where every station's finished page gets written to disk. It inserts any page-number
// links found in the rendered body, then writes the result.
func savePage(dirStation string, pageNr string, output []byte) {
	output = insertPageLinks(output)
	if err := os.WriteFile(filepath.Join(dirStation, pageNr), output, 0644); err != nil {
		fmt.Println("File write error:", err)
	}
}

// Returns a blank teletext page grid of the given row count filled with spaces
func newPageBuffer(rows int) [][]byte {
	flat := bytes.Repeat([]byte{0x20}, rows*40)
	buf := make([][]byte, rows)
	for i := range buf {
		buf[i] = flat[i*40 : (i+1)*40 : (i+1)*40]
	}
	return buf
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

func getORFDate() string {
	now := time.Now()
	days := map[string]string{"Sun": "So", "Mon": "Mo", "Tue": "Di", "Wed": "Mi", "Thu": "Do", "Fri": "Fr", "Sat": "Sa"}
	yearStr := strconv.Itoa(now.Year())
	return fmt.Sprintf("\x07%s %02d.%02d.%s %s", days[now.Format("Mon")], now.Day(), now.Month(), yearStr[2:], now.Format("15:04:05"))
}

var currentPage string

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

func buildPageNavDirectives(prevPage, nextPage int) (pp, np string) {
	if prevPage > 0 {
		pp = "pn=p_" + strconv.Itoa(prevPage) + "-1\n"
	}
	if nextPage > 0 {
		np = "pn=n_" + strconv.Itoa(nextPage) + "-1\n"
	}
	return pp, np
}

func bytesToLatin1String(b []byte) string {
	r := make([]rune, len(b))
	for i, v := range b {
		r[i] = rune(v) // Force each byte to be its own Unicode point
	}
	return string(r)
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
