FROM node:24.20.0-alpine3.23 AS build
WORKDIR /src/frontend/web
COPY frontend/web/package.json frontend/web/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci
COPY frontend/web/ ./
RUN npm run build

FROM nginx:1.30.6-alpine
ARG VERSION
LABEL org.opencontainers.image.title="GoPulse web" \
      org.opencontainers.image.version=$VERSION
RUN rm /etc/nginx/conf.d/default.conf
COPY deploy/nginx/default.conf.template /etc/nginx/templates/default.conf.template
COPY --from=build /src/frontend/web/dist/ /usr/share/nginx/html/
