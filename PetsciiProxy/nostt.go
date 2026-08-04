package main

import (
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// --- NOS Teletekst ---

// doesn't need a parser function because we get the data in raw teletext binary

func nosttGetTeletexPage(pageNr string) bool {
	urlData := fmt.Sprintf("https://teletekst-data.nos.nl/page/%s", pageNr)
	logFetchingPage(urlData)
	client := http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(urlData)
	if err != nil {
		fmt.Println("Connection Error:", err)
		return false
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		fmt.Println("HTTP Error: Could not retrieve page", pageNr, "Status:", resp.StatusCode)
		return true
	}

	rawData, err := io.ReadAll(resp.Body)
	if err != nil {
		fmt.Println("Read error:", err)
		return true
	}

	txtContent := string(rawData)

	// Build a header that resembles what you see when watching teletext on a regular TV
	days := []string{"maa", "din", "woe", "don", "vri", "zat", "zon"}
	months := []string{"jan", "feb", "mrt", "apr", "mei", "jun", "jul", "aug", "sep", "okt", "nov", "dec"}

	now := time.Now()
	dutchDate := fmt.Sprintf("%s %02d %s",
		days[(int(now.Weekday())+6)%7],
		now.Day(),
		months[now.Month()-1],
	)

	reTime := regexp.MustCompile(`(\d{2}:\d{2}:\d{2})`)
	headerTime := now.Format("15:04:05")

	match := reTime.FindStringSubmatch(txtContent)
	if len(match) > 1 {
		headerTime = match[1]
	}

	cleanNr := strings.Split(pageNr, "-")[0]
	cleanNrInt, _ := strconv.Atoi(cleanNr)

	headerText := fmt.Sprintf("\x02NOS-TT  %s\x03%s  %s", cleanNr, dutchDate, headerTime)
	newPreLine := fmt.Sprintf("<pre>%40s", headerText)

	lowerContent := strings.ToLower(txtContent)
	startIndex := strings.Index(lowerContent, "<pre>")

	modifiedContent := txtContent

	if startIndex != -1 {
		reBreak := regexp.MustCompile(`[\r\n]`)
		loc := reBreak.FindStringIndex(txtContent[startIndex:])

		if loc != nil {
			endOfLine := startIndex + loc[0]
			before := txtContent[:startIndex]
			after := txtContent[endOfLine:]
			modifiedContent = before + newPreLine + after
		} else if len(txtContent) >= startIndex+45 {
			modifiedContent = txtContent[:startIndex] + newPreLine + txtContent[startIndex+45:]
		}
	}

	finalBytes := []byte(modifiedContent)

	// post-fix double height
	// These pages used to have a double heigth row on top. At some point NOS-TT decided (probably when
	// they migrated to a new teletext system) to make it normal height and the row below became black.
	if (cleanNrInt > 702 && cleanNrInt < 733) || (cleanNrInt > 750 && cleanNrInt < 763) {
		startIndex += 5 // put startIndex after the '<pre>' closing bracket
		for x := 0; x < 39; x++ {
			if finalBytes[startIndex+2*40+x] == 0x20 {
				finalBytes[startIndex+2*40+x] = 0x0D
				break
			}
		}
		// fix 3rd row
		finalBytes[startIndex+3*40+0] = 0x02 // Green
		finalBytes[startIndex+3*40+1] = 0x1D // New Background Color
		// restore normal height on some pages where the second part of the text is not written with spaces in between
		// overzicht, vooruitzichten, nederland hh:mm, windwaarschuwing, actuele luchtkwaliteit, waarschuwing
		if (cleanNrInt >= 703 && cleanNrInt <= 705) || cleanNrInt == 710 || cleanNrInt == 711 || cleanNrInt == 713 {
			finalBytes[startIndex+13+2*40] = TCC_NORMAL_HEIGHT
		}
	}
	savePage(DirNOS, pageNr, finalBytes)
	return true
}
