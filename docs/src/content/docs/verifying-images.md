---
title: Verifying Images
description: Verify the cosign signatures, SLSA provenance and SBOM attached to the published images.
---

Every image the repository publishes is signed with **keyless cosign**
(Sigstore) using the `build-image.yaml` GitHub Actions OIDC identity - there are
no long-lived signing keys - and carries an SPDX SBOM and a SLSA provenance
attestation as OCI referrers.

| Image | quay.io | GHCR |
|---|---|---|
| api | `quay.io/nebari/provenance-collector-pack-api` | `ghcr.io/nebari-dev/provenance-collector-pack/api` |
| worker (bundles the Go collector) | `quay.io/nebari/provenance-collector-pack-worker` | `ghcr.io/nebari-dev/provenance-collector-pack/worker` |
| ui | `quay.io/nebari/provenance-collector-pack-ui` | `ghcr.io/nebari-dev/provenance-collector-pack/ui` |
| standalone collector + dashboard | `quay.io/nebari/provenance-collector` | `ghcr.io/nebari-dev/provenance-collector-pack` |

## Verify a signature

Requires [cosign](https://docs.sigstore.dev/) v3+. **Identity pinning is
mandatory**: without `--certificate-identity-regexp` and
`--certificate-oidc-issuer`, cosign accepts a signature from any identity.

```bash
VERSION=0.2.0
for img in api worker ui; do
  cosign verify \
    --certificate-identity-regexp '^https://github.com/nebari-dev/provenance-collector-pack/\.github/workflows/build-image\.yaml@refs/tags/v.*$' \
    --certificate-oidc-issuer 'https://token.actions.githubusercontent.com' \
    "quay.io/nebari/provenance-collector-pack-${img}:${VERSION}"
done
```

Images built from `main` are signed by the same workflow with
`@refs/heads/main`; adjust the regexp to verify those. A successful run prints
the verified certificate's identity and OIDC issuer.

## Inspect the SBOM and provenance

```bash
cosign tree quay.io/nebari/provenance-collector-pack-worker:0.2.0
```

This lists the attached SPDX SBOM and SLSA provenance attestations. The pack
detects the same referrers when it scans the cluster, so its own images show as
signed, with SBOM and SLSA provenance, on the Supply chain page.

## Verify images on the cluster

To have the pack verify (not only detect) signatures of the images it scans,
configure a key or a keyless identity:

```yaml
provenance:
  verifySignatures: true
  cosign:
    certificateIdentityRegexp: '^https://github.com/nebari-dev/provenance-collector-pack/\.github/workflows/build-image\.yaml@refs/(tags|heads)/.*$'
    certificateOidcIssuerRegexp: '^https://token\.actions\.githubusercontent\.com$'
```

Images signed under another identity then show as signed but unverified. See
[Supply-chain provenance](/provenance/#configuration).
