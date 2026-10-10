# GoPulse 工程稳定性改造方案

> 2026-10-10。规则与文档入口已重写；下文的可执行改造尚未实施。
> 源码调查基线：upstream/main 的 `0610f3f`，完成产品版本 `2.5.5`。
> 本文不分配 Phase、版本或分支；工具实现须纳入正式总方案与分方案。

## 1. 本次直接改掉了什么

| 问题 | 处理结果 |
| --- | --- |
| 根 AGENTS 主要是执行制度，组件和命令需要另找 | 整份重写为开发指南，先给源码地图和已有验证入口，再保留分支、版本、合同与证据约束 |
| 规划目录各有 AGENTS，重复根规则、工具规则和成本要求 | 删除两份目录 AGENTS；不新增替代 AGENTS，计划写法在现有 dev/README 说明 |
| 普通任务固定 120/180 分钟、50%/80% 汇报、逐命令成本台账 | 删除这些全仓库默认制度；昂贵构建与正式验收按具体计划估算和约定停止点 |
| 根文件维护历史 Phase/版本推导 | 删除推导规则；当前分配从正式总方案读取，完成版本仍只用 VERSION |
| ci 规则重复解释执行成本和多层权威关系 | 重写为工具复用、新增必要性、行为验证和退役要求 |
| docs 总入口保留过期的 Phase 21 未执行与“当前 2.2.4” | 重写为按任务选入口，版本和完成事实只链接权威来源 |
| 四个 docs 转发页无有效链接或代码调用 | 删除；dev/README 一处说明历史路径与当前 contracts/operations 正文 |
| 阶段索引漏 Phase 23，原生测试说明未进入验收导航 | 补合同与原生日常验证入口，历史矩阵按原候选阅读 |
| 旧图和候选设计仍使用“当前”“唯一总纲”入口名称 | 导航明确图快照和历史候选的基线，架构权威指向生效设计索引 |

当前只有根与 ci 两份 AGENTS。工程约定、文档职责、计划写法使用现有 dev/README，
不再另建工程规范、文档规范或规划 AGENTS。已分配合同中的预算和门禁保持原范围。
三份冻结设计、历史合同/日志/评审与原始证据未改；本次不改变产品 VERSION。

## 2. 整体工程能力要达到什么

| 模块 | 完成后具体能做什么 | 尚需实施 |
| --- | --- | --- |
| 文档 | 本地和 CI 能发现断链、错误命令/模块、冻结路径改动和未归类的新文档 | H |
| 环境 | 开发者能先检查工具，再准备项目内依赖，不靠猜测环境缺什么 | A |
| 依赖 | 前端锁文件变化自动重装，源码开发、Make、浏览器共用一个判断 | B |
| 代码检查 | Go 与双前端新增缺陷告警阻断，存量问题有真实清单 | C、D |
| 诊断 | 失败先留报告再清理，错误、退出码、清理结果和复现入口能对上 | E |
| 验证与 CI | 本地与远端读取同一模块清单、改动范围和必需结果 | F |
| 交付 | 干净环境真实 integration、浏览器、Compose 与 Bundle 安装能走通 | G |

沿用现有 15 个 Go 模块、两个前端、Make、devenv、acceptance 和 lifecycle。
不新增贡献流程、产品功能或另一套环境管理器。正式批次据本文的范围、文件和门禁分配。

## 3. 新入口与准确行为

以下全是待实现接口，当前不能运行：

| 命令 | 行为与成功条件 |
| --- | --- |
| `make docs-check BASE_REF=upstream/main` | 检查当前入口、修改正文、命令和冻结路径；不安装前端依赖、不启动容器，首次可编译 devenv |
| `make doctor` | 检查 Go/Node/npm/Python/Docker/Compose，列出实际版本和缺失条件；不启动业务或改全局工具 |
| `make bootstrap [MODULE=frontend]` | 安装校验过的项目内 lint 工具、前端依赖与 Chromium；MODULE 只准备指定应用 |
| `make lint MODULE=backend` | 完整 lint；存在告警非零退出，完整报告保留 |
| `make lint MODULE=frontend BASE_REF=upstream/main` | 完整扫描，再按共同祖先 diff 分类；新增告警阻断，存量报告不冒充零告警 |
| `make lint-all` | 扫描全部模块，全部无告警才成功 |
| `make verify BASE_REF=upstream/main PLAN_ONLY=1` | 只输出模块、必需步骤和选择原因，无测试/容器副作用 |
| `make verify BASE_REF=upstream/main` | 执行选择范围的既有检查、lint 与真实验证；必需步骤或清理失败均非零退出 |
| `make verify MODULE=backend` | 只做该模块 check/lint/test/race/build，报告标注模块范围 |

BASE_REF 必须存在，CI 使用 origin/main。verify 的 MODULE 与 BASE_REF 互斥。
本地范围纳入 tracked 工作区 diff 和模块内新增源码，不能漏掉未提交改动。
Go 使用 linter 的共同祖先参数；ESLint JSON 按实际文件/行 diff 分类。
删除、重命名、多提交及主线推进均需验证，不使用仅比较上一提交的 --new。

## 4. 具体实现合同与文件

### H：文档检查

复用 devenv CLI。文档分类只描述职责和检查范围，不复制产品版本、能力状态或门禁。

| 检查 | 失败条件 |
| --- | --- |
| 当前入口及修改正文的本地链接、图片、锚点 | 目标/锚点缺失、路径越出仓库；代码示例内的链接文本不当成有效引用 |
| 当前命令 | 声明的 Make target、MODULE 或 npm script 不存在；拟新增接口仅在标为方案的材料内允许 |
| 新文档归属 | 新 Markdown 未归入导航、当前正文、方案、历史或冻结类别；不能通过忽略整个目录漏检 |
| 冻结文件 | 相对实际共同祖先的 diff 触及三份冻结原文 |
| 历史边界 | 当前入口指向已删除转发页；历史正文保留旧命令/数值，不按现行接口强行重验 |

外部链接不在线探测。语义、架构一致性与能力证据仍需人工复核，链接通过不能证明产品完成。
最低层覆盖正常引用、缺失文件/锚点、重命名、代码块、错误模块/脚本及冻结 diff；
真实 docs-only CI 只运行治理与文档检查。

### A、B：工具和依赖

- 检查基线拟用 Go 1.26.7、Node 24.20.0、npm 11.19.0、golangci-lint 2.14.0。
  前三者来自调查工作区；A 先核对 npm/Node 兼容。Go 模块继续声明 1.26，治理 Python 要求 3.12+。
- 交付构建继续读取 build-bases lock（Go 1.26.0）；分别记录检查与构建版本，不顺带升级镜像。
  Docker/Compose 按必需功能检查；本地受支持的 Go 1.26.x 与固定 CI 版本差异如实报告。
- lint 二进制固定版本与 SHA256，保存在 .run/tools。Chromium 使用锁定 Playwright 的安装入口。
  宿主库不足明确失败，不自动安装系统软件。
- 前端成功标记绑定 package.json、lockfile、实际 Node/npm、OS/架构。
  目录/标记缺失或摘要变化才 npm ci；失败不写标记，输入未变则复用。
- 每工作区、每应用独立安装锁；Make、devrun、e2e 共用 frontenddeps。
  活动开发进程使用该应用时先停止，再替换依赖。

### C、D：缺陷检查

Go 首批仅 staticcheck、errcheck、unused；配置版本 2，单模块超时 10 分钟，只读依赖。
保留既有 gofmt/vet，包括普通测试；integration build tags 仍由真实入口覆盖。

前端首批仅 no-debugger、no-async-promise-executor、no-constant-condition（允许明确无限循环）、
vue/no-mutating-props、vue/no-side-effects-in-computed-properties。保留 vue-tsc/tsc。
两个配置引用共享纯规则数据，由各应用加载本地插件；D 先验证 Vue/TypeScript parser 兼容并锁定版本。

新增告警阻断，首次全量存量扫描如实登记；不整目录排除、扩大基线或全仓格式重写。
行为修复先进入分方案的精确文件与回归范围。改动复核沿用[工程入口](../README.md#代码改动约定)。

### E：失败报告

原始材料保存 .run/private-reports/<run_id>（目录 0700、文件 0600），发布副本保存
.run/reports/<run_id>。工程摘要用 gopulse.engineering-run.v1，现有验收 schema 不改。

发布白名单仅 summary.json、steps.json、errors.txt、reproduce.txt，总量 20 MiB、
单份摘录 2 MiB。记录 revision/VERSION、工具与输入摘要、scope、必需/实际步骤、退出码、
耗时、清理和复现命令。截断仅作用于副本并说明。

凭据按本次真实值脱敏；原始环境、Cookie/token、浏览器 trace/HTML/截图不自动上传。
无法验证脱敏的文件不发布，原始证据私有保留。复现命令从已知参数生成，不复制秘密命令串。
执行顺序为主失败 → 留诊断 → 停进程 → 清理归属资源 → 最终摘要；取消/超时/清理失败分别记录。

复用 workspace/proc/digest 与 acceptance 的 Session/Cleanup/receipt，两个 internal 模块不互相导入。
Compose 浏览器容器不在复制报告前 --rm：分配本次身份，退出后复制到私有目录，核对标签后定向删除；
复制失败仍记录并清理。先证明子进程失败、输出捕获、取消和归属清理，再构造真实验收栈。

### F：范围、CI 和结果

根 Makefile 新增无副作用的 list-go-modules/list-npm-modules。
ci 选择器读取清单，保留 checks/reasons，增加文档检查及 go_modules/npm_modules 输出；
verify 消费同一 JSON，不再写第二份选择器。模块集合为：

`backend componentmetrics router marshaller monitor lifecycle loadtest devtools acceptance`
及 `exporters/{elasticsearch,kafka,mysql,rabbitmq,redis,victoriametrics}`；
前端为 frontend/admin-frontend。

治理 → 文档/模块快检查 → 被选中的真实验证 → 汇总。
权限、共享消费者、部署及未知产品路径的既有选择保持有效；空集合明确跳过。
汇总对照预期矩阵和 revision，缺失、失败、取消、超时或意外跳过均失败。

| job | 入口与上限 |
| --- | --- |
| 文档 | docs-check，5 分钟；update 可运行，无产品环境 |
| Go / 前端 | 模块 check/lint/test/race/build；30 / 20 分钟 |
| integration | business / observe 原生入口，30 / 25 分钟 |
| e2e / Compose | 两浏览器 scope 各 25 分钟；verify-compose 60 分钟 |
| release candidate | 既有双架构 package 链，120 分钟，仍按现有发布许可 |

本地与 CI 的准备、参数和真实边界逐项对齐，保留现有 services 的独有步骤。
缓存预热读取同一清单；缓存不代表验证成功。结束后上传已核验的诊断白名单。
自动 PR 保持 completion_ready、身份/token 校验、update merge/保留分支与开发 squash 策略。

### 允许文件清单

| 单元 | 新增/修改文件 |
| --- | --- |
| H | Makefile；devtools/config/documents.json；internal/quality/docs.go 及测试；devenv CLI/测试；ci/quality_scope.py 及测试；quality-gates.yml |
| A | Makefile；devtools/toolchains.json；internal/quality/{doctor,bootstrap}.go 及测试；devenv CLI/测试 |
| B | internal/frontenddeps/deps.go 及测试；devrun/applications.go 与 devrun_test.go；testenv/e2e.go 与 testenv_test.go；两个前端 Makefile |
| C | .golangci.yml；15 个模块 Makefile；internal/quality/lint.go 及测试；devenv CLI/测试 |
| D | 两前端 eslint.config.mjs、package.json/lock；devtools/config/eslint-rules.mjs；lint 助手和测试 |
| E | internal/diagnostics/report.go 及测试；testenv/{testenv,processes,e2e}.go 及测试；proc/proc.go 及测试；frontend/playwright.config.ts；acceptance harness/session 与测试、scenario/{compose,suites,lifecycle}.go、CLI/测试 |
| F | internal/quality/verify.go 及测试；devenv CLI/测试；ci/quality_scope.py/测试；ci/validate_quality_result.py/测试；quality-gates、cache-warm、auto-pr-merge、release-candidate workflows；ci/test_auto_pr_workflow.py |
| 各批 / G | 根 AGENTS/README、dev/README 和原生入口登记只同步已实现接口；G 成功后登记能力状态 |

上表 internal 路径默认在 devtools；acceptance 的路径明确注明。
遇到新增产品问题，先记录复现、精确文件和回归，再修订分方案，不顺手扩大到全仓治理。

## 5. 实施顺序与本方案成本

H → A → B → C → D → E → F → G。字母只是提案单元，不是已分配批次。
以下预算仅针对这次整体工程改造，不成为所有仓库任务的固定时间制度。

| 单元 | 预计活跃时间 / 累计上限 | 固定完成门禁 |
| --- | --- | --- |
| H | 100 / 180 分钟 | 文档边界 fixture；devtools check/test/race；真实 docs-only CI |
| A | 80 / 180 分钟 | 工具成功/缺失/版本与下载身份失败；bootstrap；devtools check/test/race；治理 |
| B | 100 / 180 分钟 | 首装/复用/锁变化/失败/互斥；两前端 check/test/build；真实 dev 与两 e2e scope |
| C | 100 / 180 分钟 | 配置与全量存量报告；新增违规非零退出；Go 既有 check/test/race |
| D | 100 / 180 分钟 | parser 兼容与违规 fixture；新增/存量分类；两前端 check/test/build 和 e2e |
| E | 120 / 180 分钟 | 脱敏/路径/主失败与清理失败；实际 integration、e2e、Compose 代表失败 |
| F | 120 / 180 分钟 | diff/矩阵/失败传播；文档、单模块、共享、权限、部署、未知路径；真实 CI/artifact |
| G | 120 / 180 分钟 | 下节 G01～G09，干净环境、同候选与实际交付 |

预计 840 分钟（14 小时），累计上限 1440 分钟（24 小时），均为未测估算。
正式分方案按进入预检调整拆分；每单元的上限分配如下（分钟）：

| 单元 | 发现/实现 | 直接检查 | 构建/准备 | 真实/CI | 证据/清理 | 机动 |
| --- | --- | --- | --- | --- | --- | --- |
| H | 60 | 30 | 10 | 55 | 10 | 15 |
| A | 65 | 20 | 15 | 55 | 10 | 15 |
| B | 55 | 20 | 15 | 65 | 10 | 15 |
| C | 55 | 25 | 15 | 60 | 10 | 15 |
| D | 55 | 20 | 20 | 60 | 10 | 15 |
| E | 50 | 25 | 15 | 65 | 15 | 10 |
| F | 45 | 25 | 15 | 70 | 15 | 10 |
| G | 10 | 15 | 30 | 90 | 20 | 15 |

本改造不含新容量/长时实验，required experiment duration 为 0；真实门禁仍需执行。
首次基础设施失败停止整组，同原因最多两次、每次 10 分钟最小诊断，计入该单元成本。
到上限保留通过/失败/未开始与原始证据，归属清理后停止；具体剩余方案与明确指令后接续。

开工前仍须核对 npm/Node 与 ESLint/parser 兼容、实际 lint 存量、远端分支保护旧 job 名和构建成本。
开始构建前登记源/config 到镜像/门禁的影响，复用未变缓存和有效检查，不换绑候选回执。

## 6. 整体完成条件

H～F 进入主线后，在已提交 revision 的干净独立 checkout 运行。
阶段 diff 基线固定 0610f3f，覆盖整个改造；不能合入后以空 diff 声称全量验证完成。

| case_id | 操作 | 必须得到的结果 |
| --- | --- | --- |
| G01 | doctor → bootstrap → 再次 bootstrap | 前置条件明确，首次成功，输入未变不重装，工具差异记录 |
| G02 | 受控副本更改一份前端锁文件并调用入口 | 只重装该应用，失败后不继续测试 |
| G03 | verify BASE_REF=实际主线 PLAN_ONLY=1，输入代表性 diff | 17 模块完整，权限/共享消费者不漏选，文档-only 不启动产品 |
| G04 | verify BASE_REF=0610f3f | 本次改造必需步骤全部真实完成，与同 revision CI 对齐，无新增 lint 告警 |
| G05 | 两 integration、两 e2e、verify-compose | 实际业务/观测/浏览器通过，只清理本次资源 |
| G06 | 最小真实端口冲突、测试失败和清理失败 | 非零退出，主失败/清理分开，报告可复现，外部资源不受影响 |
| G07 | 同冻结候选 package PLATFORM=linux/amd64 RUNTIME=1，再隔离安装/启停 | Bundle/manifest/回执身份一致，已有交付合同有效 |
| G08 | 下载同 revision CI 发布诊断并检查 | 无私有凭据，必需检查完整，结果与真实退出状态相符 |
| G09 | docs-check；受控副本断链/错模块/冻结 diff | 真实问题非零退出，历史正文不改；当前命令与索引能定位 |

同候选、相关输入与环境未变的 G04/G05 成功检查不重复；相关变动仅重验受影响集合。
G07 不声明完整备份恢复或长期稳定性。全部通过后才更新能力状态和产品完成元数据，
日志只写实际命令与结果。

参考接口：[golangci 安装](https://golangci-lint.run/docs/welcome/install/local/)与
[CLI](https://golangci-lint.run/docs/configuration/cli/)；
[ESLint](https://eslint.org/docs/latest/use/configure/configuration-files)、
[Vue 插件](https://eslint.vuejs.org/user-guide/)、
[TypeScript parser](https://typescript-eslint.io/getting-started/)。

本次完成的是规则和文档重写，尚未新增上述命令、安装 lint、执行这组产品验收或改变 VERSION。
