#!/usr/bin/env bash
set -euo pipefail
REPO_ROOT=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
VERSION=$(tr -d '[:space:]' < "$REPO_ROOT/VERSION")
OUTPUT=
BINARY=
ARCH=
CONTRACT_VERSION=2
SOURCE=redis
while (($#)); do
  case $1 in
    --source) SOURCE=${2:?}; shift 2 ;;
    --version) [[ $# -ge 2 ]] || { echo 'Missing --version value.' >&2; exit 2; }; VERSION=$2; shift 2 ;;
    --output) [[ $# -ge 2 ]] || { echo 'Missing --output value.' >&2; exit 2; }; OUTPUT=$2; shift 2 ;;
    --binary) [[ $# -ge 2 ]] || { echo 'Missing --binary value.' >&2; exit 2; }; BINARY=$2; shift 2 ;;
    --contract-version) [[ $# -ge 2 ]] || { echo 'Missing --contract-version value.' >&2; exit 2; }; CONTRACT_VERSION=$2; shift 2 ;;
    --arch) [[ $# -ge 2 ]] || { echo 'Missing --arch value.' >&2; exit 2; }; ARCH=$2; shift 2 ;;
    *) echo "Unknown argument: $1" >&2; exit 2 ;;
  esac
done
[[ $VERSION =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]] || { echo 'Plugin version must be a three-part SemVer.' >&2; exit 2; }
[[ $CONTRACT_VERSION == 1 || $CONTRACT_VERSION == 2 ]] || { echo 'Contract version must be 1 or 2.' >&2; exit 2; }
[[ $SOURCE == redis || $SOURCE == mysql || $SOURCE == rabbitmq ]] || { echo "Unsupported source." >&2; exit 2; }
[[ $SOURCE == redis || $CONTRACT_VERSION == 2 ]] || { echo "Only Redis has a legacy contract." >&2; exit 2; }
command -v python3 >/dev/null && command -v tar >/dev/null && command -v gzip >/dev/null && command -v sha256sum >/dev/null || { echo 'python3, tar, gzip, and sha256sum are required.' >&2; exit 1; }
if [[ -z $ARCH ]]; then
  command -v go >/dev/null || { echo 'go is required when --arch is omitted.' >&2; exit 1; }
  ARCH=$(go env GOARCH)
fi
[[ $ARCH =~ ^[a-z0-9]+$ ]] || { echo 'Plugin architecture is invalid.' >&2; exit 2; }
if [[ -z $OUTPUT ]]; then OUTPUT="$REPO_ROOT/.run/packages/gopulse-${SOURCE}-exporter-$VERSION-linux-$ARCH.tar.gz"; fi
mkdir -p "$(dirname "$OUTPUT")"
OUTPUT=$(python3 -c 'import os,sys; print(os.path.abspath(sys.argv[1]))' "$OUTPUT")
TEMP_DIR=$(mktemp -d)
trap 'python3 -c "import shutil,sys; shutil.rmtree(sys.argv[1], ignore_errors=True)" "$TEMP_DIR"' EXIT
mkdir -p "$TEMP_DIR/package/bin"
if [[ -n $BINARY ]]; then
  [[ -f $BINARY && -x $BINARY ]] || { echo 'The --binary path must be an executable regular file.' >&2; exit 2; }
  install -m 0755 "$BINARY" "$TEMP_DIR/package/bin/gopulse-${SOURCE}-exporter"
else
  command -v go >/dev/null || { echo 'go is required when --binary is omitted.' >&2; exit 1; }
  (cd "$REPO_ROOT/exporters/$SOURCE" && CGO_ENABLED=0 GOOS=linux GOARCH="$ARCH" go build -trimpath -buildvcs=false -ldflags='-buildid=' -o "$TEMP_DIR/package/bin/gopulse-${SOURCE}-exporter" ./cmd/$SOURCE-exporter)
fi
if [[ $CONTRACT_VERSION == 1 ]]; then
DIGEST=$(sha256sum "$TEMP_DIR/package/bin/gopulse-${SOURCE}-exporter" | awk '{print $1}')
python3 - "$TEMP_DIR/package/plugin.json" "$VERSION" "$ARCH" "$DIGEST" <<'PY'
import json,sys
path,version,arch,digest=sys.argv[1:]
manifest={"schema_version":1,"id":"redis-exporter","name":"GoPulse Redis Exporter","version":version,"kind":"metrics-exporter","source":"redis","os":"linux","arch":arch,"entrypoint":"bin/gopulse-redis-exporter","entrypoint_sha256":digest,"health_path":"/health","metrics_path":"/metrics"}
with open(path,'w',encoding='utf-8',newline='\n') as f: json.dump(manifest,f,separators=(',',':'),sort_keys=True); f.write('\n')
PY
FILES=(plugin.json bin/gopulse-${SOURCE}-exporter)
else
  (cd "$REPO_ROOT/monitor" && go run ./cmd/plugin-package-metadata --source "$SOURCE" --directory "$TEMP_DIR/package" --version "$VERSION" --arch "$ARCH")
  chmod 0644 "$TEMP_DIR/package/config.schema.json"
  FILES=(plugin.json config.schema.json bin/gopulse-${SOURCE}-exporter)
fi
chmod 0644 "$TEMP_DIR/package/plugin.json"; chmod 0755 "$TEMP_DIR/package/bin/gopulse-${SOURCE}-exporter"
TAR_PATH="$TEMP_DIR/package.tar"
tar --sort=name --mtime='@0' --owner=0 --group=0 --numeric-owner --format=ustar -C "$TEMP_DIR/package" -cf "$TAR_PATH" "${FILES[@]}"
gzip -n -9 -c "$TAR_PATH" > "$OUTPUT.tmp"
mv -f "$OUTPUT.tmp" "$OUTPUT"
printf '%s\n' "$OUTPUT"
