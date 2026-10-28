// Package remux rebuilds the selected video so it has a seek index, by running
// ffmpeg with `-c copy` (no transcoding) and buffering the output in RAM.
// Media never touches the disk: only the ffmpeg binary itself is kept in
// ./tmp, and it is downloaded once when the system has none.
package remux

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

// Budget is the RAM limit shared with the piece cache: Reserve takes bytes
// away from it, Release gives them back. Remux output therefore shrinks the
// piece cache instead of growing total memory past MAX_MEMORY_MB.
type Budget interface {
	Reserve(n int64) bool
	Release(n int64)
}

const (
	chunkSize   = 128 << 10
	verifyLimit = 20 * time.Second
	ffmpegAsset = "https://github.com/eugeneware/ffmpeg-static/releases/latest/download/ffmpeg-%s-%s"
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

// Ensure returns a usable ffmpeg: the local copy first, then `ffmpeg` from
// PATH, and finally a static build downloaded into ./tmp. The result is cached.
func Ensure(ctx context.Context) (string, error) {
	ensureOnce.Do(func() { ensurePath, ensureErr = resolve(ctx) })
	return ensurePath, ensureErr
}

func resolve(ctx context.Context) (string, error) {
	local := LocalPath()
	if works(ctx, local) {
		return local, nil
	}
	if works(ctx, "ffmpeg") {
		return "ffmpeg", nil
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
	defer res.Body.Close()
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
		f.Close()
		os.Remove(part)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(part)
		return err
	}
	if err := os.Chmod(part, 0o755); err != nil && runtime.GOOS != "windows" {
		os.Remove(part)
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

// Job is one running (or finished) remux for a file index.
type Job struct {
	index int

	mu     sync.RWMutex
	buf    []byte
	ready  bool
	cancel context.CancelFunc
}

// Index is the file index this job belongs to.
func (j *Job) Index() int { return j.index }

// Manager keeps a single remux job: only one episode is muxed at a time.
type Manager struct {
	budget Budget
	mu     sync.Mutex
	job    *Job
}

// NewManager returns a manager whose output counts against budget.
func NewManager(budget Budget) *Manager {
	return &Manager{budget: budget}
}

// Start muxes the file at index from streamURL (the server's own /stream
// endpoint, so ffmpeg can seek through Range). A job for the same index is
// kept; a job for another index is cancelled first.
func (m *Manager) Start(ctx context.Context, index int, streamURL, ffmpeg string) {
	m.mu.Lock()
	if m.job != nil {
		if m.job.index == index {
			m.mu.Unlock()
			return
		}
		if m.job.cancel != nil {
			m.job.cancel()
		}
	}
	ctx, cancel := context.WithCancel(ctx)
	job := &Job{index: index, cancel: cancel}
	m.job = job
	m.mu.Unlock()

	go job.run(ctx, m.budget, ffmpeg, streamURL)
}

// Stop cancels the running job and forgets it; its goroutine returns the
// reserved bytes to the budget when it unwinds.
func (m *Manager) Stop() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.job != nil {
		if m.job.cancel != nil {
			m.job.cancel()
		}
		m.job = nil
	}
}

// Status reports the current job for state JSON.
func (m *Manager) Status() (index int, ready, exists bool) {
	m.mu.Lock()
	job := m.job
	m.mu.Unlock()
	if job == nil {
		return 0, false, false
	}
	job.mu.RLock()
	defer job.mu.RUnlock()
	return job.index, job.ready, true
}

// Output returns the finished output for index. exists is false when the job
// belongs to another file; ready is false while ffmpeg is still writing.
func (m *Manager) Output(index int) (data []byte, ready, exists bool) {
	m.mu.Lock()
	job := m.job
	m.mu.Unlock()
	if job == nil || job.index != index {
		return nil, false, false
	}
	job.mu.RLock()
	defer job.mu.RUnlock()
	if !job.ready {
		return nil, false, true
	}
	return job.buf, true, true
}

func (j *Job) run(ctx context.Context, budget Budget, ffmpeg, streamURL string) {
	cmd := exec.CommandContext(ctx, ffmpeg,
		"-hide_banner", "-loglevel", "error", "-nostdin",
		"-i", streamURL,
		"-map", "0",
		"-c", "copy",
		"-f", "matroska",
		"pipe:1",
	)
	out, err := cmd.StdoutPipe()
	if err != nil {
		log.Printf("remux: stdout ffmpeg: %v", err)
		return
	}
	if err := cmd.Start(); err != nil {
		log.Printf("remux: jalankan ffmpeg: %v", err)
		return
	}

	var (
		buf      bytes.Buffer
		reserved int64
		chunk    = make([]byte, chunkSize)
	)
	fail := func() {
		if reserved > 0 {
			budget.Release(reserved)
		}
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}

	for {
		n, readErr := out.Read(chunk)
		if n > 0 {
			// Potong budget cache piece; kalau tidak cukup untuk satu chunk,
			// remux dibatalkan daripada melewati batas memori.
			if !budget.Reserve(int64(n)) {
				log.Printf("remux: melewati batas memori, dibatalkan")
				fail()
				return
			}
			reserved += int64(n)
			buf.Write(chunk[:n])
		}
		if readErr != nil {
			if !errors.Is(readErr, io.EOF) {
				log.Printf("remux: baca keluaran ffmpeg: %v", readErr)
				fail()
				return
			}
			break
		}
	}

	if err := cmd.Wait(); err != nil {
		log.Printf("remux: ffmpeg gagal: %v", err)
		if reserved > 0 {
			budget.Release(reserved)
		}
		return
	}

	j.mu.Lock()
	j.buf = buf.Bytes()
	j.ready = true
	j.mu.Unlock()
	log.Printf("remux siap: index %d (%d byte)", j.index, len(j.buf))
}
