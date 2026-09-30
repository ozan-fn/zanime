// HLS proxying: master/variant playlist rewriting and segment piping.
package stream

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"

	"zanime/internal/hianime"
)

// copyBufPool hands every proxied body the same 32 KB scratch buffer instead of
// letting io.Copy allocate one per request. A player fetching a dozen segments
// in parallel used to mean a dozen fresh buffers; the pool is what keeps the
// proxy's resident memory flat under exactly that load.
var copyBufPool = sync.Pool{New: func() any { b := make([]byte, 32<<10); return &b }}

// copyUpstream pipes a response body to the client through a pooled buffer.
func copyUpstream(w io.Writer, body io.Reader) {
	buf := copyBufPool.Get().(*[]byte)
	defer copyBufPool.Put(buf)
	_, _ = io.CopyBuffer(w, body, *buf)
}

// FetchUpstream issues the proxied GET with the headers the stream host wants
// and fails before any byte reaches the client when upstream is not 2xx.
func FetchUpstream(ctx context.Context, rawURL, referer string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", hianime.UserAgent)
	req.Header.Set("Accept", "*/*")
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	resp, err := hianime.StreamClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		resp.Body.Close()
		return nil, fmt.Errorf("upstream HTTP %d", resp.StatusCode)
	}
	return resp, nil
}

// StreamUpstream copies an upstream response into the client instead of
// buffering it first. Every proxied byte used to sit behind an io.ReadAll, so
// the browser waited for a whole body before it saw byte one — with a double hop
// that is exactly the buffering that makes playback stutter.
func StreamUpstream(w http.ResponseWriter, r *http.Request, rawURL, referer, contentType, cachePolicy string) {
	resp, err := FetchUpstream(r.Context(), rawURL, referer)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	if contentType != "" {
		w.Header().Set("Content-Type", contentType)
	} else {
		w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
	}
	w.Header().Set("Cache-Control", cachePolicy)
	w.WriteHeader(http.StatusOK)
	copyUpstream(w, resp.Body)
}

// StreamSegment proxies one media segment in a single upstream request: it reads
// the first bytes to sniff the real content type, then streams them and the rest
// of the body on. Probing used to be a second full GET, which doubled the
// upstream load of every segment the player fetched.
func StreamSegment(w http.ResponseWriter, r *http.Request, rawURL, referer, cachePolicy string) {
	resp, err := FetchUpstream(r.Context(), rawURL, referer)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	br := bufio.NewReaderSize(resp.Body, 4<<10)
	head := make([]byte, 512)
	n, err := io.ReadFull(br, head)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	head = head[:n]
	w.Header().Set("Content-Type", MegaSegmentType(head))
	w.Header().Set("Cache-Control", cachePolicy)
	w.WriteHeader(http.StatusOK)
	if _, err := w.Write(head); err != nil {
		return
	}
	copyUpstream(w, br)
}

// StreamMaster proxies the upstream master playlist with every variant URI
// repointed at this proxy. Only the master needs rewriting: the variant
// playlists keep their relative segment names, which then resolve back into
// /api/hls/{id}/{quality}/ — the identity the segment handler reads.
// A relative URI resolved against a query-string base drops that query
// (RFC 3986), which is why segments used to come from the default variant
// whatever quality was picked.
// It returns an error instead of answering one itself so the caller can drop
// the stale resolve and try once more with a fresh token.
func StreamMaster(w http.ResponseWriter, r *http.Request, id string, src *hianime.Source) error {
	body, err := playlist(r.Context(), src.Master, src.Referer)
	if err != nil {
		return err
	}
	byURL := make(map[string]string, len(src.Qualities))
	for _, q := range src.Qualities {
		byURL[q.URL] = q.Label
	}
	base, _ := url.Parse(src.Master)
	out := rewritePlaylist(body, func(line string) string {
		ref, err := url.Parse(line)
		if err != nil {
			return ""
		}
		label, ok := byURL[base.ResolveReference(ref).String()]
		if !ok {
			// ParseMaster rejected this variant (no usable RESOLUTION).
			return ""
		}
		return "/api/hls/" + id + "/" + label + "/index.m3u8"
	})
	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control", "no-cache")
	io.WriteString(w, out)
	return nil
}

// StreamVariant proxies a media playlist, repointing every segment URI —
// relative or absolute, whatever host — at this proxy. Upstream camouflages its
// segments behind image names, so the segment is passed as its base64url'd
// absolute URL instead of a file name.
func StreamVariant(w http.ResponseWriter, r *http.Request, id, quality, variant, referer string) error {
	body, err := playlist(r.Context(), variant, referer)
	if err != nil {
		return err
	}
	base, _ := url.Parse(variant)
	out := rewritePlaylist(body, func(line string) string {
		ref, err := url.Parse(line)
		if err != nil {
			return ""
		}
		seg := base.ResolveReference(ref).String()
		return "/api/hls/" + id + "/" + quality + "/s/" + base64.RawURLEncoding.EncodeToString([]byte(seg))
	})
	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control", "no-cache")
	io.WriteString(w, out)
	return nil
}

// playlist fetches a playlist body, capped: playlists are a few KB, so a host
// that answers with something huge must not be able to hold memory.
func playlist(ctx context.Context, rawURL, referer string) (string, error) {
	resp, err := FetchUpstream(ctx, rawURL, referer)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, hianime.MaxTinyBytes))
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// rewritePlaylist maps every URI line through rewrite, keeping tag lines as
// they are. A rewrite returning "" drops the line — and the #EXT-X-STREAM-INF
// that announced it, so the player is never handed a variant with no URI.
func rewritePlaylist(body string, rewrite func(string) string) string {
	var out []string
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#EXT-X-I-FRAME-STREAM-INF") {
			continue
		}
		if strings.HasPrefix(line, "#") {
			out = append(out, line)
			continue
		}
		target := rewrite(line)
		if target == "" {
			if n := len(out); n > 0 && strings.HasPrefix(out[n-1], "#EXT-X-STREAM-INF") {
				out = out[:n-1]
			}
			continue
		}
		out = append(out, target)
	}
	return strings.Join(out, "\n") + "\n"
}

// MegaSegmentType sniffs a segment's real content type from its bytes: the
// upstream camouflages TS segments as image/document filenames. Magic bytes:
// MPEG-TS sync 0x47 at 0, MP4 `ftyp` box at offset 4.
func MegaSegmentType(head []byte) string {
	if len(head) >= 1 && head[0] == 0x47 {
		return "video/mp2t"
	}
	if len(head) >= 12 && string(head[4:8]) == "ftyp" {
		return "video/mp4"
	}
	return "application/octet-stream"
}
