# Merge nebari-security-posture-pack: Security Posture, with the collector as the provenance engine

> Draft PR from `brandonrc:security-posture-merge` into `nebari-dev:main`. Background and the
> agreed plan are in [docs/proposals/0001-merge-with-provenance-collector-pack.md](0001-merge-with-provenance-collector-pack.md).
> The diagrams are in [docs/ARCHITECTURE.md](../ARCHITECTURE.md).

## Summary

This PR merges `nebari-security-posture-pack` into this repository. The result is one pack, one
chart and one UI. Together they answer four questions about a cluster:

- **Provenance:** where did every running image come from?
- **Image vulnerabilities:** what is wrong with each image? Trivy, Grype and Clair scan every image
  and only findings they agree on are reported.
- **Workload posture:** how is each container run? Sixteen checks, mapped to the Kubernetes STIG.
- **Platform controls:** which NIST SP 800-53 controls does the platform implement and prove? 35
  live assertions, with an OSCAL SSP and component definition.

**The Go collector stays and becomes the provenance engine.** The worker image compiles
`collector/` into a binary. Once per scan, the worker runs it in a new single-run mode
(`provenance-collector --once --output <file>`) and loads its report into the database. The
Python port from the posture pack is now only a fallback.

Existing consumers keep working: the report schema, the `/api/reports*` API, the Grafana
dashboard (after a URL change), the `config.*` chart values and the chart name.

Both histories are kept. The posture pack was merged with `git merge --allow-unrelated-histories`,
and every move uses `git mv`, so `git log --follow collector/internal/report/types.go` still
reaches the original commits.

## What changed

| Area | Change |
|---|---|
| `collector/` | The Go module (`cmd/`, `internal/`, `hack/`, `go.mod`, `Makefile`, `.golangci.yml`, `Dockerfile`, `dev/`) moved here unchanged; the module path `github.com/nebari-dev/provenance-collector` is the same. **New:** `--once` and `--output <path\|->` write one report to a file or to stdout. With stdout, logs go to stderr. Without flags nothing changes. Tests: `cmd/provenance-collector/main_test.go` and `report.FileWriter` tests. |
| `api/` | Python FastAPI api and worker from the posture pack. **New:** `provenance/collector.py` loads the collector's report into the database. Engine selection is `PROVENANCE_ENGINE=collector\|python`, and the Python checks are kept as a fallback (see below). The worker Dockerfile has a `golang:1.26` build stage and installs `/usr/local/bin/provenance-collector`. Its build context is now the repository root. |
| `ui/` | React 19 + `nebari-design` admin UI (Overview, Images, Vulnerabilities, Workloads, Namespaces, Posture checks, Supply chain, Compliance, Reports, Scans, Settings). It replaces `frontend/`. |
| `chart/` | The posture chart. Its **name stays `provenance-collector`** and the version is 0.2.0. It deploys api, worker, ui, Postgres, Trivy and Clair. `templates/_compat.tpl` handles values from 0.1.x. The removed templates are the CronJob, the dashboard Deployment, Services and RBAC, and the frontend. |
| Values | `config.*` keys still work as aliases: `verifySignatures`, `cosignPublicKey`, `checkSBOM`, `checkProvenance`, `checkUpdates`, `updateLevel`, `skipPrerelease`, `helmEnabled`, `registryTimeout`, `excludeNamespaces` and `clusterName`. Values whose behaviour no longer exists stop the render with a message that names the replacement: `schedule`, `persistence.mode`, `frontend.keycloak.url`, and a non-empty `config.namespaces`. Other old keys are ignored and listed in NOTES.txt. |
| `examples/` | Nebari, standalone and ArgoCD values for the merged chart. The Grafana dashboard now points at `<release>-web-internal`. |
| Workflows | **lint:** golangci-lint and the configspec/gendocs checks in `collector/`; chart lint, template and kubeconform for six value sets; a values-compatibility job; eslint and tsc for the UI; shellcheck. **test:** go test with `-race`, pytest with a Postgres service, vitest and the UI build. **build-image:** four images (api, worker, ui, standalone collector), each with an SBOM, SLSA provenance and a keyless signature. **release:** the chart (still published as `provenance-collector`) plus collector binaries for linux and darwin on amd64 and arm64. **test-integration:** deploys the merged chart in the NIC sandbox, runs a scan, checks that the collector's report was loaded, and checks `/api/reports/latest`. **docs:** adds a `sync-docs.py --check` step. actionlint 1.7.7 reports no errors. **None of the workflows has run on GitHub yet.** |
| Docs | The Astro site has new pages: Migrating from 0.1.x, Configuration (chart values), Compliance architecture, Controls, Reports, Supply-chain provenance, Scoring, Design, Decisions and Proposal 0001. The last eight are generated from `docs/*.md` by `scripts/sync-docs.py`, and Mermaid diagrams render. These existing pages were rewritten: index, quick-start, architecture, web UI, storage, report-schema, NebariApp CRD, verifying images. The generated environment-variable reference moved to `collector-environment.md`. |
| Meta | README uses the posture text with this repo's header block, plus a migration section. CHANGELOG has an Unreleased entry. In `pack-metadata.yaml`, `display_name` is Security Posture and `level` is `experimental`. `name` is still the repo name. |

### How the collector engine works

1. The worker runs `provenance-collector --once` with its own ServiceAccount, using the
   `PROVENANCE_*` settings derived from the chart values.
   - The worker's existing reader ClusterRole and the optional `helm-releases` ClusterRole already
     cover the collector's RBAC needs. The old chart's `configmaps` write permission was only for
     the ConfigMap output and is not added.
   - If the cosign key is PEM text, the worker writes it to a temporary file and passes that to
     the collector. KMS keys and keyless identities stay with the cosign CLI, which re-verifies
     the digests the collector reports as signed.
2. Report records are matched to images in this order:
   1. digest and namespace;
   2. namespace and spec image;
   3. digest in any namespace.

   The collector reports the ReplicaSet or Job that owns a container, while the inventory records
   the controller (Deployment or CronJob). So the workload only breaks ties, using a prefix match.
   Records that match no image are logged.
3. Results go into the same tables as the Python checks (`image_provenance`, `images.provenance`,
   `helm_releases`) and are marked with `details.engine=collector` and the collector version.
4. Fallbacks:
   - An image goes through the **Python checks in the same scan** if none of its records has a
     digest (the collector could not reach its registry, for example the node-local
     `localhost:32000`) or if the report does not include it.
   - If the binary exits non-zero, times out or writes an unreadable report, Python handles every
     image.
5. **Image update checks stay in Python with both engines** (the collector runs with
   `PROVENANCE_CHECK_UPDATES=false`). The collector's semver ordering suggests CI build numbers,
   dates and other image variants as the newest version; on grace it brought back
   cert-manager `608111629` and grafana `9799770991`. The Python check filters candidate tags
   (`5ac1e7f`). Porting that filter to `collector/internal/registry/updates.go` is an open question
   below.

## Compatibility

- **Report schema and API.** These routes are unchanged: `/api/reports`, `/api/reports/latest`,
  `/api/reports/{filename}`, `/api/export?format=csv|markdown`, `/api/me`, `/api/scan`, `/healthz`.
  They return the same field order, `omitempty` behaviour and Go `MarshalIndent` formatting,
  generated from the latest completed scan.
- **Grafana.** The Infinity datasource URL changes from `<fullname>-web` to
  `<fullname>-web-internal`. To keep the old host name, set
  `provenance.compat.internalService.name: provenance-collector-web`.
- **Chart name.** It stays `provenance-collector`, so ArgoCD Applications keep working. A plain
  `helm upgrade` from a release that used a different chart name also works: grace went from
  `nebari-security-posture-pack-0.1.0` to `provenance-collector-0.2.0` with no uninstall.
- **Standalone collector.** It still works as a binary (now also a release asset) and as the
  `quay.io/nebari/provenance-collector` image. The standalone Go dashboard still builds from
  `collector/cmd/dashboard`, but the chart no longer deploys it.

## Migration (breaking for 0.1.x installs)

The full guide is [docs/src/content/docs/migrating.md](../src/content/docs/migrating.md), and the
README has a short version. In brief:

- **Schedule.** `schedule` (cron) is replaced by `scanner.intervalHours`. Setting `schedule` stops
  the render with that instruction.
- **Storage.** `persistence.mode` (http, pvc or configmap) is replaced by Postgres. History is kept
  per scan, for the last 10 scans. Old report files are **not** imported; the guide shows how to
  copy them out first.
- **Auth.** In-browser PKCE login is replaced by gateway login plus API JWT verification, gated on
  `adminGroups`. `/api/reports*` on the public listener now requires the admin group (it was any
  logged-in user). Grafana uses the unauthenticated compat Service, which a NetworkPolicy limits to
  `allowedNamespaces`.
- **Helm releases.** `config.helmEnabled` still works, but the new default is `false`, because the
  feature grants cluster-wide Secrets get/list.
- **Images.** `quay.io/nebari/provenance-collector-pack-{api,worker,ui}`.

## How it was tested

### Test suites (branch HEAD)

| Suite | Result |
|---|---|
| `collector/`: `go vet ./...`, `go test ./...` (golang:1.26) | all packages ok, including the new `cmd/provenance-collector` and `report.FileWriter` tests |
| `collector/`: golangci-lint v2.13.2, `hack/checkenvs`, `hack/gendocs --check` | 0 issues, 30 env vars covered, docs up to date |
| `api/`: pytest with `CONTROLS_ENGINE_ENABLED=false` and `TEST_DATABASE_URL` (throwaway Postgres 16) | **537 passed, 1 skipped**. Covers the adapter (matching, folding, absence semantics, Helm), the subprocess runner against a fake binary (success, exit 1, bad JSON, timeout), engine selection, keyless post-verification, key-file handoff, the Python update check under the collector engine, and Postgres integration tests for collector ingest with fallback and for binary failure |
| `ui/`: eslint, `tsc -b --noEmit`, vitest (node:22-alpine) | clean, clean, **59/59**. The flaky Overview controls-tile test now waits for the settled query; the same fix went to the posture repo |
| Chart: `helm lint`; `helm template` with the defaults, `deploy/grace/values.yaml`, `examples/nebari-values.yaml`, `examples/standalone-values.yaml`, NebariApp + SecurityPolicy, and external DB; kubeconform `-strict` | all pass. The compat aliases and the four render failures were checked by hand and are covered in `lint.yaml` |
| Docs: vitest, `astro build`, `scripts/check-links.sh` at `BASE=/` and `BASE=/provenance-collector-pack/`, `sync-docs.py --check` | 10/10 tests, 20 pages built, `LINKS_OK` at both bases, in sync |
| Workflows | actionlint clean. **Not run on GitHub.** |

### Grace (single-node MicroK8s, namespace `security-posture`, `https://security.100-89-230-107.sslip.io`)

The images were built from a clean worktree of the branch HEAD with
`deploy/grace/build-push.sh`; the deployed tag is `b727589-1791000393`. They were deployed with
`deploy/grace/deploy.sh`.

**Helm upgrade with the renamed chart:**

- Revision 10 (`nebari-security-posture-pack-0.1.0`) was upgraded to revision 11, then 12
  (`provenance-collector-0.2.0`). No uninstall was needed.
- `nameOverride: nebari-security-posture-pack` in the grace values keeps every resource name,
  every selector label and all 4 PVCs unchanged.

**Pods and NebariApp:**

- The 6 pods (api, worker, ui, postgres, trivy, clair) are Running.
- NebariApp conditions: Ready, RoutingReady and AuthReady are True. TLSReady is False with reason
  `ClusterIssuerNotConfigured`; this was already the case before this deploy (the app uses the
  shared HTTPS listener).

**Login gate:**

- Anonymous requests get a 302 to Keycloak.
- `admin`: `/api/v1/summary` 200, `/api/reports/latest` 200, and `/api/me` returns
  `canRunScan: true`.
- `alice`: the gateway returns 403 ("RBAC: access denied") on every route.
- `/healthz` returns 200 without login.

**Forced scan #8:** 79 images, 0 failed, 6 min 7 s. Cluster score **27.7 (F)**: vulnerability 8.2,
configuration 74.8, supply chain 27.4. The worker log and the scan log show:

```
provenance engine=collector (b727589-1791000393): 99 report record(s) -> 58 image(s) ingested
  (matched by digest 62, by image 37, by digest in another namespace 0); 0 unmatched record(s);
  19 image(s) unresolved by the collector and 2 not in its report go to engine=python; 17 helm release(s)
provenance: 79 image(s), 19 signed, 10 with SBOM, 35 with provenance, 0 registry error(s);
  17 helm release(s) in 364s (engine=collector 58, engine=python 21)
```

The 19 images the collector could not resolve use registries the collector cannot reach from the
pod (`localhost:32000`, the private-CA `artifacts.*` registry) or were rate-limited by Docker Hub.
The Python fallback scored all of them with 0 registry errors.

**Assertions:** run #4 was triggered by scan 8 and finished `done`. Result: 19 pass, 15 fail,
1 unknown. Moderate baseline: 20 of 287 implemented, 6 partial, 62 not implemented, 199 inherited.

**Reports:** all six auto-generated types for scan 8 finished `done`: POA&M xlsx, STIG CKLB, SAR
PDF, OSCAL AR, inventory xlsx, vulnerability CSV. All three OSCAL documents validate against the
official NIST OSCAL 1.1.2 JSON schemas with **0 errors**:

- assessment-results: 87 MB
- system-security-plan: 425 KB
- component-definition: 46 KB

**Compat API from the `monitoring` namespace** (from inside the Grafana pod, to
`security-posture-web-internal.security-posture.svc:8080`):

- `/api/reports/latest` returns 200 with the scan 8 report.
- `/api/reports` lists the reports.
- `/api/export?format=csv` and `/healthz` work.
- `/api/v1/summary` returns 404: the compat listener exposes nothing else.

**Effect of the update-check fix on the supply-chain score:**

| Scan | Engine and code | Supply-chain score | CI-number or date "newest" tags | Registry errors |
|---|---|---|---|---|
| 6 | Python, before `5ac1e7f` | **27.3** | 10 | 6 |
| 7 | Collector, including the collector's own update check, during Docker Hub 429s | 44.4 (inflated: 23 rate-limited images count as *unknown*, with no penalty) | 8 | 23 |
| 8 | Collector, with the Python update check (this PR) | **27.4** | **0** | 0 |

- Across the 70 images with no registry error in both scan 6 and scan 8, the mean image
  supply-chain score went from 25.5 to 26.9. Nine images went up: the bogus major-update penalty
  (−25) became a real minor update (−15) or disappeared. Examples are cert-manager ×3, kuberay
  operator, nebari-landing, calico, coredns, and the `checkmaite-api:2.56.0-r*` variant tags.
- One image went down. `alpine:3.20` lost 15 for "no SLSA provenance", because the collector does
  not find the attestation the Python checks found, and gained 10 because its date-tag "major
  update" became a minor update.
- The cluster score barely moved (27.3 to 27.4), because the 6 images that were *unknown* in scan 6
  are now scored.

### Screenshots

Live, after scan 8, logged in as `admin`:

- Overview: [deploy/grace/screenshots/overview.png](https://github.com/brandonrc/provenance-collector-pack/blob/security-posture-merge/deploy/grace/screenshots/overview.png)
- Supply chain: [deploy/grace/screenshots/supply-chain.png](https://github.com/brandonrc/provenance-collector-pack/blob/security-posture-merge/deploy/grace/screenshots/supply-chain.png)
- Controls (AC-3 evidence expanded): [deploy/grace/screenshots/compliance-controls.png](https://github.com/brandonrc/provenance-collector-pack/blob/security-posture-merge/deploy/grace/screenshots/compliance-controls.png)

## Open questions for the maintainers

1. **License.** The `LICENSE` file in both repositories is the Apache License 2.0 text. This repo's
   README badge says BSD-3-Clause, and the merge brief also assumed BSD-3. I left `LICENSE`
   unchanged (Apache-2.0) and kept the badge from this repo's header. Which is right? The badge or
   the file must change before release.
2. **Repo and chart name.** Both are unchanged here (repo `provenance-collector-pack`, chart
   `provenance-collector`, display name "Security Posture"). Should we rename now (proposal: repo
   and chart `nebari-security-posture-pack`) or after the next release?
3. **Update-check semantics in Go.** Should the candidate-tag filter (no CI build numbers or dates,
   same variant suffix, `maxMajorJump`) be ported to `collector/internal/registry/updates.go`, so
   that update checks can also move to the collector?
4. **Detection differences.** The collector does not read sigstore-bundle signatures, `.sbom` tags,
   or SLSA provenance carried in cosign `.att`; the Python checks do (see `alpine:3.20` above).
   Should these move into the collector, or should the worker keep supplementing it?
5. **Python port.** When should the Python provenance checks be deleted? They are now only a
   fallback for registries the collector cannot reach, and that is the main thing keeping them.
6. **Ownership.** `owner` and `product_owner` in `pack-metadata.yaml`, and CODEOWNERS (which is
   currently the posture repo's file, `@geraci`).
7. **Maturity.** Should the pack go back to `experimental` until the integration test covers the
   combined chart, as the proposal says?

## Known gaps

- None of the GitHub workflows have run. The integration workflow is new and untested, and it has
  to build the worker image, which is large.
- The compat report's `metadata.collectorVersion` is the api package version (`0.1.0+posture`), not
  the collector's version.
- With the collector engine, signature, SBOM and provenance results are not reused across scans per
  digest: `recheckHours` applies only to the Python path. Every image is re-checked each scan, so
  together with the Python fallback the collector engine sends more registry requests than the
  Python engine alone did. On grace, Docker Hub rate limits (429) were hit while images were being
  built and scanned in the same hour.
- `docs/screenshots/dashboard-*.png` (the old dashboard) were removed; the README uses
  `ui/screenshots/overview-light.png`.

## Checklist

- [x] Both histories kept (`--allow-unrelated-histories`, `git mv` for every move)
- [x] Collector `--once` / `--output` with tests; default behaviour unchanged
- [x] Worker bundles the collector; engine selector with Python fallback; adapter tests
- [x] Chart values compatibility (aliases and failures with clear messages) and migration guide
- [x] Examples and Grafana dashboard updated
- [x] Workflows for Go, Python, UI, chart, docs, four images and release assets (actionlint clean)
- [x] Docs site builds, links checked, Mermaid renders
- [x] Go, api, ui, chart and docs suites green locally
- [x] Deployed to grace by helm upgrade in place; scan, assertions, reports and OSCAL validated; compat API checked from `monitoring`; auth gate checked
- [ ] Workflows green on GitHub
- [ ] License question resolved
- [ ] Maintainers' decision on naming, Go update-check semantics and ownership

🤖 Generated with [Claude Code](https://claude.com/claude-code)
