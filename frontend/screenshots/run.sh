#!/bin/sh
# Re-generate docs/screenshots/dashboard-*.png from the provenance mock bundle
# (VITE_API_MOCK=provenance: the golden report plus two older runs). Docker only;
# no host node needed.
# Usage: frontend/screenshots/run.sh   (from anywhere)
#
# Both containers use --network host (preview on 127.0.0.1:$PORT, default 4173);
# no Docker network is created.
set -eu
cd "$(dirname "$0")/.."
PORT=${PORT:-4173}
OUT="$(cd .. && pwd)/docs/screenshots"
mkdir -p "$OUT"
docker run --rm -u "$(id -u):$(id -g)" -v "$PWD":/app -w /app -e HOME=/tmp node:22-alpine \
  sh -c 'npm ci --no-audit --no-fund >/dev/null && npm run build:mock-provenance >/dev/null'
docker rm -f pc-preview >/dev/null 2>&1 || true
docker run -d --name pc-preview --network host -u "$(id -u):$(id -g)" -v "$PWD":/app -w /app -e HOME=/tmp node:22-alpine \
  npx vite preview --outDir dist-mock-provenance --host 127.0.0.1 --port "$PORT" --strictPort >/dev/null
trap 'docker rm -f pc-preview >/dev/null 2>&1 || true' EXIT
sleep 4
WORK=$(mktemp -d)
cp screenshots/shoot.mjs "$WORK/"
docker run --rm --network host --ipc=host -u "$(id -u):$(id -g)" -e HOME=/tmp -v "$WORK":/work -v "$OUT":/out \
  -e BASE_URL="http://127.0.0.1:$PORT" -e OUT_DIR=/out -e DETAIL="${DETAIL:-ghcr.io/example/web:1.4.2}" -w /work \
  mcr.microsoft.com/playwright:v1.63.0-noble \
  sh -c 'npm init -y >/dev/null && npm i --no-audit --no-fund playwright@1.63.0 >/dev/null 2>&1 && node shoot.mjs'
rm -rf "$WORK"
