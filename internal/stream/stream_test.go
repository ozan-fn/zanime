package stream

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
)

func testData(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i*31 + i/7)
	}
	return b
}

func streamRouter(data []byte) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	handler := func(c *gin.Context) {
		Serve(c, "episode-01.mkv", bytes.NewReader(data))
	}
	r.GET("/stream", handler)
	r.HEAD("/stream", handler)
	return r
}

func serve(t *testing.T, router http.Handler, method, byteRange string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, "/stream", nil)
	if byteRange != "" {
		req.Header.Set("Range", byteRange)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func TestServe_HeadReportsFullSize(t *testing.T) {
	data := testData(512 << 10)
	w := serve(t, streamRouter(data), http.MethodHead, "")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if got := w.Header().Get("Content-Length"); got != strconv.Itoa(len(data)) {
		t.Fatalf("Content-Length = %q, want %d", got, len(data))
	}
	if got := w.Header().Get("Accept-Ranges"); got != "bytes" {
		t.Fatalf("Accept-Ranges = %q, want bytes", got)
	}
	if w.Body.Len() != 0 {
		t.Fatalf("HEAD returned %d body bytes, want 0", w.Body.Len())
	}
}

func TestServe_RangeReturnsPartialContent(t *testing.T) {
	data := testData(512 << 10)
	w := serve(t, streamRouter(data), http.MethodGet, "bytes=1000-1999")

	if w.Code != http.StatusPartialContent {
		t.Fatalf("status = %d, want 206", w.Code)
	}
	wantRange := fmt.Sprintf("bytes 1000-1999/%d", len(data))
	if got := w.Header().Get("Content-Range"); got != wantRange {
		t.Fatalf("Content-Range = %q, want %q", got, wantRange)
	}
	if !bytes.Equal(w.Body.Bytes(), data[1000:2000]) {
		t.Fatal("body does not match requested range")
	}
}

func TestServe_SeekReadsFromRequestedOffset(t *testing.T) {
	data := testData(512 << 10)
	const start, end = 300_000, 300_999
	w := serve(t, streamRouter(data), http.MethodGet, fmt.Sprintf("bytes=%d-%d", start, end))

	if w.Code != http.StatusPartialContent {
		t.Fatalf("status = %d, want 206", w.Code)
	}
	if !bytes.Equal(w.Body.Bytes(), data[start:end+1]) {
		t.Fatal("seek returned bytes from the wrong offset")
	}
}

func TestServe_UnsatisfiableRangeReturns416(t *testing.T) {
	data := testData(4096)
	w := serve(t, streamRouter(data), http.MethodGet, "bytes=4096-")

	if w.Code != http.StatusRequestedRangeNotSatisfiable {
		t.Fatalf("status = %d, want 416", w.Code)
	}
	wantRange := fmt.Sprintf("bytes */%d", len(data))
	if got := w.Header().Get("Content-Range"); got != wantRange {
		t.Fatalf("Content-Range = %q, want %q", got, wantRange)
	}
}

func TestServe_OpenEndedRangeRunsToEOF(t *testing.T) {
	data := testData(64 << 10)
	w := serve(t, streamRouter(data), http.MethodGet, "bytes=65500-")

	if w.Code != http.StatusPartialContent {
		t.Fatalf("status = %d, want 206", w.Code)
	}
	if !bytes.Equal(w.Body.Bytes(), data[65500:]) {
		t.Fatal("open-ended range body mismatch")
	}
}
