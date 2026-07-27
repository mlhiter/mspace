# syntax=docker/dockerfile:1.7

FROM node:22-bookworm AS node-runtime

FROM golang:1.24-bookworm AS builder

ARG BUILD_VERSION=dev
ARG BUILD_COMMIT_SHA=unknown
ARG BUILD_TIME=unknown

WORKDIR /src/worker

COPY worker/go.mod ./
RUN go mod download

COPY worker ./
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath \
  -ldflags="-s -w -X main.workerVersion=${BUILD_VERSION}" \
  -o /out/mspace-worker .

FROM golang:1.24-bookworm

ARG CODEX_CLI_VERSION=0.130.0
ARG KUBECTL_VERSION=v1.34.2
ARG BUILDKIT_VERSION=v0.24.0
ARG BUILD_VERSION=dev
ARG BUILD_COMMIT_SHA=unknown
ARG BUILD_TIME=unknown
ARG BUILD_SOURCE=https://github.com/mlhiter/mspace

LABEL org.opencontainers.image.version="${BUILD_VERSION}" \
  org.opencontainers.image.revision="${BUILD_COMMIT_SHA}" \
  org.opencontainers.image.created="${BUILD_TIME}" \
  org.opencontainers.image.source="${BUILD_SOURCE}"

COPY --from=node-runtime /usr/local /usr/local

RUN apt-get update \
  && apt-get install -y --no-install-recommends ca-certificates curl git openssh-client python3 make gcc g++ pkg-config unzip \
  && npm install -g "@openai/codex@${CODEX_CLI_VERSION}" \
  && curl -fsSLo /usr/local/bin/kubectl "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/amd64/kubectl" \
  && chmod +x /usr/local/bin/kubectl \
  && curl -fsSL "https://github.com/moby/buildkit/releases/download/${BUILDKIT_VERSION}/buildkit-${BUILDKIT_VERSION}.linux-amd64.tar.gz" \
    | tar -xz -C /usr/local/bin --strip-components=1 bin/buildctl \
  && groupadd --system --gid 10001 mspace \
  && useradd --system --uid 10001 --gid 10001 --home-dir /home/mspace --create-home mspace \
  && mkdir -p /var/lib/mspace-worker /var/lib/mspace-codex /etc/mspace/kubeconfigs \
  && chown -R mspace:mspace /var/lib/mspace-worker /var/lib/mspace-codex /home/mspace \
  && rm -rf /var/lib/apt/lists/* /root/.npm

COPY --from=builder /out/mspace-worker /usr/local/bin/mspace-worker

ENV CODEX_HOME=/var/lib/mspace-codex \
    MSPACE_WORKER_WORK_ROOT=/var/lib/mspace-worker \
    MSPACE_WORKER_MODE=team \
    MSPACE_WORKER_CAPABILITIES='{"protocolSmoke":true,"codex":true,"docker":true,"kubectl":true,"buildkit":false,"dryRun":false}'

USER 10001:10001

ENTRYPOINT ["/usr/local/bin/mspace-worker"]
