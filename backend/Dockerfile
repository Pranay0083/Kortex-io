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
RUN apk add --no-cache ca-certificates \
    && addgroup -S kortex && adduser -S -G kortex -H -s /sbin/nologin kortex
COPY --from=build /out/kortex /usr/local/bin/kortex
COPY --from=frontend /app/build /app/static
ENV STATIC_DIR=/app/static \
    KORTEX_PORT=8090
USER kortex
EXPOSE 8090
HEALTHCHECK --interval=15s --timeout=3s --start-period=90s --retries=3 \
    CMD wget -q -O /dev/null "http://127.0.0.1:${KORTEX_PORT}/api/health" || exit 1
ENTRYPOINT ["/usr/local/bin/kortex"]
