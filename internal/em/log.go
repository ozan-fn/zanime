package em

import "fmt"

// Level log pipeline; nilainya sama dengan internal/logbus supaya adapter di
// main bisa meneruskan apa adanya ke websocket.
const (
	LogInfo = "info"
	LogOK   = "ok"
	LogErr  = "err"
)

// Logger menerima satu baris log pipeline. `id` stabil per langkah sehingga UI
// bisa memperbarui baris yang sama ("sedang" → "ok"); id kosong = baris
// berdiri sendiri. `msg` sudah diformat.
type Logger interface {
	Log(id, level, msg string)
}

// nopLogger dipakai bila pemanggil tidak memasang logger (test, CLI).
type nopLogger struct{}

func (nopLogger) Log(string, string, string) {}

// SetLogger memasang penerima log (mis. hub websocket di main). Panggil
// sebelum Streams: Streams menyalin logger ini ke sesi per judul.
func (c *Client) SetLogger(l Logger) {
	if l == nil {
		l = nopLogger{}
	}
	c.log = l
}

// logf memformat + mengirim satu baris; aman dipanggil dari banyak worker.
func (c *Client) logf(id, level, format string, a ...any) {
	if c.log == nil {
		return
	}
	c.log.Log(id, level, fmt.Sprintf(format, a...))
}
