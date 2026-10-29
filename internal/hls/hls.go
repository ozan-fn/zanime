// Package hls turns the selected video into an HLS stream. ffmpeg reads the
// server's own /stream endpoint (so it can seek in the torrent) and writes a
// playlist plus segments into a temp dir that the browser reads. Torrent data
// stays in RAM; only the encoded output touches disk, and it is removed when
// the session ends.
//
// transcode=false copies the tracks (`-c copy`), so the browser needs codec
// support (H.264/AAC/HEVC). transcode=true re-encodes to H.264/AAC for clients
// that cannot play the source codec, at the cost of CPU.
package hls

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	verifyLimit  = 20 * time.Second
	probeTimeout = 20 * time.Second
	ffmpegAsset  = "https://github.com/eugeneware/ffmpeg-static/releases/latest/download/ffmpeg-%s-%s"
)

// LocalPath is where a downloaded ffmpeg lives, following OS naming.
func LocalPath() string {
	if runtime.GOOS == "windows" {
		return filepath.Join("tmp", "ffmpeg.exe")
	}
	return filepath.Join("tmp", "ffmpeg")
}

var (
	ensureOnce sync.Once
	ensurePath string
	ensureErr  error
)

// Ensure returns a usable ffmpeg: the one installed on the system first, then
// a build already downloaded into ./tmp, and only if both are missing a static
// build downloaded once into ./tmp. The result is cached for the process.
func Ensure(ctx context.Context) (string, error) {
	ensureOnce.Do(func() { ensurePath, ensureErr = resolve(ctx) })
	return ensurePath, ensureErr
}

func resolve(ctx context.Context) (string, error) {
	if works(ctx, "ffmpeg") {
		return "ffmpeg", nil
	}
	local := LocalPath()
	if works(ctx, local) {
		log.Printf("ffmpeg sistem tidak ada; memakai %s", local)
		return local, nil
	}
	log.Printf("ffmpeg tidak ditemukan; mengunduh build statis ke %s …", local)
	if err := download(ctx); err != nil {
		return "", fmt.Errorf("ffmpeg tidak tersedia: %w", err)
	}
	if !works(ctx, local) {
		return "", errors.New("ffmpeg unduhan tidak bisa dijalankan")
	}
	return local, nil
}

func works(ctx context.Context, path string) bool {
	ctx, cancel := context.WithTimeout(ctx, verifyLimit)
	defer cancel()
	return exec.CommandContext(ctx, path, "-version").Run() == nil
}

func download(ctx context.Context) error {
	url, err := assetURL(runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("unduh ffmpeg: HTTP %s", res.Status)
	}

	local := LocalPath()
	if err := os.MkdirAll(filepath.Dir(local), 0o755); err != nil {
		return err
	}
	// Tulis ke .part lalu rename: unduhan terputus tidak meninggalkan binary
	// setengah jadi yang terlihat valid.
	part := local + ".part"
	f, err := os.Create(part)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, res.Body); err != nil {
		_ = f.Close()
		_ = os.Remove(part)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(part)
		return err
	}
	if err := os.Chmod(part, 0o755); err != nil && runtime.GOOS != "windows" {
		_ = os.Remove(part)
		return err
	}
	return os.Rename(part, local)
}

// assetURL maps GOOS/GOARCH to the static build published by ffmpeg-static.
func assetURL(goos, goarch string) (string, error) {
	osName := map[string]string{"linux": "linux", "windows": "win32", "darwin": "darwin"}[goos]
	archName := map[string]string{
		"amd64": "x64", "arm64": "arm64", "386": "ia32", "arm": "arm",
	}[goarch]
	if osName == "" || archName == "" {
		return "", fmt.Errorf("tidak ada build ffmpeg statis untuk %s/%s", goos, goarch)
	}
	return fmt.Sprintf(ffmpegAsset, osName, archName), nil
}

// Job is one running HLS session for a file index.
type Job struct {
	index     int
	transcode bool
	dir       string
	cancel    context.CancelFunc
	done      chan struct{} // ditutup saat ffmpeg benar-benar berhenti
	ready     atomic.Bool
	progress  atomic.Int64 // permil 0..1000 dari durasi file

	mu       sync.Mutex
	playlist string // master.m3u8 untuk multi-audio, index.m3u8 untuk satu audio
	errMsg   string
}

// Status is a snapshot of the running session for the state JSON.
type Status struct {
	Index     int
	Ready     bool
	Transcode bool
	Playlist  string
	Progress  int // 0..100
	Error     string
}

// Manager keeps a single HLS session: only one episode is served at a time.
type Manager struct {
	mu  sync.Mutex
	job *Job
}

func NewManager() *Manager {
	return &Manager{}
}

// Start begins segmenting index from streamURL (the server's own /stream
// endpoint). A session for the same index and mode is kept; any other session
// is cancelled first.
func (m *Manager) Start(ctx context.Context, index int, streamURL, ffmpeg string, transcode bool) {
	m.mu.Lock()
	if m.job != nil {
		if m.job.index == index && m.job.transcode == transcode {
			m.mu.Unlock()
			return
		}
		m.job.stop()
	}
	dir, err := os.MkdirTemp("", "zanime-hls-")
	if err != nil {
		m.mu.Unlock()
		log.Printf("hls: %v", err)
		return
	}
	ctx, cancel := context.WithCancel(ctx)
	job := &Job{index: index, transcode: transcode, dir: dir, cancel: cancel, done: make(chan struct{})}
	m.job = job
	m.mu.Unlock()

	go job.run(ctx, ffmpeg, streamURL)
}

// Stop ends the running session and deletes its segments.
func (m *Manager) Stop() {
	m.mu.Lock()
	job := m.job
	m.job = nil
	m.mu.Unlock()
	if job != nil {
		job.stop()
	}
}

// stop ends ffmpeg and removes the segment dir once the process is gone.
func (j *Job) stop() {
	j.cancel()
	go func() {
		<-j.done
		if err := os.RemoveAll(j.dir); err != nil {
			log.Printf("hls: hapus %s: %v", j.dir, err)
		}
	}()
}

// Status reports the current session for state JSON.
func (m *Manager) Status() (Status, bool) {
	m.mu.Lock()
	job := m.job
	m.mu.Unlock()
	if job == nil {
		return Status{}, false
	}
	job.mu.Lock()
	playlist, errMsg := job.playlist, job.errMsg
	job.mu.Unlock()
	progress := int(min(job.progress.Load(), 1000) / 10)
	return Status{Index: job.index, Ready: job.ready.Load(), Transcode: job.transcode,
		Playlist: playlist, Progress: progress, Error: errMsg}, true
}

// Path maps a playlist or segment name to a file of the session for index.
// Clean resolves "..", so a name can never leave the session dir.
func (m *Manager) Path(index int, name string) (string, bool) {
	m.mu.Lock()
	job := m.job
	m.mu.Unlock()
	if job == nil || job.index != index {
		return "", false
	}
	clean := path.Clean("/" + strings.ReplaceAll(name, `\`, "/"))
	return filepath.Join(job.dir, filepath.FromSlash(clean)), true
}

// Banner ffmpeg: satu baris per stream audio (captures bahasa), dan baris
// `Duration: HH:MM:SS.ss` yang dipakai untuk persen persiapan.
var (
	audioStream  = regexp.MustCompile(`Stream #\d+:\d+(?:\[[^\]]*\])?(?:\(([^)]*)\))?: Audio:`)
	durationLine = regexp.MustCompile(`Duration: (\d+):(\d+):(\d+(?:\.\d+)?)`)
)

type mediaInfo struct {
	audio    []string
	duration float64
}

// probeMedia reads the banner of `ffmpeg -i` (ffprobe is not part of the static
// build): the language tag of every audio stream, so each can be published as
// its own HLS audio rendition, and the duration for the preparation progress.
// shortcut: a source whose banner hides streams yields no tracks and the
// session falls back to one audio playlist; switch to ffprobe if that happens.
func probeMedia(ctx context.Context, ffmpeg, streamURL string) mediaInfo {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	out, _ := exec.CommandContext(ctx, ffmpeg, "-hide_banner", "-i", streamURL).CombinedOutput()
	var info mediaInfo
	for _, line := range strings.Split(string(out), "\n") {
		if m := audioStream.FindStringSubmatch(line); m != nil {
			lang := m[1]
			if lang == "" {
				lang = "und"
			}
			info.audio = append(info.audio, lang)
			continue
		}
		if m := durationLine.FindStringSubmatch(line); m != nil {
			h, _ := strconv.Atoi(m[1])
			minute, _ := strconv.Atoi(m[2])
			second, _ := strconv.ParseFloat(m[3], 64)
			info.duration = float64(h*3600+minute*60) + second
		}
	}
	return info
}

// scanProgress turns ffmpeg's `-progress` stream into permil of the duration.
func scanProgress(r io.Reader, j *Job, duration float64) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		key, value, ok := strings.Cut(scanner.Text(), "=")
		if !ok || key != "out_time_us" || duration <= 0 {
			continue
		}
		micros, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			continue
		}
		j.progress.Store(min(max(int64(float64(micros)/1e6/duration*1000), 0), 999))
	}
}

// argsFor builds the ffmpeg command. With more than one audio track it writes
// a master playlist whose audio renditions the player turns into an audio menu.
func argsFor(dir, streamURL string, transcode bool, audio []string) []string {
	// -sn: subtitle dalam file tidak ikut ke TS (PGS/ASS membuat muxer gagal).
	args := []string{"-hide_banner", "-loglevel", "error", "-nostdin", "-i", streamURL, "-map", "0:v:0"}
	for i := range audio {
		args = append(args, "-map", fmt.Sprintf("0:a:%d", i))
	}
	args = append(args, "-sn")
	if transcode {
		args = append(args, "-c:v", "libx264", "-preset", "veryfast", "-c:a", "aac", "-b:a", "128k")
	} else {
		args = append(args, "-c", "copy")
	}
	// VOD (bukan event): playlist selesai + ENDLIST, jadi player tahu durasi
	// dan bisa seek ke mana saja. -progress dipakai untuk persen persiapan.
	args = append(args, "-progress", "pipe:1", "-nostats",
		"-f", "hls", "-hls_time", "4", "-hls_playlist_type", "vod", "-hls_flags", "independent_segments")
	if len(audio) > 1 {
		parts := []string{"v:0,agroup:aud,name:video"}
		for i, lang := range audio {
			p := fmt.Sprintf("a:%d,agroup:aud,name:audio%d", i, i)
			if lang != "" && lang != "und" {
				p += ",language:" + lang
			}
			if i == 0 {
				p += ",default:yes"
			}
			parts = append(parts, p)
		}
		return append(args,
			"-master_pl_name", "master.m3u8",
			"-var_stream_map", strings.Join(parts, " "),
			"-hls_segment_filename", filepath.Join(dir, "%v", "seg%05d.ts"),
			filepath.Join(dir, "%v", "index.m3u8"))
	}
	return append(args,
		"-hls_segment_filename", filepath.Join(dir, "seg%05d.ts"),
		filepath.Join(dir, "index.m3u8"))
}

func (j *Job) run(ctx context.Context, ffmpeg, streamURL string) {
	defer close(j.done)

	media := probeMedia(ctx, ffmpeg, streamURL)
	audio := media.audio
	if len(audio) == 0 {
		audio = []string{"und"} // video tanpa audio
	}
	playlist := "index.m3u8"
	if len(audio) > 1 {
		playlist = "master.m3u8"
		// ffmpeg tidak membuat folder per rendition.
		dirs := []string{"video"}
		for i := range audio {
			dirs = append(dirs, fmt.Sprintf("audio%d", i))
		}
		for _, d := range dirs {
			if err := os.MkdirAll(filepath.Join(j.dir, d), 0o755); err != nil {
				log.Printf("hls: folder rendition: %v", err)
				return
			}
		}
	}
	j.mu.Lock()
	j.playlist = playlist
	j.mu.Unlock()

	cmd := exec.CommandContext(ctx, ffmpeg, argsFor(j.dir, streamURL, j.transcode, audio)...)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		j.fail(fmt.Sprintf("stdout ffmpeg: %v", err))
		return
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		j.fail(fmt.Sprintf("jalankan ffmpeg: %v", err))
		return
	}
	// Dibaca sampai EOF (ffmpeg selesai) sebelum Wait mereap prosesnya.
	scanProgress(stdout, j, media.duration)

	if err := cmd.Wait(); err != nil {
		if ctx.Err() == nil {
			j.fail(fmt.Sprintf("ffmpeg gagal: %v: %s", err, strings.TrimSpace(stderr.String())))
		}
		return
	}
	// Segmen tetap disimpan sampai sesi diganti: player masih membacanya.
	if fi, statErr := os.Stat(filepath.Join(j.dir, playlist)); statErr != nil || fi.Size() == 0 {
		j.fail("ffmpeg selesai tanpa playlist")
		return
	}
	j.ready.Store(true)
	log.Printf("hls siap: index %d (%d audio, transcode %v, %ds)", j.index, len(audio), j.transcode, int(media.duration))
}

// fail records why the session cannot be played, so the UI stops waiting on it.
func (j *Job) fail(msg string) {
	log.Printf("hls: %s", msg)
	j.mu.Lock()
	j.errMsg = msg
	j.mu.Unlock()
}
