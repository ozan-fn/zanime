// Package proxy mengarahkan klien resty lewat proxy (HTTP atau SOCKS5) dan
// memutar kandidat IP webshare 1–10 secara paralel saat IP aktif diblokir.
package proxy

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/go-resty/resty/v2"
	xproxy "golang.org/x/net/proxy"

	"zanime/internal/pool"
)

// envFile ditulis ulang saat kandidat proxy yang lolos ditemukan.
const envFile = ".env"

// maxCandidate: sufiks username webshare yang dicoba (-1 … -10).
const maxCandidate = 10

var suffixRe = regexp.MustCompile(`^(.+)-(\d+)$`)

// ErrBlocked menandai request yang ditolak IP/proxy (403/407/429/Turnstile),
// sehingga Retry boleh berganti proxy.
var ErrBlocked = errors.New("proxy diblokir")

// Blocked membungkus err sebagai blokir proxy.
func Blocked(err error) error { return fmt.Errorf("%v: %w", err, ErrBlocked) }

type dialFunc func(ctx context.Context, network, addr string) (net.Conn, error)

// live adalah konfigurasi proxy yang sedang aktif.
type live struct {
	httpProxy *url.URL
	dial      dialFunc
}

var active atomic.Pointer[live]

type state struct {
	tmpl     *url.URL // tanpa userinfo
	user     string   // username tanpa sufiks -n
	pass     string
	webshare bool // host webshare + username bersufiks -n → boleh rotasi
	current  string
}

var (
	mu sync.Mutex
	st *state
)

// Configure memasang proxy pada klien resty. rawURL kosong berarti tanpa proxy.
// Bila username webshare berakhiran -<n>, rotasi 1–10 disiapkan.
func Configure(client *resty.Client, rawURL string) error {
	if rawURL == "" {
		return nil
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("proxy URL: %w", err)
	}
	cfg, err := build(u)
	if err != nil {
		return err
	}
	active.Store(cfg)

	mu.Lock()
	if st == nil {
		st = newState(u)
	}
	st.current = rawURL
	mu.Unlock()

	transport, err := client.Transport()
	if err != nil {
		return err
	}
	transport.Proxy = func(*http.Request) (*url.URL, error) {
		if l := active.Load(); l != nil {
			return l.httpProxy, nil
		}
		return nil, nil
	}
	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		if l := active.Load(); l != nil && l.dial != nil {
			return l.dial(ctx, network, addr)
		}
		return (&net.Dialer{}).DialContext(ctx, network, addr)
	}
	return nil
}

// Fixed memasang proxy tetap pada klien tanpa menyentuh proxy aktif global.
// Dipakai saat menguji kandidat secara paralel.
func Fixed(client *resty.Client, rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("proxy URL: %w", err)
	}
	cfg, err := build(u)
	if err != nil {
		return err
	}
	transport, err := client.Transport()
	if err != nil {
		return err
	}
	if cfg.dial != nil {
		transport.Proxy = nil
		transport.DialContext = cfg.dial
		return nil
	}
	transport.Proxy = http.ProxyURL(cfg.httpProxy)
	return nil
}

func newState(u *url.URL) *state {
	s := &state{tmpl: &url.URL{Scheme: u.Scheme, Host: u.Host, Path: u.Path}}
	if u.User != nil {
		s.user = u.User.Username()
		s.pass, _ = u.User.Password()
	}
	if m := suffixRe.FindStringSubmatch(s.user); m != nil {
		if n, err := strconv.Atoi(m[2]); err == nil && n >= 1 && n <= maxCandidate {
			s.user = m[1]
			s.webshare = strings.Contains(u.Host, "webshare.io")
		}
	}
	return s
}

// candidate menyusun URL proxy untuk sufiks n.
func (s *state) candidate(n int) string {
	u := *s.tmpl
	if s.user != "" {
		u.User = url.UserPassword(fmt.Sprintf("%s-%d", s.user, n), s.pass)
	}
	return u.String()
}

func build(u *url.URL) (*live, error) {
	switch u.Scheme {
	case "http", "https":
		return &live{httpProxy: u}, nil
	case "socks5", "socks5h":
		var auth *xproxy.Auth
		if u.User != nil {
			pass, _ := u.User.Password()
			auth = &xproxy.Auth{User: u.User.Username(), Password: pass}
		}
		d, err := xproxy.SOCKS5("tcp", u.Host, auth, xproxy.Direct)
		if err != nil {
			return nil, fmt.Errorf("proxy socks5: %w", err)
		}
		if cd, ok := d.(xproxy.ContextDialer); ok {
			return &live{dial: cd.DialContext}, nil
		}
		return &live{dial: func(ctx context.Context, network, addr string) (net.Conn, error) {
			type result struct {
				conn net.Conn
				err  error
			}
			ch := make(chan result, 1)
			go func() {
				conn, err := d.Dial(network, addr)
				ch <- result{conn, err}
			}()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case r := <-ch:
				return r.conn, r.err
			}
		}}, nil
	}
	return nil, fmt.Errorf("skema proxy %q tidak didukung (pakai http:// atau socks5://)", u.Scheme)
}

// Retry menjalankan fn. Bila gagal karena proxy diblokir dan proxy aktif adalah
// webshare bersufiks -n, kandidat 1–10 diuji paralel lewat pool; yang lolos
// dipasang dan disimpan ke .env, lalu fn diulang sekali. Error baru
// dikembalikan setelah semua kandidat dicoba.
func Retry(ctx context.Context, fn func() error, probe func(context.Context, string) error) error {
	err := fn()
	if err == nil {
		return nil
	}
	if !errors.Is(err, ErrBlocked) || !webshare() {
		return err
	}
	if _, ferr := failover(ctx, probe); ferr != nil {
		return fmt.Errorf("%v; %w", ferr, err)
	}
	return fn()
}

// failover menguji kandidat 1–10 serentak dan memasang yang pertama berhasil.
func failover(ctx context.Context, probe func(context.Context, string) error) (string, error) {
	mu.Lock()
	if st == nil || !st.webshare {
		mu.Unlock()
		return "", errors.New("bukan proxy webshare bersufiks -n")
	}
	candidates := make([]string, 0, maxCandidate)
	for n := 1; n <= maxCandidate; n++ {
		if cand := st.candidate(n); cand != st.current {
			candidates = append(candidates, cand)
		}
	}
	mu.Unlock()

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		once    sync.Once
		winner  string
		errMu   sync.Mutex
		lastErr error
	)
	group := pool.Get().NewGroupContext(ctx)
	for _, cand := range candidates {
		cand := cand
		group.Submit(func() {
			if ctx.Err() != nil {
				return
			}
			if err := probe(ctx, cand); err != nil {
				errMu.Lock()
				lastErr = err
				errMu.Unlock()
				return
			}
			once.Do(func() {
				winner = cand
				cancel() // kandidat lain tidak perlu diteruskan
			})
		})
	}
	_ = group.Wait()
	if winner == "" {
		if lastErr != nil {
			return "", fmt.Errorf("semua proxy gagal: %w", lastErr)
		}
		return "", errors.New("semua proxy gagal")
	}
	if err := install(winner); err != nil {
		return "", err
	}
	return winner, nil
}

// install memasang kandidat sebagai proxy aktif dan menyimpannya ke .env.
func install(rawURL string) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return err
	}
	cfg, err := build(u)
	if err != nil {
		return err
	}
	active.Store(cfg)
	mu.Lock()
	if st != nil {
		st.current = rawURL
	}
	mu.Unlock()
	_ = os.Setenv("PROXY_URL", rawURL)
	persist(rawURL)
	return nil
}

func webshare() bool {
	mu.Lock()
	defer mu.Unlock()
	return st != nil && st.webshare
}

// persist mengganti baris PROXY_URL di .env.
func persist(proxyURL string) {
	data, err := os.ReadFile(envFile)
	if err != nil {
		return
	}
	lines := strings.Split(string(data), "\n")
	found := false
	for i, ln := range lines {
		if strings.HasPrefix(strings.TrimSpace(ln), "PROXY_URL=") {
			lines[i] = "PROXY_URL=" + proxyURL
			found = true
		}
	}
	if !found {
		lines = append(lines, "PROXY_URL="+proxyURL, "")
	}
	_ = os.WriteFile(envFile, []byte(strings.Join(lines, "\n")), 0o644)
}

// reset mengembalikan state global ke awal (dipakai test).
func reset() {
	mu.Lock()
	st = nil
	mu.Unlock()
	active.Store(nil)
}
