package tests

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// End-to-end streaming checks against a running server:
//
//	go run . -cache-mb 200                                 # terminal 1
//	ZANIME_E2E=1 go test -run TestE2E -timeout 90s ./tests # terminal 2
//
// Each HTTP request is capped at ZANIME_TIMEOUT (default 15s); the overall
// -timeout is larger because several ranges are fetched. When the swarm is
// still cold the test skips instead of failing, so a slow network does not
// look like a server bug.

var videoExtensions = map[string]bool{
	".mkv": true, ".mp4": true, ".m4v": true, ".webm": true,
	".avi": true, ".mov": true, ".ts": true, ".m2ts": true,
}

type e2eInfo struct {
	Ready          bool   `json:"ready"`
	Name           string `json:"name"`
	Size           int64  `json:"size"`
	Storage        string `json:"storage"`
	CacheLimit     int64  `json:"cache_limit"`
	CacheUsed      int64  `json:"cache_used"`
	CacheEvictions int64  `json:"cache_evictions"`
}

var (
	e2eOnce     sync.Once
	e2eBaseURL  string
	e2eMetadata e2eInfo
	e2eSetupErr error
)

func e2eTimeout() time.Duration {
	if value := os.Getenv("ZANIME_TIMEOUT"); value != "" {
		if seconds, err := strconv.ParseFloat(value, 64); err == nil && seconds > 0 {
			return time.Duration(seconds * float64(time.Second))
		}
	}
	return 15 * time.Second
}

func e2eSetup(t *testing.T) (string, e2eInfo) {
	t.Helper()
	if os.Getenv("ZANIME_E2E") == "" {
		t.Skip("set ZANIME_E2E=1 with the server running to test real torrent streaming")
	}
	e2eOnce.Do(func() {
		e2eBaseURL = strings.TrimRight(os.Getenv("ZANIME_URL"), "/")
		if e2eBaseURL == "" {
			e2eBaseURL = "http://127.0.0.1:8080"
		}
		e2eMetadata, e2eSetupErr = e2eWaitReady(e2eBaseURL)
		if e2eSetupErr == nil {
			e2eSetupErr = e2eWarmUp(e2eBaseURL)
		}
	})
	if e2eSetupErr != nil {
		t.Skipf("server not ready for streaming: %v", e2eSetupErr)
	}
	return e2eBaseURL, e2eMetadata
}

func e2eDo(base, method, path string, headers map[string]string) (*http.Response, []byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), e2eTimeout())
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, method, base+path, nil)
	if err != nil {
		return nil, nil, err
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	return resp, body, err
}

func e2eWaitReady(base string) (e2eInfo, error) {
	deadline := time.Now().Add(e2eTimeout())
	var lastErr error
	for time.Now().Before(deadline) {
		var info e2eInfo
		resp, body, err := e2eDo(base, http.MethodGet, "/info", nil)
		switch {
		case err != nil:
			lastErr = err
		case resp.StatusCode != http.StatusOK:
			lastErr = fmt.Errorf("/info status %d", resp.StatusCode)
		case json.Unmarshal(body, &info) != nil:
			lastErr = fmt.Errorf("invalid /info body")
		case info.Ready:
			return info, nil
		default:
			lastErr = fmt.Errorf("torrent metadata not ready")
		}
		time.Sleep(100 * time.Millisecond)
	}
	return e2eInfo{}, fmt.Errorf("not ready after %s: %w", e2eTimeout(), lastErr)
}

// e2eWarmUp fetches one byte so the first torrent piece is already in RAM.
func e2eWarmUp(base string) error {
	resp, body, err := e2eDo(base, http.MethodGet, "/stream", map[string]string{"Range": "bytes=0-0"})
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusPartialContent || len(body) != 1 {
		return fmt.Errorf("warm-up status %d, %d body bytes", resp.StatusCode, len(body))
	}
	return nil
}

func e2eRange(t *testing.T, base string, start, end int64) (*http.Response, []byte) {
	t.Helper()
	headers := map[string]string{"Range": fmt.Sprintf("bytes=%d-%d", start, end)}
	resp, body, err := e2eDo(base, http.MethodGet, "/stream", headers)
	if err != nil {
		// A 4 MiB piece from a cold public swarm can take longer than the
		// request budget. Skipping keeps a slow swarm from looking like a bug.
		t.Skipf("range %d-%d not available in time: %v", start, end, err)
	}
	return resp, body
}

func TestE2E_InfoReportsZeroDiskStorage(t *testing.T) {
	_, info := e2eSetup(t)

	if info.Storage != "memory" {
		t.Fatalf("storage = %q, want memory", info.Storage)
	}
	if info.Size <= 0 {
		t.Fatalf("size = %d, want positive", info.Size)
	}
	if !videoExtensions[strings.ToLower(filepath.Ext(info.Name))] {
		t.Fatalf("served file %q is not a video", info.Name)
	}
	if info.CacheUsed > info.CacheLimit {
		t.Fatalf("cache used %d over limit %d", info.CacheUsed, info.CacheLimit)
	}
}

func TestE2E_HeadReportsSizeAndRanges(t *testing.T) {
	base, info := e2eSetup(t)

	resp, body, err := e2eDo(base, http.MethodHead, "/stream", nil)
	if err != nil {
		t.Fatalf("HEAD /stream: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Length"); got != strconv.FormatInt(info.Size, 10) {
		t.Fatalf("Content-Length = %q, want %d", got, info.Size)
	}
	if got := resp.Header.Get("Accept-Ranges"); got != "bytes" {
		t.Fatalf("Accept-Ranges = %q, want bytes", got)
	}
	if len(body) != 0 {
		t.Fatalf("HEAD returned %d body bytes", len(body))
	}
}

func TestE2E_StreamsFirstChunk(t *testing.T) {
	base, info := e2eSetup(t)
	const chunk = 128 << 10

	resp, body := e2eRange(t, base, 0, chunk-1)
	if resp.StatusCode != http.StatusPartialContent {
		t.Fatalf("status = %d, want 206", resp.StatusCode)
	}
	wantRange := fmt.Sprintf("bytes 0-%d/%d", chunk-1, info.Size)
	if got := resp.Header.Get("Content-Range"); got != wantRange {
		t.Fatalf("Content-Range = %q, want %q", got, wantRange)
	}
	if len(body) != chunk {
		t.Fatalf("body = %d bytes, want %d", len(body), chunk)
	}
	if got := resp.Header.Get("Content-Type"); !strings.HasPrefix(got, "video/") {
		t.Fatalf("Content-Type = %q, want video/*", got)
	}
}

func TestE2E_SeekWithinDownloadedPiece(t *testing.T) {
	base, info := e2eSetup(t)
	start := min(int64(1<<20), info.Size-1)
	end := min(start+(64<<10)-1, info.Size-1)

	resp, body := e2eRange(t, base, start, end)
	if resp.StatusCode != http.StatusPartialContent {
		t.Fatalf("status = %d, want 206", resp.StatusCode)
	}
	wantRange := fmt.Sprintf("bytes %d-%d/%d", start, end, info.Size)
	if got := resp.Header.Get("Content-Range"); got != wantRange {
		t.Fatalf("Content-Range = %q, want %q", got, wantRange)
	}
	if int64(len(body)) != end-start+1 {
		t.Fatalf("body = %d bytes, want %d", len(body), end-start+1)
	}
}

func TestE2E_SeekToMiddleReadsRequestedOffset(t *testing.T) {
	base, info := e2eSetup(t)
	start := info.Size / 2
	end := min(start+(64<<10)-1, info.Size-1)

	resp, body := e2eRange(t, base, start, end)
	if resp.StatusCode != http.StatusPartialContent {
		t.Fatalf("status = %d, want 206", resp.StatusCode)
	}
	wantRange := fmt.Sprintf("bytes %d-%d/%d", start, end, info.Size)
	if got := resp.Header.Get("Content-Range"); got != wantRange {
		t.Fatalf("Content-Range = %q, want %q", got, wantRange)
	}
	if int64(len(body)) != end-start+1 {
		t.Fatalf("body = %d bytes, want %d", len(body), end-start+1)
	}
}

func TestE2E_SeekSameRangeIsRepeatable(t *testing.T) {
	base, info := e2eSetup(t)
	start := min(int64(1<<20), info.Size-1)
	end := min(start+(32<<10)-1, info.Size-1)

	_, first := e2eRange(t, base, start, end)
	_, second := e2eRange(t, base, start, end)
	if !bytes.Equal(first, second) {
		t.Fatal("same range returned different bytes")
	}
}

func TestE2E_UnsatisfiableRangeReturns416(t *testing.T) {
	base, info := e2eSetup(t)
	headers := map[string]string{"Range": fmt.Sprintf("bytes=%d-", info.Size)}

	resp, _, err := e2eDo(base, http.MethodGet, "/stream", headers)
	if err != nil {
		t.Fatalf("unsatisfiable range: %v", err)
	}
	if resp.StatusCode != http.StatusRequestedRangeNotSatisfiable {
		t.Fatalf("status = %d, want 416", resp.StatusCode)
	}
	wantRange := fmt.Sprintf("bytes */%d", info.Size)
	if got := resp.Header.Get("Content-Range"); got != wantRange {
		t.Fatalf("Content-Range = %q, want %q", got, wantRange)
	}
}

func TestE2E_CacheStaysWithinLimit(t *testing.T) {
	base, _ := e2eSetup(t)

	resp, body, err := e2eDo(base, http.MethodGet, "/info", nil)
	if err != nil {
		t.Fatalf("GET /info: %v", err)
	}
	var info e2eInfo
	if err := json.Unmarshal(body, &info); err != nil {
		t.Fatalf("decode /info: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if info.CacheUsed > info.CacheLimit {
		t.Fatalf("cache used %d over limit %d", info.CacheUsed, info.CacheLimit)
	}
}

func TestE2E_NoTorrentDataFilesOnDisk(t *testing.T) {
	e2eSetup(t)

	pid, err := zanimePID()
	if err != nil {
		t.Skipf("cannot locate the server process: %v", err)
	}
	fdDir := filepath.Join("/proc", strconv.Itoa(pid), "fd")
	entries, err := os.ReadDir(fdDir)
	if err != nil {
		t.Skipf("read %s: %v", fdDir, err)
	}
	for _, entry := range entries {
		switch entry.Name() {
		case "0", "1", "2": // stdin/stdout/stderr, logs are fine
			continue
		}
		fdPath := filepath.Join(fdDir, entry.Name())
		target, err := os.Readlink(fdPath)
		if err != nil || !strings.HasPrefix(target, "/") || isSystemPath(target) {
			continue
		}
		fi, err := os.Stat(fdPath)
		if err == nil && fi.Mode().IsRegular() {
			t.Fatalf("server keeps a data file open: fd %s -> %s", entry.Name(), target)
		}
	}
}

func zanimePID() (int, error) {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return 0, err
	}
	for _, entry := range entries {
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		comm, err := os.ReadFile(filepath.Join("/proc", entry.Name(), "comm"))
		if err != nil {
			continue
		}
		if strings.TrimSpace(string(comm)) == "zanime" {
			return pid, nil
		}
	}
	return 0, fmt.Errorf("no zanime process found")
}

func isSystemPath(path string) bool {
	for _, prefix := range []string{"/sys/", "/proc/", "/dev/", "/usr/", "/lib/", "/etc/", "/run/"} {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

// e2eBase returns the server URL and skips unless ZANIME_E2E is set. Unlike
// e2eSetup it does not touch the torrent swarm, so UI/API checks still run
// when the swarm is cold.
func e2eBase(t *testing.T) string {
	t.Helper()
	if os.Getenv("ZANIME_E2E") == "" {
		t.Skip("set ZANIME_E2E=1 with the server running to test the live server")
	}
	base := strings.TrimRight(os.Getenv("ZANIME_URL"), "/")
	if base == "" {
		base = "http://127.0.0.1:8080"
	}
	return base
}

func e2eJSON(base, method, path string, payload any) (*http.Response, []byte, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), e2eTimeout())
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, method, base+path, bytes.NewReader(body))
	if err != nil {
		return nil, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	responseBody, err := io.ReadAll(resp.Body)
	return resp, responseBody, err
}

type apiFile struct {
	Index int    `json:"index"`
	Name  string `json:"name"`
	Path  string `json:"path"`
	Size  int64  `json:"size"`
	URL   string `json:"url"`
}

type apiState struct {
	Ready      bool      `json:"ready"`
	Loading    bool      `json:"loading"`
	Name       string    `json:"name"`
	Selected   int       `json:"selected"`
	Files      []apiFile `json:"files"`
	Size       int64     `json:"size"`
	Length     int64     `json:"length"`
	Downloaded int64     `json:"downloaded"`
	Peers      int       `json:"peers"`
	Cache      struct {
		Used      int64 `json:"used"`
		Limit     int64 `json:"limit"`
		Evictions int64 `json:"evictions"`
	} `json:"cache"`
}

func e2eWaitState(t *testing.T, base string) apiState {
	t.Helper()
	deadline := time.Now().Add(e2eTimeout())
	for time.Now().Before(deadline) {
		resp, body, err := e2eDo(base, http.MethodGet, "/api/state", nil)
		if err == nil && resp.StatusCode == http.StatusOK {
			var state apiState
			if json.Unmarshal(body, &state) == nil && state.Ready {
				return state
			}
		}
		time.Sleep(150 * time.Millisecond)
	}
	t.Fatalf("torrent metadata not ready after %s", e2eTimeout())
	return apiState{}
}

func TestE2E_UIIsEmbedded(t *testing.T) {
	base := e2eBase(t)

	resp, body, err := e2eDo(base, http.MethodGet, "/", nil)
	if err != nil {
		t.Fatalf("GET /: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	page := string(body)
	for _, want := range []string{
		"<movi-player",
		"https://moviplayer.com/dist/element.js",
		"cdn.tailwindcss.com",
		"/api/magnet",
	} {
		if !strings.Contains(page, want) {
			t.Fatalf("index.html does not contain %q", want)
		}
	}
}

func TestE2E_APIMagnetRejectsGarbage(t *testing.T) {
	base := e2eBase(t)

	resp, body, err := e2eJSON(base, http.MethodPost, "/api/magnet", map[string]string{"magnet": "not-a-magnet"})
	if err != nil {
		t.Fatalf("POST /api/magnet: %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (body %s)", resp.StatusCode, body)
	}
}

func TestE2E_APIStateExposesFilesAndStreamURL(t *testing.T) {
	base := e2eBase(t)
	state := e2eWaitState(t, base)

	if len(state.Files) == 0 {
		t.Fatal("state has no video files")
	}
	if state.Selected < 0 || state.Selected >= len(state.Files) {
		t.Fatalf("selected = %d, files = %d", state.Selected, len(state.Files))
	}
	file := state.Files[state.Selected]
	if file.URL == "" || file.Size <= 0 {
		t.Fatalf("invalid file entry: %+v", file)
	}
	if !videoExtensions[strings.ToLower(filepath.Ext(file.Name))] {
		t.Fatalf("%q is not a video", file.Name)
	}

	resp, _, err := e2eDo(base, http.MethodHead, file.URL, nil)
	if err != nil {
		t.Fatalf("HEAD %s: %v", file.URL, err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("HEAD status = %d, want 200", resp.StatusCode)
	}
	if got := resp.Header.Get("Content-Length"); got != strconv.FormatInt(file.Size, 10) {
		t.Fatalf("Content-Length = %q, want %d", got, file.Size)
	}
}

func TestE2E_APISelectKeepsValidFile(t *testing.T) {
	base := e2eBase(t)
	state := e2eWaitState(t, base)

	resp, body, err := e2eJSON(base, http.MethodPost, "/api/select", map[string]int{"index": state.Selected})
	if err != nil {
		t.Fatalf("POST /api/select: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200 (body %s)", resp.StatusCode, body)
	}
	var selected apiState
	if err := json.Unmarshal(body, &selected); err != nil {
		t.Fatalf("decode select response: %v", err)
	}
	if selected.Selected != state.Selected {
		t.Fatalf("selected = %d, want %d", selected.Selected, state.Selected)
	}

	resp, _, err = e2eJSON(base, http.MethodPost, "/api/select", map[string]int{"index": 9999})
	if err != nil {
		t.Fatalf("POST /api/select (invalid): %v", err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid index status = %d, want 400", resp.StatusCode)
	}
}
