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

FROM ${GO_IMAGE} AS mysql-exporter-build
WORKDIR /src/mysql
ARG GOPROXY=https://goproxy.cn,direct
COPY exporters/mysql/ ./
ARG TARGETARCH
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    GOPROXY="$GOPROXY" CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH:-$(go env GOARCH)} \
    go build -trimpath -buildvcs=false -ldflags='-s -w -buildid=' -o /out/gopulse-mysql-exporter ./cmd/mysql-exporter

FROM ${GO_IMAGE} AS rabbitmq-exporter-build
WORKDIR /src/rabbitmq
ARG GOPROXY=https://goproxy.cn,direct
COPY exporters/rabbitmq/ ./
ARG TARGETARCH
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    GOPROXY="$GOPROXY" CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH:-$(go env GOARCH)} \
    go build -trimpath -buildvcs=false -ldflags='-s -w -buildid=' -o /out/gopulse-rabbitmq-exporter ./cmd/rabbitmq-exporter

FROM ${GO_IMAGE} AS exporter-package
RUN apk add --no-cache bash python3 tar gzip
WORKDIR /src
COPY VERSION ./VERSION
COPY scripts/package-redis-exporter.sh ./scripts/package-redis-exporter.sh
COPY monitor/ ./monitor/
COPY --from=exporter-build /out/gopulse-redis-exporter /out/gopulse-redis-exporter
ARG VERSION
ARG TARGETARCH
RUN ./scripts/package-redis-exporter.sh --contract-version 2 --version "$VERSION" --arch "${TARGETARCH:-$(go env GOARCH)}" \
    --binary /out/gopulse-redis-exporter --output /out/gopulse-redis-exporter.tar.gz

COPY --from=mysql-exporter-build /out/gopulse-mysql-exporter /out/gopulse-mysql-exporter
RUN ./scripts/package-redis-exporter.sh --source mysql --version "$VERSION" --arch "${TARGETARCH:-$(go env GOARCH)}" --binary /out/gopulse-mysql-exporter --output /out/gopulse-mysql-exporter.tar.gz
COPY --from=rabbitmq-exporter-build /out/gopulse-rabbitmq-exporter /out/gopulse-rabbitmq-exporter
RUN ./scripts/package-redis-exporter.sh --source rabbitmq --version "$VERSION" --arch "${TARGETARCH:-$(go env GOARCH)}" --binary /out/gopulse-rabbitmq-exporter --output /out/gopulse-rabbitmq-exporter.tar.gz

FROM ${GO_IMAGE} AS legacy-exporter-build
WORKDIR /legacy
COPY deploy/plugins/redis-1.10.6-source.tar.gz /tmp/source.tar.gz
RUN tar -xzf /tmp/source.tar.gz -C /legacy
WORKDIR /legacy/exporters/redis
ARG GOPROXY=https://goproxy.cn,direct
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    GOPROXY="$GOPROXY" CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -buildvcs=false -ldflags='-s -w -buildid=' -o /out/legacy-exporter ./cmd/redis-exporter

FROM exporter-package AS official-packages
COPY --from=legacy-exporter-build /out/legacy-exporter /out/legacy-exporter
RUN ./scripts/package-redis-exporter.sh --contract-version 1 --version 1.10.6 --arch amd64 --binary /out/legacy-exporter --output /out/redis-1.10.6.tar.gz && \
    echo 'b992b0dfa80a0983b9af63e4c2a4770216bfd7fcb718af2cd451281cf3306727  /out/redis-1.10.6.tar.gz' | sha256sum -c -

FROM ${GO_IMAGE} AS monitor-build
WORKDIR /src/monitor
ARG GOPROXY=https://goproxy.cn,direct
COPY monitor/go.mod monitor/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod GOPROXY="$GOPROXY" go mod download
COPY monitor/ ./
COPY --from=official-packages /out/gopulse-redis-exporter.tar.gz /packages/gopulse-redis-exporter.tar.gz
COPY --from=official-packages /out/redis-1.10.6.tar.gz /packages/redis-1.10.6.tar.gz
COPY --from=official-packages /out/gopulse-mysql-exporter.tar.gz /out/gopulse-rabbitmq-exporter.tar.gz /packages/
RUN go run ./cmd/plugin-release-catalog --output internal/plugin/release_catalog_generated.go current=/packages/gopulse-redis-exporter.tar.gz current=/packages/gopulse-mysql-exporter.tar.gz current=/packages/gopulse-rabbitmq-exporter.tar.gz legacy-v1=/packages/redis-1.10.6.tar.gz
ARG TARGETOS=linux
ARG TARGETARCH
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    GOPROXY="$GOPROXY" CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH:-$(go env GOARCH)} \
    go build -trimpath -ldflags='-s -w' -o /out/monitor ./cmd/monitor

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
COPY --from=official-packages /out/gopulse-redis-exporter.tar.gz /opt/gopulse/packages/gopulse-redis-exporter.tar.gz
COPY --from=official-packages /out/redis-1.10.6.tar.gz /opt/gopulse/packages/redis-1.10.6.tar.gz
COPY --from=official-packages /out/gopulse-mysql-exporter.tar.gz /out/gopulse-rabbitmq-exporter.tar.gz /opt/gopulse/packages/
USER 10005:10001
EXPOSE 9090
VOLUME ["/var/lib/gopulse-monitor/plugins"]
ENTRYPOINT ["/usr/local/bin/monitor"]

# Acceptance-only registered releases. No runtime flag can add these to a
# production image; the production monitor target above never copies them.
FROM official-packages AS acceptance-packages
RUN cd /src/monitor && CGO_ENABLED=0 go build -trimpath -buildvcs=false -ldflags='-buildid=' -o /out/failing-exporter ./internal/plugin/testdata/failing-exporter.go
RUN ./scripts/package-redis-exporter.sh --contract-version 2 --version 1.11.90 --arch amd64 --binary /out/failing-exporter --output /out/redis-failure.tar.gz && \
    ./scripts/package-redis-exporter.sh --contract-version 2 --version 1.11.3 --arch amd64 --binary /out/gopulse-redis-exporter --output /out/redis-update.tar.gz

FROM monitor-build AS monitor-acceptance-build
COPY --from=acceptance-packages /out/redis-failure.tar.gz /out/redis-update.tar.gz /packages/
RUN go run ./cmd/plugin-release-catalog --output internal/plugin/release_catalog_generated.go \
      current=/packages/gopulse-redis-exporter.tar.gz current=/packages/gopulse-mysql-exporter.tar.gz current=/packages/gopulse-rabbitmq-exporter.tar.gz legacy-v1=/packages/redis-1.10.6.tar.gz \
      retained=/packages/redis-failure.tar.gz retained=/packages/redis-update.tar.gz && \
    CGO_ENABLED=0 go build -trimpath -buildvcs=false -ldflags='-s -w' -o /out/monitor ./cmd/monitor

FROM monitor AS monitor-acceptance
COPY --from=monitor-acceptance-build /out/monitor /usr/local/bin/monitor
COPY --from=acceptance-packages /out/redis-failure.tar.gz /out/redis-update.tar.gz /opt/gopulse/packages/
