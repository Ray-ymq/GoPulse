# syntax=docker/dockerfile:1.7
FROM --platform=$BUILDPLATFORM node:24.20.0-alpine3.23@sha256:0388af2af070cd4736a1567cfed02469ba117848845b4165d87a333edb53d2ca AS build
WORKDIR /src/frontend
ENV CI=true
COPY frontend/package.json frontend/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci
COPY frontend/ ./
RUN npm test -- --run && npm run build

FROM nginx:1.29.4-alpine3.23-slim@sha256:441b69e13e79b436f9b617910633b6b6adce314c3788c3238dcd8e03b4cb512e AS runtime
ARG VERSION
ARG REVISION
LABEL org.opencontainers.image.title="GoPulse Frontend" \
      org.opencontainers.image.source="https://github.com/Ray-ymq/GoPulse" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${REVISION}"
COPY deploy/docker/frontend/nginx.conf /etc/nginx/nginx.conf
COPY --from=build --chown=101:101 /src/frontend/dist/ /usr/share/nginx/html/
RUN find /usr/share/nginx/html -name '*.map' -delete && \
    chown -R 101:101 /usr/share/nginx/html
WORKDIR /usr/share/nginx/html
USER 101:101
EXPOSE 8080
STOPSIGNAL SIGQUIT
ENTRYPOINT ["nginx"]
CMD ["-g", "daemon off;"]
