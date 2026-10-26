package mkv

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type countingReader struct {
	r io.Reader
	n int64
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += int64(n)
	return n, err
}

// buatMKV: MKV 8 dtk + subtitle tertanam + lampiran font 8 MB.
// Lampiran inilah yang membuat ffprobe membaca puluhan MB; parser kita harus berhenti
// sebelum menyentuhnya.
func buatMKV(t *testing.T, dir string, denganLampiran bool) string {
	t.Helper()
	ffmpegBin, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg tidak ada")
	}
	srt := filepath.Join(dir, "sub.srt")
	if err := os.WriteFile(srt, []byte("1\n00:00:01,000 --> 00:00:03,000\nHalo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	font := filepath.Join(dir, "font.bin")
	if err := os.WriteFile(font, bytes.Repeat([]byte("F"), 8<<20), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "episode.mkv")
	args := []string{"-y", "-loglevel", "error",
		"-f", "lavfi", "-i", "testsrc=duration=8:size=320x240:rate=25",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=8",
		"-i", srt,
		"-map", "0:v", "-map", "1:a", "-map", "2:s",
		"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p",
		"-c:a", "aac", "-c:s", "srt",
		"-metadata:s:s:0", "language=ind", "-metadata:s:s:0", "title=Indonesia",
	}
	if denganLampiran {
		args = append(args, "-attach", font, "-metadata:s:t", "mimetype=application/octet-stream")
	}
	args = append(args, "-f", "matroska", out)
	if o, err := exec.Command(ffmpegBin, args...).CombinedOutput(); err != nil {
		t.Fatalf("gagal buat mkv: %v (%s)", err, o)
	}
	return out
}

func TestProbeBacaHanyaHeader(t *testing.T) {
	dir := t.TempDir()
	path := buatMKV(t, dir, true)
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	cr := &countingReader{r: f}
	info, err := Probe(cr)
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}
	if info.Duration < 7 || info.Duration > 9 {
		t.Fatalf("durasi harus ~8 dtk, dapat %.2f", info.Duration)
	}
	if len(info.Audio) != 1 {
		t.Fatalf("harus 1 audio, dapat %d", len(info.Audio))
	}
	if len(info.Subs) != 1 {
		t.Fatalf("harus 1 subtitle, dapat %d", len(info.Subs))
	}
	if info.Subs[0].Lang != "ind" || info.Subs[0].Title != "Indonesia" {
		t.Fatalf("metadata subtitle salah: %+v", info.Subs[0])
	}
	// inti: lampiran 8 MB tidak boleh tersentuh
	if cr.n > 256<<10 {
		t.Fatalf("hanya boleh baca header, tapi membaca %d byte", cr.n)
	}
	t.Logf("durasi=%.2f dtk, audio=%d, sub=%d, dibaca=%d byte (ffprobe butuh >8 MB)",
		info.Duration, len(info.Audio), len(info.Subs), cr.n)
}

// Index track harus sama dengan index stream ffmpeg, supaya `-map 0:N` benar.
func TestIndexSamaDenganFfprobe(t *testing.T) {
	ffprobeBin, err := exec.LookPath("ffprobe")
	if err != nil {
		t.Skip("ffprobe tidak ada")
	}
	dir := t.TempDir()
	path := buatMKV(t, dir, true)
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	info, err := Probe(f)
	if err != nil {
		t.Fatalf("Probe: %v", err)
	}

	out, err := exec.Command(ffprobeBin, "-v", "error", "-show_entries",
		"stream=index,codec_type", "-of", "csv=p=0", path).Output()
	if err != nil {
		t.Fatalf("ffprobe: %v", err)
	}
	indexByType := map[string][]string{}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		parts := strings.Split(strings.TrimSpace(line), ",")
		if len(parts) == 2 {
			indexByType[parts[1]] = append(indexByType[parts[1]], parts[0])
		}
	}
	cek := func(tipe string, tracks []Track) {
		idx := indexByType[tipe]
		if len(idx) != len(tracks) {
			t.Fatalf("%s: ffprobe %v vs parser %d track", tipe, idx, len(tracks))
		}
		for i := range tracks {
			if strings.TrimSpace(idx[i]) != itoa(tracks[i].Index) {
				t.Fatalf("%s index ke-%d: ffprobe %s vs parser %d", tipe, i, idx[i], tracks[i].Index)
			}
		}
	}
	cek("audio", info.Audio)
	cek("subtitle", info.Subs)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
