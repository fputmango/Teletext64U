package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// --- User Config (UD)
//
// Stores per-user config as a 7-byte binary file in the UD/ folder, keyed by MAC address.
//
// Endpoints (firmware sends uppercase; server accepts both):
//   GET /ud/{mac}/r           → check: \x00 if config exists, \x01 if not
//   GET /ud/{mac}/l           → load:  \x00\x07<7 bytes> on success, \x01 if not found
//   GET /ud/{mac}/s/{hexdata} → save:  \x00 on success, \x01 on error
//
// Config layout (7 bytes, little-endian):
//   [0-1] refreshtime  [2-3] cycletime  [4] station  [5-6] startpage

func udHandler(w http.ResponseWriter, r *http.Request) {
	// Normalise to lowercase (firmware uppercases the entire path)
	parts := strings.Split(strings.ToLower(strings.Trim(r.URL.Path, "/")), "/")
	// Expected: ud / mac / cmd  (and optionally ud / mac / s / hexdata)
	if len(parts) < 3 {
		http.Error(w, "Bad request", 400)
		return
	}

	mac := parts[1]
	cmd := parts[2]

	// Validate MAC: exactly 12 lowercase hex characters
	if len(mac) != 12 {
		http.Error(w, "Invalid MAC", 400)
		return
	}
	for _, c := range mac {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			http.Error(w, "Invalid MAC", 400)
			return
		}
	}

	cfgPath := filepath.Join(DirUD, mac+".cfg")

	writeBinaryResponse := func(data []byte) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(200)
		w.Write(data)
	}

	switch cmd {

	case "r": // Check — does a config exist for this MAC?
		fmt.Printf("Request: %-12s %s/r | ", DirUD, mac)
		if _, err := os.Stat(cfgPath); err == nil {
			fmt.Println("found")
			writeBinaryResponse([]byte{0x00})
		} else {
			fmt.Println("not found")
			writeBinaryResponse([]byte{0x01})
		}

	case "l": // Load — return the 7 config bytes
		fmt.Printf("Request: %-12s %s/l | ", DirUD, mac)
		data, err := os.ReadFile(cfgPath)
		if err != nil || len(data) != 7 {
			fmt.Println("not found")
			writeBinaryResponse([]byte{0x01})
			return
		}
		fmt.Println("loaded")
		resp := make([]byte, 9) // status(1) + length(1) + data(7)
		resp[0] = 0x00
		resp[1] = 7
		copy(resp[2:], data)
		writeBinaryResponse(resp)

	case "s": // Save — hex data is the next path segment
		fmt.Printf("Request: %-12s %s/s | ", DirUD, mac)
		if len(parts) < 4 {
			fmt.Println("missing hex data")
			writeBinaryResponse([]byte{0x01})
			return
		}
		hexdata := parts[3]
		if len(hexdata) != 14 { // 7 bytes × 2 hex digits
			fmt.Printf("invalid hex length: %d\n", len(hexdata))
			writeBinaryResponse([]byte{0x01})
			return
		}
		data := make([]byte, 7)
		for i := 0; i < 7; i++ {
			v, err := strconv.ParseUint(hexdata[i*2:i*2+2], 16, 8)
			if err != nil {
				fmt.Println("hex decode error:", err)
				writeBinaryResponse([]byte{0x01})
				return
			}
			data[i] = byte(v)
		}
		if err := os.WriteFile(cfgPath, data, 0644); err != nil {
			fmt.Println("write error:", err)
			writeBinaryResponse([]byte{0x01})
			return
		}
		fmt.Println("saved")
		writeBinaryResponse([]byte{0x00})

	default:
		http.Error(w, "Unknown command", 400)
	}
}
