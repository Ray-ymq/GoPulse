#!/usr/bin/env bash
# 按顺序执行测试五独立组；每组结束后停止服务，再调整下一组 MySQL CPU 配额。
set -euo pipefail
cd "$(dirname "$0")/../.."

if [[ $# -eq 0 ]]; then
  set -- cpu2-120:120:2 cpu4-120:120:4 cpu4-140:140:4 cpu2-140:140:2
fi

python3 perflab/pt4/stop.py
for spec in "$@"; do
  IFS=: read -r group rps cpus <<<"$spec"
  echo "=== $(date -u +%FT%TZ) $group rps=$rps cpus=$cpus ==="
  python3 -u perflab/pt5/run.py --group "$group" --rps "$rps" --cpus "$cpus"
done
python3 perflab/pt4/stop.py
