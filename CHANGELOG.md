# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Merged: nebari-security-posture-pack (Security Posture)

This repository now also contains `nebari-security-posture-pack`, merged with
`git merge --allow-unrelated-histories` so both histories are kept
([proposal 0001](docs/proposals/0001-merge-with-provenance-collector-pack.md)).
The pack is now the **Security Posture** pack: provenance becomes one of four
evidence layers next to three-scanner vulnerability consensus (Trivy, Grype,
Clair), Kubernetes STIG posture checks and live NIST SP 800-53 control
assertions, with POA&M, STIG checklist, SAR and OSCAL reports.

#### Added
- `collector/`: the Go module (`cmd/`, `internal/`, `hack/`, `go.mod`,
  `Makefile`, `Dockerfile`, `dev/`) moved here with `git mv`; the module path
  `github.com/nebari-dev/provenance-collector` is unchanged.
- Collector single-run mode: `provenance-collector --once [--output <path|->]`
  writes the report JSON to a file or stdout (logs then go to stderr) instead
  of the `PROVENANCE_REPORT_OUTPUT` sink. Default behaviour is unchanged.
- The collector is the provenance engine of the pack: the worker image
  bundles the binary and ingests its report once per scan
  (`provenance.engine: collector`, Python checks as per-image and whole-run
  fallback). Collector binaries for linux/darwin x amd64/arm64 are attached
  to releases.
- `api/` (FastAPI api + worker), `ui/` (React 19 + `nebari-design` admin UI),
  compliance reports, controls engine; docs pages for compliance architecture,
  controls, reports, provenance, scoring, design and decisions; a migration
  guide.
- Chart values compatibility layer (`chart/templates/_compat.tpl`): `config.*`
  keys are aliases of `provenance.*` / `scanner.excludedNamespaces` /
  `clusterName`.

#### Changed
- **Breaking:** the chart (name kept: `provenance-collector`, version 0.2.0)
  deploys api, worker, ui, Postgres, Trivy and Clair instead of the CronJob,
  the Go dashboard and the frontend. `schedule`, `persistence.mode`,
  `frontend.keycloak.url` and a non-empty `config.namespaces` fail the render
  with a pointer to `scanner.intervalHours` / Postgres / gateway auth /
  `scanner.excludedNamespaces`.
- **Breaking:** auth is gateway login + API JWT verification gated to
  `adminGroups`, including `/api/reports*` on the public listener. Grafana
  reads the unauthenticated `<release>-web-internal:8080/api/reports/latest`
  (`provenance.compat.internalService.enabled`); the Grafana example uses it.
- `config.helmEnabled` / `provenance.helmReleases.enabled` defaults to false
  (cluster-wide Secrets RBAC is opt-in).
- Images: `quay.io/nebari/provenance-collector-pack-{api,worker,ui}`; the
  standalone `quay.io/nebari/provenance-collector` image is still built.
- Workflows: lint/test cover `collector/` (Go), `api/` (pytest + Postgres),
  `ui/` (eslint, tsc, vitest) and the chart (kubeconform, values compat);
  build-image builds four images; the integration test deploys the merged
  chart and checks the collector ingest and `/api/reports/latest`.
- `pack-metadata.yaml`: display name "Security Posture", level experimental.
- Docs: the generated environment reference moved to
  `collector-environment.md`; `configuration.md` documents the chart values.

#### Removed
- `frontend/` (the standalone React table SPA and its nginx image) and its
  Playwright e2e specs (`test/e2e/`): replaced by `ui/`.
- Chart templates for the CronJob, dashboard Deployment/Services/RBAC and
  frontend.

### Changed (provenance-collector-pack, before the merge)
- Integration test migrated to `action-nebari-sandbox` v3, which provisions the
  sandbox through NIC's `local` (kind) provider instead of k3d + NIC's
  `existing` provider. The `profile` input is gone, the image is loaded with
  `kind load docker-image`, and the explicit `k3d cluster delete` cleanup step
  was dropped — v3 tears the deployment down in its own post step. `nic-version`
  is now pinned to `v0.13.0` rather than tracking `latest`.

### Fixed
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
