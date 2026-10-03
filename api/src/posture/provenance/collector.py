"""Go provenance-collector as the provenance engine (`PROVENANCE_ENGINE=collector`).

The worker image bundles the collector from `collector/` at
`/usr/local/bin/provenance-collector`. Once per scan the provenance stage runs it
with `--once --output <file>` under the worker's ServiceAccount, reads its report
(the provenance-collector-pack report schema, `collector/internal/report/types.go`)
and maps it onto this pack's images:

    report images[] (image, digest, namespace, workload{kind,name})
        -> inventory containers (namespace, spec image, resolved controller)
        -> images.id

Matching order per report record (`match_report`):

1. digest + namespace (+ workload): an image in the same namespace whose running
   digest equals the digest the collector resolved;
2. namespace + spec image (+ workload): the collector resolves the tag at report
   time, so a re-pushed tag can differ from the running digest;
3. digest alone (any namespace).

The collector reports the owning ReplicaSet / Job where the inventory resolves
the controller (Deployment / CronJob); "workload" agreement is therefore a
prefix match (`nginx-5d4f8` belongs to Deployment `nginx`) and only breaks ties.

Records with an empty `digest` mean the collector could not resolve the image in
its registry (unreachable from the pod, node-local registry aliases such as
`localhost:32000`, rate limits); those images, images the report does not cover,
and everything when the binary fails, go through the Python checks instead
(`ProvenanceStage`), so the engine switch never loses coverage.
"""

from __future__ import annotations

import asyncio
import json
import os
import shutil
import tempfile
from collections.abc import Iterable
from dataclasses import dataclass, field
from typing import Any

from ..inventory_model import ContainerRecord
from ..logs import get_logger
from .helm import HelmRelease
from .updates import UpdateInfo, needs_tag_list
from .updates import parse_image_ref as split_tag

log = get_logger(__name__)

ENGINE_COLLECTOR = "collector"
ENGINE_PYTHON = "python"
DEFAULT_BIN = "/usr/local/bin/provenance-collector"


class CollectorError(RuntimeError):
    pass


def resolve_bin(path: str | None) -> str | None:
    """Absolute path of an executable collector binary, or None."""
    path = path or DEFAULT_BIN
    if os.sep not in path:
        return shutil.which(path)
    return path if os.path.isfile(path) and os.access(path, os.X_OK) else None


# --------------------------------------------------------------------- running
@dataclass
class CollectorConfig:
    """What the stage hands the binary (their PROVENANCE_* environment)."""

    verify_signatures: bool = True
    cosign_public_key_file: str = ""  # file path only: the collector reads the key with os.ReadFile
    check_sbom: bool = True
    check_provenance: bool = True
    check_updates: bool = True
    update_level: str = "patch"
    skip_prerelease: bool = True
    helm_enabled: bool = False
    exclude_namespaces: list[str] = field(default_factory=list)
    registry_timeout_seconds: float = 30
    registry_auth_file: str | None = None
    cluster_name: str = ""

    def environ(self, base: dict[str, str] | None = None) -> dict[str, str]:
        env = dict(os.environ if base is None else base)

        def b(v: bool) -> str:
            return "true" if v else "false"

        env.update({
            "PROVENANCE_VERIFY_SIGNATURES": b(self.verify_signatures),
            "PROVENANCE_COSIGN_PUBLIC_KEY": self.cosign_public_key_file,
            "PROVENANCE_CHECK_SBOM": b(self.check_sbom),
            "PROVENANCE_CHECK_PROVENANCE": b(self.check_provenance),
            "PROVENANCE_CHECK_UPDATES": b(self.check_updates),
            "PROVENANCE_UPDATE_LEVEL": self.update_level or "patch",
            "PROVENANCE_SKIP_PRERELEASE": b(self.skip_prerelease),
            "PROVENANCE_HELM_ENABLED": b(self.helm_enabled),
            "PROVENANCE_EXCLUDE_NAMESPACES": ",".join(self.exclude_namespaces),
            "PROVENANCE_NAMESPACES": "",
            "PROVENANCE_REGISTRY_TIMEOUT": f"{max(1, int(self.registry_timeout_seconds))}s",
            "PROVENANCE_CLUSTER_NAME": self.cluster_name,
        })
        if self.registry_auth_file:
            env["PROVENANCE_REGISTRY_AUTH"] = self.registry_auth_file
        # The sink variables are irrelevant with --output; make sure a stray
        # value can never send the report elsewhere.
        for k in ("PROVENANCE_REPORT_OUTPUT", "PROVENANCE_REPORT_UPLOAD_URL"):
            env.pop(k, None)
        return env


def cosign_key_for_collector(key: str, workdir: str) -> str:
    """The collector verifies with a key *file*. PEM text is written to `workdir`;
    an existing path is passed through; KMS / remote URIs and "" return "" (the
    collector then only checks signature existence and the stage verifies with
    the cosign CLI)."""
    key = (key or "").strip()
    if not key:
        return ""
    if key.startswith("-----BEGIN"):
        path = os.path.join(workdir, "cosign.pub")
        with open(path, "w") as fh:
            fh.write(key + ("\n" if not key.endswith("\n") else ""))
        return path
    if os.path.isfile(key):
        return key
    return ""


async def run_collector(binary: str, cfg: CollectorConfig, timeout: float) -> dict[str, Any]:
    """Run `<binary> --once --output <tmp>` and return the parsed report."""
    with tempfile.TemporaryDirectory(prefix="provenance-collector-") as tmp:
        out = os.path.join(tmp, "report.json")
        proc = await asyncio.create_subprocess_exec(
            binary, "--once", "--output", out, env=cfg.environ(),
            stdout=asyncio.subprocess.PIPE, stderr=asyncio.subprocess.STDOUT)
        try:
            logs, _ = await asyncio.wait_for(proc.communicate(), timeout=timeout)
        except TimeoutError as e:
            proc.kill()
            await proc.wait()
            raise CollectorError(f"provenance-collector timed out after {int(timeout)}s") from e
        tail = (logs or b"").decode(errors="replace").strip().splitlines()[-5:]
        if proc.returncode != 0:
            raise CollectorError(f"provenance-collector exited {proc.returncode}: {' | '.join(tail)[:500]}")
        try:
            with open(out, "rb") as fh:
                report = json.load(fh)
        except (OSError, ValueError) as e:
            raise CollectorError(f"provenance-collector report unreadable: {e}") from e
    validate_report(report)
    return report


def validate_report(report: Any) -> None:
    if not isinstance(report, dict) or not isinstance(report.get("images"), list) \
            or not isinstance(report.get("metadata"), dict):
        raise CollectorError("provenance-collector report has no metadata/images")


# --------------------------------------------------------------------- mapping
@dataclass
class MatchResult:
    """Report records grouped by our image id."""

    matched: dict[int, list[dict[str, Any]]] = field(default_factory=dict)
    unmatched: list[dict[str, Any]] = field(default_factory=list)
    how: dict[str, int] = field(default_factory=lambda: {"digest": 0, "image": 0, "digest-any-namespace": 0})


def _workload_agrees(rec: dict[str, Any], c: ContainerRecord) -> bool:
    w = rec.get("workload") or {}
    name = w.get("name") or ""
    if not name:
        return False
    if (w.get("kind"), name) == (c.workload_kind, c.workload_name):
        return True
    # ReplicaSet <deploy>-<hash> / Job <cronjob>-<n> vs the resolved controller
    return bool(c.workload_name) and name.startswith(c.workload_name + "-")


def match_report(report: dict[str, Any], containers: Iterable[ContainerRecord], key_to_id: dict[str, int],
                 digests: dict[int, str | None]) -> MatchResult:
    """Map report `images[]` onto image ids (see module docstring)."""
    by_ns: dict[str, list[tuple[ContainerRecord, int]]] = {}
    by_digest: dict[str, set[int]] = {}
    for c in containers:
        iid = key_to_id.get(c.image_key or "")
        if iid is None:
            continue
        by_ns.setdefault(c.namespace, []).append((c, iid))
    for iid, d in digests.items():
        if d:
            by_digest.setdefault(d, set()).add(iid)

    res = MatchResult()

    def pick(cands: list[tuple[ContainerRecord, int]], rec: dict[str, Any]) -> int | None:
        ids = {iid for _, iid in cands}
        if len(ids) == 1:
            return next(iter(ids))
        agreeing = {iid for c, iid in cands if _workload_agrees(rec, c)}
        if len(agreeing) == 1:
            return next(iter(agreeing))
        return min(agreeing or ids) if (agreeing or ids) else None

    for rec in report.get("images") or []:
        ns, image, digest = rec.get("namespace") or "", rec.get("image") or "", rec.get("digest") or ""
        local = by_ns.get(ns, [])
        iid: int | None = None
        if digest:
            cands = [(c, i) for c, i in local if digests.get(i) == digest]
            if cands:
                iid = pick(cands, rec)
                res.how["digest"] += 1
        if iid is None:
            cands = [(c, i) for c, i in local if c.image == image]
            if cands:
                iid = pick(cands, rec)
                res.how["image"] += 1
        if iid is None and digest and len(by_digest.get(digest, ())) >= 1:
            iid = min(by_digest[digest])
            res.how["digest-any-namespace"] += 1
        if iid is None:
            res.unmatched.append(rec)
        else:
            res.matched.setdefault(iid, []).append(rec)
    return res


@dataclass
class IngestedImage:
    """One image's results from the collector, in this pack's JSON shapes."""

    digest: str | None
    signature: dict[str, Any] | None
    sbom: dict[str, Any] | None
    provenance: dict[str, Any] | None
    updates: dict[str, Any]
    workloads: list[str]
    resolved: bool  # False: the collector could not resolve any record (registry unreachable)


def ingest_records(records: list[dict[str, Any]], *, check_sbom: bool, check_provenance: bool,
                   check_updates: bool, verify_signatures: bool) -> IngestedImage:
    """Fold the report records of one image into one result.

    Their report omits `sbom` / `provenance` when none was found and `update` when
    no update is available, so absence means "checked, none" for enabled checks."""
    resolved = [r for r in records if r.get("digest")]
    src = resolved or records
    sig = next((r["signature"] for r in src if r.get("signature")), None)
    sbom = next((r["sbom"] for r in src if (r.get("sbom") or {}).get("hasSBOM")), None)
    prov = next((r["provenance"] for r in src if (r.get("provenance") or {}).get("hasProvenance")), None)
    if check_sbom and sbom is None:
        sbom = {"hasSBOM": False}
    if check_provenance and prov is None:
        prov = {"hasProvenance": False}
    if not check_sbom:
        sbom = None
    if not check_provenance:
        prov = None
    if not verify_signatures:
        sig = None
    elif sig is None:
        sig = {"signed": False, "verified": False}
    updates: dict[str, Any] = {}
    if check_updates:
        for r in src:
            _, tag = split_tag(r.get("image") or "")
            upd = r.get("update")
            if upd:
                updates[tag] = UpdateInfo.from_json(upd).as_json()  # type: ignore[union-attr]
            elif tag not in updates:
                updates[tag] = UpdateInfo(current_tag=tag).as_json()
    workloads = sorted({f"{r.get('namespace')}/{(r.get('workload') or {}).get('kind')}/"
                        f"{(r.get('workload') or {}).get('name')}" for r in records})
    return IngestedImage(next((r["digest"] for r in resolved), None), sig, sbom, prov, updates, workloads,
                         bool(resolved))


def updates_checked(update: UpdateInfo | None, check_updates: bool) -> bool:
    return check_updates and update is not None and needs_tag_list(update.current_tag)


def helm_releases(report: dict[str, Any]) -> list[HelmRelease]:
    out = []
    for h in report.get("helmReleases") or []:
        out.append(HelmRelease(
            release_name=h.get("releaseName") or "", namespace=h.get("namespace") or "", chart=h.get("chart") or "",
            version=h.get("version") or "", app_version=h.get("appVersion") or "", status=h.get("status") or "unknown",
            update=UpdateInfo.from_json(h.get("update"))))
    return out
