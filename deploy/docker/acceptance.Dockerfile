# syntax=docker/dockerfile:1.7
FROM golang:1.26.0-alpine3.23 AS exporter-package
WORKDIR /src/exporter
ARG GOPROXY=https://goproxy.cn,direct
COPY exporters/redis/go.mod exporters/redis/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod GOPROXY="$GOPROXY" go mod download
COPY exporters/redis/ ./
ARG TARGETOS=linux
ARG TARGETARCH
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    GOPROXY="$GOPROXY" CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH:-$(go env GOARCH)} \
    go build -trimpath -buildvcs=false -ldflags='-s -w -buildid=' -o /out/gopulse-redis-exporter ./cmd/redis-exporter
RUN apk add --no-cache bash python3 tar gzip
COPY VERSION /src/VERSION
COPY scripts/package-redis-exporter.sh /src/scripts/package-redis-exporter.sh
ARG VERSION
ARG UPDATE_VERSION
RUN /src/scripts/package-redis-exporter.sh --version "$VERSION" --arch "${TARGETARCH:-$(go env GOARCH)}" \
      --binary /out/gopulse-redis-exporter --output /out/redis-exporter-install.tar.gz && \
    /src/scripts/package-redis-exporter.sh --version "$UPDATE_VERSION" --arch "${TARGETARCH:-$(go env GOARCH)}" \
      --binary /out/gopulse-redis-exporter --output /out/redis-exporter-update.tar.gz

FROM mcr.microsoft.com/playwright:v1.62.1-noble
ARG VERSION
ARG REVISION
LABEL org.opencontainers.image.title="GoPulse Compose Acceptance" \
      org.opencontainers.image.source="https://github.com/Ray-ymq/GoPulse" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${REVISION}"
ENV CI=true PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD=1
WORKDIR /work/frontend
COPY --chown=1000:1000 frontend/package.json frontend/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci
COPY --chown=1000:1000 frontend/playwright.config.ts ./
COPY --chown=1000:1000 frontend/e2e/ ./e2e/
COPY --from=exporter-package --chown=1000:1000 /out/redis-exporter-install.tar.gz /work/packages/redis-exporter-install.tar.gz
COPY --from=exporter-package --chown=1000:1000 /out/redis-exporter-update.tar.gz /work/packages/redis-exporter-update.tar.gz
RUN mkdir -p /work/frontend/test-results && chown -R 1000:1000 /work/frontend /work/packages
USER 1000:1000
ENTRYPOINT ["npx", "playwright", "test"]
