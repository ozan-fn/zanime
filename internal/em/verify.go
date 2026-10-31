// Verifikasi stream: setelah resolve.php memberi URL, ambil sedikit byte
// pertama dengan header yang sama seperti proxy (Referer halaman player) untuk
// memastikan URL itu benar-benar media — bukan halaman embed HTML yang tidak
// bisa diputar player kita. Server yang gagal diuji disembunyikan dari UI.
//
// Setiap langkah uji ditulis ke Logger (id sama) sehingga UI menampilkan satu
// baris yang berubah "[sedang] uji …" → "[ok] HTTP 206 · hls · 128 KiB".
package em

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
)

const (
	// probeBytes: berapa byte awal yang diambil saat menguji URL. Cukup untuk
	// mengenali playlist HLS (kecil) maupun header kontainer MP4.
	probeBytes = 128 << 10
	// mediaReferer: host video embedmaster menolak permintaan tanpa Referer
	// halaman player mereka (juga dipakai streamProxy di main.go).
	mediaReferer = "https://embdmstrplayer.com/"
)

// embedURLRe menandai URL yang cuma halaman embed (bukan media langsung),
// polanya /e/<id>, /embed/<id>, /v/<id>, /watch/<id> di akhir path. Server
// seperti ini tidak pernah bisa diputar di player kita, jadi disaring sebelum
// diuji.
var embedURLRe = regexp.MustCompile(`(?i)/(?:e|embed|v|watch)/[a-z0-9_-]+/?$`)

// verifyStream memastikan raw benar-benar mengalirkan media. Mengembalikan
// jenis sebenarnya ("hls"/"mp4", "" bila tak dikenali) dan ok. id dipakai
// untuk baris log (lihat internal/em/log.go).
func (c *Client) verifyStream(ctx context.Context, id, raw string) (string, bool) {
	if raw == "" {
		c.logf(id, LogErr, "URL stream kosong")
		return "", false
	}
	if embedURLRe.MatchString(raw) {
		c.logf(id, LogErr, "halaman embed, bukan media · %s", hostOf(raw))
		return "", false
	}
	c.logf(id, LogInfo, "uji %s · probe 128 KiB…", hostOf(raw))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		c.logf(id, LogErr, "URL tidak valid: %v", err)
		return "", false
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Referer", mediaReferer)
	// Range kecil: cukup untuk sniff; host yang mengabaikannya tetap dibatasi
	// LimitReader di bawah.
	req.Header.Set("Range", fmt.Sprintf("bytes=0-%d", probeBytes-1))
	res, err := c.http.Do(req)
	if err != nil {
		c.logf(id, LogErr, "tidak terhubung: %v", err)
		return "", false
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusPartialContent {
		c.logf(id, LogErr, "HTTP %s", res.Status)
		return "", false
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, probeBytes))
	if err != nil || len(body) == 0 {
		c.logf(id, LogErr, "tak ada byte (err=%v, %d byte)", err, len(body))
		return "", false
	}
	ct := res.Header.Get("Content-Type")
	kind := sniffMedia(ct, body)
	if kind == "" {
		c.logf(id, LogErr, "bukan media · HTTP %s · ct=%q", res.Status, ct)
		return "", false
	}
	c.logf(id, LogOK, "%s · HTTP %s · %s · %.0f KiB", hostOf(raw), res.Status, kind, float64(len(body))/1024)
	return kind, true
}

// hostOf mengambil host dari URL untuk teks log (URL asli bisa sangat panjang).
func hostOf(raw string) string {
	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		return u.Host
	}
	return raw
}

// sniffMedia menebak jenis media dari Content-Type + isi awal. Body diperiksa
// lebih dulu karena sebagian host mengirim Content-Type text/html untuk
// playlist m3u8 yang sah (mis. cdn embedmaster).
func sniffMedia(contentType string, body []byte) string {
	if bytes.HasPrefix(body, []byte("#EXTM3U")) {
		return "hls"
	}
	ct := strings.ToLower(contentType)
	head := strings.ToLower(strings.TrimSpace(string(firstN(body, 64))))
	switch {
	case strings.Contains(ct, "html"),
		strings.HasPrefix(head, "<!doctype"),
		strings.HasPrefix(head, "<html"),
		strings.HasPrefix(head, "{"):
		// Halaman embed / JSON error — bukan media.
		return ""
	case strings.Contains(ct, "mpegurl"):
		return "hls"
	case strings.Contains(ct, "video/"), strings.Contains(ct, "audio/"):
		return "mp4"
	case hasFTYP(body), isBinary(body):
		return "mp4"
	}
	return ""
}

// hasFTYP mencari kotak "ftyp" di awal file MP4/ISO-BMFF.
func hasFTYP(b []byte) bool {
	if len(b) < 12 {
		return false
	}
	return bytes.Equal(b[4:8], []byte("ftyp"))
}

// isBinary menganggap data biner bila separuh awal byte-nya bukan teks
// terkendali (heuristik cukup untuk membedakan media dari halaman teks).
func isBinary(b []byte) bool {
	n := firstN(b, 512)
	odd := 0
	for _, c := range n {
		if c == 0 {
			return true
		}
		if c < 0x09 || (c > 0x0d && c < 0x20) {
			odd++
		}
	}
	return odd > len(n)/2
}

func firstN(b []byte, n int) []byte {
	if len(b) > n {
		return b[:n]
	}
	return b
}
