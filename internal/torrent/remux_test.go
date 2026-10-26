package torrent_test

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	atorrent "github.com/anacrolix/torrent"
	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"golang.org/x/time/rate"

	ztorrent "zanime/internal/torrent"
	"zanime/internal/web"
)

type remuxStatus struct {
	Ready bool   `json:"ready"`
	Err   string `json:"err"`
	Video struct {
		Idx       int    `json:"idx"`
		Container string `json:"container"`
	} `json:"video"`
	Remux *struct {
		Running bool   `json:"running"`
		Done    bool   `json:"done"`
		Err     string `json:"err"`
	} `json:"remux"`
	Probe struct {
		Done bool `json:"done"`
	} `json:"probe"`
	Duration float64 `json:"duration"`
	SubsEmb  []struct {
		Index int    `json:"index"`
		Lang  string `json:"lang"`
	} `json:"subsEmb"`
	SubsCached []string          `json:"subsCached"`
	SubsErr    map[string]string `json:"subsErr"`
}

func getRemuxStatus(t *testing.T, base, hash string) remuxStatus {
	t.Helper()
	resp, err := http.Get(base + "/api/torrent/status/" + hash)
	if err != nil {
		return remuxStatus{}
	}
	defer func() { _ = resp.Body.Close() }()
	var st remuxStatus
	_ = json.NewDecoder(resp.Body).Decode(&st)
	return st
}

// TestRemuxMkvStreams: MKV harus mulai diputar TANPA menunggu unduhan penuh
// (remux jalan di background → client mengejar file cache yang tumbuh),
// lalu setelah selesai, file cache faststart memberi durasi yang benar.
func TestRemuxMkvStreams(t *testing.T) {
	ffmpegBin, err := exec.LookPath("ffmpeg")
	ffprobeBin, perr := exec.LookPath("ffprobe")
	if err != nil || perr != nil {
		t.Skip("ffmpeg/ffprobe tidak ada")
	}

	// 1. MKV 10 dtk (h264 + aac) — butuh remux karena container bukan mp4
	root := t.TempDir()
	seedDir := filepath.Join(root, "seed")
	if mkErr := os.MkdirAll(seedDir, 0o755); mkErr != nil {
		t.Fatal(mkErr)
	}
	mkvPath := filepath.Join(seedDir, "test.mkv")
	// subtitle Indonesia tertanam di MKV — kasus nyata rilis fansub (ToonsHub dsb)
	srtPath := filepath.Join(root, "sub-ind.srt")
	srt := "1\n00:00:01,000 --> 00:00:04,000\nHalo dari subtitle Indonesia\n\n" +
		"2\n00:00:20,000 --> 00:00:24,000\nKotak saran?\n"
	if werr := os.WriteFile(srtPath, []byte(srt), 0o644); werr != nil {
		t.Fatal(werr)
	}
	out, gerr := exec.Command(ffmpegBin, "-y", "-loglevel", "error",
		"-f", "lavfi", "-i", "testsrc=duration=30:size=480x360:rate=25",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=30",
		"-i", srtPath,
		"-map", "0:v", "-map", "1:a", "-map", "2:s",
		"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-c:s", "srt",
		"-metadata:s:s:0", "language=ind", "-metadata:s:s:0", "title=Indonesia",
		"-t", "30", mkvPath).CombinedOutput()
	if gerr != nil {
		t.Fatalf("gagal buat mkv: %v (%s)", gerr, out)
	}
	if st, err := os.Stat(mkvPath); err != nil || st.Size() < 1024 {
		t.Fatalf("mkv tidak valid: %v", err)
	}

	// 2. tracker + seeder lokal + Manager/server aplikasi
	hash, base := seedAndServe(t, seedDir)

	// 3. tunggu siap (batas 20 dtk)
	deadline := time.Now().Add(readyDeadline)
	var st remuxStatus
	for time.Now().Before(deadline) {
		st = getRemuxStatus(t, base, hash)
		if st.Ready {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	if !st.Ready {
		t.Fatalf("tidak siap dalam %s: %q", readyDeadline, st.Err)
	}
	if st.Video.Container != "remux" {
		t.Fatalf("mkv harus lewat jalur remux, dapat container %q", st.Video.Container)
	}

	// 4. GET /stream: byte pertama harus keluar SEBELUM remux/unduhan selesai
	streamURL := base + "/stream/" + hash + "/" + strconv.Itoa(st.Video.Idx)
	type resT struct {
		resp *http.Response
		err  error
	}
	ch := make(chan resT, 1)
	go func() {
		r, e := http.Get(streamURL)
		ch <- resT{r, e}
	}()
	var res resT
	select {
	case res = <-ch:
	case <-time.After(15 * time.Second):
		q := getRemuxStatus(t, base, hash)
		t.Fatalf("GET /stream tidak balas 15 dtk (ready=%v remux=%v err=%q)", q.Ready, q.Remux, q.Err)
	}
	if res.err != nil {
		t.Fatal(res.err)
	}
	body := res.resp.Body
	defer func() { _ = body.Close() }()
	if res.resp.StatusCode != 200 {
		b, _ := io.ReadAll(body)
		t.Fatalf("status %d: %s", res.resp.StatusCode, b)
	}
	head := make([]byte, 32*1024)
	n, err := io.ReadFull(body, head)
	if err != nil && err != io.ErrUnexpectedEOF {
		t.Fatalf("baca byte awal: %v", err)
	}
	if n == 0 || !bytes.Contains(head[:n], []byte("ftyp")) {
		t.Fatalf("bukan mp4 di awal stream: %q", head[:min(16, n)])
	}
	mid := getRemuxStatus(t, base, hash)
	if mid.Remux == nil || !mid.Remux.Running {
		t.Fatalf("seharusnya remux masih berjalan saat video mulai — malah selesai (unduh penuh dulu?)")
	}
	t.Logf("mulai streaming sebelum selesai: %d byte, remux masih berjalan", n)

	// 5. habiskan stream: transmux langsung ke client, TANPA file cache di disk
	mp4 := append([]byte(nil), head[:n]...)
	rest, err := io.ReadAll(body)
	if err != nil {
		t.Fatalf("baca stream: %v", err)
	}
	mp4 = append(mp4, rest...)
	tmpMp4 := filepath.Join(t.TempDir(), "streamed.mp4")
	if err := os.WriteFile(tmpMp4, mp4, 0o644); err != nil {
		t.Fatal(err)
	}

	// 6. hasil transmux harus mp4 valid: video h264 + audio aac
	probeOut, err := exec.Command(ffprobeBin, "-v", "error",
		"-show_entries", "stream=codec_name", "-of", "default=nw=1:nk=1", tmpMp4).Output()
	if err != nil {
		t.Fatalf("ffprobe stream: %v", err)
	}
	if !bytes.Contains(probeOut, []byte("h264")) || !bytes.Contains(probeOut, []byte("aac")) {
		t.Fatalf("stream harus h264+aac, dapat: %s", probeOut)
	}
	t.Logf("stream mp4 valid: %d byte, codec=%s", len(mp4), bytes.Join(bytes.Fields(probeOut), []byte(",")))

	// durasi dikunci di header mvhd (fragmented mp4 bawaan menulis 0 → durasi dinamis)
	if d := mvhdDuration(t, tmpMp4); d < 29 || d > 31 {
		t.Fatalf("mvhd harus berisi durasi asli ±30 dtk, dapat %.2f", d)
	}
	t.Logf("durasi mvhd terkunci: %.1f dtk", mvhdDuration(t, tmpMp4))

	// tidak boleh ada cache MP4 di folder cache (penyimpanan terbatas)
	var mp4Cache []string
	_ = filepath.WalkDir(os.Getenv("ZANIME_CACHE_DIR"), func(p string, d fs.DirEntry, werr error) error {
		if werr == nil && !d.IsDir() && strings.HasSuffix(p, ".mp4") {
			mp4Cache = append(mp4Cache, p)
		}
		return nil
	})
	if len(mp4Cache) > 0 {
		t.Fatalf("tidak boleh ada cache MP4 di disk: %v", mp4Cache)
	}

	// 7. subtitle tertanam: ikut pass video (tanpa baca ulang) → subsCached + VTT 200.
	subDeadline := time.Now().Add(30 * time.Second)
	var got remuxStatus
	for time.Now().Before(subDeadline) {
		got = getRemuxStatus(t, base, hash)
		if len(got.SubsCached) > 0 {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	if len(got.SubsEmb) == 0 {
		t.Fatalf("subtitle tertanam tidak terdeteksi (probe err?)")
	}
	lang := ""
	for _, sb := range got.SubsEmb {
		if sb.Lang == "ind" {
			lang = "ind"
		}
	}
	if lang != "ind" {
		t.Fatalf("subtitle ind tidak terdeteksi: %+v", got.SubsEmb)
	}
	wantRef := "t" + strconv.Itoa(got.SubsEmb[0].Index)
	if len(got.SubsCached) == 0 {
		t.Fatalf("subtitle tidak siap otomatis (cached=%v err=%v)", got.SubsCached, got.SubsErr)
	}
	if !contains(got.SubsCached, wantRef) {
		t.Fatalf("subtitle %s tidak ada di subsCached %v", wantRef, got.SubsCached)
	}
	subURL := base + "/subs/" + hash + "/" + strconv.Itoa(got.Video.Idx) + "/" + wantRef + ".vtt"
	code2, vtt := fetchBody(t, subURL)
	if code2 != 200 {
		t.Fatalf("subtitle status %d: %s", code2, vtt)
	}
	if !bytes.HasPrefix(vtt, []byte("WEBVTT")) || !bytes.Contains(vtt, []byte("Halo dari subtitle Indonesia")) {
		t.Fatalf("VTT subtitle tertanam salah: %q", vtt)
	}
	if !bytes.Contains(vtt, []byte("00:20.")) || !bytes.Contains(vtt, []byte("00:24.")) {
		t.Fatalf("timestamp cue 20 dtk hilang: %q", vtt)
	}
	if bytes.Contains(vtt, []byte("\n\n\n")) {
		t.Fatalf("VTT punya baris kosong beruntun: %q", vtt)
	}
	if !bytes.HasPrefix(vtt, []byte("WEBVTT\n\n")) || bytes.Count(vtt, []byte("WEBVTT")) != 1 {
		t.Fatalf("header VTT tidak bersih: %q", vtt)
	}
	t.Logf("subtitle tertanam %s siap: %d byte VTT, cached=%v", wantRef, len(vtt), got.SubsCached)
}

// Regresi: MKV dengan audio AC3/E-AC3 dulu gagal total di remux
// ("Cannot write moov atom before EAC3 packets parsed") → sekarang audio jadi AAC.
func TestRemuxEac3Mkv(t *testing.T) {
	ffmpegBin, err := exec.LookPath("ffmpeg")
	ffprobeBin, perr := exec.LookPath("ffprobe")
	if err != nil || perr != nil {
		t.Skip("ffmpeg/ffprobe tidak ada")
	}
	root := t.TempDir()
	seedDir := filepath.Join(root, "seed")
	if mkErr := os.MkdirAll(seedDir, 0o755); mkErr != nil {
		t.Fatal(mkErr)
	}
	mkvPath := filepath.Join(seedDir, "eac3.mkv")
	out, gerr := exec.Command(ffmpegBin, "-y", "-loglevel", "error",
		"-f", "lavfi", "-i", "testsrc=duration=6:size=320x240:rate=25",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=6",
		"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p",
		"-c:a", "eac3", "-shortest", mkvPath).CombinedOutput()
	if gerr != nil {
		t.Fatalf("gagal buat mkv eac3: %v (%s)", gerr, out)
	}

	hash, base := seedAndServe(t, seedDir)
	deadline := time.Now().Add(readyDeadline)
	var st remuxStatus
	for time.Now().Before(deadline) {
		st = getRemuxStatus(t, base, hash)
		if st.Ready {
			break
		}
		time.Sleep(300 * time.Millisecond)
	}
	if !st.Ready {
		t.Fatalf("tidak siap dalam %s: %q", readyDeadline, st.Err)
	}
	if st.Video.Container != "remux" {
		t.Fatalf("mkv harus lewat remux, dapat container %q", st.Video.Container)
	}

	// stream langsung dari ffmpeg: audio AC3/E-AC3 harus otomatis jadi AAC
	streamURL := base + "/stream/" + hash + "/" + strconv.Itoa(st.Video.Idx)
	code, mp4 := fetchBody(t, streamURL)
	if code != 200 {
		t.Fatalf("stream status %d: %s", code, mp4)
	}
	final := filepath.Join(root, "streamed.mp4")
	if err := os.WriteFile(final, mp4, 0o644); err != nil {
		t.Fatal(err)
	}
	probeOut, err := exec.Command(ffprobeBin, "-v", "error",
		"-select_streams", "a:0", "-show_entries", "stream=codec_name",
		"-of", "default=nw=1:nk=1", final).Output()
	if err != nil {
		t.Fatalf("ffprobe stream: %v", err)
	}
	if got := strings.TrimSpace(string(probeOut)); got != "aac" {
		t.Fatalf("audio hasil stream harus aac, dapat %q", got)
	}
}

// Regresi "penyimpanan penuh": sisa data torrent versi lama yang melebihi batas dibuang
// saat start, jadi panel kecil tidak lagi penuh karena episode 2+ GiB tertinggal.
func TestCacheBersihkanSisaLama(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "[Lama] episode.mkv.part")
	if err := os.WriteFile(old, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Truncate(old, 300<<20); err != nil { // sparse: cepat dibuat, tetap dihitung 300 MB
		t.Fatal(err)
	}
	t.Setenv("ZANIME_TORRENT_DATA_DIR", dir)
	t.Setenv("ZANIME_CACHE_LIMIT_MB", "128")
	t.Setenv("ZANIME_TORRENT_NO_DHT", "1")

	mgr, err := ztorrent.New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer mgr.Close()

	if _, serr := os.Stat(old); !os.IsNotExist(serr) {
		t.Fatalf("sisa data lama harus dibuang saat start (stat err=%v)", serr)
	}
	var total int64
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, werr error) error {
		if werr == nil && !d.IsDir() {
			if info, ierr := d.Info(); ierr == nil {
				total += info.Size()
			}
		}
		return nil
	})
	if total > 128<<20 {
		t.Fatalf("cache %d byte melebihi batas 128 MB", total)
	}
	t.Logf("sisa %d MB dibuang saat start; total cache sekarang %d byte", 300, total)
}

// seedAndServe: seeder + tracker lokal untuk isi seedDir, lalu Manager + server
// HTTP seperti aplikasi asli (`go run .`). Mengembalikan hash torrent & base URL.
func seedAndServe(t *testing.T, seedDir string) (hash, base string) {
	t.Helper()
	root := filepath.Dir(seedDir)
	seederPort := freePort(t)
	peer := make([]byte, 6)
	copy(peer[:4], net.IPv4(127, 0, 0, 1).To4())
	binary.BigEndian.PutUint16(peer[4:], uint16(seederPort))
	tracker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		respPeer := peer
		if r.URL.Query().Get("port") == strconv.Itoa(seederPort) {
			respPeer = []byte{}
		}
		body, _ := bencode.Marshal(map[string]any{"interval": int64(60), "peers": respPeer})
		_, _ = w.Write(body)
	}))
	t.Cleanup(tracker.Close)

	var info metainfo.Info
	info.PieceLength = 64 * 1024
	if err := info.BuildFromFilePath(seedDir); err != nil {
		t.Fatal(err)
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
	hash = mi.HashInfoBytes().HexString()

	sCfg := atorrent.NewDefaultClientConfig()
	sCfg.DataDir = root
	sCfg.ListenPort = seederPort
	sCfg.NoDHT = true
	sCfg.DisablePEX = true
	sCfg.AlwaysWantConns = true
	sCfg.Seed = true
	sCfg.UploadRateLimiter = rate.NewLimiter(200*1024, 64*1024)
	seeder, err := atorrent.NewClient(sCfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = seeder.Close() })
	if _, err := seeder.AddTorrent(mi); err != nil {
		t.Fatal(err)
	}

	t.Setenv("ZANIME_TORRENT_NO_DHT", "1")
	t.Setenv("ZANIME_TORRENT_DATA_DIR", t.TempDir())
	t.Setenv("ZANIME_CACHE_DIR", t.TempDir()) // folder bersih: tidak memakai unduhan run sebelumnya
	mgr, err := ztorrent.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(mgr.Close)
	if _, err := mgr.Start(hash, []string{tracker.URL}, -1); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(web.New(mgr).Routes())
	t.Cleanup(srv.Close)
	return hash, srv.URL
}

// mvhdDuration: durasi (detik) dari box mvhd di header mp4.
func mvhdDuration(t *testing.T, path string) float64 {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var ts, dur uint32
	var walk func(start, end int)
	walk = func(start, end int) {
		for i := start; i+8 <= end; {
			size := int(binary.BigEndian.Uint32(b[i : i+4]))
			if size < 8 || i+size > end {
				return
			}
			switch string(b[i+4 : i+8]) {
			case "mvhd":
				if b[i+8] == 0 && i+28 <= end {
					ts = binary.BigEndian.Uint32(b[i+20 : i+24])
					dur = binary.BigEndian.Uint32(b[i+24 : i+28])
				}
				return
			case "moov", "trak", "mdia":
				walk(i+8, i+size)
			}
			i += size
		}
	}
	walk(0, len(b))
	if ts == 0 {
		return -1
	}
	return float64(dur) / float64(ts)
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
