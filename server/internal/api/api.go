package api

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"

	"miruro-proxy/internal/anilist"
	"miruro-proxy/internal/miruro"
	"miruro-proxy/internal/proxy"
)

type Banners interface {
	Banners(ctx context.Context, perPage int) ([]anilist.Banner, error)
}

type Miruro interface {
	Latest(ctx context.Context) ([]miruro.LatestEpisode, error)
	Search(ctx context.Context, query string) ([]miruro.SearchResult, error)
	Watch(ctx context.Context, animeID string, episode int) (*miruro.WatchData, error)
}

type Streamer interface {
	Fetch(ctx context.Context, target, referer, origin, rangeHeader string) (*proxy.Upstream, error)
}

func New(banners Banners, source Miruro, streams Streamer) *fiber.App {
	app := fiber.New()
	app.Use(cors.New(cors.Config{
		AllowOrigins: "*",
		AllowHeaders: "*",
		AllowMethods: "GET,OPTIONS",
	}))

	app.Get("/health", func(c *fiber.Ctx) error {
		return c.SendString("ok")
	})

	app.Get("/api/banners", func(c *fiber.Ctx) error {
		items, err := banners.Banners(c.UserContext(), 10)
		if err != nil {
			return fiber.NewError(fiber.StatusBadGateway, err.Error())
		}
		return c.JSON(items)
	})

	app.Get("/api/latest", func(c *fiber.Ctx) error {
		items, err := source.Latest(c.UserContext())
		if err != nil {
			return fiber.NewError(fiber.StatusBadGateway, err.Error())
		}
		return c.JSON(items)
	})

	app.Get("/api/search", func(c *fiber.Ctx) error {
		items, err := source.Search(c.UserContext(), c.Query("q"))
		if err != nil {
			return fiber.NewError(fiber.StatusBadGateway, err.Error())
		}
		return c.JSON(items)
	})

	app.Get("/api/watch/:id", func(c *fiber.Ctx) error {
		episode := 1
		if raw := c.Query("ep"); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed < 1 {
				return fiber.NewError(fiber.StatusBadRequest, "invalid episode")
			}
			episode = parsed
		}
		data, err := source.Watch(c.UserContext(), c.Params("id"), episode)
		if err != nil {
			return fiber.NewError(fiber.StatusBadGateway, err.Error())
		}
		return c.JSON(data)
	})

	app.Get("/stream", func(c *fiber.Ctx) error {
		upstream, err := streams.Fetch(
			c.UserContext(),
			c.Query("url"),
			c.Query("referer"),
			c.Query("origin"),
			c.Get("Range"),
		)
		if err != nil {
			switch {
			case errors.Is(err, proxy.ErrInvalidURL):
				return fiber.NewError(fiber.StatusBadRequest, err.Error())
			case errors.Is(err, proxy.ErrPrivateHost):
				return fiber.NewError(fiber.StatusForbidden, err.Error())
			default:
				return fiber.NewError(fiber.StatusBadGateway, err.Error())
			}
		}
		defer upstream.Body.Close()

		c.Status(upstream.StatusCode)
		if upstream.StatusCode >= http.StatusBadRequest {
			_, err := io.Copy(c.Response().BodyWriter(), upstream.Body)
			return err
		}

		contentType := upstream.Header.Get("Content-Type")
		if proxy.IsPlaylist(contentType, upstream.FinalURL.Path) {
			body, err := io.ReadAll(io.LimitReader(upstream.Body, 8<<20))
			if err != nil {
				return fiber.NewError(fiber.StatusBadGateway, "read error")
			}
			c.Set("Content-Type", "application/vnd.apple.mpegurl")
			return c.SendString(proxy.RewritePlaylist(
				string(body),
				upstream.FinalURL,
				c.Query("referer"),
				c.Query("origin"),
			))
		}

		if contentType != "" {
			c.Set("Content-Type", contentType)
		}
		if value := upstream.Header.Get("Content-Range"); value != "" {
			c.Set("Content-Range", value)
		}
		if value := upstream.Header.Get("Accept-Ranges"); value != "" {
			c.Set("Accept-Ranges", value)
		}
		_, err = io.Copy(c.Response().BodyWriter(), upstream.Body)
		return err
	})

	return app
}
