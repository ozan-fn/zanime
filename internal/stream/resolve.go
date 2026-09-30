// Package stream resolves megaplay embeds and proxies the HLS playlists and
// segments the browser cannot fetch itself (the stream host demands a Referer
// the player cannot send).
package stream

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"zanime/internal/hianime"
)

var reDataHash = regexp.MustCompile(`data-hash="([^"]*)"`)

// ServerHashes lists every server of the wanted audio mode whose embed the
// resolver understands, in upstream order — resolveOne tries them in turn, so
// a dead embed falls over to the next instead of failing the episode. Sub and
// dub are separate embeds; upstream names its servers HD-2, Vidstream-2 and
// the like, so the name is not filtered — the mode is what separates streams.
// ZokoAnime embeds use a different player (blob __P, not megaplay) and are
// skipped: resolve would die on data-id there.
func ServerHashes(page, mode string) []string {
	var out []string
	for _, item := range strings.Split(page, "server-item")[1:] {
		if !strings.Contains(item, `data-type="`+mode+`"`) {
			continue
		}
		m := reDataHash.FindStringSubmatch(item)
		if m == nil {
			continue
		}
		if embed, err := base64.StdEncoding.DecodeString(m[1]); err != nil ||
			!strings.Contains(string(embed), "megaplay.buzz") {
			continue
		}
		out = append(out, m[1])
	}
	return out
}

type embedConfig struct {
	Src       string `json:"src"`
	Subtitles []struct {
		Lang    string `json:"lang"`
		Src     string `json:"src"`
		Default bool   `json:"default"`
	} `json:"subtitles"`
}

// DeobfuscateBlob decodes the old zokoanime embed's base64(json XOR key) blob.
func DeobfuscateBlob(blob string) (string, error) {
	trimmed := strings.TrimSpace(blob)
	raw, err := base64.StdEncoding.DecodeString(trimmed)
	if err != nil {
		if raw, err = base64.RawStdEncoding.DecodeString(trimmed); err != nil {
			return "", fmt.Errorf("blob is not base64: %w", err)
		}
	}
	key := []byte(hianime.EmbedXORKey)
	out := make([]byte, len(raw))
	for i, b := range raw {
		out[i] = b ^ key[i%len(key)]
	}
	return string(out), nil
}

// The megaplay embed answers a same-origin XHR (stream/getSources) with an
// AES-256-CBC token whose plaintext is the master playlist URL. Key and IV
// ship in the embed's newclient.min.js; the key is zero-padded to 32 bytes.
var (
	megaKey = append([]byte("i?LMTAx0Q6,:}50U"), make([]byte, 16)...)
	megaIV  = []byte("W0;27ToaUpl_P%'c")
)

// megaSources is the getSources payload: media ids and the encrypted token.
type megaSources struct {
	ID      string `json:"id"`
	RealID  string `json:"realid"`
	MediaID string `json:"mediaid"`
	Tracks  []struct {
		File  string `json:"file"`
		Label string `json:"label"`
	} `json:"tracks"`
	Enc string `json:"enc"`
}

// MegaDecrypt decrypts the base64url token into the master playlist URL.
func MegaDecrypt(enc string) (string, error) {
	b64 := strings.NewReplacer("-", "+", "_", "/").Replace(enc)
	if pad := len(b64) % 4; pad != 0 {
		b64 += strings.Repeat("=", 4-pad)
	}
	raw, err := base64.StdEncoding.DecodeString(b64)
	if err != nil {
		return "", fmt.Errorf("enc token: %w", err)
	}
	block, err := aes.NewCipher(megaKey)
	if err != nil {
		return "", err
	}
	if len(raw)%aes.BlockSize != 0 {
		return "", errors.New("enc token: bad length")
	}
	// CBC, bukan ECB per-blok: tiap blok ciphertext didekrip lalu di-XOR dengan
	// blok sebelumnya; blok pertama pakai IV.
	plain := make([]byte, len(raw))
	cipher.NewCBCDecrypter(block, megaIV).CryptBlocks(plain, raw)
	// Remove PKCS#7 padding.
	n := len(plain)
	p := int(plain[n-1])
	if p == 0 || p > aes.BlockSize || p > n {
		return "", errors.New("enc token: bad padding")
	}
	for _, b := range plain[n-p:] {
		if int(b) != p {
			return "", errors.New("enc token: bad padding")
		}
	}
	return string(plain[:n-p]), nil
}

// Resolve finds the stream server of the wanted audio mode and returns its
// master playlist, subtitle list and the referer the stream host demands.
func Resolve(ctx context.Context, episodeID, mode string) (*hianime.Source, error) {
	if mode != "sub" && mode != "dub" {
		mode = "sub"
	}
	page, err := hianime.Get(ctx, fmt.Sprintf(hianime.ServersAPI, url.QueryEscape(episodeID)), hianime.BaseAPI+"/")
	if err != nil {
		return nil, err
	}
	page = strings.ReplaceAll(strings.ReplaceAll(page, `\"`, `"`), `\\`, "")
	hashes := ServerHashes(page, mode)
	if len(hashes) == 0 {
		return nil, fmt.Errorf("no stream source for episode %s (%s)", episodeID, mode)
	}
	// Failover: try each megaplay server in order; a dead embed (error page,
	// getSources failure, dead master) falls over to the next.
	var lastErr error
	for _, hash := range hashes {
		src, err := ResolveOne(ctx, hash, mode)
		if err == nil {
			return src, nil
		}
		lastErr = err
	}
	return nil, lastErr
}

// ResolveOne turns one embed hash into a Source.
func ResolveOne(ctx context.Context, hash, mode string) (*hianime.Source, error) {
	embedURL, err := base64.StdEncoding.DecodeString(hash)
	if err != nil {
		return nil, fmt.Errorf("embed hash: %w", err)
	}
	embed := string(embedURL)
	// The stream host wants the embed site as referer.
	referer, err := hianime.OriginOf(embed)
	if err != nil {
		return nil, err
	}
	embedPage, err := hianime.Get(ctx, embed, hianime.BaseAPI+"/")
	if err != nil {
		return nil, err
	}
	// The megaplay embed page exposes the ids it needs as data attributes on
	// #megaplay-player; getSources answers only XHRs with the page URL as referer.
	var gs megaSources
	attr := func(name string) string {
		m := regexp.MustCompile(`data-` + name + `="([0-9]+)"`).FindStringSubmatch(embedPage)
		if m == nil {
			return ""
		}
		return m[1]
	}
	gs.ID = attr("id")
	gs.RealID = attr("realid")
	gs.MediaID = attr("mediaid")
	if gs.ID == "" {
		// megaplay answers an expired/absent Referer with a 200 "Error Code: 410"
		// page — no player attributes. Say which hop broke instead of a generic
		// "not a megaplay player".
		return nil, fmt.Errorf("embed page has no player ids (upstream error page?) for %s", embed)
	}
	master, err := megaResolve(ctx, embed, &gs)
	if err != nil {
		return nil, err
	}
	// Master and variant playlists accept the embed origin as referer, but the
	// segment CDN demands megaplay.buzz specifically — the playlist host's
	// origin gets a 403 Cloudflare block on every segment.
	qualities, err := Qualities(ctx, master, referer)
	if err != nil {
		return nil, err
	}
	src := &hianime.Source{Master: master, Referer: referer, Qualities: qualities}
	for _, t := range gs.Tracks {
		src.Subtitles = append(src.Subtitles, hianime.Subtitle{Lang: t.Label, URL: t.File, Default: true})
	}
	return src, nil
}

// megaResolve calls the embed's getSources AJAX endpoint and decrypts the enc
// token into the master playlist URL.
func megaResolve(ctx context.Context, embed string, gs *megaSources) (string, error) {
	u, err := url.Parse(embed)
	if err != nil {
		return "", err
	}
	u.Path = "/stream/getSources"
	q := url.Values{}
	if gs.ID != "" {
		q.Set("id", gs.ID)
	}
	q.Set("cid", gs.RealID)
	q.Set("cidu", gs.MediaID)
	u.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", hianime.UserAgent)
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("Referer", embed)
	resp, err := hianime.HTTPClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("getSources HTTP %d", resp.StatusCode)
	}
	var payload struct {
		Enc    string `json:"enc"`
		Tracks []struct {
			File  string `json:"file"`
			Label string `json:"label"`
		} `json:"tracks"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return "", fmt.Errorf("getSources is not json: %w", err)
	}
	if payload.Enc == "" {
		return "", errors.New("getSources had no enc token")
	}
	plain, err := MegaDecrypt(payload.Enc)
	if err != nil {
		return "", err
	}
	var file struct {
		File string `json:"file"`
	}
	if err := json.Unmarshal([]byte(plain), &file); err != nil {
		return "", fmt.Errorf("enc token is not json: %w", err)
	}
	if file.File == "" {
		return "", errors.New("enc token had no playlist")
	}
	gs.Tracks = payload.Tracks
	return file.File, nil
}

// Qualities fetches the master playlist and parses its variants.
func Qualities(ctx context.Context, master, referer string) ([]hianime.Quality, error) {
	playlist, err := hianime.Get(ctx, master, referer)
	if err != nil {
		return nil, err
	}
	return ParseMaster(playlist, master), nil
}

// ParseMaster turns an HLS master playlist into height-sorted variant URLs.
func ParseMaster(playlist, masterURL string) []hianime.Quality {
	base, _ := url.Parse(masterURL)
	base.Path = base.Path[:strings.LastIndex(base.Path, "/")+1]

	var out []hianime.Quality
	lines := strings.Split(playlist, "\n")
	for i := 0; i < len(lines); i++ {
		line := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(line, "#EXT-X-STREAM-INF") || strings.Contains(line, "I-FRAME") {
			continue
		}
		height := 0
		if m := regexp.MustCompile(`RESOLUTION=(\d+)x(\d+)`).FindStringSubmatch(line); m != nil {
			height, _ = strconv.Atoi(m[2])
		}
		if height == 0 || i+1 >= len(lines) {
			continue
		}
		i++
		target := strings.TrimSpace(lines[i])
		if target == "" || strings.HasPrefix(target, "#") {
			continue
		}
		ref, err := url.Parse(target)
		if err != nil {
			continue
		}
		out = append(out, hianime.Quality{Height: height, Label: strconv.Itoa(height) + "p", URL: base.ResolveReference(ref).String()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Height > out[j].Height })
	return out
}

// PickQuality returns the variant URL for want ("best", "worst", "720p"...).
func PickQuality(src *hianime.Source, want string) (string, error) {
	if len(src.Qualities) == 0 {
		return "", errors.New("no playable variants")
	}
	switch want {
	case "", "best":
		return src.Qualities[0].URL, nil
	case "worst":
		return src.Qualities[len(src.Qualities)-1].URL, nil
	}
	height, _ := strconv.Atoi(strings.TrimSuffix(want, "p"))
	for _, q := range src.Qualities {
		if q.Height == height || q.Label == want {
			return q.URL, nil
		}
	}
	return src.Qualities[0].URL, nil
}

// ------------------------------------------------------------- resolve cache

// resolvedKey identifies one episode in one audio mode.
type resolvedKey struct{ id, mode string }

type resolvedEntry struct {
	src     *hianime.Source
	expires time.Time
}

var resolvedCache sync.Map

// ResolveTTL is how long a resolved Source stays fresh. The upstream enc token
// inside the master playlist URL expires after a while; once it does, an old
// entry makes every proxied playlist request fail (player: "Stream gagal
// dimuat: kode 1001") on a long-running app. The player reloads its stream
// every 30 m, so the cache is cycled on the same cadence: after the TTL, the
// next resolve fetches a fresh token.
const ResolveTTL = 30 * time.Minute

// CachedResolve memoises Resolve: one playlist plus every segment the browser
// asks for would otherwise re-walk the servers API and the embed page on each
// request.
func CachedResolve(ctx context.Context, id, mode string) (*hianime.Source, error) {
	key := resolvedKey{id, mode}
	if v, ok := resolvedCache.Load(key); ok {
		if e, ok := v.(resolvedEntry); ok && time.Now().Before(e.expires) {
			return e.src, nil
		}
		// Expired: drop it so the re-resolve below stores a fresh token.
		resolvedCache.Delete(key)
	}
	src, err := Resolve(ctx, id, mode)
	if err != nil {
		return nil, err
	}
	resolvedCache.Store(key, resolvedEntry{src, time.Now().Add(ResolveTTL)})
	return src, nil
}

// NormMode collapses anything that is not a known mode onto sub, so the cache
// key, the resolve and the rewritten playlist URLs all agree on two values.
func NormMode(mode string) string {
	if mode == "dub" {
		return "dub"
	}
	return "sub"
}
