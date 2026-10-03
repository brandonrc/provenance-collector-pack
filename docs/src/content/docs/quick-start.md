---
title: Quick Start
description: Install the Security Posture pack, run the first scan, and point Grafana at the provenance report.
---

On a Nebari (NIC) cluster the pack is installed through ArgoCD like every other
software pack; a standalone install works on any Kubernetes cluster for
evaluation and development.

## Requirements

| | Minimum | Notes |
|---|---|---|
| Kubernetes | 1.26+ | |
| `helm` | 3.14+ | The chart depends on the `nebari-app` library chart (`helm dependency build`). |
| Permissions | `cluster-admin` to install | The chart creates read-only ClusterRoles for the api/worker ServiceAccount. |
| Capacity | ~4 CPU / 8 GiB free, ~45 GiB storage | Worker (grype + trivy client + skopeo + collector), Clair, Trivy server, Postgres. See [Storage](/storage-modes/). |
| Nebari | nebari-operator, Envoy Gateway, Keycloak | Only with `nebariapp.enabled: true`. |

## Nebari install (operator-managed)

A complete ArgoCD `Application` lives at
[`examples/argocd-application.yaml`](https://github.com/nebari-dev/provenance-collector-pack/blob/main/examples/argocd-application.yaml)
and [`examples/nebari-values.yaml`](https://github.com/nebari-dev/provenance-collector-pack/blob/main/examples/nebari-values.yaml)
holds the same values for a plain `helm install`. The namespace must carry the
label `nebari.dev/managed=true`, otherwise the operator ignores the `NebariApp`:

```bash
kubectl create namespace provenance-system
kubectl label namespace provenance-system nebari.dev/managed=true

helm dependency build chart/
helm install provenance-collector chart/ \
  -n provenance-system \
  -f examples/nebari-values.yaml \
  --set nebariapp.hostname=security.<your-domain> \
  --set 'auth.issuers={https://keycloak.<your-domain>/auth/realms/nebari,http://keycloak-keycloakx-http.keycloak.svc.cluster.local:80/auth/realms/nebari}'
```

The values most users adjust:

```yaml
nebariapp:
  enabled: true
  hostname: security.<your-domain>   # public URL of the UI
adminGroups: [admin]                 # Keycloak groups allowed in (gateway + API)
auth:
  issuers: [...]                     # accepted token issuers; empty = every token rejected
scanner:
  intervalHours: 6                   # scheduled scans (was `schedule:` in 0.1.x)
provenance:
  helmReleases:
    enabled: true                    # Helm release discovery (cluster-wide Secrets get/list)
  compat:
    internalService:
      enabled: true                  # unauthenticated /api/reports* for Grafana
```

Check the deployment:

```bash
kubectl get nebariapp -n provenance-system -o wide   # expect Ready, AuthReady, RoutingReady
kubectl get pods -n provenance-system                # api, worker, ui, postgres, trivy, clair
```

Then open `https://security.<your-domain>`. Members of `adminGroups` see the
Overview; everybody else is stopped by the gateway (or gets the API's 403 page).
Upgrading an existing 0.1.x install: read [Migrating from 0.1.x](/migrating/) first.

## Standalone install

Use [`examples/standalone-values.yaml`](https://github.com/nebari-dev/provenance-collector-pack/blob/main/examples/standalone-values.yaml):
no operator, no gateway, no Keycloak.

:::caution
The standalone example sets `auth.mode: disabled`: the API performs **no
authentication** and anyone who reaches the ui Service is an admin. Keep it on a
trusted network, or front it with your own authenticating proxy and use
`auth.mode: oidc`.
:::

```bash
helm dependency build chart/
helm install provenance-collector chart/ \
  -n provenance-system --create-namespace \
  -f examples/standalone-values.yaml

kubectl port-forward -n provenance-system svc/provenance-collector-ui 8080:80
open http://localhost:8080
```

## The first scan

The worker scans on start-up and then every `scanner.intervalHours`. To scan now,
use **Scan now** in the UI (Scans page), or the API:

```bash
# through the gateway, with an admin token
curl -X POST https://security.<your-domain>/api/v1/scans \
  -H "Authorization: Bearer $TOKEN" -H 'content-type: application/json' -d '{"force": true}'
```

The first scan takes longer than later ones: the worker downloads the grype
database, Trivy and Clair fill their vulnerability databases, and every image is
mirrored once. The scan log (Scans page) shows each stage, including the
provenance engine line:

```
provenance engine=collector (0.2.0): 74 report record(s) -> 61 image(s) ingested ...
```

## Grafana

With `provenance.compat.internalService.enabled: true`, the provenance report is
served without auth on an in-cluster Service that only the namespaces in
`allowedNamespaces` can reach. Import
[`examples/grafana-dashboard.json`](https://github.com/nebari-dev/provenance-collector-pack/blob/main/examples/grafana-dashboard.json)
(Infinity datasource); its URL is

```
http://<release>-web-internal.<namespace>.svc:8080/api/reports/latest
# e.g. http://provenance-collector-web-internal.provenance-system.svc:8080/api/reports/latest
```

## Uninstall

```bash
helm uninstall provenance-collector -n provenance-system
```

The worker, Trivy and reports PVCs are part of the release and are deleted with
it. The Postgres StatefulSet's PVC (`data-<fullname>-postgres-0`) and the generated
database password Secret (`helm.sh/resource-policy: keep`) survive; delete them
by hand for a clean slate.
