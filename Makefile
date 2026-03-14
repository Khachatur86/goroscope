BINARY := bin/goroscope

.PHONY: build run ui fmt test

build:
	mkdir -p bin
	go build -o $(BINARY) ./cmd/goroscope

run:
	go run ./cmd/goroscope run ./app

ui:
	go run ./cmd/goroscope ui

fmt:
	go fmt ./...

test:
	go test ./...
