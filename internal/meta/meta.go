package meta

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var (
	CinemetaURL  = "https://v3-cinemeta.strem.io"
	TorrentioURL = "https://torrentio.strem.fun/sort=seeders|language=indonesian"
)

// ---------- tipe JSON ----------

type MetaPreview struct {
	ID          string `json:"id"`
	Type        string `json:"type"`
	Name        string `json:"name"`
	Year        string `json:"releaseInfo"`
	IMDBRating  string `json:"imdbRating"`
	Poster      string `json:"poster"`
	Description string `json:"description"`
}

type CatalogResp struct {
	Metas []MetaPreview `json:"metas"`
}

type Video struct {
	ID        string `json:"id"`
	Season    int    `json:"season"`
	Episode   int    `json:"episode"`
	Title     string `json:"title"`
	Released  string `json:"released"`
	Thumbnail string `json:"thumbnail"`
}

type MetaResp struct {
	Meta struct {
		ID          string   `json:"id"`
		Type        string   `json:"type"`
		Name        string   `json:"name"`
		Poster      string   `json:"poster"`
		Background  string   `json:"background"`
		Logo        string   `json:"logo"`
		Description string   `json:"description"`
		Year        string   `json:"releaseInfo"`
		IMDBRating  string   `json:"imdbRating"`
		Runtime     string   `json:"runtime"`
		Genres      []string `json:"genres"`
		Cast        []string `json:"cast"`
		Videos      []Video  `json:"videos"`
	} `json:"meta"`
}

type Stream struct {
	Name          string `json:"name"`
	Title         string `json:"title"`
	URL           string `json:"url"`
	InfoHash      string `json:"infoHash"`
	FileIdx       *int   `json:"fileIdx"`
	BehaviorHints struct {
		Filename string `json:"filename"`
	} `json:"behaviorHints"`
	Sources []string `json:"sources"`
}

type StreamResp struct {
	Streams []Stream `json:"streams"`
}

// Pick: satu baris /api/streams — data asli Torrentio + magnet siap pakai.
type Pick struct {
	Stream
	Magnet   string   `json:"magnet"`
	Trackers []string `json:"trackers"`
}

// ---------- HTTP ----------

var httpClient = &http.Client{Timeout: 90 * time.Second}

func fetchJSON(u string, v any) error {
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 frieren-cli")
	resp, err := httpClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	return json.Unmarshal(body, v)
}

// SearchAll: cari di Cinemeta (series + movie).
func SearchAll(query string) ([]MetaPreview, error) {
	var all []MetaPreview
	q := url.QueryEscape(query)
	for _, t := range []string{"series", "movie"} {
		u := fmt.Sprintf("%s/catalog/%s/top/search=%s.json", CinemetaURL, t, q)
		var r CatalogResp
		if err := fetchJSON(u, &r); err != nil {
			return nil, err
		}
		all = append(all, r.Metas...)
	}
	return all, nil
}

func GetMeta(typ, id string) (*MetaResp, error) {
	u := fmt.Sprintf("%s/meta/%s/%s.json", CinemetaURL, typ, id)
	var r MetaResp
	if err := fetchJSON(u, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

func GetStreams(typ, id string) ([]Stream, error) {
	u := fmt.Sprintf("%s/stream/%s/%s.json", TorrentioURL, typ, id)
	var r StreamResp
	if err := fetchJSON(u, &r); err != nil {
		return nil, err
	}
	return r.Streams, nil
}

// BuildMagnet: infoHash + tracker dari Sources; tanpa infoHash → URL stream.
func BuildMagnet(s Stream, fallbackName string) string {
	if s.InfoHash == "" {
		return s.URL
	}
	var trackers []string
	for _, src := range s.Sources {
		if strings.HasPrefix(src, "tracker:") {
			trackers = append(trackers, "&tr="+url.QueryEscape(strings.TrimPrefix(src, "tracker:")))
		}
	}
	name := s.BehaviorHints.Filename
	if name == "" {
		name = fallbackName
	}
	return fmt.Sprintf("magnet:?xt=urn:btih:%s&dn=%s%s",
		s.InfoHash, url.QueryEscape(name), strings.Join(trackers, ""))
}

// TrackersFromStreams: daftar tracker URL dari stream.Sources (untuk client torrent).
func TrackersFrom(s Stream) []string {
	var trs []string
	for _, src := range s.Sources {
		if strings.HasPrefix(src, "tracker:") {
			trs = append(trs, strings.TrimPrefix(src, "tracker:"))
		}
	}
	return trs
}
