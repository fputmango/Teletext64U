package main

import (
	"bytes"
	_ "embed"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// TEKSTI-TV: XML based
// gets filled with command line parameter
var tekstiAPIkey string = ""

// tekstiInfoPage is the "no API key configured" instructions page shown in place of a real
// Teksti-TV page when tekstiAPIkey is empty. It's a raw 24x40 teletext page (960 bytes)

// the bin file can be read into this web based editor https://zxnet.co.uk/teletext/editor

//go:embed assets/tekstitv-info.bin
var tekstiInfoPage []byte

// defaultTekstiRows splits the embedded tekstiInfoPage into 24 rows of 40 bytes each, the same
// [][]byte shape parseTEKSTIRows produces. Falls back to a blank page (rather than panicking)
// if the embedded asset is ever the wrong size.
func defaultTekstiRows() [][]byte {
	const wantLen = 24 * 40
	if len(tekstiInfoPage) != wantLen {
		fmt.Printf("tekstitv-info.bin: expected %d bytes, got %d - showing blank page\n", wantLen, len(tekstiInfoPage))
		return newPageBuffer(24)
	}
	rows := make([][]byte, 24)
	for i := range rows {
		rows[i] = tekstiInfoPage[i*40 : (i+1)*40]
	}
	return rows
}

type TeletextLine struct {
	Number int    `xml:"number,attr"`
	Value  string `xml:",chardata"`
}

type Content struct {
	Type  string         `xml:"type,attr"`
	Lines []TeletextLine `xml:"line"`
}

type Subpage struct {
	Number   int       `xml:"number,attr"`
	Contents []Content `xml:"content"`
}

type TeletextPage struct {
	Subpages []Subpage `xml:"subpage"`
}

// --- YLE TEKSTI-TV  ---

func tekstiGetTeletexPage(pageNr string) bool {
	parts := strings.Split(pageNr, "-")
	var rows [][]byte
	var nav NavignationInfo

	if tekstiAPIkey == "" {
		// show the user a teletext page with instructions how to obtain an API key
		rows = defaultTekstiRows()
		logFetchingPage("Yle Teksti-TV info screen")
	} else {
		url := fmt.Sprintf("https://external.api.yle.fi/v1/teletext/pages/%s.xml?%s", parts[0], tekstiAPIkey)
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

		if strings.HasPrefix(parts[1], "0") {
			parts[1] = "1"
		}

		rows, nav, err = parseTEKSTIRows(resp.Body, parts[1])
		if err != nil {
			fmt.Println("xml.Unmarshal error")
			return true
		}
	}
	ps := ""
	ns := ""
	if nav.numberOfSubpages > 1 {
		ps, ns, _ = getPrevNextSubpage(parts[0], nav)
	}

	var output []byte
	output = append(output, []byte(fmt.Sprintf(
		"%v%vftl=%v-0\nftl=%v-0\nftl=%v-0\nftl=%v-0\n<pre>", ps, ns,
		"100", "200", "300", "400"))...)

	headerRow := bytes.Repeat([]byte{0x20}, 40)
	now := time.Now()
	copy(headerRow[7:], fmt.Sprintf("\x07%s YLE TEKSTI-TV %02d.%02d.%s", parts[0], now.Day(), 3, now.Format("15:04:05")))
	output = append(output, headerRow...)

	for _, r := range rows {
		output = append(output, r...)
	}

	output = append(output, []byte("</pre>")...)
	savePage(DirTEKSTI, pageNr, output)
	return true
}

func parseTEKSTIRows(body io.ReadCloser, subpageStr string) ([][]byte, NavignationInfo, error) {
	defer body.Close()

	// Initialize empty 24x40 grid with spaces (0x20)
	pageBuffer := newPageBuffer(24)

	decoder := xml.NewDecoder(body)

	// Track state during streaming
	inTargetSubpage := false

	var pageSubpageCount int
	var nav NavignationInfo

	for {
		t, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nav, err
		}

		switch se := t.(type) {
		case xml.StartElement:
			if se.Name.Local == "page" {
				for _, attr := range se.Attr {
					switch attr.Name.Local {
					case "subpagecount":
						fmt.Sscanf(attr.Value, "%d", &pageSubpageCount)
						nav.numberOfSubpages = pageSubpageCount
						//fmt.Printf("Page has %d subpages\n", pageSubpageCount)
					case "number":
						//fmt.Printf("Page number: %s\n", attr.Value)
					}
				}
			}

			if se.Name.Local == "subpage" {
				// Check if this subpage matches the requested number
				for _, attr := range se.Attr {
					if attr.Name.Local == "number" && attr.Value == subpageStr {
						inTargetSubpage = true
						subpageInt, err := strconv.Atoi(subpageStr)
						if err == nil {
							if subpageInt > 1 {
								nav.prevSubpage = subpageInt - 1
							}
							if subpageInt < nav.numberOfSubpages {
								nav.nextSubpage = subpageInt + 1
							}
						}
					}
				}
			}

			// If inside correct subpage, look for <content type="all">
			if inTargetSubpage && se.Name.Local == "content" {
				isAllType := false
				for _, attr := range se.Attr {
					if attr.Name.Local == "type" && attr.Value == "all" {
						isAllType = true
					}
				}

				if isAllType {
					// We are inside the correct block, parse the lines
					if err := decodeTekstiLinesIntoBuffer(decoder, pageBuffer); err != nil {
						return nil, nav, err
					}
					return pageBuffer, nav, nil // Found and processed the target
				}
			}

		case xml.EndElement:
			if se.Name.Local == "subpage" {
				inTargetSubpage = false
			}
		}
	}
	return pageBuffer, nav, nil
}

// Helper to handle the internal line decoding
func decodeTekstiLinesIntoBuffer(decoder *xml.Decoder, buffer [][]byte) error {
	for {
		t, err := decoder.Token()
		if err != nil {
			return err
		}
		switch se := t.(type) {
		case xml.StartElement:
			if se.Name.Local == "line" {
				var lineNum int
				for _, attr := range se.Attr {
					if attr.Name.Local == "number" {
						fmt.Sscanf(attr.Value, "%d", &lineNum)
					}
				}
				content, err := decoder.Token()
				if err != nil {
					return err
				}
				if cd, ok := content.(xml.CharData); ok {
					if lineNum >= 1 && lineNum <= 24 {
						buffer[lineNum-1] = processTekstiLine(string(cd))
					}
				}
			}
		case xml.EndElement:
			if se.Name.Local == "content" {
				return nil
			}
		}
	}
}

func processTekstiLine(input string) []byte {
	output := make([]byte, 0, 40)
	runes := []rune(input)

	for i := 0; i < len(runes); i++ {
		if len(output) >= 40 {
			break
		}
		if runes[i] == '{' {
			end := -1
			for j := i + 1; j < len(runes); j++ {
				if runes[j] == '}' {
					end = j
					break
				}
			}
			if end != -1 {
				tagName := string(runes[i+1 : end])
				if code, ok := controlMap[tagName]; ok {
					output = append(output, code)
					i = end // Move pointer to the '}'
					continue
				}
			}
		}
		output = append(output, encodeTekstiChar(runes[i]))
		//output = append(output, byte(runes[i]))
	}
	for len(output) < 40 {
		output = append(output, 0x20)
	}
	return output[:40]
}

func encodeTekstiChar(r rune) byte {
	switch r {
	case 'Ä':
		return 0x5B
	case 'Ö':
		return 0x5C
	case 'Å':
		return 0x5D
	case 'ä':
		return 0x7B
	case 'ö':
		return 0x7C
	case 'å':
		return 0x7D
	case 'é':
		return 0xE9
	case '€':
		return 0x80
	default:
		if r < 128 {
			return byte(r)
		}
		return 0x20
	}
}
