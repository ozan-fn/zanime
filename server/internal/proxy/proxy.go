package proxy

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/go-resty/resty/v2"
)

const defaultUA = "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 " +
	"(KHTML, like Gecko) Chrome/131.0.0.0 Safari/537.36"

var (
	ErrInvalidURL  = errors.New("invalid url")
	ErrPrivateHost = errors.New("private hosts not allowed")
	uriAttr        = regexp.MustCompile(`URI="([^"]+)"`)
)

type Upstream struct {
	StatusCode int
	Header     http.Header
	Body       io.ReadCloser
	FinalURL   *url.URL
}

type Client struct {
	http         *resty.Client
	allowPrivate bool
}

func New() *Client {
	return newClient(false)
}

// NewAllowingPrivate is for tests; production only reaches public hosts.
func NewAllowingPrivate() *Client {
	return newClient(true)
}

func newClient(allowPrivate bool) *Client {
	return &Client{
		http:         resty.New().SetTimeout(60 * time.Second),
		allowPrivate: allowPrivate || os.Getenv("ALLOW_PRIVATE_HOSTS") == "1",
	}
}

func (c *Client) Fetch(
	ctx context.Context,
	target, referer, origin, rangeHeader string,
) (*Upstream, error) {
	u, err := url.Parse(target)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, ErrInvalidURL
	}
	if !c.allowPrivate && !isPublicHost(u.Host) {
		return nil, ErrPrivateHost
	}

	request := c.http.R().
		SetContext(ctx).
		SetHeader("Accept", "*/*").
		SetHeader("User-Agent", defaultUA).
		SetDoNotParseResponse(true)
	if referer != "" {
		request.SetHeader("Referer", referer)
	}
	if origin != "" {
		request.SetHeader("Origin", origin)
	}
	if rangeHeader != "" {
		request.SetHeader("Range", rangeHeader)
	}

	resp, err := request.Get(u.String())
	if err != nil {
		return nil, fmt.Errorf("upstream request: %w", err)
	}
	if resp.RawBody() == nil {
		return nil, errors.New("upstream body missing")
	}
	return &Upstream{
		StatusCode: resp.StatusCode(),
		Header:     resp.Header(),
		Body:       resp.RawBody(),
		FinalURL:   resp.RawResponse.Request.URL,
	}, nil
}

func IsPlaylist(contentType, path string) bool {
	return strings.Contains(contentType, "mpegurl") ||
		strings.HasSuffix(strings.ToLower(path), ".m3u8")
}

// RewritePlaylist resolves every URI against the manifest and routes it through
// /stream, keeping the provider headers for the child requests.
func RewritePlaylist(body string, base *url.URL, referer, origin string) string {
	lines := strings.Split(body, "\n")
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			lines[i] = uriAttr.ReplaceAllStringFunc(trimmed, func(match string) string {
				uri := match[len(`URI="`) : len(match)-1]
				return `URI="` + proxyURL(resolve(base, uri), referer, origin) + `"`
			})
			continue
		}
		lines[i] = proxyURL(resolve(base, trimmed), referer, origin)
	}
	return strings.Join(lines, "\n")
}

func resolve(base *url.URL, reference string) string {
	ref, err := url.Parse(reference)
	if err != nil {
		return reference
	}
	return base.ResolveReference(ref).String()
}

func proxyURL(raw, referer, origin string) string {
	values := url.Values{}
	values.Set("url", raw)
	if referer != "" {
		values.Set("referer", referer)
	}
	if origin != "" {
		values.Set("origin", origin)
	}
	return "/stream?" + values.Encode()
}

func isPublicHost(host string) bool {
	hostname := host
	if h, _, err := net.SplitHostPort(host); err == nil {
		hostname = h
	}
	ips, err := net.LookupIP(hostname)
	if err != nil || len(ips) == 0 {
		log.Printf("proxy: unresolvable host %q", hostname)
		return false
	}
	for _, ip := range ips {
		if ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() {
			return false
		}
	}
	return true
}
