// HLS proxying: master/variant playlist rewriting and segment piping.
package stream

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"zanime/internal/hianime"
)

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
// the browser waited for a whole segment before it saw byte one — with a
// double hop that is exactly the buffering that makes playback stutter.
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
	io.Copy(w, resp.Body)
}

// StreamMaster proxies the upstream master playlist with every variant URI
// repointed at this proxy. Only the master needs rewriting: the variant
// playlists keep their relative segment names, which then resolve back into
// /api/hls/{id}/{mode}/{quality}/ — the identity the segment handler reads.
// A relative URI resolved against a query-string base drops that query
// (RFC 3986), which is why segments used to come from the default variant
// whatever quality was picked.
func StreamMaster(w http.ResponseWriter, r *http.Request, id string, src *hianime.Source, mode string) {
	resp, err := FetchUpstream(r.Context(), src.Master, src.Referer)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	byURL := make(map[string]string, len(src.Qualities))
	for _, q := range src.Qualities {
		byURL[q.URL] = q.Label
	}
	base, _ := url.Parse(src.Master)
	out := make([]string, 0, len(src.Qualities)*2)
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#EXT-X-I-FRAME-STREAM-INF") {
			continue
		}
		if strings.HasPrefix(line, "#") {
			out = append(out, line)
			continue
		}
		ref, err := url.Parse(line)
		if err != nil {
			continue
		}
		label, ok := byURL[base.ResolveReference(ref).String()]
		if !ok {
			// ParseMaster rejected this variant (no usable RESOLUTION); keeping
			// the STREAM-INF would hand the player a variant with no URI.
			if n := len(out); n > 0 && strings.HasPrefix(out[n-1], "#EXT-X-STREAM-INF") {
				out = out[:n-1]
			}
			continue
		}
		out = append(out, "/api/hls/"+id+"/"+mode+"/"+label+"/index.m3u8")
	}
	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control", "no-cache")
	io.WriteString(w, strings.Join(out, "\n")+"\n")
}

// StreamVariant proxies a media playlist, repointing every segment URI —
// relative or absolute, whatever host — at this proxy. The upstream CDN hosts
// segments on separate hosts under camouflaged names (seg-1....jpg), so the
// segment is passed as its base64url'd absolute URL.
func StreamVariant(w http.ResponseWriter, r *http.Request, id, mode, quality, variant, referer string) {
	resp, err := FetchUpstream(r.Context(), variant, referer)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	base, _ := url.Parse(variant)
	out := make([]string, 0, 1024)
	for _, line := range strings.Split(string(body), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			out = append(out, line)
			continue
		}
		ref, err := url.Parse(line)
		if err != nil {
			continue
		}
		seg := base.ResolveReference(ref).String()
		out = append(out, "/api/hls/"+id+"/"+mode+"/"+quality+"/s/"+base64.RawURLEncoding.EncodeToString([]byte(seg)))
	}
	w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
	w.Header().Set("Cache-Control", "no-cache")
	io.WriteString(w, strings.Join(out, "\n")+"\n")
}

// MegaSegmentType sniffs a segment's real content type from its bytes: the
// upstream camouflages TS segments as image/document filenames. Magic bytes:
// MPEG-TS sync 0x47 at 0 and 188-byte stride, MP4 `ftyp` box at offset 4.
func MegaSegmentType(head []byte) string {
	if len(head) >= 1 && head[0] == 0x47 {
		return "video/mp2t"
	}
	if len(head) >= 12 && string(head[4:8]) == "ftyp" {
		return "video/mp4"
	}
	return "application/octet-stream"
}

// ProbeSegment peeks the first bytes of the upstream segment to sniff its
// content type, closing the probe body before the caller re-fetches.
func ProbeSegment(ctx context.Context, rawURL, referer string) (string, bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", false
	}
	req.Header.Set("User-Agent", hianime.UserAgent)
	req.Header.Set("Referer", referer)
	resp, err := hianime.StreamClient.Do(req)
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return "", false
	}
	head := make([]byte, 512)
	n, _ := io.ReadFull(resp.Body, head)
	return MegaSegmentType(head[:n]), true
}
