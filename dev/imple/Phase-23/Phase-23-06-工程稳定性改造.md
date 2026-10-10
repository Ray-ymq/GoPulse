# Phase-23-06：工程稳定性改造

> 状态：已分配，待实施。规划日期：2026-10-10。
> 目标版本：`2.5.6`；开发分支：`develop/2.5.6`。分配来源为[Phase 23 总方案](Phase-23-总实施方案.md#2-正式分配与进入顺序)。
> 单批、单实施文件、单开发分支、单完成版本。H→A→B→C→D→E→F→G 是文件内实施段，全部完成后统一收口。

## 1. 交付结果与已有覆盖

完整交付[工程稳定性改造设计](../../design/GoPulse-工程稳定性改造方案.md)的可执行部分：
文档检查、工具检查与准备、前端依赖失效判断、Go/前端 lint、失败诊断、共同范围选择与
CI 汇总，以及干净环境的真实开发、测试和交付验收。此前完成的规则与默认清理原则直接复用。

已有能力与本批缺口：

| 对象 | 复用位置 | 本批改变 |
| --- | --- | --- |
| 模块与 Make 调度 | [根 Makefile](../../../Makefile)、各模块 Makefile | 导出唯一模块清单，增加 docs-check/doctor/bootstrap/lint/verify |
| 开发与测试隔离 | [devenv](../../../devtools/cmd/devenv/main.go)、workspace/proc/digest、devrun/testenv | 复用归属和退出机制，补依赖失效判断与诊断保存 |
| 前端依赖 | [开发安装判断](../../../devtools/internal/devrun/applications.go)、[浏览器安装判断](../../../devtools/internal/testenv/e2e.go)、两前端 Makefile | 三处目前只看 node_modules 是否存在，统一改为输入摘要与安装锁 |
| 质量与 CI | [范围选择器](../../../ci/quality_scope.py)、[质量 workflow](../../../.github/workflows/quality-gates.yml) | 共用清单、选择结果与必需步骤；补 lint、文档门禁和失败汇总 |
| 真实验收与交付 | [acceptance Session](../../../acceptance/internal/harness/session.go)、[原生入口](../../validation/local-development-tests.md)、[Bundle](../../../deploy/release/README.md) | 复用场景、receipt 与清理；先保存报告再清理归属资源 |

不新增运行模块、全仓证据索引或另一套环境管理器；不重建已退役矩阵。
业务 API、权限、消息、公开验收 schema、交付构建基镜像及三份冻结原文沿用既有合同。
发现范围外产品缺陷时保留复现，先修订本分方案的精确文件与回归范围，再处理。

## 2. 进入、分支与候选

1. 本总/分方案及设计映射已进入主远端 main；Phase-23-05 已完成，进入基线的产品 VERSION 为 `2.5.5`。
2. 开工 fetch 主远端，复核本批唯一分配和已有分支/PR；从最新 main 创建 `develop/2.5.6`。
   已推送身份发生冲突时按根规则协调，不复用已完成分支、不自行重编号。
3. 普通范围选择使用实际共同祖先；最终完整改造的范围基线固定为 `0610f3f`，
   覆盖规则、入口与本批实现。若其不是执行候选的祖先，停止并修订基线，不以空 diff 代替。
4. H～F 的实现可以逐段提交；VERSION 保持已完成值，进度写实际同名日志，不提前打开 PR。
   G 使用已提交 revision 的干净独立 checkout，冻结配置、工具、环境和制品身份。
5. 全部固定门禁通过后，同一完成提交同步 `2.5.6` 元数据、能力状态与同名日志。
   元数据若影响已冻结的应用、验证或交付输入，重验受影响集合；原回执保留实际候选身份，不能换绑。

本次规划不创建开发分支或完成日志。实际实施时使用
`dev/logs/Phase-23/Phase-23-06-工程稳定性改造.md`，不拆成多个段日志或提前制造完成记录。

## 3. 接口与行为合同

接口细节沿用设计 §3～4，本文件定义执行范围、门禁与完成条件。以下接口均待实现：

| 入口 | 必须得到的行为 |
| --- | --- |
| `make list-go-modules` / `make list-npm-modules` | 每行一个模块，无安装、测试或容器副作用；清单只读取根 Makefile |
| `make docs-check BASE_REF=<ref>` | 本地引用/锚点、实际命令、文档归属及冻结 diff 检查；无前端安装或产品环境 |
| `make doctor` | 输出实际版本、必需功能与缺失条件；不改全局工具或启动业务 |
| `make bootstrap [MODULE=frontend]` | 校验固定版本与 SHA256，准备项目内 lint、应用依赖和锁定的 Chromium；输入有效则复用 |
| `make lint MODULE=<module> [BASE_REF=<ref>]` | 完整扫描；无 BASE_REF 时告警非零，有 BASE_REF 时仅新增告警阻断，存量报告真实保留 |
| `make lint-all` | 扫描所有模块；任何告警均非零，不把存量当零告警 |
| `make verify BASE_REF=<ref> [PLAN_ONLY=1]` | 共用选择器的模块、必需步骤和原因；PLAN_ONLY 只输出计划，实际执行缺失、失败或清理失败均非零 |
| `make verify MODULE=<module>` | 只做指定模块 check/lint/test/race/build；前端无 race，完整 lint 的告警仍非零 |

MODULE 与 BASE_REF 在 verify 中互斥；ref 缺失明确失败。共同祖先 diff 必须涵盖删除、重命名、
多提交和主线推进，本地再纳入 tracked 工作区 diff 与模块内新增源码，不漏未提交改变。
保持现有 checks/reasons 的调用兼容；未知产品路径仍保守选全量，不能因新输出而漏掉权限、
共享消费者、部署或 integration/browser 检查。Go/前端仅启用设计规定的首批规则。

## 4. 文件内实施顺序

每段先过最低有效层级的直接检查，再进入下一段。真实系统与远端组合门禁集中在 G；
段内成功不产生独立完成版本，也不替代 G。复用仍有效的检查，不为段切换重复完整构建。

| 段 | 实现结果 | 直接门禁与进入下一段的条件 |
| --- | --- | --- |
| H | devenv 文档检查；职责由目录、导航与必要配置判断，不逐文件贴标签；历史与冻结边界保持 | 正常/缺失链接和锚点、重命名、代码块、错模块/脚本、冻结 diff fixture；devtools check/test/race；docs-only 真实 CI 在 G 验证 |
| A | doctor/bootstrap；工具版本与下载摘要锁定，项目内安装，不自动安装系统库 | 工具缺失/不兼容/摘要错误实际非零；成功与复用；核对设计调查版本、npm/Node 兼容和可下载身份后写 toolchains 配置 |
| B | frontenddeps 共用摘要、成功标记与每工作区/应用安装锁，接入 devrun、e2e 和两前端 Make | 首装/复用/lock 与工具变化/失败无成功标记/互斥；两应用 check/test/build；普通应用命令只调用轻量依赖准备，不安装整套工具 |
| C | Go lint：staticcheck、errcheck、unused；保留 gofmt/vet 与 integration tags 的既有覆盖 | 15 个 Go 模块完整扫描；新增违规非零、存量报告完整、共同祖先选择正确；配置不整目录排除或扩大基线 |
| D | 两前端 ESLint，共享纯规则数据，各应用加载本地插件，固定 parser 与依赖版本 | Vue/TypeScript parser、五项首批规则、新增/存量分类 fixture；lock 变化后两应用实际 npm ci；check/test/build |
| E | 工程诊断摘要与私有原始材料；先捕获错误，再停进程和清理；报告留存有界 | 参数/路径/脱敏/取消/超时/主失败与清理失败；容器报告在删除前复制；过期普通报告可清理，活动/接续/正式依据及外部目录受保护 |
| F | 模块清单、范围 JSON、本地 verify、CI 矩阵与结果汇总；缓存读取同一清单 | docs-only/单模块/共享/权限/部署/未知路径、工作区与新增源码、缺失/失败/取消/跳过、revision 漂移 fixture；治理回归与 workflow 检查 |
| G | 同候选干净 checkout 的组合验收与同 revision CI；核验发布报告并完成文档 | §6 全部通过；设计 G01～G09 全部有真实对应结果，随后统一收口 |

### 4.1 诊断期限、容量与归属

工程摘要 schema 使用 `gopulse.engineering-run.v1`。原始材料在
`.run/private-reports/<run_id>`（0700/0600），发布副本在 `.run/reports/<run_id>`。
成功只生成必要 summary.json/steps.json，失败按需补 errors.txt/reproduce.txt。
发布白名单总量 20 MiB、单份摘录 2 MiB；截断只作用于副本并说明。

普通成功报告保留 7 日、普通失败报告保留 30 日；同一工作区两类报告目录合计上限 512 MiB。
只清理已结束、已到期且无后续用途的本工作区普通报告；当前运行、仍需诊断/接续或支撑正式
结论的材料不自动删除。用途或归属不明则不删并报告原因，禁止跟随符号链接越出归属根。
空间不足先清理符合条件的报告，仍不足明确失败；不能覆盖或截断仍需保留的原始依据。
正式结论材料先存入合同约定的可靠位置，结论保存定位信息与校验摘要，不仅依赖 CI artifact。

凭据按本次真实值脱敏；Cookie/token、环境原文、浏览器 trace/HTML/截图不自动发布。
验收 Session/receipt 保持原 schema；devtools 与 acceptance 的 internal 包不互相导入。
只清理本次拥有的进程、容器、网络、卷和文件，不使用全局 prune。

### 4.2 CI 与真实 docs-only 验证

既有 push 质量门禁仍以主线共同祖先为输入，维持身份/token 校验、completion_ready、
update merge/保留分支与开发 squash 策略；保留已有必需 job 的名称和权限边界。
Go/前端、integration、e2e、Compose、release candidate 的时间上限沿用设计 §4 F。

在同一开发分支的产品实现候选之后提交必要的 docs-only 文档同步；质量 workflow 增加
不含 PR/合并动作的手动调用，显式指定该产品候选为 BASE_REF，真实验证文档差异只运行
治理与 docs-check。记录 head/base 与实际 job；该调用不替代对主线的完整 push CI。
受控失败报告与预期失败场景分别记录，不用 continue-on-error 吞掉必需正常检查的失败。

## 5. 允许文件与复用边界

| 范围 | 允许新增/修改文件 |
| --- | --- |
| 命令与配置 | 根 Makefile；devtools/cmd/devenv/{main,main_test}.go；devtools/toolchains.json；devtools/config/documents.json、eslint-rules.mjs |
| 质量实现 | devtools/internal/quality/{docs,doctor,bootstrap,lint,verify}.go 及对应测试与 testdata；必要的 devtools/go.mod/go.sum 依赖登记 |
| 前端依赖 | devtools/internal/frontenddeps/deps.go 及测试；devtools/internal/devrun/{applications,devrun_test}.go；devtools/internal/testenv/e2e.go 及对应测试；两前端 Makefile |
| lint 配置 | .golangci.yml；根清单中 15 个 Go 模块的 Makefile；两前端 eslint.config.mjs、package.json/package-lock.json |
| 诊断与进程 | devtools/internal/diagnostics/report.go 及对应测试/testdata；devtools/internal/testenv/{testenv,processes,e2e}.go 及测试；devtools/internal/proc/proc.go 及测试；frontend/playwright.config.ts |
| 验收适配 | acceptance/internal/harness/{session,session_test}.go；acceptance/internal/scenario/{compose,suites,lifecycle}.go 及对应测试；acceptance/cmd/gopulse-acceptance/{main,main_test}.go |
| CI | ci/{quality_scope,test_quality_scope,validate_quality_result,test_validate_quality_result,test_auto_pr_workflow}.py；.github/workflows/{quality-gates,cache-warm,auto-pr-merge,release-candidate}.yml |
| 正文与完成登记 | 根 AGENTS.md/README.md；dev/README.md；dev/validation/local-development-tests.md；本总/分方案与工程设计；dev/status/capability-status.md；同名实际日志 |
| 完成版本元数据 | VERSION、.env.example、两前端 package.json/package-lock.json、deploy/runtime-contracts.json 的版本字段；只在成功收口同步 |

新增辅助函数放在对应现有包；复用 proc/workspace/digest、Session/Cleanup/receipt 与已有场景。
不得扩展到其他模块的产品源码修复、重写治理 Python、升级交付基镜像或改验收合同。
新增工具依赖须有实际用途、固定版本与对应验证；不为测试框架本身增加无关依赖。

## 6. 固定完成门禁

门禁全部完成，且结果绑定实际候选、输入与环境；阶段切换不降低门禁。
设计[整体完成条件](../../design/GoPulse-工程稳定性改造方案.md#6-整体完成条件)中的 G01～G09
是必须覆盖的验收行为，以下是本批固定执行集合。

| 门禁 | 固定命令/场景 | 通过条件 |
| --- | --- | --- |
| 治理与工具 | `python3 -m unittest discover -s ci -p 'test_*.py'`；`python3 ci/validate_versions.py`；分支校验 development/completion；workflow 静态检查 | 规则、元数据、身份和本批分配一致；开发中允许完成元数据未就绪，收口必须 completion 通过 |
| 全模块 | `make check-all`；按 `make list-go-modules` 逐模块 test/race；按 `make list-npm-modules` 逐应用 test/build | 15 个 Go、2 个前端实际覆盖；新工具行为与调用方回归通过，未执行不能记通过 |
| 工程行为 | doctor → bootstrap → bootstrap；受控锁变更与失败；docs-check；全部模块 lint；`make verify BASE_REF=0610f3f`；单模块与 PLAN_ONLY 场景 | G01～G04/G09 成立；新增 lint 为零，存量报告与 lint-all 的真实非零行为保留，不宣称零存量 |
| 源码开发 | `make deps` → `make dev` → `make stop`；`make dev-observe` → 就绪 → `make stop` | 共用依赖准备真实生效；活动资源只归本次，无残留或外部进程误停 |
| 真实链路 | `make integration SCOPE=business`、`observe`；`make e2e SCOPE=business`、`observe`；`make verify-compose` | G05 成立；现有业务、观测与浏览器独有断言保留，归属清理完成 |
| 失败与报告 | 受控端口冲突、测试失败、清理失败；实际 integration/e2e/Compose 代表失败；同 revision CI 摘要及失败报告下载 | G06/G08 成立；真实非零、主失败与清理分开、复现可定位、发布无秘密、报告留存边界有效 |
| Bundle | `make package PLATFORM=linux/amd64 RUNTIME=1`，本次专属 OUTPUT；按实际 manifest 做隔离安装/启停和卸载 | G07 成立；Bundle/manifest/receipt 同一候选，清理自有 registry/安装资源；不使用 PROMOTE、不声明完整备份恢复或长期稳定性 |
| 远端 CI | 同 revision 权威 push CI；§4.2 的真实 docs-only 调用；代表性范围 fixture | 必需矩阵与真实结果一一对应；缺失、取消、超时或意外跳过均失败，缓存和手动文档调用不能代替完整门禁 |

同候选、相关代码/配置/依赖/身份/环境未变的成功结果继续有效，最终 verify 与单列真实门禁
重合时只执行一次并引用实际结果。冷启动、受控副本与失败案例是不同场景，不能互相替代。
分支保护、远端服务或硬件不满足时如实保留未完成项，不能以本地替代结果声明 CI/交付完成。

## 7. 成本、窗口与停止

按[成本与接续](../../rules/implementation-execution-budget.md)登记单文件长时实施：
预计活跃时间 1,200 分钟（20 小时），累计上限 1,800 分钟（30 小时），均为未测估算。
依据是八段共同改动 devenv/Make/CI，以及 17 个模块扫描、前端冷准备、真实链路、失败报告、
远端矩阵和 Bundle 安装；比设计初估增加组合验收与报告留存实现余量，不套用普通文件默认上限。

| 段 | 预计活跃 / 累计上限（分钟） | 主要成本与窗口 |
| --- | --- | --- |
| H | 130 / 200 | 文档解析与 fixture、CLI/选择器接入、devtools 直接检查 |
| A | 90 / 140 | 工具/版本预检、下载身份、冷准备与复用验证 |
| B | 140 / 200 | 共享摘要/锁、三处接入、两应用检查与构建 |
| C | 130 / 200 | 15 模块 lint 配置、首次扫描、共同祖先与新增告警验证 |
| D | 130 / 200 | 两应用 parser/规则、lock 与 npm ci、分类验证 |
| E | 190 / 280 | 私有/公开报告、失败捕获、期限/容量/归属行为与最小真实失败 |
| F | 160 / 240 | 共同清单/选择器、verify、CI/job 汇总、手动文档调用与治理 |
| G | 230 / 340 | 干净 checkout 准备、同候选真实链路/CI/Bundle、核验报告与清理 |

累计上限包括预检、实现、检查、构建、下载与依赖等待、CI、诊断和清理；每段预留至少
20 分钟停止/清理余量，A 预留 10 分钟。真实验收优先复用有效镜像与输入，不缩短既有测量窗口。
新增容量/持续运行实验数量为 0，required experiment duration 为 0；真实门禁仍须执行。

从本批第一次实施预检开始累计；跨会话、候选修订或重试不重置。每段上限之和即文件总上限，
段末核对已耗、剩余和下一段准备/执行/清理所需成本；任一段触顶不借用后一段预算。
基础设施首次失败停整组；同原因最多两次、每次 10 分钟最小诊断，计入该段。
未证明原因不重启完整矩阵；某步成功后只因相关输入改变才重跑。

触顶、诊断无法推进或用户停止时，保存候选、实际通过/失败/未开始、必要诊断和剩余工作，
清理本次归属资源后停止。本批仍保持同一文件、分支和累计成本；接续需要具体剩余方案与
用户明确指令，不自动拆批、追加版本、延长上限或从头重跑。

## 8. 统一收口

H～G 全部交付、§6 全部通过，才更新完成事实：

- 文档只登记已实现入口与真实边界；能力状态记录本批工程能力，不扩大容量/恢复/生产支持声明。
- 同名日志只写实际交付、必要验证结论、偏差与限制，引用实际 CI 和必要证据，不逐条抄成功输出。
- `VERSION` 与相关版本元数据同步 `2.5.6`，与日志及本批任务文件同一成功提交；不得夹带用户或其他任务改动。
- 完成后的 PR/合并按根规则执行；PR 打开或批次完成后不继续使用该开发分支。

任何必需门禁失败或未执行均保留实际进度，本文件保持未完成，不用规划修改产生完成结论。
