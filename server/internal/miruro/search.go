package miruro

import "context"

func (c *Client) Search(ctx context.Context, query string) ([]SearchResult, error) {
	root, err := c.getData(ctx, "/search/__data.json", map[string]string{"q": query})
	if err != nil {
		return nil, err
	}
	items := asList(root["items"])
	results := make([]SearchResult, 0, len(items))
	for _, raw := range items {
		item := asMap(raw)
		title := asMap(item["title"])
		results = append(results, SearchResult{
			AnimeID:  asString(item["id"]),
			Title:    firstNonEmpty(asString(title["english"]), asString(title["romaji"])),
			ImageURL: asString(item["cover_url"]),
			Format:   asString(item["format"]),
			Year:     asIntPtr(item["season_year"]),
			Episodes: asIntPtr(item["episode_count"]),
			Score:    asIntPtr(item["average_score"]),
		})
	}
	return results, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
