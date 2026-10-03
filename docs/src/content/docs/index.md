---
title: Security Posture
description: Admin-only security posture for Nebari clusters - supply-chain provenance, three-scanner vulnerability consensus, STIG posture checks, live NIST SP 800-53 control evidence and compliance reports.
---

The **Security Posture pack** (repository `provenance-collector-pack`, Helm
chart `provenance-collector`) answers the full compliance question for a Nebari
cluster:

- **Where did every running image come from?** Supply-chain provenance:
  digests, cosign signatures, SBOM and SLSA provenance attestations, available
  updates and Helm releases. The **Go provenance collector** is the provenance
  engine: the worker runs it once per scan and ingests its report.
- **What is wrong with it?** Every image is mirrored once and scanned by
  **Trivy, Grype and Clair**; findings are correlated into a consensus with an
  agreement level per CVE.
- **How is it run?** 16 workload posture checks mapped to the **Kubernetes STIG**
  and the Container Platform SRG.
- **Which controls does the platform implement and prove?** A **NIST SP 800-53
  rev5 control evidence engine** runs live assertions against Keycloak, the
  gateway, cert-manager, Kubernetes RBAC, logging and monitoring.

Everything rolls up into a 0-100 score and an A-F grade per image, workload,
namespace and cluster, and into compliance reports: POA&M, STIG checklists
(CKL/CKLB), a Security Assessment Report (PDF), OSCAL assessment results, SSP
and component definition, inventory and vulnerability exports.

It was formed by merging `nebari-security-posture-pack` into
`provenance-collector-pack` ([proposal 0001](/proposals/0001-merge/)). The
provenance report API (`/api/reports/latest` and friends) and the Grafana
Infinity dashboard keep working.

## What it does

| Capability | Description |
|---|---|
| **Inventory** | Every running container by digest, with its owning controller and NebariApp |
| **Provenance** | Signatures (key, keyless), SBOM and SLSA attestations, update checks, Helm releases ([details](/provenance/)) |
| **Vulnerability consensus** | Trivy + Grype + Clair on identical mirrored bytes, agreement 1/3, 2/3, 3/3 |
| **Posture checks** | Privileged, root, capabilities, host namespaces, hostPath, seccomp, limits, probes, mutable tags, SA tokens, NetworkPolicy |
| **Control evidence** | 35 live assertions, control status per baseline (LOW / MODERATE / HIGH) ([details](/controls/)) |
| **Reports** | POA&M (eMASS layout), STIG CKL/CKLB, SAR PDF, OSCAL AR / SSP / component definition, CycloneDX VEX ([details](/reports/)) |
| **Web UI** | Admin-only nebari-design UI behind the Nebari gateway ([Web UI](/web-dashboard/)) |
| **Grafana** | The provenance report JSON API on an unauthenticated in-cluster Service for the Infinity datasource |

## Guides

- [Quick Start](/quick-start/) - install the pack and run your first scan.
- [Migrating from 0.1.x](/migrating/) - upgrading from the provenance-collector-only chart.
- [Architecture](/architecture/) - the runtime components and how a scan flows.
- [Web UI](/web-dashboard/) - the pages, the auth model, the JSON APIs and Grafana.
- [Storage](/storage-modes/) - Postgres and the PVCs.

## Compliance

- [Compliance architecture](/compliance-architecture/) - from container scans to an 800-53 control picture.
- [Controls](/controls/), [Reports](/reports/), [Supply-chain provenance](/provenance/), [Scoring](/scoring/).

## Reference

- [Configuration](/configuration/) - chart values, including the 0.1.x compatibility keys.
- [Collector environment](/collector-environment/) - environment variables of the Go collector binary.
- [Report Schema](/report-schema/) - the provenance report JSON.
- [NebariApp CRD](/nebariapp-crd-reference/) - operator integration.
- [Verifying Images](/verifying-images/) - cosign verification of the published images.

> **Status:** experimental. Chart values, APIs and report formats may change
> without notice while pre-1.0.

Deployment examples (standalone, Nebari, ArgoCD, Grafana dashboard) live in the
[`examples/`](https://github.com/nebari-dev/provenance-collector-pack/tree/main/examples)
directory of the repository.
