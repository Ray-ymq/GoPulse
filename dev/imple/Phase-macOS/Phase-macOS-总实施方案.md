# macOS 兼容适配 Phase 总实施方案

> 当前状态：待实施。规划基线为 `upstream/main` 的产品版本 `1.9.4`。

## 1. 目标与边界

本阶段在真实 Apple Silicon macOS 与 Docker Desktop 上适配 Phase 12 已确立的容器原生本地生命周期。宿主只负责 Bash、Git、Docker CLI/Compose 与普通系统工具；Go、Node.js、Python、数据库客户端和浏览器验收仍在镜像中运行。

不新增业务能力，不调整 Compose 服务、网络、端口、身份、卷与持久化语义，不扩展历史宿主进程验收脚本，也不提前进入 Kubernetes 工作。

## 2. 权威批次分配

本阶段只有一个批次；`1.9.5` 不改变 Phase 13 从 `1.10.1` 开始的分配。

| 执行批次 | 目标版本 | 开发分支 | 状态 |
| --- | --- | --- | --- |
| Phase-macOS-01 | `1.9.5` | `develop/macos` | 待实施 |

`develop/macos` 必须从最新 `upstream/main` 开始，完成后使用普通开发 Pull Request 流程；不得在该分支继续实现 Phase 13 或其他独立任务。

## 3. 实施内容

1. 为权威 Bash 入口提供最小宿主兼容层：安全随机十六进制 token、SHA-256、环境变量检测及必要的路径处理同时支持 Linux 与 macOS 系统工具。
2. 修改 `scripts/dev.sh`、`scripts/verify-compose.sh` 和 `scripts/verify-compose-observability.sh` 使用兼容层；`scripts/down.sh` 与 `scripts/verify.sh` 只在真实失败触发时修改。
3. 保持权威验收的宿主工具白名单，不能因 macOS 适配把 Go、Node.js、Python、curl 或数据库客户端重新引入宿主路径。
4. 扩展分支治理脚本及其直接测试，使 `develop/macos` 只映射 `Phase-macOS-01` 和 `1.9.5`，其他未分配命名分支仍失败。
5. 更新 README、使用说明、版本元数据和本实施方案状态；创建同名实施记录。

## 4. 固定验证顺序

实施中先执行最小检查，最终 diff 只执行一次固定完成门禁：

```bash
/bin/bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-compose.sh scripts/verify-compose-observability.sh
/bin/bash scripts/dev.sh --help
/bin/bash scripts/down.sh --help
/bin/bash scripts/verify-compose.sh --self-test
python3 -m unittest scripts.ci.test_validate_branch scripts.ci.test_validate_versions scripts.ci.test_verify_business
python3 scripts/ci/validate_branch.py --branch develop/macos
python3 scripts/ci/validate_versions.py
scripts/verify-compose.sh
```

若 Docker Desktop 不可用，先保留资源零变更证据并启动 Docker Desktop后重试；只有最终全栈门禁实际成功才可完成本阶段。若真实门禁暴露 Apple Silicon 镜像或 Compose 差异，只修复该失败直接要求的范围并在实施记录说明。

## 5. 批次验收与完成条件

- 系统 Bash 3.2 可解析并运行容器生命周期的帮助、自检和预检路径。
- macOS 不需要安装 GNU coreutils 即可生成安全 token、计算 SHA-256 并建立受限宿主 PATH。
- `develop/macos` 的例外同时受计划分配、目标版本和自动测试约束。
- Docker Desktop 上完整 Compose 验收通过，且退出后未删除或改写验收前的容器、网络、卷、镜像标签及 Git 工作区状态。
- Linux 使用的原命令路径仍由兼容层覆盖；CI 固定门禁通过。
- `VERSION`、`.env.example`、Frontend 包版本、README、使用说明、方案状态和 `dev/logs/Phase-macOS/Phase-macOS-总实施方案.md` 与真实提交一致。

只有以上全部满足且没有阻断失败，Phase-macOS-01 和本阶段才完成。成功后立即停止，不顺带修改历史宿主验收或 Phase 13 内容。
