#!/usr/bin/env python3
"""Publish the plain-markdown design docs (docs/*.md) as Starlight pages.

The markdown files under docs/ are the source of truth (readable on GitHub);
this script copies them into docs/src/content/docs/ with front matter and
site-relative links.

    python3 scripts/sync-docs.py           # (re)generate the pages
    python3 scripts/sync-docs.py --check   # exit 1 if a generated page is stale
"""

from __future__ import annotations

import posixpath
import re
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parent.parent
OUT = ROOT / "docs" / "src" / "content" / "docs"
GITHUB = "https://github.com/nebari-dev/provenance-collector-pack/blob/main/"

# source (repo-relative) -> (slug, description)
PAGES: dict[str, tuple[str, str]] = {
    "docs/ARCHITECTURE.md": (
        "compliance-architecture",
        "How the pack turns what Nebari deploys into continuous ATO evidence: evidence layers, "
        "control inheritance, the pipeline and the cATO loop."),
    "docs/CONTROLS.md": (
        "controls",
        "The NIST SP 800-53 rev5 control evidence engine: components, live assertions and "
        "control status derivation."),
    "docs/REPORTS.md": (
        "reports",
        "Compliance reports: POA&M, STIG checklists, SAR, OSCAL assessment results, SSP and "
        "component definition, inventory and vulnerability exports."),
    "docs/PROVENANCE.md": (
        "provenance",
        "Supply-chain provenance: the Go collector engine, the Python fallback, scoring, controls "
        "and the provenance-collector compatible API."),
    "docs/SCORING.md": (
        "scoring",
        "How image, workload, namespace and cluster scores and A-F grades are computed."),
    "docs/DESIGN.md": (
        "design",
        "Design contract of the security posture components (api, worker, ui, chart)."),
    "docs/DECISIONS.md": (
        "decisions",
        "Decisions log: deviations from and refinements of the design contract."),
    "docs/proposals/0001-merge-with-provenance-collector-pack.md": (
        "proposals/0001-merge",
        "Proposal 0001: merge provenance-collector-pack and nebari-security-posture-pack."),
}

LINK = re.compile(r"(\]\()([^)\s]+)(\))")
FENCE = re.compile(r"^\s*(```|~~~)")


def rewrite_link(target: str, src: str) -> str:
    if re.match(r"^[a-z][a-z0-9+.-]*:", target, re.I) or target.startswith(("#", "/")):
        return target
    path, _, anchor = target.partition("#")
    resolved = posixpath.normpath(posixpath.join(posixpath.dirname(src), path))
    frag = f"#{anchor}" if anchor else ""
    if resolved in PAGES:
        return f"/{PAGES[resolved][0]}/{frag}"
    return f"{GITHUB}{resolved}{frag}"


def render(src: str) -> tuple[Path, str]:
    slug, description = PAGES[src]
    lines = (ROOT / src).read_text().splitlines()
    title = None
    body: list[str] = []
    in_fence = False
    for line in lines:
        if FENCE.match(line):
            in_fence = not in_fence
        if title is None and not in_fence and line.startswith("# "):
            title = line[2:].strip()
            continue
        if not in_fence:
            line = LINK.sub(lambda m: m.group(1) + rewrite_link(m.group(2), src) + m.group(3), line)
        body.append(line)
    while body and not body[0].strip():
        body.pop(0)
    title = title or slug

    def q(s: str) -> str:
        return '"' + s.replace("\\", "\\\\").replace('"', '\\"') + '"'

    text = (f"---\ntitle: {q(title)}\ndescription: {q(description)}\n---\n\n"
            f"<!-- GENERATED from {src} - edit that file and run: python3 scripts/sync-docs.py -->\n\n"
            + "\n".join(body).rstrip() + "\n")
    return OUT / f"{slug}.md", text


def main() -> int:
    check = "--check" in sys.argv[1:]
    stale = []
    for src in PAGES:
        dest, text = render(src)
        current = dest.read_text() if dest.exists() else None
        if current == text:
            continue
        if check:
            stale.append(str(dest.relative_to(ROOT)))
        else:
            dest.parent.mkdir(parents=True, exist_ok=True)
            dest.write_text(text)
            print(f"wrote {dest.relative_to(ROOT)}")
    if stale:
        print("stale (run python3 scripts/sync-docs.py):\n  " + "\n  ".join(stale), file=sys.stderr)
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
