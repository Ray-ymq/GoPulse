# Phase-23-05：验收能力原生承接与 `scripts/` 全量退役

> 状态：初稿 2026-10-09。目标版本 `2.5.5`，分支 `develop/2.5.5`。
> 进入条件：主远端 main 含 Phase-23-04 完成提交（实测 `origin/main` = `d6f2a80`，`VERSION=2.5.4`，
> `make package` 已由 `lifecycle/cmd/gopulse-package` 原生承接；`scripts/` 实测 97 文件 / 17,701 行）。
> 用户决定（2026-10-09）：**23-05 单批全部完成** —— `scripts/` 的剩余能力按能力原生承接或按已记录
> 决定退役，使仓库中不再存在 `scripts/` 目录；本批据此登记为长时文件（§5），段边界为强制停点。
> 实现从主远端 main 创建已分配分支并使用独立工作树；不沿用完成分支，不把实现放在 `update`。

## 0. 本批范围为何大于原登记（背景、授权与重登记）

总方案 §5.1 原登记 23-05 为 90/180 分钟，主要交付"按能力删除 + §4 五条全量复验"。实测证据表明该
登记不成立：

| 事实 | 证据 |
| --- | --- |
| 总方案 §1 只登记 3 处硬依赖（镜像构建、交付链、开发命令），未登记 CI 的验收面 | `Phase-23-总实施方案.md` §1"已实测的三处硬依赖" |
| CI 今天仍直接依赖 `scripts/` | `quality-gates.yml:39`（unittest discover 19 个自测模块）、`:41/:43/:46/:52`（4 个校验器）、`:362`（`compose-full-stack` job 的唯一命令 `scripts/verify-compose.sh`）、`:379`（17 个脚本 `bash -n`）、`:382-390`（9 个 `--self-test`）、`:392-405`（Compose 渲染断言） |
| 验收镜像把 `scripts/ci/` 整体嵌入并作为 ENTRYPOINT | `deploy/docker/acceptance.Dockerfile:53,58` |
| 共享交付库仍被 8 个验收执行器导入（23-04 只退役了入口） | `dev/logs/Phase-23/Phase-23-04-交付入口原生承接与隔离安装.md` §已知限制 3 |
| `package-redis-exporter.sh` 仍被 3 个验收脚本与 `devtools` 摘要输入引用 | `verify-monitor.sh:143,198,221,228,251`、`verify-router.sh:435`、`verify-events.sh:150,151`、`devtools/internal/digest/digest.go:145` |
| 总方案 §5.3 的"承接不了就保留"是停靠条款，§4 的终点是"仓库中不存在需要 Python 才能完成的开发、测试、构建或交付步骤" | `Phase-23-总实施方案.md` §4"阶段级集成结果"、§5.3 |

因此本批的删除对象不是"已承接能力的收尾"，而是**整个验收矩阵的原生承接 + 全量退役**：97 文件中
只有治理工具被总方案 §3.3 明确排除在清理之外，其余都必须在删除前有承接或退役依据。用户
2026-10-09 明确选择"23-05 全部完成"，故本方案按长时文件重新登记预算（§5），并同步修订总方案
§2 / §5.1 / §5.4 / §6。

**若后续判断该范围不应由单批承担**，本方案的段结构与批次边界一一对应，可零改写拆分为
`A+B`（入口与 Compose 闭包）、`C`（业务与可观测专项）、`D+E+F`（生命周期、历史矩阵与文档）、
`G+H`（全量复验与删除）四批；拆分只需改总方案 §2/§5.1 的批次表，不需要重写本文件内容。

## 1. 交付与允许文件

**唯一主要交付结果**：仓库中不再存在 `scripts/` 目录（0 文件）；其原有能力全部由 Make 目标 +
Go 模块（`acceptance`、`devtools`、`lifecycle`、`monitor`）+ 既有 Playwright/Vitest/Go 用例 +
Compose 承载，或由本方案登记的退役决定正式退役；并在该终态候选上完成 §4 五条判据的全量复验。

职责边界不变（承总方案 §1）：根 Makefile 只做目标、依赖与参数调度；Compose 仍是运行拓扑的唯一
来源；断言落在 Go 用例或既有前端用例里；`VERSION` 单一来源与发布模型不变；不重建已退役矩阵；
不重写历史证据。

### 1.1 允许新增

| 文件 | 内容 |
| --- | --- |
| `acceptance/go.mod` | 新 Go 模块 `github.com/Ray-ymq/GoPulse/acceptance`（验收编排与断言，非部署组件） |
| `acceptance/Makefile` | 组件目标 `test` / `race` / `check` / `build`（与其余模块同形） |
| `acceptance/cmd/gopulse-acceptance/main.go` | CLI：`compose` / `business` / `observe` / `plugins` / `alerts` / `roles` / `pages` / `lifecycle` / `reconcile-accounts` |
| `acceptance/internal/harness/**` | 项目身份与归属、快照与恢复断言、等待、端口与卷归属、清理、失败传播 |
| `acceptance/internal/contracts/**` | 镜像合同（user / OCI 标签 / entrypoint）、网络与端口边界、只读与挂载、凭据扫描 |
| `acceptance/internal/scenario/**` | 各能力场景驱动（业务、可观测、插件、告警、角色、页面、生命周期安装） |
| `acceptance/internal/receipt/**` | 回执 schema、原子写、逐命令退出码表、脱敏 |
| `acceptance/internal/probe/**`、`acceptance/internal/fixtures/**` | 由 `scripts/ci/testdata/` 迁入的夹具：`component-probe.go`（原消费方 `verify_component_metrics.py:112`）、`plugin-fault-router.go`（`verify_plugin_isolation.py:40`）；`runtime-http.go` 随 `runtime_acceptance.py` 退役，不预置 |
| `devtools/internal/buildcache/**`、`devtools/cmd/devenv` 的 `build-cache` 子命令 | 由 `compose_build_cache.py` 迁入的构建缓存实现与用例 |
| `devtools/internal/stack/**` | 由 `scripts/dev.sh` / `down.sh` / `verify.sh` 迁入的容器原生栈日常生命周期（`stack-up` / `stack-down` / `stack-verify`） |
| `ci/**` | 治理工具新路径：`validate_versions.py`、`validate_branch.py`、`quality_scope.py`、`sync_version_metadata.py`、`AGENTS.md` 及其 5 个自测模块、2 个数据自测（迁移，不重写） |
| `backend/testdata/migration-lock.go`（或 `acceptance/internal/fixtures/`） | `scripts/ci/testdata/migration-lock.go` 的迁移落点（唯一消费方是 `backend/cmd/migrate/main_integration_test.go:103,105`） |
| `deploy/docker/acceptance.Dockerfile` 的内联入口（无新增脚本文件） | 删除 `COPY scripts/ci/` 与 `acceptance-entrypoint.sh`，ENTRYPOINT 直接使用镜像内 `npx playwright test` |

### 1.2 允许修改

| 文件 | 改动 |
| --- | --- |
| `Makefile`（根） | `GO_MODULES` 增加 `acceptance`（Go 14 → 15，模块总数 16 → 17）；新增对外目标：`verify-compose`、`verify-business`、`verify-observe`、`verify-plugins`、`verify-alerts`、`verify-roles`、`verify-pages`、`verify-lifecycle`、`stack-up` / `stack-down` / `stack-verify`、`build-images CACHE=gha`；`help` 行同步；recipe ≤3 行 |
| `devtools/Makefile`、`devtools/internal/digest/digest.go`、`digest_test.go` | 构建缓存与栈目标；`digest.go:145` 删除 `scripts/package-redis-exporter.sh` 摘要输入（23-03 §6 接续项 1） |
| `monitor/Makefile` | 提供稳定二进制入口（`.run/bin/plugin-package`）供原 `package-redis-exporter.sh` 调用方迁移使用 |
| `.github/workflows/quality-gates.yml` | `governance` 4 条命令与 discovery 路径改 `ci/`；新增 `acceptance` 模块 job；`compose-full-stack` 改 `make verify-compose`（并加 `setup-go`，`go-version-file: acceptance/go.mod`）；原 `scripts-and-compose` job 改名为 "Native acceptance tooling"（Runner 的 LF/`bash -n`/`--self-test` 步骤删除，替换为 `make check MODULE=acceptance` + `make test MODULE=acceptance` + Compose 渲染断言） |
| `.github/workflows/cache-warm.yml`、`.github/actions/compose-build-cache/action.yml` | 路径过滤改 `devtools/**`；脚本步骤改 `make build-images CACHE=gha`；job 增加 `setup-go` |
| `.github/workflows/auto-pr-merge.yml` | 仅当改名 job 被引用时同步；否则不动 |
| `deploy/docker/acceptance.Dockerfile`、`.dockerignore` | 删除 `scripts/` 相关的 COPY / ENTRYPOINT / 白名单条目 |
| `ci/quality_scope.py`、`ci/test_quality_scope.py` | 删除 `scripts/**` 映射（`:207-219`），新增 `acceptance/**` 与 `ci/**` 映射；未知路径仍保守全量 |
| `README.md`、`backend/README.md`、`marshaller/README.md`、`monitor/README.md`、`router/README.md`、`exporters/README.md`、`exporters/redis/README.md`、`admin-frontend/README.md`、`deploy/plugins/README.md`、`使用手册.md` | 所有 `scripts/...` 命令改为新 Make 目标；"Existing entry" 表同步 |
| `dev/validation/local-development-tests.md` | 保留项清单（`:25-27,41,81-87`）改写为"能力 → 原生入口"映射表；同时纠正其中已过时的保留声明（"Phase 18～20 的容量、恢复、长期评估、制品和证据工具仍保留"与 `capability-status.md:54` 已记录的退役决定冲突，且 `bash -n scripts/verify-business.sh` 一类命令将在本批失效） |
| `dev/operations/*.md`、`dev/contracts/*.md` | `verify-product-lifecycle.sh`、`verify-runtime-contracts.sh`、`verify_alerts`、`verify_admin_visual`、`migration-state.md` 等的入口命令改指新目标；契约条款不动 |
| `dev/validation/Phase-16/phase16-linux-matrix.md` | 历史命令保留原文，增加一行"执行器已随 23-05 退役，重跑需新方案"；候选冻结与矩阵语义不动 |
| `dev/imple/AGENTS.md`、`dev/phases/AGENTS.md` | 对 `scripts/AGENTS.md` 的引用改 `ci/AGENTS.md` |
| `dev/status/capability-status.md` | 新增 `scripts/` 退役范围、逐能力承接证明、退役依据与能力边界；`VERSION` 基线更新为 `2.5.5` |
| `dev/imple/Phase-23/Phase-23-总实施方案.md`、本文件、同名实施日志 | 范围/预算/决策同步与实测记录 |
| `VERSION`、`.env.example`、`frontend/package{,-lock}.json`、`admin-frontend/package{,-lock}.json`、`deploy/runtime-contracts.json` | 同步到 `2.5.5` |

### 1.3 允许删除（按能力，逐文件；行数为 `origin/main` 实测）

全部 97 文件都必须在本批结束前删除；删除时机由所在段决定，且必须先满足该行的"删除条件"。

| 组 | 文件（行数） | 承接目标 | 删除条件（门禁） | 段 |
| --- | --- | --- | --- | --- |
| G1 构建缓存 | `ci/compose_build_cache.py` 108、`ci/test_compose_build_cache.py` 122 | `devtools/internal/buildcache` + `make build-images CACHE=gha` | A1–A4 | A |
| G2 插件打包转发 | `package-redis-exporter.sh` 14 | `make -C monitor plugin-package-bin`（`.run/bin/plugin-package`） | C2-6（三个调用方已迁移） | C |
| G3 Compose 全栈闭包 | `verify-compose.sh` 447、`verify-compose-observability.sh` 795 | `acceptance compose` + `make verify-compose` | B1–B6 | B |
| G4 候选与交付环境 | `ci/candidate_runtime.py` 56、`ci/release_candidate_env.py` 15 | `acceptance` 候选解包（复用 `lifecycle/cmd/gopulse-package env`） | B6、C1-4、C2-6 | B/C |
| G5 业务验收 | `verify-business.sh` 1560、`ci/test_verify_business.py` 383 | `make verify-business` + 既有 Playwright spec + Go 用例 | C1-1…C1-5 | C |
| G6 可观测专项 | `verify-exporter.sh` 520、`verify-monitor.sh` 258、`verify-router.sh` 531、`verify-marshaller.sh` 852、`verify-logs.sh` 364、`verify-events.sh` 316、`ci/test_marshaller_cleanup.py` 25 | `make verify-observe SCOPE=<suite>` | C2-1…C2-7 | C |
| G7 插件·告警·角色·页面 | `ci/verify_plugin_metrics.py` 342、`verify_plugin_isolation.py` 322、`verify_plugin_topology.py` 409、`verify_plugin_clusters.py` 242、`verify_plugin_state.py` 173、`verify_component_metrics.py` 417、`verify_alerts.py` 352、`verify_alert_sources.py` 131、`verify_role_management.py` 394、`verify_admin_visual.py` 131、`verify_admin_frontend.py` 103、`verify_dashboard.py` 101、`reconcile_plugin_accounts.py` 243、`frontend_bundle_browser.py` 83；转发器 `verify-plugin-metrics.sh` 11、`verify-component-metrics.sh` 4、`verify-plugin-state.sh` 4、`verify-alerts.sh` 4、`verify-role-management.sh` 4、`verify-admin-frontend.sh` 9、`verify-observability-ui.sh` 9、`reconcile-plugin-accounts.sh` 4 | `make verify-plugins\|verify-alerts\|verify-roles\|verify-pages` | C3-1…C3-9 | C |
| G8 产品生命周期与恢复 | `ci/verify_product_lifecycle.py` 210、`verify_reused_install.py` 36、`verify-product-lifecycle.sh` 4、`verify_current_recovery.py` 339、`verify_backup_restore.py` 283、`verify-backup-restore.sh` 4、`ci/test_verify_current_recovery.py` 38 | `acceptance lifecycle`（D1）；恢复/备份断言迁入 Go 或用例 | D1-1…D1-6 | D |
| G9 运行时契约与阶段矩阵 | `ci/runtime_acceptance.py` 684、`test_runtime_acceptance.py` 103、`ci/verify_runtime_contracts.py` 575、`test_runtime_contracts.py` 25、`verify-runtime-contracts.sh` 16、`ci/phase16_acceptance.py` 169、`phase16_evidence.py` 108、`test_phase16_evidence.py` 60、`phase17_evidence.py` 98、`verify_phase17.py` 114、`verify_phase17_state.py` 176、`verify_phase17_migration.py` 222、`test_phase17_evidence.py` 37、`test_phase17_state.py` 45、`verify_phase14_closure.py` 290、`verify_phase15_closure.py` 490、`test_phase14_cleanup.py` 46、`verify-phase16-evidence.py` 15、`verify-phase17-evidence.py` 11、`verify-phase17.sh` 4、`verify-phase17-state.sh` 4、`ci/acceptance-entrypoint.sh` 7 | 退役（依据见 §1.5） | E1-1…E1-4、D2 | D/E |
| G10 交付库 | `ci/release_artifacts.py` 302、`release_manifest.py` 95、`test_release_artifacts.py` 75、`test_release_manifest.py` 47、`test_release_snapshot.py` 34 | Go `lifecycle/internal/packaging` 已承接 manifest / bundle / env / promote | E2-1…E2-3 | E |
| G11 治理与版本工具 | `ci/validate_versions.py` 76、`validate_branch.py` 224、`quality_scope.py` 334、`sync_version_metadata.py` 107、`test_validate_versions.py` 73、`test_validate_branch.py` 167、`test_quality_scope.py` 120、`test_sync_version_metadata.py` 78、`test_auto_pr_workflow.py` 67、`AGENTS.md` 70 | **迁移**到 `ci/`（不重写，保留 Python；总方案 §3.3 明确排除其清理） | F1-1…F1-4 | F |
| G12 容器栈日常入口 | `dev.sh` 171、`down.sh` 86、`verify.sh` 129 | `devtools/internal/stack` + `make stack-up\|stack-down\|stack-verify` | C4-1…C4-3 | C |
| G13 Windows | `dev.ps1` 667、`down.ps1` 172、`verify.ps1` 181 | 退役（零活引用；Windows 支持边界见 §1.5） | E3-1 | E |
| G14 其他入口与夹具 | `ci/test_compose_acceptance_env.py` 39、`ci/testdata/component-probe.go` 58、`plugin-fault-router.go` 57、`runtime-http.go` 53、`migration-lock.go` 33、`start-development-batch.sh` 99、`test-backup-format.sh` 5、`test-frontends.sh` 6、`test-lifecycle.sh` 5 | `component-probe.go`、`plugin-fault-router.go` 迁入 `acceptance/internal/fixtures`（C3 使用）；`migration-lock.go` 迁到 `backend/testdata`（唯一消费方 `backend/cmd/migrate/main_integration_test.go:105`）；`runtime-http.go` 随 `runtime_acceptance.py` 退役（其唯一消费方已退役；承接后的 suite 若确需容器内探针，再在 `acceptance/internal/probe` 内新增，不预置）；`start-development-batch.sh` 的校验职责由 `ci/` 或 `make` 目标承载；其余零引用退役 | E3-2、F2-1 | E/F |

合计：97 文件 / 17,701 行（与 §1.7 实测一致）。

### 1.4 明确保留（不在本批删除）

| 文件 | 理由 |
| --- | --- |
| `dev/validation/Phase-16/*.md`、`dev/validation/Phase-17/phase17-compose-matrix.md`、`dev/contracts/**`、`dev/logs/**`、`dev/imple/**` | 历史与契约记录，不因执行器退役改写；只在必要处补一行退役说明 |
| `deploy/phase16-acceptance.yaml` | 属 `deploy/`，总方案 §5.4 明确本阶段不修改；其 `phase16` 入口模式随 G9 退役后成为无引用文件，登记为后续项 |
| `lifecycle/**`、`devtools/**`、`monitor/**` 的既有实现与用例 | 承接方，本批只新增包与目标 |

### 1.5 明确退役及其依据（退役必须点名能力、决定与保留边界）

| 退役能力 | 文件 | 依据 |
| --- | --- | --- |
| Phase 16 / 17 正式矩阵执行器与证据校验器 | G9 中 `phase16_*` / `phase17_*` / `verify-phase1{6,7}*` / `test_phase1*`（13 文件 / 1,063 行，已从工作树重建并与总方案 §5.4 的"13 文件 / 1,063 行"逐文件核对一致） | `dev/status/capability-status.md:54` 已记录用户决定"Phase 16–20 正式矩阵不再重跑，退役不需要等价替代"；Phase 16/17 链在 CI 不可达（唯一入口 `verify-phase17.sh` 无调用者），`verify_phase17.py:51`、`verify_phase17_state.py:20` 还钉死 `1.13.6 → 1.14.5`，在当前 `VERSION` 下不可运行 |
| Phase 13 / 14 / 15 历史闭包路径 | `verify_phase14_closure.py`、`verify_phase15_closure.py`、`test_phase14_cleanup.py` 及 `verify-compose.sh --phase13/--phase14/--phase15` 分支 | Phase 12-03 起权威门禁是无参全栈闭包；三个历史路径无 CI 与计划调用者，`capability-status.md:56` 的保留理由随无参闭包原生承接而失效 |
| Phase 17 / 21 运行时矩阵 | `runtime_acceptance.py`、`test_runtime_acceptance.py`、`verify_runtime_contracts.py`、`test_runtime_contracts.py`、`verify-runtime-contracts.sh` | 两个阶段均已关闭（Phase 21 在 `2.3.3` 收口，`capability-status.md:9,35`）；执行器无 CI 调用，`verify-runtime-contracts.sh` 无执行调用者；`deploy/runtime-contracts.json` 的候选校验由 `gopulse-package verify` 与新的 `make verify-compose` 覆盖 |
| Windows PowerShell 入口 | `dev.ps1`、`down.ps1`、`verify.ps1` | 零执行调用者：三个文件均只在文档与彼此的一条报错文案中被提及（`dev.ps1:327` 提示用户手工运行 `down.ps1`，不是调用）；能力由 Go/Make 与容器原生栈承载；Windows 原生支持不再声明，边界写入 `capability-status.md` |
| 零引用或纯转发入口 | `test-lifecycle.sh`、`test-backup-format.sh`、`test-frontends.sh`、`verify-observability-ui.sh`（兼容转发）、`reconcile-plugin-accounts.sh` 以及 G7/G9 内已随能力承接的转发器 | 零执行调用者或唯一职责是转发到本轮已承接/退役的实现；文档引用同步更新 |

**保留 Python 的例外**：治理与版本校验器（`ci/**`，1,316 行）保留 Python 实现。总方案 §3.3 明确其
"保留为独立步骤，不属于本次清理对象"，§4 的阶段结果也只覆盖"开发、测试、构建或交付步骤"。若后续
要求连它们也 Go 化，追加子段 F1b（预计 240 / 上限 300），不在本批累计上限内。

### 1.6 不改变

- business/platform 分离部署、业务 API、权限、消息与正式证据合同；正式证据 schema 与历史回执。
- `VERSION` 单一来源与发布模型（本阶段不做组件独立版本发布）；目录结构（除新增 `acceptance/` 与
  `ci/` 两个目录）。
- 冻结的两篇设计、落地大纲（三个文件不改）；已退役矩阵不重建；历史日志与原始证据不改写。

### 1.7 开工前必须核实的实测事实（硬约束）

| 事实 | 实测值 | 用途 |
| --- | --- | --- |
| 基线 | `origin/main` = `d6f2a80`，`VERSION=2.5.4` | 建分支与候选身份 |
| 删除集合 | `git ls-tree -r --name-only origin/main -- scripts` = **97 文件 / 17,701 行** | §1.3 台账 |
| CI 依赖面 | `quality-gates.yml:39,41,43,46,52,362,379,382-390,392-405`；`release-candidate.yml:31` 已无 Python | 段 B/F 门禁 |
| 镜像依赖 | `deploy/docker/acceptance.Dockerfile:53,58`；`.dockerignore:42-46` | 段 D2 |
| 摘要依赖 | `devtools/internal/digest/digest.go:145` 与 `digest_test.go:121,132` | 段 C 门禁 C2-6 |
| 构建缓存 | `docker compose ... build --print` 对 10 个逻辑目标返回 13 个 target，脚本按 `TARGETS` 过滤；CI 两个使用点 `action.yml:64-67`、`cache-warm.yml:18,36` | 段 A |
| 测量到的真实耗时 | 集成 business 28 s / observe 60 s（旧实现基线）、e2e business 62 s / observe 96 s、隔离安装 3 m 44 s、Monitor 冷建 125 s / 复用 30 s、`make deps` 23 s、全新检出 `make dev` 98 s | §5 实验成本 |
| 未记录项（不得假设） | 任何 `make test MODULE=<模块>` 与 `make check` 的耗时、`make package` 单次耗时、CI job 墙钟 | 预算按上界估计 |

## 2. 目标架构与承接设计

### 2.1 新模块 `acceptance`

- 一个模块、一个 CLI，按能力分子命令；根 Makefile 的 `GO_MODULES` 仍是模块清单唯一来源。
- 复用 `devtools` 已验证的执行基础（`internal/proc` 的进程运行与归属、`internal/compose` 的
  `docker compose` 参数渲染、`internal/workspace` 的工作区身份与私有状态、`internal/digest` 的内容
  摘要、`internal/ready` 的就绪轮询）。跨 Go 模块不能直接 import，默认做法是：`acceptance` 模块内
  自带最小 `compose` / `proc` / `workspace` 适配，行为契约由用例锁定；若实施中发现两处必须共享同一
  实现，则把共享部分上移到 `devtools/internal/harness`，并在 `acceptance/go.mod` 用 `replace` 引用，
  保证同一候选内只存在一份实现（该选择记入实施日志与 §8）。
- 每个子命令支持 `--keep`（保留现场）与 `--receipt PATH`（回执输出）；失败一律非零退出并只清理本次
  创建的资源；所有断言失败都带可操作诊断（沿用被替代脚本的文案要点）。
- 单元测试用可注入命令执行接缝（`devtools/internal/testenv` 的 function-field 模式），覆盖 CLI 解析、
  失败传播、退出码、归属清理与回执 schema；真实依赖断言留在真实门禁（§4.3）。

### 2.2 构建缓存与插件打包承接

- `devtools` 新增 `build-cache` 子命令与 `devtools/internal/buildcache`：`docker compose --print` 取
  定义 → 过滤 10 个逻辑目标、强制 `linux/amd64` + `type=docker` → 有 `ACTIONS_RUNTIME_TOKEN` /
  `ACTIONS_RESULTS_URL` 时加 `type=gha` 缓存 → `docker buildx bake --load`；缓存类失败只重试一次并
  去掉 `cache-*`，编译器失败不重试；退出码原样传递（`proc` 现无退出码透传，需新增返回码的助手）。
  `make build-images` 增加 `CACHE=gha|none`，本地与 CI 走同一目标。
- `package-redis-exporter.sh` 的调用方在 C 段整体迁移到 `make -C monitor plugin-package-bin`（输出
  `.run/bin/plugin-package`，退出码语义不变，usage 错误仍为 2）；随后删除该转发器，并同步
  `devtools` 摘要输入集（否则 Monitor 摘要恒变、每次重建）。

### 2.3 治理与版本工具迁移

- `scripts/ci/{validate_versions,validate_branch,quality_scope,sync_version_metadata}.py`、5 个自测、
  `test_auto_pr_workflow.py`、`AGENTS.md` 原样迁移到 `ci/`；`governance` job 的 4 条命令与 discovery
  路径、`dev/imple/AGENTS.md`、`dev/phases/AGENTS.md`、`dev/README.md`、`README.md` 的引用同步更新。
- `quality_scope.py` 的 `scripts/**` 分支（`:207-219`）删除，新增 `ci/**`（治理自测 + 对应检查）与
  `acceptance/**`（acceptance 模块 + Compose/集成/浏览器按需）映射；未知路径仍保守全量。
- 迁移只改路径与引用，不改判定逻辑；迁移后 `python3 -m unittest discover -s ci -p 'test_*.py'` 必须
  与迁移前用例数一致（数量变化须逐项解释）。

### 2.4 验收镜像与浏览器入口

- `acceptance.Dockerfile` 删除 `COPY scripts/ci/ /work/scripts/ci/` 与
  `ENTRYPOINT ["/bin/sh", "/work/scripts/ci/acceptance-entrypoint.sh"]`，改为镜像内
  `ENTRYPOINT ["npx", "playwright", "test"]`；`--profile acceptance run ... <spec>` 的调用方式不变。
- `phase16` 入口模式随 G9 退役；`.dockerignore` 中 `scripts/` 相关条目删除。

### 2.5 证据与回执合同

- 回执字段沿用被替代执行器的语义（如生命周期的 `status` / `cleanup_passed` / `isolation_preserved` /
  逐命令退出码表）；新增回执使用 `gopulse.acceptance.v1` schema。
- 所有真实验收的原始证据写入工作区私有根（`acceptance` 模块的 workspace 私有目录），日志只记录
  命令、结果、耗时与路径；不把回执写入历史证据目录。

### 2.6 计划接口（本批实施时落地，落地前不得被引用）

下表全部是**计划**接口，不是既有接口。每个接口 1:1 替换一个既有脚本调用，不新增能力面；在该接口
实现并由 §4.3 对应用例证明之前，不得写进 README、规则文档或其他批次方案，也不得被后续门禁依赖。

| 计划接口 | 替换对象 | 落地段 | 依赖它的门禁 |
| --- | --- | --- | --- |
| `make verify-compose [SCOPE=observability]` | `scripts/verify-compose.sh`、`scripts/verify-compose-observability.sh` | B | B3、B4、B6、C3、H5 |
| `make verify-business` | `scripts/verify-business.sh` | C | C1-1…C1-5、C2、H5 |
| `make verify-observe SCOPE=<suite>` | `verify-{exporter,monitor,router,marshaller,logs,events}.sh` | C | C2-1…C2-7、H5 |
| `make verify-plugins\|verify-alerts\|verify-roles\|verify-pages` | 14 个专项校验器与 8 个转发器 | C | C3-1…C3-9、H5 |
| `make verify-lifecycle INSTALL=clean\|reuse` | `scripts/ci/verify_product_lifecycle.py`、`verify_reused_install.py` | D | D1-1…D1-7、C4 |
| `make stack-up\|stack-down\|stack-verify` | `scripts/dev.sh`、`down.sh`、`verify.sh` | C | C4-1…C4-3、C1 |
| `make build-images CACHE=gha\|none [DRY_RUN=1]` | `scripts/ci/compose_build_cache.py` 的两处 CI 调用 | A | A1–A5 |
| `make -C monitor plugin-package-bin` | `scripts/package-redis-exporter.sh` | C | C2-6 |
| `devenv build-cache [--print] [--no-cache]` | 同上（本机与 CI 同一实现） | A | A1–A4 |
| `gopulse-acceptance <subcommand> [--keep] [--receipt PATH] [--manifest PATH] [--candidate VER]` | 上述各脚本的 CLI 表面 | B–D | 全部真实门禁 |
| `ci/{validate_versions,validate_branch,quality_scope,sync_version_metadata}.py` | `scripts/ci/` 下同名工具（原样迁移） | F | F1-1…F1-4 |

## 3. 分段执行与强制停点

段边界是强制停点：前一段门禁未通过不得进入下一段，也不得用后一段的通过替代前一段的失败。每段
开工第一步是**断言清单取证**（§4.2），最后一步是门禁与证据登记。

| 段 | 范围 | 预计 / 上限 | 强制停点产物 |
| --- | --- | --- | --- |
| A | 构建缓存承接（G1）与 CI 面基线 | 120 / 180 | A1–A5 通过；CI 面基线清单 |
| B | Compose 全栈闭包承接（G3、G4 的 `release_candidate_env`） | 240 / 300（B1/B2 各 ≤180） | B1–B6 通过；`make verify-compose` 真实闭包回执 |
| C | 业务与可观测专项承接（G5、G6、G7、G2、G4、G12） | 420 / 540（C1 130/180、C2 150/180、C3 110/180、C4 30/120） | C1-1…C4-3 通过；各 suite 真实回执 |
| D | 产品生命周期承接与验收镜像入口（G8、G9 的 entrypoint） | 150 / 180 | D1-1…D2-2 通过；隔离安装回执 |
| E | 历史矩阵、交付库与零引用退役（G9、G10、G13、G14 的退役部分） | 120 / 180 | E1-1…E3-2 通过；活引用 0 证明 |
| F | 治理工具迁移与文档收口（G11、G14 的迁移部分） | 120 / 180 | F1-1…F3-2 通过；文档 0 处旧命令 |
| G | §4 五条全量复验（全新检出、冻结候选） | 180 / 240 | C1–C5 全部通过并留原始回执 |
| H | 全量删除与阶段收口 | 90 / 120 | `scripts/` 0 文件；CI 绿；日志、`VERSION=2.5.5`、完成提交 |

## 4. 验证映射与固定门禁

### 4.1 验证映射

| 字段 | 内容 |
| --- | --- |
| 验证对象 | ① 被替换执行器的断言集合在新实现下仍成立（成功与必要失败条件）；② CI 与本地调用同一 Make 目标；③ `scripts/` 删除后仓库无活引用；④ §4 五条判据在终态候选上成立 |
| 已有覆盖 | 被替代的 97 个文件的既有 `--self-test` 与断言；`devtools` 的 testenv/proc/workspace 用例；`backend` 等模块的 Go 集成用例；`frontend/e2e/*.spec.ts`；`lifecycle/internal/{control,packaging}` 用例；`ci/` 治理自测（迁移后） |
| 最低有效层级 | 行为等价用**真实系统层**（真实 Compose 项目、真实容器、真实浏览器、真实安装），CLI 解析/退出码/回执/失败传播用**单元层**。理由：本批改变的是编排与断言实现，静态检查无法证明归属、清理、网络与持久化事实 |
| 本批变更 | 新增 `acceptance` 模块与目标；`devtools` 增加 cache/stack；CI job 与路径改写；`acceptance.Dockerfile` 入口改写；`ci/` 迁移；删除 97 文件 |
| 固定门禁 | §4.3 段门禁 + §4.4 五条判据；每条含命令、期望结果、证据与失败分类 |

### 4.2 断言清单取证（每段第一步）

每段开工先产出该段被替代文件的断言清单（按函数/用例分组，标注真实依赖、等待上界、清理范围与
失败注入），写入实施日志；清单完成前不删除对应文件。清单同时作为"删除条件"的核对表：某一组断言
在新入口没有对应用例且无退役依据时，该文件不得删除，按 §6 的偏差处理在该段边界登记为停点。

### 4.3 段门禁

**段 A（构建缓存与 CI 面）**

| 编号 | 判据 | 命令 / 方式 | 期望结果与证据 |
| --- | --- | --- | --- |
| A1 | 定义等价 | `make build-images DRY_RUN=1` 与 `devenv build-cache --print` 对比 | 10 个目标逐个字段相等，差异只有 `platforms` / `output` / `cache-from` / `cache-to`（23-02 B2 的同一比对） |
| A2 | 缓存参数正确 | `ACTIONS_RUNTIME_TOKEN=x ACTIONS_RESULTS_URL=y devenv build-cache --print` | 每个目标有独立 `gopulse-<name>-linux-amd64-v1` scope、`ignore-error=true`；token 不出现在输出 |
| A3 | 单测覆盖 7 条行为 | `make test MODULE=devtools`、`make race MODULE=devtools`、`make check MODULE=devtools` | 冷构建、过滤等价、缓存失败重试一次、重试失败原样传播（23）、编译器失败不重试（17）、`--no-cache`、`--print` 全部有对应用例 |
| A4 | 真实 bake 可用 | 真实 `devenv build-cache --print` 后对 1 个目标 `docker buildx bake --load`（如 frontend，参考 37 s） | 退出 0，产出镜像；缓存类失败路径在真实环境不误判 |
| A5 | CI 切换 | `action.yml` 改为 `make build-images CACHE=gha`；`cache-warm.yml:18` 路径过滤改 `devtools/**`；两个 job 加 `setup-go` | YAML 可解析；`grep -c compose_build_cache .github/ Makefile` = 0；CI 结果在段 G/H 的分支运行中确认 |

**段 B（Compose 全栈闭包）**

| 编号 | 判据 | 命令 / 方式 | 期望结果与证据 |
| --- | --- | --- | --- |
| B1 | 编排核心可测 | `make test MODULE=acceptance`、`make race MODULE=acceptance`、`make check MODULE=acceptance` | 项目身份/归属快照/等待/清理/回执/端口冲突均有单测；失败传播非零 |
| B2 | `--self-test` 安全边界等价 | 旧 `bash verify-compose.sh --self-test` 的断言（项目名校验、回环发布）逐条对照新 Go 用例 | 逐条有对应用例；无 Docker 也能运行 |
| B3 | 业务路径真实通过 | `make verify-compose`（默认全栈） | 退出 0；15 个断言组全过；11 次验收容器运行；`--keep` 时现场保留、默认清理后无容器/网络残留、命名卷按合同处理 |
| B4 | 全量观测路径真实通过 | `make verify-compose SCOPE=observability` | 退出 0；40 个断言组（含 candidate 模式、隔离/权限/挂载、凭据扫描、插件矩阵）全过；24 次验收容器运行 |
| B5 | 失败注入 | 占用回环端口、未归属项目同名、构建失败各一次 | 均非零退出、只清理自有资源、既有项目不被触碰、诊断可操作 |
| B6 | CI 切换 | `quality-gates.yml:350-362` 改 `make verify-compose`（加 `setup-go`）；`test_auto_pr_workflow.py` 的断言同步 | YAML 可解析；CI `compose-full-stack` job 绿（段 G/H 的分支运行） |

**段 C（业务、可观测与专项）**

| 编号 | 判据 | 期望结果与证据 |
| --- | --- | --- |
| C1-1…C1-5 | `make verify-business` 的业务矩阵逐组等价（含隔离项目、真实 DB/消息、权限、失败恢复），`test_verify_business.py` 的断言迁为 Go 用例 | 退出 0；逐组对照清单；真实回执与耗时登记 |
| C2-1…C2-7 | `make verify-observe SCOPE=exporter\|monitor\|router\|marshaller\|logs\|events` 逐 suite 真实通过 | 每个 suite 退出 0；容器安全、插件制品、重启持久化、故障恢复断言逐组对照；`candidate_runtime` 的两处调用由新候选解包替换 |
| C2-6 | `package-redis-exporter.sh` 退役 | 三个调用方改用 `make -C monitor plugin-package-bin`；`digest.go` 输入集删除该路径；连续两次 `make dev-observe` 复用同一 Monitor tag（23-03 C5 证据） |
| C3-1…C3-9 | `make verify-plugins\|verify-alerts\|verify-roles\|verify-pages` 真实通过 | 插件生命周期/隔离/拓扑/集群/状态、告警来源与状态、角色管理、管理页面视觉与聚合页逐组对照；`reconcile_plugin_accounts.py` 的账户和解由新子命令承接（真实 MySQL） |
| C4-1…C4-3 | 容器栈日常入口承接 | `make stack-up` → `make stack-verify` → `make stack-down` 在干净工作树通过；`down` 不删命名卷；归属校验与 `scripts/down.sh` 的既有合同一致 |

**段 D（产品生命周期与镜像入口）**

| 编号 | 判据 | 期望结果与证据 |
| --- | --- | --- |
| D1-1…D1-5 | `make verify-lifecycle INSTALL=clean PLATFORM=linux/amd64` | 14 条命令退出码与矩阵一致；`status=passed`、`cleanup_passed=true`、`isolation_preserved=true`；edge HTTP `/`、`/admin/`、`/health` 200；只读 `verify` 前后 `state.json`/`secrets.json` 字节相等；无密钥泄漏；安装路径含空格 |
| D1-6 | 失败矩阵 | 锁并发 16、无守护进程 11、篡改 Bundle 10、低磁盘 13、端口占用 15、非私有目录 14、错误确认 2、外部同名卷 17、SIGINT/SIGTERM 20、暂停依赖 18、verify 未就绪 19 逐条非零且清理正确 |
| D1-7 | 复用安装 | `make verify-lifecycle INSTALL=reuse MANIFEST=…` 的前置与 `/login`、`/admin/` 200 断言等价 |
| D2-1…D2-2 | 验收镜像入口改造 | 不再 COPY `scripts/`；`docker compose --profile acceptance run --rm --no-deps acceptance e2e/compose-smoke.spec.ts` 通过 |

**段 E（历史矩阵、交付库与零引用退役）**

| 编号 | 判据 | 期望结果与证据 |
| --- | --- | --- |
| E1-1…E1-4 | 退役集合逐项核对 | 13 文件 / 1,063 行与工作树逐文件一致；无活导入；`python3 -m unittest discover`（迁移后 `ci/`）用例数变化逐项解释；`grep -rn 'phase1[67]' .github Makefile */Makefile acceptance ci devtools lifecycle` 无活引用 |
| E2-1…E2-3 | 交付库退役 | `release_artifacts` / `release_manifest` / `release_candidate_env` 活导入 0（`grep -rn` 证明）；`make package` 与 `gopulse-package env` 回归通过；`make package` 第二次数值错误（拒绝覆盖）仍为退出 2 |
| E3-1…E3-2 | 零引用与 Windows 退役 | `git grep` 证明 0 执行调用者；文档引用已改写；`testdata` 夹具迁入后 `make test MODULE=backend` 与 `make test MODULE=acceptance` 通过 |

**段 F（治理迁移与文档）**

| 编号 | 判据 | 期望结果与证据 |
| --- | --- | --- |
| F1-1…F1-4 | 迁移等价 | `ci/` 下 4 个校验器 + 自测原样运行；`python3 -m unittest discover -s ci -p 'test_*.py'` 与迁移前用例数一致；`quality_scope.py` 对 `acceptance/**`、`ci/**`、`devtools/**` 选择正确；`validate_branch.py --mode development` 在 `develop/2.5.5` 通过 |
| F2-1 | 文档 0 处旧命令 | `grep -rn 'scripts/' README.md */README.md 使用手册.md dev/validation dev/operations dev/contracts`（排除历史日志与 `dev/imple/**`）无遗留可执行命令 |
| F3-1…F3-2 | CI 步骤替换 | `scripts-and-compose` job 改名并改跑 `acceptance` 模块检查 + Compose 渲染断言；Runner 的 `bash -n` / `--self-test` 步骤删除；YAML 可解析 |

**段 H（删除与收口）**

| 编号 | 判据 | 期望结果与证据 |
| --- | --- | --- |
| H1 | 目录消失 | `git ls-files scripts` 为空；`test ! -d scripts` |
| H2 | 活引用为 0 | `git grep -n -F -e 'scripts/' -- . ':!dev' ':!docs'` 无命中；`dev/**` 命中只在历史文档 |
| H3 | 能力台账 | `dev/status/capability-status.md` 记录逐能力承接目标/退役依据/能力边界；Python 保留例外点名 |
| H4 | 版本与日志 | `VERSION=2.5.5`，元数据同步；同名实施日志只记录实际完成项；`validate_branch.py --mode completion` 通过 |
| H5 | 分支 CI | `develop/2.5.5` 全 job 绿；模块、integration、e2e、`compose-full-stack`（现 `make verify-compose`）、新 acceptance 与 governance job 均实际执行 |

### 4.4 §4 五条判据（段 G 全量复验）

在**全新 `git worktree`**（无 `.run/`、无 `node_modules`、无手工准备）中对冻结候选执行：

| 编号 | 判据 | 命令 / 方式 | 期望结果与证据 |
| --- | --- | --- | --- |
| C1 | 启动开发环境并安全停止 | `make deps`（两次）→ `make dev` → 端点 2xx → `make stop` → `make dev-observe` → `make stop` | 依赖健康且幂等；停止后无本工作区容器/网络，命名卷保留，端口可重新绑定；不存在未归属资源被误删 |
| C2 | 模块、集成与浏览器测试 | `make test MODULE=<17 模块>`、`make check-all`、`make integration SCOPE=business\|observe`、`make e2e SCOPE=business\|observe`；并发第二次 `make integration` | 全过；并发第二次在锁处立即非零且互不干扰；真实回执 `status=passed`、0 残留 |
| C3 | CI 使用相同入口 | 推送 `develop/2.5.5` 后核对全部 job | 全绿；产品 job 均调用 Make 目标；`grep -rn 'python3 scripts/' .github` = 0；治理 job 只调用 `ci/` |
| C4 | 生成 Bundle 并隔离安装 | `make package PLATFORM=linux/amd64 RUNTIME=1` → `make verify-lifecycle INSTALL=clean MANIFEST=dist/release-manifest.json PLATFORM=linux/amd64` | Bundle/manifest/摘要产出；隔离安装 14 条退出码一致、卸载后无残留 |
| C5 | 失败非零退出并清理本次资源 | 端口占用、依赖未就绪、测试失败、构建失败、非法 `VERSION`、未知 scope | 逐一非零退出、只清理本次资源、诊断可操作 |

**失败分类**：产品失败（断言不等价、归属或清理错误、真实集成/浏览器失败）修最小受影响层并只重验
受影响门禁；验收基础设施失败（Docker daemon、registry、网络、runner 资源）先做最小复现，第一次即
停止该段，不重启整段。

## 5. 预算与实验成本

| 项目 | 内容 |
| --- | --- |
| 范围 | 见 §1；一个交付结果（`scripts/` 全量退役 + 能力原生承接 + §4 全量复验），不摊入其他批次职责 |
| 总预算 | 预计 **1,440 分钟**，累计上限 **1,920 分钟**（长时文件；用户 2026-10-09 指定 23-05 全部完成，授权与计算依据登记在总方案 §6） |
| 段预算 | A 120/180；B 240/300（B1/B2 各 ≤180）；C 420/540（C1 130/180、C2 150/180、C3 110/180、C4 30/120）；D 150/180；E 120/180；F 120/180；G 180/240；H 90/120。各段上限之和即累计上限，段边界强制停点 |
| 实验成本 | 无可缩短的测量窗口，真实运行是主要成本（单元 × 预热/准备 + 测量 + 恢复/清理）：全栈闭包 1 次 ×（准备 10 + 测量 30–50 + 清理 5，含 24 次验收容器运行与 `up --wait-timeout 420`）；业务/可观测专项 6 个 suite ×（准备 2 + 测量 5–15 + 清理 3）；容器栈 3 次 ×（准备 1 + 测量 1–3 + 清理 1）；隔离安装 10（实测 3 m 44 s + 回执核对）；模块测试与 `check-all` 25；§4 五条 180（含全新检出的 `npm ci` 与依赖冷启 125 s）。CI 等待 60–120 分钟不计入主动预算，但收尾前必须取得结果。合计真实墙钟约 250–350 分钟，已含在段预算内 |
| 重试成本 | 同一未解决原因最多 2 次定向诊断、每次 ≤10 分钟；第一次基础设施失败即停止该段并改用最小复现；不以延长超时或放宽断言掩盖问题 |
| 成本构成（预计 1,440） | 断言清单取证与发现 180；实现与单测（`acceptance` 模块、`devtools` 扩展、CI/文档改写）520；构建与环境准备（Go 构建、镜像、`npm ci`、worktree 准备）120；真实验收 180（全栈闭包 40、6 个专项 suite 90、容器栈 15、隔离安装 10、模块测试 25）；§4 五条全量复验 180；删除与收口 90；安全清理与诊断余量 170 |
| 停点 | 累计 50% / 80%（960 / 1,536 分钟）以及每段内 50% / 80% 报告已完成项、剩余门禁、耗时与下一步；开始下一条昂贵命令前核对剩余预算；单个真实 suite 预计耗时 + 安全清理超过剩余预算时不启动 |
| 接续粒度 | 按段（A–H）恢复；段内不承诺更细粒度。失效规则：`acceptance/**`、`devtools/**`、`lifecycle/**`、`monitor/**` Go 源码变化 → 对应段的真实回执失效；`deploy/compose*.yaml`、Dockerfile 变化 → Compose 闭包与镜像入口回执全部失效；`VERSION` 变化 → 镜像 tag、候选与安装回执失效；`quality-gates.yml` / `ci/quality_scope.py` 变化 → 只失效治理与 CI 门禁；`frontend/**` 或 spec 变化 → 浏览器回执失效。新候选不重置累计耗时 |

## 6. 停止、接续与完成

**完成条件**（缺一不可）：

1. `git ls-files scripts` 为空且工作树中不存在 `scripts/` 目录；`dev/**` 之外的 `scripts/` 活引用为 0；
2. §4.3 段门禁 A1–H5 全部通过；未开始、待核对或未通过的项一律计为未完成，不得计入完成；
3. §4.4 的 C1–C5 在全新检出、冻结候选上全部通过并留原始回执；
4. 每个被删除文件都有承接目标或 §1.5 的退役依据，并已写入 `capability-status.md`（含能力边界与
   Python 保留例外）；
5. `VERSION` / `.env.example` / 4 个前端包文件 / `deploy/runtime-contracts.json` 同步为 `2.5.5`，
   `validate_versions.py`（迁移后 `ci/`）通过；
6. 同名实施日志已写入且只记录实际完成项、实际改动文件、真实命令与结果、偏差与后续项；`VERSION`
   在完成提交内更新；
7. 分支 CI 全绿，且模块、integration ×2、e2e ×2、`compose-full-stack`、acceptance 与 governance job
   均实际执行（非被跳过）。

**偏差处理**：

- 某组断言无法等价承接：**该文件不删除**，登记差异、最小复现与影响范围，在本段边界停止并交回用户
  决定（Go 化补做、退役该能力或拆分批次）；不以"放宽比对字段""重算摘要"作为修复。
- 真实环境不具备某一能力（例如本机无 arm64、无 QEMU）：如实登记为 CI-only，并在段 G/H 的 CI 结果中
  取得证据；不以本机替代结果声称已覆盖。
- 基础设施失败：按 §4.3 失败分类做最小复现，最多两次定向诊断；未取得已证明原因前不重启该段。
- 触顶（累计 1,920 分钟或某段上限）：在该段边界停止新工作，保留候选身份（revision、`VERSION`、镜像
  tag 与摘要）、通过/失败/未开始清单、已删除文件清单与清理结果，登记接续清单；不为未完成的批次创建
  完成提交。续做需修订的有界方案与用户明确指示；已删除文件与已通过门禁不回滚、不重做。

**接续粒度**：按段 A–H 恢复，段内不提供更细粒度恢复（不承诺不存在的 `--resume`）。段间依赖：
B 依赖 A 的 CI 面基线；C 依赖 B 的编排核心（同模块共享 harness）；D 依赖 B/C 的 harness；G 依赖
A–F 全部完成；H 依赖 G 通过。

## 7. 与其他文件的关系

- 承总方案 §4：五条判据在段 G 全量复验通过后才执行段 H 的删除；本方案同步修订总方案 §2/§5.1/§5.4/§6。
- 承 23-02 §6 与 23-03 §6 的接续项：`compose_build_cache.py` 原生承接（段 A）、`package-redis-exporter.sh`
  与 `monitor_input_digest`（段 C）、Python 退役汇总（段 H，`capability-status.md`）。
- 承 23-04 §6 的 7 个接续项：交付库退役（段 E2）、`verify-compose*.sh` 与生命周期验收承接（段 B/D）、
  arm64 措辞统一（段 D 回执）、`rabbish/PLAN-closeout-2026-10-09.md` 重新取证（本方案 §1.5 已从工作树
  重建 13 文件 / 1,063 行清单，并核对与总方案 §5.4 一致；该文件实际位于仓库外 scratch 路径
  `/home/ray/rabbish/PLAN-closeout-2026-10-09.md`，不是仓库路径，不作为权威依据）。
- 与 `ci/AGENTS.md`（迁移后的测试与验收工具规则）保持一致：新增执行器必须说明必要性；本方案不新增
  第二套 runner，只把既有断言迁入唯一新模块。
- 不修改 `dev/design/` 两份冻结设计与落地大纲；不修改 `deploy/phase16-acceptance.yaml`；不重建已退役
  的 Phase 矩阵；不重写历史日志与原始证据。

## 8. 决策记录（2026-10-09）

| 决策 | 结论与理由 |
| --- | --- |
| 批次范围 | 用户明确要求 **23-05 全部完成**：不做"先收口、后续批次再迁"的拆分，也不退役仍在使用的验收能力来换速度。本方案据此把 97 文件全部纳入删除台账 |
| 预算登记 | 单批长时文件：预计 1,440 / 累计上限 1,920 分钟，段边界强制停点。依据：CI 仍依赖的 `verify-compose*.sh`（1,242 行）与业务/可观测专项（G5+G6+G7 共 8,301 行）必须原生承接；实测吞吐（23-03 在 225 分钟内完成约 3,000 行 Go 与全部门禁；23-04 在 95 分钟内完成约 2,500 行 Go 与真实安装）给出 1,200–1,500 行/小时的参考 |
| 承接架构 | 新增 `acceptance` 模块承载验收编排与断言；不把验收逻辑塞进 `devtools`（其职责是本机开发环境），也不新增多个执行器（`ci/AGENTS.md` §3） |
| 治理工具处置 | 迁移到 `ci/` 并保留 Python：总方案 §3.3 明确其不属于本次清理对象；Go 重写登记为可选子段 F1b（240/300），需用户明确指示才执行 |
| 历史矩阵退役 | Phase 16/17（13 文件 / 1,063 行）依据 `capability-status.md:54` 的既有用户决定退役；Phase 13/14/15 历史闭包路径与 Phase 17/21 运行时矩阵按 §1.5 新增登记退役。三项退役在本方案获批时一并生效 |
| PowerShell 退役 | 零活引用；不再声明 Windows 原生入口，能力边界写入 `capability-status.md` |
| 哈希与字节一致性 | Go 与 Python 在 JSON 键顺序/转义上的差异不作为门禁；等价性以同一制品上的语义与断言语义为准（沿用 23-04 偏差 3 的处理） |
| 可拆分为多批 | 若执行中发现单批过长，段边界即批次边界：`A+B`（2.5.5）、`C`（2.5.6）、`D+E+F`（2.5.7）、`G+H`（2.5.8）；拆分只需改总方案 §2/§5.1，不需改写本文件 |
