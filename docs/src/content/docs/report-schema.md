---
title: Report Schema Reference
description: JSON structure of the provenance report - written by the Go collector and served by the api at /api/reports/*.
---

The provenance report schema is unchanged from provenance-collector-pack 0.1.x
(`collector/internal/report/types.go`). It appears in two places:

- **The Go collector's output.** `provenance-collector --once --output -` writes one
  report; the worker runs it every scan and ingests it
  ([Engines](/provenance/#engines)).
- **The compatible API.** `GET /api/reports/latest` (and `/api/reports`,
  `/api/reports/{filename}`, `/api/export`) renders the report from the latest
  completed scan's stored results, one report per scan
  (`provenance-YYYYMMDD-HHMMSS.json` from the scan's finish time). Field names,
  order, `omitempty` and formatting match the Go `json.MarshalIndent` output.

Differences in what the API serves, compared with a raw collector report (details
in [Supply-chain provenance](/provenance/#differences-from-provenance-collector-pack)):
`workload` is the resolved controller (Deployment, CronJob) rather than the
ReplicaSet / Job; `digest` is the digest actually running; `generatedAt` is the
scan's finish time; `collectorVersion` carries the `+posture` suffix; Helm
releases get `update` when `provenance.helmReleases.chartRepos` knows the chart.

## Top-Level

| Field | Type | Description |
|---|---|---|
| `metadata` | [ReportMetadata](#reportmetadata) | When and where the report was generated. |
| `images` | [ImageRecord[]](#imagerecord) | All discovered container images with provenance data. |
| `helmReleases` | [HelmRecord[]](#helmrecord) | Deployed Helm releases (omitted if Helm discovery disabled). |
| `summary` | [ReportSummary](#reportsummary) | Aggregate counts for quick compliance review. |

## ReportMetadata

| Field | Type | Description |
|---|---|---|
| `generatedAt` | ISO 8601 datetime | When the report was generated (UTC). |
| `collectorVersion` | string | Version of the provenance collector. |
| `clusterName` | string | Cluster name (from configuration). |
| `namespacesScanned` | string[] | Namespaces that were scanned. |

## ImageRecord

| Field | Type | Description |
|---|---|---|
| `image` | string | Full image reference (e.g., `nginx:1.27-alpine`). |
| `digest` | string | SHA256 content digest (e.g., `sha256:abc123...`). |
| `namespace` | string | Kubernetes namespace where the image is running. |
| `workload` | [WorkloadRef](#workloadref) | Owning workload (Deployment, StatefulSet, etc.). |
| `signature` | [SignatureInfo](#signatureinfo) | Cosign signature status (omitted if not checked). |
| `sbom` | [SBOMInfo](#sbominfo) | SBOM attestation status (omitted if not found). |
| `provenance` | [ProvenanceInfo](#provenanceinfo) | SLSA provenance attestation status (omitted if not found). |
| `update` | [UpdateInfo](#updateinfo) | Available updates (omitted if none or not checked). |

## WorkloadRef

| Field | Type | Description |
|---|---|---|
| `kind` | string | Kubernetes resource kind (e.g., `Deployment`, `StatefulSet`, `DaemonSet`). |
| `name` | string | Resource name. |

## SignatureInfo

| Field | Type | Description |
|---|---|---|
| `signed` | bool | Whether a cosign signature exists. |
| `verified` | bool | Whether the signature was verified against the configured public key. |
| `error` | string | Error message if verification failed. |

## SBOMInfo

| Field | Type | Description |
|---|---|---|
| `hasSBOM` | bool | Whether an SBOM attestation was found. |
| `format` | string | SBOM format: `spdx`, `cyclonedx`, or empty. |

## ProvenanceInfo

| Field | Type | Description |
|---|---|---|
| `hasProvenance` | bool | Whether a SLSA / in-toto provenance attestation was found. |
| `predicateType` | string | Predicate type, e.g. `https://slsa.dev/provenance/v0.2`. |

## UpdateInfo

| Field | Type | Description |
|---|---|---|
| `currentTag` | string | Currently running tag. |
| `latestInMajor` | string | Latest patch/minor in the same major version. |
| `newestAvailable` | string | Absolute newest semver tag available. |
| `updateAvailable` | bool | Whether any update is available. |

## HelmRecord

| Field | Type | Description |
|---|---|---|
| `releaseName` | string | Helm release name. |
| `namespace` | string | Release namespace. |
| `chart` | string | Chart name. |
| `version` | string | Installed chart version. |
| `appVersion` | string | Application version from chart metadata. |
| `status` | string | Release status (`deployed`, `failed`, etc.). |
| `update` | [UpdateInfo](#updateinfo) | Available chart updates (if implemented). |

## ReportSummary

| Field | Type | Description |
|---|---|---|
| `totalImages` | int | Total image records (includes duplicates across workloads). |
| `uniqueImages` | int | Count of distinct image references. |
| `signedImages` | int | Images with cosign signatures. |
| `verifiedImages` | int | Images with verified signatures. |
| `imagesWithSBOM` | int | Images with attached SBOM attestations. |
| `imagesWithProvenance` | int | Images with SLSA provenance attestations. |
| `imagesWithUpdates` | int | Images with newer versions available. |
| `totalHelmReleases` | int | Total Helm releases discovered. |
| `helmReleasesWithUpdates` | int | Helm releases with newer chart versions. |
