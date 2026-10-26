package torrent_test

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	ztorrent "zanime/internal/torrent"
	"zanime/internal/web"
)

type kuotaStatus struct {
	Ready       bool                  `json:"ready"`
	Err         string                `json:"err"`
	Downloaded  int64                 `json:"downloaded"`
	Duration    float64               `json:"duration"`
	Probe       struct{ Done bool }   `json:"probe"`
	Audio       []struct{ Index int } `json:"audio"`
	SubsEmb     []struct{ Index int } `json:"subsEmb"`
	SubsCached  []string              `json:"subsCached"`
	SubsPartial map[string]int64      `json:"subsPartial"`
	SubsErr     map[string]string     `json:"subsErr"`
	Video       struct{ Idx int }     `json:"video"`
}

func kuotaGet(t *testing.T, base, hash string) kuotaStatus {
	t.Helper()
	resp, err := http.Get(base + "/api/torrent/status/" + hash)
	if err != nil {
		return kuotaStatus{}
	}
	defer func() { _ = resp.Body.Close() }()
	var st kuotaStatus
	_ = json.NewDecoder(resp.Body).Decode(&st)
	return st
}

func kuotaWait(t *testing.T, base, hash string, limit time.Duration, ok func(kuotaStatus) bool) kuotaStatus {
	t.Helper()
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if st := kuotaGet(t, base, hash); ok(st) {
			return st
		}
		time.Sleep(500 * time.Millisecond)
	}
	t.Fatalf("tidak tercapai dalam %s", limit)
	return kuotaStatus{}
}

// TestKuotaMagnet: ukur MB yang dihabiskan untuk tiap tahap dari satu magnet nyata.
// Butuh internet (DHT/tracker). Jalankan:
//
//	MAGNET='magnet:?xt=urn:btih:...&tr=...' go test ./internal/torrent/ -run TestKuotaMagnet -v -timeout 20m
func TestKuotaMagnet(t *testing.T) {
	magnet := strings.TrimSpace(os.Getenv("MAGNET"))
	if magnet == "" {
		t.Skip(`set MAGNET="magnet:?xt=urn:btih:..." untuk mengukur kuota`)
	}
	u, err := url.Parse(magnet)
	if err != nil || u.Scheme != "magnet" {
		t.Fatalf("magnet tidak valid: %v", err)
	}
	hash := strings.TrimPrefix(u.Query().Get("xt"), "urn:btih:")
	if len(hash) != 40 {
		t.Fatalf("hash magnet tidak valid: %q", hash)
	}

	t.Setenv("ZANIME_TORRENT_DATA_DIR", t.TempDir()) // jangan tinggalkan data di folder paket
	t.Setenv("ZANIME_CACHE_DIR", t.TempDir())
	mgr, err := ztorrent.New()
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Close()
	srv := httptest.NewServer(web.New(mgr).Routes())
	defer srv.Close()

	start := time.Now()
	if _, err := mgr.Start(hash, u.Query()["tr"], -1); err != nil {
		t.Fatal(err)
	}
	mark := func(tahap string, st kuotaStatus) {
		t.Logf("%-24s %7.2f MB  %6.1f dtk", tahap, float64(st.Downloaded)/(1<<20), time.Since(start).Seconds())
	}

	// 1. metadata torrent + daftar file
	st := kuotaWait(t, srv.URL, hash, 2*time.Minute, func(s kuotaStatus) bool { return s.Ready || s.Err != "" })
	if !st.Ready {
		t.Fatalf("torrent tidak siap: %q", st.Err)
	}
	mark("1. metadata", st)

	// 2. probe = durasi + daftar audio/subtitle (hanya header)
	st = kuotaWait(t, srv.URL, hash, 3*time.Minute, func(s kuotaStatus) bool { return s.Probe.Done })
	mark("2. durasi + track", st)
	t.Logf("   durasi=%.0f dtk, audio=%d, subtitle=%d", st.Duration, len(st.Audio), len(st.SubsEmb))

	// 3. video mulai: baca 1 MB pertama dari /stream. Subtitle tertanam ikut pass ini
	// (tidak ada pass kedua), jadi biaya subtitle ≈ 0 MB tambahan.
	streamURL := srv.URL + "/stream/" + hash + "/" + strconv.Itoa(st.Video.Idx)
	resp, err := http.Get(streamURL)
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	_, _ = io.CopyN(io.Discard, resp.Body, 1<<20)
	_ = resp.Body.Close()
	st = kuotaGet(t, srv.URL, hash)
	mark("3. video", st)

	// 4. subtitle dari pass video yang sama (boleh sebagian: cue yang sudah utuh)
	if len(st.SubsEmb) > 0 {
		ref := "t" + strconv.Itoa(st.SubsEmb[0].Index)
		subURL := srv.URL + "/subs/" + hash + "/" + strconv.Itoa(st.Video.Idx) + "/" + ref + ".vtt"
		if sr, serr := http.Get(subURL); serr == nil {
			body, _ := io.ReadAll(sr.Body)
			_ = sr.Body.Close()
			st = kuotaGet(t, srv.URL, hash)
			mark("4. subtitle", st)
			t.Logf("   %s: status %d, %d byte VTT (%d byte sedang dialirkan)",
				ref, sr.StatusCode, len(body), st.SubsPartial[ref])
		}
	} else {
		t.Log("4. subtitle: tidak ada subtitle tertanam")
	}

	// ringkasan untuk dibaca cepat
	t.Logf("TOTAL sampai video mulai: %.2f MB", float64(kuotaGet(t, srv.URL, hash).Downloaded)/(1<<20))
}
