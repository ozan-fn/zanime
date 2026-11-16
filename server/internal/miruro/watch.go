package miruro

import (
	"context"
	"regexp"
	"strings"
)

var subtitleMatch = regexp.MustCompile(`\b(english|indonesian|indonesia)\b`)

func (c *Client) Watch(ctx context.Context, animeID string, episode int) (*WatchData, error) {
	root, err := c.getData(
		ctx,
		"/watch/"+animeID+"/x/__data.json",
		map[string]string{"ep": itoa(episode)},
	)
	if err != nil {
		return nil, err
	}
	data := buildWatch(root, episode)
	return &data, nil
}

func buildWatch(root map[string]any, episode int) WatchData {
	media := asMap(root["media"])
	titles := asMap(media["title"])
	cover := asMap(media["coverImage"])

	studios := []string{}
	for _, raw := range asList(asMap(media["studios"])["edges"]) {
		edge := asMap(raw)
		if edge["isMain"] == true {
			studios = append(studios, asString(asMap(edge["node"])["name"]))
		}
	}

	episodesData := asMap(root["episodesData"])
	episodes := []WatchEpisode{}
	for _, raw := range asList(episodesData["episodes"]) {
		item := asMap(raw)
		episodes = append(episodes, WatchEpisode{
			Number:      intValue(item["number"]),
			Title:       asString(item["title"]),
			ImageURL:    stringPtr(item["image"]),
			AirDate:     stringPtr(item["airDate"]),
			Description: stringPtr(item["description"]),
			Duration:    asIntPtr(item["duration"]),
		})
	}

	currentEpisode := episode
	if current, ok := asInt(asMap(episodesData["currentEpisode"])["number"]); ok {
		currentEpisode = current
	}

	sources := asMap(root["sourcesData"])
	tracks := asList(sources["tracks"])
	var track map[string]any
	for _, name := range []string{"ssub", "sub"} {
		for _, raw := range tracks {
			candidate := asMap(raw)
			if asString(candidate["track"]) == name {
				track = candidate
				break
			}
		}
		if track != nil {
			break
		}
	}
	if track == nil && len(tracks) > 0 {
		track = asMap(tracks[0])
	}

	servers := []WatchServer{}
	subtitles := []WatchSubtitle{}
	seenSubtitles := map[string]bool{}
	for _, raw := range asList(track["providers"]) {
		provider := asMap(raw)

		for _, subtitleRaw := range asList(provider["subtitles"]) {
			item := asMap(subtitleRaw)
			label := strings.TrimSpace(asString(item["label"]))
			file := strings.TrimSpace(asString(item["file"]))
			if label == "" || file == "" {
				continue
			}
			key := strings.TrimSpace(
				regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(strings.ToLower(label), " "),
			)
			match := subtitleMatch.FindStringSubmatch(key)
			if len(match) < 2 {
				continue
			}
			canonical := "english"
			if strings.HasPrefix(match[1], "indonesia") {
				canonical = "indonesian"
			}
			if seenSubtitles[canonical] {
				continue
			}
			seenSubtitles[canonical] = true
			subtitles = append(subtitles, WatchSubtitle{
				Label:     label,
				URL:       file,
				IsDefault: item["default"] == true,
			})
		}

		for _, serverRaw := range asList(provider["servers"]) {
			server := asMap(serverRaw)
			var streamURL string
			for _, streamRaw := range asList(server["streams"]) {
				stream := asMap(streamRaw)
				if url := asString(stream["url"]); url != "" {
					streamURL = url
					break
				}
			}
			if streamURL == "" {
				continue
			}
			headers := map[string]string{}
			for key, value := range asMap(server["headers"]) {
				if text := asString(value); text != "" {
					headers[key] = text
				}
			}
			servers = append(servers, WatchServer{
				Provider: asString(provider["provider"]),
				URL:      streamURL,
				Headers:  headers,
			})
			break
		}
	}

	return WatchData{
		Anime: WatchAnime{
			Title:       firstNonEmpty(asString(titles["english"]), asString(titles["romaji"])),
			ImageURL:    asString(cover["extraLarge"]),
			Description: asString(media["description"]),
			Format:      asString(media["format"]),
			Status:      asString(media["status"]),
			Season:      asString(media["season"]),
			Genres:      asStringSlice(media["genres"]),
			Studios:     studios,
			Year:        asIntPtr(media["seasonYear"]),
			Episodes:    asIntPtr(media["episodes"]),
			Duration:    asIntPtr(media["duration"]),
			Score:       asIntPtr(media["averageScore"]),
		},
		Episodes:       episodes,
		Servers:        servers,
		Subtitles:      subtitles,
		CurrentEpisode: currentEpisode,
	}
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	negative := value < 0
	if negative {
		value = -value
	}
	var digits [20]byte
	pos := len(digits)
	for value > 0 {
		pos--
		digits[pos] = byte('0' + value%10)
		value /= 10
	}
	if negative {
		pos--
		digits[pos] = '-'
	}
	return string(digits[pos:])
}

func intValue(value any) int {
	parsed, _ := asInt(value)
	return parsed
}

func stringPtr(value any) *string {
	text, ok := value.(string)
	if !ok || text == "" {
		return nil
	}
	return &text
}
