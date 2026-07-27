# syntax=docker/dockerfile:1.7

FROM golang:1.24-bookworm AS builder

ARG BUILD_VERSION=dev
ARG BUILD_COMMIT_SHA=unknown
ARG BUILD_TIME=unknown

WORKDIR /src/server

COPY server/go.mod server/go.sum ./
RUN go mod download

COPY server ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath \
  -ldflags="-s -w -X github.com/mlhiter/mspace/server/internal/control.version=${BUILD_VERSION} -X github.com/mlhiter/mspace/server/internal/control.commitSHA=${BUILD_COMMIT_SHA} -X github.com/mlhiter/mspace/server/internal/control.buildTime=${BUILD_TIME}" \
  -o /out/mspace-server ./cmd/server

FROM debian:bookworm-slim

ARG BUILD_VERSION=dev
ARG BUILD_COMMIT_SHA=unknown
ARG BUILD_TIME=unknown
ARG BUILD_SOURCE=https://github.com/mlhiter/mspace

LABEL org.opencontainers.image.version="${BUILD_VERSION}" \
  org.opencontainers.image.revision="${BUILD_COMMIT_SHA}" \
  org.opencontainers.image.created="${BUILD_TIME}" \
  org.opencontainers.image.source="${BUILD_SOURCE}"

RUN apt-get update \
  && apt-get install -y --no-install-recommends ca-certificates \
  && groupadd --system --gid 10001 mspace \
  && useradd --system --uid 10001 --gid 10001 --home-dir /home/mspace --create-home mspace \
  && mkdir -p /etc/mspace/kubeconfigs \
  && chown -R mspace:mspace /home/mspace \
  && rm -rf /var/lib/apt/lists/*

COPY --from=builder /out/mspace-server /usr/local/bin/mspace-server

ENV MSPACE_SERVER_ADDR=0.0.0.0:8787

USER 10001:10001
EXPOSE 8787

ENTRYPOINT ["/usr/local/bin/mspace-server"]
