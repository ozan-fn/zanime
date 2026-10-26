package ffmpeg

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
)

// TestRemuxDurasiTerkunci: header moov hasil transmux harus berisi durasi asli —
// fragmented mp4 bawaan menulis mvhd=0 sehingga player menampilkan durasi terus membesar.
func TestRemuxDurasiTerkunci(t *testing.T) {
	ffmpegBin, err := Path()
	if err != nil {
		t.Skip(err)
	}
	input := buatMKV(t, ffmpegBin, t.TempDir(), "aac") // 4 dtk
	mp4, err := remuxTo(t, ffmpegBin, input, RemuxOpts{Duration: 4})
	if err != nil {
		t.Fatalf("remux: %v", err)
	}
	dst := filepath.Join(t.TempDir(), "out.mp4")
	if werr := os.WriteFile(dst, mp4, 0o644); werr != nil {
		t.Fatal(werr)
	}
	if d := mvhdSeconds(t, dst); d < 3.9 || d > 4.1 {
		t.Fatalf("mvhd harus berisi durasi ±4 dtk, dapat %.2f", d)
	}
}

// mvhdSeconds: durasi (detik) dari box mvhd dalam file mp4.
func mvhdSeconds(t *testing.T, path string) float64 {
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
