BINARY := zanime
GO ?= go

.PHONY: build run vet test clean

build:
	CGO_ENABLED=0 $(GO) build -o $(BINARY) .

run:
	$(GO) run .

vet:
	$(GO) vet ./...

test:
	$(GO) test -race ./... -count=1 -timeout 15s

clean:
	rm -f $(BINARY) $(BINARY).exe
