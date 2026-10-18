# syntax=docker/dockerfile:1.7
ARG GO_IMAGE=golang:1.26.0-alpine3.23@sha256:d4c4845f5d60c6a974c6000ce58ae079328d03ab7f721a0734277e69905473e5
ARG RUNTIME_IMAGE=alpine:3.23.3@sha256:25109184c71bdad752c8312a8623239686a9a2071e8825f20acb8f2198c3f659

FROM --platform=$BUILDPLATFORM ${GO_IMAGE} AS marshaller-build
WORKDIR /src/marshaller
ARG GOPROXY=https://goproxy.cn,direct
COPY marshaller/go.mod marshaller/go.sum ./
COPY componentmetrics/ /src/componentmetrics/
RUN --mount=type=cache,target=/go/pkg/mod GOPROXY="$GOPROXY" go mod download
COPY marshaller/ ./
ARG TARGETOS=linux
ARG TARGETARCH
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    GOPROXY="$GOPROXY" CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH:-$(go env GOARCH)} \
    go build -trimpath -ldflags='-s -w' -o /out/marshaller ./cmd/marshaller

FROM --platform=$BUILDPLATFORM ${GO_IMAGE} AS exporter-build
WORKDIR /src/exporters/redis
ARG GOPROXY=https://goproxy.cn,direct
COPY exporters/redis/go.mod exporters/redis/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod GOPROXY="$GOPROXY" go mod download
COPY exporters/redis/ ./
ARG TARGETOS=linux
ARG TARGETARCH
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    GOPROXY="$GOPROXY" CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH:-$(go env GOARCH)} \
    go build -trimpath -buildvcs=false -ldflags='-s -w -buildid=' -o /out/gopulse-redis-exporter ./cmd/redis-exporter

FROM --platform=$BUILDPLATFORM ${GO_IMAGE} AS mysql-exporter-build
WORKDIR /src/exporters/mysql
ARG GOPROXY=https://goproxy.cn,direct
COPY exporters/mysql/ ./
ARG TARGETARCH
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    GOPROXY="$GOPROXY" CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH:-$(go env GOARCH)} \
    go build -trimpath -buildvcs=false -ldflags='-s -w -buildid=' -o /out/gopulse-mysql-exporter ./cmd/mysql-exporter

FROM --platform=$BUILDPLATFORM ${GO_IMAGE} AS rabbitmq-exporter-build
WORKDIR /src/exporters/rabbitmq
ARG GOPROXY=https://goproxy.cn,direct
COPY exporters/rabbitmq/ ./
ARG TARGETARCH
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    GOPROXY="$GOPROXY" CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH:-$(go env GOARCH)} \
    go build -trimpath -buildvcs=false -ldflags='-s -w -buildid=' -o /out/gopulse-rabbitmq-exporter ./cmd/rabbitmq-exporter

FROM --platform=$BUILDPLATFORM ${GO_IMAGE} AS kafka-exporter-build
WORKDIR /src/exporters/kafka
ARG GOPROXY=https://goproxy.cn,direct
COPY exporters/kafka/ ./
ARG TARGETARCH
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    GOPROXY="$GOPROXY" CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH:-$(go env GOARCH)} \
    go build -trimpath -buildvcs=false -ldflags='-s -w -buildid=' -o /out/gopulse-kafka-exporter ./cmd/kafka-exporter

FROM --platform=$BUILDPLATFORM ${GO_IMAGE} AS elasticsearch-exporter-build
WORKDIR /src/exporters/elasticsearch
ARG GOPROXY=https://goproxy.cn,direct
COPY exporters/elasticsearch/ ./
ARG TARGETARCH
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    GOPROXY="$GOPROXY" CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH:-$(go env GOARCH)} \
    go build -trimpath -buildvcs=false -ldflags='-s -w -buildid=' -o /out/gopulse-elasticsearch-exporter ./cmd/elasticsearch-exporter

FROM --platform=$BUILDPLATFORM ${GO_IMAGE} AS victoriametrics-exporter-build
WORKDIR /src/exporters/victoriametrics
ARG GOPROXY=https://goproxy.cn,direct
COPY exporters/victoriametrics/ ./
ARG TARGETARCH
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    GOPROXY="$GOPROXY" CGO_ENABLED=0 GOOS=linux GOARCH=${TARGETARCH:-$(go env GOARCH)} \
    go build -trimpath -buildvcs=false -ldflags='-s -w -buildid=' -o /out/gopulse-victoriametrics-exporter ./cmd/victoriametrics-exporter

FROM --platform=$BUILDPLATFORM ${GO_IMAGE} AS exporter-package
RUN apk add --no-cache tar gzip
WORKDIR /src
COPY devtools/go.mod devtools/go.sum /src/devtools/
COPY devtools/cmd/exporter-package/ /src/devtools/cmd/exporter-package/
COPY devtools/cmd/exporter-catalog/ /src/devtools/cmd/exporter-catalog/
COPY exporters/redis/descriptor.json /src/exporters/redis/descriptor.json
COPY exporters/redis/config.schema.json /src/exporters/redis/config.schema.json
COPY exporters/redis/VERSION /src/exporters/redis/VERSION
COPY exporters/mysql/descriptor.json /src/exporters/mysql/descriptor.json
COPY exporters/mysql/config.schema.json /src/exporters/mysql/config.schema.json
COPY exporters/mysql/VERSION /src/exporters/mysql/VERSION
COPY exporters/rabbitmq/descriptor.json /src/exporters/rabbitmq/descriptor.json
COPY exporters/rabbitmq/config.schema.json /src/exporters/rabbitmq/config.schema.json
COPY exporters/rabbitmq/VERSION /src/exporters/rabbitmq/VERSION
COPY exporters/kafka/descriptor.json /src/exporters/kafka/descriptor.json
COPY exporters/kafka/config.schema.json /src/exporters/kafka/config.schema.json
COPY exporters/kafka/VERSION /src/exporters/kafka/VERSION
COPY exporters/elasticsearch/descriptor.json /src/exporters/elasticsearch/descriptor.json
COPY exporters/elasticsearch/config.schema.json /src/exporters/elasticsearch/config.schema.json
COPY exporters/elasticsearch/VERSION /src/exporters/elasticsearch/VERSION
COPY exporters/victoriametrics/descriptor.json /src/exporters/victoriametrics/descriptor.json
COPY exporters/victoriametrics/config.schema.json /src/exporters/victoriametrics/config.schema.json
COPY exporters/victoriametrics/VERSION /src/exporters/victoriametrics/VERSION
COPY --from=exporter-build /out/gopulse-redis-exporter /out/gopulse-redis-exporter
ARG GOPROXY=https://goproxy.cn,direct
ENV GOPROXY=${GOPROXY}
ARG VERSION
ARG TARGETARCH
WORKDIR /src/devtools
RUN go run ./cmd/exporter-package --repo-root /src --source redis --version "$(cat /src/exporters/redis/VERSION)" --arch "${TARGETARCH:-$(go env GOARCH)}" \
    --binary /out/gopulse-redis-exporter --output /out/gopulse-redis-exporter.tar.gz

COPY --from=mysql-exporter-build /out/gopulse-mysql-exporter /out/gopulse-mysql-exporter
RUN go run ./cmd/exporter-package --repo-root /src --source mysql --version "$(cat /src/exporters/mysql/VERSION)" --arch "${TARGETARCH:-$(go env GOARCH)}" --binary /out/gopulse-mysql-exporter --output /out/gopulse-mysql-exporter.tar.gz
COPY --from=rabbitmq-exporter-build /out/gopulse-rabbitmq-exporter /out/gopulse-rabbitmq-exporter
RUN go run ./cmd/exporter-package --repo-root /src --source rabbitmq --version "$(cat /src/exporters/rabbitmq/VERSION)" --arch "${TARGETARCH:-$(go env GOARCH)}" --binary /out/gopulse-rabbitmq-exporter --output /out/gopulse-rabbitmq-exporter.tar.gz

COPY --from=kafka-exporter-build /out/gopulse-kafka-exporter /out/gopulse-kafka-exporter
RUN go run ./cmd/exporter-package --repo-root /src --source kafka --version "$(cat /src/exporters/kafka/VERSION)" --arch "${TARGETARCH:-$(go env GOARCH)}" --binary /out/gopulse-kafka-exporter --output /out/gopulse-kafka-exporter.tar.gz
COPY --from=elasticsearch-exporter-build /out/gopulse-elasticsearch-exporter /out/gopulse-elasticsearch-exporter
RUN go run ./cmd/exporter-package --repo-root /src --source elasticsearch --version "$(cat /src/exporters/elasticsearch/VERSION)" --arch "${TARGETARCH:-$(go env GOARCH)}" --binary /out/gopulse-elasticsearch-exporter --output /out/gopulse-elasticsearch-exporter.tar.gz
COPY --from=victoriametrics-exporter-build /out/gopulse-victoriametrics-exporter /out/gopulse-victoriametrics-exporter
RUN go run ./cmd/exporter-package --repo-root /src --source victoriametrics --version "$(cat /src/exporters/victoriametrics/VERSION)" --arch "${TARGETARCH:-$(go env GOARCH)}" --binary /out/gopulse-victoriametrics-exporter --output /out/gopulse-victoriametrics-exporter.tar.gz

FROM --platform=$BUILDPLATFORM ${GO_IMAGE} AS monitor-build
WORKDIR /src/monitor
ARG GOPROXY=https://goproxy.cn,direct
COPY monitor/go.mod monitor/go.sum ./
COPY componentmetrics/ /src/componentmetrics/
RUN --mount=type=cache,target=/go/pkg/mod GOPROXY="$GOPROXY" go mod download
COPY monitor/ ./

ARG TARGETOS=linux
ARG TARGETARCH
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    GOPROXY="$GOPROXY" CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH:-$(go env GOARCH)} \
    go build -trimpath -ldflags='-s -w' -o /out/monitor ./cmd/monitor

FROM ${RUNTIME_IMAGE} AS runtime
RUN apk add --no-cache ca-certificates tzdata && \
    addgroup -g 10001 -S gopulse && \
    adduser -u 10003 -S -D -H -G gopulse marshaller && \
    adduser -u 10004 -S -D -H -G gopulse exporter && \
    adduser -u 10005 -S -D -H -G gopulse monitor && \
    mkdir -p /app /var/lib/gopulse-monitor/plugins /var/lib/gopulse-monitor/spool /opt/gopulse/packages && \
    chown 10005:10001 /var/lib/gopulse-monitor/plugins
RUN chown 10005:10001 /var/lib/gopulse-monitor/spool
ENV TZ=UTC
WORKDIR /app
STOPSIGNAL SIGTERM
ARG VERSION
ARG REVISION
LABEL org.opencontainers.image.source="https://github.com/Ray-ymq/GoPulse" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${REVISION}"

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
USER 10005:10001
EXPOSE 9090
VOLUME ["/var/lib/gopulse-monitor/plugins", "/var/lib/gopulse-monitor/spool"]
ENTRYPOINT ["/usr/local/bin/monitor"]
