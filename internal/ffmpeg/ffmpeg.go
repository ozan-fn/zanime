package ffmpeg

import (
	"archive/tar"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ulikunitz/xz"

	"zanime/internal/tmpdir"
)

// BinDir: lokasi cache binary cadangan di tmpdir (fallback ./tmp).
func BinDir() string {
	return filepath.Join(tmpdir.Base(), "zanime-bin")
}

func Path() (string, error) {
	if p, err := exec.LookPath("ffmpeg"); err == nil {
		return p, nil
	}
	local := filepath.Join(BinDir(), "ffmpeg")
	if _, err := os.Stat(local); err == nil {
		return local, nil // sudah ada → skip download
	}
	if err := Download(); err != nil {
		return "", err
	}
	return local, nil
}

func ProbePath() (string, error) {
	if p, err := exec.LookPath("ffprobe"); err == nil {
		return p, nil
	}
	local := filepath.Join(BinDir(), "ffprobe")
	if _, err := os.Stat(local); err == nil {
		return local, nil
	}
	if err := Download(); err != nil {
		return "", err
	}
	return local, nil
}

// Download: static build (linux) sekali — tar+xz dibedah pakai module murni
// Go (ulikunitz/xz + archive/tar). OS lain: diminta install manual.
func Download() error {
	arch := runtime.GOARCH
	if runtime.GOOS != "linux" || (arch != "amd64" && arch != "arm64") {
		return fmt.Errorf("os/arch %s/%s: unduh ffmpeg manual lalu taruh di PATH atau %s", runtime.GOOS, arch, BinDir())
	}
	url := fmt.Sprintf("https://johnvansickle.com/ffmpeg/releases/ffmpeg-release-%s-static.tar.xz", arch)
	if err := os.MkdirAll(BinDir(), 0o755); err != nil {
		return err
	}
	client := &http.Client{Timeout: 15 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != 200 {
		return fmt.Errorf("download ffmpeg: HTTP %d", resp.StatusCode)
	}
	xr, err := xz.NewReader(resp.Body)
	if err != nil {
		return err
	}
	tr := tar.NewReader(xr)
	want := map[string]bool{"ffmpeg": true, "ffprobe": true}
	got := 0
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		base := filepath.Base(h.Name)
		if h.Typeflag != tar.TypeReg || !want[base] {
			continue
		}
		dst := filepath.Join(BinDir(), base)
		f, err := os.Create(dst)
		if err != nil {
			return err
		}
		if _, err := io.Copy(f, tr); err != nil {
			_ = f.Close()
			return err
		}
		_ = f.Close()
		if err := os.Chmod(dst, 0o755); err != nil {
			return err
		}
		got++
	}
	if got < len(want) {
		return fmt.Errorf("download ffmpeg: binary tidak lengkap (%d/%d)", got, len(want))
	}
	return nil
}

// RemuxOpts: pengaturan remux progresif.
type RemuxOpts struct {
	AudioIdx int
	// AudioAAC: encode audio ke AAC (video tetap copy). Wajib untuk codec yang
	// tidak bisa ditulis ke fragmented mp4 / tidak diputar browser — AC3, E-AC3,
	// DTS, TrueHD: ffmpeg gagal "Cannot write moov atom before EAC3 packets parsed".
	AudioAAC bool
	// SubFiles: index stream subtitle global → file WebVTT yang ditulis SEKALIAN dalam
	// pass ini. Jadi subtitle tidak perlu membaca ulang seluruh file (hemat kuota).
	SubFiles map[int]string
	// Duration: durasi video sebenarnya (detik). Fragmented mp4 menulis mvhd=0 (tak bisa
	// diketahui di awal), jadi player menampilkan durasi dinamis. Dengan nilai ini header
	// moov ditulis ulang → total pemutaran dikunci (mis. 23 menit).
	Duration float64
}

// IsHeaderErr: ffmpeg gagal menulis header mp4 (codec audio tidak cocok).
func IsHeaderErr(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "Cannot write moov atom") || strings.Contains(msg, "Could not write header")
}

// RemuxProgressive: input byte video (mkv/ts/avi/wmv dll) via pipe, remux ke
// fragmented mp4 — video selalu copy (hemat CPU), audio copy kecuali AudioAAC.
// Output dipipe ke writers (biasanya langsung response HTTP; tanpa file cache).
func RemuxProgressive(bin string, stdin io.Reader, o RemuxOpts, writers ...io.Writer) error {
	audioIdx := o.AudioIdx
	if audioIdx < 0 {
		audioIdx = 0
	}
	audioArgs := []string{"-c", "copy"}
	if o.AudioAAC {
		audioArgs = []string{"-c:v", "copy", "-c:a", "aac", "-b:a", "192k"}
	}
	args := []string{
		"-nostdin", "-loglevel", "error",
		"-i", "pipe:0",
		"-map", "0:v:0",
		"-map", fmt.Sprintf("0:a:%d", audioIdx),
	}
	args = append(args, audioArgs...)
	args = append(args,
		// frag_duration: pecah fragmen tiap ~2 dtk agar byte pertama cepat keluar
		"-movflags", "+frag_keyframe+empty_moov+default_base_moof",
		"-frag_duration", "2000000",
		"-f", "mp4", "pipe:1",
	)
	// Output tambahan: subtitle tertanam → WebVTT, ditulis paralel dalam pass yang sama.
	subIdx := make([]int, 0, len(o.SubFiles))
	for idx := range o.SubFiles {
		subIdx = append(subIdx, idx)
	}
	sort.Ints(subIdx)
	for _, idx := range subIdx {
		args = append(args,
			"-y", "-map", fmt.Sprintf("0:%d", idx),
			"-c:s", "webvtt",
			// flush tiap paket: cue langsung terlihat di file, jadi player bisa memasang
			// subtitle sebagian selagi episode masih dialirkan.
			"-flush_packets", "1",
			"-f", "webvtt", o.SubFiles[idx],
		)
	}
	cmd := exec.Command(bin, args...)
	stdinW, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdoutR, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _, _ = io.Copy(stdinW, stdin); _ = stdinW.Close() }()
	var out io.Writer
	if len(writers) == 1 {
		out = writers[0]
	} else {
		out = io.MultiWriter(writers...)
	}
	var patcher *moovDurWriter
	if o.Duration > 0 {
		// tulis durasi sebenarnya ke header moov sebelum byte pertama dilepas
		patcher = &moovDurWriter{w: out, dur: o.Duration}
		out = patcher
	}
	_, err = io.Copy(out, stdoutR)
	if patcher != nil {
		_ = patcher.Flush()
	}
	_, _ = io.Copy(io.Discard, stdoutR)
	if werr := cmd.Wait(); werr != nil {
		return fmt.Errorf("ffmpeg: %v: %s", werr, stderr.String())
	}
	return err
}

// ---------- durasi di header mp4 ----------
//
// Fragmented mp4 menulis mvhd=0 karena durasinya memang belum diketahui saat header
// dibuat → player menampilkan durasi dinamis (terus membesar saat buffer tumbuh).
// moovDurWriter menahan header sampai lengkap, menulis durasi yang kita tahu dari
// probe, lalu meneruskan byte apa adanya.

type moovDurWriter struct {
	w    io.Writer
	dur  float64
	buf  []byte
	done bool
}

const moovMaxBuf = 8 << 20 // jaring pengaman: header aneh → lewat tanpa ditulis ulang

func (p *moovDurWriter) Write(b []byte) (int, error) {
	if p.done {
		if _, err := p.w.Write(b); err != nil {
			return 0, err
		}
		return len(b), nil
	}
	p.buf = append(p.buf, b...)
	if end, ok := moovEnd(p.buf); ok {
		if len(p.buf) >= end {
			setMoovDuration(p.buf[:end], p.dur)
			if _, err := p.w.Write(p.buf[:end]); err != nil {
				return 0, err
			}
			p.buf = p.buf[end:]
			p.done = true
			if len(p.buf) > 0 {
				rest := append([]byte(nil), p.buf...)
				p.buf = nil
				if _, err := p.w.Write(rest); err != nil {
					return 0, err
				}
			}
			return len(b), nil
		}
	}
	if len(p.buf) > moovMaxBuf {
		// header tidak biasa (moov raksasa / tanpa moov) → lewatkan apa adanya
		buf := p.buf
		p.buf = nil
		p.done = true
		if _, err := p.w.Write(buf); err != nil {
			return 0, err
		}
	}
	return len(b), nil
}

// Flush: lewatkan sisa buffer (aliran berakhir sebelum header lengkap).
func (p *moovDurWriter) Flush() error {
	if p.done || len(p.buf) == 0 {
		return nil
	}
	buf := p.buf
	p.buf = nil
	p.done = true
	_, err := p.w.Write(buf)
	return err
}

// moovEnd: offset tepat setelah box moov (atau belum ketemu).
func moovEnd(b []byte) (int, bool) {
	i := 0
	for i+8 <= len(b) {
		size := int(binary.BigEndian.Uint32(b[i : i+4]))
		if size < 8 {
			return 0, false
		}
		typ := string(b[i+4 : i+8])
		if typ == "moov" {
			if i+size > len(b) {
				return 0, false
			}
			return i + size, true
		}
		i += size
	}
	return 0, false
}

// setMoovDuration: tulis durasi (detik) ke mvhd (basisnya), tkhd dan mdhd.
func setMoovDuration(b []byte, dur float64) {
	if dur <= 0 {
		return
	}
	// timescale mvhd jadi acuan tkhd (tkhd tidak menyimpan timescale)
	movieTS := uint32(1000)
	forEachBox(b, func(i, size int, typ string) {
		if typ == "mvhd" && i+8 < len(b) && b[i+8] == 0 && i+24 <= len(b) {
			if ts := binary.BigEndian.Uint32(b[i+20 : i+24]); ts > 0 {
				movieTS = ts
			}
		}
	})
	writeDur(b, dur, movieTS)
}

// forEachBox: iterasi top-level box + turunan container.
func forEachBox(b []byte, fn func(i, size int, typ string)) {
	containers := map[string]bool{"moov": true, "trak": true, "mdia": true, "minf": true, "stbl": true}
	var walk func(start, end int)
	walk = func(start, end int) {
		i := start
		for i+8 <= end {
			size := int(binary.BigEndian.Uint32(b[i : i+4]))
			if size < 8 || i+size > end {
				return
			}
			typ := string(b[i+4 : i+8])
			fn(i, size, typ)
			if containers[typ] {
				walk(i+8, i+size)
			}
			i += size
		}
	}
	walk(0, len(b))
}

// writeDur: tulis durasi ke mvhd/tkhd/mdhd di seluruh pohon.
func writeDur(b []byte, dur float64, movieTS uint32) {
	forEachBox(b, func(i, size int, typ string) {
		if i+8 >= len(b) {
			return
		}
		ver := b[i+8]
		switch typ {
		case "mvhd", "mdhd":
			if ver == 0 && i+28 <= len(b) {
				ts := binary.BigEndian.Uint32(b[i+20 : i+24])
				if ts == 0 {
					return
				}
				binary.BigEndian.PutUint32(b[i+24:i+28], uint32(dur*float64(ts)))
			} else if ver == 1 && i+40 <= len(b) {
				ts := binary.BigEndian.Uint32(b[i+28 : i+32])
				if ts == 0 {
					return
				}
				binary.BigEndian.PutUint64(b[i+32:i+40], uint64(dur*float64(ts)))
			}
		case "tkhd":
			// tkhd tidak punya timescale sendiri → pakai movie timescale
			if ver == 0 && i+32 <= len(b) {
				binary.BigEndian.PutUint32(b[i+28:i+32], uint32(dur*float64(movieTS)))
			} else if ver == 1 && i+40 <= len(b) {
				binary.BigEndian.PutUint32(b[i+36:i+40], uint32(dur*float64(movieTS)))
			}
		}
	})
}

// ExtractToVTT: sub file/embedded → bytes webvtt (copy codec saja).
// inputOpts mis. ["-i","pipe:0"] atau ["-i","pipe:0","-map","0:s:0"].
func ExtractToVTT(bin string, stdin io.Reader, inputOpts ...string) ([]byte, error) {
	args := []string{"-nostdin", "-loglevel", "error"}
	args = append(args, inputOpts...)
	args = append(args, "-c:s", "webvtt", "-f", "webvtt", "pipe:1")
	cmd := exec.Command(bin, args...)
	stdinW, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	outR, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	go func() { _, _ = io.Copy(stdinW, stdin); _ = stdinW.Close() }()
	data, err := io.ReadAll(outR)
	if werr := cmd.Wait(); werr != nil {
		return nil, fmt.Errorf("ffmpeg sub: %v: %s", werr, stderr.String())
	}
	return data, err
}

// Track = satu stream audio/subtitle hasil probe.
type Track struct {
	Index int    `json:"index"`
	Lang  string `json:"lang,omitempty"`
	Title string `json:"title,omitempty"`
	Codec string `json:"codec,omitempty"`
}

// Probe: daftar stream audio & subtitle dari byte video (hemat CPU: analyzeduration/
// probesize diberi batas, jadi hanya header yang dibaca — bukan seluruh file).
// probesize/analyzeduration dipilih pemanggil: 1M/2s cukup untuk header MKV/MP4 normal
// dan jauh lebih cepat di peer lambat.
func Probe(probeBin string, stdin io.Reader, probesize, analyzeUs string) (audio, subs []Track, duration float64, err error) {
	// NB: ffprobe TIDAK punya opsi -nostdin (itu opsi ffmpeg) — dulu ini membuat probe selalu gagal.
	cmd := exec.Command(probeBin,
		"-hide_banner", "-loglevel", "error",
		"-analyzeduration", analyzeUs, "-probesize", probesize,
		"-print_format", "json", "-show_streams", "-show_format", "pipe:0")
	stdinW, err := cmd.StdinPipe()
	if err != nil {
		return nil, nil, 0, err
	}
	outR, err := cmd.StdoutPipe()
	if err != nil {
		return nil, nil, 0, err
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return nil, nil, 0, err
	}
	go func() { _, _ = io.Copy(stdinW, stdin); _ = stdinW.Close() }()
	var r struct {
		Streams []struct {
			Index     int    `json:"index"`
			CodecType string `json:"codec_type"`
			CodecName string `json:"codec_name"`
			Tags      struct {
				Language string `json:"language"`
				Title    string `json:"title"`
			} `json:"tags"`
		} `json:"streams"`
		Format struct {
			Duration string `json:"duration"`
		} `json:"format"`
	}
	if err := json.NewDecoder(outR).Decode(&r); err != nil {
		_ = cmd.Wait()
		if msg := strings.TrimSpace(stderr.String()); msg != "" {
			return nil, nil, 0, fmt.Errorf("ffprobe: %v: %s", err, msg)
		}
		return nil, nil, 0, err
	}
	if werr := cmd.Wait(); werr != nil {
		return nil, nil, 0, fmt.Errorf("ffprobe: %v: %s", werr, stderr.String())
	}
	if d, perr := strconv.ParseFloat(r.Format.Duration, 64); perr == nil {
		duration = d
	}
	for _, st := range r.Streams {
		t := Track{Index: st.Index, Lang: st.Tags.Language, Title: st.Tags.Title, Codec: st.CodecName}
		switch st.CodecType {
		case "audio":
			audio = append(audio, t)
		case "subtitle":
			// subtitle bitmap (PGS/DVD) tidak bisa dikonversi ke WebVTT → jangan ditawarkan
			switch st.CodecName {
			case "hdmv_pgs_subtitle", "dvd_subtitle", "dvb_subtitle", "xsub":
				continue
			}
			subs = append(subs, t)
		}
	}
	return audio, subs, duration, nil
}
