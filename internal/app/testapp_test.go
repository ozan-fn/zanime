package app

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"
)

// testApp menjalankan app fiber di port acak supaya test memakai routing +
// middleware yang sama dengan produksi.
type testApp struct {
	t    *testing.T
	srv  *Server
	base string
	http *http.Client
}

// startTestApp menjalankan Server pada port acak. register opsional untuk
// memasang route khusus test; tanpa itu seluruh route produksi dipasang.
func startTestApp(t *testing.T, srv *Server, register func(*fiber.App)) *testApp {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	app := fiber.New(fiber.Config{DisableStartupMessage: true})
	if register == nil {
		srv.Register(app)
	} else {
		register(app)
	}
	go func() { _ = app.Listener(ln) }()
	ta := &testApp{
		t:    t,
		srv:  srv,
		base: "http://" + ln.Addr().String(),
		http: &http.Client{Timeout: 10 * time.Second},
	}
	t.Cleanup(func() { _ = app.Shutdown() })
	return ta
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
