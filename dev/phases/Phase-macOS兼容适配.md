# macOS 兼容适配 Phase

> 本阶段位于已完成的 Phase 12 与尚未开始的 Phase 13 之间，不占用 Phase 0～16 编号。

## 版本与分支

- 基线版本：`1.9.4`。
- 完成版本：`1.9.5`。
- 唯一执行分支：`develop/macos`。
- 本阶段只有一个执行批次，详细分配以 `dev/imple/Phase-macOS/Phase-macOS-总实施方案.md` 为准。

## 阶段目标

在不改变 Phase 12 业务、数据、身份、容器拓扑与安全契约的前提下，让 Apple Silicon macOS 用户可通过系统 Bash 与 Docker Desktop 使用容器原生开发生命周期，并在同一环境运行权威 Compose 验收。

## 开发范围

- 消除权威生命周期和验收入口对 Linux `/proc`、GNU `sha256sum` 及 Bash 4 专属语法的宿主依赖。
- 保持验收宿主工具白名单和“业务运行时只存在于容器”的隔离证明。
- 让分支治理、版本治理、README 和使用说明准确覆盖 `develop/macos` 与 macOS 前置条件。
- 在真实 Apple Silicon macOS + Docker Desktop 环境运行固定验收矩阵。

## 不做

- 不维护或恢复 PowerShell 能力。
- 不新增业务、API、页面、可观测消息或部署拓扑。
- 不替代 Phase 13～16 的 Kubernetes、Ingress、自观测与工程化路线。
- 不承诺 Intel macOS、非 Docker Desktop 容器引擎或生产部署支持。
- 不将历史宿主进程诊断脚本全部改造成跨平台入口；本阶段只维护 Phase 12 已确立的容器权威路径。

## 阶段验收

- `/bin/bash scripts/dev.sh --help`、`scripts/down.sh --help` 与 `scripts/verify-compose.sh --self-test` 可在 macOS 系统 Bash 3.2 上执行。
- 分支与版本治理接受且只接受计划内的 `develop/macos` / `1.9.5` 例外。
- 容器生命周期预检在 Docker Desktop 未启动时给出明确错误，不产生项目资源。
- Docker Desktop 可用时，`scripts/verify-compose.sh` 在 Apple Silicon macOS 完成唯一-tag镜像构建、全栈业务/可观测闭环、恢复与归属清理。
- macOS 适配不改变 `deploy/compose.yaml` 的内部端口、网络、身份与卷边界，Linux CI 门禁保持通过。
- 对应实施记录、README/使用说明和版本元数据与实际结果一致。

达到上述条件、固定门禁成功且无阻断问题后，本阶段完成并停止；未启动 Docker Desktop 导致无法执行权威全栈验收时，不得把阶段标记为完成。
