.PHONY: deps web embed server client build test

# deps: modul generator embed (sekali saja).
deps:
	v install gechandesu.embedfs

# web: build SPA + hasilkan web_dist.v (embedfs) untuk build ter-embed.
web:
	pnpm --dir web build
	v run gen_embed.vsh
	v fmt -w web_dist.v

# embed/build: biner tunggal, web/dist sudah di dalam biner.
embed: web
	v -d embed_dist -o zanime .

build: embed

# server: dev, aset dibaca dari web/dist di disk (tanpa embed).
server: web
	v run .

client:
	pnpm --dir web install
	pnpm --dir web dev

test:
	v test internal/
