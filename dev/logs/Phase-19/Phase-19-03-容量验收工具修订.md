# Phase-19-03：容量验收工具修订实施记录

## 执行信息

- 目标版本：`2.1.3`
- 分支：`develop/2.1.3`
- 实施日期：`2026-09-29`
- 本记录对应的最终有界预检目录：`/tmp/gopulse-phase19-03-preflight-20260929k`

## 实际完成工作

- 将 Compose 宿主合同改为可解析的最低版本 `2.24.0`，并固定 21 个必需组件和资源/信号契约。
- 为 Go load runner 增加 append-only、逐条 fsync 的进度记录、窗口边界和报告引用；进度计数字段在新输出中始终存在。
- 重写 Phase 19 runner 的私有输入、确定性配方、空数据卷、每轮 endpoint、候选绑定、真实异步/可观测性回执、资源汇总和归属清理流程。
- 修复实际预检中发现的 MySQL 容器内部地址、RabbitMQ 查询头部、负载进程退出时 sampler 竞态、边界样本周期校验、原始资源覆盖、init 容器资源集合和进度零值字段问题。
- 将 recovery 查询与进度边界采样解耦，并使严格 verifier 从原始报告、进度、资源和阶段回执重算结构与绑定。
- 保持正式 CLI 仅接受 `--manifest` 与新 `--work` 目录；preflight 输出保持 `formal=false`、`capacity_status=null`，不生成正式容量 summary。
- 同步版本元数据与说明文档至 `2.1.3`，并说明正式容量认证仍属于 Phase-19-04。

## 实际变更文件

- `VERSION`、`.env.example`、`frontend/package.json`、`frontend/package-lock.json`、`admin-frontend/package.json`、`admin-frontend/package-lock.json`
- `loadtest/capacity-profile.json`、`loadtest/capacity-profile.schema.json`、`loadtest/cmd/load/main.go`、`loadtest/internal/load/runner.go`、`loadtest/internal/load/types.go`、`loadtest/report.schema.json`
- `scripts/ci/phase19_capacity.py`、`scripts/ci/phase19_sampler.py`、`scripts/ci/phase19_evidence.py`
- `scripts/ci/test_phase19_evidence.py`、`scripts/ci/test_phase19_sampler.py`
- `scripts/verify-phase19-capacity.sh`、`scripts/verify-phase19-evidence.py`
- `README.md`、`docs/capability-status.md`、`docs/capacity-methodology.md`
- `dev/logs/Phase-19/Phase-19-03-容量验收工具修订.md`

## 实际检查与结果

- 执行 `git fetch upstream`，在 `develop/2.1.3` 上完成本批次工作。
- 受影响的 Go 测试通过：`go -C loadtest test -count=1 ./...`。
- Go 竞态测试通过：`go -C loadtest test -race -count=1 ./...`。
- Phase 19 Python 测试通过：14 个测试；三份 CI Python 文件编译通过。
- `scripts/verify-phase19-capacity.sh --self-test` 通过；`scripts/verify-phase19-capacity.sh --calibration` 通过并输出 `formal=false`、`capacity_status=null`、`writes_formal_summary=false`。
- `python3 scripts/ci/validate_versions.py`、`python3 scripts/ci/validate_branch.py --branch develop/2.1.3 --base-ref upstream/main`、Compose config 检查和 `git diff --check` 均通过。
- 最终 bounded preflight：
  `scripts/verify-phase19-capacity.sh --preflight --work /tmp/gopulse-phase19-03-preflight-20260929k`
  成功返回；3 个独立 Compose project、4 个阶梯、真实 recovery/observability raw receipts、资源摘要和 strict validation 均完成；每轮 cleanup 为 `passed`，前后资源清单相同。
- 最终 preflight 输出为 `formal=false`、`capacity_status=null`、`strict_validation.status=passed`，未生成 `capacity-evidence.json`。
- 预检过程中不执行 Docker global prune；最终未残留 `gopulse-p19-*` 容器、卷或网络。

## 偏差与限制

- 实际 Docker Compose 为 `v5.5.0`；按修订后的最低版本合同接受，不再使用固定 major 字符串判断。
- 首轮真实预检暴露了构建并行 npm 缓存竞争、MySQL 宿主端口未发布、RabbitMQ 头部解析、sampler 终止竞态和原始资源摘要问题；均在最终预检前修复并重新验证。
- 有界 preflight 使用私有短时 profile，工作目录中的 corpus、credentials 和环境文件未进入发布证据。
- 本批没有调用正式容量入口，没有运行正式三重复认证，也没有发布 `target_met`、`boundary_found` 或其他容量能力结论；正式认证保留给 Phase-19-04 的冻结 `2.1.3` candidate。
