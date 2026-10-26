package torrent_test

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	atorrent "github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"golang.org/x/time/rate"

	ztorrent "zanime/internal/torrent"
	"zanime/internal/web"
)

// readyDeadline: batas torrent siap — 20 detik sesuai permintaan.
const readyDeadline = 20 * time.Second

// freePort ambil port kosong lalu lepas — untuk seeder lokal.
func freePort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := l.Addr().(*net.TCPAddr).Port
	_ = l.Close()
	return port
}

type statusResp struct {
	Ready      bool    `json:"ready"`
	Err        string  `json:"err"`
	Peers      int     `json:"peers"`
	Seeders    int     `json:"seeders"`
	Downloaded int64   `json:"downloaded"`
	Total      int64   `json:"total"`
	DownBps    float64 `json:"downBps"`
	UpBps      float64 `json:"upBps"`
	Served     int64   `json:"served"`
	Video      struct {
		Idx       int    `json:"idx"`
		Container string `json:"container"`
	} `json:"video"`
	SubFiles []struct {
		Idx  int    `json:"idx"`
		Name string `json:"name"`
	} `json:"subFiles"`
	Probe struct {
		Started bool   `json:"started"`
		Done    bool   `json:"done"`
		Err     string `json:"err"`
	} `json:"probe"`
}

func getStatus(t *testing.T, base, hash string) statusResp {
	t.Helper()
	resp, err := http.Get(base + "/api/torrent/status/" + hash)
	if err != nil {
		return statusResp{}
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	var st statusResp
	if resp.StatusCode == 200 {
		_ = json.Unmarshal(body, &st)
	}
	return st
}

func fetchBody(t *testing.T, url string) (int, []byte) {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, body
}

// TestEndToEndVideoLoad: alur penuh seperti browser —
// start torrent (tracker lokal) → status ready ≤20s → GET /stream →
// byte video keluar PROGRESIF (belum terunduh penuh) → kecepatan turun/naik →
// subtitle (Indonesia) bisa diambil → Range/seek → stop.
func TestEndToEndVideoLoad(t *testing.T) {
	ffmpegBin, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg tidak ada — lewati test video")
	}

	// 1. video ~6MB (60 detik) + subtitle Indonesia, diseed dari folder
	root := t.TempDir()
	seederDir := filepath.Join(root, "seed")
	if mkErr := os.MkdirAll(seederDir, 0o755); mkErr != nil {
		t.Fatal(mkErr)
	}
	videoPath := filepath.Join(seederDir, "test.mp4")
	gen := func(codec string) *exec.Cmd {
		return exec.Command(ffmpegBin, "-y", "-loglevel", "error",
			"-f", "lavfi", "-i", "testsrc=duration=60:size=640x480:rate=30",
			"-pix_fmt", "yuv420p", "-b:v", "800k", "-c:v", codec, videoPath)
	}
	if out, gerr := gen("libx264").CombinedOutput(); gerr != nil {
		if out2, err2 := gen("mpeg4").CombinedOutput(); err2 != nil {
			t.Fatalf("gagal buat video: %v (%s) / %v (%s)", gerr, out, err2, out2)
		}
	}
	want, err := os.ReadFile(videoPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(want) < 1024 {
		t.Fatalf("video terlalu kecil: %d byte", len(want))
	}
	subPath := filepath.Join(seederDir, "Test.Indonesia.srt")
	subText := "1\n00:00:00,500 --> 00:00:02,000\nHalo dari subtitle Indonesia\n\n" +
		"2\n00:00:02,500 --> 00:00:04,000\nBaris kedua\n"
	if err := os.WriteFile(subPath, []byte(subText), 0o644); err != nil {
		t.Fatal(err)
	}

	// 2. tracker lokal — balas peer seeder, tanpa internet
	seederPort := freePort(t)
	peer := make([]byte, 6)
	copy(peer[:4], net.IPv4(127, 0, 0, 1).To4())
	binary.BigEndian.PutUint16(peer[4:], uint16(seederPort))
	var announces atomic.Int64
	tracker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if n := announces.Add(1); n <= 4 {
			t.Logf("tracker announce #%d: %s", n, r.URL.RawQuery)
		}
		respPeer := peer
		// jangan beri alamat seeder ke announce milik seeder sendiri (hindari self-dial)
		if r.URL.Query().Get("port") == strconv.Itoa(seederPort) {
			respPeer = []byte{}
		}
		body, mErr := bencode.Marshal(map[string]any{
			"interval": int64(60),
			"complete": int64(1),
			"peers":    respPeer,
		})
		if mErr != nil {
			http.Error(w, mErr.Error(), 500)
			return
		}
		_, _ = w.Write(body)
	}))
	defer tracker.Close()

	// 3. seeder dengan upload dibatasi (300KB/s) supaya terbukti
	//    video diserve sebelum unduhan selesai
	var info metainfo.Info
	info.PieceLength = 64 * 1024 // kecil → ambil 1 subtitle hanya butuh ~64-128KB
	if err := info.BuildFromFilePath(seederDir); err != nil {
		t.Fatal(err)
	}
	subIdx := -1
	for i, f := range info.Files {
		if strings.HasSuffix(strings.ToLower(strings.Join(f.Path, "/")), ".srt") && subIdx < 0 {
			subIdx = i
		}
	}
	if subIdx < 0 {
		t.Fatalf("subtitle tidak masuk torrent: %+v", info.Files)
	}
	infoBytes, err := bencode.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	mi := &metainfo.MetaInfo{
		InfoBytes:    infoBytes,
		Announce:     tracker.URL,
		AnnounceList: metainfo.AnnounceList{{tracker.URL}},
	}
	hash := mi.HashInfoBytes().HexString()

	sCfg := atorrent.NewDefaultClientConfig()
	sCfg.DataDir = root
	sCfg.ListenPort = seederPort
	sCfg.NoDHT = true
	sCfg.DisablePEX = true
	sCfg.AlwaysWantConns = true
	sCfg.Seed = true // WAJIB: default anacrolix "tidak altruistik" → tanpa ini seeder tidak kirim data
	sCfg.UploadRateLimiter = rate.NewLimiter(300*1024, 64*1024)
	seeder, err := atorrent.NewClient(sCfg)
	if err != nil {
		t.Fatal(err)
	}
	defer seeder.Close()
	if _, err := seeder.AddTorrent(mi); err != nil {
		t.Fatal(err)
	}

	// 4. server aplikasi penuh (API + stream) seperti `go run .`
	//    mode hemat kuota: tanja DHT/PEX — test ini murni localhost (0 byte internet)
	t.Setenv("ZANIME_TORRENT_NO_DHT", "1")
	t.Setenv("ZANIME_TORRENT_DATA_DIR", t.TempDir())
	t.Setenv("ZANIME_CACHE_DIR", t.TempDir()) // folder bersih: tidak memakai unduhan run sebelumnya
	mgr, err := ztorrent.New()
	if err != nil {
		t.Fatal(err)
	}
	defer mgr.Close()
	if _, err := mgr.Start(hash, []string{tracker.URL}, -1); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(web.New(mgr).Routes())
	defer srv.Close()

	// 5. poll status sampai ready — batas 20 detik
	deadline := time.Now().Add(readyDeadline)
	var st statusResp
	for time.Now().Before(deadline) {
		st = getStatus(t, srv.URL, hash)
		if st.Ready {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	if !st.Ready {
		t.Fatalf("torrent tidak siap dalam %s (err=%q, peers=%d, seeders=%d, announce=%d)",
			readyDeadline, st.Err, st.Peers, st.Seeders, announces.Load())
	}
	if st.Video.Idx < 0 {
		t.Fatalf("video tidak ditemukan: %+v", st.Video)
	}
	t.Logf("siap dalam batas: idx=%d container=%s peers=%d", st.Video.Idx, st.Video.Container, st.Peers)

	// 6. GET /stream — byte pertama harus keluar sebelum unduhan selesai
	streamURL := srv.URL + "/stream/" + hash + "/" + strconv.Itoa(st.Video.Idx)
	type getRes struct {
		resp *http.Response
		err  error
	}
	gCh := make(chan getRes, 1)
	go func() {
		resp, gErr := http.Get(streamURL)
		gCh <- getRes{resp, gErr}
	}()
	var res getRes
	select {
	case res = <-gCh:
	case <-time.After(6 * time.Second):
		for i := 0; i < 4; i++ {
			q := getStatus(t, srv.URL, hash)
			t.Logf("poll hang: ready=%v peers=%d seeders=%d dl=%d/%d",
				q.Ready, q.Peers, q.Seeders, q.Downloaded, q.Total)
			time.Sleep(1 * time.Second)
		}
		t.Fatalf("GET /stream tidak balas dalam 6 detik")
	}
	resp, err := res.resp, res.err
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("status stream %d: %s", resp.StatusCode, body)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "video/mp4" {
		t.Fatalf("content-type salah: %q", ct)
	}
	head := make([]byte, 64*1024)
	n, err := io.ReadFull(resp.Body, head)
	if err != nil && err != io.ErrUnexpectedEOF {
		t.Fatalf("baca byte pertama: %v", err)
	}
	head = head[:n]

	// bukti progresif: sedang streaming, tapi unduhan belum penuh
	mid := getStatus(t, srv.URL, hash)
	if mid.Total > 0 && mid.Downloaded >= mid.Total {
		t.Fatalf("video diunduh full dulu (%d/%d) — seharusnya streaming progresif",
			mid.Downloaded, mid.Total)
	}
	if !bytes.Equal(head, want[:len(head)]) {
		t.Fatalf("prefix video tidak cocok (%d byte)", len(head))
	}
	t.Logf("streaming progresif: %d byte terlayani, unduhan %d/%d (%.1f%%)",
		len(head), mid.Downloaded, mid.Total, float64(mid.Downloaded)/float64(mid.Total)*100)

	// 6b. kecepatan: terus baca di background supaya server tetap mengirim,
	//     lalu cek downBps (peer→server) & upBps (server→client) > 0
	go func() {
		_, _ = io.CopyN(io.Discard, resp.Body, 512*1024)
	}()
	time.Sleep(700 * time.Millisecond)
	_ = getStatus(t, srv.URL, hash) // sampel pertama
	time.Sleep(700 * time.Millisecond)
	spd := getStatus(t, srv.URL, hash) // sampel kedua → rate terhitung
	if spd.DownBps <= 0 {
		t.Fatalf("downBps (peer→server) harus > 0, dapat %v", spd.DownBps)
	}
	if spd.UpBps <= 0 {
		t.Fatalf("upBps (server→client) harus > 0, dapat %v (served=%d)", spd.UpBps, spd.Served)
	}
	if spd.Served < int64(len(head)) {
		t.Fatalf("served %d < byte yang diterima %d", spd.Served, len(head))
	}
	t.Logf("kecepatan: down %.2f Mbps, up %.2f Mbps (served %d byte)",
		spd.DownBps*8/1e6, spd.UpBps*8/1e6, spd.Served)

	// 6c. subtitle: file .srt eksternal terdaftar & bisa diambil sebagai VTT
	if len(spd.SubFiles) == 0 {
		t.Fatalf("subFiles kosong — pemilih subtitle tidak akan terisi (probe err=%q)", spd.Probe.Err)
	}
	found := false
	for _, sf := range spd.SubFiles {
		if sf.Idx == subIdx {
			found = true
			if !strings.Contains(strings.ToLower(sf.Name), "indonesia") {
				t.Fatalf("nama sub tidak sesuai: %q", sf.Name)
			}
		}
	}
	if !found {
		t.Fatalf("subtitle idx %d tidak ada di subFiles %+v", subIdx, spd.SubFiles)
	}
	// tunggu probe selesai supaya pengukuran kuota subtitle tidak tercampur baca video
	probeDeadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(probeDeadline) {
		q := getStatus(t, srv.URL, hash)
		if q.Probe.Done {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	before := getStatus(t, srv.URL, hash).Downloaded
	code, vtt := fetchBody(t, srv.URL+"/subs/"+hash+"/"+strconv.Itoa(st.Video.Idx)+"/f"+strconv.Itoa(subIdx)+".vtt")
	afterSub := getStatus(t, srv.URL, hash).Downloaded
	quota := afterSub - before
	t.Logf("kuota untuk 1 subtitle: %d byte (video total %d byte)", quota, mid.Total)
	if quota > 1024*1024 {
		t.Fatalf("subtitle memakan %d byte (>1MB) — harusnya hanya potongan kecil file sub", quota)
	}
	if quota >= mid.Total {
		t.Fatalf("subtitle membaca seluruh video (%d/%d byte)", quota, mid.Total)
	}
	if code != 200 {
		t.Fatalf("subtitle status %d: %s", code, vtt)
	}
	if !bytes.HasPrefix(vtt, []byte("WEBVTT")) {
		t.Fatalf("subtitle bukan VTT: %q", vtt[:min(40, len(vtt))])
	}
	if !bytes.Contains(vtt, []byte("Halo dari subtitle Indonesia")) {
		t.Fatalf("isi subtitle hilang: %q", vtt)
	}

	// 7. dukungan seek: Range harus 206 + potongan tepat
	rReq, _ := http.NewRequest("GET", streamURL, nil)
	rReq.Header.Set("Range", "bytes=100-199")
	rResp, err := http.DefaultClient.Do(rReq)
	if err != nil {
		t.Fatal(err)
	}
	chunk, _ := io.ReadAll(rResp.Body)
	_ = rResp.Body.Close()
	if rResp.StatusCode != http.StatusPartialContent {
		t.Fatalf("Range harus 206, dapat %d", rResp.StatusCode)
	}
	if !bytes.Equal(chunk, want[100:200]) {
		t.Fatalf("potongan Range tidak cocok (%d byte)", len(chunk))
	}

	// 8. stop → status wajib 404 (menguji path stop juga)
	sResp, err := http.Get(srv.URL + "/api/torrent/stop/" + hash)
	if err != nil {
		t.Fatal(err)
	}
	_ = sResp.Body.Close()
	if sResp.StatusCode != 200 {
		t.Fatalf("stop gagal: %d", sResp.StatusCode)
	}
	after, err := http.Get(srv.URL + "/api/torrent/status/" + hash)
	if err != nil {
		t.Fatal(err)
	}
	_ = after.Body.Close()
	if after.StatusCode != http.StatusNotFound {
		t.Fatalf("setelah stop harus 404, dapat %d", after.StatusCode)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
