.PHONY: server client build web

web:
	pnpm --dir web build

server: web
	cargo run --release

client:
	pnpm --dir web install
	pnpm --dir web dev

build: web
	cargo build --release
	rm zanime
	cp target/release/zanime ./zanime
