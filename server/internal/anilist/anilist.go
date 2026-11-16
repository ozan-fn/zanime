package anilist

import (
	"context"
	"fmt"
	"time"

	"github.com/go-resty/resty/v2"
)

const bannersQuery = `query($perPage: Int) { Page(page: 1, perPage: $perPage) {
  media(type: ANIME, status: RELEASING, sort: TRENDING_DESC) {
    title { romaji english }
    bannerImage
    coverImage { extraLarge }
    genres
    seasonYear
    episodes
    nextAiringEpisode { episode }
  }
} }`

type Banner struct {
	Title    string   `json:"title"`
	ImageURL string   `json:"imageUrl"`
	Genres   []string `json:"genres"`
	Year     *int     `json:"year"`
	Episodes *int     `json:"episodes"`
	Aired    *int     `json:"aired"`
}

type Client struct {
	http     *resty.Client
	endpoint string
}

func New() *Client {
	return NewWithEndpoint("https://graphql.anilist.co")
}

func NewWithEndpoint(endpoint string) *Client {
	return &Client{
		endpoint: endpoint,
		http:     resty.New().SetTimeout(30 * time.Second),
	}
}

func (c *Client) Banners(ctx context.Context, perPage int) ([]Banner, error) {
	var payload struct {
		Data struct {
			Page struct {
				Media []struct {
					Title struct {
						Romaji  string `json:"romaji"`
						English string `json:"english"`
					} `json:"title"`
					BannerImage string `json:"bannerImage"`
					CoverImage  struct {
						ExtraLarge string `json:"extraLarge"`
					} `json:"coverImage"`
					Genres            []string `json:"genres"`
					SeasonYear        *int     `json:"seasonYear"`
					Episodes          *int     `json:"episodes"`
					NextAiringEpisode *struct {
						Episode int `json:"episode"`
					} `json:"nextAiringEpisode"`
				} `json:"media"`
			} `json:"Page"`
		} `json:"data"`
	}
	resp, err := c.http.R().
		SetContext(ctx).
		SetBody(map[string]any{
			"query":     bannersQuery,
			"variables": map[string]int{"perPage": perPage},
		}).
		SetResult(&payload).
		Post(c.endpoint)
	if err != nil {
		return nil, fmt.Errorf("anilist request: %w", err)
	}
	if resp.StatusCode() != 200 {
		return nil, fmt.Errorf("anilist HTTP %d", resp.StatusCode())
	}

	media := payload.Data.Page.Media
	banners := make([]Banner, 0, len(media))
	for _, item := range media {
		image := item.BannerImage
		if image == "" {
			image = item.CoverImage.ExtraLarge
		}
		aired := item.Episodes
		if item.NextAiringEpisode != nil {
			value := item.NextAiringEpisode.Episode - 1
			aired = &value
		}
		title := item.Title.English
		if title == "" {
			title = item.Title.Romaji
		}
		banners = append(banners, Banner{
			Title:    title,
			ImageURL: image,
			Genres:   item.Genres,
			Year:     item.SeasonYear,
			Episodes: item.Episodes,
			Aired:    aired,
		})
	}
	return banners, nil
}
