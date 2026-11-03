package proxy

import (
	"context"
	"errors"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/go-resty/resty/v2"
)

// TestRetryNonWebshareOnce: proxy biasa tidak diputar — fn dipanggil sekali
// dan error aslinya dikembalikan.
func TestRetryNonWebshareOnce(t *testing.T) {
	reset()
	t.Cleanup(reset)
	if err := Configure(resty.New(), "http://user:pass@proxy.example.com:80"); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	probes := 0
	err := Retry(context.Background(), func() error {
		calls.Add(1)
		return Blocked(errors.New("HTTP 403 Forbidden"))
	}, func(context.Context, string) error {
		probes++
		return nil
	})
	if !errors.Is(err, ErrBlocked) {
		t.Fatalf("err = %v, mau ErrBlocked", err)
	}
	if calls.Load() != 1 || probes != 0 {
		t.Fatalf("fn=%d probe=%d, mau 1/0", calls.Load(), probes)
	}
}

// TestRetryWebshareSwitches: webshare bersufiks -n menguji kandidat 1–10
// paralel, memasang yang lolos, menyimpannya ke PROXY_URL, lalu mengulang fn.
func TestRetryWebshareSwitches(t *testing.T) {
	reset()
	t.Cleanup(reset)
	t.Setenv("PROXY_URL", "")
	if err := Configure(resty.New(), "http://user-1:pass@p.webshare.io:80"); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	probes := map[string]bool{}
	probe := func(_ context.Context, candidate string) error {
		mu.Lock()
		probes[candidate] = true
		mu.Unlock()
		if strings.Contains(candidate, "user-4:") {
			return nil
		}
		return errors.New("diblokir")
	}
	var calls atomic.Int32
	err := Retry(context.Background(), func() error {
		if calls.Add(1) == 1 {
			return Blocked(errors.New("HTTP 403 Forbidden"))
		}
		return nil
	}, probe)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got := os.Getenv("PROXY_URL"); got != "http://user-4:pass@p.webshare.io:80" {
		t.Fatalf("PROXY_URL = %q, mau kandidat -4", got)
	}
	if calls.Load() != 2 {
		t.Fatalf("fn dipanggil %d kali, mau 2", calls.Load())
	}
	if probes["http://user-1:pass@p.webshare.io:80"] {
		t.Fatalf("kandidat -1 (aktif) ikut diuji: %v", probes)
	}
}

// TestRetryAllCandidatesFail: error baru muncul setelah semua kandidat gagal.
func TestRetryAllCandidatesFail(t *testing.T) {
	reset()
	t.Cleanup(reset)
	t.Setenv("PROXY_URL", "")
	if err := Configure(resty.New(), "http://user-1:pass@p.webshare.io:80"); err != nil {
		t.Fatal(err)
	}
	original := Blocked(errors.New("HTTP 403 Forbidden"))
	err := Retry(context.Background(), func() error { return original },
		func(context.Context, string) error { return errors.New("timeout") })
	if !errors.Is(err, ErrBlocked) || !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("err = %v, mau ErrBlocked + alasan kandidat gagal", err)
	}
	if got := os.Getenv("PROXY_URL"); got != "" {
		t.Fatalf("PROXY_URL berubah jadi %q padahal tidak ada yang lolos", got)
	}
}
