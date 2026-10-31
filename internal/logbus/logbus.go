// Package logbus menyiarkan baris log server ke klien web lewat websocket:
// satu Hub fan-out ke banyak klien (riwayat terakhir ikut di-replay ke klien
// baru), dan pengiriman non-blocking supaya logging tidak pernah memperlambat
// jalur stream.
package logbus

import (
	"fmt"
	"sync"
	"time"
)

// Level satu baris log: info (sedang berjalan), ok (berhasil), err (gagal).
// Nilainya sama dengan internal/em dan kontrak JSON ke frontend.
const (
	LevelInfo = "info"
	LevelOK   = "ok"
	LevelErr  = "err"
)

// Event satu baris log. ID opsional: kiriman berikutnya dengan ID sama
// menggantikan baris sebelumnya di UI (mis. "[sedang] uji X" → "[ok] X"),
// sehingga satu langkah tampil sebagai satu baris yang berubah.
type Event struct {
	ID    string `json:"id,omitempty"`
	At    string `json:"at"`
	Level string `json:"level"`
	Text  string `json:"text"`
}

// historyCap = berapa baris terakhir yang disimpan untuk klien yang baru
// menyambung (halaman reload di tengah pemutaran tetap punya konteks).
const historyCap = 200

// Hub menyiarkan Event ke semua klien yang berlangganan.
type Hub struct {
	mu      sync.Mutex
	subs    map[chan Event]struct{}
	history []Event
}

func New() *Hub { return &Hub{subs: map[chan Event]struct{}{}} }

// Subscribe mengembalikan kanal event: riwayat terakhir dulu, lalu event baru.
// Fungsi kedua adalah pembatalan (idempoten, aman dipanggil berkali-kali).
func (h *Hub) Subscribe() (<-chan Event, func()) {
	ch := make(chan Event, 512)
	h.mu.Lock()
	for _, ev := range h.history {
		select {
		case ch <- ev:
		default:
		}
	}
	h.subs[ch] = struct{}{}
	h.mu.Unlock()

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			h.mu.Lock()
			delete(h.subs, ch)
			close(ch)
			h.mu.Unlock()
		})
	}
	return ch, cancel
}

// Subscribers = jumlah klien aktif (dipakai test/diagnostik).
func (h *Hub) Subscribers() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.subs)
}

// Emit menyiarkan satu baris ke semua klien. Klien yang lambat (buffer penuh)
// dilewati, bukan diblok: log tidak boleh menahan scrape/proxy.
func (h *Hub) Emit(ev Event) {
	if h == nil {
		return
	}
	if ev.At == "" {
		ev.At = now()
	}
	if ev.Level == "" {
		ev.Level = LevelInfo
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.history = append(h.history, ev)
	if len(h.history) > historyCap {
		h.history = append([]Event(nil), h.history[len(h.history)-historyCap:]...)
	}
	for ch := range h.subs {
		select {
		case ch <- ev:
		default:
		}
	}
}

// Info/OK/Err = baris berdiri sendiri (tanpa ID).
func (h *Hub) Info(text string) { h.Emit(Event{Level: LevelInfo, Text: text}) }
func (h *Hub) OK(text string)   { h.Emit(Event{Level: LevelOK, Text: text}) }
func (h *Hub) Err(text string)  { h.Emit(Event{Level: LevelErr, Text: text}) }

// Infof/OKf/Errf seperti di atas dengan format.
func (h *Hub) Infof(format string, a ...any) { h.Info(fmt.Sprintf(format, a...)) }
func (h *Hub) OKf(format string, a ...any)   { h.OK(fmt.Sprintf(format, a...)) }
func (h *Hub) Errf(format string, a ...any)  { h.Err(fmt.Sprintf(format, a...)) }

func now() string { return time.Now().Format("15:04:05.000") }
