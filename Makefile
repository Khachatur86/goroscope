BINARY := bin/goroscope
VERSION ?= dev

.PHONY: build run ui fmt test test-race vet lint bench vscode

build:
	mkdir -p bin
	go build -ldflags "-X github.com/Khachatur86/goroscope/internal/version.Version=$(VERSION)" -trimpath -o $(BINARY) ./cmd/goroscope

run:
	go run ./cmd/goroscope run ./app

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
	golangci-lint run

bench:
	go test -bench=. -benchmem ./internal/tracebridge/... ./internal/analysis/...

vscode:
	cd vscode && npm install && npm run compile
