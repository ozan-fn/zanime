package resolve

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestRegisterLoadRoundtrip: register → file di ./tmp/resolve → load balik
// URL asli; register ulang URL sama → slug sama (idempoten).
func TestRegisterLoadRoundtrip(t *testing.T) {
	t.Chdir(t.TempDir())
	p := New()
	slug := p.Register("https://host/a.mp4", "Film Keren 1080p")
	if slug != "film-keren-1080p.mp4" {
		t.Fatalf("slug = %q, want film-keren-1080p.mp4", slug)
	}
	entries, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	if len(entries) != 1 {
		t.Fatalf("file map = %v, want 1", entries)
	}
	if got, ok := p.Load(slug); !ok || got != "https://host/a.mp4" {
		t.Fatalf("Load = %q %v", got, ok)
	}
	if again := p.Register("https://host/a.mp4", "Film Keren 1080p"); again != slug {
		t.Fatalf("register ulang beda slug: %q vs %q", again, slug)
	}
	entries, _ = filepath.Glob(filepath.Join(dir, "*.json"))
	if len(entries) != 1 {
		t.Fatalf("register ulang bikin file baru: %v", entries)
	}
}

// TestSlugKeepsMediaExtension: slug menyertakan ekstensi media dari URL, karena
// player memilih jalur adaptif HLS/DASH hanya bila src-nya berakhiran .m3u8/.mpd.
// URL tanpa ekstensi tidak ditambahi apa pun.
func TestSlugKeepsMediaExtension(t *testing.T) {
	t.Chdir(t.TempDir())
	p := New()
	cases := map[string]string{
		"https://host/a.m3u8":        "stream.m3u8",
		"https://host/x/y.mp4":       "stream.mp4",
		"https://host/sub.vtt?tok=1": "stream.vtt",
		"https://host/noext":         "stream",
	}
	for raw, want := range cases {
		if got := p.Register(raw, "stream"); got != want {
			t.Fatalf("Register(%q) = %q, want %q", raw, got, want)
		}
	}
}

// TestRegisterCollision: dua URL beda dengan hint sama → slug kedua dapat
// hash tambahan; load masing-masing tetap benar.
func TestRegisterCollision(t *testing.T) {
	t.Chdir(t.TempDir())
	p := New()
	s1 := p.Register("https://host/a.m3u8", "seg")
	s2 := p.Register("https://host/b.m3u8", "seg")
	if s1 == s2 {
		t.Fatalf("kolisi tidak dipecah: %q == %q", s1, s2)
	}
	for slug, want := range map[string]string{s1: "https://host/a.m3u8", s2: "https://host/b.m3u8"} {
		if got, ok := p.Load(slug); !ok || got != want {
			t.Fatalf("Load(%q) = %q %v, want %q", slug, got, ok, want)
		}
	}
}

// TestSlugHintDirty: hint kotor (spasi, simbol, emoji, kapital) di-normalize;
// kosong → seg-<hash>; max 48 char.
func TestSlugHintDirty(t *testing.T) {
	t.Chdir(t.TempDir())
	p := New()
	if s := p.Register("u1", "Bersih Bersih!!"); s != "bersih-bersih" {
		t.Fatalf("s = %q", s)
	}
	if s := p.Register("u2", ""); !strings.HasPrefix(s, "seg-") {
		t.Fatalf("hint kosong = %q", s)
	}
	long := strings.Repeat("ab", 60)
	if s := p.Register("u3", long); len(s) > 48 {
		t.Fatalf("slug kepanjangan: %d", len(s))
	}
}

// TestLoadGuard: slug kosong, mengandung slash (path traversal), dan slug
// tidak dikenal semua ditolak.
func TestLoadGuard(t *testing.T) {
	t.Chdir(t.TempDir())
	p := New()
	for _, slug := range []string{"", "../secret", "a/b", "ngaco-absolut"} {
		if got, ok := p.Load(slug); ok || got != "" {
			t.Fatalf("Load(%q) harus gagal, dapat %q", slug, got)
		}
	}
}

// TestRegisterTempInMemory: slug segmen tidak menulis file (jumlahnya ribuan),
// tetap bisa di-load selama proses hidup, dan tidak bentrok satu sama lain.
func TestRegisterTempInMemory(t *testing.T) {
	t.Chdir(t.TempDir())
	p := New()
	s1 := p.RegisterTemp("https://host/seg1.html?t=1")
	s2 := p.RegisterTemp("https://host/seg2.html?t=2")
	if s1 == s2 {
		t.Fatalf("slug segmen harus unik: %q", s1)
	}
	for slug, want := range map[string]string{s1: "https://host/seg1.html?t=1", s2: "https://host/seg2.html?t=2"} {
		if got, ok := p.Load(slug); !ok || got != want {
			t.Fatalf("Load(%q) = %q %v, want %q", slug, got, ok, want)
		}
	}
	// Tidak ada file tertulis untuk slug sementara.
	entries, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	if len(entries) != 0 {
		t.Fatalf("RegisterTemp menulis file: %v", entries)
	}
	// Idempoten: URL sama → slug sama.
	if again := p.RegisterTemp("https://host/seg1.html?t=1"); again != s1 {
		t.Fatalf("RegisterTemp tidak idempoten: %q vs %q", again, s1)
	}
	// Pool baru tidak melihat slug sementara (memang sengaja tidak persisten).
	if _, ok := New().Load(s1); ok {
		t.Fatalf("slug sementara bocor ke pool lain: %q", s1)
	}
}

// TestTidyDropsStaleTempEntries: entri memori (tanpa file) dibuang Tidy setelah
// melewati urlTTL supaya tidak menumpuk sepanjang umur proses.
func TestTidyDropsStaleTempEntries(t *testing.T) {
	t.Chdir(t.TempDir())
	p := New()
	slug := p.RegisterTemp("https://host/seg.html")
	p.mu.Lock()
	p.urls[slug].at = time.Now().Add(-urlTTL - time.Minute)
	p.mu.Unlock()
	p.Tidy()
	if _, ok := p.Load(slug); ok {
		t.Fatal("entri memori basi harus dipangkas")
	}
}

// TestPersistAcrossPools: file map di ./tmp/resolve dibaca pool baru tanpa
// register ulang (server restart tetap bisa memutar slug lama).
func TestPersistAcrossPools(t *testing.T) {
	t.Chdir(t.TempDir())
	s1 := New().Register("https://host/live.m3u8", "live")
	if got, ok := New().Load(s1); !ok || got != "https://host/live.m3u8" {
		t.Fatalf("pool baru gagal baca: %q %v", got, ok)
	}
}

// TestCorruptFileIgnored: file .json rusak → Load false, tanpa panic.
func TestCorruptFileIgnored(t *testing.T) {
	t.Chdir(t.TempDir())
	_ = os.MkdirAll(dir, 0o755)
	if err := os.WriteFile(filepath.Join(dir, "rusak.json"), []byte("{bukan json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := New().Load("rusak"); ok {
		t.Fatal("file rusak harus ditolak")
	}
}

// TestTidyPrunesOldEntries: entri yang mtime-nya lebih tua dari urlTTL
// dihapus; yang masih segar bertahan.
func TestTidyPrunesOldEntries(t *testing.T) {
	t.Chdir(t.TempDir())
	p := New()
	segar := p.Register("https://host/fresh.mp4", "fresh")
	basi := p.Register("https://host/old.mp4", "old")
	old := time.Now().Add(-urlTTL - time.Minute)
	if err := os.Chtimes(filepath.Join(dir, basi+".json"), old, old); err != nil {
		t.Fatal(err)
	}

	p.Tidy()
	if _, ok := p.Load(segar); !ok {
		t.Fatal("entri segar harus bertahan")
	}
	if _, ok := p.Load(basi); ok {
		t.Fatal("entri basi harus dipangkas")
	}
}

// TestRegisterSchedulesTidy: Register memangkas entri basi begitu tidyEvery
// terlampaui sejak pembersihan terakhir (tanpa goroutine/timer).
func TestRegisterSchedulesTidy(t *testing.T) {
	t.Chdir(t.TempDir())
	p := New()
	basi := p.Register("https://host/old.mp4", "old")
	old := time.Now().Add(-urlTTL - time.Minute)
	if err := os.Chtimes(filepath.Join(dir, basi+".json"), old, old); err != nil {
		t.Fatal(err)
	}
	// Belum lewat tidyEvery → belum dipangkas.
	p.lastTidy = time.Now()
	p.Register("https://host/a.mp4", "a")
	if _, ok := p.Load(basi); !ok {
		t.Fatal("pangkas jalan terlalu dini")
	}
	// Sudah lewat tidyEvery → dipangkas saat Register berikutnya.
	p.lastTidy = time.Now().Add(-tidyEvery - time.Minute)
	p.Register("https://host/b.mp4", "b")
	if _, ok := p.Load(basi); ok {
		t.Fatal("entri basi harus sudah dipangkas")
	}
}
