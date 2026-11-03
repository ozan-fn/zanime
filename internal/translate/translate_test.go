package translate

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"zanime/internal/pool"
)

func testClient(t *testing.T) *Client {
	t.Helper()
	return New()
}

// googleStub meniru endpoint batchexecute.
type googleStub struct {
	mu        sync.Mutex
	rpcHits   int
	failRPC   bool
	cookies   []string
	form      []url.Values
	query     []url.Values
	lastError string
	url       string
}

func (g *googleStub) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/_/TranslateWebserverUi/data/batchexecute", func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			g.lastError = err.Error()
		}
		g.mu.Lock()
		g.rpcHits++
		fail := g.failRPC
		g.cookies = append(g.cookies, r.Header.Get("Cookie"))
		g.form = append(g.form, r.PostForm)
		g.query = append(g.query, r.URL.Query())
		g.mu.Unlock()
		if fail {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if got := r.URL.Query().Get("rpcids"); got != rpcID {
			http.Error(w, "rpcids="+got, http.StatusBadRequest)
			return
		}
		text, err := stubText(r.PostForm.Get("f.req"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, ")]}'\n\n419\n%s\n25\n[[\"e\",4,null,null,419]]\n", stubBatch(text))
	})
	return mux
}

// stubText membaca teks sumber dari f.req.
func stubText(freq string) (string, error) {
	var outer []json.RawMessage
	if err := json.Unmarshal([]byte(freq), &outer); err != nil || len(outer) == 0 {
		return "", fmt.Errorf("f.req %q: %w", freq, err)
	}
	// f.req = [[["MkEWBc", "<inner>", null, "generic"]]]
	var batch []json.RawMessage
	if err := json.Unmarshal(outer[0], &batch); err != nil || len(batch) == 0 {
		return "", fmt.Errorf("batch %q: %w", freq, err)
	}
	var rpc []json.RawMessage
	if err := json.Unmarshal(batch[0], &rpc); err != nil || len(rpc) < 2 {
		return "", fmt.Errorf("rpc %q: %w", freq, err)
	}
	var innerJSON string
	if err := json.Unmarshal(rpc[1], &innerJSON); err != nil {
		return "", err
	}
	var inner []json.RawMessage
	if err := json.Unmarshal([]byte(innerJSON), &inner); err != nil || len(inner) == 0 {
		return "", fmt.Errorf("inner %q: %w", innerJSON, err)
	}
	var src []json.RawMessage
	if err := json.Unmarshal(inner[0], &src); err != nil || len(src) == 0 {
		return "", err
	}
	var text string
	if err := json.Unmarshal(src[0], &text); err != nil {
		return "", err
	}
	return text, nil
}

// stubBatch menyusun payload MkEWBc dengan satu kalimat hasil.
func stubBatch(text string) string {
	inner := fmt.Sprintf(
		`[null,[[[null,null,null,null,null,[[%q,null,null,null,null,null,%q,1]],null,null,null,[]]],"id",1,"en",[]],"id"]`,
		"[id] "+text, text,
	)
	line, _ := json.Marshal([]any{[]any{"wrb.fr", rpcID, inner}})
	return string(line)
}

func newStub(t *testing.T, c *Client) *googleStub {
	t.Helper()
	g := &googleStub{}
	srv := httptest.NewServer(g.handler())
	t.Cleanup(srv.Close)
	g.url = srv.URL
	c.Origin = srv.URL
	return g
}

// TestBatchExecuteTranslatesAndCaches: tiap cue lewat RPC tanpa cookie/at, dan
// permintaan kedua dilayani cache.
func TestBatchExecuteTranslatesAndCaches(t *testing.T) {
	t.Chdir(t.TempDir())
	c := testClient(t)
	g := newStub(t, c)
	// Endpoint publik tidak boleh terpakai selama RPC sehat.
	c.Base = g.url + "/publik"

	vtt := "WEBVTT\n\n1\n00:00:01.000 --> 00:00:02.000\nHello\n\n2\n00:00:03.000 --> 00:00:04.000\nBye\n"
	res, err := c.TranslateVTT(context.Background(), vtt, "en", "id")
	if err != nil {
		t.Fatalf("TranslateVTT: %v", err)
	}
	for _, want := range []string{"[id] Hello", "[id] Bye", "00:00:01.000 --> 00:00:02.000"} {
		if !strings.Contains(res.VTT, want) {
			t.Fatalf("VTT tanpa %q:\n%s", want, res.VTT)
		}
	}
	if g.rpcHits != 2 {
		t.Fatalf("rpc = %d, want 2 (satu per cue)", g.rpcHits)
	}
	if got := g.cookies[0]; got != "" {
		t.Fatalf("permintaan mengirim cookie %q, want kosong", got)
	}
	if _, ok := g.form[0]["at"]; ok {
		t.Fatalf("form mengirim at: %v", g.form[0])
	}
	if got := g.query[0].Get("rpcids"); got != rpcID {
		t.Fatalf("rpcids = %q", got)
	}
	if got := g.query[0].Get("source-path"); got != "/" {
		t.Fatalf("source-path = %q", got)
	}

	// Permintaan kedua (konten sama) dilayani cache: tanpa RPC baru.
	if _, err := c.TranslateVTT(context.Background(), vtt, "en", "id"); err != nil {
		t.Fatal(err)
	}
	if g.rpcHits != 2 {
		t.Fatalf("cache menghubungi upstream: rpc=%d", g.rpcHits)
	}
}

// TestTranslateFallsBackWhenRPCFails: RPC gagal → endpoint publik dipakai.
func TestTranslateFallsBackWhenRPCFails(t *testing.T) {
	t.Chdir(t.TempDir())
	c := testClient(t)
	g := newStub(t, c)
	g.failRPC = true
	var hits atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		_ = json.NewEncoder(w).Encode([]any{[]any{[]any{"publik " + r.URL.Query().Get("q")}}})
	}))
	defer up.Close()
	c.Base = up.URL

	out, err := c.text(context.Background(), "en", "id", "Hello")
	if err != nil {
		t.Fatal(err)
	}
	if out != "publik Hello" {
		t.Fatalf("hasil = %q", out)
	}
	if g.rpcHits != 1 {
		t.Fatalf("rpc = %d, want 1", g.rpcHits)
	}
	if hits.Load() != 1 {
		t.Fatalf("publik = %d, want 1", hits.Load())
	}
}

// TestTranslateVTTPreservesTimingAndCaches: tiap cue diterjemahkan, timestamp
// dan struktur VTT dipertahankan, dan permintaan kedua dilayani cache.
func TestTranslateVTTPreservesTimingAndCaches(t *testing.T) {
	t.Chdir(t.TempDir())
	var hits atomic.Int32
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		q := r.URL.Query().Get("q")
		_ = json.NewEncoder(w).Encode([]any{[]any{[]any{"[id] " + q}}})
	}))
	defer up.Close()

	c := testClient(t)
	c.Base = up.URL
	g := newStub(t, c) // RPC gagal → jalur publik
	g.failRPC = true
	vtt := "WEBVTT\n\n1\n00:00:01.000 --> 00:00:02.000\nHello\nworld\n\n2\n00:00:03.000 --> 00:00:04.000\nBye\n"

	res, err := c.TranslateVTT(context.Background(), vtt, "en", "id")
	if err != nil {
		t.Fatalf("TranslateVTT: %v", err)
	}
	if res.Cached || res.Cues != 2 {
		t.Fatalf("hasil = %+v, want 2 cue bukan cache", res)
	}
	for _, want := range []string{
		"WEBVTT",
		"00:00:01.000 --> 00:00:02.000",
		"[id] Hello\nworld",
		"00:00:03.000 --> 00:00:04.000",
		"[id] Bye",
	} {
		if !strings.Contains(res.VTT, want) {
			t.Fatalf("VTT tanpa %q:\n%s", want, res.VTT)
		}
	}
	if got := hits.Load(); got != 2 {
		t.Fatalf("permintaan terjemahan = %d, want 2", got)
	}

	again, err := c.TranslateVTT(context.Background(), vtt, "en", "id")
	if err != nil {
		t.Fatalf("TranslateVTT (cache): %v", err)
	}
	if !again.Cached || again.VTT != res.VTT {
		t.Fatalf("cache tidak dipakai: %+v", again)
	}
	if got := hits.Load(); got != 2 {
		t.Fatalf("cache tetap menghubungi upstream: %d permintaan", got)
	}
	if cached, ok := Cached(res.Key); !ok || cached != res.VTT {
		t.Fatal("Cached tidak mengembalikan hasil tersimpan")
	}
}

// TestTranslateCuesRunInWorkerPool: cue yang sedang diterjemahkan terlihat
// sebagai worker aktif (dipakai badge jumlah worker di web).
func TestTranslateCuesRunInWorkerPool(t *testing.T) {
	t.Chdir(t.TempDir())
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		once.Do(func() { close(entered) })
		<-release
		_ = json.NewEncoder(w).Encode([]any{[]any{[]any{"halo"}}})
	}))
	defer up.Close()

	c := testClient(t)
	c.Base = up.URL
	g := newStub(t, c) // RPC gagal → jalur publik
	g.failRPC = true
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = c.TranslateVTT(context.Background(), "WEBVTT\n\n1\n00:00:01.000 --> 00:00:02.000\nHello\n", "en", "id")
	}()

	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		close(release)
		t.Fatal("permintaan terjemahan tidak masuk worker pool")
	}
	if got := pool.Get().RunningWorkers(); got < 1 {
		t.Fatalf("worker aktif saat menerjemahkan = %d, want >= 1", got)
	}

	close(release)
	<-done
}

// TestCachedRejectsBadKey: kunci yang bukan hex 40 karakter tidak dibaca dari
// disk (mencegah path traversal).
func TestCachedRejectsBadKey(t *testing.T) {
	for _, key := range []string{"", "../rahasia", "zzzz", strings.Repeat("a", 41)} {
		if _, ok := Cached(key); ok {
			t.Fatalf("kunci %q seharusnya ditolak", key)
		}
	}
}

// TestParseSingleJoinsSegments: jawaban Google bisa berisi beberapa segmen;
// semuanya digabung.
func TestParseSingleJoinsSegments(t *testing.T) {
	body := []byte(`[[["Halo ","Hello ","",null],["dunia","world",null,null]],null,"en"]`)
	got, err := parseSingle(body)
	if err != nil {
		t.Fatal(err)
	}
	if got != "Halo dunia" {
		t.Fatalf("parseSingle = %q", got)
	}
	if _, err := parseSingle([]byte(`[]`)); err == nil {
		t.Fatal("jawaban kosong harus error")
	}
}

// TestParseBatchRealResponse memakai bentuk jawaban batchexecute asli: awalan
// )]}', baris panjang, lalu inner JSON yang di-quote.
func TestParseBatchRealResponse(t *testing.T) {
	inner := `[[null,null,"id",[[[0,[[[null,10]],[true]]]],10],null,null,["Hello world. This is a test.","auto","id",true,null,2]],[[[null,null,null,null,null,[["Halo dunia.",null,null,null,null,null,"Hello world.",1],["Ini adalah ujian.",null,true,null,null,null,"This is a test.",1]],null,null,null,[]]],"id",1,"en",["Hello world. This is a test.","auto","id",true,null,2]],"id",null,null,null,null,[[[0]]]]`
	body := ")]}'\n\n419\n" + `[["wrb.fr","MkEWBc",` + fmt.Sprintf("%q", inner) + `,null,null,null,"generic"],["di",67]]` + "\n25\n[[\"e\",4,null,null,455]]\n"
	got, err := parseBatch([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if got != "Halo dunia. Ini adalah ujian." {
		t.Fatalf("parseBatch = %q", got)
	}
	if _, err := parseBatch([]byte(")]}'\n\n25\n[]\n")); err == nil {
		t.Fatal("jawaban tanpa wrb.fr harus error")
	}
}
