package ffmpeg

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// buatMKV: video uji 4 dtk dengan codec audio tertentu (ac3/eac3/aac).
func buatMKV(t *testing.T, ffmpegBin, dir, audioCodec string) []byte {
	t.Helper()
	out := filepath.Join(dir, "input-"+audioCodec+".mkv")
	args := []string{"-y", "-loglevel", "error",
		"-f", "lavfi", "-i", "testsrc=duration=4:size=320x240:rate=25",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=4",
		"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p",
		"-c:a", audioCodec, "-shortest", out,
	}
	if o, err := exec.Command(ffmpegBin, args...).CombinedOutput(); err != nil {
		t.Fatalf("gagal buat mkv %s: %v (%s)", audioCodec, err, o)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func remuxTo(t *testing.T, bin string, input []byte, opts RemuxOpts) ([]byte, error) {
	t.Helper()
	var buf bytes.Buffer
	err := RemuxProgressive(bin, bytes.NewReader(input), opts, &buf)
	return buf.Bytes(), err
}

// Regresi: AC3/E-AC3 tidak bisa ditulis ke fragmented mp4 dengan empty_moov
// ("Cannot write moov atom before EAC3 packets parsed") → audio harus jadi AAC.
func TestRemuxAc3KeAAC(t *testing.T) {
	ffmpegBin, err := exec.LookPath("ffmpeg")
	ffprobeBin, perr := exec.LookPath("ffprobe")
	if err != nil || perr != nil {
		t.Skip("ffmpeg/ffprobe tidak ada")
	}
	for _, codec := range []string{"ac3", "eac3"} {
		t.Run(codec, func(t *testing.T) {
			input := buatMKV(t, ffmpegBin, t.TempDir(), codec)

			_, err := remuxTo(t, ffmpegBin, input, RemuxOpts{AudioIdx: 0})
			if err == nil {
				t.Skipf("%s: ffmpeg lokal masih bisa copy ke fragmented mp4", codec)
			}
			if !IsHeaderErr(err) {
				t.Fatalf("error bukan masalah header audio: %v", err)
			}

			mp4, err := remuxTo(t, ffmpegBin, input, RemuxOpts{AudioIdx: 0, AudioAAC: true})
			if err != nil {
				t.Fatalf("remux AAC gagal: %v", err)
			}
			if !bytes.Contains(mp4[:min(64, len(mp4))], []byte("ftyp")) {
				t.Fatalf("output bukan mp4: %q", mp4[:min(16, len(mp4))])
			}
			path := filepath.Join(t.TempDir(), "out.mp4")
			if err := os.WriteFile(path, mp4, 0o644); err != nil {
				t.Fatal(err)
			}
			o, err := exec.Command(ffprobeBin, "-v", "error",
				"-select_streams", "a:0", "-show_entries", "stream=codec_name",
				"-of", "default=nw=1:nk=1", path).Output()
			if err != nil {
				t.Fatalf("ffprobe output: %v", err)
			}
			if got := strings.TrimSpace(string(o)); got != "aac" {
				t.Fatalf("audio output harus aac, dapat %q", got)
			}
		})
	}
}

// Codec yang didukung browser tetap di-copy (tanpa transcode/kehilangan kualitas).
func TestRemuxAacTetapCopy(t *testing.T) {
	ffmpegBin, err := exec.LookPath("ffmpeg")
	ffprobeBin, perr := exec.LookPath("ffprobe")
	if err != nil || perr != nil {
		t.Skip("ffmpeg/ffprobe tidak ada")
	}
	input := buatMKV(t, ffmpegBin, t.TempDir(), "aac")
	mp4, err := remuxTo(t, ffmpegBin, input, RemuxOpts{AudioIdx: 0, AudioAAC: false})
	if err != nil {
		t.Fatalf("remux copy aac gagal: %v", err)
	}
	path := filepath.Join(t.TempDir(), "out.mp4")
	if err := os.WriteFile(path, mp4, 0o644); err != nil {
		t.Fatal(err)
	}
	o, err := exec.Command(ffprobeBin, "-v", "error",
		"-select_streams", "a:0", "-show_entries", "stream=codec_name",
		"-of", "default=nw=1:nk=1", path).Output()
	if err != nil {
		t.Fatalf("ffprobe output: %v", err)
	}
	if got := strings.TrimSpace(string(o)); got != "aac" {
		t.Fatalf("audio harus aac, dapat %q", got)
	}
}
