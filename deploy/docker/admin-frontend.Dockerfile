# syntax=docker/dockerfile:1.7
FROM node:24.20.0-alpine3.23 AS build
WORKDIR /src/admin-frontend
ENV CI=true
COPY admin-frontend/package.json admin-frontend/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci
COPY admin-frontend/ ./
RUN npm test -- --run && npm run build

FROM nginx:1.29.4-alpine3.23-slim AS runtime
ARG VERSION
ARG REVISION
LABEL org.opencontainers.image.title="GoPulse Admin Frontend" \
      org.opencontainers.image.source="https://github.com/Ray-ymq/GoPulse" \
      org.opencontainers.image.version="${VERSION}" \
      org.opencontainers.image.revision="${REVISION}"
COPY deploy/docker/admin-frontend/nginx.conf /etc/nginx/nginx.conf
COPY --from=build --chown=101:101 /src/admin-frontend/dist/ /usr/share/nginx/html/admin/
RUN find /usr/share/nginx/html -name '*.map' -delete && \
    chown -R 101:101 /usr/share/nginx/html
WORKDIR /usr/share/nginx/html
USER 101:101
EXPOSE 8080
STOPSIGNAL SIGQUIT
ENTRYPOINT ["nginx"]
CMD ["-g", "daemon off;"]
