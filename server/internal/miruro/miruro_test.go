package miruro

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestParseDevalueResolvesRefsAndChunks(t *testing.T) {
	payload := strings.Join([]string{
		`{"type":"data","nodes":[{"type":"data","data":[` +
			`{"title":1,"genres":2,"chunk":["Promise",4],"count":6},` +
			`"Hello",[1,7],0,5,0,42,"world"]}]}`,
		`{"type":"chunk","id":5,"data":["World"]}`,
	}, "\n")

	data, err := parseDevalue(payload)
	if err != nil {
		t.Fatal(err)
	}
	root, err := data.root(0)
	if err != nil {
		t.Fatal(err)
	}
	page, ok := root.(map[string]any)
	if !ok {
		t.Fatalf("expected map, got %T", root)
	}
	if page["title"] != "Hello" {
		t.Fatalf("title = %v", page["title"])
	}
	genres, _ := page["genres"].([]any)
	if len(genres) != 2 || genres[0] != "Hello" || genres[1] != "world" {
		t.Fatalf("genres = %v", genres)
	}
	if page["chunk"] != "World" {
		t.Fatalf("chunk = %v", page["chunk"])
	}
	if page["count"] != json.Number("42") && page["count"] != 42 {
		t.Fatalf("count = %v (%T)", page["count"], page["count"])
	}
}

func TestLatestParsesHomeHTML(t *testing.T) {
	html := `<html><script>
const a={}, b={};
a.english="Frieren"; a.romaji="Sousou no Frieren"; b.sub=12; b.dub=3;
raw:{latest:[{animeId:"id1",episode:5,coverUrl:"https://img/1.jpg",airingAt:1700000000,format:"TV",seasonYear:2024,title:a,episodeCounts:b},{animeId:"id2",episode:2,coverUrl:"https://img/2.jpg",airingAt:1700000100,format:"ONA",seasonYear:2023,title:{romaji:"Only Romaji"},episodeCounts:{sub:7}}],upcoming:[]}
</script></html>`

	episodes, err := ParseLatest(html)
	if err != nil {
		t.Fatal(err)
	}
	if len(episodes) != 2 {
		t.Fatalf("got %d episodes", len(episodes))
	}
	first := episodes[0]
	if first.AnimeID != "id1" || first.Title != "Frieren" ||
		first.Episode != 5 || first.Format != "TV" || first.Year != 2024 {
		t.Fatalf("first = %+v", first)
	}
	if first.SubCount == nil || *first.SubCount != 12 ||
		first.DubCount == nil || *first.DubCount != 3 {
		t.Fatalf("counts = %v/%v", first.SubCount, first.DubCount)
	}
	if !first.AiredAt.Equal(time.Unix(1700000000, 0).UTC()) {
		t.Fatalf("airedAt = %v", first.AiredAt)
	}
	second := episodes[1]
	if second.Title != "Only Romaji" || second.Romaji != "Only Romaji" ||
		second.SubCount == nil || *second.SubCount != 7 || second.DubCount != nil {
		t.Fatalf("second = %+v", second)
	}
}

func TestBuildWatchFiltersServersAndSubtitles(t *testing.T) {
	root := map[string]any{
		"media": map[string]any{
			"title":        map[string]any{"english": "Dungeon Meshi", "romaji": "Dungeon"},
			"coverImage":   map[string]any{"extraLarge": "https://img/cover.jpg"},
			"description":  "Desc",
			"format":       "TV",
			"status":       "FINISHED",
			"season":       "WINTER",
			"genres":       []any{"Adventure", "Fantasy"},
			"seasonYear":   2024,
			"episodes":     24,
			"duration":     25,
			"averageScore": 86,
			"studios": map[string]any{"edges": []any{
				map[string]any{"isMain": true, "node": map[string]any{"name": "TRIGGER"}},
				map[string]any{"isMain": false, "node": map[string]any{"name": "Other"}},
			}},
		},
		"episodesData": map[string]any{
			"currentEpisode": map[string]any{"number": 2},
			"episodes": []any{
				map[string]any{"number": 1, "title": "Hot Pot", "airDate": "2024-01-04"},
				map[string]any{"number": 2, "title": "Tart"},
			},
		},
		"sourcesData": map[string]any{"tracks": []any{
			map[string]any{"track": "sub", "providers": []any{
				map[string]any{
					"provider": "sun",
					"subtitles": []any{
						map[string]any{"label": "English", "file": "https://sub/en.vtt"},
						map[string]any{"label": "Indonesian", "file": "https://sub/id.vtt"},
						map[string]any{"label": "Indonesia", "file": "https://sub/id2.vtt"},
						map[string]any{"label": "Arabic", "file": "https://sub/ar.vtt"},
						map[string]any{"label": "English", "file": ""},
					},
					"servers": []any{
						map[string]any{"streams": []any{
							map[string]any{"url": ""},
							map[string]any{"url": "https://cdn/1.m3u8"},
						}, "headers": map[string]any{"Referer": "https://ultracloud.cc"}},
						map[string]any{"streams": []any{map[string]any{"url": ""}}},
					},
				},
			}},
		}},
	}

	data := buildWatch(root, 1)
	if data.Anime.Title != "Dungeon Meshi" || data.Anime.ImageURL != "https://img/cover.jpg" {
		t.Fatalf("anime = %+v", data.Anime)
	}
	if len(data.Anime.Studios) != 1 || data.Anime.Studios[0] != "TRIGGER" {
		t.Fatalf("studios = %v", data.Anime.Studios)
	}
	if len(data.Episodes) != 2 || data.Episodes[0].Title != "Hot Pot" {
		t.Fatalf("episodes = %v", data.Episodes)
	}
	if data.CurrentEpisode != 2 {
		t.Fatalf("currentEpisode = %d", data.CurrentEpisode)
	}
	if len(data.Servers) != 1 || data.Servers[0].URL != "https://cdn/1.m3u8" ||
		data.Servers[0].Headers["Referer"] != "https://ultracloud.cc" {
		t.Fatalf("servers = %+v", data.Servers)
	}
	if len(data.Subtitles) != 2 ||
		data.Subtitles[0].Label != "English" ||
		data.Subtitles[1].Label != "Indonesian" {
		t.Fatalf("subtitles = %+v", data.Subtitles)
	}
}
