# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Changed
- Web dashboard: `frontend/` is replaced by the Nebari design-system UI shared
  with the Nebari Security Posture pack, running in provenance-only mode.
  - New pages: Overview with an A–F supply-chain score, an Images inventory
    with a per-image detail page (Used by, Supply chain with score
    deductions), a Supply chain page with the Helm releases table and
    unsigned / outdated lists, Reports (history, View to load an earlier
    report, JSON / CSV / Markdown downloads per report) and Scans (Run scan,
    followed until the new report lands).
  - Login is unchanged: keycloak-js with PKCE S256 from
    `frontend.keycloak.{url,realm,clientId}`. `frontend.title` and
    `frontend.branding.*` keep working.
  - The image is now based on `nginx-unprivileged` (uid 101, port 8080) and
    renders its own nginx config from `NGINX_PORT` / `API_UPSTREAM`; the chart
    no longer mounts an `nginx.conf` and drops the `/var/run` emptyDir.
    `config.json` gains `"mode": "provenance"`.
  - Frontend CI: ESLint + `tsc` replace Biome; Vitest runs with a coverage
    gate; a new Playwright job runs against the mock bundles; the
    integration e2e specs (`test/e2e`) and the auto-captured screenshots
    (`docs/screenshots/dashboard-*-{light,dark}.png`) follow the new UI.
  - Kept in sync with the posture pack's `ui/`: its SCAP / STIG views come
    along but are posture-mode only (hidden, `/stig/*` redirects to `/`), and
    the runtime image moves to `nginx-unprivileged:1.31-alpine-slim`, pinned by
    digest, with `apk upgrade` at build.

- Integration test migrated to `action-nebari-sandbox` v3, which provisions the
  sandbox through NIC's `local` (kind) provider instead of k3d + NIC's
  `existing` provider. The `profile` input is gone, the image is loaded with
  `kind load docker-image`, and the explicit `k3d cluster delete` cleanup step
  was dropped — v3 tears the deployment down in its own post step. `nic-version`
  is now pinned to `v0.13.0` rather than tracking `latest`.

### Removed
- The frontend image's `BRANDING_*` / `KEYCLOAK_*` environment overrides
  (`docker-entrypoint.sh`). Outside Kubernetes, mount a `config.json` over
  `/usr/share/nginx/html/config.json` instead.
- The saved theme preference moves from the `provenance:themeMode` to the
  `nebari:themeMode` localStorage key, so users see the System default once.

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
