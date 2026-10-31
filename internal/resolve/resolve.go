// Package resolve: URL media (host video embedmaster) didaftarkan jadi slug
// pendek supaya browser tidak membawa URL panjang ter-encode.
//
// Slug stream/kualitas/subtitle yang layak hidup lintas restart ditulis ke
// ./tmp/resolve/<slug>.json (satu file per slug). Playlist turunan dan segmen
// HLS — jumlahnya ribuan per judul — hanya disimpan di memori, karena cuma
// dipakai selama satu sesi pemutaran dan browser selalu membacanya ulang dari
// playlist induk.
package resolve

import (
	"crypto/sha1"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	dir = "tmp/resolve"
	// urlTTL: URL segmen/stream embedmaster kedaluwarsa (≈15 mnt); simpan
	// sedikit lebih lama supaya playback yang masih jalan tidak putus.
	urlTTL = 6 * time.Hour
	// tidyEvery: pembersihan berkala di dalam Register (tiap satu jam) agar
	// direktori tidak menumpuk ribuan file slug segmen.
	tidyEvery = time.Hour
)

type Pool struct {
	mu       sync.RWMutex
	urls     map[string]*item
	lastTidy time.Time
}

type item struct {
	url    string
	at     time.Time
	onDisk bool // punya file di ./tmp/resolve (TTL-nya mengikuti mtime file)
}

func New() *Pool { return &Pool{urls: map[string]*item{}} }

type entry struct {
	URL string `json:"url"`
}

// Register mendaftarkan URL sebagai slug persisten: file ./tmp/resolve/<slug>.json
// ditulis agar server restart masih bisa memutar slug lama. hint membentuk slug
// yang terbaca manusia; bila bentrok (dalam proses ini) ditambah hash pendek.
func (p *Pool) Register(rawURL, hint string) string {
	return p.RegisterKind(rawURL, hint, "")
}

// RegisterKind seperti Register, tapi ekstensi slug dipaksa `kind` (mis.
// ".m3u8") untuk stream yang hasil verifikasinya HLS walau URL-nya tanpa
// ekstensi. kind kosong = pakai ekstensi dari URL.
func (p *Pool) RegisterKind(rawURL, hint, kind string) string {
	p.mu.Lock()
	slug := p.assignLocked(rawURL, hint, kind)
	p.mu.Unlock()
	p.persist(slug, rawURL)
	p.tidyIfDue()
	return slug
}

// RegisterTemp mendaftarkan URL hanya di memori — untuk playlist turunan dan
// segmen HLS: jumlahnya ribuan dan hanya dipakai selama satu sesi. Slug
// diturunkan langsung dari URL (stabil antar permintaan, jadi playlist hasil
// rewrite bisa di-cache browser tanpa slug bergeser).
func (p *Pool) RegisterTemp(rawURL string) string {
	slug := "seg-" + short(rawURL) + extOf(rawURL)
	p.mu.Lock()
	p.urls[slug] = &item{url: rawURL, at: time.Now()}
	p.mu.Unlock()
	return slug
}

// assignLocked menyimpan URL di bawah slug; pemanggil memegang p.mu. Ekstensi
// media ditambahkan ke slug supaya player mengenali jenis sumbernya; `kind`
// (bila tidak kosong) menimpa ekstensi hasil URL.
func (p *Pool) assignLocked(rawURL, hint, kind string) string {
	ext := kind
	if ext == "" {
		ext = extOf(rawURL)
	}
	slug := p.slug(hint) + ext
	if existing, ok := p.urls[slug]; ok && existing.url != rawURL {
		slug = p.slug(hint) + "-" + short(rawURL) + ext
	}
	p.urls[slug] = &item{url: rawURL, at: time.Now()}
	return slug
}

// persist menulis satu file slug; gagal tulis tidak fatal (slug tetap jalan
// selama proses hidup).
func (p *Pool) persist(slug, rawURL string) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	data, err := json.Marshal(entry{URL: rawURL})
	if err != nil {
		return
	}
	if err := os.WriteFile(filepath.Join(dir, slug+".json"), data, 0o644); err != nil {
		return
	}
	p.mu.Lock()
	if it := p.urls[slug]; it != nil {
		it.onDisk = true
	}
	p.mu.Unlock()
}

// Tidy membersihkan entri lama (dipanggil saat start server; Register juga
// menjadwalkannya berkala).
func (p *Pool) Tidy() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lastTidy = time.Now()
	p.pruneLocked()
}

// Load mengambil URL untuk slug, false bila slug tidak ada. Peta memori
// melayani permintaan segmen; file dipakai sebagai fallback lintas restart.
func (p *Pool) Load(slug string) (string, bool) {
	slug = strings.Trim(slug, "/")
	if slug == "" || strings.ContainsAny(slug, "/\\") {
		return "", false
	}
	p.mu.RLock()
	it, ok := p.urls[slug]
	p.mu.RUnlock()
	if ok {
		return it.url, true
	}
	// Fallback: slug yang ditulis proses sebelumnya (belum ada di peta).
	data, err := os.ReadFile(filepath.Join(dir, slug+".json"))
	if err != nil {
		return "", false
	}
	var e entry
	if err := json.Unmarshal(data, &e); err != nil || e.URL == "" {
		return "", false
	}
	p.mu.Lock()
	p.urls[slug] = &item{url: e.URL, at: time.Now(), onDisk: true}
	p.mu.Unlock()
	return e.URL, true
}

func (p *Pool) tidyIfDue() {
	p.mu.RLock()
	due := time.Since(p.lastTidy) > tidyEvery
	p.mu.RUnlock()
	if !due {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.lastTidy = time.Now()
	p.pruneLocked()
}

// pruneLocked membuang entri basi: file lebih tua dari urlTTL dihapus, lalu
// entri memori tanpa file dibatasi umur yang sama. Pemanggil memegang p.mu.
func (p *Pool) pruneLocked() {
	now := time.Now()
	entries, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err == nil {
		stale := make(map[string]bool, len(entries))
		for _, path := range entries {
			info, statErr := os.Stat(path)
			if statErr != nil {
				continue
			}
			if now.Sub(info.ModTime()) > urlTTL {
				_ = os.Remove(path)
				stale[strings.TrimSuffix(filepath.Base(path), ".json")] = true
			}
		}
		for slug := range stale {
			if it := p.urls[slug]; it != nil && it.onDisk {
				delete(p.urls, slug)
			}
		}
	}
	// Entri memori (segmen/playlist turunan) tidak punya file: buang berdasarkan
	// umur supaya tidak menumpuk sepanjang umur proses.
	for slug, it := range p.urls {
		if !it.onDisk && now.Sub(it.at) > urlTTL {
			delete(p.urls, slug)
		}
	}
}

// ExtForKind memberi ekstensi slug untuk jenis media hasil verifikasi
// (internal/em), supaya player memilih jalur adaptif HLS/DASH yang benar
// walau URL-nya sendiri tanpa ekstensi.
func ExtForKind(kind string) string {
	switch kind {
	case "hls":
		return ".m3u8"
	case "mp4":
		return ".mp4"
	}
	return ""
}

var (
	reSlugSpace = regexp.MustCompile(`[^a-z0-9]+`)
	reSlugDash  = regexp.MustCompile(`^-+|-+$`)
	reExt       = regexp.MustCompile(`\.[a-z0-9]{1,5}$`)
)

// extOf mengambil ekstensi media dari path URL (".m3u8", ".mp4", ".vtt"), ""
// bila tidak ada. Slug menyertakannya karena player (movi-player) memilih jalur
// adaptif — menu kualitas + track subtitle manifest — hanya bila src-nya
// mengandung ".m3u8"/".mpd"; tanpa itu HLS diperlakukan sebagai file tunggal.
func extOf(rawURL string) string {
	u, err := url.Parse(rawURL)
	if err != nil {
		return ""
	}
	return reExt.FindString(strings.ToLower(u.Path))
}

// slug teks hint jadi kecil, alnum-dash, max 48 kokoh; kosong → seg-<hash>.
func (p *Pool) slug(hint string) string {
	s := reSlugDash.ReplaceAllString(reSlugSpace.ReplaceAllString(strings.ToLower(hint), "-"), "")
	if s == "" {
		return "seg-" + short(hint)
	}
	if len(s) > 48 {
		s = s[:48]
	}
	return s
}

func short(s string) string {
	sum := sha1.Sum([]byte(s))
	return fmt.Sprintf("%x", sum)[:12]
}
