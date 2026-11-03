package app

import (
	"archive/zip"
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFetchSubtitleText(t *testing.T) {
	srt := "1\n00:00:01,000 --> 00:00:02,000\nHalo\n"
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("sub.srt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte(srt)); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	var gotReferer, gotOrigin string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotReferer, gotOrigin = r.Header.Get("Referer"), r.Header.Get("Origin")
		_, _ = w.Write(buf.Bytes())
	}))
	defer srv.Close()

	text, err := fetchSubtitleText(context.Background(), srv.URL, map[string]string{
		"Referer": "https://miruro.example/",
		"Origin":  "https://miruro.example",
	})
	if err != nil {
		t.Fatalf("fetchSubtitleText: %v", err)
	}
	if gotReferer != "https://miruro.example/" || gotOrigin != "https://miruro.example" {
		t.Fatalf("header slug tidak diteruskan: referer=%q origin=%q", gotReferer, gotOrigin)
	}
	if !strings.Contains(text, "00:00:01,000 --> 00:00:02,000") {
		t.Fatalf("zip tidak diekstrak ke teks: %q", text)
	}
}
