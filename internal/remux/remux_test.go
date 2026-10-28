package remux

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"
)

func TestAssetURL(t *testing.T) {
	cases := []struct {
		goos, goarch, want string
	}{
		{"linux", "amd64", "https://github.com/eugeneware/ffmpeg-static/releases/latest/download/ffmpeg-linux-x64"},
		{"linux", "arm64", "https://github.com/eugeneware/ffmpeg-static/releases/latest/download/ffmpeg-linux-arm64"},
		{"windows", "amd64", "https://github.com/eugeneware/ffmpeg-static/releases/latest/download/ffmpeg-win32-x64"},
		{"darwin", "amd64", "https://github.com/eugeneware/ffmpeg-static/releases/latest/download/ffmpeg-darwin-x64"},
	}
	for _, c := range cases {
		got, err := assetURL(c.goos, c.goarch)
		if err != nil {
			t.Fatalf("assetURL(%s,%s): %v", c.goos, c.goarch, err)
		}
		if got != c.want {
			t.Fatalf("assetURL(%s,%s) = %q, want %q", c.goos, c.goarch, got, c.want)
		}
	}
	if _, err := assetURL("plan9", "amd64"); err == nil {
		t.Fatal("assetURL(plan9) harus error")
	}
}

// budget is a tiny stand-in for the piece cache limit shared with remux.
type budget struct {
	mu    sync.Mutex
	limit int64
	used  int64
}

func (b *budget) Reserve(n int64) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.used+n > b.limit {
		return false
	}
	b.used += n
	return true
}

func (b *budget) Release(n int64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.used -= n
	if b.used < 0 {
		b.used = 0
	}
}

func (b *budget) usedBytes() int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.used
}

// fakeFFmpeg writes a shell script that ignores its arguments and prints n
// spaces on stdout, so the whole mux loop runs without real media.
func fakeFFmpeg(t *testing.T, n int) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "ffmpeg")
	script := "#!/bin/sh\nprintf '%*s' " + strconv.Itoa(n) + " ''\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("tulis ffmpeg palsu: %v", err)
	}
	return path
}

func waitReady(t *testing.T, m *Manager, index int) []byte {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if data, ready, exists := m.Output(index); exists && ready {
			return data
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("remux tidak selesai dalam batas waktu")
	return nil
}

func TestManagerBuffersOutput(t *testing.T) {
	b := &budget{limit: 1 << 20}
	m := NewManager(b)
	m.Start(context.Background(), 0, "http://127.0.0.1:1/stream/0/x.mkv", fakeFFmpeg(t, 16))

	data := waitReady(t, m, 0)
	if len(data) != 16 {
		t.Fatalf("keluaran remux %d byte, want 16", len(data))
	}
	if b.usedBytes() != 16 {
		t.Fatalf("budget terpakai %d, want 16", b.usedBytes())
	}
	if index, ready, exists := m.Status(); !exists || !ready || index != 0 {
		t.Fatalf("Status() = %d,%v,%v", index, ready, exists)
	}
	if _, ready, exists := m.Output(1); exists || ready {
		t.Fatal("Output(1) harus tidak ada")
	}
}

func TestManagerCancelsWhenBudgetExhausted(t *testing.T) {
	b := &budget{limit: 4}
	m := NewManager(b)
	m.Start(context.Background(), 0, "http://127.0.0.1:1/stream/0/x.mkv", fakeFFmpeg(t, 64))

	// Budget tidak cukup untuk satu chunk: job harus gagal dan mengembalikan
	// semua byte yang sempat dipesan.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, ready, _ := m.Output(0); ready {
			t.Fatal("remux harus gagal saat budget tidak cukup")
		}
		if b.usedBytes() == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("budget tidak dikembalikan setelah remux gagal")
}
