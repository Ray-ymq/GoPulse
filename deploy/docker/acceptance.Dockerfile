# syntax=docker/dockerfile:1.7
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
RUN mkdir -p /work/frontend/test-results && chown -R 1000:1000 /work/frontend
USER 1000:1000
ENTRYPOINT ["npx", "playwright", "test"]
