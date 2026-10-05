---
title: Web Dashboard
description: The React dashboard UI, its JSON API, and how to surface provenance data in Grafana.
---

The UI is a standalone **React + TypeScript SPA** built on the
[Nebari design system](https://github.com/nebari-dev/nebari-design) (Vite +
Tailwind), shipped as its own unprivileged nginx image and deployed as a
separate `Deployment`/`Service`. The Go dashboard is **API-only**: nginx serves
the SPA and reverse-proxies `/api/*` to the dashboard over cluster DNS. Enable
both:

```yaml
webUI:
  enabled: true       # dashboard API + report-upload endpoint (required in http mode)
frontend:
  enabled: true       # standalone React UI (nginx)
  keycloak:
    url: https://keycloak.<your-domain>   # required: the browser keycloak-js login endpoint
```

![Overview: supply-chain score, signature / SBOM / provenance / update tiles, report metadata and recent reports](https://raw.githubusercontent.com/nebari-dev/provenance-collector-pack/main/docs/screenshots/dashboard-overview-light.png)

## What it shows

- **Overview**: the supply-chain score (A–F; each image starts at 100 and loses
  points for a missing or unverified signature, no SBOM, no SLSA provenance, an
  available update and a mutable tag; the cluster value is the
  container-weighted mean), Signed / Verified / SBOM / SLSA provenance / update
  tiles, report metadata (cluster, collector version, schema version,
  namespaces scanned), the collector's warnings and the last few reports.
- **Images**: one row per unique image with its supply-chain grade, signature,
  SBOM, provenance and update status, namespaces and workloads; search,
  filters, sorting and paging. The image detail page has **Used by** (every
  namespace / workload / container running it) and **Supply chain**
  (signature, SBOM format, SLSA predicate type, available updates and the score
  deductions). Checks the collector did not run, for example on an image whose
  digest could not be resolved, show as "not checked" and cost nothing.
- **Supply chain**: the same tiles, the **Helm releases** table (installed vs
  latest chart version, status) and lists of unsigned / unverified and
  outdated images.
- **Reports**: every collector run, newest first. **View** loads an earlier
  report into Overview, Images and Supply chain, with a banner to go back to
  the latest. Every row downloads as JSON, CSV or Markdown. With
  `webUI.features.timelineDeltas` a Δ column shows the `+N / -N` unique-image
  change between adjacent runs.
- **Scans**: **Run scan** triggers a one-shot Job from the same CronJob
  template the schedule uses. It is shown only when `/api/me` returns
  `canRunScan` (`webUI.oidcIssuer` set and the user's groups intersect
  `webUI.adminGroups`). The dashboard has no job-status endpoint, so the UI
  polls `/api/reports` every 5 s for up to 5 minutes and loads the new report
  when it lands. Manual Jobs are cleaned up after `webUI.manualJobTTL`
  (default 1h).
- Light / Dark / System theme, chosen from the profile menu (defaults to
  System).

![Image detail, Supply chain tab (dark theme)](https://raw.githubusercontent.com/nebari-dev/provenance-collector-pack/main/docs/screenshots/dashboard-image-detail-dark.png)

## Authentication

The SPA runs the OIDC login in the browser via `keycloak-js` (PKCE `S256`,
`login-required`, no session iframe) before it renders, attaching the access
token to every `/api` call and refreshing it when less than 30 s of validity
is left. After a 401 it forces one refresh and retries; a second 401 shows a
"Session expired" screen. nginx forwards the token to the dashboard, which
validates it against Keycloak. Under `nebariapp.enabled: true` the
operator provisions the public SPA client and registers routing/landing-page —
the gateway itself does **not** enforce auth
(`nebariapp.auth.enforceAtGateway: false`). The operator also wires
`webUI.oidcIssuer` / `webUI.adminGroups` from the `nebariapp.auth` block so
Run scan lights up for users in the configured groups. See the
[NebariApp CRD reference](/nebariapp-crd-reference/) for the full field list.

## Branding

The UI ships with built-in Nebari branding (title, logos, favicon, theme
colors) and needs no configuration. Operators can rebrand it **without
rebuilding the image**: branding is delivered at runtime through the same
`/config.json` the SPA already fetches for Keycloak settings, and applied before
React mounts (title, favicon, and theme CSS variables) and in the header (logo).

### Configurable fields

| Field | Description |
|---|---|
| `title` | Product title in the header, sidebar and browser tab (default "Supply-chain provenance"). |
| `logoUrl` | Header logo (light mode / default). Absolute `http(s)` URL, root-relative path or base64 image `data:` URI. |
| `logoUrlDark` | Dark-mode header logo. Falls back to `logoUrl`, then the built-in dark logo. |
| `faviconUrl` | Favicon URL. |
| `theme.light` / `theme.dark` | CSS variable overrides per mode. Supported tokens: `primary`, `primaryForeground`, `primaryHover`, `background`, `foreground`, `secondary`, `secondaryForeground`, `muted`, `mutedForeground`, `accent`, `accentForeground`, `border`, `ring`, `radius`, `sidebarPrimary`, `sidebarPrimaryForeground`, `sidebarRing`. |

Every field is optional. Any field left empty uses the built-in Nebari default,
so an unbranded install looks exactly as it does today.

`primaryHover` (the button/badge hover and active shade), `sidebarPrimary`,
`sidebarPrimaryForeground` and `sidebarRing` follow `primary`,
`primaryForeground` and `ring` when you do not set them (`primaryHover` is
`primary` mixed with 15% black), so setting `primary` is enough to rebrand
hover and sidebar states as well. Override them explicitly only to pin a
specific shade.

Token keys are written to CSS as kebab-case custom properties
(`primaryForeground` → `--primary-foreground`) with no allow-list, so any other
theme variable the SPA defines can technically be set here. Only the tokens
listed above are supported.

### Kubernetes / Helm

Set `frontend.branding` (and optionally `frontend.title`) in values. The chart
renders them into the `/config.json` ConfigMap mounted into the nginx pod:

```yaml
frontend:
  enabled: true
  title: "Acme Provenance"
  branding:
    logoUrl: "https://cdn.acme.example/logo.svg"
    logoUrlDark: "https://cdn.acme.example/logo-dark.svg"
    faviconUrl: "https://cdn.acme.example/favicon.svg"
    theme:
      light:
        primary: "oklch(55% 0.19 250)"
        primaryForeground: "#ffffff"
      dark:
        primary: "oklch(62% 0.21 250)"
```

A branding-only `helm upgrade` rolls the frontend pod automatically (the
deployment is annotated with a checksum of the rendered ConfigMap).

### Outside Kubernetes

Running the standalone `frontend` image without the chart, mount your own
`config.json` over the one baked into the image. It takes the same keys as the
chart-rendered file (see [Runtime config](#runtime-config)):

```bash
docker run -p 8080:8080 --read-only --tmpfs /tmp --tmpfs /var/cache/nginx \
  -e API_UPSTREAM=dashboard.example.internal:8080 \
  -v "$PWD/config.json:/usr/share/nginx/html/config.json:ro" \
  ghcr.io/nebari-dev/provenance-collector-pack/frontend
```

The `BRANDING_*` / `KEYCLOAK_*` environment overrides of the previous image
are gone: the image runs as uid 101 with a read-only root filesystem and does
not rewrite its own files at startup.

### Security

Theme token values are validated in the browser before they are applied: any
value containing CSS-injection characters (`;`, `{`, `}`, `<`, `>`, quotes,
backslash, `url(`, `expression(`, `javascript:`) is dropped rather than injected
into the stylesheet. Logo and favicon URLs are restricted to `http(s)` URLs and
root-relative paths and base64-encoded image `data:` URIs; anything else is
ignored and the built-in logo or favicon is used.

## Runtime config

The SPA reads `/config.json` once at startup. The chart renders it from
`frontend.*` values; unknown keys are ignored.

| Key | Chart value | Meaning |
|---|---|---|
| `mode` | always `provenance` | Which backend is behind `/api/`. Without it the SPA probes `GET /api/v1/summary` and picks provenance mode on a 404 (see [below](#shared-with-the-security-posture-pack)). |
| `provenanceApiBase` | always `/api` | Base path of the dashboard API. |
| `keycloak.url`, `.realm`, `.clientId` | `frontend.keycloak.*` | Browser login. All three must be set; without a complete block no login happens and no bearer is sent (dashboard with auth off). |
| `title`, `logoUrl`, `logoUrlDark`, `faviconUrl`, `theme` | `frontend.title`, `frontend.branding.*` | [Branding](#branding). |

## Image

| Item | Value |
|---|---|
| Image | `ghcr.io/nebari-dev/provenance-collector-pack/frontend` (tag defaults to the chart's `appVersion`) |
| Base | `nginxinc/nginx-unprivileged:1.27-alpine`, uid/gid 101 |
| Port | `8080` (`NGINX_PORT`; chart: `frontend.port`) |
| `/api/` upstream | `API_UPSTREAM` (`host:port`; chart: `<fullname>-web:<webUI.port>`). A bare Service name is qualified with the pod's `<ns>.svc.<cluster-domain>` search domain, and nginx re-resolves it every 30 s. The URI and all request headers, including `Authorization` and `Sec-Fetch-Site` (the `POST /api/scan` CSRF guard), are passed through unchanged. |
| Writable paths | `/tmp` and `/var/cache/nginx` (emptyDirs in the chart); works with `readOnlyRootFilesystem: true` and `capabilities.drop: [ALL]` |
| `/healthz` | static `200 ok` from nginx (liveness / readiness probes) |
| Headers | `X-Frame-Options: DENY`, `Content-Security-Policy: frame-ancestors 'none'`, `nosniff`, `Referrer-Policy`, `Permissions-Policy`, `Cross-Origin-Opener-Policy` |

### Shared with the Security Posture pack

The same image is the UI of the
[Nebari Security Posture pack](https://github.com/nebari-dev/nebari-security-posture-pack),
which adds vulnerability scanning, workload posture checks and compliance
reports on top of this pack's provenance data. With `"mode": "provenance"` (or
when `GET /api/v1/summary` answers 404) only the sections above are shown; the
posture sections stay hidden and their routes redirect to the Overview.

## Running the UI locally

Port-forward the dashboard API and point the Vite dev server at it. The dev
server's `config.json` has no `keycloak` block, so no login happens (matching
a dashboard with auth off):

```bash
kubectl port-forward svc/provenance-collector-web 8080:8080 -n provenance-system &
cd frontend
npm ci
API_PROXY=http://localhost:8080 npm run dev   # → http://localhost:5173
```

Without a cluster, `npm run dev:mock-provenance` runs the UI against an
in-browser mock of the dashboard API that serves the golden test report plus
two older runs. Append `?mockAuth=viewer` for a user without `canRunScan`.

The [`dev/Makefile`](https://github.com/nebari-dev/provenance-collector-pack/tree/main/dev)
wraps this as `make ui-up` (install the chart, API only) + `make seed`
(sample reports) + `make ui-dev` (port-forward + Vite).

## Dashboard API

The dashboard exposes a JSON API that the SPA and external tools consume:

| Endpoint | Description |
|---|---|
| `GET /api/reports` | List all reports (newest first) with summary |
| `GET /api/reports/latest` | Get the most recent report |
| `GET /api/reports/<filename>` | Get a specific report by filename |
| `GET /api/export?format=csv\|markdown\|md` | Render the selected report as CSV or Markdown. Optional `&filename=<file>` to pin a historical report; defaults to latest. |
| `GET /api/me` | Calling user's identity + feature flags. Returns `authEnabled`, `canRunScan`, `features.timelineDeltas`. |
| `POST /api/scan` | Trigger a manual scan Job. 403 if the caller isn't in an admin group, 503 if `PROVENANCE_NAMESPACE` / `PROVENANCE_CRONJOB_NAME` aren't configured. |
| `GET /healthz` | Health check |

The JSON payloads follow the structure documented in the
[Report Schema reference](/report-schema/).

## Grafana integration

The provenance data can be surfaced in Grafana using the
[Infinity datasource](https://grafana.com/grafana/plugins/yesoreyeram-infinity-datasource/)
plugin, which queries the dashboard's JSON API.

### Setup

1. Enable the web dashboard (`webUI.enabled: true`)
2. Install the Infinity datasource in Grafana
3. Add a datasource pointing at the dashboard service:
   - **URL:** `http://provenance-collector-web.provenance-system.svc:8080`
   - **Type:** JSON

### Example panels

**Stat panel** (unique images count):

- Type: JSON, URL: `/api/reports/latest`
- Column: `summary.uniqueImages`

**Images table**:

- Type: JSON, URL: `/api/reports/latest`, Root: `images`
- Columns: `image`, `namespace`, `signature.signed`, `provenance.hasProvenance`, `update.updateAvailable`

**Alerting** (images with updates):

```
WHEN count() OF images WHERE updateAvailable = true IS ABOVE 0
```

An example dashboard (11 panels covering unique images, signature status, SLSA
provenance, Helm releases, and more) is available at
[`examples/grafana-dashboard.json`](https://github.com/nebari-dev/provenance-collector-pack/blob/main/examples/grafana-dashboard.json).
Import it directly into Grafana as a `dashboard.grafana.app/v2beta1` resource.
