# Phase 23 总实施方案：原生入口替代 `scripts/`

> 状态：初稿 2026-10-09；同日合并修订（原 23-04、23-05 并入 23-03，见 §6）。当前完成版本 `2.5.2`。**待用户批准后进入主远端 main。**
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
| Phase-23-03 | `2.5.3` | `develop/2.5.3` | 本机环境编排原生承接：`devtools` 助手 + `make deps` / `dev` / `dev-observe` / `stop` / `integration` / `e2e`；退役 `local_development.py` 与 `make monitor-image`（原 23-03、23-04、23-05 合并，分段 A/B/C） | 02 完成并进入 main，且本合并修订已进入主远端 main |
| Phase-23-04 | `2.5.4` | `develop/2.5.4` | `make package`：Bundle/manifest/摘要 + `lifecycle` 隔离安装（原 23-06） | 03 完成并进入 main |
| Phase-23-05 | `2.5.5` | `develop/2.5.5` | 验收能力原生承接与 `scripts/` 全量退役：新增 `acceptance` 模块承接全栈闭包与专项验收，构建缓存、插件打包、容器栈入口迁入 `devtools` / `monitor`，治理工具迁入 `ci/`；删除全部 97 文件（原 23-07），随后按 §4 全量复验 | 04 完成并进入 main；§4 五条判据在删除前的候选上全量通过 |

批次编号在 2026-10-09 合并修订后重排：原 23-04（`integration`）与原 23-05（`e2e`）并入 23-03，
原 23-06、23-07 顺延为 23-04、23-05。映射与原 23-01/23-02 完成方案中的历史引用见 §6。

01～02 无真实依赖等待，可连续执行。03 起每批都要求上一批的入口在 CI 与本地同时可用。
23-03 三段（A 开发与依赖、B 集成、C 浏览器）在同一分支与版本内顺序执行，段边界是强制停点：
前一段门禁未通过不得进入下一段，也不得用后一段的通过替代前一段的失败。

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
| `make build-images [CACHE=gha\|none] [DRY_RUN=1]` | 构建产品镜像（本地与 CI 同一目标，缓存实现由 `devtools` 承接） | 助手 + Compose |
| `make verify-compose [SCOPE=]` / `make verify-business` / `make verify-observe SCOPE=` / `make verify-plugins\|verify-alerts\|verify-roles\|verify-pages` | 23-05 承接的验收能力：全栈闭包与各专项矩阵 | `acceptance` 模块 |
| `make verify-lifecycle [INSTALL=clean\|reuse]` | 隔离安装与复用安装验收 | `acceptance` 模块 + `lifecycle` |
| `make stack-up` / `make stack-down` / `make stack-verify` | 容器原生栈日常生命周期（原 `scripts/dev.sh` 等） | `devtools` 助手 |
| `make stop` | 停止当前工作区创建的资源 | 助手 |

`make help` 保持为默认目标。`make monitor-image` 不单独保留：镜像准备由 23-03 的原生助手统一实现，
`make dev-observe` 与 observe 测试入口（`make integration` / `make e2e SCOPE=observe`）在需要时
自行按内容摘要 tag 准备并复用；该目标与两个 CI 调用步骤在 23-03 内删除。根 Makefile 中的模块
清单是唯一来源（23-03 增加 `devtools`，见 §3.2），不再保留 Python 侧的第二份 `MODULES` 表。

### 3.2 模块集合必须完整

GoPulse 有多个独立 Go 模块，`make test` / `make check` 必须覆盖**明确且完整**的集合：

- Go（15）：`backend`、`componentmetrics`、`router`、`marshaller`、`monitor`、`lifecycle`、
  `loadtest`、`devtools`、`acceptance`、`exporters/{elasticsearch,kafka,mysql,rabbitmq,redis,victoriametrics}`
- 前端（2）：`frontend`、`admin-frontend`

`devtools` 是 23-03 新增的本机环境编排助手模块（进程归属、就绪探测、Compose 编排、测试隔离），
不属于部署组件，不进入发布清单。`acceptance` 是 23-05 新增的验收编排与断言模块（全栈闭包、
业务与可观测专项、插件/告警/角色/页面、隔离安装），同样不属于部署组件，不进入发布清单。
模块总数因此为 17（23-01 时为 15，23-03 时为 16）。

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

环境编排的并存期收敛在 23-03 内部：先在基线提交上采集旧实现回执（私有 env 文件、状态 JSON、
Compose 项目名、端口集合、就绪端点、退出码），再用新助手逐项比对，比对通过后在同一批删除
`local_development.py` 及其自测，不把并存期拖到后续批次。仍在使用、但属于其他能力的
`scripts/*.sh` 验证工具不因本阶段改动而删除，其承接与退役归 23-05。不以"保留旧入口更安全"
为由长期并存。

## 4. 固定阶段验收

阶段完成的唯一判据：在**全新检出、没有旧工作目录、没有手工准备**的环境中，下列五条全部成立。

| # | 判据 | 验证方式 |
| --- | --- | --- |
| 1 | 启动开发环境并安全停止 | `make deps` → 依赖健康；`make dev` / `make dev-observe` → 就绪；`make stop` → 只停本工作区资源，无残留容器/网络/卷 |
| 2 | 模块测试、隔离集成测试、浏览器测试 | `make test MODULE=<每个模块>`、`make check`、`make integration`、`make e2e SCOPE=business\|observe` 全过；并发执行两次互不干扰 |
| 3 | CI 使用相同入口 | `quality-gates.yml` 各 job 调用与本地相同的 Make 目标；`update` 与 `develop` 均可运行 |
| 4 | 生成 Bundle 并隔离安装启动 | `make package` 产出 manifest 与摘要 → `lifecycle` 在全新环境安装、启动、卸载 |
| 5 | 失败非零退出并清理本次资源 | 注入失败（端口占用、依赖未就绪、测试失败、构建失败）逐一验证退出码与清理范围 |

**这五条全部成立后**，才允许执行 23-05 删除 `scripts/`。已正式退役的 Phase 矩阵无需重建。
判据 1、2 在 23-03 已按分段门禁真实验证；23-05 仍需按本表全量复验，不以 23-03 的局部结果替代。

阶段级集成结果：删除完成后，仓库中不存在需要 Python 才能完成的开发、测试、构建或交付步骤。

## 5. 成本、证据与停止

### 5.1 预算

每批按 `dev/rules/implementation-execution-budget.md` 登记：普通实施文件目标 120 分钟、累计上限
180 分钟；长时文件（23-03、23-05，均由用户在 2026-10-09 明确授权）按各段上限之和登记，段边界为
强制停点。整体 5 批，不通过改名或重开预算隐藏超预算任务。

| 批次 | 预计 | 上限 | 主要成本项 |
| --- | --- | --- | --- |
| 23-01 | 90 | 180 | 模块集合核对、Makefile 骨架、CI 切换（已完成） |
| 23-02 | 100 | 180 | 镜像构建入口、`package-redis-exporter.sh` 承接（已完成） |
| 23-03 | 390 | 390 | 长时文件（用户 2026-10-09 授权，见 §6）：`devtools` 助手与 `deps` / `dev` / `dev-observe` / `stop`（A 150）、`integration`（B 130）、`e2e` 与旧实现退役（C 110） |
| 23-04 | 140 | 180 | 交付单一入口 + `lifecycle` 安装验收（含真实安装运行） |
| 23-05 | 1440 | 1920 | 长时文件（用户 2026-10-09 指定"23-05 全部完成"，见 §6）：构建缓存与 CI 面（A 120）、Compose 全栈闭包承接（B 240）、业务与可观测专项承接（C 420）、生命周期与镜像入口承接（D 150）、历史矩阵与交付库退役（E 120）、治理迁移与文档（F 120）、§4 五条全量复验（G 180）、全量删除与收口（H 90） |

23-03 与 23-05 是本阶段的两个长时文件：预计与累计上限按上表登记，各段上限之和即该文件的累计上限，
段边界为强制停点；任一段触顶即在该段边界收尾并登记接续，不占用后一段预算，也不重开预算。两次授权
各自只覆盖对应批次的范围（23-03 为原 23-03/04/05 合并后的三段；23-05 为 2026-10-09 用户指定的
"全部完成"范围，即验收能力原生承接与 `scripts/` 全量退役），不放宽其他批次的普通文件上限。
23-04 接近上限；若发现超出，在该批拆出子批，不延长单批上限。23-02 登记的接续项
（`compose_build_cache.py` 原生承接与 CI 镜像步骤切换）随重编号转入 23-05，已由 23-05 分方案
登记为段 A。

### 5.2 阶段级证据

- 每批的完成日志按既有规则写入 `dev/logs/Phase-23/`（与分方案同名）。
- §4 五条判据的验证在 `2.5.5` 执行并留原始回执；不得以早期批次的局部结果替代阶段级复验。
- `dev/status/capability-status.md` 在 23-05 记录：`scripts/` 退役范围、承接证明、
  以及**能力边界**（哪些能力改为由 Make 目标与原生测试承载）。23-03 已退役的
  `scripts/ci/local_development.py`（1,156 行）与 `scripts/ci/test_local_development.py`
  （179 行）的承接证明在同一处汇总。
- 历史证据不改写：`dev/logs/**` 只增不改。

### 5.3 停止与接续

- 在累计上限或两次定向诊断未获已证明原因时停止，保留实际进度与证据，登记接续清单。
- 不在未证明原因的情况下重启整套入口；不以延长超时掩盖问题。
- 23-05 的目标是 `scripts/` 全量退役（97 文件 / 17,701 行），§5.1 的长时登记与 23-05 分方案的
  逐能力承接台账是这一目标的执行依据。若某能力在预算内仍无法找到等价承接，**该能力对应的
  `scripts/` 内容不删除**，并在该段边界停止、记录差异、最小复现与影响范围，交回用户决定
  （补做 Go 化、正式退役该能力或拆分批次）；该保留项不得被当作已完成，也不改变阶段终点
  （§4：仓库中不存在需要 Python 才能完成的开发、测试、构建或交付步骤）。

### 5.4 与其他文件的关系

- `dev/imple/Phase-22/Phase-22-总实施方案.md` 已完成，其原生入口成果由本阶段复用。
- Phase 16/17 退役（13 文件 / 1,063 行）并入 23-05，与其他 `scripts/` 内容按能力一并删除；
  不单独先做。该清单原引用 `rabbish/PLAN-closeout-2026-10-09.md`：实测该文件位于**仓库外**
  scratch 路径 `/home/ray/rabbish/PLAN-closeout-2026-10-09.md`（未跟踪、不在 CI 或仓库规则内），
  仓库内不存在 `rabbish/` 路径；23-05 分方案已从工作树逐文件重建同一清单（13 文件 / 1,063 行）
  并核对一致，此后以 23-05 分方案 §1.5 为准。
- 本阶段不修改 `deploy/phase16-acceptance.yaml`（属 `deploy/`，需批次或单独授权）；该文件的
  `phase16` 入口模式随 23-05 的执行器退役后成为无引用文件，登记为后续项。

## 6. 2026-10-09 合并修订与决策记录

原 23-04（`make integration`）与原 23-05（`make e2e`）并入 23-03，成为单批单文件；本表与 §2、§3、
§5 已按新编号同步。做出该修订的原因、授权与影响如下。

| 项目 | 记录 |
| --- | --- |
| 修订原因 | 三段共用同一个环境助手与同一份 workspace / 进程归属 / Compose / 端口隔离契约。分段独立会两次重写同一实现，并在并存期产生两份互相背离的隔离契约；用户 2026-10-09 决定合并为一批实施 |
| 批次映射 | 原 23-03 + 23-04 + 23-05 → 新 23-03（`2.5.3`，`develop/2.5.3`）；原 23-06 → 新 23-04（`2.5.4`）；原 23-07 → 新 23-05（`2.5.5`） |
| 未创建分支重算 | `develop/2.5.3`、`develop/2.5.4`、`develop/2.5.5` 均未创建，按新编号分配；已存在的 `develop/2.5.1`、`develop/2.5.2` 不改名、不重编号 |
| 历史引用 | 23-01、23-02 两份已完成方案中的 `23-06` / `23-07` 引用保留原文，按上表映射阅读；不改写历史方案与日志 |
| 长时文件授权 | 三段合计预计 390 分钟，超出普通实施文件 180 分钟上限。用户 2026-10-09 明确选择单批单文件并授权按长时文件登记：预计与累计上限同为 390，分段 A/B/C 各 150/130/110、各段上限 180，段边界强制停点。该授权只覆盖 23-03 的合并范围，不写入其他批次，也不构成"每批可用 390 分钟"的先例 |
| 23-05 范围重登记 | 23-04 完成后实测 `scripts/` 仍为 97 文件 / 17,701 行，其中 CI 关键面（`verify-compose*.sh`、9 个 `--self-test`、19 个自测模块、验收镜像入口）从未被分配承接批次；总方案 §1 只登记了 3 处硬依赖，§5.1 原登记 23-05 为 90/180，不足以完成承接。用户 2026-10-09 明确要求"23-05 全部完成"，即不做"先收口、后续批次再迁"的拆分，也不退役仍在使用的验收能力换速度。据此 §2/§3.1/§3.2/§5 与 23-05 分方案同步重登记 |
| 23-05 长时文件授权 | 按 §5.1 登记为长时文件：预计 1,440 分钟、累计上限 1,920 分钟（各段上限之和），段 A–H 边界强制停点，成本构成、实验成本与诊断限制见 23-05 分方案 §5。该授权只覆盖 23-05 的"验收能力原生承接 + `scripts/` 全量退役"范围；若执行中发现单批过长，按该分方案 §8 的段边界拆为 `A+B`、`C`、`D+E+F`、`G+H` 四批，只改本表与 §5.1，不改写分方案内容 |
| 23-05 退役决定（用户 2026-10-09 确认） | Phase 16/17 正式矩阵执行器（13 文件 / 1,063 行）依据 `dev/status/capability-status.md:54` 的既有用户决定退役；Phase 13/14/15 历史闭包路径与 Phase 17/21 运行时矩阵按 23-05 分方案 §1.5 新增登记退役；PowerShell 三个入口按零活引用退役。三组退役自该确认起生效，能力边界与重跑前提写入 `capability-status.md` |
| 治理工具处置（用户 2026-10-09 确认） | 治理与版本校验器（1,316 行）**保留 Python** 并迁移到 `ci/`：只更新必要的路径引用（工具内部路径表、CI 命令、规则文档引用），校验规则与失败语义逐字不变，原有 19 个自测与 CI 治理检查全部通过；不做 Go 重写。依据：§3.3 明确其"保留为独立步骤，不属于本次清理对象"，§4 的阶段结果只覆盖开发、测试、构建与交付步骤。23-05 因此把治理迁移（段 E）排在退役（段 F）之前，先证明迁移等价再删除随能力退役的测试模块 |
| 模块集合变化 | 23-03 新增 `devtools` 模块（本机环境编排助手，非部署组件）：Go 13 → 14，模块总数 15 → 16；23-05 新增 `acceptance` 模块（验收编排与断言，非部署组件）：Go 14 → 15，模块总数 16 → 17；根 Makefile 仍是模块清单唯一来源 |
| `monitor-image` 归属 | 两个 CI 调用者（`integration-observe`、`e2e-observe`）随合并进入 23-03 范围，因此镜像准备的承接**与删除**都在 23-03 内完成，不再保留到后续批次；职责由 `devtools` 助手统一实现，由 `make dev-observe` 与 observe 测试入口按需调用（见 §3.1） |
| 阶段验收不变 | §4 五条判据、`2.5.5` 的全量复验与 `scripts/` 删除条件不变；23-03 的分段门禁是过程门禁，不替代阶段级复验 |
