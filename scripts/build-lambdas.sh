#!/usr/bin/env bash
# Build the Step Functions Lambdas (provided.al2023, arm64) into infra/build/<name>.zip.
# The bootstrap binary needs mode 0755 inside the zip, which Windows tooling does not preserve, so
# the zip is written with Python's zipfile and explicit Unix permissions.
source "$(dirname "${BASH_SOURCE[0]}")/lib.sh"

OUT="infra/build"
mkdir -p "$OUT/bin"
for dir in cmd/lambdas/*/; do
  name="$(basename "$dir")"
  mkdir -p "$OUT/bin/$name"
  log "building lambda $name"
  CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags="-s -w" -tags lambda.norpc -o "$OUT/bin/$name/bootstrap" "./cmd/lambdas/$name"
  python - "$OUT/bin/$name/bootstrap" "$OUT/$name.zip" <<'PY'
import sys, zipfile
src, dst = sys.argv[1], sys.argv[2]
info = zipfile.ZipInfo("bootstrap", date_time=(2026, 1, 1, 0, 0, 0))  # fixed timestamp: reproducible zips
info.external_attr = 0o755 << 16
info.compress_type = zipfile.ZIP_DEFLATED
with zipfile.ZipFile(dst, "w") as z:
    z.writestr(info, open(src, "rb").read())
PY
done
ls -la "$OUT"/*.zip
