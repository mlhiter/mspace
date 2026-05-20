# syntax=docker/dockerfile:1.7

FROM golang:1.24-bookworm AS builder

WORKDIR /src/server

COPY server/go.mod server/go.sum ./
RUN go mod download

COPY server ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/mspace-server ./cmd/server

FROM debian:bookworm-slim

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
