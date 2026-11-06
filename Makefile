GOCACHE ?= /tmp/tidings-go-cache
export GOCACHE
PORT ?= 8083
.DEFAULT_GOAL := help

.PHONY: help demo build run test check test-mutations test-browser fuzz-smoke
help:
	@printf 'Tidings\n\n  make demo [PORT=9000]  Disposable demo with sample stories and logins\n  make build             Build the standalone server\n  make run               Run your instance using TIDINGS_* settings\n  make check             Formatting, vet, race tests, and mutations\n  make test-browser      Browser checks\n  make fuzz-smoke        Feed and OPML parser fuzzing\n'
demo:
	go run -buildvcs=false ./scripts/demo -port "$(PORT)"
build:
	go build -trimpath -o bin/tidings ./cmd/tidings
run:
	go run ./cmd/tidings serve
test:
	go test -race ./...
check:
	python3 scripts/test_project.py
	test -z "$$(gofmt -l cmd internal deploy scripts/demo)"
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
	go test ./internal/reader -run '^$$' -fuzz '^FuzzDecodeExport$$' -fuzztime 3s -parallel 2

PREFIX ?= /usr/local
DESTDIR ?=
install: build
	install -d "$(DESTDIR)$(PREFIX)/bin"
	install -m 0755 bin/tidings "$(DESTDIR)$(PREFIX)/bin/tidings"
install-systemd: install
	install -d "$(DESTDIR)/etc/tidings" "$(DESTDIR)/usr/local/lib/systemd/system"
	install -m 0644 deploy/systemd/tidings.service "$(DESTDIR)/usr/local/lib/systemd/system/tidings.service"
	test -e "$(DESTDIR)/etc/tidings/tidings.env" || install -m 0600 deploy/systemd/tidings.env "$(DESTDIR)/etc/tidings/tidings.env"
install-openrc: install
	install -d "$(DESTDIR)/etc/init.d" "$(DESTDIR)/etc/conf.d"
	install -m 0755 deploy/openrc/tidings "$(DESTDIR)/etc/init.d/tidings"
	test -e "$(DESTDIR)/etc/conf.d/tidings" || install -m 0600 deploy/openrc/tidings.conf "$(DESTDIR)/etc/conf.d/tidings"

# Service installation is explicit; these targets never start or enable it.

.PHONY: install install-systemd install-openrc
