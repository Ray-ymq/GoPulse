# GoPulse 工程稳定性改造方案

> 状态：具体改造提案，未实施。制定日期：2026-10-10。
> 调查基线：`upstream/main` 的 `0610f3f`，完成产品版本 `2.5.5`。
> 本文定义改动、接口、验证与建议拆分，不分配 Phase、产品版本或开发分支。
> 产品执行前，须将本文纳入新的 Phase 总实施方案及带预算的分方案，按根 AGENTS.md 分配版本和分支。

## 1. 这次具体交付什么

完成一套覆盖 15 个 Go 模块和两个前端的工程验证流程：

1. `make doctor` 检查开发工具是否符合要求。
2. `make bootstrap` 准备项目内的检查工具和前端依赖。
3. `make verify BASE_REF=upstream/main` 根据实际改动执行检查、测试和所需真实验证。
4. 本地与 CI 使用相同的模块命令、改动选择规则和报告格式。
5. 失败能指出具体步骤、退出状态和复现入口；清理失败也使最终结果失败。

上述三个入口均为拟新增接口，当前仓库尚不能执行。现有 `make check/test/race/build`、
`make integration/e2e/verify-compose/package` 是承接基础。工程报告不替代产品验收回执。

范围是工程入口、依赖准备、静态检查、改动验证、CI 调度、失败诊断和工程约定。
业务服务和平台服务仍遵循已批准架构；沿用现有顶层模块、业务 API、消息与数据合同。
本文不新增贡献流程，不重建已退役矩阵，不分配长期容量实验，也不改三份冻结设计。

## 2. 已核对的现状与具体缺口

| 现状证据 | 具体缺口 | 本次处理 |
| --- | --- | --- |
| 根 `Makefile` 已登记 15 个 Go 模块与两个前端，模块各有 Makefile | `ci/quality_scope.py` 与 CI 的模块清单仍独立维护 | 根 Makefile 输出模块清单，选择器与 CI 读取它；保留已有行为映射 |
| Go 模块 `check` 已执行 `gofmt`、`go vet`；前端有类型检查 | 尚未引入 Go 与 Vue/TypeScript 的额外缺陷检查 | 增加明确、少量的 lint 规则；完整报告与新增告警门禁分开记录 |
| 两个前端 Makefile 的 `node_modules` 目标没有依赖文件前置条件 | 锁文件更新后，已有目录不会触发重新安装 | 按输入摘要判断依赖是否需要重装 |
| `devrun/applications.go`、`testenv/e2e.go` 也按目录存在性跳过 npm 安装 | Make、源码开发和浏览器测试都存在同一准备缺口 | 三种入口调用同一个依赖准备函数 |
| `frontend/playwright.config.ts` 使用 line reporter，失败时保留 trace | 报告位置未按运行隔离；e2e 状态记录的是根 `test-results` 路径 | 用 run_id 隔离输出，并从实际配置记录路径 |
| `quality-gates.yml` 没有 artifact 上传步骤 | 私有日志和浏览器结果没有显式收集、脱敏和上传流程 | 建立诊断产物白名单及 CI 上传步骤 |
| `acceptance` 已有 Session、命令记录、清理与 `gopulse.acceptance.v1` 回执 | 临时目录会在清理中删除；失败诊断需要在清理前保存 | 在现有 harness 内保存诊断，继续使用既有回执合同 |
| `AcceptanceRunArgs` 使用 `compose run --rm`，acceptance 服务未挂载报告目录 | 一次性容器退出时，容器内的浏览器报告无法再收集 | 给本次容器明确身份，报告复制后立即定向删除，再执行 Session 清理 |
| `ci/quality_scope.py` 已按共同祖先 diff 选择模块、集成、浏览器和 Compose | 本地缺少使用同一选择结果的总验证入口 | 新入口直接读取选择器 JSON，不重写第二份选择器 |
| 自动 PR 已等待 push 质量门禁，并检查完成元数据 | 调整 CI 后需要保证必需检查没有因条件或矩阵展开而漏跑 | 增加结果汇总，校验预期检查与实际结果对应 |

以上是文件行为的核对，不是新一轮产品验收或缺陷复现结果。已有长期稳定性、容量、备份恢复
的证据边界继续以能力状态和历史日志为准。

## 3. 文件改动清单

以下为拟新增或修改文件；本文提交时只写方案与索引，不执行这些改动。

| 文件 | 改动内容 | 归属单元 |
| --- | --- | --- |
| `Makefile` | 增加 doctor/bootstrap/lint/lint-all/verify 和模块清单输出；保留既有目标语义 | A、C、F |
| `devtools/toolchains.json`（新增） | 工具要求、标准 CI 检查版本、lint 下载身份 | A |
| `devtools/internal/quality/doctor.go`、`bootstrap.go` 及对应测试（新增） | 工具探测、工具下载/校验与项目内缓存 | A |
| `devtools/cmd/devenv/main.go`、`main_test.go` | 扩展现有 CLI 的 doctor/bootstrap/frontend-deps/lint/verify/report 命令 | A～F，按新增接口分别修改 |
| `devtools/internal/frontenddeps/deps.go`、`deps_test.go`（新增） | 前端依赖摘要、互斥、安装与成功标记 | B |
| `devtools/internal/devrun/applications.go`、`devrun_test.go` | 开发启动复用依赖准备 | B |
| `devtools/internal/testenv/e2e.go`、`testenv_test.go` | 浏览器启动复用依赖准备；隔离并记录报告路径 | B、E |
| `frontend/Makefile`、`admin-frontend/Makefile` | 依赖准备改为调用原生助手；增加 lint，保留 check/test/build | B、D |
| `.golangci.yml`（新增） | 固定 Go 检查器与运行参数 | C |
| 15 个 Go 模块的 `Makefile` | 增加 lint 目标，并复用已安装工具 | C |
| `devtools/internal/quality/lint.go`、`lint_test.go`（新增） | 运行 lint、解析报告、区分完整告警与本次改动告警 | C、D |
| `frontend/eslint.config.mjs`、`admin-frontend/eslint.config.mjs`（新增） | 各模块加载本地插件，引用同一份规则数据 | D |
| `devtools/config/eslint-rules.mjs`（新增） | 两个前端共用的纯规则数据，不导入第三方插件 | D |
| 两个前端的 `package.json`、`package-lock.json` | 增加固定版本的 lint 依赖和 npm lint 入口 | D |
| `frontend/playwright.config.ts` | 报告目录来自本次运行参数；增加机器可读报告 | E |
| `devtools/internal/diagnostics/report.go`、`report_test.go`（新增） | 诊断收集、脱敏、白名单和大小限制 | E |
| `devtools/internal/testenv/testenv.go`、`processes.go` | 集成步骤记录及清理前诊断；不更改既有用例选择 | E |
| `devtools/internal/proc/proc.go`、`proc_test.go` | 为命令记录提供输出和退出结果；保护原有进程身份、失败传播与停止行为 | E |
| `acceptance/internal/harness/session.go`、`session_test.go` | 失败诊断在清理前落盘；清理结果分别记录 | E |
| `acceptance/internal/scenario/compose.go`、`suites.go`、`lifecycle.go` | 接入已存在 Session 的诊断生命周期；保留回执与失败语义 | E |
| `acceptance/cmd/gopulse-acceptance/main.go`、`main_test.go` | 增加可选诊断输出参数，Make 默认传入私有目录 | E |
| `devtools/internal/quality/verify.go`、`verify_test.go`（新增） | 按选择器结果编排既有命令，传播失败，输出总报告 | F |
| `ci/quality_scope.py`、`ci/test_quality_scope.py` | 读取根模块清单，输出矩阵数据与选择理由 | F |
| `ci/validate_quality_result.py`、`test_validate_quality_result.py`（新增） | 核对选中的检查确实执行、结果成功且属于同一 revision | F |
| `.github/workflows/quality-gates.yml` | 模块矩阵、先快后慢、诊断上传与最终汇总 | F |
| `.github/workflows/cache-warm.yml` | 从同一模块清单构造缓存矩阵 | F |
| `.github/workflows/auto-pr-merge.yml`、`ci/test_auto_pr_workflow.py` | 继续消费总门禁和 completion_ready，适配新 job 结构 | F |
| `.github/workflows/release-candidate.yml` | 保留 package 链；补失败诊断上传，不更改发布许可 | F |
| `AGENTS.md`、`dev/rules/engineering-conventions.md`（新增） | 增加工程入口与改动复核要求，引用现有权威合同 | A |
| `README.md`、`dev/validation/local-development-tests.md` | 更新实际可用命令及验证边界 | G |
| `dev/status/capability-status.md` | 只在实际验收完成后登记工程能力 | G |

Go 模块的准确集合：`backend`、`componentmetrics`、`router`、`marshaller`、`monitor`、
`lifecycle`、`loadtest`、`devtools`、`acceptance`，以及
`exporters/{elasticsearch,kafka,mysql,rabbitmq,redis,victoriametrics}`。

本提案没有授权任意产品代码修复。首次扫描只登记实际存量告警；若需要修复行为或扩大
代码文件范围，先把复现、具体文件与回归写入正式分方案，重新核对该单元的预算。

## 4. 命令与环境合同

### 4.1 新增命令的准确行为

| 拟新增命令 | 执行什么 | 成功条件 |
| --- | --- | --- |
| `make doctor` | 检查 Go、Node、npm、Python、Docker Engine 和 Compose；输出实际版本与仍待 bootstrap 的项目依赖 | 机器工具满足要求；项目依赖待准备有明确提示；不启动容器，不修改全局工具 |
| `make bootstrap` | 校验/安装项目内固定 lint 二进制，准备两个前端依赖和浏览器测试使用的 Chromium | 下载身份正确，依赖与浏览器准备成功；不启动业务服务 |
| `make bootstrap MODULE=frontend` | 只准备指定前端依赖 | 不操作另一个前端的目录 |
| `make lint MODULE=backend` | 对该 Go 模块做完整 lint | 没有告警；有存量告警时如实非零退出并保存报告 |
| `make lint MODULE=frontend BASE_REF=upstream/main` | 扫描完整前端，报告并阻断相对共同祖先 diff 的新增告警 | 新增告警为零；完整存量报告仍保存 |
| `make lint-all` | 对 17 个模块生成完整报告 | 无告警才返回成功；不能把存量报告写成通过 |
| `make verify BASE_REF=upstream/main` | 获取完整 diff 的选择结果，运行选中的既有检查和新增告警门禁 | 所有必需步骤成功，清理成功，报告完整 |
| `make verify BASE_REF=upstream/main PLAN_ONLY=1` | 只打印模块、必需检查和原因 | 不运行测试、不启动依赖服务 |
| `make verify MODULE=backend` | 仅执行指定模块的 check、lint、test、race、build | 报告明确标记 module 范围，不声明跨模块验证完成 |

`BASE_REF` 必须是实际存在的 Git ref；CI 使用 `origin/main`，本地示例使用 `upstream/main`。
`MODULE` 与 `BASE_REF` 总验证模式不能同时指定，避免含义模糊。
新增告警门禁比较实际 diff，不使用仅比较上一提交的 `--new` 模式。
Go 采用工具的共同祖先参数；ESLint JSON 由助手按 diff 文件/行范围分类。删除、重命名、
多提交和主线推进须有边界验证。未提交改动必须纳入本地范围，或明确拒绝并要求提交；
不允许静默忽略工作区。本方案选择纳入 tracked 工作区 diff 和模块内新增源码。

### 4.2 工具版本与依赖准备

语言版本继续读取 15 个 `go.mod` 的 `go 1.26`，模块集合读取根 Makefile。
标准检查版本拟固定为 Go `1.26.7`、Node `24.20.0`、npm `11.19.0`、golangci-lint `2.14.0`。
前三者是本次工作区实测版本；npm 需在 A 的预检中核对其与固定 Node 分发的兼容性。
交付构建仍读取现有 build-bases lock（其中 Go 为 `1.26.0`）；报告分别记录标准检查工具与
交付构建工具，不声称两条工具基线相同，也不在本次顺手升级交付镜像。
Docker/Compose 检查实际必需功能和可用性，不擅自升级宿主 Docker。
治理 Python 要求 `3.12+`；实际版本都写入报告。

`doctor` 可报告受支持的 Go 1.26.x 本地工具，但正式 CI 检查使用固定检查基线；本地工具与标准
版本不同时，报告标明差异，不能声称工具版本完全一致。版本策略仅细化开发工具，不另建
产品 VERSION 或独立组件发布模型。

lint 使用官方发布二进制，固定版本与下载 SHA256，保存到 `.run/tools/`，不安装到全局 PATH。
Chromium 使用已锁定 Playwright 的安装入口；浏览器版本纳入准备记录，由实际 e2e scope 在
启动环境前核对可用性。宿主系统库不足时明确失败并指出条件，不在入口内擅自安装系统软件。
ESLint、Vue 与 TypeScript parser 的具体版本在 D 的进入预检中核对现有 Node/TypeScript
组合后写入 package 与 lockfile；不通过降级现有产品依赖使工具强行兼容。

前端依赖成功标记记录以下输入的摘要：package.json、package-lock.json、实际 Node/npm 版本、
操作系统和架构。目录缺失、标记缺失或摘要改变时执行 `npm ci`；输入未变时复用。
安装失败不写成功标记。安装互斥按工作区和前端模块建立，两个前端不共用一个锁。
源码开发、浏览器测试和 Make 入口共用 `frontenddeps`；不保留三份存在性判断。
活动开发进程正在使用该模块时，不在后台替换其依赖，明确要求先正常停止开发入口。

## 5. 代码检查与改动复核

Go 的首批额外检查固定为 `staticcheck`、`errcheck`、`unused`；gofmt 与 vet 继续在既有 check
目标执行，不重复添加另一遍 vet。运行参数固定配置版本 2、单模块总超时 10 分钟、只读
依赖模式；包括普通测试，集成 build tags 的行为仍由既有真实集成入口验证。

前端首批规则限定为 `no-debugger`、`no-async-promise-executor`、`no-constant-condition`
（允许明确的无限循环）、`vue/no-mutating-props`、`vue/no-side-effects-in-computed-properties`。
TypeScript 继续使用既有 vue-tsc/tsc 类型检查。此轮不加入整仓格式重写、命名偏好或复杂度门禁。

新增告警必须阻断 verify/CI。完整存量扫描的结果保留为实际问题清单，单独报告，不把新增门禁
通过解释为“全仓零告警”。不允许整目录排除或通过扩大基线掩盖本批新增问题。
明确的误报只允许针对具体规则和位置给出理由。

工程约定集中补充以下具体要求，并链接已有合同：

| 改动边界 | 必须复核的内容 | 使用已有验证 |
| --- | --- | --- |
| HTTP/API/权限 | 状态码、错误码、Cookie、实时授权和角色装配 | Backend Go 测试、业务/观测 integration、对应浏览器 scope |
| 数据库/迁移 | 事实源、事务边界、并发锁、dirty 状态及失败恢复 | 既有真实 MySQL 迁移和业务集成 |
| 消息/重试 | ack/commit 时机、幂等、所有权、终态和有界重试 | 所属模块测试与既有真实链路 |
| 并发/生命周期 | goroutine 所有者、取消传播、资源释放、关停预算 | race、原生工具自测、归属清理与生命周期入口 |
| 配置/序列化/公开接口 | 默认值、未知值拒绝、字段与协议兼容、调用方影响 | 相关合同测试、模块消费者与实际集成 |

每批最终验证前复核完整 diff，仅记录本次改变的边界和实际结论。没有相关改变时不强制
运行额外矩阵，也不把写了一张检查表当成产品行为已经得到验证。

## 6. 失败报告的文件与行为

所有工程运行使用唯一 run_id，原始诊断保存到
`.run/private-reports/<run_id>/`；可发布文件保存到 `.run/reports/<run_id>/`。
目录私有权限为 0700，原始文件为 0600；现有 `.run/` ignore 继续生效。

发布白名单只有 `summary.json`、`steps.json`、`errors.txt`、`reproduce.txt`。
总量上限 20 MiB，单份日志摘录上限 2 MiB；截断只发生在发布副本，并注明截断。
记录 revision、VERSION、工具版本、输入摘要、scope、必需步骤、实际步骤、退出码、耗时、
清理结果和复现命令。工程摘要使用独立标识 `gopulse.engineering-run.v1`，不更换现有验收 schema。

原始环境、Cookie、token、数据库口令、原始 Playwright trace/HTML 和截图不自动上传。
测试运行的实际凭据用于文本脱敏；无法验证脱敏的文件不进入发布白名单。原始材料继续私有保留。
复现命令由已知 Make 参数重新生成，不复制含口令的原始命令串。

失败顺序固定为：记录主失败 → 保存诊断 → 停止本次进程 → 清理本次资源 → 写入清理结果
与最终摘要。报告错误和清理错误分别记录，不覆盖原来的主失败；任一必需步骤失败都返回非零。
不以 `--keep` 作为 CI 留证方式；保留诊断文件与保留运行资源是两件独立操作。

复用 `devtools` 的 workspace/proc/digest 和 `acceptance` 的 Session/Cleanup/receipt；
诊断 collector 位于现有 devenv，不另建框架、环境管理器或跨模块循环依赖。
acceptance 只输出自身私有诊断，collector 按受限路径收集；两模块不互相导入 internal 包。
Compose 浏览器运行不再在报告复制前使用 `--rm`：先给一次性容器分配本次 project/run 身份，
执行结束后复制实际报告到私有目录，再核对 project 标签并删除该容器。复制失败保留该失败
记录，仍进入本次资源清理；不能通过 `--keep` 长期保留容器。复制路径、文件归属和正常/失败
两种退出都需最低层与最小真实容器验证。
修改命令记录或截止时间处理前，先在最低层验证子进程失败、输出捕获和归属停止行为；
正常失败、取消和超时在摘要中分别记录，不能让“报告生成成功”覆盖执行失败。

## 7. CI 的具体改法

新的执行顺序：治理与改动选择 → 模块快检查 → 选中的真实集成/浏览器/Compose → 总结果汇总。
缓存预热从同一份根模块清单构造矩阵，缓存命中不能作为验证成功。

| 层级 | 执行内容 | 运行条件与预算 |
| --- | --- | --- |
| 治理 | 既有 ci 自测、版本、分支、范围校验与选择器 | 所有分支；update 保持治理范围 |
| Go 模块矩阵 | make check/lint/test/race/build MODULE=选中模块 | 快检查 job 上限 30 分钟 |
| 前端模块矩阵 | make check/lint/test/build MODULE=选中前端 | job 上限 20 分钟 |
| 真实依赖 | make integration SCOPE=business/observe | 快检查成功后，按既有选择器执行；30/25 分钟 |
| 浏览器 | make e2e SCOPE=business/observe | 快检查成功后，按既有选择器执行；每 scope 25 分钟 |
| Compose | make verify-compose | 快检查成功后，保留当前全栈闭包；60 分钟 |
| 交付 workflow | 既有 make package PLATFORM=linux/amd64,linux/arm64 RUNTIME=1 PROMOTE=1 | 继续手动触发及既有发布许可；120 分钟 |
| 汇总 | 对照选择结果、模块清单、各 job 状态及报告 revision | 必需检查缺失、失败、取消、超时或意外跳过都失败 |

job 上限是本次工程方案的预算，不缩短已有产品案例所要求的测量窗口。
构建/安装等待和取消清理必须在 job 截止前保留收尾时间；具体时间由分方案预检登记。
CI 的本地真实入口、环境参数和选择范围必须实际等价；仅同名不算一致。
如业务 integration 的 services 准备与原生入口已有独有步骤，逐项保留或迁移后再删除。

根 Makefile 新增 `make list-go-modules` 与 `make list-npm-modules`，分别逐行输出 15 个 Go
模块与两个前端；两个目标不安装依赖、不构建工具。ci 仍负责选择并输出 JSON，保持已有
`checks`/`reasons` 字段，并新增选中的 `go_modules`/`npm_modules` 数组；devenv 只是消费它。
依赖、权限、共享模块和未知产品路径的选择规则保持有效，具体变化由行为测试证明。
模块矩阵收到空集合时按明确条件跳过；汇总区分“无需执行”和“本应执行却被跳过”。

每个真实 job 在失败/成功结束后收集已生成的白名单诊断，再上传对应产物。
自动 PR 继续等待总质量结论和完成元数据；update 的 merge commit/保留分支规则保持有效。
不改变 GitHub 身份校验、token 授权或远端自动合并权限。

## 8. 建议实施拆分与预算

这是一个整体改造，建议按 A → B → C → D → E → F → G 顺序交付。
字母只是本文的拆分标识，不是已经分配的实现批次或开发分支。

| 单元 | 单一交付结果 | 预计活跃时间 | 累计上限 | 固定完成门禁 |
| --- | --- | --- | --- | --- |
| A | 工具检查、准备入口与工程约定 | 80 分钟 | 180 分钟 | doctor/bootstrap 的成功、版本不符、缺失工具与下载身份失败；devtools check/test/race；治理检查 |
| B | 三种入口共用锁文件驱动的前端依赖准备 | 100 分钟 | 180 分钟 | 首装/复用/锁变化/失败不写标记/并发互斥；两个前端 check/test/build；实际 dev 与两个 e2e scope |
| C | 15 个 Go 模块接入额外缺陷检查 | 100 分钟 | 180 分钟 | 配置校验；完整模块扫描与实际存量记录；新增错误 fixture 非零退出；既有 check/test/race |
| D | 两个前端接入同一套缺陷规则 | 100 分钟 | 180 分钟 | Vue/TS parser 兼容；违规 fixture；新增/存量分类；两前端 check/test/build 与两个 e2e scope |
| E | 三类真实验证入口保留诊断且正常清理 | 120 分钟 | 180 分钟 | collector 脱敏与路径边界；主失败/清理失败分别保留；实际 integration、e2e、Compose 的代表性失败 |
| F | 本地 verify 与 CI 复用同一选择和结果合同 | 120 分钟 | 180 分钟 | diff/矩阵/失败传播行为测试；文档、单模块、共享模块、权限、部署、未知路径选择；实际远端 CI 与 artifact |
| G | 干净环境中的整条工程与交付流程验收 | 120 分钟 | 180 分钟 | 下表 G01～G08 全部满足，记录实际能力和限制 |

预计合计 740 分钟（12 小时 20 分），累计上限合计 1260 分钟（21 小时）。
这是估算，不是已测耗时；发现某单元不能装进上限时，在它开始前调整拆分与正式总方案，
不能沿用一个大文件反复重试。A～F 每批仍须完成正式分方案规定的 CI/完成元数据检查。

每个单元上限的阶段分配如下；正式分方案需按进入预检修正，但不得增加未获授权的上限：

| 单元 | 发现/实现 | 直接检查 | 构建/准备 | 真实/CI 验证 | 证据/清理 | 机动 | 总计 |
| --- | --- | --- | --- | --- | --- | --- | --- |
| A | 65 | 20 | 15 | 55 | 10 | 15 | 180 |
| B | 55 | 20 | 15 | 65 | 10 | 15 | 180 |
| C | 55 | 25 | 15 | 60 | 10 | 15 | 180 |
| D | 55 | 20 | 20 | 60 | 10 | 15 | 180 |
| E | 50 | 25 | 15 | 65 | 15 | 10 | 180 |
| F | 45 | 25 | 15 | 70 | 15 | 10 | 180 |
| G | 10 | 15 | 30 | 90 | 20 | 15 | 180 |

本方案没有不可缩短的新容量/长时测量窗口；required experiment duration 为 0，
但真实依赖和浏览器门禁仍必须执行，不能由静态/unit fixture 替代。
每个单元在累计 90/144 分钟报告进度。首次基础设施失败停止整组，
同一原因最多两次诊断，每次最多 10 分钟，全部计入原预算。
新候选、另一工作目录、另一 agent turn 或一次重试均不重置累计时间。
到上限或诊断停点，保留 passed/failed/not_started、原始诊断及归属清理，不作完成提交；
接续须形成具体剩余预算并由用户明确指示。方案写入不自动启动实施或恢复暂停验收。

## 9. 最终完成条件与验证命令

以下命令为拟新增入口与现有入口的组合，只能在 A～F 完成、正式方案进入主线后执行。
选择一个已提交 revision 的干净独立 checkout；所有报告绑定同一源码身份与工具/config 输入。
阶段总验收的改动基线固定使用本次改造前的 `0610f3f`，覆盖整个工程改造；不能在全部合入
主线后以空 diff 的 verify 结果冒充阶段总验收。日常开发仍对最新主线选择必要检查。

| case_id | 验证操作 | 必须得到的结果 |
| --- | --- | --- |
| G01 | make doctor → make bootstrap → 再次 bootstrap | 前置条件明确；首次准备成功；输入未变时不重复安装；工具差异如实记录 |
| G02 | 在受控副本更改一个前端锁文件，再调用该模块入口 | 自动重装，仅影响该前端；安装失败时不会继续测试 |
| G03 | make verify BASE_REF=实际主线 PLAN_ONLY=1；分别输入代表性 diff | 17 模块完整，权限与共享依赖消费者不漏选，文档-only 不启动产品环境 |
| G04 | make verify BASE_REF=0610f3f | 整个改造涉及的步骤都真实完成；与同 revision CI 对照必要范围；无新增 lint 告警 |
| G05 | make integration SCOPE=business/observe；make e2e SCOPE=business/observe；make verify-compose | 实际业务/观测与浏览器链路通过；只清理本次拥有的资源 |
| G06 | 端口占用、明确的测试失败、清理失败；真实范围内各选最小案例 | 非零退出，主失败与清理结果不丢失；报告能指导复现；外部资源不受影响 |
| G07 | 冻结候选上 make package PLATFORM=linux/amd64 RUNTIME=1；按既有生命周期入口隔离安装 | manifest/Bundle/验证结果身份一致，工程改造不破坏现有交付合同 |
| G08 | 下载同一 CI revision 的白名单诊断并核验 | 无私有环境或凭据；必需检查不缺失；报告与真实退出结果相符 |

G04 和 G05 中同一候选、相同输入及环境的成功检查不重复执行；若 verify 已实际执行
某项，使用其记录。候选或相关配置改变后只重验受影响检查；不得把别的候选回执换绑。
G06 的产品测试用例可复用既有失败 fixture；只有新诊断/清理边界未覆盖时才补用例。
G07 是实际安装与启停验证，不据此宣称完整真实备份恢复或长期稳定性。

每次构建前按实际变动登记影响：devtools 改动对应宿主 devenv；acceptance Go 改动对应宿主
验收程序；前端 package/lock 对应前端及 acceptance 镜像输入；Playwright 配置对应 acceptance
镜像与原生浏览器；CI 配置对应调度与报告合同。模块 Makefile 若被 Docker COPY 纳入上下文，
也登记其缓存影响。复用未变的构建缓存，不重绑旧候选回执；正式版本元数据的影响另按根规则登记。

所有条目通过才登记工程改造完成。阶段完成版本、候选版本与 revision 的对应关系须在正式
Phase 总方案中分配；日志只写执行过的命令和结果，已退役历史证据保持原样。

## 10. 开工前仍须核对的参数

- npm 标准版本与固定 Node 分发的实际兼容性；核对失败时在 A 的进入预检修订工具值。
- ESLint/Vue/TypeScript parser 的兼容版本；D 必须固定实际验证过的组合。
- 首次完整 lint 的实际告警数量与位置；当前没有扫描结果，不能声称零告警或已修复。
- 新 job 矩阵与仓库实际分支保护的关联；如远端保护引用旧 job 名，先给出兼容方案。
- 干净环境构建、CI 排队与 package 的实际成本；开始昂贵命令前检查剩余预算。

这些是明确的进入预检，不是允许无限诊断的空白范围。无法在边界内解决时按预算规则停止。

## 11. 参考接口

- [golangci-lint 官方安装](https://golangci-lint.run/docs/welcome/install/local/)：使用发布二进制并固定版本；本文不在产品 go.mod 中安装 linter。
- [golangci-lint CLI](https://golangci-lint.run/docs/configuration/cli/)：共同祖先与报告输出参数。
- [ESLint 配置](https://eslint.org/docs/latest/use/configure/configuration-files)、[Vue 插件](https://eslint.vuejs.org/user-guide/)、[TypeScript parser](https://typescript-eslint.io/getting-started/)：前端 flat config 与兼容性预检的接口依据。
- [工程设计复核参考](https://github.com/trpc-group/trpc-agent-go/blob/main/AGENTS.md)：公开合同、资源所有权与按风险选择验证；适用范围服从 GoPulse 已采用目标和仓库规则。

本文只完成方案制定。尚未新增命令、安装 lint 工具、运行改造后的验证或改变产品完成版本。
