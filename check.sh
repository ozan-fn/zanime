#!/usr/bin/env bash
# Verifies the code, then restores `module main` in go.mod.
# go test cannot import a module literally named "main", so the swap is
# temporary: the delivered go.mod keeps the name you asked for.
set -euo pipefail
export PATH="$HOME/.local/sdk/go/bin:$HOME/.local/bin:$PATH"
# mise exports GOROOT for its own toolchain; that mismatch breaks the compiler.
unset GOROOT
cd /home/ozan/projects/zanime

cp go.mod go.mod.bak
trap 'mv go.mod.bak go.mod' EXIT

go mod tidy
sed -i 's/^module main$/module zanimetest/' go.mod
go vet ./...
go test "$@" ./...
