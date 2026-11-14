FROM golang:1.26.9-alpine3.23 AS build
WORKDIR /src
ENV CGO_ENABLED=0 GOWORK=off GOTOOLCHAIN=local
COPY backend/business/go.mod backend/business/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY backend/business/ ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    set -eu; mkdir /out; \
    for name in server business-worker search-indexer migrate search-reindex admin-role; do \
      go build -trimpath -ldflags='-s -w' -o /out/$name ./cmd/$name; \
    done

FROM alpine:3.23.8
RUN apk add --no-cache ca-certificates curl && \
    addgroup -g 10001 app && adduser -D -H -u 10001 -G app app
ARG VERSION
LABEL org.opencontainers.image.title="GoPulse backend" \
      org.opencontainers.image.version=$VERSION
WORKDIR /app
COPY --from=build /out/ /app/
USER 10001:10001
CMD ["/app/server"]
