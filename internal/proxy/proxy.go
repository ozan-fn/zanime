// Package proxy mengarahkan klien resty lewat proxy SOCKS5 (mis. webshare).
package proxy

import (
	"context"
	"fmt"
	"net"
	"net/url"

	"github.com/go-resty/resty/v2"
	xproxy "golang.org/x/net/proxy"
)

// Configure memasang proxy pada klien resty. Skema http/https memakai proxy
// HTTP biasa; socks5/socks5h memakai dialer SOCKS5. rawURL kosong berarti
// tanpa proxy (koneksi langsung).
func Configure(client *resty.Client, rawURL string) error {
	if rawURL == "" {
		return nil
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("proxy URL: %w", err)
	}
	switch u.Scheme {
	case "http", "https":
		client.SetProxy(rawURL)
		return nil
	case "socks5", "socks5h":
	default:
		return fmt.Errorf("skema proxy %q tidak didukung (pakai http:// atau socks5://)", u.Scheme)
	}
	var auth *xproxy.Auth
	if u.User != nil {
		pass, _ := u.User.Password()
		auth = &xproxy.Auth{User: u.User.Username(), Password: pass}
	}
	dialer, err := xproxy.SOCKS5("tcp", u.Host, auth, xproxy.Direct)
	if err != nil {
		return fmt.Errorf("proxy socks5: %w", err)
	}
	transport, err := client.Transport()
	if err != nil {
		return err
	}
	transport.Proxy = nil
	if cd, ok := dialer.(xproxy.ContextDialer); ok {
		transport.DialContext = cd.DialContext
		return nil
	}
	transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		type result struct {
			conn net.Conn
			err  error
		}
		ch := make(chan result, 1)
		go func() {
			conn, err := dialer.Dial(network, addr)
			ch <- result{conn, err}
		}()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case r := <-ch:
			return r.conn, r.err
		}
	}
	return nil
}
