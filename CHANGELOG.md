# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added
- `provenance-collector --output <path|->` writes the report to a file
  (atomically: temp file + rename) or to stdout instead of the
  `PROVENANCE_REPORT_OUTPUT` sink, so another tool can run the binary once and
  read the result. With `--output -` logs go to stderr. Without the flag
  nothing changes.
- `metadata.schemaVersion` (semver, now `1.1.0`) on every report, a JSON Schema
  generated from `internal/report/types.go` at `schema/report.schema.json`
  (`go run ./hack/genschema`, drift-checked in CI), and a golden report
  `testdata/report.golden.json` produced by an end-to-end test of the collector
  against a fake cluster.
- `warnings` on the report: Helm namespaces whose release Secrets could not be
  listed, images whose digest could not be resolved, and failed update checks.
  Previously these were only logged, so a 403 on Secrets looked like "no Helm
  releases".
- `PROVENANCE_REGISTRY_CA_FILE` (extra CAs for registry TLS) and
  `PROVENANCE_REGISTRY_INSECURE` (hosts allowed over HTTP / unverified TLS).
- Release binaries (linux/darwin, amd64/arm64) with SLSA build provenance and a
  keyless cosign signature on `checksums.txt`; `report.schema.json` is attached
  to each release.
- Docs: [Consuming the Report](docs/src/content/docs/consuming-the-report.md).

### Changed
- Helm releases are read directly from Helm's release Secrets instead of
  through `helm list`'s action client. Same result (latest revision of every
  release, any status), no REST mapper / discovery client, and a much smaller
  dependency tree.
- Release workflow is gated on the Test and Lint workflows and builds images
  itself (`build-image.yaml` is now called from `release.yaml` instead of
  triggering on `release` separately), so the chart can't be published ahead
  of its images. All actions, including the org reusable workflow and
  `helm-repository`'s `sync-chart`, are pinned by SHA.
- CI: `go mod tidy -diff`, a coverage floor for `internal/...` (85%),
  `govulncheck` (reachable vulnerabilities fail the build unless allowlisted
  with a reason), and the schema drift check.
- Go toolchain pinned in one place, the `toolchain` line in `go.mod`
  (`go1.26.8`): CI uses it via `setup-go` with `GOTOOLCHAIN=local`, and the
  Dockerfile switches to it at build time. The Dockerfile also honours
  `TARGETARCH` instead of hard-coding amd64.
- Dependencies flagged by `govulncheck` bumped (grpc, x/crypto, x/net, x/text,
  go-jose, sigstore-go, rekor, timestamp-authority, go-tuf, in-toto-golang).
- Cosign tests no longer touch Docker Hub: they sign and verify images in an
  in-memory registry. The whole Go test suite runs offline.
- Integration test migrated to `action-nebari-sandbox` v3, which provisions the
  sandbox through NIC's `local` (kind) provider instead of k3d + NIC's
  `existing` provider. The `profile` input is gone, the image is loaded with
  `kind load docker-image`, and the explicit `k3d cluster delete` cleanup step
  was dropped — v3 tears the deployment down in its own post step. `nic-version`
  is now pinned to `v0.13.0` rather than tracking `latest`.

### Fixed
- `PROVENANCE_REGISTRY_AUTH` is now actually used, for every registry call
  (digests, tag listing, signatures, SBOM and provenance/referrers lookups).
  It was read but ignored, and referrers lookups were always anonymous. The
  chart now points it at the mounted Secret directory, so both
  `docker-registry` Secrets (`.dockerconfigjson`) and `config.json` keys work.
  A set but unreadable auth or CA file now fails the run at startup.
- `PROVENANCE_REGISTRY_TIMEOUT` is applied to digest lookups (it was read but
  never used).
- A run interrupted by SIGTERM/SIGINT no longer writes a report made of failed
  lookups.
- `metadata.namespacesScanned` is sorted, so identical clusters give identical
  reports.
- Integration test no longer races ArgoCD's first sync. `add-software-pack`'s
  `wait-healthy` returns as soon as the Application exists, because ArgoCD
  aggregates an Application with zero live resources to `Healthy`; the
  subsequent `kubectl wait` then exited `NotFound` immediately (it does not
  retry on a missing object, so its `--timeout` never applied). The workflow
  now waits for the chart's Deployment and CronJob to exist before waiting on
  their conditions.
- Integration test no longer fails at sandbox setup with `configuration
  validation failed: repository field is required`. The v2 action's default
  `nic-version: latest` rolled to NIC v0.13.0, which dropped the
  existing-cluster + `file://` GitOps combination v2 depended on.
- Dashboard branding: overriding `frontend.branding.theme.*.primary` now also
  rebrands button/badge hover and active states and the sidebar tokens.
  `--primary-hover`, `--sidebar-primary`, `--sidebar-primary-foreground` and
  `--sidebar-ring` were hard-coded to the Nebari magenta, so a rebranded
  dashboard flashed magenta on hover. They are now derived from `--primary`,
  `--primary-foreground` and `--ring`, and are additionally documented as
  overridable tokens (`primaryHover`, `sidebarPrimary`,
  `sidebarPrimaryForeground`, `sidebarRing`).

## [0.1.1] - 2026-07-21

### Added
- Dashboard branding and theming support: the logo, title, and theme colors
  can be customized through chart values and are injected into the frontend at
  runtime (no rebuild required).

### Fixed
- Web dashboard no longer errors on page load when there are no reports yet;
  the empty state renders cleanly on a fresh install.

## [0.1.0] - 2026-07-15

First stable release. Supersedes the `0.1.0-alpha.*` pre-releases.

### Added
- Core provenance collector: image discovery, digest resolution, cosign
  signature verification (keyless and key-based), SBOM detection, SLSA
  provenance detection, semver update checking, and Helm release tracking.
- Report output modes selected by `persistence.mode`: HTTP upload to the
  dashboard's internal endpoint (default, RWO-safe), a shared PVC, or a
  ConfigMap.
- Web dashboard: a standalone React + TypeScript SPA (served by nginx) backed
  by an API-only Go service, with in-browser OIDC login (`keycloak-js`, PKCE).
  - Summary stat cards, a report timeline with opt-in unique-image delta
    badges, and a filterable/sortable/paginated image table with a detail
    drawer.
  - Report export as CSV, Markdown, or JSON for the selected report.
  - Admin-gated "Run Scan" button that triggers a one-shot Job from the
    CronJob template, with automatic cleanup of manual Jobs.
- Published collector and dashboard images are signed with keyless cosign
  (Sigstore, via GitHub Actions OIDC - no managed key) and carry SPDX SBOM and
  SLSA provenance (`mode=max`) attestations, all discoverable via the OCI
  referrers API. See "Verifying the Collector Image" in the docs.
- Helm chart: CronJob, RBAC, report storage, dashboard and frontend
  Deployments/Services, and optional NebariApp CRD integration.
- Grafana dashboard example wired to the JSON API via the Infinity datasource.
- Documentation site built with Astro + Starlight and the shared
  `@nebari/starlight` theme, deployed to Cloudflare Pages and routed through
  `packs.nebari.dev/provenance-collector-pack/`, with per-PR previews.
- SecurityContext hardening (runAsNonRoot, readOnlyRootFilesystem, drop ALL
  capabilities).

### Changed
- README restructured operator-first, with refreshed dashboard sections.
- Configuration reference is generated from a single source of truth
  (`internal/configspec`), guarded against drift in CI.
- Integration test runs on `action-nebari-sandbox` (platform profile, v2)
  instead of a bare kind cluster.
- CI actions bumped to Node-24-compatible majors; releases stamp
  `examples/*.yaml` to the released version.

### Fixed
- SBOM and SLSA provenance detection now read both the OCI referrers index and
  BuildKit's in-index attestation manifests, so attestations attached by
  `docker/build-push-action` are discovered and shown in the dashboard. The
  legacy cosign attestation tag (`.att`) is retained as a fallback for images
  attested with older `cosign attest` runs.

### Known limitations
- Air-gapped clusters and private registry mirrors are not yet supported
  (tracked in #1).
