# provenance-collector-frontend

The web dashboard of provenance-collector-pack: a React 19 + Vite 7 + TypeScript (strict) +
Tailwind v4 SPA built on the [Nebari design system](https://github.com/nebari-dev/nebari-design),
served by an unprivileged nginx that also proxies `/api/` to the Go dashboard
(`cmd/dashboard`). User-facing docs: [Web Dashboard](../docs/src/content/docs/web-dashboard.md).

The same bundle is the UI of the
[Nebari Security Posture pack](https://github.com/nebari-dev/nebari-security-posture-pack). It
decides at startup which backend it is talking to (see [Modes](#modes)); in this pack it always
runs in **provenance** mode.

## Container contract (for the chart)

| Item | Value |
|---|---|
| Base image | `nginxinc/nginx-unprivileged:1.27-alpine` |
| User | uid/gid **101**; works with `runAsNonRoot`, `readOnlyRootFilesystem: true`, `capabilities.drop: [ALL]` |
| Listen port | **8080** (`NGINX_PORT`) |
| Writable paths | `/tmp` (pid, temp dirs, rendered server block in `/tmp/nginx/conf.d`) and `/var/cache/nginx`; mount emptyDirs there |
| `API_UPSTREAM` | `host:port` of the dashboard, scheme optional. Default `provenance-collector-web:8080`; the chart sets `<fullname>-web:<webUI.port>` |
| `/healthz` | static `200 ok` |
| `/config.json` | runtime config; the chart's ConfigMap is mounted over `/usr/share/nginx/html/config.json`. Served `no-store` |
| Caching | `index.html` / SPA routes `no-cache`; `/assets/*` (content-hashed) `max-age=1y, immutable`; gzip on |

`/etc/nginx/templates/default.conf.template` is rendered by the official image's envsubst
entrypoint into `/tmp/nginx/conf.d`, and `nginx.conf` includes it from there.
`docker/05-security-posture.envsh` runs first: it strips any scheme from `API_UPSTREAM` and, when
`/etc/resolv.conf` carries a Kubernetes `<ns>.svc.<domain>` search domain, qualifies a bare service
name with it. The proxy uses a variable upstream + `resolver`, so nginx starts before the dashboard
Service resolves and follows Service IP changes.

`location /api/` passes the URI and every request header through unchanged, including
`Authorization` (the keycloak-js bearer) and `Sec-Fetch-Site` (the dashboard's `POST /api/scan` CSRF
guard requires `same-origin`), with `proxy_read_timeout 120s` and no buffering.

```sh
docker build -t provenance-collector-frontend:dev frontend/
docker run --rm -p 8080:8080 --user 101:101 --read-only \
  --tmpfs /tmp:uid=101,gid=101 --tmpfs /var/cache/nginx:uid=101,gid=101 --cap-drop ALL \
  -e API_UPSTREAM=host.docker.internal:8080 provenance-collector-frontend:dev
curl localhost:8080/healthz
```

## Runtime config (`/config.json`)

Unknown keys are ignored; malformed values fall back to the defaults.

| Key | Default | Used for |
|---|---|---|
| `mode` | `auto` | `provenance` \| `posture` \| `auto` (probe, see below). The chart sets `provenance`. |
| `provenanceApiBase` | `/api` | Go dashboard base (`/reports`, `/me`, `/scan`, `/export`) |
| `keycloak.url`, `.realm`, `.clientId` | unset | PKCE login; all three required, otherwise no login and no bearer |
| `title` | "Supply-chain provenance" | header, sidebar, tab title |
| `logoUrl`, `logoUrlDark`, `faviconUrl`, `theme.{light,dark}` | unset | branding (`src/branding.ts`) |
| `apiBase` | `/api/v1` | Security Posture API base; also the `auto` probe |

## Modes

`src/capabilities.ts`. With `"mode": "auto"` the SPA probes `GET {apiBase}/summary`
(`/api/v1/summary`): any 2xx, 401, 403 or 5xx means the Security Posture API is there (`posture`);
a 404, an HTML 200 (a static SPA fallback) or a network error means `provenance`. `useCapabilities()`
returns `{mode, features}`; the sidebar, routes and pages read it.

In **provenance** mode only Overview, Images (+ detail), Supply chain, Reports and Scans exist.
The posture-only pages (Vulnerabilities, Workloads, Namespaces, Posture checks, Compliance,
Settings, scan detail) are compiled in but hidden, and their routes redirect to `/`. Their code
paths are exercised only by the posture-mode unit tests and the `chromium` Playwright project.

**Report adapter** (`src/api/provenance-adapter.ts`) maps the collector report
(`src/api/provenance-report.ts`, written from `schema/report.schema.json` 1.x) onto the UI's
image / supply-chain / Helm / namespace shapes. `src/api/provenance-adapter.test.ts` walks the
vendored schema (`src/mocks/fixtures/report.schema.json`) and fails if a field has no mapping.

- Images are grouped by reference (the collector's `uniqueImages` key); signed / SBOM / provenance /
  update counts are recomputed per unique image.
- The score comes from `src/lib/supply-chain.ts`; the cluster value is the mean of the image scores
  weighted by container count.
- The report omits `sbom`, `provenance` and `update` both when nothing was found and when the check
  is off. A check counts as on if any record in the report has the object; a missing object on an
  image with a resolved digest is then a negative. Images without a digest stay "not checked".
- `schemaVersion` and `warnings` are optional (reports from collectors that predate them render).

**Auth** (`src/auth/`). With a complete `keycloak` block, keycloak-js logs in before the first
render (`login-required`, PKCE `S256`, no session iframe). Every request carries
`Authorization: Bearer`; the token is refreshed when less than 30 s remain; after a 401 the client
forces a refresh and retries once, and a second 401 shows Session expired. Sign out calls Keycloak
logout. `src/api/client.ts` is the only fetch layer and takes its headers from the strategy in
`src/auth/strategy.ts`.

**Scans.** The dashboard has no job-status endpoint, so after `POST /api/scan` the UI polls
`/api/reports` every 5 s for up to 5 min and adopts the first newer report. Run scan shows only when
`/api/me` returns `canRunScan`; 403 / 409 / 503 show a toast.

## Development (Docker only, no host node needed)

```sh
cd frontend
alias dnode='docker run --rm -it -u "$(id -u):$(id -g)" -v "$PWD":/app -w /app -e HOME=/tmp -p 5173:5173 node:22-alpine'
dnode npm ci
dnode npm run dev:mock-provenance   # http://localhost:5173, MSW mock of the Go dashboard, no backend
dnode npm run dev                   # proxies /api to $API_PROXY (add -e API_PROXY=http://<dashboard>:8080 to the alias)
dnode npm run lint                  # eslint
dnode npm run typecheck             # tsc -b --noEmit
dnode npm run test:coverage         # vitest + coverage gate (thresholds in vitest.config.ts)
dnode npm run build                 # dist/
```

`.npmrc` sets `legacy-peer-deps=true` (npm 10 crashes resolving the optional peer graph of
vitest/msw otherwise).

**Mock mode.** `VITE_API_MOCK=provenance` starts an MSW copy of the Go dashboard
(`src/mocks/provenance-backend.ts`) serving the golden report (`src/mocks/fixtures/report.golden.json`)
plus two older runs, answering 404 for `/api/v1/*`, with a stand-in keycloak-js session. A manual
scan finishes after about 8 s. Add `?mockAuth=viewer` (no `canRunScan`), `?mockAuth=401` (rejected
token) or `?mockAuth=noauth` (dashboard with OIDC off). `VITE_API_MOCK=1` is the Security Posture
API mock.

**Playwright.** `npm run build:mock-preview` builds both mock bundles; `npm run e2e` runs
`playwright/provenance.spec.ts` against the provenance bundle and the remaining specs (posture mode)
against the other, in `mcr.microsoft.com/playwright:v1.63.0-noble`:

```sh
dnode npm run build:mock-preview
docker run --rm --network host --ipc=host -u "$(id -u):$(id -g)" -e HOME=/tmp -v "$PWD":/app -w /app \
  mcr.microsoft.com/playwright:v1.63.0-noble npx playwright test -c playwright/playwright.config.ts
```

The end-to-end specs against a real dashboard live in `../test/e2e` (Integration Test workflow).

**Screenshots.** `screenshots/run.sh` builds the provenance mock bundle, serves it with
`vite preview` and captures Overview, Images, Image detail, Supply chain and Reports in light and dark
into `../docs/screenshots/dashboard-*-{light,dark}.png` (both containers use `--network host`).
The Integration Test workflow runs the same `screenshots/shoot.mjs` against the sandbox on every
push to `main`.

## Design system

Nebari registry items are vendored as source and treated as upstream-managed; customise at the
call site:

- `src/index.css`: `@nebari/theme` verbatim, then app-owned header tokens in separate blocks.
- `src/components/ui/*`: alert, badge, breadcrumb, button, card, checkbox, code-block, data-table,
  dialog, dropdown-menu, field, input, label, navigation-menu, select, sidebar, skeleton, spinner,
  switch, table, tabs, textarea, toast, tooltip.
- `src/hooks/*`: `use-theme-preference`, `theme-provider`; `src/lib/utils.ts`: `cn()`.
- `public/`: Nebari symbol / favicon and horizontal lockups, unmodified (CC BY-NC-ND 4.0).
- `@base-ui/react` is pinned to `~1.6.0` (the registry's tested version; 1.8 changes toast types).

Grade colours use semantic tokens only (`src/lib/severity-styles.ts`): A/B success, C warning,
D/F destructive.
