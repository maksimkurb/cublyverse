# syntax=docker/dockerfile:1

# --- WebApp -----------------------------------------------------------------
FROM node:24-bookworm-slim AS web

RUN npm install --global pnpm@12.6.0

WORKDIR /src/web

COPY web/package.json web/pnpm-lock.yaml web/pnpm-workspace.yaml ./
RUN pnpm install --frozen-lockfile

COPY web/ ./
RUN pnpm run build

# --- Go binary --------------------------------------------------------------
FROM golang:1.27-bookworm AS build

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

# The build context excludes web/dist; the WebApp is embedded from the previous
# stage so the binary serves it on its own.
COPY . .
COPY --from=web /src/web/dist ./web/dist

# cgo is required by the Matrix transport (SQLite crypto store).
ENV CGO_ENABLED=1
ARG VERSION=dev
RUN go build -trimpath \
    -ldflags="-s -w -X github.com/maksimkurb/cublyverse/internal/version.Version=${VERSION}" \
    -o /out/cublyverse ./cmd/cublyverse

# --- Runtime ----------------------------------------------------------------
FROM debian:bookworm-slim

RUN apt-get update \
    && apt-get install -y --no-install-recommends ca-certificates tzdata wget \
    && rm -rf /var/lib/apt/lists/*

RUN useradd --system --create-home --uid 10001 cublyverse

COPY --from=build /out/cublyverse /usr/local/bin/cublyverse

USER cublyverse
WORKDIR /home/cublyverse

ENV HTTP_ADDR=:8080 \
    LOG_FORMAT=json

EXPOSE 8080

HEALTHCHECK --interval=10s --timeout=3s --start-period=10s --retries=5 \
    CMD wget --quiet --tries=1 --spider http://127.0.0.1:8080/readyz || exit 1

ENTRYPOINT ["cublyverse"]
CMD ["serve"]
