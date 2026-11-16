# miruro proxy

Go backend for the Flutter app. It scrapes miruro/AniList, exposes JSON APIs,
and relays streams/subtitles so provider `Origin`/`Referer` headers can be
added and HLS manifests rewritten. Flutter no longer talks to those upstreams
directly.

## Structure

```
main.go              fiber app entry (ADDR, default 127.0.0.1:8080)
internal/api         fiber routes + handlers
internal/anilist     banner GraphQL client
internal/miruro      latest/search/watch clients + SvelteKit (devalue) parser
internal/proxy       upstream fetch + HLS playlist rewrite
```

## Run

```sh
cd server
air        # hot reload, config in .air.toml
# or: go run .
```

Flutter always uses `http://localhost:8080` (`lib/services/proxy.dart`). On a
device or emulator, forward the port instead of changing the URL:

```sh
adb reverse tcp:8080 tcp:8080
```

## Endpoints

- `GET /health`
- `GET /api/banners` — AniList trending currently-airing anime.
- `GET /api/latest` — miruro "Latest Episodes", same array/order.
- `GET /api/search?q=<query>` — miruro search suggestions.
- `GET /api/watch/:id?ep=<n>` — anime info, episodes, servers, subtitles.
- `GET /stream?url=<encoded>&referer=<encoded>&origin=<encoded>`
  - HLS `.m3u8` (content-type or path): manifest with every segment/key URI
    rewritten back to `/stream`.
  - Anything else: streamed through with status, range and content headers.
- CORS `*` for the Flutter web dev server.

Bind stays on loopback. Requests to private/loopback hosts are rejected unless
`ALLOW_PRIVATE_HOSTS=1` is set. Override the bind with `ADDR=host:port`.

## Tests

```sh
go vet ./...
go test ./...
```

Unit tests cover devalue parsing, latest/search/watch mapping, AniList mapping
and playlist rewriting. `internal/api` integration tests exercise every route
with stubbed services through `fiber.App.Test`.
