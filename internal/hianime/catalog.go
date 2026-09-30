// Catalog scraping: search, anime details, episode lists.
package hianime

import (
	"context"
	"fmt"
	"html"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

var (
	// The top 10 sidebar repeats the result markup, cut it off before matching.
	reSidebar   = regexp.MustCompile(`(?s)id="main-sidebar".*`)
	reFilmBlock = regexp.MustCompile(`<div class="film-detail">`)
	reFilmName  = regexp.MustCompile(`(?s)<h3 class="film-name">\s*<a href="[^"]*/([^"/]*)"\s+title="([^"]*)"`)
	// Search cards ship the poster as a plain src (no lazyload) and a short
	// synopsis right under the title.
	rePoster   = regexp.MustCompile(`<img src="([^"]+)"\s+class="film-poster-img"`)
	reFilmDesc = regexp.MustCompile(`(?s)<div class="description">\s*(.*?)\s*</div>`)
	// Each result card is one flw-item; poster sits before film-detail inside it.
	reFlwItem = regexp.MustCompile(`(?s)<div class="flw-item "`)
	// Related / recommended sections on the anime page: id+title link pairs.
	reCardLink = regexp.MustCompile(`<a href="https?://[^/]+/([a-z0-9-]+)"\s+title="([^"]*)"`)
	reHeading  = regexp.MustCompile(`cat-heading">([^<]+)<`)

	// The anime page's poster is an <img src>, no lazyload class.
	rePagePoster = regexp.MustCompile(`<img src="([^"]+)"\s+class="film-poster-img"`)
	reH1Name     = regexp.MustCompile(`(?s)<h2 class="film-name dynamic-name" data-jname="([^"]*)"`)
	reInfoItem   = regexp.MustCompile(`(?s)<span class="item-head">([^<]+)</span>(.*?)</div>`)
	reGenreLink  = regexp.MustCompile(`/genres/[a-z-]+"[^>]*>\s*([^<]+?)\s*<`)
	reOverview   = regexp.MustCompile(`(?s)Overview:</span>\s*<div class="text">\s*(.*?)\s*</div>`)
	reTickCount  = regexp.MustCompile(`(?s)tick-item tick-sub">.*?([0-9,]+)</div>.*?tick-item tick-dub">.*?([0-9,]+)</div>.*?tick-item tick-eps">([0-9,]+)</div>`)

	reEpItem = regexp.MustCompile(`ep-item`)
)

// proxiedImg rewrites an upstream image URL into a local /img reference so the
// browser never sees the real CDN host. URLs that fail the allow-list pass
// through unchanged; the proxy would refuse them anyway.
func proxiedImg(raw string) string {
	if !ReImgHost.MatchString(raw) {
		return raw
	}
	return "/api/img?u=" + Base64RawURL([]byte(raw))
}

// SectionCards scrapes one "cat-heading" section of the anime page into a
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

// InfoValue cleans one info row: links keep their text, tags vanish, runs of
// whitespace collapse.
func infoValue(raw string) string {
	flat := reTag.ReplaceAllString(raw, " ")
	return strings.TrimSpace(strings.Join(strings.Fields(flat), " "))
}

// AnimeDetails scrapes the anime page for everything the poster and the
// one-line synopsis cannot carry: japanese title, airing window, duration,
// status, MAL score, studios and producers.
func AnimeDetails(ctx context.Context, slug string) (*Anime, error) {
	page, err := Get(ctx, BaseAPI+"/"+slug, BaseAPI+"/")
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
		a.Synopsis = StripTags(m)
	}
	// Badges next to the poster: sub/dub counts and total episodes.
	if m := reTickCount.FindStringSubmatch(page); m != nil {
		a.SubCount, a.DubCount, a.EpisodeCount = m[1], m[2], m[3]
	}
	a.Related, a.Recommended = sectionCards(page, "Related Anime"), sectionCards(page, "Recommended For You")
	return a, nil
}

// Search returns what one card on the search page shows: id, title, poster,
// and the short synopsis. The per-anime details live on the anime's own page
// and are fetched separately by AnimeDetails.
func Search(ctx context.Context, query string) ([]Anime, error) {
	page, err := Get(ctx, fmt.Sprintf(SearchAPI, url.QueryEscape(query)), BaseAPI+"/")
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
		a.Synopsis = StripTags(reFilmDesc.FindStringSubmatch(block))
		out = append(out, a)
	}
	return out, nil
}

func Episodes(ctx context.Context, slug string) ([]Episode, error) {
	// The list route is keyed by the trailing id, not the slug.
	id := slug[strings.LastIndex(slug, "-")+1:]
	page, err := Get(ctx, fmt.Sprintf(EpisodesAPI, id), BaseAPI+"/")
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
	sort.Slice(out, func(i, j int) bool { return EpNumber(out[i].Number) < EpNumber(out[j].Number) })
	return out, nil
}

func EpNumber(s string) float64 {
	f, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return f
}
