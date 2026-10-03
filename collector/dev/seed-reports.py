#!/usr/bin/env python3
"""Generate rich seed provenance reports and POST them to the dashboard's
internal upload endpoint so the UI has varied rows + timeline history."""
import json, sys, time, urllib.request

UPLOAD_URL = sys.argv[1] if len(sys.argv) > 1 else "http://localhost:8081/internal/reports"

# A varied catalog of images exercising every UI state: signed/unsigned,
# verified, SLSA yes/no, SBOM yes/no, updates available, long workload names,
# many namespaces, enough rows for pagination.
BASE_IMAGES = [
    # image, ns, kind, workload, signed, verified, sbom, sbomfmt, slsa, cur, latest, upd
    ("nginx:1.27-alpine", "default", "Deployment", "nginx", True, True, True, "spdx", True, "1.27", "1.27.3", True),
    ("redis:7.2", "cache", "StatefulSet", "redis", True, True, True, "cyclonedx", False, "7.2", "7.4.1", True),
    ("postgres:16.2", "data", "StatefulSet", "postgres-primary", True, False, True, "spdx", False, "16.2", "16.4", True),
    ("ghcr.io/nebari-dev/provenance-collector:0.1.0", "provenance-system", "CronJob", "provenance-collector", True, True, True, "spdx", True, "0.1.0", "0.1.0", False),
    ("busybox:1.36", "default", "DaemonSet", "node-agent-with-a-really-long-workload-name-for-truncation", False, False, False, "", False, "1.36", "1.36.1", True),
    ("prom/prometheus:v2.51.0", "monitoring", "Deployment", "prometheus", True, True, False, "", True, "v2.51.0", "v2.53.0", True),
    ("grafana/grafana:10.4.0", "monitoring", "Deployment", "grafana", False, False, True, "cyclonedx", False, "10.4.0", "11.1.0", True),
    ("quay.io/keycloak/keycloak:24.0", "auth", "StatefulSet", "keycloak", True, True, True, "spdx", True, "24.0", "25.0.1", True),
    ("bitnami/kafka:3.7", "streaming", "StatefulSet", "kafka", True, False, True, "spdx", False, "3.7", "3.8.0", True),
    ("hashicorp/vault:1.16", "security", "StatefulSet", "vault", True, True, True, "cyclonedx", True, "1.16", "1.17.2", True),
    ("traefik:v3.0", "ingress", "Deployment", "traefik", True, True, False, "", True, "v3.0", "v3.1.0", True),
    ("curlimages/curl:8.7.1", "default", "Job", "smoke-test", False, False, False, "", False, "8.7.1", "8.9.0", True),
    ("alpine:3.19", "default", "Deployment", "sidecar-shipper", False, False, False, "", False, "3.19", "3.20", True),
    ("docker.io/library/mongo:7.0", "data", "StatefulSet", "mongo", True, False, True, "spdx", False, "7.0", "7.0.12", True),
    ("cgr.dev/chainguard/static:latest", "default", "Deployment", "static-svc", True, True, True, "spdx", True, "latest", "latest", False),
    ("minio/minio:RELEASE.2024-05-01", "storage", "StatefulSet", "minio", True, False, True, "cyclonedx", False, "RELEASE.2024-05-01", "RELEASE.2024-06-29", True),
]

HELM = [
    ("ingress-nginx", "ingress", "ingress-nginx", "4.8.0", "1.9.4", "deployed", True, "4.11.1"),
    ("prometheus", "monitoring", "kube-prometheus-stack", "58.0.0", "0.73.0", "deployed", True, "61.3.1"),
    ("keycloak", "auth", "keycloakx", "2.3.0", "24.0", "deployed", False, ""),
    ("provenance-collector", "provenance-system", "provenance-collector", "0.1.0", "0.1.0", "deployed", False, ""),
    ("cert-manager", "cert-manager", "cert-manager", "1.14.4", "1.14.4", "deployed", True, "1.15.1"),
]


def build_report(images, generated_at):
    img_records = []
    for (image, ns, kind, wl, signed, verified, sbom, sbomfmt, slsa, cur, latest, upd) in images:
        rec = {
            "image": image,
            "digest": "sha256:" + (abs(hash(image)) % (16**64)).to_bytes(32, "big").hex(),
            "namespace": ns,
            "workload": {"kind": kind, "name": wl},
            "signature": {"signed": signed, "verified": verified},
            "sbom": {"hasSBOM": sbom, "format": sbomfmt} if sbom else {"hasSBOM": False},
            "provenance": {"hasProvenance": slsa, "predicateType": "https://slsa.dev/provenance/v1"} if slsa else {"hasProvenance": False},
            "update": {"currentTag": cur, "latestInMajor": latest, "newestAvailable": latest, "updateAvailable": upd},
        }
        img_records.append(rec)

    helm_records = []
    for (name, ns, chart, ver, appver, status, upd, latest) in HELM:
        h = {"releaseName": name, "namespace": ns, "chart": chart,
             "version": ver, "appVersion": appver, "status": status}
        if upd:
            h["update"] = {"currentTag": ver, "newestAvailable": latest, "updateAvailable": True}
        helm_records.append(h)

    namespaces = sorted({i["namespace"] for i in img_records})
    summary = {
        "totalImages": len(img_records),
        "uniqueImages": len({i["image"] for i in img_records}),
        "signedImages": sum(1 for i in img_records if i["signature"]["signed"]),
        "verifiedImages": sum(1 for i in img_records if i["signature"]["verified"]),
        "imagesWithSBOM": sum(1 for i in img_records if i["sbom"].get("hasSBOM")),
        "imagesWithProvenance": sum(1 for i in img_records if i["provenance"].get("hasProvenance")),
        "imagesWithUpdates": sum(1 for i in img_records if i["update"]["updateAvailable"]),
        "totalHelmReleases": len(helm_records),
        "helmReleasesWithUpdates": sum(1 for h in helm_records if h.get("update", {}).get("updateAvailable")),
    }
    return {
        "metadata": {
            "generatedAt": generated_at,
            "collectorVersion": "dev",
            "clusterName": "provenance-dev",
            "namespacesScanned": namespaces,
        },
        "images": img_records,
        "helmReleases": helm_records,
        "summary": summary,
    }


def post(report):
    data = json.dumps(report).encode()
    req = urllib.request.Request(UPLOAD_URL, data=data, headers={"Content-Type": "application/json"}, method="POST")
    with urllib.request.urlopen(req, timeout=30) as resp:
        return resp.status


# Three historical scans: older ones have fewer images / different states so the
# timeline deltas + history browsing have something to show.
scans = [
    (BASE_IMAGES[:10], "2026-07-07T06:00:00Z"),
    (BASE_IMAGES[:13], "2026-07-08T06:00:00Z"),
    (BASE_IMAGES,      "2026-07-09T06:00:00Z"),
]

for i, (imgs, ts) in enumerate(scans):
    status = post(build_report(imgs, ts))
    print(f"uploaded scan {i+1}/{len(scans)} ({len(imgs)} images, {ts}) -> {status}")
    if i < len(scans) - 1:
        time.sleep(1.2)  # ensure distinct second-precision filenames
