# syntax=docker/dockerfile:1
# Dockerfile for the reporterd standalone binary.
# Single module — the reporter does not depend on layer-monitor (the dispute
# monitor is intentionally not included). It DOES depend on the
# bridge-remote-signer/api module (mTLS + SignTx types), which is vendored into
# ./vendor-api in the build context and wired in via the relative replace in go.mod.

### Build stage
FROM golang:1.24-bookworm AS builder

WORKDIR /src

ENV GOTOOLCHAIN=auto

COPY . /src/

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -o /tmp/reporterd ./cmd

### Runtime stage
FROM debian:bookworm-slim

RUN apt-get update && apt-get install -y --no-install-recommends \
    ca-certificates \
    wget \
    && rm -rf /var/lib/apt/lists/*

WORKDIR /app
COPY --from=builder /tmp/reporterd /usr/local/bin/reporterd

ENTRYPOINT ["/usr/local/bin/reporterd"]
