# GoPulse 工程稳定性改造方案

> 2026-10-10。规则、文档入口、默认清理与证据留存已修订；可执行改造已分配到 Phase-23-06，待实施。
> 源码调查基线：upstream/main 的 `0610f3f`，完成产品版本 `2.5.5`。
> 本文保留实现设计；批次分配由 [Phase 23 总方案](../imple/Phase-23/Phase-23-总实施方案.md)登记，文件范围、固定门禁和预算由唯一[实施文件](../imple/Phase-23/Phase-23-06-工程稳定性改造.md)定义。

## 1. 本次直接改掉了什么

| 问题 | 处理结果 |
| --- | --- |
| 根 AGENTS 主要是执行制度，组件和命令需要另找 | 整份重写为开发指南，先给源码地图和已有验证入口，再保留分支、版本、合同与证据约束 |
| 规划目录各有 AGENTS，重复根规则、工具规则和成本要求 | 删除两份目录 AGENTS；不新增替代 AGENTS，计划写法在现有 dev/README 说明 |
| 普通任务固定 120/180 分钟、50%/80% 汇报、逐命令成本台账 | 删除这些全仓库默认制度；昂贵构建与正式验收按具体计划估算和约定停止点 |
| 根文件维护历史 Phase/版本推导 | 删除推导规则；当前分配从正式总方案读取，完成版本仍只用 VERSION |
| ci 规则重复解释执行成本和多层权威关系 | 重写为工具复用、新增必要性、行为验证和退役要求 |
| 默认清理与必要说明的边界未定义 | 根规则明确默认清理：核对用途和依赖，更新有效引用后直接删除；必要说明只用于避免误用 |
| 历史过程材料与结论证据一律要求保留 | 日常验证复用 CI，日志只记必要结论；仍被引用的能力证据保留可复核材料，纯历史过程材料可由 Git 或可靠归档留存 |
| 现行成本规则夹带取消批次的过程记录，一次性 CI 交接仍有旧脚本接续步骤 | 删除成本规则里的历史过程正文和两份已完成的 CI 维护/交接文件，累计限时与接续要求保留在现行规则 |
| docs 总入口保留过期的 Phase 21 未执行与“当前 2.2.4” | 重写为按任务选入口，版本和完成事实只链接权威来源 |
| 四个 docs 转发页无有效链接或代码调用 | 删除；有效引用指向当前 contracts/operations 正文，不再维护已删除入口表 |
| 阶段索引漏 Phase 23，原生测试说明未进入验收导航 | 补合同与原生日常验证入口，历史矩阵按原候选阅读 |
| 旧图和候选设计仍使用“当前”“唯一总纲”入口名称 | 导航明确图快照和历史候选的基线，架构权威指向生效设计索引 |

当前只有根与 ci 两份 AGENTS。工程约定、文档职责、计划写法使用现有 dev/README，
不再另建工程规范、文档规范或规划 AGENTS。已分配合同中的预算和门禁保持原范围。
三份冻结设计、既有验收合同与回执保持原文；本次不改变产品 VERSION 或历史能力结论。
本轮移除的过程记录原文可从 Git revision `c379699` 追溯，不另建清理台账或归档目录。

## 2. 整体工程能力要达到什么

| 模块 | 完成后具体能做什么 | 文件内实施段 |
| --- | --- | --- |
| 文档 | 本地和 CI 能按已有目录与导航发现断链、错误命令/模块、冻结路径改动和无正文归属的新文档 | H |
| 环境 | 开发者能先检查工具，再准备项目内依赖，不靠猜测环境缺什么 | A |
| 依赖 | 前端锁文件变化自动重装，源码开发、Make、浏览器共用一个判断 | B |
| 代码检查 | Go 与双前端新增缺陷告警阻断，存量问题有真实清单 | C、D |
| 诊断 | 成功保存必要摘要，失败先留诊断再清理；报告有留存边界，错误、退出码、清理结果和复现入口能对上 | E |
| 验证与 CI | 本地与远端读取同一模块清单、改动范围和必需结果 | F |
| 交付 | 干净环境真实 integration、浏览器、Compose 与 Bundle 安装能走通 | G |

沿用现有 15 个 Go 模块、两个前端、Make、devenv、acceptance 和 lifecycle。
不新增贡献流程、产品功能或另一套环境管理器。下文设计由 Phase-23-06 在一个实施文件内全部交付。

## 3. 新入口与准确行为

以下全是待实现接口，当前不能运行：

| 命令 | 行为与成功条件 |
| --- | --- |
| `make docs-check BASE_REF=upstream/main` | 检查当前入口与当前正文、命令和冻结路径；不安装前端依赖、不启动容器，首次可编译 devenv |
| `make doctor` | 检查 Go/Node/npm/Python/Docker/Compose，列出实际版本和缺失条件；不启动业务或改全局工具 |
| `make bootstrap [MODULE=frontend]` | 安装校验过的项目内 lint 工具、前端依赖与 Chromium；MODULE 只准备指定应用 |
| `make lint MODULE=backend` | 完整 lint；存在告警非零退出，完整扫描报告写入本次运行目录 |
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

复用 devenv CLI。文档职责和检查范围从已有目录、导航、冻结路径及必要的配置例外判断，
不要求逐个文件添加历史、保留或清理标签，不复制产品版本、能力状态或门禁。

| 检查 | 失败条件 |
| --- | --- |
| 当前入口与当前正文（含本次未修改的正文，首扫覆盖存量断链）的本地链接、图片、锚点 | 目标/锚点缺失、路径越出仓库；代码示例内的链接文本不当成有效引用 |
| 当前命令 | 声明的 Make target、MODULE 或 npm script 不存在；拟新增接口仅在标为方案的材料内允许 |
| 新文档归属 | 新 Markdown 无法从目录、导航或配置确定正文归属；不能通过忽略整个目录漏检 |
| 冻结文件 | 相对实际共同祖先的 diff 触及三份冻结原文 |
| 历史边界 | 当前入口指向已删除文件或没有定位信息的归档；保留的历史正文按原候选解释，不按现行接口强行重验 |

外部链接不在线探测。语义、架构一致性与能力证据仍需人工复核，链接通过不能证明产品完成。
最低层覆盖正常引用、缺失文件/锚点、重命名、代码块、错误模块/脚本及冻结 diff；
真实 docs-only CI 只运行治理与文档检查。

已有调查发现存量断链位于冻结原文、历史日志与评审、已取消方案和已退役执行器引用中。
docs-check 首扫按材料职责报告现状与本次回归；当前入口的断链仍须修复，
保留的历史正文不为清零改写。无独有用途的重复或过时正文按根默认清理原则删除。
每次扫描的数量和范围写入实际检查结果，方案不维护随正文变化的链接计数。

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

日常诊断是有界运行产物；长期能力证据按[实施记录](../logs/README.md#证据按用途留存)管理。
原始材料保存 .run/private-reports/<run_id>（目录 0700、文件 0600），发布副本保存
.run/reports/<run_id>。工程摘要用 gopulse.engineering-run.v1，现有验收 schema 不改。

成功运行保存 summary.json 与 steps.json；失败、取消或清理失败再生成实际需要的诊断和
复现材料。不重复抄写终端成功输出，不为成功运行生成空错误文件，不自动提交到 dev/logs。

发布白名单仅 summary.json、steps.json、errors.txt、reproduce.txt，总量 20 MiB、
单份摘录 2 MiB。记录 revision/VERSION、工具与输入摘要、scope、必需/实际步骤、退出码、
耗时、清理和复现命令。截断仅作用于副本并说明。

凭据按本次真实值脱敏；原始环境、Cookie/token、浏览器 trace/HTML/截图不自动上传。
无法验证脱敏的文件不发布，仍需诊断的原始材料私有保存。复现命令从已知参数生成，不复制秘密命令串。
执行顺序为主失败 → 留诊断 → 停进程 → 清理归属资源 → 最终摘要；取消/超时/清理失败分别记录。

实施文件的段 E 明确诊断保留期限、每工作区容量上限及超限行为。清理复用归属检查，只处理
已结束、无后续用途且达到留存期限的本工作区报告目录；当前运行、仍需接续或支撑正式结论的
材料不得自动删除。不能确定用途或安全归属时报告原因；超限不覆盖仍需保留的材料。
长期依据先选定并存入合同约定的位置，已有结论保存定位信息与校验摘要，
不能仅依赖会过期的 CI artifact。不新增全仓证据索引、保留标签或清理执行器。

复用 workspace/proc/digest 与 acceptance 的 Session/Cleanup/receipt，两个 internal 模块不互相导入。
Compose 浏览器容器不在复制报告前 --rm：分配本次身份，退出后复制到私有目录，核对标签后定向删除；
复制失败仍记录并清理。先证明子进程失败、输出捕获、取消、归属清理和报告留存边界，再构造真实验收栈。

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
缓存预热读取同一清单；缓存不代表验证成功。结束后上传实际结果摘要，失败时再上传已核验的诊断白名单。
自动 PR 保持 completion_ready、身份/token 校验、update merge/保留分支与开发 squash 策略。

### 实施文件范围

允许新增/修改文件和必要的既有覆盖统一登记在
[Phase-23-06](../imple/Phase-23/Phase-23-06-工程稳定性改造.md#5-允许文件与复用边界)，
本设计不再维护第二份执行清单。新增产品问题先记录复现、精确文件和回归，再修订该分方案。

## 5. 单文件实施

按用户 2026-10-10 的要求，本设计的可执行改造全部纳入一个 Phase-23-06 实施文件。
H → A → B → C → D → E → F → G 是文件内实施顺序，共用分支、候选和完成版本；
过程检查通过后进入下一段，全部实现与组合验收通过后统一收口，不按字母拆分批次。

分配查询[Phase 23 总方案](../imple/Phase-23/Phase-23-总实施方案.md#2-正式分配与进入顺序)；
成本、窗口、固定门禁、停止与接续查询
[实施文件](../imple/Phase-23/Phase-23-06-工程稳定性改造.md#7-成本窗口与停止)。
本设计不另维护预算，触顶或失败按同一文件保留实际进度，不能通过换文件或重开计时绕过停止点。

开工前核对工具/npm/Node/parser 兼容、下载身份、实际 lint 存量、远端必需 job 名与构建成本。
相关输入未变的成功检查继续有效；候选、Bundle、manifest 或合同变化时只重验受影响证据，
不换绑既有回执。普通报告与长期能力依据按既有清理和留存原则处理。

## 6. 整体完成条件

H～F 完成后，在合并前以同一已提交 revision 的干净独立 checkout 完成组合验收。
下表定义必须覆盖的行为；固定执行集合、范围基线与候选身份以
[单文件实施合同](../imple/Phase-23/Phase-23-06-工程稳定性改造.md#6-固定完成门禁)为准，
不能合入后以空 diff 声称全量验证完成。

| case_id | 操作 | 必须得到的结果 |
| --- | --- | --- |
| G01 | doctor → bootstrap → 再次 bootstrap | 前置条件明确，首次成功，输入未变不重装，工具差异记录 |
| G02 | 受控副本更改一份前端锁文件并调用入口 | 只重装该应用，失败后不继续测试 |
| G03 | verify BASE_REF=实际主线 PLAN_ONLY=1，输入代表性 diff | 17 模块完整，权限/共享消费者不漏选，文档-only 不启动产品 |
| G04 | verify BASE_REF=0610f3f | 本次改造必需步骤全部真实完成，与同 revision CI 对齐，无新增 lint 告警 |
| G05 | 两 integration、两 e2e、verify-compose | 实际业务/观测/浏览器通过，只清理本次资源 |
| G06 | 最小真实端口冲突、测试失败和清理失败 | 非零退出，主失败/清理分开，报告可复现，外部资源不受影响 |
| G07 | 同冻结候选 package PLATFORM=linux/amd64 RUNTIME=1，再隔离安装/启停 | Bundle/manifest/回执身份一致，已有交付合同有效 |
| G08 | 下载同 revision CI 结果摘要与代表失败诊断并检查 | 无私有凭据，必需检查完整，结果与真实退出状态相符；成功输出精简，长期依据可定位 |
| G09 | docs-check；受控副本断链/错模块/冻结 diff | 真实问题非零退出，历史正文不改；当前命令与索引能定位 |

同候选、相关输入与环境未变的 G04/G05 成功检查不重复；相关变动仅重验受影响集合。
G07 不声明完整备份恢复或长期稳定性。全部通过后才更新能力状态和产品完成元数据，
日志只写实际交付、必要验证结论、偏差和限制，引用已有 CI 与结论所需证据。

参考接口：[golangci 安装](https://golangci-lint.run/docs/welcome/install/local/)与
[CLI](https://golangci-lint.run/docs/configuration/cli/)；
[ESLint](https://eslint.org/docs/latest/use/configure/configuration-files)、
[Vue 插件](https://eslint.vuejs.org/user-guide/)、
[TypeScript parser](https://typescript-eslint.io/getting-started/)。

当前完成的是规则和文档重写以及 Phase-23-06 的单文件规划；上述命令、lint 与产品组合验收仍待实施，VERSION 保持已完成值。
