#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."
source /etc/os-release
if [[ "$ID" != debian || "$VERSION_ID" != 13 || "$(dpkg --print-architecture)" != amd64 ]]; then
  echo '此安装入口只支持 Debian 13 amd64；其他系统须另行固定工具版本。' >&2
  exit 2
fi
mapfile -t pt4_packages < <(python3 - <<'PY'
import json
from pathlib import Path
for name, version in json.loads(Path('perflab/pt4/system-tools.json').read_text())['packages'].items():
    print(name + '=' + version)
PY
)
pt4_options=()
if [[ -n "${HTTP_PROXY:-}" ]]; then pt4_options+=(-o "Acquire::http::Proxy=$HTTP_PROXY"); fi
if [[ -n "${HTTPS_PROXY:-}" ]]; then pt4_options+=(-o "Acquire::https::Proxy=$HTTPS_PROXY"); fi
sudo -n apt-get "${pt4_options[@]}" install -y --no-install-recommends "${pt4_packages[@]}"
