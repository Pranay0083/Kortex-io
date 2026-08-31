# Kortex frontend

React 19 console for Kortex — the URL shortener + real-time click analytics service. It covers
the landing page and the authenticated console: overview, links, per-link analytics,
infrastructure, and the admin-only benchmark.

Built with Vite, Tailwind CSS 3, Recharts, React Router 7 and sonner. In production the Go
server serves the contents of `build/` (static assets + SPA fallback) and answers `/{code}`
redirects itself.

## Scripts

```sh
yarn install
yarn dev       # http://localhost:3000, proxies /api to http://localhost:8090
yarn build     # production bundle in ./build
yarn preview   # serve ./build locally (same /api proxy)
yarn test      # vitest, single run (CI)
yarn lint      # eslint, zero warnings allowed
yarn format    # prettier
```

`yarn dev` expects the Go backend on `:8090` (e.g. `docker compose up postgres redis` and run the
server, or the full compose stack). Point the proxy elsewhere with
`KORTEX_DEV_BACKEND=http://host:port yarn dev`.

## Configuration

| Variable                | Used at    | Meaning                                                                                                                                     |
| ----------------------- | ---------- | ------------------------------------------------------------------------------------------------------------------------------------------- |
| `REACT_APP_BACKEND_URL` | build time | Absolute origin of the Kortex server, e.g. `https://kortex.example.com`. Leave empty when the SPA is served by the Go server (same origin). |

Short URLs, copy buttons and QR codes are always built as absolute URLs from that origin (or
`window.location.origin` when it is empty).

## Auth

Browser auth is cookie-only: the server sets httpOnly cookies on login/register/refresh and the
client never sees a token. Requests go out with `withCredentials`; a 401 triggers one shared
`POST /api/auth/refresh` and the request is retried, otherwise the user is sent back to
`/login`.
