# Phase 23 总实施方案：原生入口替代 `scripts/`

> 状态：初稿 2026-10-09。当前完成版本 `2.4.5`。**待用户批准后进入主远端 main。**
> 方向已获用户确认：Makefile 统一开发、测试、构建和交付入口，本地与 CI 调用相同目标。

## 1. 阶段结果

把本机开发、测试、构建和交付收敛为一组 Make 目标，本地与 CI 调用同一份命令；随后删除
`scripts/`（101 文件）。

职责四分，互不越界：

- **Makefile**：目标、依赖关系、参数、命令调用。保持短小，recipe 不放长篇 Shell。
- **Go / Vitest / Playwright**：测试用例与断言。
- **Compose**：依赖服务、健康检查、网络和卷。
- **小型助手**：确有需要的进程管理、测试隔离、失败清理。必须可测试，优先 Go 实现。

目标状态：`make <target>` 是唯一入口，`scripts/` 退出。复杂状态管理不在 recipe 里，而在
职责明确、可被 `go test` 覆盖的实现里。

不改变：正式 business/platform 分离部署、业务 API、权限、消息与正式证据合同；**`VERSION`
单一来源与发布模型不变**（本阶段不做组件独立版本发布）；**目录结构不变**（不迁移到
`pkg/<组件>/`，现有顶层目录已等价）；架构依据仍为冻结的两篇设计及落地大纲（这三个文件不改）；
已正式退役的 Phase 矩阵不重建；不重写历史证据。

**已实测的三处硬依赖**（删除前必须已承接）：

| 依赖 | 证据 |
|---|---|
| 镜像构建 | `deploy/docker/observability.Dockerfile:98` COPY 并 RUN `scripts/package-redis-exporter.sh` 15+ 次；`acceptance.Dockerfile:18` 同 |
| 交付链 | `.github/workflows/release-candidate.yml:32` → `release_artifacts.py build` → `verify-release-artifacts.sh` → `promote` |
| 开发命令 | `Makefile:4` 8 个 target 全部转发 `scripts/ci/local_development.py`（1,187 行） |

**关键约束**：`local_development.py` 同时是 CI 入口——`quality-gates.yml` 的 `:510`、`:527`、
`:552`、`:579` 分别以 `make integration` / `make e2e` 调用它。因此只能**先承接、后删除**，
不能先删后补。

## 2. 正式分配与进入顺序

本总方案正式分配 `2.5.x`。patch `0` 是阶段规划基线，不发布 `2.5.0`。完成版本在每批成功
结束前保持上一批完成值。

顺序遵循用户确认的次序：**先接单测、检查和构建，再接开发环境、集成测试与交付。**

| 批次 | 目标版本 | 开发分支 | 主要交付 | 进入条件 |
| --- | --- | --- | --- | --- |
| Phase-23-01 | `2.5.1` | `develop/2.5.1` | Makefile 统一入口骨架：`make test`（完整模块集合）、`make check`；删除纯转发层 | 本总方案及分方案已进入主远端 main |
| Phase-23-02 | `2.5.2` | `develop/2.5.2` | `make build`：程序、前端、镜像构建单一入口；`componentmetrics` 发版并切换 9 个模块依赖 | 01 完成并进入 main |
| Phase-23-03 | `2.5.3` | `develop/2.5.3` | `make deps` / `make dev` / `make dev-observe` / `make stop`：环境编排承接 | 02 完成并进入 main |
| Phase-23-04 | `2.5.4` | `develop/2.5.4` | `make integration`：Go 集成测试 + 共享夹具 | 03 完成并进入 main |
| Phase-23-05 | `2.5.5` | `develop/2.5.5` | `make e2e`：Playwright fixture 承接 | 04 完成并进入 main |
| Phase-23-06 | `2.5.6` | `develop/2.5.6` | `make package`：Bundle/manifest/摘要 + `lifecycle` 隔离安装 | 05 完成并进入 main |
| Phase-23-07 | `2.5.7` | `develop/2.5.7` | 删除 `scripts/`，按能力逐项附承接证明 | §4 五条判据全部通过 |

01～02 无真实依赖等待，可连续执行。03 起每批都要求上一批的入口在 CI 与本地同时可用。

## 3. 接口与共同约束

### 3.1 Make 目标契约（两级调度）

采用分级 Makefile：**根 Makefile 只负责选择组件与调度，组件 Makefile 负责自己的构建细节**
（参考 `bkmonitor-datalink` 的工程组织方式）。

```makefile
# 根 Makefile —— 保持短小，不写长篇 Shell recipe
GO_MODULES := backend componentmetrics router marshaller monitor lifecycle loadtest \
              exporters/elasticsearch exporters/kafka exporters/mysql \
              exporters/rabbitmq exporters/redis exporters/victoriametrics
NPM_MODULES := frontend admin-frontend
MODULES := $(GO_MODULES) $(NPM_MODULES)

test:
	@test -n "$(MODULE)" || { echo 'MODULE is required, e.g. make test MODULE=backend' >&2; exit 2; }
	@cd $(MODULE) && $(MAKE) test
check:
	@cd $(MODULE) && $(MAKE) check
check-all:
	@for m in $(MODULES); do $(MAKE) --no-print-directory check MODULE=$$m || exit 1; done
```

组件 Makefile 自行决定编译参数与产物（示例：`backend/Makefile` 的 `test` / `race` / `check` / `build`）。

对外目标契约：

| 目标 | 承接职责 | 实现层 |
| --- | --- | --- |
| `make deps` | Compose 启动依赖，等待健康检查 | Compose |
| `make dev` / `make dev-observe` | 准备数据库，启动对应源码服务与 Vite | Compose + 助手 |
| `make test MODULE=<name>` | 调度到组件 Makefile，运行该模块测试 | 根 → 组件 Makefile |
| `make check [MODULE=]` / `make check-all` | 格式检查、Go vet、前端类型检查 | 根 → 组件 Makefile |
| `make integration [SCOPE=]` | 准备隔离环境，调用 Go 集成测试，清理资源 | 助手 + Go 测试 |
| `make e2e [SCOPE=]` | 准备隔离环境，调用 Playwright，清理资源 | 助手 + Playwright |
| `make build` / `make package` | 构建程序与前端 / 构建镜像及交付 Bundle | 组件 Makefile + 交付工具 |
| `make stop` | 停止当前工作区创建的资源 | 助手 |

`make help` 保持为默认目标。`make monitor-image` 的职责并入 `make dev-observe` 或
`make build`，不单独保留。根 Makefile 中的模块清单是唯一来源，不再保留 Python 侧的
第二份 `MODULES` 表。

### 3.2 模块集合必须完整

GoPulse 有多个独立 Go 模块，`make test` / `make check` 必须覆盖**明确且完整**的集合：

- Go（13）：`backend`、`componentmetrics`、`router`、`marshaller`、`monitor`、`lifecycle`、
  `loadtest`、`exporters/{elasticsearch,kafka,mysql,rabbitmq,redis,victoriametrics}`
- 前端（2）：`frontend`、`admin-frontend`

每个模块获得自己的 `Makefile`，至少提供 `test` 与 `check`；Go 模块另提供 `race` 与 `build`。
模块可独立执行：`cd <module> && make test`。

**现存缺口**：当前 `MODULES` 表与 CI 的 20 个 job **都不覆盖 `lifecycle` 与 `loadtest`**——
两者共有 14 个测试文件 / 1,477 行从未在统一入口或 CI 中运行。01 必须把它们纳入目标集合，
或在不纳入时记录明确理由与替代运行时点。

### 3.2.1 共享模块的依赖解析

`componentmetrics` 是 9 个模块的唯一共享依赖，当前在所有 `go.mod` 中声明为 `v0.0.0` 并配
相对路径 `replace`；仓库 **0 个 git tag**，该模块从未发布。因此“禁用工作区后已发布依赖仍可
解析”这一工程标准当前**不成立**。

23-02 与 `make build` 同批处理：为 `componentmetrics` 打子目录 tag（`componentmetrics/v0.1.0`），
并把 9 个模块的 `go.mod` 从 `v0.0.0 + replace` 改为真实版本依赖。

该动作只影响库依赖解析，**不触碰发布模型**——`componentmetrics` 不是部署组件，`VERSION`
单一来源规则不变。

不引入 `go.work`：当前签入的 `replace` 已使跨 Module 联调可用，真正缺失的是已发布依赖解析。

### 3.3 CI 只维护一份步骤

`quality-gates.yml` 的对应 job 改为调用同一 Make 目标，不再重复书写命令。执行步骤与本地
一致；规则校验（governance 的 4 个校验器）保留为独立步骤，不属于本次清理对象。

### 3.4 失败与清理

每个目标在任一步骤失败时返回非零状态，并清理**本次运行创建**的资源。归属校验保留：只停止
本工作区创建的进程、容器、网络和卷；不做全局 prune。

### 3.5 迁移过程中的双重维护

03 起新入口与旧 `scripts/` 会短期并存。并存期内两者必须产生等价结果；每批记录等价性证据，
并在该批完成时删除已被完整替代的旧实现。不以"保留旧入口更安全"为由长期并存。

## 4. 固定阶段验收

阶段完成的唯一判据：在**全新检出、没有旧工作目录、没有手工准备**的环境中，下列五条全部成立。

| # | 判据 | 验证方式 |
| --- | --- | --- |
| 1 | 启动开发环境并安全停止 | `make deps` → 依赖健康；`make dev` / `make dev-observe` → 就绪；`make stop` → 只停本工作区资源，无残留容器/网络/卷 |
| 2 | 模块测试、隔离集成测试、浏览器测试 | `make test MODULE=<每个模块>`、`make check`、`make integration`、`make e2e SCOPE=business\|observe` 全过；并发执行两次互不干扰 |
| 3 | CI 使用相同入口 | `quality-gates.yml` 各 job 调用与本地相同的 Make 目标；`update` 与 `develop` 均可运行 |
| 4 | 生成 Bundle 并隔离安装启动 | `make package` 产出 manifest 与摘要 → `lifecycle` 在全新环境安装、启动、卸载 |
| 5 | 失败非零退出并清理本次资源 | 注入失败（端口占用、依赖未就绪、测试失败、构建失败）逐一验证退出码与清理范围 |

**这五条全部成立后**，才允许执行 23-07 删除 `scripts/`。已正式退役的 Phase 矩阵无需重建。

阶段级集成结果：删除完成后，仓库中不存在需要 Python 才能完成的开发、测试、构建或交付步骤。

## 5. 成本、证据与停止

### 5.1 预算

每批为普通实施文件，按 `dev/rules/implementation-execution-budget.md` 登记：目标 120 分钟、
累计上限 180 分钟。整体 7 批，不通过改名或重开预算隐藏超预算任务。

| 批次 | 预计 | 上限 | 主要成本项 |
| --- | --- | --- | --- |
| 23-01 | 90 | 180 | 模块集合核对、Makefile 骨架、CI 切换 |
| 23-02 | 100 | 180 | 镜像构建入口、`package-redis-exporter.sh` 承接 |
| 23-03 | 150 | 180 | 进程管理、就绪检查、归属校验（最大的单批） |
| 23-04 | 130 | 180 | Go 集成测试夹具、隔离与清理 |
| 23-05 | 110 | 180 | Playwright fixture 归位 |
| 23-06 | 140 | 180 | 交付单一入口 + `lifecycle` 安装验收（含真实安装运行） |
| 23-07 | 90 | 180 | 按能力删除 + §4 五条全量复验 |

03 与 06 接近上限。若发现超出，在该批拆出子批，不延长单批上限。

### 5.2 阶段级证据

- 每批的完成日志按既有规则写入 `dev/logs/Phase-23/`（与分方案同名）。
- §4 五条判据的验证在 `2.5.7` 执行并留原始回执；不得以早期批次的局部结果替代阶段级复验。
- `dev/status/capability-status.md` 在 23-07 记录：`scripts/` 退役范围、承接证明、
  以及**能力边界**（哪些能力改为由 Make 目标与原生测试承载）。
- 历史证据不改写：`dev/logs/**` 只增不改。

### 5.3 停止与接续

- 在累计上限或两次定向诊断未获已证明原因时停止，保留实际进度与证据，登记接续清单。
- 不在未证明原因的情况下重启整套入口；不以延长超时掩盖问题。
- 若某能力在预算内无法找到等价承接，**该能力对应的 `scripts/` 内容不删除**，并在 23-07
  记录为有理由的保留项与后续批次建议。删除不是本阶段的目标，承接才是。

### 5.4 与其他文件的关系

- `dev/imple/Phase-22/Phase-22-总实施方案.md` 已完成，其原生入口成果由本阶段复用。
- `rabbish/PLAN-closeout-2026-10-09.md` 的 Phase 16/17 退役（13 文件 / 1,063 行）并入 23-07，
  与其他 `scripts/` 内容按能力一并删除；不单独先做。
- 本阶段不修改 `deploy/phase16-acceptance.yaml`（属 `deploy/`，需批次或单独授权）。
