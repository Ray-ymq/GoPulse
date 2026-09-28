# Phase-18-01 定界排查实施记录

## 1. 实际完成

- 阅读 Phase-18 总方案、Phase-18-01 原始方案和既有排查结论，确认本任务只做一次短测定界，不调整候选、产品参数、配方规模或验收门禁。
- 为 loadtest 增加可选的私有诊断报告：1 秒窗口，按阶段和路由保留状态码与 P50/P95/P99/max，并在 5xx 时只保存响应 `X-Request-ID`、`error_code`、方法、路由、状态和延迟。
- 新增一次性诊断 runner，复用正式候选绑定、确定性 recipe、正式 search reindex、baseline 重启和持有 project 清理；固定执行 `0s warmup + 2m steady(150 RPS) + 1m burst(300 RPS) + 2m drain`。
- 为诊断 runner 增加 Backend metrics、MySQL Outbox status/attempt/available_at、慢查询与 InnoDB row-lock 状态，以及 Backend claim/publish/retry 日志汇总。
- 在私有 work `.run/phase18-01-diagnostic-v1` 和唯一 project `gopulse-p18-diag-18e59d4de8f8` 执行一次短测；未运行固定三轮 runner，未生成 `capacity.json`。
- 短测完成后汇总阶段因“无 5xx 时 Go 空列表被编码为 JSON null”而抛出 `TypeError`；Compose 项目已在 `finally` 中完整清理。随后只修复该汇总缺陷并新增回归测试，没有重试短测或重建第二个 project。
- 基于已落盘的 raw load report 生成 `diagnostic-summary.partial.json`，并更新 `Phase-18-01-排查结论.md`。

## 2. 实际改动

- `loadtest/cmd/load/main.go`
- `loadtest/internal/load/types.go`
- `loadtest/internal/load/runner.go`
- `loadtest/internal/load/runner_test.go`
- `scripts/ci/phase18_diagnostic.py`
- `scripts/ci/test_phase18_diagnostic.py`
- `dev/imple/Phase-18/Phase-18-01-排查结论.md`
- `dev/logs/Phase-18/Phase-18-01-定界排查.md`

实现提交：

- `166b08b`：诊断报告与一次性 runner；
- `c6c64a3`：空 5xx 列表持久化与 Python 汇总回归修复。

未修改 `VERSION`、候选 manifest、产品镜像、配方规模、正式 SLO/门禁或 Phase-18-02。

## 3. 实际执行的命令与结果

诊断前检查：

```bash
go -C loadtest test ./...
scripts/verify-phase18-capacity.sh --self-test
PYTHONPATH=scripts/ci python3 -m unittest scripts/ci/test_phase18_diagnostic.py
python3 -m py_compile scripts/ci/phase18_diagnostic.py
git diff --check
```

结果：Go tests 通过；capacity self-test `27 tests OK`；诊断 Python tests 通过；编译与 diff check 通过。

宿主 preflight：

```bash
PYTHONPATH=scripts/ci python3 scripts/ci/phase18_capacity.py \
  --preflight-only --work .run/phase18-01-diagnostic-v1
```

结果：`problems: []`；无运行中的 Compose project。候选 registry 仅在本次诊断期间启动，结束后停止。

唯一短测：

```bash
PYTHONPATH=scripts/ci python3 scripts/ci/phase18_diagnostic.py \
  --manifest dist/phase18-01-candidate-v6/release-manifest.json \
  --work .run/phase18-01-diagnostic-v1
```

结果：环境准备、recipe、reindex、baseline 重启和 3 分钟负载完成；load 汇总阶段因空错误列表 `null` 抛出 `TypeError`。允许的 `finally` 清理成功，容器、卷和网络均无残留。随后执行的回归验证：

```bash
go -C loadtest test ./...
PYTHONPATH=scripts/ci python3 -m unittest scripts/ci/test_phase18_diagnostic.py
git diff --check
```

结果：通过。没有重跑环境或短测。

## 4. 短测结果

- 候选 revision：`5aca8753587a3e23730ecc5f90e45dd6ae8e5470`；manifest SHA-256 `56c67a79445b3e3175a92b08e444b10eda746ac89a6aee76e8bad1eb0782c84e`。
- 配方修复基线：`e4e7fd7`；执行时诊断工具 commit：`166b08b`。
- load binary SHA-256：`d62bbc06b6e91df93aafeffb9ce7747e98f109a8e29de89a786557ae763f26ed`。
- recipe binary SHA-256：`4aaa5e080eb4a8eef64baf97dacc84d87deebb293e4d5a0ca3811c143e866231`。
- corpus SHA-256：`e9c6688eddc89c4fbd49d3246ea6b3896af153c33bd8b1e20cbcc4d9048e63e0`。
- steady/burst 各 18,000 请求，全部成功；404、500、timeout、transport error 和明确拒绝均为 0。
- steady `max_schedule_lag_ms=5.2715`、burst `max_schedule_lag_ms=4.8555`、dropped slots 均为 0。
- steady 类别 P95/P99：read `14.69/17.32 ms`、search `16.72/83.81 ms`、content write `15.89/30.63 ms`、interaction write `7.92/10.87 ms`、notification/bookmark read `12.71/23.81 ms`、identity/session `54.58/67.55 ms`。
- steady 首个无预热窗口 P95/P99 为 `218.82/257.77 ms`；其余窗口只有 3 个 P95 超过 50 ms。burst 各窗口 P95 均低于 50 ms。

## 5. 计划偏差

- 为定界新增了最小诊断代码和 runner，属于计划明确允许的准备动作。
- 短测没有额外 warmup，只按任务指定的 steady/burst/drain 窗口运行。
- 预期采集的 Outbox、Backend/MySQL 与容器资源记录因 load 后 TypeError 未写入 `resources.json`；不重试、不以其他时点数据补造。
- 临时 registry 在诊断前启动、结束后停止；未执行 Docker global prune。

## 6. 已知限制与后续项

- 404 已在本轮 36,000 请求中消失，但这只是一次短测，不能替代三轮重复性验收。
- 本轮没有 500 可关联；既有少量 500 的原因仍未定位。
- 稳态本轮未复现旧轮次的周期性跳变，但无三窗口重复性，也未取得同秒 Backend/MySQL/Outbox 证据，根因仍未定位。
- Outbox 净积压与发布速率本轮没有可用证据；不得把旧轮次相关性写成因果。
- Phase-18-01 仍未验收。正式运行前应修复后的 per-second 摘要、corpus/hash 绑定和 Outbox/资源同窗记录纳入正式 capacity runner/evidence，并重新生成候选证据；本次结束点不自动启动正式三轮或 Phase-18-02。

## 7. 证据路径

- `.run/phase18-01-diagnostic-v1/diagnostic/load-report.json`
- `.run/phase18-01-diagnostic-v1/diagnostic/load-diagnostic.json`
- `.run/phase18-01-diagnostic-v1/diagnostic/diagnostic-summary.partial.json`
- `.run/phase18-01-diagnostic-v1/binding.json`
- `.run/phase18-01-diagnostic-v1/evidence/preflight.json`
- `.run/phase18-01-diagnostic-v1/run-error.txt`
