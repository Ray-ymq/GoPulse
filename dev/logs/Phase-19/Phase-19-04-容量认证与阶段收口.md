# Phase-19-04 容量认证与阶段收口实施日志

## 结果

- 批次分支：`develop/2.1.4`，基于本批开始前最新 `origin/main` 创建。
- 收口版本：`2.1.4`。
- 被测 candidate：版本 `2.1.3`，revision `7251d32a20bc00c126bdef2a207620320f9939bf`，candidate manifest SHA-256 为 `sha256:09ed1dafa379472071f09d55cde57ec29ff9eb28f430f4742479f32b0ddc046e`。
- 正式入口调用一次，三次独立重复、四个固定 RPS 阶梯均执行完成；严格 evidence verifier 通过。
- 最终状态：`execution_status=complete`，`capability_status=boundary_found`。
- 首个观察到的边界为 `50 RPS` 的异步/观测恢复门禁：三次恢复时间分别约为 `123.017s`、`122.794s`、`122.960s`，均超过 `120s`；同步请求门禁在全部阶梯和重复中通过。

## 实际完成工作

- 从 primary remote 获取最新主线并创建 `develop/2.1.4`；候选与 profile、Compose、runtime contract、recipe、宿主和清理回执完成绑定。
- 针对九个 candidate 服务构建带 revision 的本地 candidate images，并用镜像标签检查确认版本为 `2.1.3`、revision 与候选一致。
- 先运行当前候选的有界 preflight，确认宿主、Compose、确定性配方、轮次 endpoint、资源采样和清理路径可用。
- 按计划唯一一次调用正式容量入口，保留三轮原始负载、资源、异步恢复和观测进展证据；未修改产品代码或验收工具，也未筛选或重跑正式结果。
- 生成脱敏发布摘要和 evidence manifest；凭据、DSN、corpus、环境文件、load password 和 lock 文件未进入发布目录。
- 将根 `VERSION`、两个前端 package metadata 与 `.env.example` 同步到 `2.1.4`，并更新阶段状态、能力清单、README、总实施方案和本日志。

## 实际变更文件

- `VERSION`
- `.env.example`
- `frontend/package.json`
- `frontend/package-lock.json`
- `admin-frontend/package.json`
- `admin-frontend/package-lock.json`
- `README.md`
- `docs/capability-status.md`
- `dev/phases/README.md`
- `dev/phases/Plan.md`
- `dev/phases/Phase-19-容量认证与性能可观测.md`
- `dev/imple/Phase-19/Phase-19-总实施方案.md`
- `dev/logs/Phase-19/Phase-19-04-evidence/summary.json`
- `dev/logs/Phase-19/Phase-19-04-evidence/evidence-manifest.json`
- `dev/logs/Phase-19/Phase-19-04-容量认证与阶段收口.md`

本批没有修改产品实现、测试实现或验收程序。

## 实际执行命令与结果

| 命令 | 结果 |
| --- | --- |
| `git fetch origin --prune` | 通过；`origin/main` 更新到 `7251d32a20bc00c126bdef2a207620320f9939bf`。 |
| `scripts/verify-phase19-capacity.sh --preflight --work /tmp/gopulse-phase19-04-preflight-20260929a` | 退出码 `0`；`formal=false`，三轮 preflight 完成，清理通过。 |
| Docker Compose candidate build（九个 candidate 服务，tag `2.1.3-candidate-7251d32a20bc`） | 通过；镜像标签均为候选版本与当前 revision。 |
| `scripts/verify-phase19-capacity.sh --manifest /tmp/gopulse-phase19-04-candidate-7251d32a20bc.json --work /tmp/gopulse-phase19-04-formal-20260929a` | 退出码 `0`；`execution_status=complete`、`capability_status=boundary_found`，三轮完成，清理通过。 |
| `python3 scripts/verify-phase19-evidence.py --capacity /tmp/gopulse-phase19-04-formal-20260929a` | 通过：`PASS: Phase 19 evidence is bound, raw-preserving, and strictly verified (complete/boundary_found)`。 |
| `python3 scripts/ci/sync_version_metadata.py --version 2.1.4` | 通过；六个版本元数据文件同步。 |
| `python3 scripts/ci/validate_versions.py` | 通过：版本元数据与根 `VERSION` 一致。 |
| `python3 scripts/ci/validate_branch.py --branch develop/2.1.4 --base-ref origin/main` | 通过：分支治理检查通过。 |
| `docker compose --env-file .env.example --file deploy/compose.yaml config --quiet` | 通过。 |
| `git diff --check` | 通过。 |
| 发布摘要、manifest 与正式工作目录来源 SHA-256 自检 | 通过；发布摘要及 `46` 个来源哈希一致，发布目录仅包含脱敏摘要和 manifest。 |

## 偏差

- 旧的 Phase-19-03 preflight 证据绑定到已过期 revision，未复用；本批针对当前 candidate 重新执行了有界 preflight。
- 当前环境的 Compose 版本为 `v5.5.0`，runner 按最低版本合同接受该环境；未通过修改工具规避版本检查。
- candidate manifest 只声明版本和 revision，因此正式执行前按该候选绑定构建了本地 images；没有替换正式 evidence 中的候选绑定。
- 首次自定义发布敏感项检查把摘要中对排除文件名的说明误判为泄露；随后改为校验发布目录、JSON 敏感字段、摘要哈希和全部来源哈希，检查通过，未修改 evidence。

## 已知限制与后续事项

- `boundary_found` 只适用于本次冻结 candidate、profile、数据配方和 Linux `amd64` 宿主；不外推为生产容量或公开 RPS 承诺。
- Kafka lag、Outbox pending、观测 Events 不进展和 Kafka 同窗 CPU 峰值是相关窗口证据，不证明单一因果根因；优化与调参留待新的实施批次和总方案。
- 本批按计划不修复边界、不修改产品或验收程序、不进行筛选重跑。Trace、观测数据生命周期、Monitor/状态层 HA 与 Kubernetes 仍未排期。
