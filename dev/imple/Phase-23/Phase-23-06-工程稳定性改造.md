# Phase-23-06：工程稳定性改造

> 实际状态：已完成。目标版本 `2.5.6`，开发分支 `develop/2.5.6`，固定基线 `0610f3f`。
> 收口提交：`ed4ce5e`（版本元数据）、`b68a85b`（候选与收口日志）；主线合并提交 `d2a7fe2`，PR `#242`。
> 最终远端质量 workflow：`38123004454`，44 个 job 全部成功。
> 目标版本：`2.5.6`；开发分支：`develop/2.5.6`。分配见[Phase 23 总方案](Phase-23-总实施方案.md#2-正式分配与进入顺序)。
> 一个实施文件、一个开发分支、一个完成版本。H→A→B→C→D→E→F→G 是文件内工作顺序。

## 1. 问题与交付

本批落实[工程稳定性设计](../../design/GoPulse-工程稳定性改造方案.md)：让开发者和 Agent 找得到代码与
事实来源，环境和依赖准备可靠，新增缺陷被拦截，本地与 CI 使用相同入口，失败后有材料可查。
根 AGENTS、文档入口与默认清理已经重写；本批检查这些入口是否准确，并随实际命令落地更新正文。

调查基线是 `0610f3f`，当时完成版本为 `2.5.5`。下表是源码核对发现的缺口，不代表已复现全部故障
或已经执行新的产品验收。开工使用最新主线，先确认这些缺口是否仍存在。

| 实际问题 | 源码或正文入口 | 本批交付 |
| --- | --- | --- |
| 开发规则曾以执行制度为主，重复入口和过期状态影响阅读 | [根指南](../../../AGENTS.md)、[工程入口](../../README.md)、[产品文档](../../../docs/README.md) | 检查组件地图、命令和正文归属；有问题的文件可删除重写，更新有效引用 |
| 环境条件分散，完整运行前难以确定缺什么 | [devenv](../../../devtools/cmd/devenv/main.go)、各 go.mod、前端 package/lock | doctor 明确缺失条件，bootstrap 可靠准备项目内工具和依赖 |
| 前端 Make、源码开发、浏览器入口只看 node_modules 是否存在 | [用户端 Make](../../../frontend/Makefile)、[管理端 Make](../../../admin-frontend/Makefile)、[devrun](../../../devtools/internal/devrun/applications.go)、[e2e](../../../devtools/internal/testenv/e2e.go) | 共用锁文件驱动的安装判断，变化后重装，失败后不继续运行 |
| 已有格式、vet 和类型检查，缺少额外缺陷检查 | 各模块 Makefile、两应用 package scripts | Go 与双前端接入少量缺陷规则；新增告警阻断，存量如实呈现 |
| 根 Make 与选择器重复登记模块，本地缺少共同验证入口 | [根 Make](../../../Makefile)、[选择器](../../../ci/quality_scope.py)、[质量 CI](../../../.github/workflows/quality-gates.yml) | 根 Make 提供唯一清单，本地与 CI 共用模块命令和改动选择 |
| 容器和临时目录清理会带走诊断，CI 没有明确的收集入口 | [Session](../../../acceptance/internal/harness/session.go)、[Compose 场景](../../../acceptance/internal/scenario/compose.go)、[测试环境](../../../devtools/internal/testenv/) | 失败材料先保存再清理，必要的脱敏摘录可从 CI 下载 |
| 工程工具改变需要证明现有开发与交付仍可用 | [原生入口](../../validation/local-development-tests.md)、[Bundle](../../../deploy/release/BUNDLE-README.md) | 干净环境中的开发、真实测试与现有交付链通过 |

模块从根 Makefile 读取，当前为 15 个 Go 模块和两个前端。沿用现有业务、权限、消息、验收回执和
交付合同；三份冻结设计保持原路径与原文。范围外产品缺陷记录复现后再修订本分方案，
存量 lint 告警不作为本批全仓修复任务。

## 2. 实施顺序与文件范围

每段先完成直接检查，再进入下一段。段内检查证明具体改变，G 证明组合后的工程入口可用。
已经有效的同输入检查直接复用；过程中发现没有独有用途的旧判断、工具或正文，更新调用方后删除。

### H：开发指南、文档与轻量检查

先检查根 AGENTS 的每个组件入口、已有验证命令和常见陷阱；检查 README、dev/docs 导航、
原生测试说明和直接受影响的组件说明。正文只承担自己的职责：
设计写目标，status 写事实，contracts 写合同，operations/validation 写步骤，imple 写执行要求，
logs 写实际结果，导航给入口。重复或错误正文直接删改，不补转发页、删除清单或历史迁移表。

新增 `ci/check_docs.py`，根 Make 提供 `docs-check`。检查当前导航、开发/使用指南、当前 contracts、
operations/validation 正文以及本次新增或修改的当前文档：

- 本地链接、图片和锚点能定位；识别代码块、带空格路径和中文标题，不执行文档里的命令。
- 当前命令表中的 Make target、MODULE、npm script 确实存在；命令语义和前置条件由人工核对。
- 相对 BASE_REF 的共同祖先，三份冻结原文没有改动；本地检查同时看工作区变更。
- 历史日志、评审、冻结正文和待实现方案不按当前命令扫描；当前导航指向它们的链接仍需有效。

范围按已有目录和入口确定，不增加逐文件分类配置或未来命令白名单，不探测外链。
最低层用正常/断链/错锚点/错模块/冻结 diff 等 fixture 验证；正文是否冗余、事实来源是否正确仍需人工判断。

允许文件：根 AGENTS/README、ci/AGENTS、dev/README、docs/README、直接受影响的当前指南与导航；
`ci/check_docs.py`、`ci/test_check_docs.py`、根 Makefile。历史材料的删除与证据留存遵循根规则。

### A：环境检查与准备

沿用 devenv，新增 doctor/bootstrap；工具要求集中在 `devtools/toolchains.json`。
Go 的支持范围读取 go.mod，Node/npm 核对前端依赖兼容；治理 Python 要求 3.12+。
调查时的 Go 1.26.7、Node 24.20.0、npm 11.19.0 与 golangci-lint 2.14.0 是预检起点，
先核实实际兼容与可下载性，再锁定 CI 检查版本和 lint 的官方下载 SHA256。
交付镜像继续读取原 build-bases lock，不顺带升级。

doctor 检查工具版本、Docker daemon/buildx 和 Compose 必需功能，输出缺什么及下一步。
它不启动业务或安装系统软件；Compose 的实际运行能力由 G 验证。
bootstrap 准备项目内 lint 工具、指定前端依赖和锁定 Playwright 的 Chromium。
工具保存在 .run/tools，校验版本和摘要后复用；安装失败不能留下成功标记。
普通 dev/e2e 只调用所属应用的依赖助手，不先安装整套检查工具。

允许文件：`devtools/toolchains.json`、`devtools/internal/quality/{doctor,bootstrap}.go` 及测试、
devenv CLI、根 Makefile；必要的 devtools 依赖文件。

### B：前端依赖失效判断

新增 `devtools/internal/frontenddeps/deps.go`，把 devrun、e2e 和两个前端 Make 的安装判断接到它。
成功标记绑定 package.json、package-lock.json、实际 Node/npm、OS/架构。
目录或标记缺失、输入摘要变化才执行 npm ci；成功才写标记，失败直接阻断后续步骤。
每工作区、每应用使用安装锁；活动开发进程使用该应用时先正常停止，不能边运行边替换依赖。

允许文件：frontenddeps 及测试、devrun/applications.go、testenv/e2e.go 及相关测试、两前端 Makefile。
三处旧的目录存在性判断在接入后删除。

### C、D：Go 与双前端缺陷检查

Go 首批启用 staticcheck、errcheck、unused，根 `.golangci.yml` 配置版本 2；扫描依赖只读。
保留现有 gofmt/vet、普通测试和真实 integration tags 覆盖。
前端首批启用 no-debugger、no-async-promise-executor、no-constant-condition（允许明确无限循环）、
vue/no-mutating-props、vue/no-side-effects-in-computed-properties；保留现有类型检查。
两应用本地加载 ESLint/Vue/TypeScript 插件，共用 `devtools/config/eslint-rules.mjs` 的纯规则数据，
先验证 parser 兼容，再固定依赖版本与 lock。

根 `make lint MODULE=...` 调用小型 `ci/lint.py` 包装原生工具，在对应模块目录扫描。
Go 固定根配置路径，前端复用各自 npm lint；共享调度不在 15 个 Go Makefile 重抄。
完整扫描保留工具原生输出；有 BASE_REF 时另按共同祖先 diff 判断新增，原始扫描错误始终阻断。
Go 可用原生增量参数，完整与增量扫描共用每模块 10 分钟上限；前端按 ESLint 文件/行报告分类。
本地 tracked 工作区改变和新增源码也纳入，不能只看 HEAD。
存量、新增数量和原始退出码直接打印并注明报告路径，不新增 lint schema 或报告层级。

允许文件：`.golangci.yml`、`ci/lint.py` 及行为测试、两前端 ESLint 配置与 package/lock、
共享规则数据、根 Makefile。源码告警用受控 fixture 验证，不为清零重写产品源码。

### E：失败后留得下诊断

扩展现有 proc/testenv 和 acceptance Session，不改变已有 receipt schema 或成功判据。
失败时保存实际命令、原始退出状态、必要日志、候选/环境身份和可执行的复现入口；
随后停止本次进程、清理归属资源，单独记录清理失败，保留主失败。
Compose 浏览器报告在容器删除前复制；给本次容器明确身份，复制后定向删除，不能长期 keep 资源。

材料存入已有工作区身份下的独立失败目录，目录 0700、原始文件 0600，控制路径和符号链接边界。
成功运行复用终端、CI 和既有回执；doctor/bootstrap/dev/stop 不强制生产第二套工程摘要。
普通失败诊断默认保留 30 日；排障/接续仍需要或支撑正式结论的材料按既有用途规则留存。
确认无后续用途且到期的材料可随本工作区清理处理，不新增引用扫描、配额索引或后台清理器。

CI 只上传实际核验过的 failure.txt、文本日志摘录和复现命令。
复用诊断回归用例中的受控失败子进程，在同次成功 CI 生成并上传一个安全摘录，验证可下载和脱敏；
用例必须核对真实非零退出，不用 continue-on-error 放过本应成功的检查。
副本总量 20 MiB，单份文本摘录 2 MiB；注明摘录范围，原始材料不因此改写。
使用本次实际凭据脱敏，复现命令从已知 Make 参数生成，不复制秘密参数；原始环境、Cookie/token、
trace/HTML/截图不自动上传。
收集或清理失败如实记录，不覆盖原失败；不得把已有失败包装成成功，也不因诊断目录配额改变成功测试的结果。

允许文件：`devtools/internal/diagnostics/` 及测试、proc/testenv 的直接调用点、
前端 Playwright 配置；acceptance 的 Session、直接使用它的场景及相关测试和 CLI。
复用现有 workspace/进程归属，不为跨模块报告另建公共 schema。

### F：同一份 Make 命令、选择器与 CI

根 Make 提供无副作用的 list-go-modules/list-npm-modules；选择器读取清单，移除独立模块集合。
保留现有 checks/reasons 和路径选择行为，补 docs 与选中模块数组。
本地 verify 直接消费选择结果，调用现有 Make 目标；模块组合统一写在根 Make 的 verify MODULE 模式，
CI 的模块 job 调用同一组合。不在 JSON 里登记 argv、步骤协议或可执行计划。

| 改动范围 | 必须选择 |
| --- | --- |
| 当前文档与导航 | 治理、docs；无产品环境 |
| ci/check_docs.py 与其测试 | 治理、docs；无产品环境 |
| .golangci.yml、ci/lint.py | 真实消费者；Go 配置只选 Go，公用包装同时覆盖两前端 |
| 共享 ESLint 规则 | 两前端与两 e2e；保持直接相关 devtools 覆盖 |
| toolchains 配置、frontenddeps 或环境助手 | 工具/依赖真实消费者、对应模块及受影响 integration/e2e |
| 权限、共享模块、部署、工作流、VERSION 与未知产品路径 | 保持现有保守覆盖，不能借新输出缩小 |

保留已有 job ID、保护名称、services/迁移/搜索准备和权限/token 逻辑。
文档 job 只需 Python；模块 job 直接运行 Make，集成/浏览器/Compose 沿用真实入口。
CI 模块组合显式传入 `BASE_REF=origin/main`；涉及共同祖先的 job 使用 `fetch-depth: 0` 并取回基线。
提取 native-acceptance 已有 Compose 静态断言到 `ci/check_compose.py`，
本地与 CI 共用 `make check-compose`，它不启动真实栈。

增加一个 quality-result 汇总及小型 `ci/validate_quality_result.py`：
汇总 job 使用 always 条件，只对照本次选择和 GitHub needs 的真实 job 结果：
治理始终必需，docs 按选择运行；`run_product_checks=false` 时不要求产品 job，
其余被选中的 job 缺失、失败、取消或意外跳过即失败，未选中 job 可正常跳过。
job 内的步骤是否成功由同一 Make 命令的退出状态保证，不另外上传逐步骤证明。
completion_ready 继续使用现有版本/同名日志校验；自动 PR 仍必须等待整个质量 workflow 成功，
不解析实施日志状态，不加入本批特有的候选阶段，不新增 docs-only 子工作流。
文档-only 的选择与条件通过行为测试证明；新文档 job 在本批真实 push CI 中运行，确认自身无产品环境。

cache-warm 从根清单构造矩阵，保持 main 限制。lint 缓存键绑定 OS/架构、版本和下载摘要，
缓存命中后重新核验；冷下载计入 job 上限。main 专属预热未运行时注明限制。
job 上限：文档 5、Go 30、前端 20、integration business/observe 30/25、e2e 25/30、Compose 60 分钟，
包括该 job 的冷准备与清理，不缩短已有真实案例的断言和窗口。
release-candidate 不在本批修改或触发范围。

允许文件：根 Makefile、`devtools/internal/quality/verify.go` 及测试、devenv CLI；
quality_scope、check_compose、validate_quality_result 及对应 ci 测试；
quality-gates、cache-warm、auto-pr-merge 和既有 workflow 测试。现有治理 Python 规则继续复用。

收口时允许同步 VERSION、.env.example、两前端 package/lock 和 deploy/runtime-contracts.json 的版本字段，
维护本总/分方案、工程设计、能力状态与同名实际日志；只改与实际交付直接相关的内容。

## 3. 命令与失败行为

下列新增入口全部待实现；只能在实现后用于验收。

| 入口 | 具体行为 |
| --- | --- |
| `make list-go-modules` / `make list-npm-modules` | 从根 Make 每行输出一模块，无编译、安装或测试 |
| `make docs-check BASE_REF=<ref>` | 检查 H 的当前正文和冻结 diff；问题非零，无产品环境 |
| `make doctor` | 输出实际工具和缺失条件；必需条件不满足非零 |
| `make bootstrap [MODULE=frontend] [TOOLS_ONLY=1]` | MODULE 只允许空/frontend/admin-frontend；空准备两应用及检查工具，指定值准备该应用及检查工具；TOOLS_ONLY 仅准备 Go lint，与 MODULE 互斥 |
| `make lint MODULE=<module> [BASE_REF=<ref>]` | 无 ref 严格检查全部告警；有 ref 仅新增告警阻断，工具/配置错误始终阻断 |
| `make lint-all` | 全模块严格扫描，有任何告警非零，不允许用 ref 放宽 |
| `make verify BASE_REF=<ref> [BRANCH=<branch>] [PLAN_ONLY=1]` | 共同祖先加本地工作区选择范围，运行治理、文档、选中模块及真实检查；BRANCH 供 detached checkout 的治理校验；PLAN_ONLY 直接打印选择器 JSON，无编译/安装/运行目录副作用 |
| `make verify MODULE=<module> [BASE_REF=<ref>]` | 只做该模块 check/lint/test/race/build，前端无 race；ref 只决定 lint 新增判据，不扩大成跨模块验收；CI 使用此形式 |
| `make check-compose` | 沿用全部现有 Compose 静态断言，只渲染、不启动栈 |

未知模块、无效 ref 和安装失败明确非零。BASE_REF 模式保留 changed_files/checks/reasons，
只补选中模块等实际需要的信息，不给 JSON 定义新的版本协议。
devenv 的现有失败传播、取消和归属清理继续有效；诊断保留原子进程退出状态，
不能用 GNU Make 的外层退出码代替原始原因。选中的真实检查不能由普通模块测试替代。

## 4. 直接检查与固定验收

### 直接检查

改变的助手先在最低有效层级验证。下面是必须覆盖的行为，不新建执行器或逐项报告格式。

| 改变 | 必须证明 |
| --- | --- |
| 指南与文档检查 | 组件/命令能定位；正常链接、断链、锚点、代码块、错模块和冻结 diff 得到正确结果 |
| 工具准备 | 缺失/不兼容/错摘要实际失败；冷准备成功，输入未变第二次不下载重装 |
| 前端依赖 | 首装、复用、lock/工具变化、失败不写标记、互斥和活动进程保护；三种入口都使用新判断 |
| lint | 首次 17 模块扫描如实记录；每项规则有真实违规 fixture；新增/存量、未提交源码和扫描错误正确区分 |
| 诊断 | 真正的失败子进程、输出捕获、取消、清理失败、脱敏和路径归属；复制发生在容器删除之前 |
| verify 与 CI | 文档/单模块/共享/权限/部署/未知路径、多提交/主线推进/删除重命名/工作区选择；PLAN_ONLY 无副作用；缺失/失败/意外跳过不能通过汇总 |

实现时运行所属模块与治理的直接检查；最终候选执行下表。
范围基线固定为 `0610f3f`，必须是候选祖先，不能合入后用空 diff 冒充整批验收。
冻结实际已提交 revision、工具、配置和环境；独立 checkout 使用真实分支身份校验，detached 时显式提供
已分配的 `BRANCH=develop/2.5.6`，不能从 detached HEAD 猜测分支。

### G：组合验收

| 门禁 | 固定操作 | 验收结果 |
| --- | --- | --- |
| 环境与复用 | doctor → bootstrap → 再次 bootstrap | 前置条件明确，真实准备成功，输入未变不重装 |
| 依赖回归 | 受控副本改变一个前端 lock，分别调用 Make、devrun、e2e 的准备路径 | 只重装该应用；失败后不启动后续测试 |
| 模块与选择 | `make verify BASE_REF=0610f3f`；未选中模块用 `make verify MODULE=<module> BASE_REF=0610f3f` 补齐；`make lint-all` 核对首扫 | 17 模块实际覆盖，新增为零；存量的严格 lint 非零如实作为已验证的预期结果 |
| 源码开发 | `make deps` → `make dev` → `make dev-observe` → `make stop` | 业务/观测就绪，依赖正确，停止只处理本工作区 |
| 真实测试 | `make integration SCOPE=business` / `observe`；`make e2e SCOPE=business` / `observe`；`make verify-compose` | 原生业务、观测、浏览器和全栈闭包全部通过 |
| 失败诊断 | 最小真实端口/子进程失败；一个真实 Compose 浏览器失败 | 非零退出，材料在清理后可读，主失败/清理失败分开，外部资源不受影响 |
| 交付回归 | 同候选 `make package PLATFORM=linux/amd64 RUNTIME=1` → `make verify-lifecycle INSTALL=clean PLATFORM=linux/amd64` | 原 Bundle/manifest/receipt 合同成立，隔离安装/启停/清理通过 |
| 指南与门禁 | `make docs-check BASE_REF=0610f3f`，人工沿 AGENTS 地图核对代码/命令/事实来源 | 当前入口准确，重复错误正文已经处理，冻结原文未变 |
| 远端质量 | 最终 head 的既有 push 质量 workflow；下载本次选定诊断摘录 | 必需 job 完整成功，CI 与本地命令一致，发布材料已脱敏 |
| 完成元数据 | 现有 validate_versions 与 validate_branch completion | 版本/分支/分配/同名实际日志一致，结果引用真实候选 |

verify 已真实完成的 integration/e2e/Compose 或模块检查直接计入对应门禁，不再执行一次。
负向场景复用现有 fixture，只补新诊断与清理边界；不要求每个 scope 各搭一个失败栈。
文档-only 选择由选择器与 workflow 行为测试核对；新文档 job 在本批实际 CI 中确认，后续 update 沿用它。
未实际运行的 main 预热或 release-candidate 不写为本批完成事实。
本批没有新增容量或长时测量，也不据此更新完整备份恢复或长期稳定性结论。

## 5. 版本、证据与收口

开工前 fetch 主远端；本总/分方案进入 main 后，从最新 main 创建 `develop/2.5.6`。
复核 05 已完成、唯一分配有效、`0610f3f` 是祖先以及 runner/下载/服务权限可用。

实现和行为验收期间 VERSION 保持已完成值。成功收口按根指南同步目标版本及其关联元数据，
与同名实际日志一起提交；沿用现有 sync_version_metadata、validate_versions 和分支校验。
日志写已经执行的范围、结果、偏差与限制，不预写远端 CI 或新版本制品已通过。
最终 head 的权威 CI 成功后才能确认完成、进入自动 PR；推送前完成必要本地检查。

| 完成元数据或后续修改影响什么 | 必须处理什么 |
| --- | --- |
| 前端 package/lock 或工具身份 | 依赖助手重新判断，验证实际安装与对应前端检查/浏览器 |
| 应用配置、运行镜像、Bundle/manifest/安装输入 | 在实际新候选重新构建并重验受影响运行/交付证据 |
| 选择器、Make 或 CI 条件 | 重验相关选择/失败传播，最终 head 的权威 CI 必须使用实际分支范围 |
| 只有日志、导航或事实登记 | 核对正文和治理；未变的应用/制品证据引用原候选与有效输入，不换绑 |

版本变化后的必要重验纳入本批成本，由实际输入影响决定；不预先规定两轮本地全量、三次远端 CI
或三种日志状态。既有 VERSION/部署/工作流的保守 CI 选择仍有效。
正式验收回执保留自己的 revision、VERSION、制品与实际结果；完成日志引用它们，
不能把旧制品说成来自完成提交。新版本发布结论只能引用新版本的实际验收材料。
发布前核验实际选定的摘录；长期结论仍按原合同留存必要材料，不仅依赖会过期的 artifact。

日志使用 `dev/logs/Phase-23/Phase-23-06-工程稳定性改造.md`，
只记录实际交付、必要结论与 CI/回执定位。最终 CI 链接可在交付说明提供，
不为回填每条成功输出增加提交。PR 打开或批次完成后不继续使用开发分支。

## 6. 成本与停止点

本批尚未实施，以下是规划估算。预计 1,200 分钟（20 小时），累计上限 1,800 分钟（30 小时）。
估算覆盖具体实现、直接检查、一组组合验收、必要的元数据重验与最终远端 CI；
缓存失效、重试和排队均计入，不能把等待当免费，也不把并行 job 时间串行累加。

| 段 | 预计 / 上限（分钟） |
| --- | --- |
| H 指南、文档与检查 | 110 / 170 |
| A 工具检查与准备 | 90 / 140 |
| B 依赖判断 | 130 / 190 |
| C Go 检查 | 120 / 180 |
| D 双前端检查 | 110 / 160 |
| E 失败诊断 | 150 / 230 |
| F 共同入口与 CI | 140 / 210 |
| G 组合验收与收口 | 350 / 520 |

G 的成本分解如下，已计入上表，原验收的断言和等待窗口保持：

| 工作 | 预计 / 上限（分钟） |
| --- | --- |
| 干净 checkout、工具与依赖准备 | 20 / 30 |
| 17 模块直接回归与扫描 | 60 / 90 |
| 源码开发就绪与安全停止 | 15 / 25 |
| 两个 integration scope | 40 / 55 |
| 两个 e2e scope | 40 / 55 |
| Compose 闭包 | 45 / 60 |
| Bundle 与隔离安装、必要交付重验 | 50 / 80 |
| 最终远端 CI（含排队） | 60 / 90 |
| 失败诊断、文档、元数据与归属清理 | 20 / 35 |

开工用最低层冷准备/扫描测得的耗时核对预算；完整构建和真实验收前确认剩余时间足以完成及清理。
各段结束报告结果和剩余工作，开始 G 前核对总成本；接近上限时及时说明，沿用同一累计窗口。
不设置逐命令时间台账或百分比汇报，不因换 checkout、重试或继续对话重开计时。

基础设施失败立即停止整组，先做最小复现；同一原因最多两次、每次最多 10 分钟，
修正并通过直接检查后只重跑受影响项。开始 G 与后续昂贵操作时，在 G 剩余预算中保留至少
20 分钟收尾余量；各操作在自己的 job 上限内安排停止和清理，不用额外重开预算。
到段/总上限或无法推进时保留通过/失败/未开始、必要诊断与剩余方案，按
[成本与接续规则](../../rules/implementation-execution-budget.md)停止，不作完成声明。

## 7. 完成条件

1. AGENTS 的地图与命令准确；当前正文职责明确，有问题的重复入口已处理，没有新增删除清单或规则副本。
2. H～F 的具体行为和 G 的固定门禁都有真实结果，17 模块与相关真实消费者未漏验。
3. 本地与 CI 共用模块清单、选择和 Make 组合；失败、取消、意外跳过或清理失败不能通过。
4. 失败材料在资源清理后仍能定位，实际上传副本已核验脱敏，正式回执与历史结论没有改写。
5. 目标版本与实际日志按根规则收口，元数据影响已验证；新旧候选、制品和回执引用准确。
6. 最终 head 的权威 CI 成功后才确认完成，能力状态只登记实际验证的工程能力和限制。
