---
title: Migrating from 0.1.x
description: Upgrading from the provenance-collector-pack 0.1.x chart (CronJob + dashboard) to the Security Posture pack.
---

provenance-collector-pack 0.1.x was a CronJob that ran the Go collector, a Go
dashboard and a thin React frontend. From 0.2.0 the same repository and chart
deploy the **Security Posture pack**: the collector is the provenance engine
inside the worker, alongside vulnerability scanning, posture checks, the control
evidence engine and compliance reports. This page lists what changes for an
existing install.

## Summary

| | 0.1.x | 0.2.x |
|---|---|---|
| Chart | `provenance-collector` | `provenance-collector` (same name, so ArgoCD Applications and `helm upgrade` keep working) |
| Workloads | CronJob, `-web` dashboard, `-frontend` nginx | `-api`, `-worker`, `-ui`, `-postgres`, `-trivy`, `-clair` |
| Schedule | `schedule` (cron) | `scanner.intervalHours` |
| Report storage | `persistence.mode`: http / pvc / configmap | Postgres, history per scan ([Storage](/storage-modes/)) |
| Login | in-browser PKCE (`frontend.keycloak.*`), any logged-in user | gateway login + `adminGroups`, verified again by the API |
| Grafana URL | `http://<fullname>-web.<ns>.svc:8080/api/reports/latest` | `http://<fullname>-web-internal.<ns>.svc:8080/api/reports/latest` |
| Report schema | | unchanged ([Report Schema](/report-schema/)) |

## Values

The chart accepts the 0.1.x keys (`chart/templates/_compat.tpl`), so an old
values file either renders or fails with a precise message. Install notes list
every key that was applied as an alias (*DEPRECATED*) or ignored (*IGNORED*).

| 0.1.x | 0.2.x | |
|---|---|---|
| `config.verifySignatures` | `provenance.verifySignatures` | alias |
| `config.cosignPublicKey` | `provenance.cosignPublicKey` (or `provenance.cosign.existingSecret`) | alias |
| `config.checkSBOM`, `config.checkProvenance`, `config.checkUpdates` | `provenance.checkSBOM`, `.checkProvenance`, `.checkUpdates` | alias |
| `config.updateLevel`, `config.skipPrerelease` | `provenance.updateLevel`, `.skipPrerelease` | alias |
| `config.helmEnabled` | `provenance.helmReleases.enabled` | alias; the new default is `false` (it grants cluster-wide Secrets get/list), set it to keep Helm releases |
| `config.registryTimeout` | `provenance.registryTimeoutSeconds` | alias (Go duration converted) |
| `config.excludeNamespaces` | `scanner.excludedNamespaces` | alias (merged) |
| `config.clusterName` | `clusterName` | alias |
| `config.namespaces` | - | **fails**: list the namespaces you do *not* want in `scanner.excludedNamespaces` |
| `schedule` | `scanner.intervalHours` | **fails**: e.g. daily `0 6 * * *` -> `intervalHours: 24` |
| `persistence.mode` | - | **fails**: remove it; `persistence.storageClass` still applies |
| `frontend.keycloak.url` | `nebariapp.*`, `adminGroups`, `auth.issuers` | **fails** |
| `webUI.*`, other `frontend.*`, `config.report*`, `image`, `registryCredentials`, `resources`, Job settings | see [Configuration](/configuration/) | ignored |

The failure messages look like:

```
Error: execution error at (provenance-collector/templates/worker.yaml:1:4): `schedule: "0 6 * * *"` is from
provenance-collector-pack <= 0.1.x (the collector CronJob). Scans now run every scanner.intervalHours hours
(default 6); remove `schedule` and set e.g. `--set scanner.intervalHours=24`. See docs: migrating.
```

A 0.1.x values file translated (see also
[`examples/nebari-values.yaml`](https://github.com/nebari-dev/provenance-collector-pack/blob/main/examples/nebari-values.yaml)):

```yaml
# 0.1.x                                # 0.2.x
# schedule: "0 6 * * *"                scanner: { intervalHours: 24 }
# config:                              provenance:
#   verifySignatures: true               verifySignatures: true
#   helmEnabled: true                    helmReleases: { enabled: true }
#   clusterName: prod                  clusterName: prod
# persistence: { mode: http }          persistence: { storageClass: "" }
# webUI: { enabled: true }             provenance.compat.internalService.enabled: true   # for Grafana
# frontend:                            adminGroups: [admin]
#   keycloak: { url: https://kc... }   auth: { issuers: [https://kc.../auth/realms/nebari, ...] }
```

## Grafana

The provenance report API is served by the api. For Grafana, enable the
unauthenticated in-cluster listener and change the datasource URLs:

```yaml
provenance:
  compat:
    internalService:
      enabled: true
      allowedNamespaces: [monitoring]   # Grafana's namespace
```

```
before: http://provenance-collector-web.provenance-system.svc:8080/api/reports/latest
after:  http://provenance-collector-web-internal.provenance-system.svc:8080/api/reports/latest
```

To keep the **old URL byte-for-byte**, name the Service like the 0.1.x one (same
release namespace): `provenance.compat.internalService.name: provenance-collector-web`.
Panels, root selectors and columns need no change.

## Authentication

0.1.x let any logged-in user read reports, and the React SPA logged in itself
(public PKCE client, `enforceAtGateway: false`). Now:

- the gateway enforces login (`enforceAtGateway: true`) and, where the operator
  supports it, `adminGroups`;
- the API checks the token and `adminGroups` on every route, including
  `/api/reports*` and `/api/export` on the public listener;
- `canRunScan` in `GET /api/me` is true for admin group members; `POST /api/scan`
  still exists and queues a scan.

Add the realm issuer(s) to `auth.issuers` and make sure report readers are in an
`adminGroups` group (or use the Grafana listener for read-only automation).

## Storage and history

There is no `persistence.mode`. Old report files on the 0.1.x dashboard PVC are
**not** imported; the first scan after the upgrade starts the new history. The
old PVC is removed by the upgrade (it is no longer part of the chart) - copy the
files out first if you need them:

```bash
kubectl cp provenance-system/<dashboard-pod>:/reports ./provenance-reports-0.1
```

## Release name, resource names

Resource names derive from the release and chart name exactly as before: release
`provenance-collector` gives `provenance-collector-api`, `-worker`, `-ui`,
`-web-internal`. A release with another name gets `<release>-provenance-collector-*`
unless `fullnameOverride` / `nameOverride` is set.

## What was removed

- The collector **CronJob** and its RBAC (configmaps write).
- The Go **dashboard** Deployment (`-web`, `-web-internal` upload endpoint).
- The **frontend** image (`ghcr.io/nebari-dev/provenance-collector-pack/frontend`) and its branding values.

## What stays

- The **Go collector** (`collector/`): built into the worker image, released as a
  standalone image (`quay.io/nebari/provenance-collector`) and as binaries on
  each GitHub release. New single-run mode:
  `provenance-collector --once --output -` writes one report to stdout (logs to
  stderr) using the current kubeconfig, without a dashboard or sink.
- The report schema, the `/api/reports*` / `/api/export` / `/api/me` /
  `/api/scan` / `/healthz` routes and the Grafana dashboard.
