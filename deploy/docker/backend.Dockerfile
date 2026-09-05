# syntax=docker/dockerfile:1.7
FROM golang:1.26.0-alpine3.23 AS build
WORKDIR /src/backend
ARG GOPROXY=https://goproxy.cn,direct
COPY backend/go.mod backend/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod GOPROXY="$GOPROXY" go mod download
COPY backend/ ./
ARG TARGETOS=linux
ARG TARGETARCH
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    GOPROXY="$GOPROXY" CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH:-$(go env GOARCH)} \
    go build -trimpath -ldflags='-s -w' -o /out/server ./cmd/server && \
    GOPROXY="$GOPROXY" CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH:-$(go env GOARCH)} \
    go build -trimpath -ldflags='-s -w' -o /out/migrate ./cmd/migrate && \
    GOPROXY="$GOPROXY" CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH:-$(go env GOARCH)} \
    go build -trimpath -ldflags='-s -w' -o /out/search-reindex ./cmd/search-reindex && \
    GOPROXY="$GOPROXY" CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH:-$(go env GOARCH)} \
    go build -trimpath -ldflags='-s -w' -o /out/admin-role ./cmd/admin-role && \
    GOPROXY="$GOPROXY" CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH:-$(go env GOARCH)} \
    go build -trimpath -ldflags='-s -w' -o /out/business-worker ./cmd/business-worker && \
    GOPROXY="$GOPROXY" CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH:-$(go env GOARCH)} \
    go build -trimpath -ldflags='-s -w' -o /out/search-indexer ./cmd/search-indexer

FROM alpine:3.23.3 AS runtime
ARG VERSION
ARG REVISION
LABEL org.opencontainers.image.title="GoPulse Backend" \
      org.opencontainers.image.source="https://github.com/Ray-ymq/GoPulse" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${REVISION}"
RUN apk add --no-cache ca-certificates tzdata && \
    addgroup -g 10001 -S gopulse && \
    adduser -u 10001 -S -D -H -G gopulse gopulse
ENV TZ=UTC
WORKDIR /app
USER 10001:10001
STOPSIGNAL SIGTERM

FROM runtime AS backend
COPY --from=build --chown=10001:10001 /out/server /out/migrate /out/search-reindex /out/admin-role /usr/local/bin/
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/server"]

FROM runtime AS business-worker
LABEL org.opencontainers.image.title="GoPulse Business Worker"
COPY --from=build --chown=10001:10001 /out/business-worker /usr/local/bin/business-worker
ENTRYPOINT ["/usr/local/bin/business-worker"]

FROM runtime AS search-indexer
LABEL org.opencontainers.image.title="GoPulse Search Indexer"
COPY --from=build --chown=10001:10001 /out/search-indexer /usr/local/bin/search-indexer
ENTRYPOINT ["/usr/local/bin/search-indexer"]
