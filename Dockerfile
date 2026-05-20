# syntax=docker/dockerfile:1
# Dockerfile for reporterd binary.

### Build stage
FROM golang:1.23-bookworm AS builder

WORKDIR /src/layer-daemons

COPY go.mod go.sum ./

ENV GOTOOLCHAIN=auto

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go mod download

COPY . .

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -o /tmp/reporterd ./cmd

### Runtime stage
FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app

COPY --from=builder /tmp/reporterd /usr/local/bin/reporterd

ENTRYPOINT ["/usr/local/bin/reporterd"]
