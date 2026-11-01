# syntax=docker/dockerfile:1
# ── Stage 1: Build frontend ─────────────────────────────
FROM node:22-alpine AS frontend
WORKDIR /app
COPY frontend/package.json frontend/yarn.lock ./
RUN yarn install --frozen-lockfile
COPY frontend/ .
ARG REACT_APP_BACKEND_URL=
ENV REACT_APP_BACKEND_URL=${REACT_APP_BACKEND_URL}
RUN yarn build

# ── Stage 2: Build Go backend ───────────────────────────
FROM golang:1.24-alpine AS build
WORKDIR /src
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ .
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/kortex .

# ── Stage 3: Final runtime ──────────────────────────────
FROM alpine:3.21
# redis backs the single-container mode in docker-entrypoint.sh
RUN apk add --no-cache ca-certificates redis \
    && addgroup -S kortex && adduser -S -G kortex -H -s /sbin/nologin kortex
WORKDIR /app
COPY --from=build /out/kortex /usr/local/bin/kortex
COPY --from=frontend /app/build /app/static
COPY --chmod=0755 backend/docker-entrypoint.sh /usr/local/bin/docker-entrypoint.sh
# Hosts that cannot inject env vars get /app/.env (main loads ./.env) from a
# build secret; real env vars still win.
RUN --mount=type=secret,id=kortex_env,required=false \
    if [ -f /run/secrets/kortex_env ]; then \
      cp /run/secrets/kortex_env /app/.env && chown kortex:kortex /app/.env && chmod 0400 /app/.env; \
    fi
ENV STATIC_DIR=/app/static \
    KORTEX_PORT=8090
USER kortex
EXPOSE 8090
HEALTHCHECK --interval=15s --timeout=3s --start-period=90s --retries=3 \
    CMD wget -q -O /dev/null "http://127.0.0.1:${KORTEX_PORT}/api/health" || exit 1
ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]
