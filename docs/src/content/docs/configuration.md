---
title: Configuration Reference
description: Environment variables read by the collector and dashboard binaries. Generated from internal/configspec/spec.go.
---

<!-- GENERATED FILE — do not edit by hand.
     Source of truth: internal/configspec/spec.go.
     Regenerate with: make docs (or: go run ./hack/gendocs)
     CI guards drift between this file and the spec. -->

> **Generated** from `internal/configspec/spec.go`. Edit the spec and run `make docs` to update this file.

The collector and dashboard binaries are configured entirely via environment
variables. When deployed with the Helm chart, these are set through
`values.yaml` and rendered into the relevant pod's env block.

## Collector

Read by `cmd/provenance-collector` (the CronJob).

| Variable | Type | Default | Description |
|---|---|---|---|
| `PROVENANCE_NAMESPACES` | string list | *(empty)* | Comma-separated namespaces to scan. Empty means scan all namespaces. |
| `PROVENANCE_EXCLUDE_NAMESPACES` | string list | *(empty)* | Comma-separated namespaces to exclude from the scan. |
| `PROVENANCE_REGISTRY_AUTH` | string | *(empty)* | Path to a Docker `config.json` for private registry authentication, or a directory containing `config.json` or `.dockerconfigjson` (a mounted `docker-registry` Secret). Used for every registry call (digests, tags, signatures, SBOM and provenance lookups); registries it does not list fall back to `$DOCKER_CONFIG` and credential helpers. The collector exits at startup if the file is set but unreadable. |
| `PROVENANCE_REGISTRY_CA_FILE` | string | *(empty)* | Path to a PEM bundle of extra CA certificates trusted for registry TLS, added to the system roots. Use for registries behind a private CA. |
| `PROVENANCE_REGISTRY_INSECURE` | string list | *(empty)* | Comma-separated registry hosts (`host` or `host:port`) that may be reached over plain HTTP or with an unverified TLS certificate. Applies only to the listed hosts. |
| `PROVENANCE_REGISTRY_TIMEOUT` | duration | `30s` | Timeout for registry operations (digest resolution, tag listing). |
| `PROVENANCE_CHECK_UPDATES` | bool | `true` | Check registries for newer semver tags. |
| `PROVENANCE_UPDATE_LEVEL` | string | `patch` | Minimum version bump to flag as an update: `patch`, `minor`, or `major`. |
| `PROVENANCE_SKIP_PRERELEASE` | bool | `true` | Ignore alpha / beta / RC versions when checking for updates. |
| `PROVENANCE_VERIFY_SIGNATURES` | bool | `true` | Check for cosign signatures on images. |
| `PROVENANCE_COSIGN_PUBLIC_KEY` | string | *(empty)* | Path or KMS URI for the cosign public key. Empty = existence check only, no trust-chain verification. |
| `PROVENANCE_CHECK_SBOM` | bool | `true` | Check for SBOM attestations on discovered images. |
| `PROVENANCE_CHECK_PROVENANCE` | bool | `true` | Check for SLSA provenance attestations on discovered images. |
| `PROVENANCE_HELM_ENABLED` | bool | `true` | Discover deployed Helm releases. |
| `PROVENANCE_REPORT_OUTPUT` | string | `http` | Report output type: `http` (POST to the dashboard's internal endpoint), `pvc` (write to a shared PVC), or `configmap`. |
| `PROVENANCE_REPORT_CONFIGMAP` | string | `provenance-report` | ConfigMap name used by the `configmap` report output type. |
| `PROVENANCE_REPORT_CONFIGMAP_NAMESPACE` | string | `default` | Namespace for the report ConfigMap. Typically overridden by the chart to the release namespace. |
| `PROVENANCE_REPORT_UPLOAD_URL` | string | *(set by chart)* | Full URL the collector POSTs to in `http` mode, e.g. `http://provenance-collector-web-internal.<ns>.svc:8081/internal/reports`. |
| `PROVENANCE_REPORT_UPLOAD_TIMEOUT` | duration | `30s` | Timeout for the report upload request in `http` mode. |
| `PROVENANCE_CLUSTER_NAME` | string | *(empty)* | Cluster name included in report metadata. Informational only. |
| `KUBECONFIG` | string | *(empty)* | Path to a kubeconfig file. Empty = use in-cluster credentials (the normal case for the chart-deployed CronJob). |

## Shared

Read by both binaries.

| Variable | Type | Default | Description |
|---|---|---|---|
| `PROVENANCE_REPORT_PATH` | string | `/reports` | Filesystem path used by the dashboard pod (always) and the collector pod (only in `pvc` mode). |
| `PROVENANCE_REPORT_RETENTION` | duration | `168h` | Auto-prune reports older than this. Use `-1` to disable cleanup and keep all reports. |

## Dashboard

Read by `cmd/dashboard` (the web UI pod).

| Variable | Type | Default | Description |
|---|---|---|---|
| `PROVENANCE_DASHBOARD_ADDR` | string | `:8080` | Listen address for the public HTTP server (UI + read API). |
| `PROVENANCE_DASHBOARD_INTERNAL_ADDR` | string | `:8081` | Listen address for the internal upload endpoint. Never put this behind an Ingress. |
| `PROVENANCE_UPLOAD_MAX_BYTES` | bytes | `16777216` | Max body size in bytes accepted on the internal upload endpoint. Default ~16 MiB. |
| `PROVENANCE_OIDC_ISSUER` | string | *(empty)* | OIDC issuer URL the dashboard calls for userinfo (e.g. `https://keycloak.example.com/realms/nebari`). Empty = auth disabled. |
| `PROVENANCE_ADMIN_GROUPS` | string list | *(empty)* | Comma-separated OIDC groups whose members may trigger a scan via the dashboard's Run Scan button. |
| `PROVENANCE_MANUAL_JOB_TTL` | duration | `1h` | TTL after which dashboard-triggered Jobs are auto-cleaned. `0` (or any zero-duration string) keeps Jobs forever. |
| `PROVENANCE_NAMESPACE` | string | *(set by chart)* | Namespace the dashboard runs in. Used to address the CronJob from `/api/scan`. The chart populates this via the downward API. |
| `PROVENANCE_CRONJOB_NAME` | string | *(set by chart)* | Name of the CronJob the dashboard creates manual Jobs from. The chart sets this to the release's full name. |
| `PROVENANCE_FEATURE_TIMELINE_DELTAS` | bool | `false` | Opt-in: render a `+N / -N` unique-image delta badge on each timeline card vs the previous scan. Exposed in the chart as `webUI.features.timelineDeltas`. |

## Value formats

### Duration

Duration values use Go's `time.ParseDuration` format:

- `30s` — 30 seconds
- `5m` — 5 minutes
- `1h30m` — 1 hour 30 minutes
- `168h` — 7 days

Where noted, `-1` disables the timeout / retention.

### Boolean

Boolean values accept `true` / `1` / `yes` / `on` (case-insensitive) as truthy. Anything else, including unset, is false.

### Bytes

Integer count of bytes (decimal). `16777216` is 16 MiB.

### String list

Comma-separated. Whitespace around commas is trimmed; empty entries are dropped.
