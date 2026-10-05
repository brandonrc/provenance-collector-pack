---
title: Architecture
description: How the collector, dashboard API, frontend, and storage modes fit together.
---

The Provenance Collector runs as a Kubernetes `CronJob` that discovers what is
running on the cluster, enriches it with supply-chain data from the container
registry, and ships a timestamped JSON report to the configured sink.

```mermaid
flowchart TD
    Cron[Kubernetes CronJob<br/>default: daily 06:00 UTC]
    ImgDisc[Image Discovery<br/>pods + workload owners]
    HelmDisc[Helm Discovery<br/>via Helm SDK]
    Enrich[Enrichment<br/>digest resolve · cosign verify<br/>SBOM · SLSA · update check]
    Report[(JSON Provenance Report)]
    Browser[Browser<br/>React SPA · keycloak-js PKCE]
    Frontend[Frontend<br/>nginx · serves SPA · proxies /api]
    Dash[Dashboard API<br/>JSON API · owns the PVC]
    CM[(ConfigMap<br/>persistence.mode=configmap)]
    PVC[(Shared PVC<br/>persistence.mode=pvc)]
    Grafana[Grafana<br/>via Infinity datasource]

    Cron --> ImgDisc
    Cron --> HelmDisc
    ImgDisc --> Enrich
    Enrich --> Report
    HelmDisc --> Report
    Report -->|HTTP upload, default| Dash
    Report -.-> CM
    Report -.-> PVC
    PVC -.-> Dash
    Browser --> Frontend
    Frontend -->|/api proxy| Dash
    Dash --> Grafana
```

The collector reads the Kubernetes API for inventory and the registry for
enrichment, then ships the report to the dashboard's internal upload endpoint
(default), a ConfigMap, or a shared PVC depending on `persistence.mode`
(see [Storage Modes](/storage-modes/)). It does not push to remote services or
mutate cluster state outside the configured sink. The UI is served separately:
the browser loads the React SPA from the frontend nginx container, which
reverse-proxies `/api/*` to the dashboard.

## What it does

| Capability | Description |
|---|---|
| **Image Discovery** | Scans all pods across namespaces, deduplicates by workload owner |
| **Digest Resolution** | Resolves every image tag to its immutable SHA256 digest |
| **Signature Verification** | Checks for cosign signatures (existence or key-based verification) |
| **SLSA Provenance** | Detects SLSA provenance attestations via OCI referrers API |
| **SBOM Detection** | Detects attached SPDX / CycloneDX attestations |
| **Update Checking** | Compares running tags against latest semver tags (configurable level, pre-release filtering) |
| **Helm Release Tracking** | Discovers all deployed Helm releases with chart versions |
| **Web Dashboard** | Optional React + TypeScript SPA on the Nebari design system (served by nginx): supply-chain score, image inventory with per-image supply-chain detail, Helm releases, report history and downloads |
| **Grafana Integration** | JSON API compatible with the Infinity datasource for dashboards and alerting |
| **Provenance Reports** | Outputs timestamped JSON reports via the dashboard's internal upload endpoint (default), a shared PVC, or a ConfigMap, with automatic retention |

## Learn more

- [Quick Start](/quick-start/) — install and run your first scan.
- [Storage Modes](/storage-modes/) — how reports move from the collector to the dashboard.
- [Web Dashboard](/web-dashboard/) — the UI, its JSON API, and Grafana integration.
- [Report Schema](/report-schema/) — the JSON output structure.
