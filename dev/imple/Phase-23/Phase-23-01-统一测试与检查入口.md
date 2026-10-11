# Phase-23-01：组件级 Makefile 与统一测试检查入口

> 状态：初稿 2026-10-09。目标版本 `2.5.1`，分支 `develop/2.5.1`。
> 进入条件：Phase-23 总实施方案及本分方案已进入主远端 main。

## 1. 交付与允许文件

**唯一主要交付结果**：根 Makefile 只负责选择组件与调度，每个模块有自己的 Makefile 负责构建
细节；`make test` / `make check` 成为本地与 CI 共用的测试与检查入口，覆盖完整模块集合。

允许新增：

| 文件 | 内容 |
| --- | --- |
| `<13 个 Go 模块>/Makefile` | 各自的 `test` `race` `check` `build`；Go 命令与参数由组件自己决定 |
| `frontend/Makefile`、`admin-frontend/Makefile` | 各自的 `test` `check` `build`；调用本目录 npm 脚本 |

允许修改：

| 文件 | 改动 |
| --- | --- |
| `Makefile`（根） | 新增 `GO_MODULES` / `NPM_MODULES` 变量作为**模块清单唯一来源**；`test` / `race` / `check` / `build` 改为 `$(MAKE) -C $(MODULE) …` 调度；新增 `check-all`；保留既有 `dev` / `dev-observe` / `integration` / `e2e` / `stop` / `monitor-image` 不动 |
| `scripts/ci/local_development.py` | 仅删除纯转发部分：`MODULES` 表、`run_test`、`test` 子命令及其 argparse 注册。`ensure_npm_dependencies` 保留（`start_lifecycle` 与 `run_e2e` 仍调用） |
| `.github/workflows/quality-gates.yml` | 11 个 Go job 的命令改为 `make check` / `make test` / `make race`；2 个前端 job 改为 `make test` / `make build`；新增 `lifecycle` 与 `loadtest` 两个 job。**原 20 个 job 全部保留，`if` 条件不变** |
| `scripts/ci/quality_scope.py` | `MODULES` / `GO_MODULE_ROOTS` 增加 `lifecycle`、`loadtest`；新增 `STANDALONE_MODULES` 防止这两个模块误触发集成与浏览器矩阵 |
| `scripts/ci/test_local_development.py` | 移除 `MODULES` 导入与断言，保留 `compose_environment` 拒绝未知模块的断言 |
| `VERSION` 及产品版本元数据 | `.env.example`、`frontend/package{,-lock}.json`、`admin-frontend/package{,-lock}.json` 同步到 `2.5.1` |
| `dev/imple/Phase-23/Phase-23-01-*.md` | 完成后的实测记录 |

不属于本批：`make deps` / `dev` / `stop`（23-03）、`integration` / `e2e`（23-04、05）、
`build` / `package` 的镜像与交付部分（23-02、06）、`componentmetrics` 发版（23-02）、
`scripts/` 其余内容的删除（23-07）。

**根 `build` 的边界**：本批提供**组件级** `build`（Go 为 `go build ./...`，前端为 `npm run build`），
因为 CI 前端 job 原本就执行 `npm run build`。产品级构建、镜像与 Bundle 仍属 23-02 / 23-06。

**模块集合**（15 个，根 Makefile 变量为唯一来源）：

- Go（13）：`backend`、`componentmetrics`、`router`、`marshaller`、`monitor`、`lifecycle`、
  `loadtest`、`exporters/{elasticsearch,kafka,mysql,rabbitmq,redis,victoriametrics}`
- 前端（2）：`frontend`、`admin-frontend`

**现存缺口**：当前 `MODULES` 表与 CI 的 20 个 job 都不覆盖 `lifecycle`（6 个测试文件 / 692 行）
与 `loadtest`（8 个 / 785 行），共 1,477 行从未在统一入口或 CI 中运行。本批必须纳入并为其
增加 CI 步骤；若无法纳入，须写明理由与替代运行时点，不得静默遗漏。

**命令单一来源**：CI 保留既有 job 结构与 runner 缓存设置，只把命令替换为 Make 目标。不把
20 个 job 合并成一个 `check-all`——那会丢失并行度与按模块缓存，且超出本批范围。

**不改变**：`VERSION` 单一来源、发布模型、目录结构、`go.mod` 依赖声明（属 23-02）。

## 2. 验证映射与固定门禁

| 字段 | 内容 |
| --- | --- |
| 验证对象 | 组件级测试与检查入口是否真实执行原命令；根 Makefile 是否正确调度；模块清单是否唯一；CI 与本地是否执行同一目标 |
| 已有覆盖 | 各模块 `go.mod` 与 `_test.go`；`scripts/ci/local_development.py` 的 `MODULES` 表与 `run_test`（本批退役）；`.github/workflows/quality-gates.yml` 各模块 job 的内联命令（本批改为调用 Make）；`scripts/ci/test_quality_scope.py` |
| 最低有效层级 | 命令层与单元层。改动的行为是"执行哪个命令、覆盖哪些模块"，不是产品逻辑；集成与浏览器层的既有门禁不在本批范围，也不需要为本批新增 |
| 本批变更 | 新增 15 个组件 `Makefile`；改写根 `Makefile`；删除 Python 转发层与其用例；`quality_scope.py` 增加两个模块并防误触发；改写 13 个模块 job、新增 2 个 job |
| 固定门禁 | 见下表判据，以及本节末尾的治理门禁命令 |

| 判据 | 验证方式 |
| --- | --- |
| 组件 Makefile 齐全 | 15 个模块各有 `Makefile`，提供 `test` 与 `check`；Go 模块另有 `race` 与 `build` |
| 组件可独立执行 | `cd <module> && make test` 对 15 个模块逐一成功；不依赖根 Makefile |
| 根调度正确 | `make test MODULE=x` 调度到该模块的 Makefile；`MODULE` 缺失时以非零退出并给出用法 |
| 目标集合完整 | `make check-all` 覆盖 15 个模块（含 `lifecycle`、`loadtest`）；缺一个即失败 |
| 与旧入口等价 | 对同一模块，`make test MODULE=x` 与改动前 `python3 scripts/ci/local_development.py test --module x` 执行同一命令并同样传递退出码 |
| `check` 语义正确 | Go 覆盖 `gofmt` 与 `go vet`，前端覆盖 `typecheck`；注入格式错误或 `vet` 失败时必须非零退出并给出诊断 |
| `race` 目标可用 | `make race MODULE=x` 实际带 `-race`；Go 模块均提供该目标（前端不提供） |
| 纯转发层已删 | `local_development.py` 中不再有 `MODULES` / `run_test` / `test` 子命令；`make test` 不调用 Python |
| 模块清单唯一 | 仓库中只有根 Makefile 一处模块清单；Python 侧无第二份 |
| CI 命令单一来源 | 各模块 job 内不再出现 `go test` / `gofmt` / `go vet` / `npm test` / `npm run build` 字面命令；原 20 个 job 全部保留且 `if` 条件不变，另新增 `lifecycle` / `loadtest` 两个 |
| 无重复检查 | 同一 job 内不重复执行同一检查；前端 `typecheck` 由 `npm run build` 内部执行，故前端 job 不再单独加 `check` 步骤 |
| CI 与本地一致 | CI 绿，且本地跑同一目标得到相同结果 |
| 无回归 | `python3 -m unittest discover -s scripts/ci -p 'test_*.py'` 通过（数量变化须逐项解释） |

固定门禁：`validate_versions.py`、`validate_branch.py --mode development`、`quality_scope.py`、
`bash -n` 全部 `scripts/*.sh`、9 个 `--self-test`、YAML 解析。

## 3. 预算与实验成本

| 项目 | 内容 |
| --- | --- |
| 范围 | 见 §1；一个交付结果（组件级 Makefile + 统一测试检查入口），不摊入后续批次职责 |
| 总预算 | 目标 110 分钟，累计上限 180 分钟 |
| 阶段预算 | 实现 40 / 直接检查 20 / 构建启动 15 / 真实验收 30 / 证据清理 10 / 机动 25 |
| 实验成本 | 无长时实验。成本为 15 个模块各一次组件级测试 + 一次 `check-all` + 一次 CI 全绿，约 20–30 分钟墙钟 |
| 重试成本 | 同一未解决原因最多两次定向诊断，每次 ≤10 分钟；不重启整套 CI 探查 |
| 停点 | 50% / 80% 报告进度、剩余门禁与下一步；超时保留进度、停止新工作并登记接续 |

15 个组件 Makefile 是本批主要工作量，但内容短（各 4–8 行）。若某个模块的命令无法用统一的
四目标表达（例如需要特殊环境变量或非标准工作目录），按 §4 偏差处理，不为它破坏统一形状。

## 4. 停止、接续与完成

**完成条件**：§2 全部门禁通过；15 个模块可 `cd` 进去独立构建与测试；`make test` / `make check`
在全新检出可用；CI 绿且 job 结构不变；`lifecycle` 与 `loadtest` 已纳入或被记录为保留项。

**偏差处理**：若某模块无法通过统一目标表达，在该批次记录该模块、原因与替代命令，
**不修改该模块的既有 CI 行为**，并列入 23-07 的保留项。

**接续**：本批完成并进入 main 后，23-02 才开始。本批不触碰 `deploy/`、`lifecycle/` 与
`frontend/` 的源码实现、任何 `go.mod` 依赖声明、任何证据文件。
