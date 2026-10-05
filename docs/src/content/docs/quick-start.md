---
title: Quick Start
description: Install the Provenance Collector, trigger a scan, and view your first provenance report.
---

The Provenance Collector is normally installed by the
[Nebari Operator](https://github.com/nebari-dev/nebari-operator) as part of
NIC's foundational software — you don't run any `helm` commands yourself, the
operator and ArgoCD do it for you. The operator-managed path is the supported
default; the standalone install below exists for vanilla Kubernetes clusters
and local development.

## Operator-managed install (default)

A complete ArgoCD `Application` manifest lives at
[`examples/argocd-application.yaml`](https://github.com/nebari-dev/provenance-collector-pack/blob/main/examples/argocd-application.yaml)
and is auto-stamped to the latest released chart version on every release.
Apply it from your gitops repo or directly:

```bash
kubectl apply -f examples/argocd-application.yaml
```

The values most users adjust:

```yaml
nebariapp:
  enabled: true                       # register the pack with the Nebari Operator
  hostname: provenance.<your-domain>  # public URL the UI responds on

webUI:
  enabled: true                       # dashboard API + report-upload endpoint; required when persistence.mode=http
  features:
    timelineDeltas: false             # opt-in; show a +N/-N column on the Reports page

frontend:
  enabled: true                       # standalone React UI (nginx); serves the SPA and proxies /api to the dashboard
  keycloak:
    url: https://keycloak.<your-domain>  # required when frontend.enabled: the browser keycloak-js (PKCE) login endpoint
```

Setting `nebariapp.enabled: true` renders a `NebariApp` custom resource that
registers the pack with the Nebari Operator. The operator wires up routing,
provisions a public Keycloak SPA client, and registers the landing page so the
UI is reachable through the Nebari gateway under `https://<hostname>` and
surfaced on the [Nebari Landing page](https://github.com/nebari-dev/nebari-landing).
The React SPA performs the OIDC login in the browser via `keycloak-js` (PKCE);
the gateway does not enforce auth (`nebariapp.auth.enforceAtGateway: false`).
Leave `nebariapp.enabled: false` for clusters that aren't running the operator.
Full field reference: [NebariApp CRD](/nebariapp-crd-reference/).

Verify:

```bash
# Application picked up by ArgoCD
kubectl get application provenance-collector -n argocd

# Chart unpacked: CronJob + dashboard pods exist in the target namespace
kubectl get cronjob -n provenance-system
kubectl get pods -n provenance-system -l app.kubernetes.io/name=provenance-collector
```

## Standalone install (without the Nebari Operator)

:::caution
Use this path only on a vanilla Kubernetes cluster *without* NIC. Without the
operator you're responsible for routing and OIDC yourself if you want the
dashboard reachable from outside the cluster.
:::

### Prerequisites

| Tool | Minimum version | Notes |
| --- | --- | --- |
| `kubectl` | 1.26+ | Cluster interaction |
| `helm` | 3.14+ | Chart install |
| Kubernetes cluster | 1.26+ | Local (kind / k3d / minikube) or remote |
| Cluster permissions | `cluster-admin` | Chart creates a `ClusterRole` + `ClusterRoleBinding` |

If you want `nebariapp.enabled: true` on a standalone cluster, the Nebari
Operator CRDs must still be installed first — see the
[NebariApp CRD reference](/nebariapp-crd-reference/). Most standalone installs
leave `nebariapp.enabled: false` and hit the dashboard's JSON API via
`kubectl port-forward` (the browser UI is the separate `frontend` container —
see [Web Dashboard](/web-dashboard/)).

### Install

```bash
helm repo add nebari https://nebari-dev.github.io/helm-repository
helm repo update

helm install provenance-collector nebari/provenance-collector \
  --namespace provenance-system \
  --create-namespace
```

Or install from a local checkout when iterating on the chart:

```bash
helm install provenance-collector ./chart \
  --namespace provenance-system \
  --create-namespace
```

### Verify

```bash
kubectl get cronjob -n provenance-system
kubectl get pods -n provenance-system -l app.kubernetes.io/name=provenance-collector
```

### Trigger a manual run

Two options:

1. **From the dashboard** — click **Run scan** on the Overview or Scans page.
   The button only renders for users whose OIDC groups intersect with
   `webUI.adminGroups`, so it's hidden by default until you wire up
   `webUI.oidcIssuer` and at least one admin group. Under operator-managed
   installs (`nebariapp.enabled: true`) this is handled automatically — the
   operator routes through Keycloak with the groups in `nebariapp.auth.groups`.
2. **With `kubectl`** — fall back to creating a Job from the CronJob directly:

```bash
kubectl create job --from=cronjob/provenance-collector \
  manual-run -n provenance-system

kubectl wait --for=condition=complete job/manual-run \
  -n provenance-system --timeout=5m
```

Either path creates a one-shot Job from the same CronJob template, so the
resulting report is identical. Manual Jobs are auto-cleaned after
`webUI.manualJobTTL` (default 1h); the kubectl-created Job above has no TTL
and persists until you delete it.

### View the report

```bash
# Default (persistence.mode=http) — the dashboard exposes the JSON API (it is
# API-only; the browser UI is the separate frontend container).
kubectl port-forward -n provenance-system \
  svc/provenance-collector-web 8080:8080

# In another shell, fetch the latest JSON:
curl -s http://localhost:8080/api/reports/latest | jq .

# persistence.mode=configmap — no dashboard required.
kubectl get configmap provenance-report \
  -n provenance-system \
  -o jsonpath='{.data.report\.json}' | jq .
```

To open the browser UI, use the Nebari gateway (`nebariapp.enabled: true`, at
`https://<hostname>`) or run it locally against the port-forwarded API — see
[Web Dashboard](/web-dashboard/). The report's JSON structure is documented in
the [Report Schema reference](/report-schema/).

### Uninstall

```bash
helm uninstall provenance-collector -n provenance-system

# Also remove the namespace (and any PVC-stored reports):
kubectl delete namespace provenance-system
```

## Next steps

- [Architecture](/architecture/) — how the pieces fit together.
- [Storage Modes](/storage-modes/) — choosing between `http`, `pvc`, and `configmap`.
- [Configuration](/configuration/) — every environment variable and chart value.
