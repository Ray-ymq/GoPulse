# Phase-23-03：本机开发、集成与浏览器环境承接

> 状态：初稿 2026-10-09（含同日合并修订）。目标版本 `2.5.3`，分支 `develop/2.5.3`。
> 进入条件：Phase-23-02 完成并进入主远端 main（`VERSION=2.5.2`，实测 commit `49f5c3e`），且本方案
> 与总方案 2026-10-09 合并修订已进入主远端 main。
> 用户决定（2026-10-09）：原 23-04（`integration`）与原 23-05（`e2e`）并入本批，单批单文件；三段
> 合计超出普通实施文件 180 分钟上限，按长时文件登记（预计 390、累计上限 390，分段 A/B/C 各
> 150/130/110，段边界为强制停点）。总方案 §2、§3、§5.1、§6 已同步。
> 实现从主远端 main 创建已分配分支并使用独立工作树；不沿用完成分支，不把实现放在 `update`。

## 1. 交付与允许文件

**唯一主要交付结果**：`make deps` / `make dev` / `make dev-observe` / `make stop` / `make integration` /
`make e2e` 六项目标由同一个原生 Go 助手承接，`scripts/ci/local_development.py`（1,156 行）及其自测
（179 行）退役；`make monitor-image` 目标取消，镜像准备并入需要它的入口。行为与现有 Python 实现
等价，等价性有先取证后替换的回执。

职责边界不变（承总方案 §1）：Makefile 只负责目标、依赖与调度；Go / Vitest / Playwright 负责断言；
Compose 负责依赖服务、健康检查、网络与卷；助手只负责进程管理、测试隔离与失败清理。

### 1.1 允许新增

| 文件 | 内容 |
| --- | --- |
| `devtools/go.mod`、`devtools/go.sum` | 新模块 `github.com/Ray-ymq/GoPulse/devtools`；只用标准库，不引入第三方依赖 |
| `devtools/Makefile` | 组件级 `test` / `race` / `check` / `build`（23-01 契约）；产物 `devtools/bin/devenv` |
| `devtools/cmd/devenv/main.go` | CLI：`deps` / `dev` / `dev-observe` / `stop` / `integration` / `e2e`；参数校验、退出码、信号与回滚入口 |
| `devtools/internal/**` | 环境合成、workspace 与状态、Compose 编排、进程归属、就绪探测、内容摘要、开发/集成/浏览器生命周期实现 |
| `devtools/internal/**/*_test.go`、`devtools/internal/**/testdata/**` | 单元用例与夹具（临时目录、假命令、夹具 env 文件） |
| `devtools/bin/`（构建产物） | 由 `devtools/Makefile` 生成；`.gitignore` 同时增加 `devtools/bin/`。运行期状态仍写已忽略的 `.run/local/<workspace-id>/`，不新增签入路径 |

### 1.2 允许修改

| 文件 | 改动 |
| --- | --- |
| `Makefile`（根） | 新增 `deps`；`dev` / `dev-observe` / `stop` / `integration` / `e2e` 改调 `devtools` 组件 Makefile 与助手二进制；删除 `monitor-image`、`LOCAL_DEVELOPMENT`、`PYTHON` 与 Python 转发；`GO_MODULES` 增加 `devtools`；`help` 同步。recipe 保持短小，不放长篇 Shell |
| `.github/workflows/quality-gates.yml` | 新增 `devtools` job（`make check` / `make test` / `make race MODULE=devtools`，形状同 `lifecycle`/`loadtest` job）；删除 `integration-observe`（`:513-514`）与 `e2e-observe`（`:565-566`）的 `make monitor-image` 步骤。其余 job 结构、`if` 条件与命令不动 |
| `scripts/ci/quality_scope.py` | `MODULES` / `GO_MODULE_ROOTS` 增加 `devtools`；`devtools/**` 选择 `devtools` + `integration_business` + `integration_observe` + `e2e_business` + `e2e_observe`（替换已失效的 `scripts/ci/local_development` 规则，见 `:194-211`） |
| `scripts/ci/test_quality_scope.py` | 增加 `devtools/**` 选择用例；删除 `scripts/ci/local_development` 路径用例 |
| `.gitignore` | 增加 `devtools/bin/`（与既有 `backend/bin/` 同形） |
| `README.md` | 日常入口加 `make deps`；删除 `make monitor-image` 段落，改为说明 observe 入口按需准备/复用镜像 |
| `dev/validation/local-development-tests.md` | 入口映射改为原生助手；登记 Python 实现退役后被 Go 用例与真实门禁承接的检查 |
| `dev/imple/Phase-23/Phase-23-03-本机环境编排承接.md`、`dev/logs/Phase-23/Phase-23-03-本机环境编排承接.md` | 本批实测记录与同名日志（`validate_branch.py --mode completion` 要求同名日志） |
| `VERSION`、`.env.example`、`frontend/package{,-lock}.json`、`admin-frontend/package{,-lock}.json` | 同步到 `2.5.3`（`sync_version_metadata.py`） |

### 1.3 允许删除（先取证、后删除，三段末端执行）

| 文件 | 行数 | 删除条件 |
| --- | --- | --- |
| `scripts/ci/local_development.py` | 1,156 | 六项子命令全部由 `devtools` 承接，A/B/C 段等价性回执与真实门禁通过后整体删除 |
| `scripts/ci/test_local_development.py` | 179 | 其保护的行为按 §4 映射迁移到 `devtools` Go 用例，且 `python3 -m unittest discover -s scripts/ci -p 'test_*.py'` 全绿后删除 |

删除前必须核对引用面（`scripts/AGENTS.md` §4）：`Makefile`、`quality_scope.py:196`、
`test_local_development.py` 的导入、`dev/validation/local-development-tests.md`、`README.md`。
历史方案与 `dev/logs/**` 中的引用不改写。

### 1.4 不属于本批

`make package` 的 Bundle/manifest/promote 与 `lifecycle` 隔离安装（23-04）；`scripts/` 其余内容
（`dev.sh`、`down.sh`、`verify-*.sh`、`release_artifacts.py`、`compose_build_cache.py` 等）的承接与
删除（23-05）；业务与观测产品代码及其断言；`deploy/compose.yaml` 的正式部署合同。

### 1.5 不改变

`VERSION` 单一来源与发布模型；业务 API、权限、消息与正式证据合同；目录结构（不迁移到
`pkg/<组件>/`）；`deploy/compose.yaml`、`deploy/compose.local.yaml`、`deploy/compose.local-linux.yaml`
与 `deploy/runtime-contracts.json`（本批只引用，如必须修改须在 §6 偏差中登记并说明理由）；
冻结的两篇设计、落地大纲与 `dev/design/README.md` 注册基线；已退役的 Phase 矩阵不重建；
历史证据不改写。

### 1.6 实测事实与硬约束（开工前已核实）

| 事实 | 证据 |
| --- | --- |
| 六项目标现状 | `Makefile:4` 的 `LOCAL_DEVELOPMENT` 把 `dev` / `dev-observe` / `integration` / `e2e` / `stop` / `monitor-image` 全部转发 `scripts/ci/local_development.py`（1,156 行）；`make deps` 在全仓库不存在（`build_parser()` 只有 6 个子命令，`local_development.py:1116-1138`） |
| CI 调用面 | 没有任何 workflow 调用 `make dev` / `dev-observe` / `stop` / `deps` 或直接调用 `local_development.py`；`make monitor-image` 被 `quality-gates.yml:514`（`integration-observe`）与 `:566`（`e2e-observe`）调用；`make integration` / `make e2e` 各两个 job |
| 归属与清理合同 | 进程归属用 `/proc/<pid>/stat` 的 starttime（第 22 字段）；`compose down --remove-orphans`（**无** `--volumes`）；`stop` 无状态时退出 0；`check_ports` 拒绝未归属占用且不杀占用者 |
| 隔离身份 | 开发项目 `gopulse-<sha256(realpath)[:12]>-dev`；测试项目 `gopulse-<id>-test-<VERSION 去点>`；私有根 `.run/local/<id>/`（`state.json` / `integration-state.json` / `<mode>.env` 0600 / `bin/` / `logs/`） |
| 测试隔离端口 | 18080 / 15173 / 15174、23306 / 26379 / 25672-3 / 29200-1 / 19092 / 18428 / 19090-1 / 19093 / 19121；组件私有端口 19101 / 19102 / 19103 / 19105 / 19106 对应 `deploy/runtime-contracts.json` |
| 就绪与构建缓存 | HTTP GET、2 秒超时、0.25 秒间隔、无代理、可选 Bearer；`go build -trimpath -o .run/local/<id>/bin/<name> <pkg>` 按摘要复用；Vite 必须带 `--host 127.0.0.1`（Phase-22-04 的 IPv4 修复） |
| Monitor 镜像 | tag `gopulse/monitor:local-<monitor_input_digest[:16]>`；摘要覆盖 `VERSION`、`componentmetrics/`、`monitor/`、`exporters/`、`deploy/docker/observability.Dockerfile`、`scripts/package-redis-exporter.sh`、`deploy/plugins/`；构建命令 `docker build --pull=false --target monitor --file deploy/docker/observability.Dockerfile --build-arg VERSION --build-arg REVISION --tag <tag> .` |
| 治理门禁 | `governance` job 恒跑 `python3 -m unittest discover -s scripts/ci -p 'test_*.py'`；改根 `Makefile` 触发全部 21 项检查（`quality_scope.py` 保守默认，既有行为） |
| 工具链与退出码 | `go1.26.7`、`docker 29.7.2` / `compose v5.5.0`；实测 `go run` 把程序非零退出折叠为 1（`os.Exit(7)` → `go run` 退出 1），因此入口必须 build 后 exec 才能保留退出码与信号语义 |

## 2. 原生助手设计（`devtools`）

### 2.1 调用链

```makefile
# 根 Makefile —— 只做调度，保持短小
GO_MODULES := ... devtools ...            # 模块清单唯一来源，共 16 个
DEVENV := $(MAKE) --no-print-directory -C devtools build && ./devtools/bin/devenv

deps:            ; @$(DEVENV) deps $(if $(SCOPE),--scope $(SCOPE),)
dev:             ; @$(DEVENV) dev
dev-observe:     ; @$(DEVENV) dev-observe
stop:            ; @$(DEVENV) stop
integration:     ; @$(DEVENV) integration --scope "$(if $(SCOPE),$(SCOPE),business)"
e2e:             ; @$(DEVENV) e2e --scope "$(if $(SCOPE),$(SCOPE),business)"
```

`devtools/Makefile` 自行决定编译参数与产物。不用 `go run`：实测它把非零退出折叠为 1，无法保留
退出码，且多一层进程影响信号与归属判定。

### 2.2 CLI 契约

| Make 目标 | 助手子命令 | 行为 |
| --- | --- | --- |
| `make deps [SCOPE=observe]` | `deps [--scope observe]` | 启动/刷新本工作树的开发依赖 Compose 项目并等待健康检查；记录 `stage=deps` 状态；重复执行幂等 |
| `make dev` | `dev` | 依赖（基础集）→ `migrate up` / `search-reindex --if-missing` → Backend、Business Worker、Search Indexer → user Vite；**不构建任何镜像** |
| `make dev-observe` | `dev-observe` | 在 dev 之上加 observe profile 依赖、Router、Marshaller、admin Vite 与 Monitor 容器；Monitor 镜像按内容摘要按需准备或复用 |
| `make stop` | `stop` | 只终止本工作树归属的进程与 Compose 项目；命名卷保留；无归属状态时退出 0 并说明 |
| `make integration [SCOPE=]` | `integration --scope business\|observe` | 独立测试项目/卷/端口 + 文件锁；真实 Go 集成测试；结束后只清理本次创建的资源 |
| `make e2e [SCOPE=]` | `e2e --scope business\|observe` | 测试项目 + 源码进程 + 两个 Vite + Playwright；trace 保留在 `frontend/test-results/` |

退出码：`0` 成功；`2` 参数或用法错误（未知 scope、端口越界、环境文件缺失、`VERSION` 非三段
SemVer、缺少必需路径）；`1` 运行期或依赖/测试失败；`130` 收到 SIGINT（先回滚本次创建的资源）。
子进程失败必须传播为非零，不得吞掉。

### 2.3 包职责与测试接缝

| 包 | 职责 | 最低有效层级用例 |
| --- | --- | --- |
| `internal/envfile` | `.env.example` 默认值 + 显式/`GOPULSE_ENV_FILE`/`.env` 变量 + 调用者变量合成；`dev` / `observe` / `test` 三套模式默认值；私有 env 文件 0600 排序写出 | 无 Docker 的表驱动用例 |
| `internal/workspace` | 路径身份 `sha256(realpath)[:12]`、私有根、状态 JSON（schema 1）保存/加载/归属校验、项目命名 | 临时目录用例 |
| `internal/digest` | 路径+字节摘要；`source` / `e2e_source` / `monitor_input` / `compose` 摘要集合 | 确定性用例 |
| `internal/compose` | 命令构造（`--project-name` / `--env-file` / `--profile` / 多 `--file`）、`up --detach --wait --wait-timeout 420`、`down --remove-orphans`、`exec` | 命令构造用例 + 真实 Compose 门禁 |
| `internal/proc` | 进程出生身份（`/proc/<pid>/stat` starttime）、新会话启动、进程组 SIGTERM→30 秒→SIGKILL、端口归属检查 | 真实子进程用例（假命令） |
| `internal/ready` | HTTP 就绪轮询（2 秒超时、0.25 秒间隔、无代理、可选 Bearer）、进程已退出即失败 | 本地 `httptest` 用例 |
| `internal/devrun` | `deps` / `dev` / `dev-observe` / `stop` / Monitor 镜像准备与复用 | 单元 + A 段真实门禁 |
| `internal/testenv` | `test` 模式隔离、文件锁、测试项目/卷、源码测试进程、账号注册与清理、集成与浏览器命令 | 单元 + B/C 段真实门禁 |

### 2.4 必须逐项保持的等价合同

- 身份与路径：workspace 身份、两个状态文件名、项目名规则、私有 env 文件名与 0600、排序键。
- Compose 调用：参数顺序与取值（`--project-name` → `--env-file` → `--profile observe` →
  `--file deploy/compose.local.yaml` → `--file deploy/compose.local-linux.yaml`），`up` 与 `down`
  的旗标集合，失败时只清理本次创建的项目。
- 端口与归属：开发必需端口 = `HTTP_PORT`、`FRONTEND_PORT`、19101/19102/19103；observe 追加
  `ROUTER_HTTP_PORT`、19105、`MARSHALLER_HTTP_PORT`、19106、`MONITOR_HTTP_PORT`、
  `ADMIN_FRONTEND_PORT`、`REDIS_EXPORTER_HTTP_PORT`；测试端口集合沿用 `integration_ports` /
  `e2e_ports` 的定义。占用者非本次归属时拒绝启动且不杀它。
- 进程与实例：`GOPULSE_INSTANCE_ID` 取值（`backend-local`、`business-worker-local`、
  `search-indexer-local`、`router-local`、`marshaller-local`；测试 `*-test`；浏览器 `*-e2e`）、
  日志路径、启动即记录身份、失败即回滚。
- 就绪判定：Backend `HTTP_PORT/ready`、Worker `19102/ready`、Indexer `19103/ready`、Router 与
  Marshaller 带 Bearer 的 `/ready`、Monitor `/ready`、user Vite `/`、admin Vite `/admin/`。
- 状态与阶段：`status`（running/stopped/failed）、`mode`（dev/observe）、新字段 `stage`
  （`deps` / `dev` / `dev-observe`）、`observe`、`project`、`compose_files`、`env_file`、
  `source_digest`、`environment_digest`、`compose_digest`、`monitor_input_digest`、`monitor_image`、
  进程记录、`started_at` / `stopped_at`；`integration-state.json` 沿用 schema 1。
- 阶段迁移规则（新增能力，只放宽 `deps` 态）：`stage=deps` 可被 `dev` 或 `dev-observe` 就地接续；
  已运行 `dev` 时请求 `dev-observe`（或反向）仍按既有 mode 冲突拒绝并要求 `make stop`；
  输入 digest 变化或进程记录失活时同样拒绝；旧（Python 写入、无 `stage`）状态文件必须可被 `stop`
  处理，并按 `observe` 字段判定其阶段。
- 隔离：测试模式固定使用 `gopulse_integration` 账号/库、Redis DB 15、测试端口段；开发与测试
  项目、卷、端口不重叠；`make integration` / `make e2e` 在开发生命周期运行期间明确拒绝。
- 既有 Go 命令原样转发：业务集成 `go -C backend test -p 1 -tags=integration ./...`；
  观测集成 `go -C backend test -p 1 -tags=integration,observability_integration ./internal/http
  -run ^TestObservabilityFlowIntegration$ -count=1 -timeout 6m`；浏览器命令与 `--grep` /
  `--grep-invert` 过滤、账号注册与清理 SQL 的 `observe_(admin|user|demote)_<hex>` 限定不变。

### 2.5 新增能力的边界（`scripts/AGENTS.md` §3）

本批不新增 runner、sampler 或 verifier：`devtools` 是现有环境编排实现的**一对一替换**，断言仍由
既有 Go 集成测试与 Playwright 用例承担，环境健康仍由既有 Compose healthcheck 承担，证据仍写既有
私有状态文件与 `frontend/test-results/`。新增文件只有助手实现与其模块单元测试；公共部分（Compose
编排、进程归属、就绪探测、摘要）集中在该模块，供三段与 23-04/23-05 复用，不复制执行器。

## 3. 三段执行、取证与退役

三段在同一分支与版本内顺序执行；段边界是强制停点，前一段门禁未通过不得进入下一段。

### 3.1 A 段：依赖与开发环境（预计 150 分钟）

交付：`devtools` 模块与 `make deps` / `dev` / `dev-observe` / `stop`；Monitor 镜像准备与复用并入
`dev-observe`；Python 的 `start_lifecycle`、`check_existing_state`、`stop`、`stop_state`、
`prepare_monitor_image` 与其 argparse 注册删除；`quality_scope.py` 增加 `devtools` 规则与新 CI
job；根 Makefile 增加 `deps`、删除 `monitor-image` 转发。

先取证：在基线提交上用旧实现运行一次 `dev` 并采集回执（私有 env 文件逐行、`state.json` 字段、
项目名、容器端口集合、就绪端点与耗时、非法参数的退出码）。替换后逐项比对，并在旧实现仍运行时
用 `devtools/bin/devenv stop` 停止该运行（证明旧状态文件可被新入口处理）。

### 3.2 B 段：隔离集成环境（预计 130 分钟）

交付：`make integration` 由 `internal/testenv` 承接——测试模式环境合成、文件锁、测试项目/卷、
端口集合、`migrate` / `search-reindex` 一次性准备、观测源码进程（Backend/Router/Marshaller）、
观测测试管理员注册与 `admin-role promote`、两类 Go 集成测试命令、失败状态与 `finally` 清理。

先取证：用旧实现运行 `SCOPE=business` 与 `SCOPE=observe`，记录测试项目名、端口集合、env 键值、
测试命令、状态字段与清理结果；替换后逐项比对，再删除 `run_integration`、
`start_observe_test_processes`、`cleanup_observe_admin` 及仅被它们使用的辅助函数。

### 3.3 C 段：浏览器环境与整体退役（预计 110 分钟）

交付：`make e2e` 由 `internal/testenv` 承接——Vite 启动（保留 `--host 127.0.0.1`）、
`npm ci` 按需执行、业务与观测浏览器命令与过滤、账号注册/提升/清理、trace 目录与端口覆盖参数；
然后删除 `make monitor-image` 目标与 `quality-gates.yml` 两处步骤；删除 `local_development.py`
与 `test_local_development.py`；更新 `README.md` 与 `dev/validation/local-development-tests.md`。

先取证：用旧实现运行 `SCOPE=business` 与 `SCOPE=observe`，记录浏览器命令、过滤表达式、环境
变量、账号清理结果与 trace 路径；替换后逐项比对，再执行整体删除。
`observe` 的真实回执必须覆盖 Monitor 容器由内容摘要 tag 提供、且输入未变时不重建。

## 4. 验证映射与固定门禁

| 字段 | 内容 |
| --- | --- |
| 验证对象 | (a) 六项入口是否真实执行既有命令并保持归属、隔离、保卷与失败传播；(b) 助手与旧实现是否等价；(c) 退役后是否仍有活引用；(d) CI 与治理门禁是否仍覆盖被改动的行为 |
| 已有覆盖 | `scripts/ci/test_local_development.py` 的 15 条用例（本批迁移，见 §4.1）；`backend/internal/integrationtest/environment.go` 的测试目标校验；既有 Go 集成测试与 Playwright 用例；`deploy/compose.local*.yaml` 的 healthcheck；`deploy/runtime-contracts.json` 的私有端口合同；`quality_scope.py` 与 `test_quality_scope.py`；`validate_versions.py` / `validate_branch.py` |
| 最低有效层级 | 环境合成、身份、摘要、参数校验、命令构造、进程归属与端口拒绝用 Go 单元层（无 Docker，可复现注入）；Compose 健康、真实进程启停、测试隔离、锁、保卷、真实集成与真实浏览器只在真实系统层验证。静态检查不替代真实门禁 |
| 本批变更 | 见 §1；复用既有 `go test`、Playwright、Compose healthcheck、模块 Makefile 契约与治理脚本，不新增执行器、不新增证据 schema |
| 固定门禁 | 下表 A1–A10、B1–B5、C1–C6、D1–D4；命令、期望结果、证据与失败分类同时登记 |

### 4.1 既有自测的迁移映射（删除 Python 用例前必须逐条落地）

| Python 用例 | 迁移到 |
| --- | --- |
| `test_dotenv_merge_keeps_defaults_and_caller_override_without_expansion` | `internal/envfile`：默认值、显式文件、调用者变量优先级、不做变量展开 |
| `test_test_environment_isolation_is_strict` | `internal/envfile`：test 模式端口/账号/DB 15 与开发身份分离 |
| `test_test_environment_rejects_development_dependency_identity` | `internal/envfile`：拒绝开发依赖身份 |
| `test_integration_scope_rejects_unknown_before_docker` | `cmd/devenv`：未知 scope 在触达 Docker 前失败 |
| `test_e2e_scope_rejects_unknown_before_docker` | 同上 |
| `test_e2e_ports_include_source_and_browser_ports` | `internal/testenv`：端口集合含 19101/19102/19103 与 observe 的 15174 |
| `test_e2e_port_argument_rejects_out_of_range_values` | `cmd/devenv`：端口参数越界拒绝 |
| `test_integration_lock_rejects_second_owner` | `internal/testenv`：真实文件锁第二个持有者失败 |
| `test_source_digest_includes_local_replace_module` | `internal/digest`：backend + componentmetrics；observe 追加 router/marshaller |
| `test_digest_changes_for_path_and_content` | `internal/digest`：路径或字节变化即摘要变化 |
| `test_process_birth_identity_prevents_pid_reuse_cleanup` | `internal/proc`：出生身份不匹配即不终止 |
| `test_process_group_stop_is_bounded_and_owned` | `internal/proc`：进程组 SIGTERM→上限→SIGKILL；非归属不动 |
| `test_port_conflict_fails_without_touching_existing_socket` | `internal/proc`：端口占用者不被杀 |
| `test_workspace_identity_is_path_scoped` | `internal/workspace`：不同路径不同身份、同一路径稳定 |
| `test_compose_environment_rejects_unknown_module` | 不再迁移：模块名校验已归 23-01 的根 Makefile |

### 4.2 A 段门禁

| 编号 | 判据 | 命令 / 方式 | 期望结果与证据 |
| --- | --- | --- | --- |
| A1 | 入口与帮助正确 | `make help`；`make -n deps dev dev-observe stop`；`grep -c local_development Makefile` | `help` 列出 `deps`、不再列 `monitor-image`；四个 dry-run 均指向 `devtools/bin/devenv`；Makefile 中 0 处 `local_development`；每个目标 recipe ≤3 行 |
| A2 | 依赖启动、健康与幂等 | 干净工作树连续两次 `make deps` | 两次退出 0；`docker compose -p gopulse-<id>-dev ps` 四个基础服务 `healthy`；第二次容器 ID 不变 |
| A3 | 开发环境就绪且不建镜像 | `make dev`；`curl -sf` 8080/ready、19102/ready、19103/ready、5173/；`docker images` 前后对比 | 全部 2xx；第二次 `make dev` 报"已在运行"且进程未重启；不新增 gopulse 业务镜像 |
| A4 | 观测环境与镜像复用 | `make stop` 后 `make dev-observe`；`docker image inspect gopulse/monitor:local-*`；随后在运行态执行 `make dev` | Router/Marshaller/Monitor/user Vite/admin Vite 就绪；输入未变时复用同一 tag 不重建；已运行 `dev` 时请求 `dev-observe`（或反向）按既有合同拒绝并提示 `make stop`（与旧实现的 mode 冲突行为一致） |
| A5 | 停止、归属与保卷 | `make stop`；`docker ps -a` / `network ls` / `volume ls`；按 `state.json` 记录的 PID 与端口 8080/5173/5174 可重新绑定核验 | 本次创建进程（含 Vite）全部退出；项目容器与网络不存在；命名卷保留；无状态时退出 0；并存的一个 unowned 项目未被触碰 |
| A6 | 与旧实现等价（先取证后替换） | 基线旧实现回执 vs 新助手回执；旧运行以新 `stop` 收尾 | 项目名、env 键值、状态关键字段、端口集合、实例身份一致；非法参数退出码一致；旧状态文件可被新入口停止 |
| A7 | 失败注入非零退出与清理 | (a) 未归属进程占用 8080；(b) `MYSQL_PORT` 指向关闭端口后 `make deps`；(c) `VERSION=2.5`；(d) 非法 `HTTP_PORT` | 四种注入均非零退出并给出可操作诊断；不杀占用者；只清理本次创建的容器/网络；卷保留 |
| A8 | 单元与静态检查 | `make test MODULE=devtools`、`make race MODULE=devtools`、`make check MODULE=devtools` | 全过；§4.1 映射的 14 条行为均有对应用例 |
| A9 | 全新检出可用性 | 新建 `git worktree`（无 `.run/`、无 `node_modules`）：`make deps` → `make dev` → `make stop` | 三步成功；`npm ci` 由助手按需执行；结束后无残留项目资源（总方案 §4 判据 1 的本批部分） |
| A10 | 治理门禁 | `validate_versions.py`；`validate_branch.py --mode development`；`quality_scope.py --changed-file devtools/cmd/devenv/main.go`；`python3 -m unittest discover -s scripts/ci -p 'test_*.py'`；改动过的 YAML 解析 | 全过；`devtools/**` 选中 `devtools` + 四个矩阵检查；已无 `scripts/ci/local_development` 死规则 |

### 4.3 B 段门禁

| 编号 | 判据 | 命令 / 方式 | 期望结果与证据 |
| --- | --- | --- | --- |
| B1 | 业务集成真实通过 | `make integration SCOPE=business` | 退出 0；真实 MySQL/Redis/RabbitMQ/ES 上集成测试通过；`integration-state.json` 为 `status=passed`、`mode=integration`、项目 `gopulse-<id>-test-<ver>`；记录耗时 |
| B2 | 观测集成真实通过 | `make integration SCOPE=observe` | 退出 0；`TestObservabilityFlowIntegration` 通过；Monitor 由内容摘要 tag 提供且按需准备 |
| B3 | 隔离、锁与拒绝 | 并发第二个 `make integration`；开发运行态下执行 `make integration`；`make integration SCOPE=bogus` | 第二个在锁处立即非零退出；开发运行态被拒绝并提示 `make stop`；未知 scope 非零且不启动 Docker |
| B4 | 失败传播与清理 | (a) 未归属进程占用 23306；(b) 单元用例：子命令非零 → 助手非零、`status=failed`、执行清理 | (a) 非零退出、不杀占用者、无残留容器/网络；(b) 用例证明失败传播与清理路径 |
| B5 | 删除与回归 | `local_development.py` 的集成实现已删除；`python3 -m unittest discover -s scripts/ci -p 'test_*.py'`；`make test MODULE=backend` | Python 套件全绿（用例数变化逐项解释）；backend 模块测试通过 |

### 4.4 C 段门禁

| 编号 | 判据 | 命令 / 方式 | 期望结果与证据 |
| --- | --- | --- | --- |
| C1 | 业务浏览器真实通过 | `make e2e SCOPE=business` | 退出 0；`business.spec.ts` + `compose-business.spec.ts`（`--grep-invert search-rebuild\|search-live`）；trace 生成于 `frontend/test-results/` |
| C2 | 观测浏览器真实通过 | `make e2e SCOPE=observe` | 退出 0；admin 三条固定用例与 `compose-observability.spec.ts` 的 admin 场景通过；`observe_{admin,user,demote}_<id>` 账号在 MySQL 中已清理，清理 SQL 仍限定该命名模式 |
| C3 | 参数与 scope 校验 | 直接调用 `devtools/bin/devenv e2e --scope bogus`、`--frontend-port 0`、`--http-port 70000` | 均以非零退出并给出诊断；越界值在触达 Docker 前失败（Make 入口参数面与现状一致，不新增参数） |
| C4 | 浏览器失败清理 | 未归属进程占用 15173 后 `make e2e SCOPE=business` | 非零退出；只清理本次创建资源；trace 保留；其他项目不受影响 |
| C5 | `monitor-image` 取消与 CI 同步 | `grep -rn monitor-image Makefile .github/workflows README.md`；`make monitor-image`；连续两次 observe 运行 | 目标、两个 CI 步骤与 README 段落已删除且无死引用；`make monitor-image` 报无此目标；第二次 observe 运行不重建镜像 |
| C6 | 旧实现整体退役 | 两个 Python 文件不存在；`grep -rn local_development`（排除历史方案与日志）；`python3 -m unittest discover -s scripts/ci -p 'test_*.py'` | 仓库内 0 处活引用；Python 套件全绿；剩余命中只在 `dev/imple/**`、`dev/logs/**` 历史文件 |

### 4.5 D 段门禁（交付）

| 编号 | 判据 | 命令 / 方式 | 期望结果与证据 |
| --- | --- | --- | --- |
| D1 | 版本元数据一致 | `VERSION`、`.env.example`、4 个前端包文件；`python3 scripts/ci/validate_versions.py` | 均为 `2.5.3` 且校验通过 |
| D2 | 文档与入口一致 | `README.md`、`dev/validation/local-development-tests.md`、`make help` | 文档命令可执行、与 `help` 一致；不再出现 `make monitor-image` |
| D3 | 分支 CI 绿 | 推送后 `quality-gates.yml` 的 governance、devtools、integration ×2、e2e ×2、scripts-and-compose job | 全绿；四个矩阵 job **实际执行**而非被跳过；root Makefile 改动触发的全量选择为既有行为，按实际结果记录 |
| D4 | 日志与完成提交 | `dev/logs/Phase-23/Phase-23-03-本机环境编排承接.md`；`validate_branch.py --mode completion` | 日志只记录实际完成项、实际改动文件、真实命令与结果、偏差与后续项；完成提交包含 `VERSION=2.5.3` |

**失败分类**：产品失败（等价性不一致、归属或清理错误、真实集成/浏览器失败、治理门禁不通过）
修最小受影响层并只重验受影响门禁；基础设施失败（Docker daemon、registry、网络、npm 安装、
GHA runner）先做最小复现，第一次即停止该段，不重启整段。

## 5. 预算与实验成本

| 项目 | 内容 |
| --- | --- |
| 范围 | 见 §1；一个交付结果（六项入口 + 助手 + 旧实现退役），不摊入 `make package` 与 `scripts/` 其余删除 |
| 总预算 | 预计 390 分钟，**累计上限 390 分钟**（长时文件，用户 2026-10-09 授权；总方案 §6 已登记） |
| 段预算 | A 150：实现 70 / 直接检查 20 / 构建启动 30 / 真实验收 20 / 证据清理 10。B 130：实现 55 / 直接检查 15 / 构建启动 20 / 真实验收 30 / 证据清理 10。C 110：实现 45 / 直接检查 15 / 构建启动 15 / 真实验收 25 / 证据清理 10。各段上限 180，段边界强制停点；各段之和即总预算 |
| 实验成本 | 无长时测量窗口。真实单元与预计墙钟：依赖首次建卷 2–4 分钟；`dev` 启动 2 次 + 停止 2 次（每次就绪 ≤180 秒、停止 ≤30 秒、依赖 `--wait` ≤420 秒）；`dev-observe` 1 次（复用则不构建，冷构建 4–8 分钟）；全新检出 1 次（含 `npm ci` 1–3 分钟）；业务集成 1 次 5–12 分钟；观测集成 1 次 6–12 分钟；业务浏览器 1 次 5–8 分钟；观测浏览器 1 次 6–10 分钟；失败注入 6 次各 ≤2 分钟。合计约 45–75 分钟墙钟，已含在段预算内 |
| 重试成本 | 同一未解决原因最多 2 次定向诊断、每次 ≤10 分钟；第一次基础设施失败即停止该段并改用最小复现（单容器、单模块、单夹具）；不以延长超时掩盖问题 |
| 停点 | 累计 50% / 80%（195 / 312 分钟）以及每段内 50% / 80% 报告已完成项、剩余门禁、耗时与下一步；开始下一条昂贵命令前核对剩余预算 |

## 6. 停止、接续与完成

**完成条件**（缺一不可）：

1. A/B/C 三段的 A1–A10、B1–B5、C1–C6 全部通过，或未通过项被明确登记为"未开始/待核对"且不计入完成；
2. 六项入口在全新检出可用；`make stop` 只停本工作区资源且保留命名卷；失败注入均非零退出并只清理本次资源；
3. `scripts/ci/local_development.py` 与 `scripts/ci/test_local_development.py` 已删除，仓库内无活引用，
   `python3 -m unittest discover -s scripts/ci -p 'test_*.py'` 全绿；
4. `make monitor-image` 目标与两个 CI 步骤已删除，observe 入口自行准备并复用内容摘要镜像；
5. `VERSION` / `.env.example` / 4 个前端包文件同步为 `2.5.3`，`validate_versions.py` 通过；
6. 同名实施日志已写入且只记录实际完成项；`VERSION` 在完成提交内更新；
7. 分支 CI 绿，且 governance、devtools、四个矩阵 job、scripts-and-compose 实际执行（D3/D4）。

**偏差处理**：

- 等价性无法证明（某字段/退出码/端口集合不一致）：保留旧实现，登记差异、最小复现与影响范围，
  本批不声明完成；不以"放宽比对字段"或"重算摘要"作为修复。
- 某能力在段预算内无法等价承接：**该能力对应的旧实现不删除**，登记保留项、调用者与后续建议，
  交由 23-05 处理；删除不是本批的目标，承接才是。
- 基础设施失败：按 §4 失败分类做最小复现，最多两次定向诊断；未取得已证明原因前不重启该段。
- 触顶（累计 390 分钟或某段上限 180 分钟）：在该段边界停止新工作，保留候选身份（revision、
  `VERSION`、镜像 tag 与摘要）、通过/失败/未开始清单与清理结果，登记接续清单；不为未完成的批次
  创建完成提交，续做需修订的有界方案与用户明确指示。

**接续粒度与失效规则**：真实回执按段恢复（A / B / C 各自独立，段内不提供更细粒度恢复，也不
承诺不存在的 `--resume`）。使其失效的改动：`devtools/**` Go 源码变化 → 对应段真实回执失效；
`deploy/compose.local*.yaml` 变化 → 三段全部失效；`VERSION` 变化 → 镜像 tag 与 Monitor 复用失效；
`quality-gates.yml` / `quality_scope.py` 变化 → 只失效治理门禁与 D3；前端源码或 Playwright 用例变化
→ C 段真实浏览器回执失效。新候选不重置累计耗时。

**接续项（登记给后续批次，不属本批）**：

1. `monitor_input_digest` 覆盖 `scripts/package-redis-exporter.sh`；该薄转发入口在 23-05 删除时，
   摘要输入集合必须同步移除该路径，否则摘要恒变、Monitor 每次重建 → 23-05。
2. `compose_build_cache.py`（108 行）的原生承接与 `quality-gates.yml` / `cache-warm.yml` 镜像步骤
   切换（23-02 §6 已登记，随重编号转入）→ 23-05，由 23-05 分方案重新登记预算。
3. 根 `Makefile` 改动触发 `quality_scope.py` 的保守全量选择（21 项检查）为既有行为，登记为已知
   成本而非缺陷。
4. `deploy/compose.local.yaml:175` 的 `networks: local:` 未被任何服务引用（渲染结果只有 `default`）；
   本批不改，登记为可清理项，需单独批次或授权。
5. Python 退役（1,335 行）的 `dev/status/capability-status.md` 汇总归 23-05（总方案 §5.2）。

## 7. 与其他文件的关系

- 复用 23-01 建立的组件 Makefile 调度与模块清单单一来源（本批把 `devtools` 加入同一清单）；
  复用 23-02 的 `make build` 语义与 `componentmetrics` 已发布依赖，不重复实现构建入口。
- 总方案合并修订（2026-10-09）把原 23-04、23-05 并入本批：原 23-06 → 新 23-04（`2.5.4`），
  原 23-07 → 新 23-05（`2.5.5`）。23-01、23-02 已完成方案中的 `23-06` / `23-07` 引用保留原文，
  按该映射阅读；不改写历史方案、日志与回执。
- 23-02 登记的 `make monitor-image` 移交（§2、§6）在本批完成：两个 CI 调用者随合并进入本批范围，
  因此承接与删除都在本批内，不再保留到后续批次。
- 不修改 `dev/design/` 两份冻结设计与落地大纲；不重建已退役的 Phase 矩阵；不重写历史证据。

## 8. 决策记录（2026-10-09）

| 决策 | 结论与理由 |
| --- | --- |
| 批次合并 | 原 23-04、23-05 并入本批，单批单文件（`validate_branch.py:100-104` 每批只允许一份分方案）；长时文件登记 390/390，A/B/C 段边界为强制停点。理由：三段共用同一份 workspace / 进程归属 / Compose / 端口隔离契约，分段独立会两次重写同一实现并产生两份背离的隔离契约 |
| 助手位置 | 新建独立模块 `devtools`（用户选择）：领域清晰、与产品模块隔离、可用 `go test` 覆盖；代价是模块清单 15→16、`quality_scope.py` 与 `quality-gates.yml` 各增一处，已同步总方案 §3.2 |
| 入口形式 | Make → `devtools` 组件 Makefile 构建 → exec `devtools/bin/devenv`。不用 `go run`（实测把非零退出折叠为 1，且多一层进程影响信号与归属） |
| 镜像复用判据 | 保留 `monitor-image.json` marker，同时增加"内容摘要 tag 已存在即复用"：marker 位于 workspace 私有根，新建工作树会强制重建同一个 tag；tag 由输入内容决定，复用不改变制品身份。C5 证明输入未变时连续两次运行不重建 |
| `make deps` 状态 | 状态 JSON 增加 `stage`（`deps` / `dev` / `dev-observe`），使 `make deps` → `make dev` 可就地接续、`make stop` 能停止仅依赖态；旧（Python 写入）状态文件必须仍可被 `stop` 处理（A6 覆盖） |
| Python 退役时机 | 不等 23-05：三段承接完成即整体删除 1,335 行 Python 与其实测用例，避免双份隔离契约长期并存；`capability-status.md` 的阶段级汇总仍归 23-05 |
| 不新增执行器 | `devtools` 是既有编排实现的一对一替换：断言仍在既有 Go 集成测试与 Playwright，环境健康仍由 Compose healthcheck 承担，证据仍写既有私有状态与 trace 路径 |
