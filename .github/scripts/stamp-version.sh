#!/usr/bin/env bash
# Stamp a release version onto the chart, its first-party image tags and the
# examples. Usage: .github/scripts/stamp-version.sh 0.2.1
set -euo pipefail
VERSION="${1:?usage: stamp-version.sh <version>}"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
cd "$ROOT"

sed -i -E "s|^version:.*|version: ${VERSION}|" chart/Chart.yaml
sed -i -E "s|^appVersion:.*|appVersion: \"${VERSION}\"|" chart/Chart.yaml
# images.{api,worker,ui}.tag: the line after each provenance-collector-pack-<x> repository.
sed -i -E "/repository: quay.io\/nebari\/provenance-collector-pack-(api|worker|ui)$/{n;s|^(\s+tag:\s*)\"[^\"]*\"|\1\"${VERSION}\"|}" chart/values.yaml
# ArgoCD Application targetRevision.
sed -i -E "s|^(\s*targetRevision:\s*)\"[^\"]*\"(.*)|\1\"${VERSION}\"\2|" examples/*.yaml || true
