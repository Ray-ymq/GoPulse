# Phase-23-06：工程稳定性改造

> 状态：已分配，待实施。规划与执行合同修订日期：2026-10-10。
> 目标版本：`2.5.6`；开发分支：`develop/2.5.6`。分配来源为[Phase 23 总方案](Phase-23-总实施方案.md#2-正式分配与进入顺序)。
> 单批、单实施文件、单开发分支、单完成版本。H→A→B→C→D→E→F→G 全部完成后统一收口；本文的门禁编号、退出语义、候选与预算是执行合同。

## 1. 交付结果与已有覆盖

完整交付[工程稳定性设计](../../design/GoPulse-工程稳定性改造方案.md)的可执行部分，
复用已完成的规则、默认清理和证据留存。15 Go + 2 前端 = 17 模块，清单只来自根 Makefile。

| 已有文件 / 覆盖 | 本批具体改变 | 验证缺口 |
| --- | --- | --- |
| [根 Makefile](../../../Makefile)、模块 Makefile | 增加清单、docs-check、doctor/bootstrap、lint/verify；提取 check-compose 薄入口 | 清单无副作用；本地与 CI 步骤相同 |
| [devenv](../../../devtools/cmd/devenv/main.go)、workspace/proc/digest 与对应测试 | 接入质量命令，沿用工作区身份、子进程取消与摘要 | 新参数、非零退出、输入身份与选择结果 |
| [devrun/applications.go](../../../devtools/internal/devrun/applications.go)、[testenv/e2e.go](../../../devtools/internal/testenv/e2e.go)、两前端 Makefile | 把仅检查 node_modules 的三处路径接到同一 frontenddeps | 锁文件/工具变化、安装互斥、失败无成功标记 |
| [quality_scope.py](../../../ci/quality_scope.py)、[质量 workflow](../../../.github/workflows/quality-gates.yml)、已有治理测试 | 保留旧 checks/reasons，新增 docs 与步骤 JSON；显式映射共享配置、汇总真实结果 | 文档当前不选择检查；共享规则漏前端；新根配置误选全量 |
| [acceptance Session](../../../acceptance/internal/harness/session.go)、现有场景及 receipt 测试 | 删除容器/临时目录前保存必要诊断；给 receipt 加工程摘要旁路引用 | 原始材料丢失、清理失败、工程报告与回执的边界 |
| [原生验证](../../validation/local-development-tests.md)、[Bundle](../../../deploy/release/README.md)、lifecycle 既有覆盖 | 沿用真实 integration/e2e/Compose、建包与隔离安装 | 同候选完整验收、元数据变更后的再次验证 |

不新增运行模块、全仓证据索引或另一套环境管理器，不重建退役矩阵。业务 API、权限、
消息、公开验收 schema、交付基镜像和三份冻结原文沿用既有合同。范围外缺陷先保留复现，
修订本文件的精确文件与回归后处理；存量 lint 债不在本批全仓修复。

## 2. 进入、预检与候选

### 2.1 开工前必须核实

规划先进入主远端 main；开工 fetch 主远端，从最新 main 创建唯一 `develop/2.5.6`，
确认 Phase-23-05 已完成、基线 VERSION 为 `2.5.5`，没有身份冲突的分支/PR。
普通范围用实际共同祖先；完整改造范围基线固定 `0610f3f`，必须是候选祖先。
本次规划不创建开发分支或日志；实施时只用同名
`dev/logs/Phase-23/Phase-23-06-工程稳定性改造.md`。

| 核实项 | 命令 / 方式 | 记录位置与不满足时的处理 |
| --- | --- | --- |
| 主线、分配、候选基线 | fetch；`git rev-parse HEAD`；`git merge-base --is-ancestor 0610f3f HEAD`；开发模式分支校验 | 同名日志“预检”：远端、main SHA、VERSION、分配；祖先/身份不符停止，不用空 diff |
| 工具与 parser | `go version`、`node --version`、`npm --version`、`python3 --version`、`docker version`、`docker compose version`；核对包 engines/parser 支持 | “预检”：实际版本；设计中的 Go 1.26.7 / Node 24.20.0 / npm 11.19.0 只是调查起点，A 核实兼容与下载身份 |
| 下载与成本 | lint 下载版本/SHA256、Chromium 与 Playwright lock；最低层试装、首次模块扫描计时 | “预检”：来源、摘要、冷准备/扫描时间；超出 §7 剩余预算不启动完整矩阵 |
| CI 与身份 | 读取真实 workflow/job 名、输入、权限、分支保护、token 身份及可用 runner | “预检”：实际 job 对照；服务/权限不足留未完成，不冒充本地已替代远端 |
| lint 存量 | C/D 的全量报告和新增分类 | “验证/C1、D1”：原始扫描退出码、总量/存量/新增数与路径；不预设一定有债 |

### 2.2 冻结和版本顺序

采用两次候选验收，避免提前修改 VERSION，也不把 `2.5.5` 的制品当作 `2.5.6`：

1. H～F 逐段提交，VERSION 保持 `2.5.5`；最后做必要的 docs-only 同步提交。
   冻结其已提交 SHA 为 **R0**，在干净独立 checkout 运行 §4 G00～G09，包含权威 push CI。
2. R0 全部成功后，才同步 VERSION、.env.example、两前端包/lock 与 runtime-contracts 的
   `2.5.6` 版本字段，并与同名实际日志提交为 **R1**。日志记录 R0 结果及“收口核验中”，
   不写 R1 已通过；§3.6 的 completion_ready 保护阻止此时自动开 PR。
3. 干净 checkout 冻结 R1，按 G10 和 §8 的版本敏感清单重验全量选中范围、建包/安装、
   元数据与权威 CI。R0/R1 的回执分别保留真实 SHA、VERSION、输入和制品，不编辑或换绑。
4. R1 成功后，只补实际日志、能力状态和必要正文，形成收口登记 **R2**；日志写明
   R0/R1 的结果与复用理由。发布候选仍为 R1，不宣称 R1 制品来自 R2。
   R2 日志记录“收口记录就绪，最终 CI 待确认”，不能预写尚未运行的 G11 成功。
   R2 的治理、docs-check 与权威 push CI 必须绿；汇总才输出 completion_ready=true，允许 PR/合并。
   日志/状态正文自身不使未变的产品输入失效；改变应用、验证或交付输入则回到受影响门禁。

实际日志“候选与失效”记录 R0/R1 SHA，R2 由包含最终日志的提交定位；“远端验证”
记录 R0/R1 的 run URL、head/base、job、结论和报告路径。R2 最终 run 用精确 head 与
CI 摘要定位并在最终交付说明链接，不为追加 URL 再提交。PR 打开/完成后不继续开发分支。

## 3. 接口与行为合同

以下新增接口均待实现。表中 `0` / `非零` 指实际命令；devenv 子命令的校验失败为 1、
非法参数/ref/扫描器或报告错误为 2、超时 124、取消 130。GNU Make 通常把子命令失败转为 2，
报告必须同时保存 command_exit_code 与 child_exit_code，不能凭 Make 的 2 判断 lint 扫描器错误。

### 3.1 命令边界

| 入口 | 参数、成功与失败合同 |
| --- | --- |
| `make list-go-modules` / `make list-npm-modules` | 每行一模块、顺序来自根 Makefile，不用 MODULE 筛选；无安装/编译/测试/容器副作用 |
| `make docs-check BASE_REF=<ref>` | BASE_REF 必填且可解析；检查当前链接/锚点、实际命令/归属与冻结 diff；问题非零；不安装前端或启动产品 |
| `make doctor` | 检查 Go 1.26.x、锁定 Node/npm 的兼容关系、Python 3.12+、可用 Docker daemon/buildx、Compose v2 的 config JSON、up/wait、run/copy 功能；必需缺失非零；输出实际版本与原因，不安装/启动业务 |
| `make bootstrap [MODULE=frontend] [TOOLS_ONLY=1]` | MODULE 仅空/frontend/admin-frontend；空准备两应用，指定值准备该应用，同时准备 lint 工具；TOOLS_ONLY=1 仅准备 Go lint，与 MODULE 互斥；版本/SHA/安装失败非零，不能续跑检查 |
| `make lint MODULE=<module> [BASE_REF=<ref>]` | MODULE 必须在 17 模块清单中；完整扫描，有 ref 时分类新增；无 ref 时严格判所有告警；工具/ref/报告错误始终阻断 |
| `make lint-all` | 对 17 模块完整严格扫描；零告警 0，有告警非零；不支持用 BASE_REF 暗中放宽 |
| `make verify BASE_REF=<ref> [PLAN_ONLY=1]` | ref 必填；lint 步骤继承同一 BASE_REF；按 §3.3 的 required_steps 执行，新增为 0 才通过；缺失、实际失败、清理失败非零 |
| `make verify MODULE=<module> [PLAN_ONLY=1]` | MODULE 与 BASE_REF 互斥；指定模块 check/严格 lint/test/race/build，前端没有 race；有存量即预期非零，绝不作为整批新增门禁的唯一命令 |
| `make check-compose` | 提取现有 native-acceptance 的 Compose 渲染/loopback/镜像/依赖/内部网络静态断言；只渲染，问题非零；本地 verify 与 CI 共用，不能代替 verify-compose |

bootstrap 的复用必须满足工具版本/下载摘要、package/lock、实际 Node/npm、OS/架构和成功标记
一致；Chromium 绑定锁定 Playwright revision。失败不写标记；每工作区/应用安装锁，使用中的
开发应用先停再替换依赖；普通应用入口只调用轻量 frontenddeps，不安装整套工具或系统库。

### 3.2 lint 的判据与报告

本批阻断判据是 **BASE_REF 分类后的新增数为 0，且完整扫描没有工具/配置/报告错误**。
CI 的每个 Go/前端 lint 步骤必须是 `make lint MODULE=<module> BASE_REF=origin/main`，
checkout 采用 `fetch-depth: 0` 并确保 origin/main 已取回。
严格 lint/lint-all 在存量非零时是负向合同的预期结果：保存退出码、报告路径及条数快照，
不据此把整批判失败；若没有存量则应为 0。新增告警或扫描器错误绝不作为预期成功豁免。

Go 首批 staticcheck、errcheck、unused；各模块工作目录调用固定工具，显式
`--config <仓库绝对路径>/.golangci.yml`。先保存全量扫描，再用同一输入与工具缓存的
`--new-from-merge-base <ref>` 分类新增；不只保留过滤后的输出。前端首批
no-debugger、no-async-promise-executor、no-constant-condition（允许明确无限循环）、
vue/no-mutating-props、vue/no-side-effects-in-computed-properties，完整 ESLint JSON 按实际 diff 分类。

Go 关闭按 linter/同消息/同行去重的输出限额，保存全部 issue；本地另合并 tracked 工作区
diff 与未跟踪源码（新文件全部告警为新增），不能只依赖 HEAD 的 native 分类漏掉未提交问题。
完整/分类扫描的原始工具退出码另存 tool_exit_codes，与包装命令退出码分开。
同一 Go 模块的全量与分类共用 10 分钟 lint 上限，第二次使用剩余时间，不能各自重开 10 分钟。

§3.4 的私有目录 P 保存 `lint-raw.json`（原始工具输出）与 `lint.json`（规范结果），后者
schema 为 `gopulse.lint.v1`，含 tool/module/base_ref/merge_base、total_count/new_count/
existing_count、scan_error，以及规则、仓库相对路径、行列、消息和新增/存量分类。
每模块报告不得覆盖；总 lint-all 摘要引用各子运行。它们由 engineering-run 摘要引用，
不把 lint schema 冒充验收 receipt；原始 issue/message 不自动上传。

### 3.3 选择器 JSON、步骤与路径映射

`make verify ... PLAN_ONLY=1` stdout 只输出一个 JSON 文档，stderr 放诊断；不创建运行目录、
安装、测试或容器。selector 的 `--format json` 与 verify 使用同一 `gopulse.quality-scope.v1`
结构，旧 changed_files/reasons 保持字符串数组，checks 保持完整 check key→布尔映射；
新增字段固定为：

- mode（base/module）、base_ref、merge_base、head_revision、worktree_dirty；MODULE 模式两个 ref 为 null；scope_id 为不含自身的规范计划 JSON 摘要前 12 位十六进制。
- go_modules、npm_modules 是本次选中模块的字符串数组，按根清单顺序；空数组有效，完整 17 模块的 facts 仍来自 list-*，不能维护第二份硬编码清单。
- required_steps：按顺序的 id、check_id、argv（参数数组）、cwd（仓库相对路径）、required、expectation；唯一 id，expectation 为 zero 或 lint_new_zero。

raw JSON 中 exporter key 保留 `/`；`--github-output` 保留已有 `/`→`_` 转换。
新增 check key 固定为 `docs`，job id 为 `docs-check`，名称 Documentation；CI 治理总是运行，
BASE_REF 计划的治理步骤 check_id 为 null；MODULE 计划只包含指定模块步骤。
治理步骤的 branch 本地取实际分支，CI 显式传 workflow_call 的 branch，不能从 detached
HEAD 猜测；quality_scope 可新增可选 --branch，保留旧参数调用兼容。
本地在共同祖先 diff 上加入 tracked 工作区与模块内新增源码；删除、重命名、多提交和主线
推进不能漏选。MODULE 的严格 lint expectation 为 zero，BASE_REF 的为 lint_new_zero。

| selector check id | required_steps 的实际命令 / CI job |
| --- | --- |
| 15 个 Go 模块 key（含 exporters/…） | `make check MODULE=<module>` → lint（BASE_REF 模式显式传 ref）→ test → race → build；保留既有模块 job id/name |
| frontend / admin_frontend | 对应 frontend/admin-frontend 的 check → lint → test → build；保留 frontend/admin-frontend job |
| integration_business / integration_observe | `make integration SCOPE=business` / `observe`；保留 integration/integration-observe；CI services 的迁移、搜索准备等独有步骤继续有效 |
| e2e_business / e2e_observe | `make e2e SCOPE=business` / `observe`；保留 e2e-business/e2e-observe |
| compose | `make verify-compose`；保留 compose-full-stack |
| tools | acceptance check/test + `make check-compose`；治理另外总是运行；保留 native-acceptance 的全部独有静态断言，不另启动真实栈 |
| docs | `make docs-check BASE_REF=<ref>`；新增 docs-check，不受 run_product_checks=false 屏蔽 |

重合步骤按命令/输入去重；输出 required_steps 是本地与 CI 的同一执行合同，不能用 tests
成功代替被选中的 integration/browser。质量汇总新 job `quality-result` 必须核对预期集合、
实际步骤、head 与结果，缺失/失败/取消/超时/意外跳过均非零。

CI 适配复用同一个 verify 实现：先 `make build MODULE=devtools`，再用
`./devtools/bin/devenv verify --plan <scope.json> --check-id <key>` 执行该已选 check 的步骤。
该形式与 MODULE/BASE_REF 模式互斥，校验计划 schema、head/ref/步骤身份，不另选范围；
未选 key、坏计划或输入漂移非零。lint 的 argv 来自计划，保留明确 BASE_REF，产相同 P/R。
CI 的 services/迁移/搜索准备放在适配调用之前并记录实际结果；准备失败同样阻断，不能因
未进入 verify 而从汇总漏掉。需要 devenv 的前端/docs job 也设置锁定 Go，准备计入 job 上限。

汇总固定 CLI 为 `python3 ci/validate_quality_result.py --scope <scope.json> --results <job-results.json> --head <sha> --branch <branch> --report-dir <R>`，
可用 --github-output 写输出。results 每项含 job_id、head_revision、scope_id、实际 result
（success/failure/cancelled/skipped）与 steps，来源为真实 job/步骤结果，不能从预期集合伪造。
退出 0 只说明必需结果匹配；metadata_ready/completion_ready 另按 §3.6 输出，开发中未收口
仍可质量通过。正常失败/缺失/身份不符非零，P/R 摘要如实标明缺项。

| 改动路径（先于通用 devtools/未知路径分支匹配） | 显式选择 |
| --- | --- |
| dev/**、docs/**、当前 Markdown 入口 | docs；治理；无产品检查 |
| devtools/config/eslint-rules.mjs | frontend、admin_frontend、e2e_business、e2e_observe；保留 devtools 通用覆盖 |
| .golangci.yml | 15 个 Go 模块；不因该配置单独选择 Compose/integration/e2e |
| devtools/toolchains.json | 17 模块、tools、两 integration/两 e2e；工具版本变化需覆盖消费者 |
| devtools/config/documents.json | docs、devtools；其余 devtools/config 未知文件沿用通用覆盖并选两前端 |
| VERSION、.env.example | 既有全量产品集合，加 docs；不得缩小 |
| 前端 package/lock、deploy/runtime-contracts.json、工作流 | 沿用现有选择并补足共享消费者；R1 用 VERSION 强制全量 |
| 其余权限/共享/部署/未知产品路径 | 保留现有保守覆盖；未知产品路径选全量并说明原因 |

### 3.4 报告生产方与归属

复用 workspace.For 的真实根路径摘要，identity 为已有 12 位十六进制身份；
run_id 固定 `YYYYMMDDTHHMMSSZ-<12位随机十六进制>`，原子创建、碰撞重试，不接受任意路径。
P = `.run/local/<identity>/private-reports/<run_id>`（0700/0600）；
R = `.run/local/<identity>/reports/<run_id>`（脱敏发布副本，目录 0700、文件 0600）。
同工作区的子步骤使用独立 run_id，父摘要引用；两个 worktree 即便共用 .run 也不得互删。

| 入口 | 产物 / 路径 | 留存与 CI 上传 |
| --- | --- | --- |
| list-*、verify PLAN_ONLY | stdout JSON/清单，无文件 | 无报告、无 artifact |
| doctor/bootstrap/docs-check/lint/lint-all/verify/check-compose | P/summary.json、steps.json；docs-check 另 docs.json，lint 另 lint*.json；失败按需 errors.txt/reproduce.txt | 普通报告期限；CI 只上传核验过的 R 白名单 |
| integration/e2e/dev/dev-observe/stop | P 的工程摘要引用已有 private state、日志和必要浏览器原始材料；失败先保存再清理 | 既有 state 归属优先；浏览器 trace/HTML/截图只私有，不自动上传 |
| verify-compose 及本批使用的 acceptance 场景 | P 的工程摘要 + 既有 schema receipt 的实际路径/摘要；Session 临时材料在 Cleanup 前复制 | receipt 的断言/候选判定优先；工程摘要补诊断，不能把失败 receipt 包装成成功 |
| package/verify-lifecycle | 原有 Bundle/manifest/verification/安装 receipt 保持；verify 父摘要引用它们，不要求改 package 实现 | 正式交付按既有合同留存，不能按普通报告 TTL 删除；本批不改 release-candidate |
| CI job / quality-result | 每 job 的实际步骤结果与 R/summary.json、steps.json；汇总关联 scope、head/base、job 与 artifact | 成功仅必要摘要；失败白名单；artifact 不是长期结论的唯一依据 |

工程摘要 schema 为 `gopulse.engineering-run.v1`，含 identity/run_id、revision/VERSION、
输入与工具摘要、scope、必需/实际步骤、command/child 退出码、开始/结束 UTC、耗时、清理
和复现。成功没有空错误文件；发布仅 summary.json/steps.json/errors.txt/reproduce.txt，
总量 20 MiB、单份摘录 2 MiB；完整原始材料不截断，不重复抄终端成功输出。

普通成功 7 日、普通失败 30 日；P/R 合计每 identity 上限 512 MiB。到期按摘要的 ended_at UTC
计算，不能用 mtime；报告启动时持工作区锁检查到期项，容量不足时再检查，结束时核验容量。
仅清理身份/用途已确认、已结束且到期的普通报告；活动/接续状态引用、正式结论引用和
未知归属/无效摘要均保护，报告原因。无可清项而超限明确非零，不覆盖必要依据。
不新增保留标签、全仓索引或独立清理目标；借用已有 state 和结论引用辨认用途。

原始凭据按本次真实值脱敏，无法验证的文件不发布。Cookie/token/环境原文不上传；
先捕获主失败、复制容器报告，再停进程/清理，最后写清理结果。禁止跟随符号链接越出身份根，
只清理本次拥有的资源。devtools 与 acceptance 的 internal 包不互相导入；工程 schema
可各模块编码并用同一 JSON fixture 校验一致性，receipt 格式保持。
H～D 先引用既有测试/CLI 的实际结果与同名日志必要结论；E 接入后 G 复核最终报告，
不为早期段另造报告执行器，也不补造过去未生成的报告。

CI artifact 名固定 `engineering-<head>-<scope_id>-<job-id>`；scope_id 区分主线调用与
docs-only 子调用，避免同一 run/head 的两个 docs-check 或 quality-result 覆盖/上传冲突。

### 3.5 文档检查的方案豁免

dev/imple/Phase-*/ 的实施方案及 dev/design/ 下已登记的设计正文可以声明待实现命令，
但仅豁免本设计 §3 与本文件 §3.1 的明确接口白名单；对应正文必须标明“待实现”。
不是整目录忽略：链接、锚点、模块、正文归属、冻结 diff 和其他命令仍检查。
历史正文按原候选解释；冻结原文不为首扫清零而改写，当前入口断链必须修复。
本批到 F 后各入口应真实存在，G09 必须取消本批白名单豁免仍通过；配置例外随完成删除。
新的未来设计需有已登记正文和准确待实现接口，不以方案目录逃避任意错命令。

### 3.6 CI 调用、工具缓存与完成保护

保留现有 `workflow_call` 的 branch/base_ref/run_product_checks 输入与默认行为、job 名、
身份/token 校验、update merge/保留分支及产品 squash。不在本批新增手动 dispatch：
[GitHub 要求手动触发工作流进入默认分支](https://docs.github.com/en/actions/how-tos/manage-workflow-runs/manually-run-a-workflow)，
不能用尚未进入 main 的触发器作为合并前的固定门禁。

auto-pr-merge 增加只读 `docs-only-scope`，在 develop push 上用已取回的 HEAD 父提交计算
差异，输出 docs_only/docs_base；须有实际文档差异且所有产品 check 为 false。
只在此条件成立时增加 `docs-only-regression`，复用 quality-gates：branch 为实际分支、
base_ref 为该父 SHA、run_product_checks=false；子调用只运行治理/docs-check/quality-result。
R0 的产品父提交和必要 docs-only 提交可一次 push；命令为
`git push origin HEAD:develop/2.5.6`，用 `gh run list --workflow auto-pr-merge.yml --branch develop/2.5.6`
按实际 head 找 run，再 `gh run watch <run-id> --exit-status`。日志“远端验证/docs-only”登记
父/head、run URL、子调用 job 和未启动产品的证据。主调用始终用 origin/main 做完整 push CI，
子调用不能替代它，也不含 PR/合并动作。

Go job 恢复 actions/cache 的 `.run/tools`，key 绑定 OS/架构、toolchains 文件摘要、Go 版本和
lint 固定版本；随后 `make bootstrap TOOLS_ONLY=1` 再校验缓存版本/SHA256。冷 miss 各 job
最多一次下载，不假设 15 个 runner 共享下载；缓存不代替验证，下载计入 30 分钟 job 上限。
Go/前端 30/20、integration business/observe 30/25、e2e business/observe 25/30、
Compose 60、docs-check 5 分钟；quality-result 必须 always 汇总失败并验证报告。

治理仍先做既有目标 VERSION/同名日志校验，结果只代表 metadata_ready；对外的
completion_ready 改由 quality-result 在检查实际必需结果后输出。本批再由
ci/validate_quality_result.py 核对日志状态、R1 SHA、G10 结果/CI 引用。R1“收口核验中”
输出 false；R2 的“收口记录就绪”只是登记就绪，只有本次主调用全部成功才输出 true。
docs-only 子调用不输出产品完成信号。测试 pending/登记就绪/缺引用/身份不符/当前 CI 失败；
既有批次保留元数据判据并同样等实际质量结果。open-and-merge 使用 always 后显式核验依赖：主质量必须成功，
子调用被选择时必须成功，未选择时允许其条件跳过；失败/取消阻止 PR，合法 skip 不阻断
普通产品或 update 收口。保留原身份与 completion_ready 条件，增加对应 DAG 行为测试。

cache-warm 改为消费同一清单，保留仅 main 执行条件；分支用 fixture/静态检查验证矩阵。
main 合并后的真实预热作为只读跟踪项，合并前不得记已跑，也不作为产品质量门禁。
Go 缓存依赖路径取各模块实际 go.sum，无该文件时回退 go.mod；F4 覆盖无 sum 的工具模块。
release-candidate 的双架构/PROMOTE 链不改、不触发、不列允许文件；本批交付门禁为 G07/G10。

## 4. 验证映射与固定门禁

| 验证对象 | 已有覆盖 | 最低有效层级 / 本批缺口 |
| --- | --- | --- |
| 文档与命令 | 治理版本/分支测试，无 docs-check | Go 临时仓库 fixture + 已编译 CLI；真实文档-only CI 留 G08 |
| 工具和依赖 | digest、devrun/testenv 的已有 Go 测试 | Go 输入/锁/失败行为 + CLI；真实下载、npm ci 与复用留 A2/B2/G01/G02 |
| lint 和范围 | check 的 gofmt/vet、两前端类型检查；quality_scope 行为测试 | 原始 JSON/diff fixture + 真实 linter；15 Go + 2 前端首扫不可用 mock 替代 |
| 诊断和归属 | proc、workspace、Session/receipt 测试 | 真实失败子进程/临时目录/取消/容量边界；真实容器复制与清理留 G06 |
| verify/CI 结果 | quality_scope、auto-pr workflow 测试 | Python 选择/汇总行为、Go CLI 调度与工作流静态合同；真实 push 调用留 G08/G10/G11 |
| 开发、交付 | integration/e2e、Compose、package/lifecycle 现有真实入口 | 真实系统；不能用文本断言或短探针代替 |

门禁以下列编号为准。L 指同名日志的“验证/<编号>”，只写必要结论/计数/退出码与已有
结果定位；P/R 是 §3.4 的实际运行目录，不能预填 run_id。E 前的段检查记录 L 与真实
测试/CI 输出定位；E 后引用 P/R。G 记录每项实际候选、输入、命令、退出码及结果路径。
Go 测试前用 `go test -list` 确认对应 Test 前缀有实际用例，零用例不算通过。
每行列出的正常命令都必须为 0；标记 **N** 的子场景明确预期非零且必须核验原因。

### 4.1 H～F 分段门禁

| 编号 | 判据 / 层级 | 固定命令或方式 | 期望结果与证据 |
| --- | --- | --- | --- |
| H1 | 文档规则，Go + CLI | `go -C devtools test ./internal/quality ./cmd/devenv -run '^TestDocs'`；正常/断链/错锚点/重命名/代码块/错模块/冻结 diff 临时仓库 | 测试 0；**N** 真正错误 CLI 非零；方案豁免只限白名单，其他检查仍触发；L/H1 |
| H2 | 当前正文与调用回归 | `make docs-check BASE_REF=0610f3f`；`make check MODULE=devtools`、test、race | 0；当前入口无回归，冻结原文/历史材料未改；L/H2，最终 P/docs.json |
| A1 | 工具前置与参数，Go + CLI | `go -C devtools test ./internal/quality ./cmd/devenv -run '^TestDoctor'` 和同路径 `-run '^TestBootstrap'`；工具缺失/版本不兼容/SHA 不符/坏 MODULE | 测试 0；**N** 各 CLI 非零、无后续安装/检查；L/A1 |
| A2 | 真实准备与复用 | `make doctor`；两次 `make bootstrap TOOLS_ONLY=1`；受控副本错摘要 | 前两项 0，第二次无下载/重装；**N** 错摘要非零且无成功标记；L/A2、实际版本/来源/摘要 |
| B1 | 安装输入/锁，Go 行为 | `go -C devtools test ./internal/frontenddeps ./internal/devrun ./internal/testenv` | 0；覆盖首装/复用/lock/Node/npm/OS 变化、失败无标记、并发互斥与在用应用保护；L/B1 |
| B2 | 三处调用与双应用回归 | 两应用分别 `make check MODULE=<app>`、test、build；CLI 行为用例核对 devrun/e2e/Make 使用同一安装判断 | 0；无第二份判断、普通调用不装整套工具；安装输入/次数与 L/B2 |
| C1 | 15 Go 全量扫描与分类 | 按 list-go-modules 逐个 `make lint MODULE=<module> BASE_REF=0610f3f`；Go 原始结果/diff fixture | 0，新增=0、完整存量不丢、配置明确来自根；**N** 新增/扫描器错非零；L/C1、各模块 lint.json 和扫描退出码 |
| C2 | Go 严格模式 | `make lint MODULE=backend`；受控存量/新增、删除/重命名/主线推进 fixture | 无告警为 0；**N** 有告警非零，工具错误与存量分开；L/C2 的条数/退出码，不能把严格非零算新增门禁失败 |
| D1 | 两前端规则/parser | 两应用 `make lint MODULE=<app> BASE_REF=0610f3f`；`go -C devtools test ./internal/quality -run '^TestFrontendLint'` | 0、新增=0；五项规则逐项实际触发、Vue/TS parser 与 diff 分类正确；**N** 新增/解析错非零；L/D1、lint.json |
| D2 | 全模块严格合同与 lock 回归 | `make lint-all`；两应用 lock 变化后实际 npm ci，再 check/test/build | lint-all 零告警为 0，有存量为 **N**，必须 scan_error=false、总数和所有 17 份报告齐全；应用回归 0；L/D2 |
| E1 | 报告/容量/期限/路径，真实文件 | `go -C devtools test ./internal/diagnostics ./internal/workspace`；临时身份根降额容量 fixture、ended_at 过期/mtime 改动、共享 .run、符号链接 | 0；到期可用项删除，活动/接续/正式引用/其他身份不删；**N** 无可清空间或未知路径非零，原始数据完整；L/E1、目录前后差异 |
| E2 | 失败/超时/取消/清理，真实子进程 | `go -C devtools test ./internal/proc ./internal/testenv ./cmd/devenv`；`make test MODULE=acceptance`；report/receipt fixture | 0；**N** 失败子命令非零，主失败与清理结果分开，容器报告复制先于定向删除；L/E2、P 与实际 receipt |
| E3 | 脱敏与生产入口 | 对 §3.4 每类入口检查最终摘要；注入本次生成的 token/Cookie/秘密错误文本，核对 R 白名单 | 成功只有必要文件；失败材料可复现，原始凭据不进入 R/日志；20/2 MiB 上限生效；L/E3、P/R 文件列表/摘要 |
| F1 | selector/汇总兼容，Python + Go | `python3 -m unittest discover -s ci -p 'test_*.py'`；`go -C devtools test ./internal/quality -run '^TestVerify'` 和同路径 `-run '^TestPlan'` | 0；旧 key/type/转换兼容；共享规则、.golangci、toolchains、docs、权限/部署/未知路径均按 §3.3；L/F1 |
| F2 | 清单与只读计划，实际 CLI | `make -n list-go-modules`、list-npm-modules；实际 list-* 与 Make 清单比对，PATH 工具 tripwire；`make verify BASE_REF=0610f3f PLAN_ONLY=1` | 0；15 Go + 2 前端；tripwire 不触发、无运行产物；stdout JSON 的字段/步骤/ref/diff 都正确；L/F2、必要 JSON 快照 |
| F3 | verify lint 与错误退出，CLI | `make verify MODULE=backend`；正常/存量/新增/坏 ref/取消/清理失败临时仓库；BASE_REF 与 MODULE 的 PLAN_ONLY | 严格模式存量为 **N**，报告不能声称成功；BASE_REF 模式只新增阻断；非法组合非零；L/F3、P/summary.json、steps.json |
| F4 | CI、完成保护与缓存，静态 + 行为 | 治理测试含 test_validate_quality_result/test_auto_pr_workflow；validator 的成功/缺失/失败/取消/skip/head 漂移 CLI fixture；cache-warm 矩阵生成 fixture | 测试 0；**N** 无效结果非零；R1 pending 不能开 PR；fetch-depth/ref/cache/父差异调用与 job 名保持；L/F4 |

### 4.2 G 组合验收（对应设计 G01～G09）

R0 的 G00～G09 必须齐全；R1 按 G10 重验版本敏感集合；R2 按 G11 收口。
同候选且相关输入未变的重合步骤只执行一次，编号引用实际结果，不能给未跑步骤造证据。

| 编号 / 对应设计 | 固定命令 / 场景 | 期望结果与证据 |
| --- | --- | --- |
| G00 / 全模块前置 | `make check-all`；按两个 list 清单逐模块 check、BASE_REF lint、test/race/build（前端无 race）；治理单测、validate_versions、development 分支校验 | 全部 0，15 Go + 2 前端齐全，新告警=0；L/G00、各实际步骤/报告、存量快照；可引用 G04 的重合步骤 |
| G01 / G01 | `make doctor` → `make bootstrap` → 再次 bootstrap | 0；真实冷准备与复用、工具版本/摘要、未重装证据；L/G01、P/summary.json/steps.json |
| G02 / G02 | 独立受控副本改变一个应用的有效 lock 摘要，调用其 Make/test 入口；再用不合法 lock 触发真实 npm ci 失败 | 正常 0，仅该应用实际重装；**N** 坏 lock 非零且无成功标记/续跑；L/G02、输入摘要/安装次数与失败报告 |
| G03 / G03 | `make verify BASE_REF=<实际主线ref> PLAN_ONLY=1`；代表 docs/单模块/共享/权限/部署/未知 diff fixture | 0；JSON、17 模块映射/必需步骤准确，docs-only 只治理/docs，stdout 无杂文且无产品副作用；L/G03、计划快照 |
| G04 / G04 | `make verify BASE_REF=0610f3f` | 0；required_steps 与实际完整对应、无新增 lint、清理通过，和权威 CI 对齐；L/G04、P/R 摘要与 scope |
| G05 / G05 | `make deps` → dev → stop；dev-observe → 就绪 → stop；两 integration、两 e2e、`make verify-compose` | 全部 0；既有独有断言通过，真实应用就绪/浏览器运行，资源归属/清理正确；L/G05、工程报告与既有 receipt；可复用 G04 的真实步骤 |
| G06 / G06 | 实际端口冲突/测试失败/清理失败；integration/e2e/Compose 各一个受控代表失败，使用本次副本/端口/资源，不改冻结原件 | **N** 非零且原因符合注入；先复制必要诊断，主失败/清理分开，外部资源保持；L/G06、失败 P/R、原 receipt，不用继续正常全矩阵诊断 |
| G07 / G07 | `make package PLATFORM=linux/amd64 RUNTIME=1 OUTPUT=<本次专属目录>`；`make verify-lifecycle INSTALL=clean PLATFORM=linux/amd64 MANIFEST=<该目录>/release-manifest.json` | 0；Bundle/manifest/verification/安装 receipt 同候选；安装/HTTP/启停/卸载与隔离断言通过，自有 registry/安装资源清理；L/G07 与这些原始制品，不用 PROMOTE |
| G08 / G08 | R0 权威 push CI + docs-only-regression；`gh run watch <run-id> --exit-status`；`gh run download <run-id> --name engineering-<head>-<scope_id>-quality-result --dir <本次目录>`；下载同 head 的代表诊断 artifact | 0；log“远端验证”有实际 run/head/base/job；正常必需 jobs 全成功，预期失败子进程的真实非零报告可下载且脱敏；不靠 continue-on-error；R 中无秘密、结论依据可定位 |
| G09 / G09 | F 已删除本批待实现接口白名单后，`make docs-check BASE_REF=0610f3f`；受控副本断链/错模块/冻结 diff | 正常 0；**N** 各真实错误非零；当前索引可定位、冻结/历史原件未改；L/G09、P/docs.json |
| G10 / 版本敏感重验 | R1 `python3 ci/validate_versions.py`、completion 分支校验；`make verify BASE_REF=<R0-sha>`；G05 的源码开发序列、G07；同 R1 权威 push CI 和报告核验 | 全部 0；VERSION 强制全量范围，前端依赖/build、integration/e2e/Compose、Bundle/安装重新绑定 2.5.6；日志记录 R0/R1 两 SHA、实际回执/CI 和复用理由；G08 同样的报告标准 |
| G11 / 完成登记 | R2 治理、版本/分支 completion 校验、docs-check；R2 权威 push CI（发生文档父差异时也跑子调用） | 0；只有日志/状态/必要正文差异，发布候选保留 R1；实际最终 CI 由 run head 和摘要定位，自动 PR 在其绿后；不为补最终 run URL 再使用完成分支 |

Go/前端存量严格非零属于已声明的 N 场景；正常 G00/G04/G10 lint 显式传 BASE_REF。
报告的正常结果与预期失败结果分开，不能把意外取消/跳过解释成 N。清理失败即使主任务成功
也非零；长期依据先按已有合同归档并核验可读性/摘要，不能只有会到期的 CI artifact。

### 4.3 失败分类

| 分类 | 判定与处理 |
| --- | --- |
| 预期负向 | 编号明确的 N；实际错误/退出码/清理与预期一致才通过该测试，保留必要失败报告；不是忽略一般失败 |
| 产品或工具失败 | 新 lint、错误选择/漏检查、解析/断言/归属/脱敏/清理缺陷、摘要与 receipt 不符；修最低受影响层，再只重验失效集合 |
| 基础设施失败 | daemon/buildx/registry/网络/runner/安装源不可用；首次停止整组，最多两次各 10 分钟最小诊断，不重跑全矩阵 |
| 合同或身份不符 | ref 不存在、候选/manifest/head/job 漂移、缺结果、无法判定 secret/用途；停止该门禁，查明事实再修计划/输入，不能更换回执身份 |

## 5. 分段执行与强制停点

字母沿用设计分段；H 先行，使文档改动从第一段起可检查。这里是文件内顺序，不拆批、
版本或分支；段边界核对是已授权执行的继续条件，满足即可继续，不逐段另请确认。

| 段 | 实现结果 | 门禁编号 | 段末必须核对 |
| --- | --- | --- | --- |
| H | docs-check、待实现接口有限豁免与纯 Make 模块清单；当前入口修复、历史边界 | H1/H2 | 文档/冻结无回归，清单可供后续扫描 |
| A | toolchains 配置、doctor/bootstrap、项目内下载校验/复用 | A1/A2 | 版本/下载身份真实，冷准备成本已记录 |
| B | frontenddeps 的摘要、成功标记、安装锁；三处共用 | B1/B2 | 两应用回归和输入变化/失败/在用保护成立 |
| C | Go 三项 lint、全量/新增分类与根配置 | C1/C2 | 15 Go 扫描齐全、新增为 0；严格非零已分类 |
| D | 五项前端规则、固定 parser/依赖、共享纯规则与 lock | D1/D2 | 两前端扫描齐全；17 模块严格报告完整 |
| E | 私有/发布报告、失败捕获、目录身份/期限/容量/脱敏/清理 | E1/E2/E3 | 入口生产方表已覆盖、原 receipt 保持、无误删/泄密 |
| F | 选择器 JSON/步骤、本地 verify、CI/caches/汇总/完成保护；删除本批接口豁免 | F1～F4 | 必需映射无遗漏；docs-only 子调用与 pending 保护可用 |
| G | R0 完整验收→R1 版本敏感重验→R2 完成登记/最终 CI | G00～G11 | 对应候选/制品/回执/CI 无漂移，§9 完成条件齐全 |

每段末报告实际完成/失败/未开始、已耗/剩余、下一段准备/执行/清理成本。最低层失败先在
该层修正；不把未通过段带入真实矩阵。超限/未知基础设施原因按 §8 停止，不借后段预算。

## 6. 允许文件与复用边界

| 段 | 允许新增/修改文件 |
| --- | --- |
| H/A/F | 根 Makefile；devtools/cmd/devenv/{main,main_test}.go；devtools/toolchains.json；devtools/config/documents.json |
| H/A/C/D/F | devtools/internal/quality/{docs,doctor,bootstrap,lint,verify,plan}.go 及对应测试/testdata；必要的 devtools/go.mod/go.sum |
| B | devtools/internal/frontenddeps/deps.go 及测试；devtools/internal/devrun/{applications,devrun_test}.go；devtools/internal/testenv/e2e.go 及测试；两前端 Makefile |
| C/D | .golangci.yml；15 Go 模块的 Makefile；devtools/config/eslint-rules.mjs；两前端 eslint.config.mjs、package.json/package-lock.json |
| E | devtools/internal/diagnostics/report.go 及测试/testdata；devtools/internal/workspace/{workspace,workspace_test}.go；devtools/internal/testenv/{testenv,processes,e2e}.go 及测试；devtools/internal/proc/proc.go 及测试；frontend/playwright.config.ts |
| E | acceptance/internal/harness/{session,session_test}.go；acceptance/internal/scenario/{compose,suites,lifecycle}.go 及对应测试；acceptance/cmd/gopulse-acceptance/{main,main_test}.go；工程摘要合同 JSON fixture 放 acceptance/internal/contracts/testdata |
| F | ci/{quality_scope,test_quality_scope,validate_quality_result,test_validate_quality_result,test_auto_pr_workflow}.py；.github/workflows/{quality-gates,cache-warm,auto-pr-merge}.yml |
| 各段/G | 根 AGENTS.md/README.md、ci/AGENTS.md、dev/README.md、dev/validation/local-development-tests.md；本总/分方案与工程设计；dev/status/capability-status.md；同名实际日志 |
| G 的 R1 | VERSION、.env.example、两前端 package.json/package-lock.json、deploy/runtime-contracts.json 的版本字段；仅 R0 门禁全部成功后与实际日志提交 |

新增辅助放相应现有包，复用 proc/workspace/digest、Session/Cleanup/receipt 与场景。
不修其他模块产品源码、不重写现有治理 Python、不升级交付镜像/公开验收 schema。
ci/validate_quality_result.py 是缺失结果/身份/完成状态的必要增补，保留既有 validate_branch
与 sync_version_metadata。新增依赖须固定版本、真实用途及对应验证。
release-candidate.yml 不在范围；cache-warm 的 main 真实运行限制见 §3.6，静态检查是 F4
的固定项，不能把缓存运行与产品测试混为同一完成事实。

## 7. 预算与实验成本

本批未开工；以下均为未测估算，按[成本与接续](../../rules/implementation-execution-budget.md)
使用同一累计执行窗口，**预计 1,890 分钟（31 小时 30 分钟），上限 2,900 分钟（48 小时 20 分钟）**。
修订原 1,200/1,800 的依据是：原预算没有装下 R1 全量重验、三次权威 CI 及报告核验/清理。
这是本次规划的成本修正，不是执行触顶后的自动延期，也不新增批次。

| 段 | 预计 / 上限（分钟） | 包含的工作 |
| --- | --- | --- |
| H | 130 / 200 | 文档解析/CLI fixture、Make 清单/当前引用、devtools 直接检查 |
| A | 90 / 140 | 工具兼容/下载身份、冷准备与复用 |
| B | 140 / 200 | 摘要/安装锁/三处接入、两应用直接回归 |
| C | 130 / 200 | 15 Go 首扫/分类/严格行为、根配置 |
| D | 130 / 200 | 两 parser/五规则/lock/分类与严格全仓报告 |
| E | 190 / 280 | 报告/生产方、期限容量/身份、失败捕获/脱敏/receipt 兼容 |
| F | 180 / 280 | JSON/步骤映射、verify、CI/缓存/完成保护与治理 |
| G | 900 / 1400 | 下表的两轮真实验收、三次权威 CI、报告/登记/清理 |

所有本地准备、实现、构建、扫描、依赖下载、运行、诊断和清理，以及**远端 CI 排队/等待**
均计入累计窗口；不用“活跃时间”排除等待。并行任务按实际重叠墙钟计一次，不把全部 CI
job 的运行时长串行相加。中间 push 的 CI 成本计在实际所属段，不能当作免费。
每段上限内硬预留至少 20 分钟停止/清理；这是预留用途，不是“上限减预计”的差额。

G 的实验/验证成本如下，预计列合计 900、上限列合计 1,400，已包含在段预算内：

| 执行块 | 数量、窗口与方法 | 预计 / 上限（分钟） |
| --- | --- | --- |
| 干净 checkout/冷准备 | 两候选的环境/工具/依赖身份，尽量复用有效缓存 | 40 / 60 |
| 17 模块检查/扫描/测试/构建 | R0/R1 两轮；lint 全量与缓存分类；每轮预计 70、上限 120 | 140 / 240 |
| 源码开发就绪与退出 | 两候选 deps/dev/dev-observe/stop；每轮 15/20 | 30 / 40 |
| integration | 两候选各 business+observe；每轮预计 40、上限 30+25 | 80 / 110 |
| e2e | 两候选各 business+observe；每轮预计 40、上限 25+30 | 80 / 110 |
| Compose 闭包 | 两次原有完整窗口；每轮预计 45、上限 60，不缩短断言/等待 | 90 / 120 |
| Bundle 与隔离安装 | 两次 linux/amd64 构建/验证/安装/启停/卸载；每轮 50/80 | 100 / 160 |
| 受控负向与报告边界 | G02/G06/G09、TTL/容量、脱敏/代表 CI 失败子报告核验 | 50 / 90 |
| 远端 CI | R0/R1/R2 三次权威 push，每次预计 60、上限 90（含排队）；docs-only 子调用同 run 并行，不另加一整轮 | 180 / 270 |
| 正文/日志/证据核对、清理与诊断余量 | 包含最终只读状态获取、实际结果定位和有界资源清理 | 110 / 200 |

无新增容量或持续运行实验，required experiment duration 为 0；真实链路的原有等待/
测量窗口仍完整执行。Go/前端 job 上限含冷工具准备；若 A/C/D 实测或 runner 排队显示
后续集合不能落在剩余预算内，在强制停点登记具体剩余方案，不缩短窗口或假定 CI 免费。
CI 单次等待上限 90 分钟、三次共 270；到限保留 run 身份并停止本次等待，不取消他人作业。

本批专用停点：每段上限的 50%/80% 与段末，以及总上限的 50%/80%（1,450/2,320 分钟）
报告已完成/剩余/成本/下一步；只在同名日志写必要进度，不恢复全仓普通任务的时间台账。
从第一次实施预检累计，跨会话/候选修订/重试不重置，段上限之和就是总上限。
昂贵命令开跑前，剩余必须覆盖准备、完整执行与 20 分钟清理；不借其他段未用额度。

## 8. 停止、接续与证据失效

基础设施首次失败停整组，同原因最多两次、每次 10 分钟最小诊断，计入原段；未证明原因
不重启全矩阵。产品失败先修最低受影响层，只重验失效集合。触顶、诊断无法推进或用户
停止时保存候选、真实通过/失败/未开始、必要诊断与具体剩余，再有界清理本次资源并停止。
接续需要具体剩余方案与用户明确指令；不拆批/加版本/重开计时，不用规划修订自动恢复。

| 变动 / 版本敏感依据 | 必须重新验证 / 可复用边界 |
| --- | --- |
| VERSION / .env.example / runtime-contracts 版本 | validate_versions；VERSION 的全量 scope/verify/权威 CI；运行时镜像 tag、配置与观测目录身份；Bundle/manifest/verification、lifecycle 安装 receipt；R1 必须全量 |
| 两前端 package/lock 版本字段 | frontenddeps 失效/实际 npm ci，check/test/build 版本字段和两 e2e；R1 不能复用 2.5.5 前端制品 |
| package/Bundle/manifest | 重新建同候选制品、摘要和运行验证，再隔离安装/启停/卸载；不能给 R0 回执换 VERSION/SHA |
| devtools/acceptance 源码、工具/config/依赖、环境 | 受影响直接测试、诊断/选择器/真实调用方；改变必需集合或环境时原步骤不得冒充有效 |
| 只有日志/能力状态/必要正文（R2） | 新治理/docs-check 与 R2 权威 CI；R1 运行/Bundle/安装输入未变可复用，日志写原 revision/输入摘要与理由，发布身份仍为 R1 |
| 身份/合同/制品或来源无法确认 | 停止，保留原证据，修正输入或明确缺项；不编辑 receipt、manifest 绑定或人工拼接通过 |

每轮证据保留实际 revision、VERSION、环境、方法与关键原始数据，日志“候选与失效”
逐项说明 R0→R1 的重验及未重验理由。仍有诊断/接续/结论用途的报告不按普通 TTL 删除。
只读获取 R2 的最终 CI 状态可以在 PR/完成后进行，不能为追加 URL 再改已完成开发分支。

## 9. 统一收口

以下缺一不可；未通过或未执行就保留实际进度，不作完成声明：

1. H～F 实现与编号门禁全部成立；R0 的 G00～G09、R1 的 G10、R2 的 G11 都有真实结果。
   正常检查 0，严格 lint 存量及其他 N 按原因/条数核验；无新增 lint、扫描器错误或漏步骤。
2. `2.5.6` 元数据是在 R0 成功后与同名实际日志进入 R1，版本敏感制品与回执重新绑定 R1。
   R1 日志为“收口核验中”；R2 日志为“收口记录就绪，最终 CI 待确认”，记录已实际通过的
   R1 结果，不预写 G11 成功。最终完成由同 R2 的权威 CI 摘要与实际结果确认，不凭日志
   存在就开 PR，也不凭开发模式允许目标 VERSION 提前同步。R2 从 Git 定位，避免自引用 SHA。
3. 同名日志有实际交付、必要验证结论、偏差/限制；“远端验证”含 R0/R1 的实际 run URL、
   head/base/job 与结论；R2 最终 run 由 CI 摘要/精确 head 定位并在最终交付说明链接。
   报告和日志经实际凭据核对无秘密，不逐条复制成功输出。
4. capability-status 只登记已实现的文档检查、工具准备、依赖失效、lint 新增门禁、诊断
   与范围/结果共用能力，以及存量 lint 和 cache-warm main 未实跑等限制；不扩大容量/
   恢复/生产支持声明。删除已落地接口的临时豁免，当前正文/导航能定位。
5. 按实际 state、标签、PID birth 与安装/registry 身份核对清理结果，无本次意外残留或
   外部误清；必要原始证据在可靠位置可读且摘要匹配，发布副本和期限/容量边界有效。
6. 本任务文件使用英文 Conventional Commits，成功提交不夹带他人改动；固定门禁绿后
   按根 PR/合并规则收口。PR 打开/批次完成后停止使用开发分支；main 缓存预热仅只读
   跟踪，未跑就保持该限制，问题记录后续项。
