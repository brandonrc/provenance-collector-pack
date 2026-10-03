---
title: Report Schema Reference
description: JSON structure of the provenance reports the collector emits.
---

The provenance collector outputs JSON reports with the following structure.
The machine-readable contract is the JSON Schema at
[`schema/report.schema.json`](https://github.com/nebari-dev/provenance-collector-pack/blob/main/schema/report.schema.json),
generated from `internal/report/types.go`, and an example report produced by an
end-to-end test run is at
[`testdata/report.golden.json`](https://github.com/nebari-dev/provenance-collector-pack/blob/main/testdata/report.golden.json).
See [Consuming the Report](/consuming-the-report/) for how other tools should
read it.

## Versioning

`metadata.schemaVersion` is a semantic version of the report format (currently
`1.1.0`). A minor bump only adds fields; a major bump removes, renames or
changes the meaning of one. Consumers should accept any report with the major
version they were written for and ignore fields they don't know. Reports
written before the field existed have no `schemaVersion`; treat them as `1.0.0`.

## Top-Level

| Field | Type | Description |
|---|---|---|
| `metadata` | [ReportMetadata](#reportmetadata) | When and where the report was generated. |
| `images` | [ImageRecord[]](#imagerecord) | All discovered container images with provenance data. |
| `helmReleases` | [HelmRecord[]](#helmrecord) | Deployed Helm releases (omitted if Helm discovery disabled). |
| `summary` | [ReportSummary](#reportsummary) | Aggregate counts for quick compliance review. |
| `warnings` | string[] | Problems that made the report incomplete without failing the run: Helm namespaces whose release Secrets could not be listed, images whose digest could not be resolved, failed update checks. Omitted when empty. *(1.1.0)* |

## ReportMetadata

| Field | Type | Description |
|---|---|---|
| `schemaVersion` | string | Report format version, e.g. `1.1.0`. *(1.1.0)* |
| `generatedAt` | ISO 8601 datetime | When the report was generated (UTC). |
| `collectorVersion` | string | Version of the provenance collector. |
| `clusterName` | string | Cluster name (from configuration). |
| `namespacesScanned` | string[] | Namespaces that were scanned. |

## ImageRecord

| Field | Type | Description |
|---|---|---|
| `image` | string | Full image reference (e.g., `nginx:1.27-alpine`). |
| `digest` | string | SHA256 content digest (e.g., `sha256:abc123...`). Omitted when it could not be resolved; `warnings` says why. |
| `namespace` | string | Kubernetes namespace where the image is running. |
| `workload` | [WorkloadRef](#workloadref) | Owning workload (Deployment, StatefulSet, etc.). |
| `signature` | [SignatureInfo](#signatureinfo) | Cosign signature status (omitted if not checked). |
| `sbom` | [SBOMInfo](#sbominfo) | SBOM attestation status (omitted if not found). |
| `provenance` | [ProvenanceInfo](#provenanceinfo) | SLSA provenance status (omitted if not found). |
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
| `hasProvenance` | bool | Whether a SLSA provenance attestation was found. |
| `predicateType` | string | The attestation's in-toto predicate type, e.g. `https://slsa.dev/provenance/v1`. |

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
