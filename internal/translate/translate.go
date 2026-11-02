// Package translate menerjemahkan subtitle VTT lewat endpoint publik Google
// Translate (yang juga dipakai subtitlewise: translate_a/single, client=gtx).
// Hasilnya di-cache di ./tmp/translate supaya subtitle yang sama tidak
// diterjemahkan dua kali.
package translate

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/go-resty/resty/v2"
)

const (
	// dir: cache terjemahan per konten subtitle + bahasa tujuan.
	dir = "tmp/translate"
	// defaultBase: endpoint terjemahan Google (dipakai subtitlewise).
	defaultBase = "https://translate.googleapis.com/translate_a/single"
	// client: dict-chrome-ex dipakai, bukan gtx — endpoint yang sama tapi gtx
	// kini dibalas halaman "Sorry..." untuk permintaan non-browser.
	clientParam = "dict-chrome-ex"
	// ua: Google menolak permintaan tanpa UA browser yang wajar.
	ua = "Mozilla/5.0 (X11; Linux x86_64; rv:132.0) Gecko/20100101 Firefox/132.0"
)

// Client menerjemahkan subtitle. Base dapat ditimpa test.
type Client struct {
	Base string
	http *resty.Client
}

// Result adalah hasil satu terjemahan.
type Result struct {
	Key    string // kunci cache (hex sha1)
	VTT    string
	Cues   int
	Cached bool
}

func New() *Client {
	return &Client{
		Base: defaultBase,
		http: resty.New().SetTimeout(20*time.Second).SetHeader("User-Agent", ua),
	}
}

// TranslateVTT menerjemahkan isi cue satu VTT ke bahasa `to`, mempertahankan
// header, nomor cue, dan timestamp. Hasil disimpan di cache dan dipakai ulang
// untuk konten yang sama.
func (c *Client) TranslateVTT(ctx context.Context, vtt, from, to string) (Result, error) {
	key := cacheKey(vtt, from, to)
	if data, ok := cached(key); ok {
		return Result{Key: key, VTT: data, Cues: strings.Count(data, "-->"), Cached: true}, nil
	}
	out, cues, err := c.translateVTT(ctx, vtt, from, to)
	if err != nil {
		return Result{}, err
	}
	_ = os.MkdirAll(dir, 0o755)
	_ = os.WriteFile(filepath.Join(dir, key+".vtt"), []byte(out), 0o644)
	return Result{Key: key, VTT: out, Cues: cues}, nil
}

// Cached mengembalikan terjemahan yang sudah tersimpan untuk kunci cache.
func Cached(key string) (string, bool) { return cached(key) }

func cached(key string) (string, bool) {
	if !validKey(key) {
		return "", false
	}
	data, err := os.ReadFile(filepath.Join(dir, key+".vtt"))
	if err != nil || len(data) == 0 {
		return "", false
	}
	return string(data), true
}

var keyRe = regexp.MustCompile(`^[0-9a-f]{40}$`)

func validKey(key string) bool { return keyRe.MatchString(key) }

func cacheKey(vtt, from, to string) string {
	sum := sha1.Sum([]byte(from + "|" + to + "|" + vtt))
	return hex.EncodeToString(sum[:])
}

// translateVTT memecah VTT jadi blok, menerjemahkan teks tiap cue, lalu
// menyusun ulang dengan timestamp aslinya.
func (c *Client) translateVTT(ctx context.Context, vtt, from, to string) (string, int, error) {
	blocks := splitBlocks(vtt)

	type cue struct {
		block, at, end int
		text           string
	}
	var cues []cue
	for bi, b := range blocks {
		timing := -1
		for i, ln := range b {
			if strings.Contains(ln, "-->") {
				timing = i
				break
			}
		}
		if timing < 0 || timing == len(b)-1 {
			continue
		}
		text := strings.TrimSpace(strings.Join(b[timing+1:], "\n"))
		if text == "" {
			continue
		}
		cues = append(cues, cue{block: bi, at: timing + 1, end: len(b), text: text})
	}
	if len(cues) == 0 {
		return "", 0, fmt.Errorf("subtitle tanpa cue teks")
	}

	// Semua cue diterjemahkan serentak (satu goroutine per cue, tanpa batas
	// worker): satu cue = satu permintaan, jadi ini yang menentukan durasi.
	out := make([]string, len(cues))
	errs := make([]error, len(cues))
	var wg sync.WaitGroup
	for i := range cues {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			out[i], errs[i] = c.text(ctx, from, to, cues[i].text)
		}(i)
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			return "", 0, fmt.Errorf("cue %d: %w", i+1, err)
		}
	}

	for i, cu := range cues {
		translated := out[i]
		if strings.TrimSpace(translated) == "" {
			translated = cu.text
		}
		blocks[cu.block] = append(append([]string{}, blocks[cu.block][:cu.at]...), strings.Split(translated, "\n")...)
	}
	var sb strings.Builder
	for i, b := range blocks {
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString(strings.Join(b, "\n"))
		sb.WriteString("\n")
	}
	return sb.String(), len(cues), nil
}

// splitBlocks memecah teks jadi blok yang dipisahkan baris kosong.
func splitBlocks(body string) [][]string {
	lines := strings.Split(strings.ReplaceAll(body, "\r\n", "\n"), "\n")
	var blocks [][]string
	var cur []string
	for _, ln := range lines {
		if strings.TrimSpace(ln) == "" {
			if len(cur) > 0 {
				blocks = append(blocks, cur)
				cur = nil
			}
			continue
		}
		cur = append(cur, ln)
	}
	if len(cur) > 0 {
		blocks = append(blocks, cur)
	}
	return blocks
}

// text menerjemahkan satu potongan teks (satu cue) lewat endpoint Google.
func (c *Client) text(ctx context.Context, from, to, s string) (string, error) {
	var last error
	for attempt := 0; attempt < 2; attempt++ {
		res, err := c.http.R().SetContext(ctx).
			SetQueryParams(map[string]string{
				"client": clientParam,
				"sl":     from,
				"tl":     to,
				"dt":     "t",
				"q":      s,
			}).Get(c.Base)
		if err != nil {
			last = err
			continue
		}
		if res.StatusCode() != 200 {
			last = fmt.Errorf("translate HTTP %s", res.Status())
			continue
		}
		out, err := parseSingle(res.Body())
		if err != nil {
			last = err
			continue
		}
		return out, nil
	}
	return "", last
}

// parseSingle membaca jawaban translate_a/single: [[["hasil","asal",...],...],...]
func parseSingle(body []byte) (string, error) {
	var outer []json.RawMessage
	if err := json.Unmarshal(body, &outer); err != nil || len(outer) == 0 {
		return "", fmt.Errorf("jawaban terjemahan tidak dikenal")
	}
	var segments [][]json.RawMessage
	if err := json.Unmarshal(outer[0], &segments); err != nil {
		return "", fmt.Errorf("segmen terjemahan tidak dikenal: %w", err)
	}
	var sb strings.Builder
	for _, seg := range segments {
		if len(seg) == 0 {
			continue
		}
		var part string
		if err := json.Unmarshal(seg[0], &part); err == nil {
			sb.WriteString(part)
		}
	}
	if sb.Len() == 0 {
		return "", fmt.Errorf("terjemahan kosong")
	}
	return sb.String(), nil
}
