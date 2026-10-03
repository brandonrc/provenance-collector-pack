## What's New

-

## Installation

```bash
helm repo add nebari https://nebari-dev.github.io/helm-repository/
helm repo update
helm install provenance-collector nebari/provenance-collector \
  --version ${VERSION} \
  --namespace provenance-system \
  --create-namespace \
  --set nebariapp.enabled=true \
  --set nebariapp.hostname=security.example.com \
  --set 'auth.issuers={https://keycloak.example.com/auth/realms/nebari}'
```

Upgrading from provenance-collector-pack 0.1.x: `schedule`, `persistence.mode`
and `frontend.keycloak.url` are retired (the render fails with the replacement),
`config.*` keys are still accepted. See the migration guide:
https://packs.nebari.dev/provenance-collector-pack/migrating/

<details>
<summary>Other install methods</summary>

### From OCI (quay.io)

```bash
helm install provenance-collector \
  oci://quay.io/nebari/charts/provenance-collector \
  --version ${VERSION} \
  --namespace provenance-system \
  --create-namespace \
  -f examples/nebari-values.yaml
```

### From source

```bash
git clone https://github.com/nebari-dev/provenance-collector-pack.git
cd provenance-collector-pack
git checkout v${VERSION}
helm dependency build chart/
helm install provenance-collector chart/ \
  --namespace provenance-system \
  --create-namespace \
  -f examples/nebari-values.yaml
```

### Container images

```
quay.io/nebari/provenance-collector-pack-api:${VERSION}
quay.io/nebari/provenance-collector-pack-worker:${VERSION}
quay.io/nebari/provenance-collector-pack-ui:${VERSION}
quay.io/nebari/provenance-collector:${VERSION}   # standalone collector + dashboard
```

### Collector binary

`provenance-collector_${VERSION}_<os>_<arch>.tar.gz` (linux/darwin, amd64/arm64)
is attached to this release: `provenance-collector --once --output -` writes one
report to stdout using the current kubeconfig.

</details>

> **Note:** This is a pre-release. APIs and report format may change before v1.0.
