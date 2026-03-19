BINARY := bin/goroscope
VERSION ?= dev

.PHONY: build run run-react ui fmt test test-race vet lint lint-fix bench web vscode pre-commit

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

ui-react: build web
	./bin/goroscope ui -ui=react -ui-path=web/dist -open-browser

vscode:
	cd vscode && npm install && npm run compile
