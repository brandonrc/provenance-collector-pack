"""Go provenance-collector engine: report -> image mapping, ingest, subprocess runner,
and the stage's collector pass (no database)."""

from __future__ import annotations

import json
import os
import stat
from pathlib import Path

import pytest

from posture.app_settings import ProvenanceSettings
from posture.config import Settings
from posture.images import ImageRef
from posture.inventory_model import InventorySnapshot
from posture.provenance import collector as cm
from posture.provenance.checks import CosignConfig
from posture.provenance.stage import ImageWork, ProvenanceStage, config_fingerprint

from ..conftest import make_container
from .fakes import FakeCosign

FIXTURE = Path(__file__).parent.parent / "fixtures" / "provenance-collector-report.json"
D_WEB = "sha256:" + "1" * 64
D_GRAFANA_RUNNING = "sha256:" + "8" * 64  # the tag was re-pushed: collector resolved 999...
D_TOOL = "sha256:" + "3" * 64
D_REDIS = "sha256:" + "4" * 64


def report() -> dict:
    return json.loads(FIXTURE.read_text())


def inventory() -> tuple[InventorySnapshot, dict[str, int], list[ImageWork]]:
    cs = [
        make_container(namespace="app", pod="web-5d4f8c7b9-x", image="ghcr.io/org/web:1.0", image_key="web",
                       workload_kind="Deployment", workload_name="web"),
        make_container(namespace="app", pod="web-migrate-29311200-y", image="ghcr.io/org/web:1.0", image_key="web",
                       workload_kind="CronJob", workload_name="web-migrate", running=False),
        make_container(namespace="monitoring", pod="grafana-7c9d-z", image="grafana/grafana:11.2.0",
                       image_key="grafana", workload_name="grafana"),
        make_container(namespace="registry-test", pod="tool-6b5-a", image="localhost:32000/team/tool:dev",
                       image_key="tool", workload_name="tool"),
        make_container(namespace="cache", pod="redis-0", image="redis:7.2.4", image_key="redis",
                       workload_kind="StatefulSet", workload_name="redis"),
    ]
    key_to_id = {"web": 1, "grafana": 2, "tool": 3, "redis": 4}
    items = [
        ImageWork(1, ImageRef("ghcr.io", "org/web", "1.0", D_WEB), {"1.0"}),
        ImageWork(2, ImageRef("docker.io", "grafana/grafana", "11.2.0", D_GRAFANA_RUNNING), {"11.2.0"}),
        ImageWork(3, ImageRef("localhost:32000", "team/tool", "dev", D_TOOL), {"dev"}, mutable=True),
        ImageWork(4, ImageRef("docker.io", "library/redis", "7.2.4", D_REDIS), {"7.2.4"}),
    ]
    return InventorySnapshot(containers=cs), key_to_id, items


def test_match_report_digest_image_and_unmatched():
    inv, key_to_id, items = inventory()
    res = cm.match_report(report(), inv.containers, key_to_id, {w.image_id: w.ref.digest for w in items})
    assert sorted(res.matched) == [1, 2, 3]
    assert len(res.matched[1]) == 2  # Deployment's ReplicaSet + the CronJob's Job
    assert res.how == {"digest": 2, "image": 2, "digest-any-namespace": 0}
    assert [r["image"] for r in res.unmatched] == ["quay.io/gone/ghost:3.0"]


def test_match_report_digest_in_other_namespace_and_workload_tiebreak():
    inv, key_to_id, items = inventory()
    digests = {w.image_id: w.ref.digest for w in items}
    rep = {"metadata": {}, "images": [{"image": "mirror.local/redis:7.2.4", "digest": D_REDIS, "namespace": "other",
                                       "workload": {"kind": "ReplicaSet", "name": "x-1"}}]}
    res = cm.match_report(rep, inv.containers, key_to_id, digests)
    assert res.matched == {4: rep["images"]} and res.how["digest-any-namespace"] == 1
    # two different images under the same spec ref in one namespace: the workload decides
    cs = [make_container(namespace="n", image="app:1", image_key="a", workload_name="alpha"),
          make_container(namespace="n", image="app:1", image_key="b", workload_name="beta")]
    rec = {"image": "app:1", "namespace": "n", "workload": {"kind": "ReplicaSet", "name": "beta-77f"}}
    res = cm.match_report({"images": [rec]}, cs, {"a": 10, "b": 11}, {10: None, 11: None})
    assert res.matched == {11: [rec]}


def test_ingest_records_fold_and_absence_semantics():
    inv, key_to_id, items = inventory()
    res = cm.match_report(report(), inv.containers, key_to_id, {w.image_id: w.ref.digest for w in items})
    web = cm.ingest_records(res.matched[1], check_sbom=True, check_provenance=True, check_updates=True,
                            verify_signatures=True)
    assert web.resolved and web.digest == D_WEB
    assert web.signature == {"signed": True, "verified": False}
    assert web.sbom == {"hasSBOM": True, "format": "spdx"}
    assert web.provenance == {"hasProvenance": True, "predicateType": "https://slsa.dev/provenance/v0.2"}
    assert web.updates == {"1.0": {"currentTag": "1.0", "latestInMajor": "1.4.2", "newestAvailable": "2.1.0",
                                   "updateAvailable": True}}
    assert web.workloads == ["app/Job/web-migrate-29311200", "app/ReplicaSet/web-5d4f8c7b9"]
    graf = cm.ingest_records(res.matched[2], check_sbom=True, check_provenance=True, check_updates=True,
                             verify_signatures=True)
    # omitted sbom/provenance/update in their report = checked, none found / no update
    assert graf.sbom == {"hasSBOM": False} and graf.provenance == {"hasProvenance": False}
    assert graf.updates == {"11.2.0": {"currentTag": "11.2.0", "updateAvailable": False}}
    tool = cm.ingest_records(res.matched[3], check_sbom=True, check_provenance=True, check_updates=True,
                             verify_signatures=True)
    assert tool.resolved is False  # no digest: registry unreachable from the collector
    off = cm.ingest_records(res.matched[2], check_sbom=False, check_provenance=False, check_updates=False,
                            verify_signatures=False)
    assert (off.signature, off.sbom, off.provenance, off.updates) == (None, None, None, {})


def test_helm_releases_and_validation():
    [h] = cm.helm_releases(report())
    assert (h.release_name, h.namespace, h.chart, h.version, h.status, h.update) == (
        "kube-prom-stack", "observability", "kube-prometheus-stack", "65.1.0", "deployed", None)
    with pytest.raises(cm.CollectorError):
        cm.validate_report({"images": "nope"})


def test_collector_environment_and_cosign_key(tmp_path):
    cfg = cm.CollectorConfig(verify_signatures=False, helm_enabled=True, exclude_namespaces=["a", "b"],
                             registry_timeout_seconds=12.5, cluster_name="grace", update_level="minor")
    env = cfg.environ({"PROVENANCE_REPORT_OUTPUT": "http", "PATH": "/bin"})
    assert env["PROVENANCE_VERIFY_SIGNATURES"] == "false" and env["PROVENANCE_HELM_ENABLED"] == "true"
    assert env["PROVENANCE_EXCLUDE_NAMESPACES"] == "a,b" and env["PROVENANCE_REGISTRY_TIMEOUT"] == "12s"
    assert env["PROVENANCE_UPDATE_LEVEL"] == "minor" and env["PROVENANCE_CLUSTER_NAME"] == "grace"
    assert "PROVENANCE_REPORT_OUTPUT" not in env and env["PATH"] == "/bin"
    pem = "-----BEGIN PUBLIC KEY-----\nabc\n-----END PUBLIC KEY-----"
    path = cm.cosign_key_for_collector(pem, str(tmp_path))
    assert Path(path).read_text().startswith("-----BEGIN PUBLIC KEY-----")
    assert cm.cosign_key_for_collector(path, str(tmp_path)) == path
    assert cm.cosign_key_for_collector("awskms:///alias/x", str(tmp_path)) == ""
    assert cm.cosign_key_for_collector("", str(tmp_path)) == ""


def _fake_binary(tmp_path: Path, body: str) -> str:
    p = tmp_path / "provenance-collector"
    p.write_text("#!/bin/sh\n" + body)
    p.chmod(p.stat().st_mode | stat.S_IXUSR)
    return str(p)


async def test_run_collector_subprocess(tmp_path):
    # mimics the Go flags: --once --output <file>; logs on stdout, report in the file
    binary = _fake_binary(tmp_path, f'''
[ "$1" = "--once" ] && [ "$2" = "--output" ] || exit 2
[ "$PROVENANCE_CHECK_SBOM" = "true" ] || exit 3
echo '{{"level":"INFO","msg":"starting provenance collector"}}'
cp {FIXTURE} "$3"
''')
    assert cm.resolve_bin(binary) == binary
    rep = await cm.run_collector(binary, cm.CollectorConfig(), timeout=30)
    assert rep["metadata"]["collectorVersion"] == "0.2.0" and len(rep["images"]) == 5


async def test_run_collector_errors(tmp_path):
    failing = _fake_binary(tmp_path, 'echo \'{"level":"ERROR","msg":"collection failed"}\'; exit 1\n')
    with pytest.raises(cm.CollectorError, match="exited 1: .*collection failed"):
        await cm.run_collector(failing, cm.CollectorConfig(), timeout=30)
    os.remove(failing)
    garbage = _fake_binary(tmp_path, 'echo "not json" > "$3"\n')
    with pytest.raises(cm.CollectorError, match="unreadable"):
        await cm.run_collector(garbage, cm.CollectorConfig(), timeout=30)
    os.remove(garbage)
    slow = _fake_binary(tmp_path, "sleep 5\n")
    with pytest.raises(cm.CollectorError, match="timed out"):
        await cm.run_collector(slow, cm.CollectorConfig(), timeout=0.2)
    assert cm.resolve_bin(str(tmp_path / "missing")) is None


def test_engine_selection(tmp_path):
    assert ProvenanceStage(Settings(provenance_collector_bin=str(tmp_path / "nope")), None).engine == "python"
    assert ProvenanceStage(Settings(provenance_engine="python"), None).collector_runner is None
    binary = _fake_binary(tmp_path, "exit 0\n")
    st = ProvenanceStage(Settings(provenance_collector_bin=binary), None)
    assert st.engine == "collector" and st.collector_runner is not None


async def test_stage_collector_pass_keyless_verification():
    inv, key_to_id, items = inventory()
    seen: list[cm.CollectorConfig] = []

    async def runner(cfg):
        seen.append(cfg)
        return report()

    ps = ProvenanceSettings(helm_releases=True, cosign_certificate_identity_regexp="^me$",
                            cosign_certificate_oidc_issuer_regexp="^https://issuer$")
    st = ProvenanceStage(Settings(excluded_namespaces=["kube-system"]), None, collector_runner=runner)
    cos = FakeCosign({"ghcr.io/org/web@" + D_WEB})
    cosign_cfg = CosignConfig(ps.cosign_public_key, ps.cosign_certificate_identity_regexp,
                              ps.cosign_certificate_oidc_issuer_regexp)
    out, helm, msg = await st._collector_pass(items, inv, key_to_id, ps, cos, cosign_cfg, config_fingerprint(ps))
    assert seen[0].helm_enabled and seen[0].exclude_namespaces == ["kube-system"]
    assert seen[0].cosign_public_key_file == ""  # keyless: the collector only checks existence
    assert sorted(out) == [1, 2]  # 3 unresolved, 4 not in the report -> python
    web = out[1]
    assert cos.calls == ["ghcr.io/org/web@" + D_WEB]
    assert web.signature == {"signed": True, "verified": True}
    assert web.details["engine"] == "collector" and web.details["collectorVersion"] == "0.2.0"
    assert web.inputs.major_behind and web.score == 100 - 25  # signed+verified, SBOM, SLSA, major update
    graf = out[2]
    assert graf.digest == D_GRAFANA_RUNNING and graf.signature == {"signed": False, "verified": False}
    assert graf.score == 100 - 40 - 20 - 15
    assert [h.release_name for h in helm] == ["kube-prom-stack"]
    assert msg.startswith("provenance engine=collector (0.2.0): 5 report record(s) -> 2 image(s) ingested")
    assert "1 unmatched record(s); 1 image(s) unresolved by the collector and 1 not in its report" in msg


async def test_stage_collector_pass_key_file_is_verified_by_the_collector():
    inv, key_to_id, items = inventory()
    seen = []

    async def runner(cfg):
        seen.append(cfg.cosign_public_key_file)
        assert Path(cfg.cosign_public_key_file).read_text().startswith("-----BEGIN")
        return report()

    ps = ProvenanceSettings(cosign_public_key="-----BEGIN PUBLIC KEY-----\nk\n-----END PUBLIC KEY-----")
    st = ProvenanceStage(Settings(), None, collector_runner=runner)
    cos = FakeCosign(set())
    out, _, _ = await st._collector_pass(items, inv, key_to_id, ps, cos, CosignConfig(ps.cosign_public_key, "", ""),
                                         config_fingerprint(ps))
    assert seen and not os.path.exists(seen[0])  # temp key removed after the run
    assert cos.calls == [] and out[1].signature == {"signed": True, "verified": False}
