---
title: Configuration
description: Chart values of the provenance-collector chart (Security Posture pack), including the provenance-collector-pack 0.1.x compatibility keys.
---

All values live in
[`chart/values.yaml`](https://github.com/nebari-dev/provenance-collector-pack/blob/main/chart/values.yaml),
which is commented key by key; this page is the map. Many scan, provenance and
controls-engine settings are only **defaults**: after install they are edited at
runtime on the Settings page (`PUT /api/v1/settings`).

The Go collector binary's own environment variables are listed in
[Collector environment](/collector-environment/); in the pack the worker sets
them from the `provenance.*` values below.

## Images

| Key | Default | Notes |
|---|---|---|
| `images.api` / `images.worker` / `images.ui` | `quay.io/nebari/provenance-collector-pack-{api,worker,ui}:<appVersion>` | First-party images. A non-empty `digest` wins over `tag`. The worker image bundles the Go collector. |
| `images.trivy` / `images.clair` / `images.postgres` | `aquasec/trivy`, `quay.io/projectquay/clair`, `postgres:16-alpine` | Pinned third-party images. |

## Access: `adminGroups`, `auth`, `adminGate`

| Key | Default | Notes |
|---|---|---|
| `adminGroups` | `[admin]` | Keycloak groups allowed to use the pack. Feeds the NebariApp `auth.groups`, the optional SecurityPolicy and the api's own check. A leading `/` is ignored. |
| `auth.mode` | `oidc` | `disabled` turns off all API authentication (development only). |
| `auth.jwksUrl` | in-cluster Keycloak realm certs | JWKS used to verify token signatures. |
| `auth.issuers` | `[]` | Accepted `iss` values (public and in-cluster realm URLs). **Required** with `oidc`: an empty list rejects every token. |
| `adminGate.securityPolicy.enabled` | `false` | Render an Envoy Gateway `SecurityPolicy` (OIDC + JWT + deny unless group) for operators that only use `auth.groups` for landing-page visibility. Needs `externalIssuer`. Sets the NebariApp's `enforceAtGateway: false`. |

## Scanning: `scanner`

| Key | Default | Notes |
|---|---|---|
| `scanner.intervalHours` | `6` | Scheduled scan interval (replaces the 0.1.x `schedule` cron). |
| `scanner.excludedNamespaces` | `[]` | Never inventoried. `kube-system` is scanned by default. |
| `scanner.parallelism` / `timeoutSeconds` / `rescanAfterHours` | `3` / `600` / `24` | Images in parallel; per-scanner timeout; digest rescan interval unless forced. |
| `scanner.trivy.enabled` / `grype.enabled` / `clair.enabled` | `true` | Each scanner can be switched off (Trivy and Clair also drop their Deployments). |
| `scanner.mirror.*` | enabled, `registry.container-registry.svc.cluster.local:5000` | skopeo mirror registry, `insecure`, and `rewrite` (registry host aliases, e.g. `localhost:32000`). |
| `registryAuth.existingSecret` | `""` | dockerconfigjson Secret for private registries (worker and collector). |
| `extraCACerts.secretName` | `""` | Extra CA certificates trusted by the worker and Clair. |

## Supply-chain provenance: `provenance`

| Key | Default | Notes |
|---|---|---|
| `provenance.enabled` | `true` | The provenance stage. |
| `provenance.engine` | `collector` | `collector`: run the bundled Go collector once per scan and ingest its report, with per-image and whole-run fallback to the Python checks. `python`: Python checks only. See [Engines](/provenance/#engines). |
| `provenance.verifySignatures` | `true` | Without a key or keyless identity this is an existence check. |
| `provenance.cosignPublicKey` / `cosign.existingSecret` | `""` | PEM text, path or KMS / remote URI; or a Secret with `cosign.pub`. |
| `provenance.cosign.certificateIdentityRegexp` / `certificateOidcIssuerRegexp` | `""` | Keyless verification (both required). |
| `provenance.checkSBOM` / `checkProvenance` / `checkUpdates` | `true` | Individual checks. |
| `provenance.updateLevel` / `skipPrerelease` | `patch` / `true` | Update check semantics, as in provenance-collector-pack. |
| `provenance.recheckHours` / `concurrency` / `registryTimeoutSeconds` | `24` / `8` / `30` | Python engine cache and registry settings (the timeout also goes to the collector). |
| `provenance.helmReleases.enabled` | `false` | Helm release discovery; adds cluster-wide Secrets get/list. |
| `provenance.helmReleases.chartRepos` | `[]` | Chart repositories for chart update checks. |
| `provenance.compat.internalService.*` | disabled, port `8080`, `allowedNamespaces: [monitoring]` | Unauthenticated `/api/reports*` Service for Grafana. `name` overrides the Service name (e.g. `provenance-collector-web` to keep a 0.1.x URL). |

## Control evidence engine: `controlsEngine`

| Key | Default | Notes |
|---|---|---|
| `controlsEngine.enabled` | `true` | Live NIST SP 800-53 assertions after every scan. |
| `controlsEngine.baseline` | `moderate` | `low`, `moderate` or `high`. |
| `controlsEngine.adminSubjectsAllowlist` | `[]` | Approved privileged Keycloak users and cluster-admin subjects. |
| `controlsEngine.keycloak.*` | in-cluster Keycloak, realm `nebari`, Secret `keycloak/nebari-realm-admin-credentials` | Admin API access for the identity assertions (`get` on that one Secret only). |
| `controlsEngine.lokiUrl` / `prometheusUrl` / `alertmanagerUrl` / `registryUrl` | `""` (discover) | Observability and registry endpoints. |

See [Controls](/controls/) for the assertions and their permissions.

## Reports: `reports`

| Key | Default | Notes |
|---|---|---|
| `reports.dir` | `/data/reports` | Report files (reports PVC). |
| `reports.keepPerType` | `50` | Retention per report type. |
| `reports.autoGenerate` | `[]` | Report types generated after every completed scan (e.g. `[poam, stig-checklist, sar, oscal-ar]`). |

## Storage: `persistence`, `postgresql`, `externalDatabase`

| Key | Default | Notes |
|---|---|---|
| `persistence.enabled` | `true` | `false` = emptyDirs everywhere. |
| `persistence.storageClass` / `accessMode` | `""` / `ReadWriteOnce` | |
| `persistence.worker` / `trivy` / `postgres` / `reports` | `20Gi` / `10Gi` / `10Gi` / `2Gi` | Volume sizes. |
| `postgresql.enabled` | `true` | Bundled Postgres; credentials generated once into `<fullname>-db` (kept on uninstall). |
| `externalDatabase.*` | | Host, user, databases (`posture`, `clair`), `existingSecret` when `postgresql.enabled=false`. |

See [Storage](/storage-modes/).

## Cluster and integration

| Key | Default | Notes |
|---|---|---|
| `clusterName` | `""` (`nebari`) | Report metadata `clusterName` and OSCAL system name default. |
| `networkPolicy.enabled` | `true` | Ingress policies between components; `gatewayNamespaces` and `uiAllowedNamespaces` may reach the ui. |
| `nebariapp.*` | disabled | The `NebariApp` CR ([NebariApp CRD](/nebariapp-crd-reference/)). `nebariapp.hostname` is required when enabled. |
| `api` / `worker` / `ui` / `trivy` / `clair` / `postgres` | | `resources`, `podSecurityContext`, `nodeSelector`, `tolerations`, `affinity` per component; `worker.extraEnv`. |
| `serviceAccount.*`, `rbac.create` | create | ServiceAccount and read-only ClusterRoles. |
| `nameOverride` / `fullnameOverride` | `""` | Resource naming. A release whose name contains the chart name collapses to the release name (`provenance-collector` -> `provenance-collector`). |

## Values from provenance-collector-pack 0.1.x

The chart still accepts the values of the CronJob chart
(`chart/templates/_compat.tpl`). Aliased keys win over the value they map to and
are listed as *DEPRECATED* in the install notes; retired keys **fail the render**
with a pointer to their replacement. See [Migrating from 0.1.x](/migrating/).

| 0.1.x key | Now | Handling |
|---|---|---|
| `config.verifySignatures` | `provenance.verifySignatures` | alias |
| `config.cosignPublicKey` | `provenance.cosignPublicKey` | alias |
| `config.checkSBOM` / `config.checkProvenance` / `config.checkUpdates` | `provenance.checkSBOM` / `checkProvenance` / `checkUpdates` | alias |
| `config.updateLevel` / `config.skipPrerelease` | `provenance.updateLevel` / `skipPrerelease` | alias |
| `config.helmEnabled` | `provenance.helmReleases.enabled` | alias (note: default `false` now, `true` in 0.1.x) |
| `config.registryTimeout` (Go duration) | `provenance.registryTimeoutSeconds` | alias, converted to seconds |
| `config.excludeNamespaces` | `scanner.excludedNamespaces` | alias, merged with the new list |
| `config.clusterName` | `clusterName` | alias |
| `config.namespaces` (non-empty) | none: every namespace except `scanner.excludedNamespaces` is scanned | **fails** |
| `schedule` | `scanner.intervalHours` | **fails** (a cron expression is not translated) |
| `persistence.mode` | Postgres + PVCs ([Storage](/storage-modes/)) | **fails** |
| `frontend.keycloak.url` | gateway login + `adminGroups` | **fails** |
| `config.reportPath`, `config.reportRetention`, `config.reportConfigMap`, `config.reportUploadTimeout`, `webUI.*`, `frontend.*`, `image`, `registryCredentials`, `resources`, `backoffLimit`, `activeDeadlineSeconds`, `successfulJobsHistoryLimit`, `failedJobsHistoryLimit`, `concurrencyPolicy` | no equivalent | accepted, ignored, listed as *IGNORED* in the install notes |
