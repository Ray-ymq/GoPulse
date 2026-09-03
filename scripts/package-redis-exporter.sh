#!/usr/bin/env bash
set -euo pipefail
REPO_ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
VERSION=$(tr -d '[:space:]' < "$REPO_ROOT/VERSION")
OUTPUT=
while (($#)); do
  case $1 in
    --version) [[ $# -ge 2 ]] || { echo 'Missing --version value.' >&2; exit 2; }; VERSION=$2; shift 2 ;;
    --output) [[ $# -ge 2 ]] || { echo 'Missing --output value.' >&2; exit 2; }; OUTPUT=$2; shift 2 ;;
    *) echo "Unknown argument: $1" >&2; exit 2 ;;
  esac
done
[[ $VERSION =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]] || { echo 'Plugin version must be a three-part SemVer.' >&2; exit 2; }
command -v go >/dev/null && command -v python3 >/dev/null && command -v tar >/dev/null && command -v gzip >/dev/null || { echo 'go, python3, tar, and gzip are required.' >&2; exit 1; }
if [[ -z $OUTPUT ]]; then OUTPUT="$REPO_ROOT/.run/packages/gopulse-redis-exporter-$VERSION-linux-$(go env GOARCH).tar.gz"; fi
mkdir -p "$(dirname "$OUTPUT")"
OUTPUT=$(python3 -c 'import os,sys; print(os.path.abspath(sys.argv[1]))' "$OUTPUT")
TEMP_DIR=$(mktemp -d); trap 'rm -rf -- "$TEMP_DIR"' EXIT
mkdir -p "$TEMP_DIR/package/bin"
(cd "$REPO_ROOT/exporters/redis" && CGO_ENABLED=0 go build -trimpath -buildvcs=false -ldflags='-buildid=' -o "$TEMP_DIR/package/bin/gopulse-redis-exporter" ./cmd/redis-exporter)
DIGEST=$(sha256sum "$TEMP_DIR/package/bin/gopulse-redis-exporter" | awk '{print $1}')
ARCH=$(go env GOARCH)
python3 - "$TEMP_DIR/package/plugin.json" "$VERSION" "$ARCH" "$DIGEST" <<'PY'
import json,sys
path,version,arch,digest=sys.argv[1:]
manifest={"schema_version":1,"id":"redis-exporter","name":"GoPulse Redis Exporter","version":version,"kind":"metrics-exporter","source":"redis","os":"linux","arch":arch,"entrypoint":"bin/gopulse-redis-exporter","entrypoint_sha256":digest,"health_path":"/health","metrics_path":"/metrics"}
with open(path,'w',encoding='utf-8',newline='\n') as f: json.dump(manifest,f,separators=(',',':'),sort_keys=True); f.write('\n')
PY
chmod 0644 "$TEMP_DIR/package/plugin.json"; chmod 0755 "$TEMP_DIR/package/bin/gopulse-redis-exporter"
TAR_PATH="$TEMP_DIR/package.tar"
tar --sort=name --mtime='@0' --owner=0 --group=0 --numeric-owner --format=ustar -C "$TEMP_DIR/package" -cf "$TAR_PATH" plugin.json bin/gopulse-redis-exporter
gzip -n -9 -c "$TAR_PATH" > "$OUTPUT.tmp"
mv -f "$OUTPUT.tmp" "$OUTPUT"
printf '%s\n' "$OUTPUT"
