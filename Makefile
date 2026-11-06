GOCACHE ?= /tmp/tidings-go-cache
export GOCACHE

.PHONY: build run test check test-mutations test-browser fuzz-smoke
build:
	go build -trimpath -o bin/tidings ./cmd/tidings
run:
	go run ./cmd/tidings serve
test:
	go test -race ./...
check:
	test -z "$$(gofmt -l cmd internal)"
	go vet ./...
	go test -race ./...
	python3 scripts/mutate.py scripts/mutations.json
test-mutations:
	python3 scripts/mutate.py scripts/mutations.json
test-browser: build
	node scripts/browser/check.mjs
fuzz-smoke:
	go test ./internal/reader -run '^$$' -fuzz '^FuzzParseFeed$$' -fuzztime 3s -parallel 2
	go test ./internal/reader -run '^$$' -fuzz '^FuzzOPML$$' -fuzztime 3s -parallel 2
