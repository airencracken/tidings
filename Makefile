GOCACHE ?= /tmp/tidings-go-cache
export GOCACHE
PORT ?= 8085
.DEFAULT_GOAL := help

.PHONY: help demo build run test check test-mutations test-browser fuzz-smoke
help:
	@printf 'Tidings\n\n  make demo [PORT=9000]  Disposable demo with sample stories and logins\n  make build             Build the standalone server\n  make run               Run your instance using TIDINGS_* settings\n  make check             Formatting, vet, race tests, and mutations\n  make test-browser      Browser checks\n  make fuzz-smoke        Feed and OPML parser fuzzing\n'
demo:
	go run -buildvcs=false ./scripts/demo -port "$(PORT)"
build:
	go build -trimpath -ldflags "-X main.version=$$(cat VERSION)" -o bin/tidings ./cmd/tidings
run:
	go run ./cmd/tidings serve
test:
	go test -race ./...
check: test-suite-assets test-config test-cli
	python3 -m unittest discover -s scripts/containers -p "test_*.py"
	python3 scripts/release/test_config.py
	python3 scripts/test_project.py
	test -z "$$(gofmt -l cmd internal deploy scripts/demo)"
	go vet ./...
	go test -race ./...
	python3 scripts/mutate.py scripts/mutations.json scripts/audit-mutations.json scripts/suite-mutations.json
test-mutations:
	python3 scripts/mutate.py scripts/mutations.json scripts/audit-mutations.json scripts/suite-mutations.json
test-browser: build
	node scripts/browser/check.mjs
fuzz-smoke:
	go test ./internal/reader -run '^$$' -fuzz '^FuzzParseFeed$$' -fuzztime 3s -parallel 2
	go test ./internal/reader -run '^$$' -fuzz '^FuzzOPML$$' -fuzztime 3s -parallel 2
	go test ./internal/reader -run '^$$' -fuzz '^FuzzDecodeExport$$' -fuzztime 3s -parallel 2
	go test ./internal/reader -run '^$$' -fuzz '^FuzzArticleMarkup$$' -fuzztime 3s -parallel 2
	go test ./internal/reader -run '^$$' -fuzz '^FuzzCleanArticleURL$$' -fuzztime 3s -parallel 2
	go test ./internal/reader -run '^$$' -fuzz '^FuzzAdaptiveScheduleBounds$$' -fuzztime 3s -parallel 2

PREFIX ?= /usr/local
DESTDIR ?=
install: build
	install -d "$(DESTDIR)$(PREFIX)/bin"
	install -m 0755 bin/tidings "$(DESTDIR)$(PREFIX)/bin/tidings"
install-systemd: install
	install -d "$(DESTDIR)/etc/tidings" "$(DESTDIR)/usr/local/lib/systemd/system"
	install -m 0644 deploy/systemd/tidings.service "$(DESTDIR)/usr/local/lib/systemd/system/tidings.service"
	@if [ -e "$(DESTDIR)/etc/tidings/tidings.env" ] || [ -L "$(DESTDIR)/etc/tidings/tidings.env" ]; then \
		echo "Keeping existing $(DESTDIR)/etc/tidings/tidings.env"; \
	else install -m 0600 deploy/systemd/tidings.env "$(DESTDIR)/etc/tidings/tidings.env"; fi
install-openrc: install
	install -d "$(DESTDIR)/etc/init.d" "$(DESTDIR)/etc/conf.d"
	install -m 0755 deploy/openrc/tidings "$(DESTDIR)/etc/init.d/tidings"
	@if [ -e "$(DESTDIR)/etc/conf.d/tidings" ] || [ -L "$(DESTDIR)/etc/conf.d/tidings" ]; then \
		echo "Keeping existing $(DESTDIR)/etc/conf.d/tidings"; \
	else install -m 0600 deploy/openrc/tidings.conf "$(DESTDIR)/etc/conf.d/tidings"; fi

# Service installation is explicit; these targets never start or enable it.

.PHONY: install install-systemd install-openrc

.PHONY: test-containers test-container-registry
test-containers:
	python3 -m unittest discover -s scripts/containers -p 'test_*.py'
test-container-registry:
	python3 scripts/containers/check_registry.py

.PHONY: test-cli
test-cli: build
	python3 "$$(go list -m -f '{{.Dir}}' github.com/airencracken/comfylib)/tools/check_password_cli.py" --binary bin/tidings --command create-owner --data-env TIDINGS_DATA_DIR --prompt "Password: " --confirmation "Confirm password: "
	python3 "$$(go list -m -f '{{.Dir}}' github.com/airencracken/comfylib)/tools/check_setup.py" --binary bin/tidings --app tidings

.PHONY: test-config
test-config:
	python3 scripts/test_service_config.py

.PHONY: test-suite-assets
test-suite-assets: ## Verify synchronized suite source and assets
	python3 scripts/verify_suite.py
	python3 scripts/test_suite_deployment.py
	python3 scripts/test_release_dispatch.py
