# syntax=docker/dockerfile:1.7
FROM --platform=$BUILDPLATFORM golang:1.26.0-alpine3.23@sha256:d4c4845f5d60c6a974c6000ce58ae079328d03ab7f721a0734277e69905473e5 AS build
WORKDIR /src/lifecycle
COPY lifecycle/ ./
ARG TARGETOS
ARG TARGETARCH
ARG VERSION
ARG REVISION
RUN --mount=type=cache,target=/root/.cache/go-build CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -buildvcs=false -ldflags="-s -w -X main.version=$VERSION -X main.revision=$REVISION" -o /out/gopulse ./cmd/gopulse
FROM alpine:3.23.3@sha256:25109184c71bdad752c8312a8623239686a9a2071e8825f20acb8f2198c3f659
ARG VERSION
ARG REVISION
LABEL org.opencontainers.image.title="GoPulse Lifecycle" \
 org.opencontainers.image.source="https://github.com/Ray-ymq/GoPulse" \
 org.opencontainers.image.version="$VERSION" \
 org.opencontainers.image.revision="$REVISION"
COPY --from=build /out/gopulse /usr/local/bin/gopulse
USER 10001:10001
WORKDIR /bundle
ENTRYPOINT ["/usr/local/bin/gopulse"]
CMD ["version", "--json"]
