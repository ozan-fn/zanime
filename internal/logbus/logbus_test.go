package logbus

import (
	"testing"
	"time"
)

// TestFanout: semua pelanggan menerima baris yang sama.
func TestFanout(t *testing.T) {
	h := New()
	a, cancelA := h.Subscribe()
	defer cancelA()
	b, cancelB := h.Subscribe()
	defer cancelB()

	h.OK("dua pelanggan")

	for _, ch := range []<-chan Event{a, b} {
		select {
		case ev := <-ch:
			if ev.Level != LevelOK || ev.Text != "dua pelanggan" || ev.At == "" {
				t.Fatalf("event = %+v", ev)
			}
		case <-time.After(time.Second):
			t.Fatal("timeout menunggu event")
		}
	}
}

// TestReplay: klien yang menyambung belakangan tetap menerima riwayat.
func TestReplay(t *testing.T) {
	h := New()
	h.Info("baris lama")
	h.Emit(Event{ID: "srv:X", Level: LevelInfo, Text: "sedang uji X"})
	h.Emit(Event{ID: "srv:X", Level: LevelOK, Text: "X siap"})

	ch, cancel := h.Subscribe()
	defer cancel()
	for i, want := range []string{"baris lama", "sedang uji X", "X siap"} {
		select {
		case ev := <-ch:
			if ev.Text != want {
				t.Fatalf("event %d = %q, want %q", i, ev.Text, want)
			}
		case <-time.After(time.Second):
			t.Fatalf("timeout menunggu %q", want)
		}
	}
}

// TestCancel: pembatalan menutup kanal dan idempoten; Emit setelahnya tidak
// panik dan tidak lagi dikirim.
func TestCancel(t *testing.T) {
	h := New()
	ch, cancel := h.Subscribe()
	h.Info("satu")
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("timeout")
	}
	cancel()
	cancel() // idempoten
	if h.Subscribers() != 0 {
		t.Fatalf("pelanggan = %d, want 0", h.Subscribers())
	}
	h.Info("setelah cancel") // tidak boleh panik
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("kanal harus tertutup")
		}
	case <-time.After(time.Second):
		t.Fatal("kanal tidak tertutup")
	}
}

// TestSlowSubscriberTidakBlok: buffer penuh → baris dilewati, Emit tetap
// langsung kembali (log tidak boleh menahan jalur stream).
func TestSlowSubscriberTidakBlok(t *testing.T) {
	h := New()
	_, cancel := h.Subscribe() // sengaja tidak dibaca
	defer cancel()

	done := make(chan struct{})
	go func() {
		for range 2000 {
			h.Info("spam")
		}
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Emit memblok pelanggan lambat")
	}
}

// TestHistoryCap: riwayat dibatasi historyCap baris terakhir.
func TestHistoryCap(t *testing.T) {
	h := New()
	for i := range historyCap + 50 {
		h.Emit(Event{ID: string(rune('a' + i%26)), Text: "x"})
	}
	if len(h.history) != historyCap {
		t.Fatalf("len(history) = %d, want %d", len(h.history), historyCap)
	}
}
