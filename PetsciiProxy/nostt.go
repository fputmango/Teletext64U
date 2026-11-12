// --- NOS Teletekst ---
//
// Dutch Teletext service
//
// The first Teletext service that was supported by PetsciiProxy
//
// PetsciiProxy - Copyright (C) 2026 Frank Putman
// SPDX-License-Identifier: GPL-3.0-or-later

package main

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

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

	const preTag = "<pre>"
	preIndex := bytes.Index(bytes.ToLower(rawData), []byte(preTag))
	if preIndex == -1 || len(rawData) < preIndex+len(preTag)+40 {
		fmt.Println("NOS-TT: unexpected page format for", pageNr)
		return true
	}
	bodyStart := preIndex + len(preTag)
	nosHeader := rawData[bodyStart : bodyStart+40]

	// Build a header that resembles what you see when watching teletext on a regular TV
	headerTime := string(nosHeader[32:])
	days := []string{"maa", "din", "woe", "don", "vri", "zat", "zon"}
	months := []string{"jan", "feb", "mrt", "apr", "mei", "jun", "jul", "aug", "sep", "okt", "nov", "dec"}
	now := time.Now()
	dutchDate := fmt.Sprintf("%s %02d %s",
		days[(int(now.Weekday())+6)%7],
		now.Day(),
		months[now.Month()-1],
	)

	cleanNr, _, _ := strings.Cut(pageNr, "-")
	headerText := fmt.Sprintf("\x02NOS-TT  %s\x03%s  %s", cleanNr, dutchDate, headerTime)
	copy(nosHeader, fmt.Sprintf("%40s", headerText))

	finalBytes := rawData

	// post-fix double height
	// These pages used to have a double heigth row on top. At some point NOS-TT decided (probably when
	// they migrated to a new teletext system) to make it normal height and the row below became black.
	cleanNrInt, _ := strconv.Atoi(cleanNr)
	if (cleanNrInt > 702 && cleanNrInt < 733) || (cleanNrInt > 750 && cleanNrInt < 763) {
		for x := range 39 {
			if finalBytes[bodyStart+2*40+x] == 0x20 {
				finalBytes[bodyStart+2*40+x] = TCC_DOUBLE_HEIGHT
				break
			}
		}
		// fix 3rd row
		finalBytes[bodyStart+3*40+0] = TCC_ALPHA_GREEN
		finalBytes[bodyStart+3*40+1] = TCC_NEW_BACKGROUND
		// restore normal height on some pages where the second part of the text is not written with spaces in between
		// overzicht, vooruitzichten, nederland hh:mm, windwaarschuwing, actuele luchtkwaliteit, waarschuwing
		if (cleanNrInt >= 703 && cleanNrInt <= 705) || cleanNrInt == 710 || cleanNrInt == 711 || cleanNrInt == 713 {
			finalBytes[bodyStart+13+2*40] = TCC_NORMAL_HEIGHT
		}
		if cleanNrInt == 751 {
			finalBytes[bodyStart+20+2*40] = TCC_NORMAL_HEIGHT
		}
	}
	savePage(DirNOS, pageNr, finalBytes)
	return true
}
