# syntax=docker/dockerfile:1

# ── Stage 1: build React UI ───────────────────────────────────────────────────
FROM node:22-alpine AS web-builder
WORKDIR /app/web
COPY web/package*.json ./
RUN npm ci --prefer-offline
COPY web/ ./
RUN npm run build

# ── Stage 2: build Go binary ──────────────────────────────────────────────────
FROM golang:1.24-alpine AS go-builder
WORKDIR /app

# Fetch dependencies first (cached layer).
COPY go.mod go.sum ./
RUN go mod download

# Copy source and React dist (embedded via go:embed).
COPY . .
COPY --from=web-builder /app/web/dist ./internal/api/reactui/

ARG VERSION=dev
RUN CGO_ENABLED=0 GOOS=linux go build \
      -ldflags "-s -w -X github.com/Khachatur86/goroscope/internal/version.Version=${VERSION}" \
      -trimpath \
      -o /goroscope \
      ./cmd/goroscope

# ── Stage 3: minimal runtime image ───────────────────────────────────────────
FROM scratch
COPY --from=go-builder /goroscope /goroscope
# ca-certificates are needed for HTTPS targets (attach --target=https://...)
COPY --from=go-builder /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/

EXPOSE 7070
ENTRYPOINT ["/goroscope"]
CMD ["ui"]
