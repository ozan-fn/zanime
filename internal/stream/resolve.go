// Package stream resolves stream embeds and proxies the HLS playlists and
// segments the browser cannot fetch itself (the stream host demands a Referer
// the player cannot send).
//
// The flow is index.js, hop for hop: servers -> pick embed -> embed page ->
// player config -> master playlist. Two players exist behind those embeds and
// both are understood here:
//
//   - ZokoAnime: the embed page ships window.__P, a base64 blob XOR'd with
//     "otaku-embed-v1"; its JSON holds the master playlist and subtitle URLs.
//   - MegaPlay: the embed exposes data-id; /stream/getSources answers an
//     AES-256-CBC token whose plaintext is the master playlist URL.
//
// ZokoAnime is tried first — it is the player upstream serves by default — and
// the megaplay embeds are the fallback (see Resolve).
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

// Embed is one server row of the servers API: its display name, its audio type
// and the decoded embed URL.
type Embed struct {
	Name string `json:"name"`
	Type string `json:"type"`
	URL  string `json:"url"`
}

// The servers API answers an escaped JSON blob whose html field carries rows
// like data-type="dub" data-server-name="HD-2" ... data-hash="<base64 url>".
// The hash is the embed URL, base64'd.
var (
	reServer = regexp.MustCompile(`data-type="([^"]+)"\s*data-server-name="([^"]+)"[\s\S]*?data-hash="([A-Za-z0-9+/=]+)"`)
	// ZokoAnime's player config, and megaplay's player id.
	reWindowP = regexp.MustCompile(`window\.__P="([^"]+)"`)
	reDataID  = regexp.MustCompile(`data-id="(\d+)"`)
)

// Audio types, and why sub is preferred and dub still matters.
//
// Measured on episode 6647 (Sep 2026): the dub embeds of both providers carry a
// single subtitle track of 7 cues — title cards and place names. The dialogue,
// 363 cues, exists only on the sub embed. A dub-first resolve therefore hands
// the translator a file with a handful of lines, which is exactly what the
// player showed. Sub is still not the only option: licensed dubs sometimes ship
// without a sub stream, so dub rows stay in the list as the fallback.
const (
	typeSub = "sub"
	typeDub = "dub"
)

// Servers lists every embed the episode offers, in upstream order, deduplicated
// by URL. Neither audio type nor host is filtered here: Resolve orders them.
func Servers(page string) []Embed {
	// The API answers {"status":true,"html":"…"}; the html is a JSON string, so
	// it is decoded as one. Scrubbing the escapes by hand left the row separators
	// as a literal backslash and "n", which no whitespace class matches.
	var env struct {
		HTML string `json:"html"`
	}
	if json.Unmarshal([]byte(page), &env) == nil && env.HTML != "" {
		page = env.HTML
	}
	var out []Embed
	for _, m := range reServer.FindAllStringSubmatch(page, -1) {
		if m[1] != typeSub && m[1] != typeDub {
			continue
		}
		raw, err := base64.StdEncoding.DecodeString(m[3])
		if err != nil {
			continue
		}
		embed := string(raw)
		if !strings.HasPrefix(embed, "http://") && !strings.HasPrefix(embed, "https://") {
			continue
		}
		if slicesContainsURL(out, embed) {
			continue
		}
		out = append(out, Embed{Name: m[2] + " [" + m[1] + "]", Type: m[1], URL: embed})
	}
	return out
}

func slicesContainsURL(embeds []Embed, raw string) bool {
	for _, e := range embeds {
		if e.URL == raw {
			return true
		}
	}
	return false
}

// IsZoko reports whether an embed is served by the ZokoAnime player, which is
// the one to try first.
func IsZoko(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && strings.Contains(u.Host, "zoko")
}

// embedConfig is the ZokoAnime player config behind window.__P.
type embedConfig struct {
	Src       string `json:"src"`
	Subtitles []struct {
		Lang    string `json:"lang"`
		Src     string `json:"src"`
		Default bool   `json:"default"`
	} `json:"subtitles"`
}

// DeobfuscateBlob decodes the zokoanime embed's base64(json XOR key) blob.
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

// DecryptEnc decrypts the base64url token into the master playlist URL.
func DecryptEnc(enc string) (string, error) {
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

// megaSources is the getSources payload index.js reads: the encrypted token,
// the track list and the plain sources it falls back to.
type megaSources struct {
	Tracks []struct {
		File  string `json:"file"`
		Label string `json:"label"`
	} `json:"tracks"`
	Enc     string          `json:"enc"`
	Sources json.RawMessage `json:"sources"`
	File    string          `json:"file"`
}

// serversURL is the servers endpoint Resolve reads. Injectable so a test can
// exercise the failover without reaching upstream.
var serversURL = hianime.ServersAPI

// Resolve finds a playable stream for an episode: every embed is tried, the sub
// ZokoAnime one first, then the sub megaplay one, then the same pair for dub.
// index.js lets a human pick between providers; here the choice is made for
// them, which is the same order, just without the prompt.
//
// A server is only returned once it looks like the whole episode: its best
// subtitle track has to reach hianime.DenseCues. Playing the first server that
// answers is what showed a 7-cue track for an episode whose dialogue lives on
// another server — same episode, another provider or another audio type. A
// server whose track is thin is kept aside and used only when no other server
// is better: its video is fine, so a missing translation must not lose the
// episode.
func Resolve(ctx context.Context, episodeID string) (*hianime.Source, error) {
	page, err := hianime.GetTiny(ctx, fmt.Sprintf(serversURL, url.QueryEscape(episodeID)), hianime.BaseAPI+"/")
	if err != nil {
		return nil, err
	}
	embeds := Servers(page)
	if len(embeds) == 0 {
		return nil, fmt.Errorf("no stream source for episode %s", episodeID)
	}
	// Failover: a dead embed (error page, getSources failure, dead master)
	// falls over to the next server instead of failing the episode.
	var fallback *hianime.Source
	var lastErr error
	for _, embed := range ordered(embeds) {
		src, err := ResolveOne(ctx, embed.URL)
		if err != nil {
			lastErr = err
			continue
		}
		if src.DialogueCues >= hianime.DenseCues {
			return src, nil
		}
		if fallback == nil {
			fallback = src
		}
	}
	if fallback != nil {
		return fallback, nil
	}
	return nil, lastErr
}

// ordered returns the embeds in the order Resolve should try them: sub before
// dub, ZokoAnime before megaplay, upstream order inside each group. Kept stable
// so the same episode always resolves the same way.
func ordered(embeds []Embed) []Embed {
	out := append([]Embed(nil), embeds...)
	sort.SliceStable(out, func(i, j int) bool { return rank(out[i]) < rank(out[j]) })
	return out
}

// rank scores one embed: 0 sub+zoko, 1 sub+megaplay, 2 dub+zoko, 3 dub+megaplay.
func rank(e Embed) int {
	n := 0
	if e.Type != typeSub {
		n += 2
	}
	if !IsZoko(e.URL) {
		n++
	}
	return n
}

// ResolveOne turns one embed URL into a Source by reading its player config.
func ResolveOne(ctx context.Context, embed string) (*hianime.Source, error) {
	// The stream host wants the embed site as referer.
	referer, err := hianime.OriginOf(embed)
	if err != nil {
		return nil, err
	}
	embedPage, err := hianime.GetTiny(ctx, embed, hianime.BaseAPI+"/")
	if err != nil {
		return nil, err
	}
	// ZokoAnime: window.__P = xor(base64(json)). The JSON carries the playlist
	// and the subtitle tracks, so no second request is needed for either.
	if m := reWindowP.FindStringSubmatch(embedPage); m != nil {
		plain, err := DeobfuscateBlob(m[1])
		if err != nil {
			return nil, err
		}
		var cfg embedConfig
		if err := json.Unmarshal([]byte(plain), &cfg); err != nil {
			return nil, fmt.Errorf("zoko player config is not json: %w", err)
		}
		if cfg.Src == "" {
			return nil, errors.New("zoko player config had no src")
		}
		src := &hianime.Source{Master: cfg.Src, Referer: referer}
		for _, sub := range cfg.Subtitles {
			if sub.Src == "" {
				continue
			}
			src.Subtitles = append(src.Subtitles, hianime.Subtitle{Lang: sub.Lang, URL: sub.Src})
		}
		pickDialogue(ctx, src, referer)
		return finish(ctx, src, referer)
	}

	// MegaPlay: data-id -> getSources -> enc (AES-CBC) -> master playlist.
	m := reDataID.FindStringSubmatch(embedPage)
	if m == nil {
		return nil, fmt.Errorf("embed page is not a known player (no __P / data-id) for %s", embed)
	}
	payload, err := getSources(ctx, embed, m[1])
	if err != nil {
		return nil, err
	}
	master, err := masterFrom(payload)
	if err != nil {
		return nil, err
	}
	src := &hianime.Source{Master: master, Referer: referer}
	for _, t := range payload.Tracks {
		if t.File == "" {
			continue
		}
		src.Subtitles = append(src.Subtitles, hianime.Subtitle{Lang: t.Label, URL: t.File})
	}
	pickDialogue(ctx, src, referer)
	return finish(ctx, src, referer)
}

// pickDialogue marks the default subtitle track, in two steps.
//
// Language first: among the tracks in the best language available (Indonesian,
// else English — hianime.LangRank), the densest one wins. Language cannot be
// skipped in favour of "most cues": every dialogue track is dense, so a
// Chinese-first list (megaplay ships Simplified, Traditional, English,
// Indonesian, Japanese...) would hand the translator Han text, and Han text is
// what ends up in the .vtt.
//
// Density is the tie-breaker, and the fallback when no preferred language exists:
// both players list a sparse "signs & songs" track — the one index.js would
// take, since it reads subtitles[0] or the first captions track and only prints
// the URL for a human. Conversion cannot work from that track (title cards and
// place names: 8 cues against 553 for the dialogue), so the head of each
// candidate decides: 64 KB is enough to tell dense from sparse, costs one small
// request, and is cached with the rest of the resolve for 30 minutes. A track
// that cannot be fetched counts as empty.
//
// The winner's cue count is recorded on the source: Resolve uses it to tell a
// server that carries the episode from one that only carries its signs.
func pickDialogue(ctx context.Context, src *hianime.Source, referer string) {
	if len(src.Subtitles) == 0 {
		return
	}
	const candidates = 3
	rank := hianime.LangRank(src.Subtitles[0].Lang)
	for _, s := range src.Subtitles {
		if r := hianime.LangRank(s.Lang); r < rank {
			rank = r
		}
	}
	best, bestCues := 0, -1
	probed := 0
	for i := range src.Subtitles {
		src.Subtitles[i].Default = false
		if probed >= candidates {
			continue
		}
		// With a preferred language present, only its tracks are compared; the
		// others are not even fetched.
		if hianime.LangRank(src.Subtitles[i].Lang) > rank {
			continue
		}
		probed++
		if n := probeCues(ctx, src.Subtitles[i].URL, referer); n > bestCues {
			best, bestCues = i, n
		}
	}
	src.Subtitles[best].Default = true
	src.DialogueCues = bestCues
}

// probeCues counts cue timings in the head of a subtitle track.
func probeCues(ctx context.Context, rawURL, referer string) int {
	head, err := hianime.GetPart(ctx, rawURL, referer, 64<<10, cueProbeTimeout)
	if err != nil {
		return 0
	}
	return strings.Count(head, "-->")
}

// cueProbeTimeout bounds one track peek. Generous next to the other small hops:
// 64 KB is nothing, but the subtitle CDNs answer the first byte slowly.
const cueProbeTimeout = 8 * time.Second

// finish parses the master playlist into variants, which is what the proxy and
// the quality menu index by.
func finish(ctx context.Context, src *hianime.Source, referer string) (*hianime.Source, error) {
	qualities, err := Qualities(ctx, src.Master, referer)
	if err != nil {
		return nil, err
	}
	src.Qualities = qualities
	return src, nil
}

// getSources calls the embed's AJAX endpoint. Only id is sent: upstream started
// answering 403 to the cid/cidu pair it used to require (verified Sep 2026 —
// with them it 403s, with id alone it answers the token).
func getSources(ctx context.Context, embed, id string) (*megaSources, error) {
	u, err := url.Parse(embed)
	if err != nil {
		return nil, err
	}
	u.Path = "/stream/getSources"
	u.RawQuery = url.Values{"id": {id}}.Encode()
	ctx, cancel := context.WithTimeout(ctx, hianime.TinyTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", hianime.UserAgent)
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("Referer", embed)
	resp, err := hianime.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, hianime.MaxTinyBytes))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("getSources HTTP %d", resp.StatusCode)
	}
	var payload megaSources
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, fmt.Errorf("getSources is not json: %w", err)
	}
	return &payload, nil
}

// masterFrom picks the playlist URL out of a getSources payload: the encrypted
// token first, then the plain sources index.js falls back to.
func masterFrom(payload *megaSources) (string, error) {
	if payload.Enc != "" {
		plain, err := DecryptEnc(payload.Enc)
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
		return file.File, nil
	}
	// sources is either a plain string or [{file: ...}].
	var s string
	if json.Unmarshal(payload.Sources, &s) == nil && s != "" {
		return s, nil
	}
	var list []struct {
		File string `json:"file"`
	}
	if json.Unmarshal(payload.Sources, &list) == nil && len(list) > 0 && list[0].File != "" {
		return list[0].File, nil
	}
	if payload.File != "" {
		return payload.File, nil
	}
	return "", errors.New("getSources had no playlist")
}

// Qualities fetches the master playlist and parses its variants. A URL that
// turns out to be a media playlist (segments, no variants) is still playable —
// index.js reads it the same way — so it becomes the single quality instead of
// a source with nothing to play.
func Qualities(ctx context.Context, master, referer string) ([]hianime.Quality, error) {
	playlist, err := hianime.GetTiny(ctx, master, referer)
	if err != nil {
		return nil, err
	}
	if qs := ParseMaster(playlist, master); len(qs) > 0 {
		return qs, nil
	}
	if strings.Contains(playlist, "#EXTINF") {
		return []hianime.Quality{{Label: "auto", URL: master}}, nil
	}
	return nil, errors.New("playlist has no playable variants")
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
		if m := reResolution.FindStringSubmatch(line); m != nil {
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

var reResolution = regexp.MustCompile(`RESOLUTION=(\d+)x(\d+)`)

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

type resolvedEntry struct {
	src     *hianime.Source
	expires time.Time
}

var resolvedCache sync.Map // episode id -> resolvedEntry

// Invalidate drops one episode's resolve cache entry. Called when the upstream
// rejects a cached master/variant URL: the token inside it expired, and the
// next resolve must fetch a fresh one instead of waiting for the TTL.
func Invalidate(id string) {
	resolvedCache.Delete(id)
}

// ResolveTTL is how long a resolved Source stays fresh. Both players hand out
// URLs with a short-lived token inside them; once it expires, an old entry makes
// every proxied playlist request fail (player: "Stream gagal dimuat: kode 1001")
// on a long-running app. The player reloads its stream every 30 m, so the cache
// is cycled on the same cadence.
const ResolveTTL = 30 * time.Minute

// CachedResolve memoises Resolve: one playlist plus every segment the browser
// asks for would otherwise re-walk the servers API and the embed page on each
// request.
func CachedResolve(ctx context.Context, id string) (*hianime.Source, error) {
	if v, ok := resolvedCache.Load(id); ok {
		if e, ok := v.(resolvedEntry); ok && time.Now().Before(e.expires) {
			return e.src, nil
		}
		// Expired: drop it so the re-resolve below stores a fresh token.
		resolvedCache.Delete(id)
	}
	src, err := Resolve(ctx, id)
	if err != nil {
		return nil, err
	}
	resolvedCache.Store(id, resolvedEntry{src, time.Now().Add(ResolveTTL)})
	return src, nil
}
