# syntax=docker/dockerfile:1.7
ARG GO_IMAGE=golang:1.26.0-alpine3.23
ARG RUNTIME_IMAGE=alpine:3.23.3

FROM ${GO_IMAGE} AS router-build
WORKDIR /src/router
ARG GOPROXY=https://goproxy.cn,direct
COPY router/go.mod router/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod GOPROXY="$GOPROXY" go mod download
COPY router/ ./
ARG TARGETOS=linux
ARG TARGETARCH
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    GOPROXY="$GOPROXY" CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH:-$(go env GOARCH)} \
    go build -trimpath -ldflags='-s -w' -o /out/router ./cmd/router

FROM ${GO_IMAGE} AS marshaller-build
WORKDIR /src/marshaller
ARG GOPROXY=https://goproxy.cn,direct
COPY marshaller/go.mod marshaller/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod GOPROXY="$GOPROXY" go mod download
COPY marshaller/ ./
ARG TARGETOS=linux
ARG TARGETARCH
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    GOPROXY="$GOPROXY" CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH:-$(go env GOARCH)} \
    go build -trimpath -ldflags='-s -w' -o /out/marshaller ./cmd/marshaller

FROM ${GO_IMAGE} AS monitor-build
WORKDIR /src/monitor
ARG GOPROXY=https://goproxy.cn,direct
COPY monitor/go.mod monitor/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod GOPROXY="$GOPROXY" go mod download
COPY monitor/ ./
ARG TARGETOS=linux
ARG TARGETARCH
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    GOPROXY="$GOPROXY" CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH:-$(go env GOARCH)} \
    go build -trimpath -ldflags='-s -w' -o /out/monitor ./cmd/monitor

FROM ${GO_IMAGE} AS exporter-build
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

FROM ${GO_IMAGE} AS exporter-package
RUN apk add --no-cache bash python3 tar gzip
WORKDIR /src
COPY VERSION ./VERSION
COPY scripts/package-redis-exporter.sh ./scripts/package-redis-exporter.sh
COPY --from=exporter-build /out/gopulse-redis-exporter /out/gopulse-redis-exporter
ARG VERSION
ARG TARGETARCH
RUN ./scripts/package-redis-exporter.sh --version "$VERSION" --arch "${TARGETARCH:-$(go env GOARCH)}" \
    --binary /out/gopulse-redis-exporter --output /out/gopulse-redis-exporter.tar.gz

FROM ${RUNTIME_IMAGE} AS runtime
ARG VERSION
ARG REVISION
RUN apk add --no-cache ca-certificates tzdata && \
    addgroup -g 10001 -S gopulse && \
    adduser -u 10002 -S -D -H -G gopulse router && \
    adduser -u 10003 -S -D -H -G gopulse marshaller && \
    adduser -u 10004 -S -D -H -G gopulse exporter && \
    adduser -u 10005 -S -D -H -G gopulse monitor && \
    mkdir -p /app /var/lib/gopulse-monitor/plugins /opt/gopulse/packages && \
    chown 10005:10001 /var/lib/gopulse-monitor/plugins
ENV TZ=UTC
WORKDIR /app
STOPSIGNAL SIGTERM
LABEL org.opencontainers.image.source="https://github.com/Ray-ymq/GoPulse" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${REVISION}"

FROM runtime AS router
LABEL org.opencontainers.image.title="GoPulse Message Router"
COPY --from=router-build --chown=10002:10001 /out/router /usr/local/bin/router
USER 10002:10001
EXPOSE 9091
ENTRYPOINT ["/usr/local/bin/router"]

FROM runtime AS marshaller
LABEL org.opencontainers.image.title="GoPulse Marshaller"
COPY --from=marshaller-build --chown=10003:10001 /out/marshaller /usr/local/bin/marshaller
USER 10003:10001
EXPOSE 9093
ENTRYPOINT ["/usr/local/bin/marshaller"]

FROM runtime AS redis-exporter
LABEL org.opencontainers.image.title="GoPulse Redis Exporter"
COPY --from=exporter-build --chown=10004:10001 /out/gopulse-redis-exporter /usr/local/bin/gopulse-redis-exporter
USER 10004:10001
EXPOSE 9121
ENTRYPOINT ["/usr/local/bin/gopulse-redis-exporter"]

FROM runtime AS monitor
LABEL org.opencontainers.image.title="GoPulse Monitor"
COPY --from=monitor-build --chown=10005:10001 /out/monitor /usr/local/bin/monitor
COPY --from=exporter-package --chown=10005:10001 /out/gopulse-redis-exporter.tar.gz /opt/gopulse/packages/gopulse-redis-exporter.tar.gz
USER 10005:10001
EXPOSE 9090
VOLUME ["/var/lib/gopulse-monitor/plugins"]
ENTRYPOINT ["/usr/local/bin/monitor"]
