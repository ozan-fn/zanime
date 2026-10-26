.PHONY: build

# index.html sudah di-embed lewat include_str! di src/main.rs
build:
	cargo build --release --bin zanime
	upx --best --lzma target/release/zanime
	rm -f ./zanime
	cp target/release/zanime ./zanime
