#!/bin/sh
# Hosts that run a single container (no Redis service beside it) get a
# memory-only Redis on localhost. Compose sets REDIS_URL, so it is skipped there.
# `exec` makes kortex PID 1 so SIGTERM reaches its graceful shutdown.
set -e
if [ -z "$REDIS_URL" ] && ! grep -qs '^REDIS_URL=' /app/.env; then
  redis-server --bind 127.0.0.1 --port 6379 --dir /tmp --save '' --appendonly no \
    --maxmemory 64mb --maxmemory-policy allkeys-lru --loglevel warning &
  export REDIS_URL=redis://127.0.0.1:6379
fi
exec /usr/local/bin/kortex "$@"
