"""Edge proxy for the Kortex Go service.

Supervisor owns uvicorn on :8001, so this ASGI app forwards every request to
the Go binary listening on 127.0.0.1:8090 (the real API). Redirects are passed
through untouched so `GET /api/r/{code}` still answers with a raw 301/302.
"""

import os
from contextlib import asynccontextmanager
from pathlib import Path

import httpx
from dotenv import load_dotenv
from fastapi import FastAPI, Request
from fastapi.responses import Response

load_dotenv(Path(__file__).parent / ".env")

UPSTREAM = os.getenv("KORTEX_UPSTREAM", "http://127.0.0.1:8090")

HOP_BY_HOP = {
    "connection", "keep-alive", "proxy-authenticate", "proxy-authorization",
    "te", "trailers", "transfer-encoding", "upgrade", "content-encoding",
    "content-length",
}


@asynccontextmanager
async def lifespan(app: FastAPI):
    app.state.client = httpx.AsyncClient(
        base_url=UPSTREAM,
        timeout=httpx.Timeout(60.0, connect=5.0),
        follow_redirects=False,
        limits=httpx.Limits(max_connections=200, max_keepalive_connections=100),
    )
    try:
        yield
    finally:
        await app.state.client.aclose()


app = FastAPI(title="kortex-edge", lifespan=lifespan)


@app.api_route("/{path:path}", methods=["GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "HEAD"])
async def proxy(path: str, request: Request):
    body = await request.body()
    headers = {k: v for k, v in request.headers.items() if k.lower() not in HOP_BY_HOP | {"host"}}
    try:
        upstream = await request.app.state.client.request(
            request.method,
            "/" + path,
            content=body,
            headers=headers,
            params=dict(request.query_params),
        )
    except (httpx.ConnectError, httpx.TimeoutException):
        return Response(
            content='{"detail":"kortex engine is starting up"}',
            status_code=503,
            media_type="application/json",
        )
    out_headers = {k: v for k, v in upstream.headers.items() if k.lower() not in HOP_BY_HOP}
    return Response(
        content=upstream.content,
        status_code=upstream.status_code,
        headers=out_headers,
        media_type=upstream.headers.get("content-type"),
    )
