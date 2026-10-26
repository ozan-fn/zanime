package torrent

import (
	"testing"

	alog "github.com/anacrolix/log"
)

type recHandler struct{ got []string }

func (h *recHandler) Handle(r alog.Record) { h.got = append(h.got, r.Msg.String()) }

// Pesan "read failed after reader reset" muncul saat reader ditutup bersamaan torrent
// ditutup — bukan gangguan nyata, jangan sampai membanjiri log panel.
func TestQuietHandlerDropsReaderReset(t *testing.T) {
	next := &recHandler{}
	h := quietHandler{next: next}
	h.Handle(alog.Record{Msg: alog.Str("read failed after reader reset")})
	h.Handle(alog.Record{Msg: alog.Str("pesan lain yang tetap penting")})

	if len(next.got) != 1 {
		t.Fatalf("harus 1 pesan diteruskan, dapat %d: %v", len(next.got), next.got)
	}
	if next.got[0] != "pesan lain yang tetap penting" {
		t.Fatalf("pesan yang diteruskan salah: %q", next.got[0])
	}
}
