---
title: Web UI
description: The admin UI, its authentication model, the JSON APIs (native and provenance-collector compatible) and the Grafana integration.
---

The UI is a React app built on [nebari-design](https://github.com/nebari-dev/nebari-design)
(shared Nebari header, tokens and components), served by the **ui** nginx
container, which proxies `/api/*` to the **api**. The NebariApp routes the
gateway to the ui Service. It replaces the provenance-collector-pack 0.1.x table
UI (`frontend/`) and Go dashboard.

## Pages

| Route | Page |
|---|---|
| `/` | **Overview**: cluster score and grade, findings by severity, score trend, riskiest images, supply-chain and NIST 800-53 control tiles. |
| `/images`, `/images/:id` | **Images**: every unique image by digest with score, findings, signature / SBOM / provenance glyphs and update arrow. Detail tabs: consensus findings with per-scanner agreement, supply chain, used by, raw scanner output. |
| `/vulnerabilities`, `/vulnerabilities/:id` | **Vulnerabilities**: consensus findings across the cluster, affected images, first seen (SLA). |
| `/workloads` | **Workloads**: controllers with their workload score and failing posture checks. |
| `/namespaces` | **Namespaces**: per-namespace scores. |
| `/checks`, `/checks/:id` | **Posture checks**: the 16 checks with STIG / SRG mapping and failing workloads. |
| `/supply-chain` | **Supply chain**: signed / verified / SBOM / provenance / update tiles, unsigned and outdated images, Helm releases. |
| `/compliance` | **Compliance**: NIST SP 800-53 controls per baseline with engine status and evidence, assertion runs, STIG view. |
| `/reports` | **Reports**: generate and download POA&M, STIG checklist, SAR, OSCAL, inventory and vulnerability exports. |
| `/scans`, `/scans/:id` | **Scans**: history, live log, **Scan now**. |
| `/settings` | **Settings**: scanning, compliance reporting (auto-generated reports), supply chain (signature verification, keyless identity, SBOM / SLSA / update checks, Helm releases) and the control evidence engine. |

## Authentication

1. The gateway sends unauthenticated browsers to Keycloak (NebariApp
   `enforceAtGateway: true`) and, on operators that enforce it, admits only
   `adminGroups` (or the chart's own `SecurityPolicy`, see
   [NebariApp CRD](/nebariapp-crd-reference/#the-admin-gate)).
2. The api verifies the forwarded token (JWKS signature, `auth.issuers`, expiry)
   and the group claim against `adminGroups` on **every** request.

`GET /api/v1/me` is the only route a non-admin can call (the UI uses it to show
"access denied" instead of an error). This differs from 0.1.x, where reading
reports only needed a login and the SPA ran its own PKCE flow; see
[Migrating](/migrating/#authentication).

## Native API (`/api/v1`)

JSON, camelCase, admin-only. Main routes: `summary`, `images`, `images/{id}`,
`vulnerabilities`, `workloads`, `checks`, `supply-chain`, `helm-releases`,
`scans` (`POST` starts a scan), `scanners`, `reports` (`POST` generates,
`/{id}/download`), `compliance/controls`, `compliance/assertions`,
`compliance/runs`, `compliance/stig`, `settings` (`GET` / `PUT`), `me`,
`health`, `ready`. The full contract is in [Design](/design/).

## Provenance-collector compatible API

Served by the api outside `/api/v1`, so the ui nginx proxies them unchanged:

| Endpoint | Notes |
|---|---|
| `GET /api/reports` | `[{filename, generatedAt, summary, clusterName?}]`, newest first; one report per completed scan with provenance results. |
| `GET /api/reports/latest`, `/api/reports/provenance-latest.json`, `/api/reports/{filename}` | The report document ([Report Schema](/report-schema/)), byte-compatible formatting. |
| `GET /api/export?format=csv\|markdown\|md\|json&filename=` | CSV / Markdown export of a report. |
| `GET /api/me` | `{authEnabled, email?, groups?, canRunScan, features}`. |
| `POST /api/scan` | Same-origin CSRF rule; admin only; queues a scan, 409 if one is running. |
| `GET /healthz` | `{"status":"ok"}` |

On the public listener these need the admin group like everything else.
`POST /internal/reports` (the 0.1.x collector upload) no longer exists: the
worker writes the database directly. Details: [Supply-chain provenance](/provenance/#compatible-api).

## Grafana integration

Grafana's [Infinity datasource](https://grafana.com/grafana/plugins/yesoreyeram-infinity-datasource/)
reads the report without a token from a separate, read-only listener:

```yaml
provenance:
  compat:
    internalService:
      enabled: true
      allowedNamespaces: [monitoring]   # Grafana's namespace (NetworkPolicy)
      # name: provenance-collector-web  # optional: keep the 0.1.x Service host name
```

This starts a second listener in the api (`:8081`, only `/api/reports*`,
`/api/export`, `/healthz`) behind the ClusterIP Service `<fullname>-web-internal`
(port 8080), never routed by the gateway. Datasource URL:

```
http://<fullname>-web-internal.<namespace>.svc:8080/api/reports/latest
```

The `root_selector`s (`""`, `images`, `helmReleases`) and columns
(`summary.uniqueImages`, `signature.signed`, `sbom.hasSBOM`,
`provenance.hasProvenance`, `update.updateAvailable`, ...) are unchanged.
[`examples/grafana-dashboard.json`](https://github.com/nebari-dev/provenance-collector-pack/blob/main/examples/grafana-dashboard.json)
(11 panels) points at `provenance-collector-web-internal.provenance-system`;
import it as a `dashboard.grafana.app/v2beta1` resource.

## Running the UI locally

See [`ui/README.md`](https://github.com/nebari-dev/provenance-collector-pack/blob/main/ui/README.md):
`npm run dev:mock` serves the UI with MSW fixtures and no API; `npm run dev`
proxies `/api` to a local or port-forwarded api.
