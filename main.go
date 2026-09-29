// Command main serves a small HTTP API around hianime.at: search, episode list,
// source resolution and Indonesian subtitles (via kenari).
package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"embed"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"html"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

const (
	baseAPI   = "https://hianime.at"
	userAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0.0.0 Safari/537.36"

	// The embed page ships its config as base64(json XOR "otaku-embed-v1").
	embedXORKey = "otaku-embed-v1"

	llmURL   = "https://kenari.id/v1/chat/completions"
	llmKey   = "REDACTED"
	llmModel = "deepseek-v4-1-flash"
)

// limitRequestsPerMinute is our own pacing, not a kenari number. The 5/min it
// used to hold was the free route's window, advertised in x-ratelimit-limit;
// this account on a paid model answers 60 concurrent calls without complaint and
// sends no such header. Pacing still buys something, because a burst is what a
// 429 punishes, so a generous ceiling stays — far under what the account takes.
const limitRequestsPerMinute = 30

// contextTokens is the ceiling a request must stay inside. deepseek-v4-1-flash
// models 1M, so this is under a tenth of what it holds: a bound that has to be
// reasoned about rather than one that is ever reached.
const contextTokens = 120_000

// maxReplyTokens is the reply allowance sent as max_completion_tokens. It has to
// cover the translation (about as long as its cues) plus room for a model that
// thinks before answering. An answer that overruns it comes back
// finish_reason=length and the batch is split.
const maxReplyTokens = 12_000

// batchTokens is the slice of the window one call spends on its cues: half the
// reply allowance, because a translation runs about as long as its source. It is
// sized so that a whole episode is one call. That is about more than speed: the
// same model asked twice will word a recurrent line two ways unless it can see
// how it worded the last one, so every call an episode does not need is one more
// chance for the file to read as two translations. A real episode is 285-394
// cues, about 3-4k source tokens, so this covers one comfortably. Prompt plus
// reply stay far inside contextTokens throughout.
const batchTokens = maxReplyTokens / 2

// tokensFor estimates a text's token count from its byte length. It only has to
// be conservative: an overestimate costs a slightly smaller batch, an
// underestimate costs a rejected or truncated call.
func tokensFor(s string) int { return (len(s) + 3) / 4 }

var (
	searchAPI   = baseAPI + "/search?keyword=%s"
	episodesAPI = baseAPI + "/api/theme/episode/list/%s"
	serversAPI  = baseAPI + "/api/theme/episode/servers?episodeId=%s"

	errCloudflare = errors.New("blocked by cloudflare: install curl-impersonate or retry from another egress IP")
)

//go:embed all:web/dist
var distFS embed.FS

// webRoot is the built SPA served at /: index.html plus hashed assets.
var webRoot, _ = fs.Sub(distFS, "web/dist")

// MaxIdleConnsPerHost defaults to 2, which queues the player's parallel
// segment fetches behind each other; the proxy is only as smooth as this pool.
var httpTransport = &http.Transport{
	MaxIdleConns:          64,
	MaxIdleConnsPerHost:   16,
	IdleConnTimeout:       90 * time.Second,
	ResponseHeaderTimeout: 20 * time.Second,
}

// httpClient is for scraping: one bounded response. streamClient pipes media,
// where the body may legitimately take longer than any scrape timeout.
var (
	httpClient   = &http.Client{Timeout: 20 * time.Second, Transport: httpTransport}
	streamClient = &http.Client{Transport: httpTransport}
)

// ---------------------------------------------------------------- scraping

// get fetches rawURL with browser-ish headers. referer may be empty.
func get(ctx context.Context, rawURL, referer string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	resp, err := httpClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("upstream HTTP %d for %s", resp.StatusCode, rawURL)
	}
	page := string(body)
	if strings.Contains(strings.ToLower(page), "just a moment") ||
		strings.Contains(page, "cf-browser-verification") {
		return "", errCloudflare
	}
	return page, nil
}

// fetchUpstream issues the proxied GET with the headers the stream host wants
// and fails before any byte reaches the client when upstream is not 2xx.
func fetchUpstream(ctx context.Context, rawURL, referer string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "*/*")
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	resp, err := streamClient.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		resp.Body.Close()
		return nil, fmt.Errorf("upstream HTTP %d", resp.StatusCode)
	}
	return resp, nil
}

// streamUpstream copies an upstream response into the client instead of
// buffering it first. Every proxied byte used to sit behind an io.ReadAll, so
// the browser waited for a whole segment before it saw byte one — with a
// double hop that is exactly the buffering that makes playback stutter.
func streamUpstream(w http.ResponseWriter, r *http.Request, rawURL, referer, contentType, cachePolicy string) {
	resp, err := fetchUpstream(r.Context(), rawURL, referer)
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

// streamMaster proxies the upstream master playlist with every variant URI
// repointed at this proxy. Only the master needs rewriting: the variant
// playlists keep their relative segment names, which then resolve back into
// /api/hls/{id}/{mode}/{quality}/ — the identity the segment handler reads.
// A relative URI resolved against a query-string base drops that query
// (RFC 3986), which is why segments used to come from the default variant
// whatever quality was picked.
func streamMaster(w http.ResponseWriter, r *http.Request, id string, src *Source, mode string) {
	resp, err := fetchUpstream(r.Context(), src.Master, src.Referer)
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
			// parseMaster rejected this variant (no usable RESOLUTION); keeping
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

// streamVariant proxies a media playlist, repointing every segment URI —
// relative or absolute, whatever host — at this proxy. The upstream CDN hosts
// segments on separate hosts under camouflaged names (seg-1....jpg), so the
// segment is passed as its base64url'd absolute URL.
func streamVariant(w http.ResponseWriter, r *http.Request, id, mode, quality, variant, referer string) {
	resp, err := fetchUpstream(r.Context(), variant, referer)
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

// megaSegmentType sniffs a segment's real content type from its bytes: the
// upstream camouflages TS segments as image/document filenames. Magic bytes:
// MPEG-TS sync 0x47 at 0 and 188-byte stride, MP4 `ftyp` box at offset 4.
func megaSegmentType(head []byte) string {
	if len(head) >= 1 && head[0] == 0x47 {
		return "video/mp2t"
	}
	if len(head) >= 12 && string(head[4:8]) == "ftyp" {
		return "video/mp4"
	}
	return "application/octet-stream"
}

// probeSegment peeks the first bytes of the upstream segment to sniff its
// content type, closing the probe body before the caller re-fetches.
func probeSegment(ctx context.Context, rawURL, referer string) (string, bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", false
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Referer", referer)
	resp, err := streamClient.Do(req)
	if err != nil {
		return "", false
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return "", false
	}
	head := make([]byte, 512)
	n, _ := io.ReadFull(resp.Body, head)
	return megaSegmentType(head[:n]), true
}

func deobfuscateBlob(blob string) (string, error) {
	trimmed := strings.TrimSpace(blob)
	raw, err := base64.StdEncoding.DecodeString(trimmed)
	if err != nil {
		if raw, err = base64.RawStdEncoding.DecodeString(trimmed); err != nil {
			return "", fmt.Errorf("blob is not base64: %w", err)
		}
	}
	key := []byte(embedXORKey)
	out := make([]byte, len(raw))
	for i, b := range raw {
		out[i] = b ^ key[i%len(key)]
	}
	return string(out), nil
}

var (
	// The top 10 sidebar repeats the result markup, cut it off before matching.
	reSidebar   = regexp.MustCompile(`(?s)id="main-sidebar".*`)
	reFilmBlock = regexp.MustCompile(`<div class="film-detail">`)
	reFilmName  = regexp.MustCompile(`(?s)<h3 class="film-name">\s*<a href="[^"]*/([^"/]*)"\s+title="([^"]*)"`)
	// Search cards ship the poster as a plain src (no lazyload) and a short
	// synopsis right under the title.
	rePoster   = regexp.MustCompile(`<img src="([^"]+)"\s+class="film-poster-img"`)
	reFilmDesc = regexp.MustCompile(`(?s)<div class="description">\s*(.*?)\s*</div>`)
	reTag      = regexp.MustCompile(`<[^>]*>`)
	// Each result card is one flw-item; poster sits before film-detail inside it.
	reFlwItem = regexp.MustCompile(`(?s)<div class="flw-item "`)
	// Related / recommended sections on the anime page: id+title link pairs.
	reCardLink = regexp.MustCompile(`<a href="https?://[^/]+/([a-z0-9-]+)"\s+title="([^"]*)"`)
	reHeading  = regexp.MustCompile(`cat-heading">([^<]+)<`)
)

// stripTags flattens an HTML fragment into one line of text.
func stripTags(fragment []string) string {
	if len(fragment) < 2 {
		return ""
	}
	flat := reTag.ReplaceAllString(fragment[1], " ")
	return html.UnescapeString(strings.Join(strings.Fields(flat), " "))
}

// proxiedImg rewrites an upstream image URL into a local /img reference so the
// browser never sees the real CDN host. URLs that fail the allow-list pass
// through unchanged; the proxy would refuse them anyway.
func proxiedImg(raw string) string {
	if !reImgHost.MatchString(raw) {
		return raw
	}
	return "/api/img?u=" + base64.RawURLEncoding.EncodeToString([]byte(raw))
}

// animeDetails scrapes the anime page for everything the poster and the
// one-line synopsis cannot carry: japanese title, airing window, duration,
// status, MAL score, studios and producers.
func animeDetails(ctx context.Context, slug string) (*Anime, error) {
	page, err := get(ctx, baseAPI+"/"+slug, baseAPI+"/")
	if err != nil {
		return nil, err
	}
	a := &Anime{ID: slug}
	// h1's data-jname is the real title; the <title> tag says "Watch X | HiAnime".
	if m := reH1Name.FindStringSubmatch(page); m != nil {
		a.Name = html.UnescapeString(strings.TrimSpace(m[1]))
	}
	if m := rePagePoster.FindStringSubmatch(page); m != nil {
		a.Poster = proxiedImg(m[1])
	}
	// Info rows are <span class="item-head">Label:</span> followed by either
	// plain text, a text div, or a run of <a> values.
	for _, m := range reInfoItem.FindAllStringSubmatch(page, -1) {
		value := infoValue(m[2])
		switch strings.TrimSuffix(m[1], ":") {
		case "Japanese":
			a.Japanese = value
		case "Aired":
			a.Aired = value
		case "Premiered":
			a.Premiered = value
		case "Duration":
			a.Duration = value
		case "Status":
			a.Status = value
		case "MAL Score":
			a.Score = value
		case "Studios":
			a.Studios = value
		case "Producers":
			a.Producers = value
		case "Genres":
			for _, g := range reGenreLink.FindAllStringSubmatch(m[2], -1) {
				a.Genres = append(a.Genres, strings.TrimSpace(g[1]))
			}
		}
	}
	if m := reOverview.FindStringSubmatch(page); m != nil {
		a.Synopsis = stripTags(m)
	}
	// Badges next to the poster: sub/dub counts and total episodes.
	if m := reTickCount.FindStringSubmatch(page); m != nil {
		a.SubCount, a.DubCount, a.EpisodeCount = m[1], m[2], m[3]
	}
	a.Related, a.Recommended = sectionCards(page, "Related Anime"), sectionCards(page, "Recommended For You")
	return a, nil
}

var (
	// The anime page's poster is an <img src>, no lazyload class.
	rePagePoster = regexp.MustCompile(`<img src="([^"]+)"\s+class="film-poster-img"`)
	reH1Name     = regexp.MustCompile(`(?s)<h2 class="film-name dynamic-name" data-jname="([^"]*)"`)
	reInfoItem   = regexp.MustCompile(`(?s)<span class="item-head">([^<]+)</span>(.*?)</div>`)
	reGenreLink  = regexp.MustCompile(`/genres/[a-z-]+"[^>]*>\s*([^<]+?)\s*<`)
	reOverview   = regexp.MustCompile(`(?s)Overview:</span>\s*<div class="text">\s*(.*?)\s*</div>`)
	reTickCount  = regexp.MustCompile(`(?s)tick-item tick-sub">.*?([0-9,]+)</div>.*?tick-item tick-dub">.*?([0-9,]+)</div>.*?tick-item tick-eps">([0-9,]+)</div>`)
)

// infoValue cleans one info row: links keep their text, tags vanish, runs of
// whitespace collapse.
func infoValue(raw string) string {
	flat := reTag.ReplaceAllString(raw, " ")
	return strings.TrimSpace(strings.Join(strings.Fields(flat), " "))
}

// searchResult carries what one card on the search page shows: id, title,
// poster, and the short synopsis. The per-anime details live on the anime's
// own page and are fetched separately by animeDetails.
func search(ctx context.Context, query string) ([]Anime, error) {
	page, err := get(ctx, fmt.Sprintf(searchAPI, url.QueryEscape(query)), baseAPI+"/")
	if err != nil {
		return nil, err
	}
	page = reSidebar.ReplaceAllString(page, "")
	out := []Anime{}
	// Cards are flw-item blocks: the poster image precedes film-detail inside
	// the same block, which reFilmBlock alone cut in half.
	for _, block := range reFlwItem.Split(page, -1) {
		m := reFilmName.FindStringSubmatch(block)
		if m == nil {
			continue
		}
		a := Anime{ID: m[1], Name: html.UnescapeString(m[2])}
		if pm := rePoster.FindStringSubmatch(block); pm != nil {
			a.Poster = proxiedImg(pm[1])
		}
		a.Synopsis = stripTags(reFilmDesc.FindStringSubmatch(block))
		out = append(out, a)
	}
	return out, nil
}

var reEpItem = regexp.MustCompile(`ep-item`)

func episodes(ctx context.Context, slug string) ([]Episode, error) {
	// The list route is keyed by the trailing id, not the slug.
	id := slug[strings.LastIndex(slug, "-")+1:]
	page, err := get(ctx, fmt.Sprintf(episodesAPI, id), baseAPI+"/")
	if err != nil {
		return nil, err
	}
	// The API answers with an escaped JSON blob; drop the backslashes first.
	page = strings.ReplaceAll(page, `\`, "")
	// The id is only unique within a provider, so keep this slug's episodes only.
	reEpLink := regexp.MustCompile(`data-number="([^"]*)".*?data-id="([0-9]+)".*?/watch/` + regexp.QuoteMeta(slug) + `\?ep=`)
	out := []Episode{}
	for _, block := range reEpItem.Split(page, -1) {
		m := reEpLink.FindStringSubmatch(block)
		if m == nil {
			continue
		}
		out = append(out, Episode{ID: m[2], Number: m[1]})
	}
	sort.Slice(out, func(i, j int) bool { return epNumber(out[i].Number) < epNumber(out[j].Number) })
	return out, nil
}

func epNumber(s string) float64 {
	f, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return f
}

var reDataHash = regexp.MustCompile(`data-hash="([^"]*)"`)

// serverHash picks the first server of the wanted audio mode. Sub and dub are
// separate embeds; upstream names its servers HD-2, Vidstream-2 and the like
// (the old ZokoAnime server is gone), so the name is not filtered — the mode
// is what separates the streams.
func serverHash(page, mode string) string {
	for _, item := range strings.Split(page, "server-item")[1:] {
		if !strings.Contains(item, `data-type="`+mode+`"`) {
			continue
		}
		if m := reDataHash.FindStringSubmatch(item); m != nil {
			return m[1]
		}
	}
	return ""
}

type embedConfig struct {
	Src       string `json:"src"`
	Subtitles []struct {
		Lang    string `json:"lang"`
		Src     string `json:"src"`
		Default bool   `json:"default"`
	} `json:"subtitles"`
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
	ID     string `json:"id"`
	RealID string `json:"realid"`
	MediaID string `json:"mediaid"`
	Tracks []struct {
		File  string `json:"file"`
		Label string `json:"label"`
	} `json:"tracks"`
	Enc string `json:"enc"`
}

// megaDecrypt decrypts the base64url token into the master playlist URL.
func megaDecrypt(enc string) (string, error) {
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

// resolve finds the stream server of the wanted audio mode and returns its
// master playlist, subtitle list and the referer the stream host demands.
func resolve(ctx context.Context, episodeID, mode string) (*Source, error) {
	if mode != "sub" && mode != "dub" {
		mode = "sub"
	}
	page, err := get(ctx, fmt.Sprintf(serversAPI, url.QueryEscape(episodeID)), baseAPI+"/")
	if err != nil {
		return nil, err
	}
	page = strings.ReplaceAll(page, `\"`, `"`)
	hash := serverHash(page, mode)
	if hash == "" {
		return nil, fmt.Errorf("no stream source for episode %s (%s)", episodeID, mode)
	}
	embedURL, err := base64.StdEncoding.DecodeString(hash)
	if err != nil {
		return nil, fmt.Errorf("embed hash: %w", err)
	}
	embed := string(embedURL)
	// The stream host wants the embed site as referer.
	referer, err := originOf(embed)
	if err != nil {
		return nil, err
	}
	embedPage, err := get(ctx, embed, baseAPI+"/")
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
		return nil, errors.New("embed page is not a megaplay player")
	}
	master, err := megaResolve(ctx, embed, &gs)
	if err != nil {
		return nil, err
	}
	qualities, err := qualities(ctx, master, referer)
	if err != nil {
		return nil, err
	}
	src := &Source{Master: master, Referer: referer, Qualities: qualities}
	for _, t := range gs.Tracks {
		src.Subtitles = append(src.Subtitles, Subtitle{Lang: t.Label, URL: t.File, Default: true})
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
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	req.Header.Set("Referer", embed)
	resp, err := httpClient.Do(req)
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
	plain, err := megaDecrypt(payload.Enc)
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

func originOf(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("bad embed url %q", raw)
	}
	return u.Scheme + "://" + u.Host + "/", nil
}

func qualities(ctx context.Context, master, referer string) ([]Quality, error) {
	playlist, err := get(ctx, master, referer)
	if err != nil {
		return nil, err
	}
	return parseMaster(playlist, master), nil
}

// parseMaster turns an HLS master playlist into height-sorted variant URLs.
func parseMaster(playlist, masterURL string) []Quality {
	base, _ := url.Parse(masterURL)
	base.Path = base.Path[:strings.LastIndex(base.Path, "/")+1]

	var out []Quality
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
		out = append(out, Quality{Height: height, Label: strconv.Itoa(height) + "p", URL: base.ResolveReference(ref).String()})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Height > out[j].Height })
	return out
}

func pickQuality(src *Source, want string) (string, error) {
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

// ---------------------------------------------------------------- subtitles

// parseVTT splits a WebVTT file into the header block and the cue blocks,
// keeping raw lines so timing and cue ids survive a round trip.
func parseVTT(vtt string) (header string, blocks [][]string) {
	for _, raw := range strings.Split(strings.ReplaceAll(vtt, "\r\n", "\n"), "\n\n") {
		block := strings.Split(strings.TrimRight(raw, "\n"), "\n")
		if len(block) == 1 && block[0] == "" {
			continue
		}
		if header == "" && strings.HasPrefix(block[0], "WEBVTT") {
			header = strings.Join(block, "\n")
			continue
		}
		blocks = append(blocks, block)
	}
	return header, blocks
}

func rebuildVTT(header string, blocks [][]string) string {
	var b strings.Builder
	if header != "" {
		b.WriteString(header)
		b.WriteString("\n\n")
	}
	for _, blk := range blocks {
		b.WriteString(strings.Join(blk, "\n"))
		b.WriteString("\n\n")
	}
	return b.String()
}

// timingIndex locates the "-->" line of a cue block, which is not always at a
// fixed offset: a block may carry a cue id line before it.
func timingIndex(block []string) int {
	for i, line := range block {
		if strings.Contains(line, "-->") {
			return i
		}
	}
	return -1
}

// cueText joins everything after the timing line of a cue block.
func cueText(block []string) string {
	i := timingIndex(block)
	if i < 0 {
		return ""
	}
	return strings.TrimSpace(strings.Join(block[i+1:], " "))
}

func setCueText(block []string, text string) []string {
	i := timingIndex(block)
	if i < 0 {
		return block
	}
	out := make([]string, 0, i+2)
	out = append(out, block[:i+1]...)
	return append(out, text)
}

// translateSystem carries the consistency rules. An episode is normally one
// call, but a long file, or a second pass repairing lines the first one dropped,
// still means a second call — and a model asked twice will word a recurring line
// two ways unless it is told not to. These rules are what keeps the finished file
// reading as one translation.
const translateSystem = `Translate English anime subtitle lines into natural spoken Indonesian — how a professional subtitler writes, never literal.

Reply (absolute):
- One reply line per input line: <number>|<translation>. No notes, no blank lines, no preamble.
- Never merge, split, reorder, drop or add lines. A line you cannot translate still gets its number.

Style:
- Meaning, emotion and intent over words; restructure long English into concise Indonesian without losing anything.
- Register follows the character: aku/kau when close or in conflict, saya/Anda only when the character speaks formally.
- Keep crude speech as crude as the source; carry humor, puns and idioms by their effect, not word-for-word.
- Everyday concepts take their established Indonesian word, never the English loanword — however common that English word is in casual Indonesian speech. Keep an English word only when Indonesian truly has no word for the thing.
- Same word every time a name, term or catchphrase returns.

Japanese content — keep in romaji: names, places, techniques, organizations; honorifics (-san, -kun, -chan, -sama, -senpai, -sensei) while they carry relationship meaning; a Japanese word the source keeps (itadakimasu, oyasumi).

Format:
- Preserve tags ({\an8}, <i>, <b>, \N) exactly, both dashes of a two-speaker line, and the source's punctuation, ellipses and capitalisation.
- Sound cues translate only when the source has them ([gasps] -> [terengah]); add nothing new.
- Song lyrics: translate the meaning, keep the line structure.

Examples:
12|You idiot! What were you thinking?! -> 12|Dasar bodoh! Apa yang kau pikirkan?!
13|I never said I hated you. -> 13|Aku tak pernah bilang membencimu.
14|Senpai, are you all right? -> 14|Senpai, kau tidak apa-apa?`

// apiError is kenari's error envelope. type and code carry the same code, so
// reading code is enough to tell a rate limit from a request that is simply
// wrong — kenari also answers 429 for upstream rejections.
type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	retry   time.Duration
}

func (e apiError) Error() string { return fmt.Sprintf("kenari %s: %s", e.Code, e.Message) }

func (e apiError) Is(target error) bool { return target == errRetryable && retryableCodes[e.Code] }

// parseRetryAfter reads the seconds form kenari sends. Anything else (an HTTP
// date, an empty header) becomes 0, which callers read as "no hint".
func parseRetryAfter(v string) time.Duration {
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n <= 0 {
		return 0
	}
	return time.Duration(n) * time.Second
}

// translateBatch asks the model to translate "<index>|<text>" lines and returns
// the translations keyed by index.
func translateBatch(ctx context.Context, lines []string) (map[int]string, error) {
	// Charge the limiter for every real call. translateSplit fans one job out
	// into many calls, so charging once per HTTP request undercounted the quota
	// by an order of magnitude and let a burst walk straight into a 429.
	if err := waitForBudget(ctx); err != nil {
		return nil, err
	}
	// The wait above may be long on purpose, so the deadline starts here. The
	// client cannot carry this one: its Timeout covers reading the streamed
	// body, and an answer legitimately runs past any scrape timeout — that is
	// what produced "context deadline exceeded (Client.Timeout ... while reading
	// body)" on the first batch of an episode.
	ctx, cancel := context.WithTimeout(ctx, translateCallTimeout)
	defer cancel()
	payload := map[string]any{
		"model": llmModel,
		// Low, not zero: sampling is what makes the same line come back worded
		// differently each time it appears, and a subtitle file that renders
		// "Ugh, my head..." two ways reads as machine output.
		"temperature":           0.2,
		"max_completion_tokens": maxReplyTokens,
		"top_p":                 0.95,
		"stream":                true,
		"messages": []map[string]string{
			{"role": "system", "content": translateSystem},
			{"role": "user", "content": strings.Join(lines, "\n")},
		},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, llmURL, strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+llmKey)
	resp, err := streamClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		var env struct {
			Error apiError `json:"error"`
		}
		if json.Unmarshal(b, &env) == nil && env.Error.Code != "" {
			env.Error.retry = parseRetryAfter(resp.Header.Get("Retry-After"))
			return nil, env.Error
		}
		return nil, fmt.Errorf("kenari HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	var answer strings.Builder
	finish := ""
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			continue
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Content string `json:"content"`
				} `json:"delta"`
				FinishReason string `json:"finish_reason"`
			} `json:"choices"`
			Error *apiError `json:"error"`
		}
		if json.Unmarshal([]byte(payload), &chunk) != nil {
			continue
		}
		// A mid-stream failure rides in a 200 chunk, not in the status code.
		if chunk.Error != nil {
			return nil, *chunk.Error
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		if r := chunk.Choices[0].FinishReason; r != "" {
			finish = r
		}
		// A model that thinks before answering puts that in a sibling field;
		// only delta.content is the translation, so reasoning is ignored.
		answer.WriteString(chunk.Choices[0].Delta.Content)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}

	out := map[int]string{}
	for _, line := range strings.Split(answer.String(), "\n") {
		line = strings.TrimSpace(line)
		bar := strings.Index(line, "|")
		if bar < 1 {
			continue
		}
		idx, err := strconv.Atoi(strings.TrimSpace(line[:bar]))
		if err != nil {
			continue
		}
		if text := strings.TrimSpace(line[bar+1:]); text != "" {
			out[idx] = text
		}
	}
	// finish_reason=length with lines still unanswered means the answer was cut
	// off, which is what a reasoning model does when it spends the whole reply
	// budget thinking: it returns finish_reason=length and no lines at all. The
	// batch is simply more than one reply can carry, so it gets split.
	if finish == "length" && len(out) < len(lines) {
		return nil, errTruncated
	}
	if len(out) == 0 {
		return nil, errors.New("model returned no parsable translations")
	}
	return out, nil
}

// errRetryable marks a failure a retry can clear, so the batch is split and
// retried instead of failing outright.
var errRetryable = errors.New("kenari retryable failure")

// errTruncated marks an answer the model cut off mid-list. Nothing is
// throttling us, so the fix is a smaller batch rather than a wait.
var errTruncated = errors.New("kenari answer truncated")

// translateCallTimeout bounds one streaming call. It is generous because the
// batch is deliberately large and a reasoning model can think for a while.
const translateCallTimeout = 5 * time.Minute

// retryableCodes are the failures a retry can clear: kenari's rate limits, and
// the 503 it answers when every backend for the route failed at once. Any other
// code is an upstream rejection of the request itself, where extra patience
// only delays the report.
var retryableCodes = map[string]bool{
	"rate_limit_exceeded":  true,
	"free_quota_rpm":       true,
	"free_quota_daily":     true,
	"plan_limit_reached":   true,
	"upstream_error":       true,
	"all_providers_failed": true,
}

// batchFn is indirect so tests can exercise the batching without kenari.
var batchFn = translateBatch

// waitForBudget blocks until the request window allows one more call.
// Conversion runs in a background job, so waiting only costs time — which is
// exactly the point: pacing beats a 429 storm.
func waitForBudget(ctx context.Context) error {
	for {
		wait, ok := llmLimit.take()
		if ok {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
}

// rateLimitPause is the fallback wait when a 429 arrives without a usable
// Retry-After.
const rateLimitPause = 15 * time.Second

// translateSplit runs translateBatch and, on a retryable failure, halves the
// batch. Whatever the limit was, it counts calls rather than lines, so a batch
// that loses the race is better off split than retried at the same size — and a
// truncated answer can only be fixed by asking for less.
func translateSplit(ctx context.Context, lines []string) (map[int]string, error) {
	for paused := false; ; {
		got, err := batchFn(ctx, lines)
		if err == nil {
			return got, nil
		}
		if !errors.Is(err, errRetryable) && !errors.Is(err, errTruncated) {
			return nil, err
		}
		retry := rateLimitPause
		var ae apiError
		if errors.As(err, &ae) && ae.retry > 0 {
			retry = ae.retry
		}
		// A window this far out is the daily allowance or a plan quota, not a
		// burst: recursion would stall the job for hours before reporting it.
		if retry > time.Minute {
			return nil, err
		}
		if len(lines) > 1 {
			half := len(lines) / 2
			a, err := translateSplit(ctx, lines[:half])
			if err != nil {
				return nil, err
			}
			b, err := translateSplit(ctx, lines[half:])
			if err != nil {
				return nil, err
			}
			for k, v := range b {
				a[k] = v
			}
			return a, nil
		}
		if errors.Is(err, errTruncated) {
			// One cue is the smallest ask there is; a model that cannot answer
			// it will not answer a retry of it either.
			return nil, err
		}
		if paused {
			return nil, err
		}
		paused = true
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(retry):
		}
	}
}

// translateRange translates the listed cue blocks in batches, writing results
// back into blocks and returning the blocks the model left unanswered. onBatch
// gets the number of cues actually answered, so a dropped line does not
// overstate progress before the retry pass catches it.
func translateRange(ctx context.Context, blocks [][]string, idx []int, onBatch func(int)) ([]int, error) {
	var missing []int
	for start := 0; start < len(idx); {
		end := batchEnd(blocks, idx, start)
		batch := idx[start:end]
		lines := make([]string, len(batch))
		for i, b := range batch {
			lines[i] = fmt.Sprintf("%d|%s", i+1, cueText(blocks[b]))
		}
		got, err := translateSplit(ctx, lines)
		if err != nil {
			return nil, err
		}
		answered := 0
		for i, b := range batch {
			text, ok := got[i+1]
			if !ok {
				missing = append(missing, b)
				continue
			}
			blocks[b] = setCueText(blocks[b], text)
			answered++
		}
		if onBatch != nil && answered > 0 {
			onBatch(answered)
		}
		start = end
	}
	return missing, nil
}

// batchEnd returns the slice of idx that fits one call: cues are added while the
// estimate stays inside batchTokens. The first cue is taken whatever it costs,
// so a single long line still has somewhere to go.
func batchEnd(blocks [][]string, idx []int, start int) int {
	used, end := 0, start
	for end < len(idx) {
		t := tokensFor(cueText(blocks[idx[end]])) + 4 // the "<n>|" prefix and newline
		// The cues, the system prompt and the whole reply allowance still have to
		// fit the window every model on the route is guaranteed.
		if end > start && (used+t > batchTokens || used+t+maxReplyTokens > contextTokens) {
			break
		}
		used += t
		end++
	}
	return end
}

// translateVTT walks the cue blocks in batches, translating each into the
// target language and reporting progress as distinct lines answered out of the
// distinct lines the file holds.
func translateVTT(ctx context.Context, vtt string, progress func(done, total int)) (string, error) {
	header, blocks := parseVTT(vtt)
	var queue []int
	for i, blk := range blocks {
		if cueText(blk) != "" {
			queue = append(queue, i)
		}
	}
	if len(queue) == 0 {
		return vtt, nil
	}
	// Identical lines are translated once and copied. Dialogue repeats heavily
	// ("Yes", "What?!", catchphrases), so this shrinks the prompt — keeping the
	// whole episode in one call — and makes consistency free: one source line,
	// one wording, everywhere. It also fixes what progress counts: the model
	// answers one line per distinct text, so that is the total the player is
	// told, and the bar can reach its end.
	seen := map[string]int{}
	dupOf := map[int]int{} // cue index -> index of its first occurrence in uniq
	var uniq []int
	for _, i := range queue {
		t := cueText(blocks[i])
		first, ok := seen[t]
		if !ok {
			first = len(uniq)
			seen[t] = first
			uniq = append(uniq, i)
		}
		dupOf[i] = first
	}
	done := 0
	bump := func(n int) {
		done += n
		if progress != nil {
			progress(done, len(uniq))
		}
	}
	// Reported before the first call so the player can show the size of the job
	// while the model is still thinking about it.
	if progress != nil {
		progress(0, len(uniq))
	}
	missing, err := translateRange(ctx, blocks, uniq, bump)
	if err != nil {
		return "", err
	}
	// The model occasionally drops a line. Without this pass those cues stay
	// English in an otherwise Indonesian file, which reads as "the subtitle was
	// not converted" even though most of it was.
	if len(missing) > 0 {
		// translateRange now reports its own progress, so cues the retry pass
		// rescues are counted too.
		if _, err := translateRange(ctx, blocks, missing, bump); err != nil {
			return "", err
		}
	}
	// Copy each unique line's (now Indonesian) text to its duplicates.
	for _, i := range queue {
		if first := uniq[dupOf[i]]; first != i {
			blocks[i] = setCueText(blocks[i], cueText(blocks[first]))
		}
	}
	return rebuildVTT(header, blocks), nil
}

// translateFn is indirect so the job's progress reporting is testable offline.
var translateFn = translateVTT

// ------------------------------------------------------------ rate limiting

// limiter paces calls per minute. The number is ours, not kenari's: it exists to
// keep a job from arriving in a burst, since a burst is what a 429 punishes.
type limiter struct {
	mu       sync.Mutex
	minStart time.Time
	minCount int
}

func newLimiter() *limiter {
	return &limiter{minStart: time.Now()}
}

// take reserves budget for one call, returning how long to wait when full.
func (l *limiter) take() (time.Duration, bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if now.Sub(l.minStart) >= time.Minute {
		l.minStart, l.minCount = now, 0
	}
	if l.minCount >= limitRequestsPerMinute {
		return time.Until(l.minStart.Add(time.Minute)), false
	}
	l.minCount++
	return 0, true
}

func (l *limiter) usage() map[string]int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return map[string]int{
		"requests_this_minute": l.minCount,
		"requests_per_minute":  limitRequestsPerMinute,
	}
}

var llmLimit = newLimiter()

// ------------------------------------------------------------------ caching

// subtitleCache keeps finished translations on disk so a rewatch never pays for
// the same episode twice. A cache miss on every play would re-translate the whole
// file on each seek.
type subtitleCache struct {
	dir string
	mu  sync.Mutex
	mem map[string]string
}

func newSubtitleCache(dir string) *subtitleCache {
	os.MkdirAll(dir, 0o755)
	return &subtitleCache{dir: dir, mem: map[string]string{}}
}

func (c *subtitleCache) path(key string) string {
	sum := sha256.Sum256([]byte(key))
	return filepath.Join(c.dir, hex.EncodeToString(sum[:])+".vtt")
}

func (c *subtitleCache) get(key string) (string, bool) {
	c.mu.Lock()
	if v, ok := c.mem[key]; ok {
		c.mu.Unlock()
		return v, true
	}
	c.mu.Unlock()
	b, err := os.ReadFile(c.path(key))
	if err != nil {
		return "", false
	}
	c.mu.Lock()
	c.mem[key] = string(b)
	c.mu.Unlock()
	return string(b), true
}

func (c *subtitleCache) put(key, vtt string) {
	c.mu.Lock()
	c.mem[key] = vtt
	c.mu.Unlock()
	tmp := c.path(key) + ".tmp"
	if os.WriteFile(tmp, []byte(vtt), 0o644) == nil {
		os.Rename(tmp, c.path(key))
	}
}

// -------------------------------------------------------------------- types

type Anime struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Poster       string   `json:"poster,omitempty"`
	Synopsis     string   `json:"synopsis,omitempty"`
	Japanese     string   `json:"japanese,omitempty"`
	Aired        string   `json:"aired,omitempty"`
	Premiered    string   `json:"premiered,omitempty"`
	Duration     string   `json:"duration,omitempty"`
	Status       string   `json:"status,omitempty"`
	Score        string   `json:"score,omitempty"`
	Genres       []string `json:"genres,omitempty"`
	Studios      string   `json:"studios,omitempty"`
	Producers    string   `json:"producers,omitempty"`
	SubCount     string   `json:"subCount,omitempty"`
	DubCount     string   `json:"dubCount,omitempty"`
	EpisodeCount string   `json:"episodeCount,omitempty"`
	Related      []Anime  `json:"related,omitempty"`
	Recommended  []Anime  `json:"recommended,omitempty"`
}

// sectionCards scrapes one "cat-heading" section of the anime page into a
// slim id+title list. Everything between that heading and the next one is in
// scope, so late cards cannot bleed into the next section.
func sectionCards(page, heading string) []Anime {
	start := -1
	for _, m := range reHeading.FindAllStringIndex(page, -1) {
		if strings.Contains(html.UnescapeString(page[m[0]:m[1]]), heading) {
			start = m[1]
			break
		}
	}
	if start < 0 {
		return nil
	}
	end := len(page)
	for _, m := range reHeading.FindAllStringIndex(page[start:], -1) {
		end = start + m[0]
		break
	}
	var out []Anime
	for _, c := range reCardLink.FindAllStringSubmatch(page[start:end], -1) {
		out = append(out, Anime{ID: c[1], Name: html.UnescapeString(c[2])})
	}
	return out
}

type Episode struct {
	ID     string `json:"id"`
	Number string `json:"number"`
}

type Quality struct {
	Height int    `json:"height"`
	Label  string `json:"label"`
	URL    string `json:"url"`
}

type Subtitle struct {
	Lang    string `json:"lang"`
	URL     string `json:"url"`
	Default bool   `json:"default"`
}

type Source struct {
	Master    string     `json:"master"`
	Referer   string     `json:"referer"`
	Qualities []Quality  `json:"qualities"`
	Subtitles []Subtitle `json:"subtitles"`
}

// resolvedKey identifies one episode in one audio mode.
type resolvedKey struct{ id, mode string }

type resolvedEntry struct {
	src     *Source
	expires time.Time
}

var resolvedCache sync.Map

// cachedResolve memoises resolve for a few minutes: one playlist plus every
// segment the browser asks for would otherwise re-walk the servers API and the
// embed page on each request.
func cachedResolve(ctx context.Context, id, mode string) (*Source, error) {
	key := resolvedKey{id, mode}
	if v, ok := resolvedCache.Load(key); ok {
		if e, ok := v.(resolvedEntry); ok && time.Now().Before(e.expires) {
			return e.src, nil
		}
	}
	src, err := resolve(ctx, id, mode)
	if err != nil {
		return nil, err
	}
	resolvedCache.Store(key, resolvedEntry{src, time.Now().Add(10 * time.Minute)})
	return src, nil
}

// reSegment keeps the segment proxy from turning into an open proxy: a plain
// file name in one known directory, never a path or a URL.
var reSegment = regexp.MustCompile(`^[A-Za-z0-9_-]+\.(ts|m4s|mp4)$`)

// reImgHost allow-lists the image hosts the scrapers can hand out. Anything
// else the client asks to proxy is refused, so /img is not an open relay.
var reImgHost = regexp.MustCompile(`^https://(cdn\.anipixcdn\.co|[^/]*\.?hianime\.at)/`)

// normMode collapses anything that is not a known mode onto sub, so the cache
// key, the resolve and the rewritten playlist URLs all agree on two values.
func normMode(mode string) string {
	if mode == "dub" {
		return "dub"
	}
	return "sub"
}

// variantFor returns the chosen variant playlist URL plus the referer the
// stream host insists on. Mode and quality come from the path, not the query:
// the browser resolves relative segment URIs against the playlist URL and
// drops its query string (RFC 3986), which sent every segment to the default
// variant no matter which quality was picked.
func variantFor(req *http.Request, id, mode, quality string) (variant, referer string, err error) {
	src, err := cachedResolve(req.Context(), id, normMode(mode))
	if err != nil {
		return "", "", err
	}
	variant, err = pickQuality(src, quality)
	if err != nil {
		return "", "", err
	}
	return variant, src.Referer, nil
}

// ------------------------------------------------------------------- server

func newServer(cache *subtitleCache) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.Timeout(5*time.Minute), middleware.Recoverer)

	// Semua API hidup di bawah /api/ agar terpisah tegas dari aset SPA.
	r.Route("/api", func(api chi.Router) {
		api.Get("/healthz", func(w http.ResponseWriter, req *http.Request) {
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "limits": llmLimit.usage()})
		})

		api.Get("/limits", func(w http.ResponseWriter, req *http.Request) {
			writeJSON(w, http.StatusOK, llmLimit.usage())
		})
		api.Get("/search", func(w http.ResponseWriter, req *http.Request) {
			q := strings.TrimSpace(req.URL.Query().Get("q"))
			if q == "" {
				writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing q"})
				return
			}
			results, err := search(req.Context(), q)
			if err != nil {
				writeErr(w, err)
				return
			}
			writeJSON(w, http.StatusOK, results)
		})
		// The player loads the master playlist, and the browser cannot send the
		// Referer the stream host wants, so all three levels are proxied. Mode and
		// quality live in the path: the browser resolves the playlist's relative
		// segment URIs against its own URL and drops that URL's query, so segments
		// asked for without them always came back from the default variant.
		api.Get("/hls/{id}/master.m3u8", func(w http.ResponseWriter, req *http.Request) {
			id := chi.URLParam(req, "id")
			mode := normMode(req.URL.Query().Get("mode"))
			src, err := cachedResolve(req.Context(), id, mode)
			if err != nil {
				writeErr(w, err)
				return
			}
			streamMaster(w, req, id, src, mode)
		})
		api.Get("/hls/{id}/{mode}/{quality}/index.m3u8", func(w http.ResponseWriter, req *http.Request) {
			variant, referer, err := variantFor(req, chi.URLParam(req, "id"), chi.URLParam(req, "mode"), chi.URLParam(req, "quality"))
			if err != nil {
				writeErr(w, err)
				return
			}
			streamVariant(w, req, chi.URLParam(req, "id"), chi.URLParam(req, "mode"), chi.URLParam(req, "quality"), variant, referer)
		})
		// Segmen ditulis sebagai base64url URL absolutnya: upstream menyimpannya
		// di host lain dengan nama berkamuflase (seg-N....jpg), jadi handler tidak
		// bisa merekonstruksi URL dari nama file.
		api.Get("/hls/{id}/{mode}/{quality}/s/{seg}", func(w http.ResponseWriter, req *http.Request) {		dec, err := base64.RawURLEncoding.DecodeString(chi.URLParam(req, "seg"))
		if err != nil || !strings.HasPrefix(string(dec), "https://") {
			http.NotFound(w, req)
			return
		}

			_, referer, err := variantFor(req, chi.URLParam(req, "id"), chi.URLParam(req, "mode"), chi.URLParam(req, "quality"))
			if err != nil {
				writeErr(w, err)
				return
			}
			// Content type di-sniff dari byte pertama: TS berkamuflase .jpg/.html,
			// MP4 berkamuflase .js. Segmen immutable → cache panjang.
			ct, ok := probeSegment(req.Context(), string(dec), referer)
			if !ok {
				writeErr(w, errors.New("segment probe failed"))
				return
			}
			streamUpstream(w, req, string(dec), referer, ct, "public, max-age=3600")
		})
		// The detail page carries what the search card does not: full synopsis,
		// japanese title, airing window, score, studios. Fetched once per browse.
		// /img proxies a remote image so the browser never sees the upstream CDN
		// host. The URL rides in a query param, base64url'd so signs and dots do
		// not invite URL parsing; only http(s) URLs on the known image hosts pass.
		api.Get("/img", func(w http.ResponseWriter, req *http.Request) {
			dec, err := base64.RawURLEncoding.DecodeString(req.URL.Query().Get("u"))
			if err != nil || !reImgHost.Match(dec) {
				http.Error(w, "bad image ref", http.StatusBadRequest)
				return
			}
			streamUpstream(w, req, string(dec), baseAPI+"/", "", "public, max-age=86400")
		})
		api.Get("/anime/{id}", func(w http.ResponseWriter, req *http.Request) {
			slug := chi.URLParam(req, "id")
			detail, err := animeDetails(req.Context(), slug)
			if err != nil {
				writeErr(w, err)
				return
			}
			writeJSON(w, http.StatusOK, detail)
		})
		api.Get("/anime/{id}/episodes", func(w http.ResponseWriter, req *http.Request) {
			eps, err := episodes(req.Context(), chi.URLParam(req, "id"))
			if err != nil {
				writeErr(w, err)
				return
			}
			writeJSON(w, http.StatusOK, eps)
		})
		api.Get("/episode/{id}", func(w http.ResponseWriter, req *http.Request) {
			src, err := cachedResolve(req.Context(), chi.URLParam(req, "id"), req.URL.Query().Get("mode"))
			if err != nil {
				writeErr(w, err)
				return
			}
			writeJSON(w, http.StatusOK, src)
		})
		// /subtitle answers with the converted .vtt once the job finishes, and with
		// 202 + progress until then. A <track> cannot show progress, so the player
		// polls /status and mounts the track only when the file is ready.
		api.Get("/subtitle/{id}", func(w http.ResponseWriter, req *http.Request) {
			id, mode := chi.URLParam(req, "id"), req.URL.Query().Get("mode")
			lang := wantedLang(req)
			job, err := subtitleStatus(req, cache, id, mode, lang)
			if err != nil {
				writeErr(w, err)
				return
			}
			if state, _, _, _ := job.snapshot(); state != "ready" {
				writeJob(w, http.StatusAccepted, job)
				return
			}
			vtt, ok := cache.get(subKey(id, mode, lang))
			if !ok {
				writeErr(w, errors.New("finished subtitle is missing from the cache"))
				return
			}
			w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
			// Tiap panggilan /subtitle/{id} menjalankan subtitleStatus, yang memulai
			// job baru kalau hasilnya belum masuk cache. Tanpa no-store, respons 202
			// yang tersimpan di cache browser menyebabkan poll /status dan fetch
			// .vtt saling membalik: fetch vtt membaca job "converting" lama dari cache
			// HTTP, polling terus menunggu, dan subtitle terpasang hanya setelah
			// refresh menyegarkan cache-nya.
			w.Header().Set("Cache-Control", "no-store")
			io.WriteString(w, vtt)
		})
		api.Get("/subtitle/{id}/status", func(w http.ResponseWriter, req *http.Request) {
			job, err := subtitleStatus(req, cache, chi.URLParam(req, "id"), req.URL.Query().Get("mode"), wantedLang(req))
			if err != nil {
				writeErr(w, err)
				return
			}
			writeJob(w, http.StatusOK, job)
		})
	})

	// SPA: semua path non-/api dilayani dari embed — file dist apa adanya
	// (ServeContent mengurus MIME per ekstensi), sisanya index.html supaya
	// preact-router yang memutuskan 404.
	r.Get("/*", func(w http.ResponseWriter, req *http.Request) {
		name := strings.TrimPrefix(req.URL.Path, "/")
		if name != "" && !strings.Contains(name, "..") {
			if data, err := fs.ReadFile(webRoot, name); err == nil {
				http.ServeContent(w, req, name, time.Time{}, bytes.NewReader(data))
				return
			}
		}
		index, err := fs.ReadFile(webRoot, "index.html")
		if err != nil {
			http.Error(w, "web/dist belum dibuild: jalankan `cd web && npm run build` lalu compile ulang", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(index)
	})
	return r
}

func wantedLang(req *http.Request) string {
	if lang := req.URL.Query().Get("lang"); lang != "" {
		return lang
	}
	return "id"
}

// maxConcurrentJobs bounds how many conversions translate at once. Each job
// holds a whole subtitle in memory and competes for the same five-calls-a-minute
// window, so an unbounded pile of viewers would queue without limit and none of
// them would finish. The extras wait for a slot; the player is polling status,
// so waiting is visible rather than an error.
const maxConcurrentJobs = 4

// jobSlots is the conversion semaphore. Buffered rather than a mutex so the
// wait below stays selectable against the job's context.
var jobSlots = make(chan struct{}, maxConcurrentJobs)

// --------------------------------------------------------------- sub jobs

// subJob tracks one episode's conversion. Translation runs in the background
// because a whole episode takes minutes — far past any sane request timeout —
// and the client disconnects long before it ends. The player
// polls this progress instead of waiting on a track that never loads.
//
// done and total are written together by setProgress and always describe the
// same unit: the distinct lines the model is asked to translate.
type subJob struct {
	mu      sync.Mutex
	start   time.Time
	total   int
	done    int
	err     error
	ready   bool
	running bool
}

// elapsed is how long this job has been converting, which is the denominator of
// the rate the ETA is projected from.
func (j *subJob) elapsed() time.Duration {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.start.IsZero() {
		return 0
	}
	return time.Since(j.start)
}

func (j *subJob) snapshot() (state string, done, total int, err error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	switch {
	case j.ready:
		state = "ready"
	case j.err != nil:
		state = "error"
	case j.running:
		state = "converting"
	default:
		state = "pending"
	}
	return state, j.done, j.total, j.err
}

// setProgress records where the run is. Totalling by addition would have to be
// fed increments, but the translator reports a running count: a split batch used
// to be added to the running total and drove the bar past its end, and the lines
// it deduplicates were never counted at all, so an unbroken run stopped short.
// One setter for both numbers is what keeps them commensurable.
func (j *subJob) setProgress(done, total int) {
	j.mu.Lock()
	j.done, j.total = done, total
	j.mu.Unlock()
}

func (j *subJob) finish(err error) {
	j.mu.Lock()
	j.running, j.err, j.ready = false, err, err == nil
	j.mu.Unlock()
}

// subKey names one finished conversion: an episode, one audio mode, one target
// language. It is both the cache key and the job key.
func subKey(episodeID, mode, lang string) string {
	return episodeID + "|" + mode + "|" + lang
}

var subJobs sync.Map // subKey -> *subJob

// etaSeconds projects the cues still to come from how fast the job has actually
// been going. A fixed rate would be a guess now that one call carries a whole
// episode's worth of cues and the first answer is what tells us the speed.
func etaSeconds(done, total int, elapsed time.Duration) int {
	if total <= done || done == 0 || elapsed <= 0 {
		return 0
	}
	remaining := elapsed / time.Duration(done) * time.Duration(total-done)
	return int(remaining / time.Second)
}

// subtitleStatus returns this request's conversion, starting it on first ask. A
// source already in the target language is cached verbatim: no job, no quota.
func subtitleStatus(req *http.Request, cache *subtitleCache, episodeID, mode, lang string) (*subJob, error) {
	key := subKey(episodeID, mode, lang)
	if _, ok := cache.get(key); ok {
		return &subJob{ready: true}, nil
	}
	// A conversion already under way is answered from memory, before upstream is
	// touched. Polling used to re-resolve the episode on every call, so a scrape
	// that failed mid-conversion turned each /status into an error — the player
	// counted misses, gave up, and left a finished translation sitting in the
	// cache until the page was reloaded.
	if v, ok := subJobs.Load(key); ok {
		return v.(*subJob), nil
	}
	src, err := cachedResolve(req.Context(), episodeID, mode)
	if err != nil {
		return nil, err
	}
	pick := pickSubtitle(src.Subtitles, lang)
	if pick == nil {
		return nil, errors.New("episode has no subtitles")
	}
	if strings.HasPrefix(strings.ToLower(pick.Lang), lang) {
		raw, err := get(req.Context(), pick.URL, src.Referer)
		if err != nil {
			return nil, err
		}
		cache.put(key, raw)
		return &subJob{ready: true}, nil
	}
	job := &subJob{running: true, start: time.Now()}
	actual, loaded := subJobs.LoadOrStore(key, job)
	if loaded {
		return actual.(*subJob), nil
	}
	go runSubtitleJob(cache, key, episodeID, mode, lang, job)
	return job, nil
}

func runSubtitleJob(cache *subtitleCache, key, episodeID, mode, lang string, job *subJob) {
	// Deliberately not the request context: this job outlives the request that
	// started it, which is the whole reason it runs in a goroutine.
	ctx := context.Background()
	fail := func(err error) {
		log.Printf("subtitle %s: %v", key, err)
		// Sticky on purpose: what translateSplit gives up on is structural — a
		// rejected request, a spent quota — so re-running it on each poll would
		// only burn calls to hear the same answer.
		job.finish(err)
	}
	src, err := resolve(ctx, episodeID, mode)
	if err != nil {
		fail(err)
		return
	}
	pick := pickSubtitle(src.Subtitles, lang)
	if pick == nil {
		fail(errors.New("episode has no subtitles"))
		return
	}
	raw, err := get(ctx, pick.URL, src.Referer)
	if err != nil {
		fail(err)
		return
	}
	// The fetches above run without holding a slot; only translating competes
	// for the model window.
	select {
	case jobSlots <- struct{}{}:
		defer func() { <-jobSlots }()
	case <-ctx.Done():
		fail(ctx.Err())
		return
	}
	done, err := translateFn(ctx, raw, job.setProgress)
	if err != nil {
		fail(err)
		return
	}
	cache.put(key, done)
	job.finish(nil)
}

// writeJob turns a job into the progress body the player polls.
func writeJob(w http.ResponseWriter, code int, job *subJob) {
	state, done, total, err := job.snapshot()
	body := map[string]any{
		"state":       state,
		"done":        done,
		"total":       total,
		"eta_seconds": etaSeconds(done, total, job.elapsed()),
	}
	if err != nil {
		body["error"] = err.Error()
	}
	if code == http.StatusAccepted {
		w.Header().Set("Retry-After", "3")
	}
	writeJSON(w, code, body)
}

func pickSubtitle(subs []Subtitle, lang string) *Subtitle {
	if len(subs) == 0 {
		return nil
	}
	for i := range subs {
		if strings.EqualFold(subs[i].Lang, lang) {
			return &subs[i]
		}
	}
	for i := range subs {
		if subs[i].Default {
			return &subs[i]
		}
	}
	return &subs[0]
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, errRetryable):
		// The job paces itself, so reaching this means the account limit or a
		// provider upstream is throttling traffic we did not make.
		secs := 30
		var ae apiError
		if errors.As(err, &ae) && ae.retry > 0 {
			secs = int(ae.retry / time.Second)
		}
		w.Header().Set("Retry-After", strconv.Itoa(secs))
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": err.Error()})
	case errors.Is(err, errCloudflare):
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
	default:
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
	}
}

func main() {
	addr := flag.String("addr", envOr("ADDR", ":8080"), "listen address")
	cacheDir := flag.String("cache", envOr("CACHE_DIR", ".cache/subtitles"), "subtitle cache directory")
	flag.Parse()

	srv := &http.Server{
		Addr:              *addr,
		Handler:           newServer(newSubtitleCache(*cacheDir)),
		ReadHeaderTimeout: 10 * time.Second,
		// No WriteTimeout: streams are long-lived and the server must not cut them.
	}
	log.Printf("listening on %s", *addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
