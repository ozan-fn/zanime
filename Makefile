BINARY := target/release/zanime
CARGO ?= cargo

.PHONY: build run test fmt lint clean

build:
	$(CARGO) build --release
	rm -f zanime
	cp $(BINARY) .
	upx --best --lzma zanime

run:
	$(CARGO) run --release

test:
	$(CARGO) test

fmt:
	$(CARGO) fmt

lint:
	$(CARGO) clippy --all-targets

clean:
	$(CARGO) clean
