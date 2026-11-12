/*****************************************************************
C interface for the cc65 WiC64 driver (wic64.s)

Minimal subset of the WiC64 protocol needed for Teletext 64 Ultimate.
Based on the WiC64 library by Henning Liebenau, (c) 2023, 2-clause BSD
licence: full text in wic64.s.
******************************************************************/

#ifndef WIC64_H
#define WIC64_H

#ifdef __cplusplus
extern "C" {
#endif

#define WIC64_DRV_TIMEOUT				0xFFu

#define WIC64_SUCCESS						0x00u
#define WIC64_INTERNAL_ERROR		0x01u
#define WIC64_CLIENT_ERROR			0x02u
#define WIC64_CONNECTION_ERROR	0x03u
#define WIC64_NETWORK_ERROR			0x04u
#define WIC64_SERVER_ERROR			0x05u   // HTTP status >= 400

extern unsigned char wic64_status;
extern unsigned int  wic64_response_size;

// returns nonzero if the WiC64 responds
unsigned char wic64_detect(void);

// buf must be >= 19; returns 1 on success, 0 on timeout
unsigned char __fastcall__ wic64_get_mac(char *buf);

// URL: NUL-terminated ASCII string, max 255 bytes
// full response body is written to buf capped at max bytes
// returns the number of bytes in buf
// -> read wic64_status to detect HTTP errors (e.g. 404 -> WIC64_SERVER_ERROR)
// -> read wic64_response_size for the original response length
unsigned int __fastcall__ wic64_http_get(const char *url, void *buf, unsigned int max);

// Fetch the PRG at url, load it at $0801 and run it via BASIC
// URL must be NUL-terminated ASCII (and call petscii_to_ascii first)
// -> on device/network error, returns with wic64_status set (WIC64_DRV_TIMEOUT or a firmware error code)
void __fastcall__ wic64_load_and_run(const char *url);

#ifdef __cplusplus
}
#endif

#endif
