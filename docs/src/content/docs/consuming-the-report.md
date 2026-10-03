---
title: Consuming the Report
description: How another tool runs the collector binary and reads its versioned report.
---

The collector is usable as a building block: another tool can run the released
binary once against a cluster and read one JSON document back, without
deploying the chart, the dashboard, a PVC or a ConfigMap. The first consumer is
the [Nebari security posture pack](https://github.com/nebari-dev/nebari-security-posture-pack),
which uses the collector as its provenance engine.

## Run it

The binary always performs a single collection and exits. Pass `--output` to
write the report somewhere other than the `PROVENANCE_REPORT_OUTPUT` sink:

```bash
# Report on stdout, logs (JSON) on stderr.
provenance-collector --output - > report.json

# Report written atomically to a file (temp file + rename in the same
# directory), so a reader never sees a half-written document.
provenance-collector --output /work/report.json
```

With `--output`, `PROVENANCE_REPORT_OUTPUT` and `PROVENANCE_REPORT_UPLOAD_URL`
are ignored. Everything else is configured with the usual `PROVENANCE_*`
environment variables (see [Configuration](/configuration/)); `KUBECONFIG`
selects the cluster, or the in-cluster service account is used when it is
unset.

Exit codes:

| Code | Meaning |
|---|---|
| `0` | Report written. It may still carry `warnings` (see below). |
| `1` | Collection failed: no cluster access, image discovery failed, unreadable registry auth/CA file, or the report could not be written. Nothing usable was produced. |
| `2` | Bad command-line flags. |

Send `SIGTERM` (or `SIGINT`) to stop a run early: in-flight registry calls are
cancelled and the collector exits 1 without writing a report.

A consumer that runs the binary as a subprocess should pass only the
environment it needs (not its own secrets), set a wall-clock timeout, and
terminate the process (SIGTERM, then SIGKILL) if its own job is cancelled.

## Read it

1. Parse the JSON and check `metadata.schemaVersion`. Accept the major version
   you were written against (today `1`), refuse others, and ignore fields you
   don't recognise: minor versions only add fields.
2. Validate against the JSON Schema if you want a strict contract check. The
   schema is committed at
   [`schema/report.schema.json`](https://github.com/nebari-dev/provenance-collector-pack/blob/main/schema/report.schema.json)
   and attached to every GitHub Release as `report.schema.json`.
3. Read `warnings`. An empty or missing list means nothing was skipped. A
   non-empty list means the report is usable but incomplete, for example:
   - `helm: listing helm releases in namespace X: ... forbidden` - no Secret
     read access there, so that namespace's releases are missing (as opposed
     to "there are none").
   - `image R: digest not resolved: ...` - the registry was unreachable or
     refused the credentials; that image record has no `digest`.
   - `image R: update check failed: ...` - tag listing failed (often rate
     limiting).

For contract tests, use
[`testdata/report.golden.json`](https://github.com/nebari-dev/provenance-collector-pack/blob/main/testdata/report.golden.json).
It is produced by an end-to-end run of the collector's `run()` against a fake
cluster and stub registries (`go test ./cmd/provenance-collector -run Golden`),
includes every optional field the collector emits plus `warnings`, and is validated against the schema in CI, so a
field rename on the collector side breaks the collector's own build before it
reaches you. Pin a collector release and copy the fixture from the same tag.

## Private and internal registries

Every registry call (digests, tags, signatures, SBOM and provenance lookups)
uses the same settings:

| Variable | Use |
|---|---|
| `PROVENANCE_REGISTRY_AUTH` | Docker `config.json`, or a directory holding `config.json` / `.dockerconfigjson` (a mounted `docker-registry` Secret). Registries it doesn't list fall back to `$DOCKER_CONFIG` and credential helpers. |
| `PROVENANCE_REGISTRY_CA_FILE` | PEM bundle of extra CAs for registries behind a private CA. |
| `PROVENANCE_REGISTRY_INSECURE` | Comma-separated `host[:port]` list allowed over plain HTTP or with unverified TLS. Only those hosts. |

A set but unreadable auth or CA file stops the collector at startup (exit 1)
instead of silently falling back to anonymous pulls.

## Verify the binary

Release tarballs carry a SLSA build provenance attestation, and
`checksums.txt` is signed keyless with cosign:

```bash
gh attestation verify provenance-collector_<version>_linux_amd64.tar.gz \
  --repo nebari-dev/provenance-collector-pack

cosign verify-blob checksums.txt \
  --bundle checksums.txt.sigstore.json \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  --certificate-identity-regexp '^https://github.com/nebari-dev/provenance-collector-pack/\.github/workflows/release\.yaml@'
sha256sum --check --ignore-missing checksums.txt
```
