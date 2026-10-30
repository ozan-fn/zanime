BINARY := zanime
GO ?= go

# -s -w: buang tabel simbol + info DWARF (binary jauh lebih kecil, tetap bisa
# dijalankan).
LDFLAGS := -s -w

.PHONY: build run vet test clean

# Build frontend (Vite → web/dist) dulu, baru binary Go yang meng-embed
# web/dist. Di-strip lalu dikompres upx kalau tersedia; kalau upx tidak ada,
# build tetap selesai dan hanya memberi pesan.
build:
	pnpm --dir web build
	$(GO) build -ldflags "$(LDFLAGS)" -o $(BINARY) .
	@if command -v upx >/dev/null 2>&1; then \
		upx --best --lzma ./$(BINARY); \
	else \
		echo "upx tidak terpasang: $(BINARY) dibiarkan tanpa kompresi"; \
	fi

run:
	$(GO) run .

vet:
	$(GO) vet ./...

test:
	$(GO) test -race ./... -count=1 -timeout 15s

clean:
	rm -f $(BINARY) $(BINARY).exe
