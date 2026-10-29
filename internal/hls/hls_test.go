package hls

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
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

// fakeFFmpeg writes a script that plays ffmpeg's part: it writes the playlist
// (its last argument) plus one segment next to it.
func fakeFFmpeg(t *testing.T) string {
	t.Helper()
	file := filepath.Join(t.TempDir(), "ffmpeg")
	script := strings.Join([]string{
		"#!/bin/sh",
		// Invokasi probe (`-i` tanpa output) tidak menulis apa pun.
		`case "$*" in *"-f hls"*) ;; *) exit 0 ;; esac`,
		`for a in "$@"; do last=$a; done`,
		`dir=$(dirname "$last")`,
		`printf '#EXTM3U\n#EXTINF:4.0,\nseg00000.ts\n' > "$last"`,
		`printf 'segment-bytes' > "$dir/seg00000.ts"`,
		"",
	}, "\n")
	if err := os.WriteFile(file, []byte(script), 0o755); err != nil {
		t.Fatalf("tulis ffmpeg palsu: %v", err)
	}
	return file
}

func waitReady(t *testing.T, m *Manager) Status {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if st, exists := m.Status(); exists && st.Ready {
			return st
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("playlist hls tidak muncul dalam batas waktu")
	return Status{}
}

func TestManagerServesPlaylistAndSegments(t *testing.T) {
	m := NewManager()
	t.Cleanup(m.Stop)
	m.Start(context.Background(), 3, "http://127.0.0.1:1/stream/3/x.mkv", fakeFFmpeg(t), true)

	if st := waitReady(t, m); st.Index != 3 || !st.Transcode || st.Playlist != "index.m3u8" {
		t.Fatalf("Status = %+v; want index 3, transcode true, index.m3u8", st)
	}

	playlist, ok := m.Path(3, "/index.m3u8")
	if !ok {
		t.Fatal("playlist tidak ditemukan")
	}
	data, err := os.ReadFile(playlist)
	if err != nil || !strings.Contains(string(data), "seg00000.ts") {
		t.Fatalf("playlist = %q (%v)", data, err)
	}
	segment, ok := m.Path(3, "seg00000.ts")
	if !ok {
		t.Fatal("segmen tidak ditemukan")
	}
	if bytes, err := os.ReadFile(segment); err != nil || string(bytes) != "segment-bytes" {
		t.Fatalf("segmen = %q (%v)", bytes, err)
	}
}

func TestManagerRejectsOtherIndexAndEscapes(t *testing.T) {
	m := NewManager()
	t.Cleanup(m.Stop)
	m.Start(context.Background(), 0, "http://127.0.0.1:1/stream/0/x.mkv", fakeFFmpeg(t), false)
	waitReady(t, m)

	if _, ok := m.Path(1, "index.m3u8"); ok {
		t.Fatal("index lain tidak boleh dilayani")
	}
	escaped, ok := m.Path(0, "../../etc/passwd")
	if !ok {
		t.Fatal("Path harus tetap mengembalikan path di dalam sesi")
	}
	playlist, _ := m.Path(0, "index.m3u8")
	dir := filepath.Dir(playlist)
	if !strings.HasPrefix(escaped, dir+string(filepath.Separator)) {
		t.Fatalf("path %q keluar dari sesi %q", escaped, dir)
	}
}

// Toggle transcode memulai sesi baru: index sama, mode berbeda.
func TestManagerRestartsOnModeChange(t *testing.T) {
	m := NewManager()
	t.Cleanup(m.Stop)
	m.Start(context.Background(), 0, "http://127.0.0.1:1/stream/0/x.mkv", fakeFFmpeg(t), false)
	waitReady(t, m)

	m.Start(context.Background(), 0, "http://127.0.0.1:1/stream/0/x.mkv", fakeFFmpeg(t), true)
	if st, exists := m.Status(); !exists || !st.Transcode {
		t.Fatalf("sesi transcode tidak berjalan: exists %v transcode %v", exists, st.Transcode)
	}
}

// Setiap track audio jadi rendition sendiri, jadi player punya menu audio.
func TestArgsForPublishesEachAudioTrack(t *testing.T) {
	args := strings.Join(argsFor("/tmp/sesi", "http://x/stream/0/a.mkv", false, []string{"eng", "jpn", "und"}), " ")
	for _, want := range []string{
		"-map 0:a:0", "-map 0:a:1", "-map 0:a:2",
		"-master_pl_name master.m3u8",
		"v:0,agroup:aud,name:video",
		"a:0,agroup:aud,name:audio0,language:eng,default:yes",
		"a:1,agroup:aud,name:audio1,language:jpn",
		"a:2,agroup:aud,name:audio2",
		"/tmp/sesi/%v/seg%05d.ts",
	} {
		if !strings.Contains(args, want) {
			t.Fatalf("argumen tanpa %q:\n%s", want, args)
		}
	}
}

// probeAudio membaca banner ffmpeg (bukan ffprobe) untuk menghitung track audio
// dan bahasanya. Butuh ffmpeg sungguhan; skip kalau tidak ada.
func TestProbeAudioReadsLanguageTags(t *testing.T) {
	ffmpeg, err := Ensure(context.Background())
	if err != nil {
		t.Skipf("ffmpeg tidak tersedia: %v", err)
	}
	clip := filepath.Join(t.TempDir(), "multi.mkv")
	gen := exec.Command(ffmpeg, "-y", "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "testsrc2=size=64x64:rate=5",
		"-f", "lavfi", "-i", "sine=frequency=440",
		"-f", "lavfi", "-i", "sine=frequency=880",
		"-t", "1", "-map", "0:v", "-map", "1:a", "-map", "2:a",
		"-c:v", "mpeg4", "-c:a", "aac",
		"-metadata:s:a:0", "language=eng", "-metadata:s:a:1", "language=jpn", clip)
	if out, err := gen.CombinedOutput(); err != nil {
		t.Skipf("tidak bisa membuat fixture: %v: %s", err, out)
	}

	info := probeMedia(context.Background(), ffmpeg, clip)
	if !slices.Equal(info.audio, []string{"eng", "jpn"}) {
		t.Fatalf("audio = %v, want [eng jpn]", info.audio)
	}
	if info.duration < 0.5 || info.duration > 1.5 {
		t.Fatalf("duration = %v, want ~1 dtk", info.duration)
	}
}

// Sesi nyata dengan ffmpeg: dua track audio menghasilkan master playlist yang
// menyebut kedua rendition, dan semua file di dalamnya bisa diambil.
func TestRealSessionWritesMultiAudioMaster(t *testing.T) {
	ffmpeg, err := Ensure(context.Background())
	if err != nil {
		t.Skipf("ffmpeg tidak tersedia: %v", err)
	}
	clip := filepath.Join(t.TempDir(), "multi.mp4")
	gen := exec.Command(ffmpeg, "-y", "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "testsrc2=size=64x64:rate=5",
		"-f", "lavfi", "-i", "sine=frequency=440",
		"-f", "lavfi", "-i", "sine=frequency=880",
		"-t", "2", "-map", "0:v", "-map", "1:a", "-map", "2:a",
		"-c:v", "mpeg4", "-c:a", "aac",
		"-metadata:s:a:0", "language=eng", "-metadata:s:a:1", "language=jpn", clip)
	if out, err := gen.CombinedOutput(); err != nil {
		t.Skipf("tidak bisa membuat fixture: %v: %s", err, out)
	}

	m := NewManager()
	t.Cleanup(m.Stop)
	m.Start(context.Background(), 0, clip, ffmpeg, false)
	if st := waitReady(t, m); st.Playlist != "master.m3u8" {
		t.Fatalf("playlist = %q, want master.m3u8", st.Playlist)
	}

	master, _ := m.Path(0, "master.m3u8")
	data, err := os.ReadFile(master)
	if err != nil {
		t.Fatalf("baca master: %v", err)
	}
	if got := strings.Count(string(data), "#EXT-X-MEDIA:TYPE=AUDIO"); got != 2 {
		t.Fatalf("jumlah rendition audio = %d, want 2:\n%s", got, data)
	}
	for _, name := range []string{"video/index.m3u8", "audio0/index.m3u8", "audio1/index.m3u8"} {
		file, ok := m.Path(0, name)
		if !ok {
			t.Fatalf("%s tidak bisa diambil", name)
		}
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("%s tidak ada: %v", name, err)
		}
		// VOD: playlist lengkap dengan ENDLIST, jadi player tahu durasi dan seek.
		if !strings.Contains(string(data), "#EXT-X-PLAYLIST-TYPE:VOD") || !strings.Contains(string(data), "#EXT-X-ENDLIST") {
			t.Fatalf("%s bukan playlist VOD:\n%s", name, data)
		}
	}
}

func TestScanProgressFromFFmpegOutput(t *testing.T) {
	j := &Job{}
	scanProgress(strings.NewReader("frame=10\nout_time_us=5000000\nprogress=continue\nout_time_us=20000000\nprogress=end\n"), j, 10)
	// 10 detik durasi: 5 dtk = 500 permil, lalu 20 dtk dipotong ke 999.
	if got := j.progress.Load(); got != 999 {
		t.Fatalf("progress = %d permil, want 999", got)
	}
}

func TestArgsForSingleAudio(t *testing.T) {
	args := strings.Join(argsFor("/tmp/sesi", "http://x/stream/0/a.mkv", true, []string{"eng"}), " ")
	if strings.Contains(args, "-master_pl_name") || strings.Contains(args, "%v") {
		t.Fatalf("satu audio tidak boleh memakai master playlist: %s", args)
	}
	for _, want := range []string{"-map 0:a:0", "libx264", "index.m3u8"} {
		if !strings.Contains(args, want) {
			t.Fatalf("argumen tanpa %q:\n%s", want, args)
		}
	}
}
