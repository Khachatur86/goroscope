BINARY := bin/goroscope
VERSION ?= dev

.PHONY: build run run-react ui fmt test test-race vet lint lint-fix bench web vscode pre-commit embed-web build-dist

build:
	mkdir -p bin
	go build -ldflags "-X github.com/Khachatur86/goroscope/internal/version.Version=$(VERSION)" -trimpath -o $(BINARY) ./cmd/goroscope

run:
	go run ./cmd/goroscope run ./app

run-react: build web
	./bin/goroscope run -ui=react -ui-path=web/dist -open-browser ./examples/trace_demo

run-react-complex: build web
	./bin/goroscope run -ui=react -ui-path=web/dist -open-browser ./examples/complex_demo

ui:
	go run ./cmd/goroscope ui

fmt:
	go fmt ./...

test:
	go test ./...

test-race:
	go test -race ./...

vet:
	go vet ./...

lint:
	golangci-lint run --timeout=5m

lint-fix:
	gofmt -w .
	golangci-lint run --fix --timeout=5m

# Run before commit: fmt, vet, test-race, lint
pre-commit: fmt vet test-race lint
	@echo "All checks passed. Safe to commit."

bench:
	go test -bench=. -benchmem ./internal/tracebridge/... ./internal/analysis/...

web:
	cd web && npm install && npm run build

# embed-web: build the React UI and copy it into the Go embed directory so that
# the next `make build` (or `make build-dist`) produces a self-contained binary.
embed-web: web
	rm -rf internal/api/reactui/assets
	cp -r web/dist/. internal/api/reactui/

# build-dist: single-command release build — React bundle baked into the binary.
build-dist: embed-web build

ui-react: build web
	./bin/goroscope ui -ui=react -ui-path=web/dist -open-browser

vscode:
	cd vscode && npm install && npm run compile
