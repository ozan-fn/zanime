package app

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/goceleris/celeris"
)

// testApp menjalankan app celeris di port acak (engine std) supaya test memakai
// routing + middleware yang sama dengan produksi, tanpa chi.
type testApp struct {
	t    *testing.T
	srv  *Server
	base string
	http *http.Client
}

// startTestApp menjalankan Server pada port acak. register opsional untuk
// memasang route khusus test; tanpa itu seluruh route produksi dipasang.
func startTestApp(t *testing.T, srv *Server, register func(*celeris.Server)) *testApp {
	t.Helper()
	mux := celeris.New(celeris.Config{Addr: "127.0.0.1:0"})
	if register == nil {
		srv.Register(mux)
	} else {
		register(mux)
	}
	go func() { _ = mux.Start() }()
	deadline := time.Now().Add(5 * time.Second)
	for mux.Addr() == nil {
		if time.Now().After(deadline) {
			t.Fatal("server uji tidak siap")
		}
		time.Sleep(5 * time.Millisecond)
	}
	app := &testApp{
		t:    t,
		srv:  srv,
		base: "http://" + mux.Addr().String(),
		http: &http.Client{Timeout: 10 * time.Second},
	}
	t.Cleanup(func() { _ = mux.Shutdown(context.Background()) })
	return app
}

// ServeHTTP menjalankan satu permintaan ke server uji lalu menulis balasannya
// ke recorder, supaya call site test tetap `r.ServeHTTP(res, req)`.
func (a *testApp) ServeHTTP(res *httptest.ResponseRecorder, req *http.Request) {
	a.t.Helper()
	base, err := url.Parse(a.base)
	if err != nil {
		a.t.Fatal(err)
	}
	req.URL.Scheme, req.URL.Host = base.Scheme, base.Host
	req.RequestURI = ""
	// Transport Go menambahkan Accept-Encoding: gzip sendiri kalau header ini
	// kosong, lalu mencoba men-decompress balasan — menyembunyikan apa yang
	// dikirim server. Test yang tidak peduli kompresi meminta identity.
	if req.Header.Get("Accept-Encoding") == "" {
		req.Header.Set("Accept-Encoding", "identity")
	}
	if req.Host == "" {
		req.Host = base.Host
	}
	resp, err := a.http.Do(req)
	if err != nil {
		a.t.Fatalf("%s %s: %v", req.Method, req.URL.Path, err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		a.t.Fatalf("baca body %s (accept=%q status=%d hdr=%v partial=%q): %v", req.URL.Path,
			req.Header.Get("Accept-Encoding"), resp.StatusCode, resp.Header, body, err)
	}
	for k, vv := range resp.Header {
		for _, v := range vv {
			res.Header().Add(k, v)
		}
	}
	res.Code = resp.StatusCode
	_, _ = res.Body.Write(body)
}

// wsURL mengubah base http → ws untuk test websocket.
func (a *testApp) wsURL(path string) string {
	return "ws" + strings.TrimPrefix(a.base, "http") + path
}
