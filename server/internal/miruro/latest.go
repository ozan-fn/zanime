package miruro

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var (
	englishRef = regexp.MustCompile(`([A-Za-z0-9_$]+)\.english="((?:[^"\\]|\\.)*)"`)
	romajiRef  = regexp.MustCompile(`([A-Za-z0-9_$]+)\.romaji="((?:[^"\\]|\\.)*)"`)
	subRef     = regexp.MustCompile(`([A-Za-z0-9_$]+)\.sub=(\d+)`)
	dubRef     = regexp.MustCompile(`([A-Za-z0-9_$]+)\.dub=(\d+)`)

	animeIDRe    = regexp.MustCompile(`^([^"]+)`)
	episodeRe    = regexp.MustCompile(`\bepisode:(\d+)`)
	coverRe      = regexp.MustCompile(`coverUrl:"([^"]+)"`)
	airingAtRe   = regexp.MustCompile(`airingAt:(\d+)`)
	formatRe     = regexp.MustCompile(`format:"([^"]+)"`)
	seasonYearRe = regexp.MustCompile(`seasonYear:(\d+)`)
	titleRe      = regexp.MustCompile(`title:(\{[^{}]*\}|[A-Za-z0-9_$]+)`)
	countsRe     = regexp.MustCompile(`episodeCounts:(\{[^{}]*\}|[A-Za-z0-9_$]+)`)
)

// Latest mirrors the "Latest Episodes" array and order on miruro.cx.
func (c *Client) Latest(ctx context.Context) ([]LatestEpisode, error) {
	body, err := c.get(ctx, c.base+"/", "text/html")
	if err != nil {
		return nil, err
	}
	return ParseLatest(body)
}

// ParseLatest extracts the serialized raw:{latest:[...]} array from the
// minified home HTML. Upgrade to a real API if the markup changes.
func ParseLatest(html string) ([]LatestEpisode, error) {
	start := strings.Index(html, "raw:{latest:[")
	end := strings.Index(html, "],upcoming:")
	if start < 0 || end < 0 || end < start {
		return nil, fmt.Errorf("latest episodes array not found")
	}

	scriptStart := strings.LastIndex(html[:start], "<script")
	scriptEnd := strings.Index(html[start:], "</script>")
	if scriptEnd < 0 {
		scriptEnd = len(html) - start
	}
	if scriptStart < 0 {
		scriptStart = 0
	}
	script := html[scriptStart : start+scriptEnd]

	english := map[string]string{}
	romaji := map[string]string{}
	sub := map[string]int{}
	dub := map[string]int{}
	for _, match := range englishRef.FindAllStringSubmatch(script, -1) {
		english[match[1]] = unescape(match[2])
	}
	for _, match := range romajiRef.FindAllStringSubmatch(script, -1) {
		romaji[match[1]] = unescape(match[2])
	}
	for _, match := range subRef.FindAllStringSubmatch(script, -1) {
		sub[match[1]], _ = strconv.Atoi(match[2])
	}
	for _, match := range dubRef.FindAllStringSubmatch(script, -1) {
		dub[match[1]], _ = strconv.Atoi(match[2])
	}

	segment := html[start:end]
	chunks := strings.Split(segment, `{animeId:"`)
	episodes := make([]LatestEpisode, 0, len(chunks)-1)
	for _, chunk := range chunks[1:] {
		if episode, ok := parseLatestChunk(chunk, english, romaji, sub, dub); ok {
			episodes = append(episodes, episode)
		}
	}
	return episodes, nil
}

func parseLatestChunk(
	chunk string,
	english, romaji map[string]string,
	sub, dub map[string]int,
) (LatestEpisode, bool) {
	animeID := firstGroup(animeIDRe, chunk)
	episodeNumber, episodeErr := strconv.Atoi(firstGroup(episodeRe, chunk))
	cover := firstGroup(coverRe, chunk)
	airingAt, airingErr := strconv.Atoi(firstGroup(airingAtRe, chunk))
	format := firstGroup(formatRe, chunk)
	year, yearErr := strconv.Atoi(firstGroup(seasonYearRe, chunk))
	if animeID == "" || episodeErr != nil || cover == "" ||
		airingErr != nil || format == "" || yearErr != nil {
		return LatestEpisode{}, false
	}

	titleEn := textField(firstGroup(titleRe, chunk), "english", english)
	titleRo := textField(firstGroup(titleRe, chunk), "romaji", romaji)
	title := titleEn
	if title == "" {
		title = titleRo
	}
	if title == "" {
		title = animeID
	}

	countToken := firstGroup(countsRe, chunk)
	return LatestEpisode{
		AnimeID:  animeID,
		Title:    title,
		Romaji:   titleRo,
		ImageURL: cover,
		Episode:  episodeNumber,
		AiredAt:  time.Unix(int64(airingAt), 0).UTC(),
		Format:   format,
		Year:     year,
		SubCount: countField(countToken, "sub", sub),
		DubCount: countField(countToken, "dub", dub),
	}, true
}

func firstGroup(re *regexp.Regexp, value string) string {
	match := re.FindStringSubmatch(value)
	if len(match) < 2 {
		return ""
	}
	return match[1]
}

func textField(token, key string, refs map[string]string) string {
	if token == "" {
		return ""
	}
	if !strings.HasPrefix(token, "{") {
		return refs[token]
	}
	re := regexp.MustCompile(key + `:"((?:[^"\\]|\\.)*)"`)
	return unescape(firstGroup(re, token))
}

func countField(token, key string, refs map[string]int) *int {
	if token == "" {
		return nil
	}
	if !strings.HasPrefix(token, "{") {
		if value, ok := refs[token]; ok {
			return &value
		}
		return nil
	}
	value, err := strconv.Atoi(firstGroup(regexp.MustCompile(key+`:(\d+)`), token))
	if err != nil {
		return nil
	}
	return &value
}

func unescape(raw string) string {
	var value string
	if err := json.Unmarshal([]byte(`"`+raw+`"`), &value); err != nil {
		return raw
	}
	return value
}
