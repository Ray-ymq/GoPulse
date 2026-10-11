# Phase-24-04：管理端阶段页面与原生验收

> 状态：已分配，尚未实施。目标版本 `2.6.4`，分支 `develop/2.6.4`。
> 结果：管理员能查看 V1 阶段依据；现有 acceptance 具备同候选固定矩阵、失败回执、归属清理及证据核验能力。
> 正式 V01～V12 的动作和通过条件由 [05](Phase-24-05-V1冻结验收与阶段收口.md)定义，本批只开发、直接验证和短预检。

## 1. 进入条件与允许文件

03 已进入主远端 main；状态 API 的授权、阶段/质量语义、日志 strict mapping 和预算已验证。
从最新主远端 main 创建本批分支，复用管理布局、observability services、前端时间格式和浏览器认证助手。
正式验收前置缺口在本批解决，不留给 05 在矩阵中修改工具。

| 允许修改 | 文件/目录与具体目的 |
| --- | --- |
| 页面/路由 | 拟新增 `admin-frontend/src/views/BusinessStagesView.vue` 及 Vitest；`router/index.ts`、`components/AdminLayout.vue`、`styles.css` 和对应导航测试 |
| 类型/服务/关联 | 拟新增 `admin-frontend/src/services/businessStages.ts` 及测试；`types/observability.ts`、`services/observability.ts`、`views/ObservabilityLogsView.vue` 及测试，仅增加固定关联过滤/跳转 |
| 浏览器/本机选择 | 拟新增 `frontend/e2e/business-stages.spec.ts`；已有 business/compose-business/admin 用例按需复用；`devtools/internal/testenv/e2e.go` 及选择行为测试 |
| 原生场景与 fixture | `acceptance/internal/scenario/`、`harness/`、`receipt/`、`contracts/`、`cmd/gopulse-acceptance/` 及直接测试；既有 fixture 和拟新增 `acceptance/internal/fixtures/business-fault-proxy/`，不复制栈管理器或采样器 |
| 命令/测试部署 | 根 `Makefile` 的 verify-business 参数透传；现有 `deploy/phase16-acceptance.yaml`、`deploy/docker/acceptance.Dockerfile` 与仅用于验收的 owned override；必要 `acceptance/go.mod` / `go.sum` |
| 正文/结果 | 拟新增 `dev/validation/business-v1.md`；`dev/validation/README.md`、`local-development-tests.md`、`dev/contracts/business-stages.md`、`admin-frontend/README.md`、直接验收入口 README；同名日志、能力状态、总/分方案结果标记和成功版本元数据 |

不修改业务行为、03 状态语义、正式阈值或增加生产故障控制接口。
新增 fixture 只在验收私网/独立进程使用，不能进入正式应用路由。
若直接检查证明需要产品修正，先保存失败依据，在合法范围内明确修订对应合同后处理，
不靠 UI 或回执隐藏产品结果。

## 2. 管理端使用流程

管理路由新增 `/business-stages`，复用现有布局及实时管理员授权。
进入时通过 02 的用户可用 command context 取得当前 epoch，管理员也可显式选择历史 epoch；
一次只填写 command/event/post 中一种身份。点击查证发起一个请求，默认手动刷新，
离开页面或新查证取消旧请求，晚到响应不能覆盖新对象。

页面分别显示：命令提交、Broker 确认记录、搜索应用完成、当前查询可见、日志/Trace 线索。
每项同时展示结果、来源、读取/完成时间、适用 epoch/generation/revision。
已提交但搜索未观察到时保留两项不同结果；日志报错或缺 Span 不将已提交变成失败。
legacy、无依据、保留过期、来源失败与 Trace 查询未配置有各自提示，未知不涂成“没执行”。
应用完成后 ack 是否成功仍可能未知，不增加依据不足的“消息已确认”绿色阶段。

当前可见性是明确的可选检查。请求超过预算或被拒给出固定错误和手动刷新，
不隐藏失败、自动无限重试、扫描全部帖子或持续轮询。
页面只读，不提供重放/删除/强制完成/换 epoch 等操作。
不展示正文、命令键、request digest、Cookie/凭证或原始 SQL/依赖错误。

日志跳转限定到已解析的精确身份及有界时间窗口，Logs 页面实际接受这些 typed 过滤。
Trace 只展示/复制已有 ID 和能力说明；未配置后端时不能生成假链接。
普通用户 NewPostView 保持 02 的提交/安全重试流程，不增加完整诊断页。

## 3. 原生入口扩展

当前 `verify-business` 仅运行已有浏览器业务 suite。
它缺少持久阶段断言、稳定丢响应/确认控制、逐 case 回执和原始证据核验；
在现有 acceptance 中补齐这些缺口，不重建已退役的 Phase 18～21 runner。

本批拟实现以下参数，01～03 不能提前把它们当成已运行的门禁：

| Make/CLI | 固定合同 |
| --- | --- |
| `verify-business SCOPE=v1` / `business --scope v1` | 保留默认旧 suite；v1 要求 manifest/receipt，使用 pinned 生产镜像，缺身份拒绝而非源码回退 |
| `MANIFEST`、`RECEIPT` | 透传现有 CLI 参数；检查完整 revision、镜像 digest、Bundle、合同/配置与工具一致，原始记录自动保存在 owned 私有运行目录 |
| `PREFLIGHT=1` / `--preflight` | 只运行依赖/身份/权限及一条真实创建→完成→页面的短检查；回执 mode=preflight，不可作为完整矩阵通过 |
| `BUDGET=30m` / `--budget 30m` | 正值、有限时间预算，传递到场景和子进程；故障窗口不可压缩；剩余不足以执行及清理时停止 |
| `VERIFY_EVIDENCE=1` / `--verify-evidence` | 只校验已生成 receipt/raw，不能启动栈或补写案例；核对 hash、身份、状态/时间和清理事实 |
| `PUBLICATION` / `--publication` | 验证通过后按白名单生成摘要，再核验实际选定文件；不复制原环境、凭证、SQL dump 或完整请求正文 |

正式执行不接受 keep、不允许换 manifest、不重用同名 mutable tag。
工具每个 case 写 passed/failed/not_started、失败分类、计划/实际动作、身份、关键时刻、
raw 路径及 checksum；发生错误仍原子落盘，并保留失败和清理错误两个事实。
原始回执不能人工编辑、拼接或换绑候选。

Go 业务断言与 harness 栈管理分开，复用候选装配、账号准备、session、ports、
owner 资源清理、现有 lifecycle/manifest 库。runtime proxy 只补现有工具没有的
HTTP/AMQP/MySQL 响应或确认丢失边界；确定性测试必须证明“真实已提交/已写入、结果未到达”，
不能仅模拟返回 500 或没有 Span。代理不新增业务数据库池；所有客户端纳入共享预算。
ES/Router/OTLP 延迟可复用既有 HTTP fault fixture，避免另建通用环境管理器。
OTLP 场景复用现有 `deploy/phase20-trace.yaml` 与 Collector 配置，在验收 owned profile 中装配；
默认产品栈不因此增加新的 Trace 存储角色。

保留现有 `gopulse.acceptance.v1` 外壳，增加版本化 V1 evidence section，
列明 candidate/manifest/Bundle/source/contract/tool/config、epoch/代际、环境/负载及逐案例记录。
validator 拒绝未知/缺失身份、未开始案例冒充完成、缩短窗口、raw hash 不符、
业务计数不一致、额外正文/凭证字段和 cleanup 未通过。
对负例、错误退出、取消、非归属资源保护和 publication 再校验有实际测试，
不能仅以 JSON key 存在检查代替行为验证。

跨 package/build/独立检查的累计成本仍由执行者按同名日志核对；
工具内 budget 不代表自动覆盖所有外部准备命令，不能声称若干 timeout 就是批次总限时。

## 4. 已有覆盖与直接检查

| 对象 | 复用/缺口 | 本批验收 |
| --- | --- | --- |
| UI 质量与并发 | 既有 Dashboard/Logs Vitest；缺阶段 DTO | 已提交+缺日志、旧 generation、来源失败/过期、删除、晚到响应、取消及权限变化各有组件测试 |
| 实际管理流程 | 既有 Playwright 认证/同源；缺新页面 | 浏览器真实创建→查询页面→精确日志跳转；普通用户拒绝；同 Cookie 降级立即失权 |
| 原生 scope/身份 | 既有 acceptance parse/candidate/session | 默认 suite 不变；非法参数、缺 manifest、错误 digest、preflight 误当 formal、错误退出和取消均失败 |
| 故障控制 | 既有 HTTP fixture；缺提交/ack 丢失 | fixture 自测和最小真实依赖检查确认丢的是指定阶段结果；未指定对象/协议不能无限阻塞 |
| 证据/清理 | 既有 receipt/session 清理 | 注入 missing case、short window、bad hash、wrong epoch、cleanup 失败、敏感字段，validator 全部拒绝；无归属资源不删 |

新浏览器文件必须接入 native observe 选择和 acceptance v1 scope；
不能因文件未被选择而报告 e2e passed。已有业务编辑/删除/搜索回归仍保留。

## 5. 固定门禁与短预检

现有门禁，在仓库根执行：

```bash
rtk proxy make verify MODULE=admin-frontend BASE_REF=upstream/main
rtk proxy make verify MODULE=acceptance BASE_REF=upstream/main
rtk proxy make verify MODULE=devtools BASE_REF=upstream/main
rtk proxy make e2e SCOPE=observe
rtk proxy make check-compose
rtk proxy make docs-check BASE_REF=upstream/main
```

下列是本批待实现的扩展；直接测试通过后才可执行。登记 PHASE24_* 的私有绝对路径与
受本任务管理且保持存活的 registry。先提交开发候选，在干净 checkout 使用实际当前版本
`2.6.3` 构建；这是 04 的短预检身份，不伪称已完成 `2.6.4`。

```bash
rtk proxy make package PLATFORM=linux/amd64 OUTPUT="$PHASE24_PREFLIGHT_CANDIDATE" REGISTRY="$PHASE24_REGISTRY" RUNTIME=1
rtk proxy make verify-business SCOPE=v1 MANIFEST="$PHASE24_PREFLIGHT_MANIFEST" RECEIPT="$PHASE24_PREFLIGHT_RECEIPT" PREFLIGHT=1 BUDGET=30m
rtk proxy make verify-business SCOPE=v1 MANIFEST="$PHASE24_PREFLIGHT_MANIFEST" RECEIPT="$PHASE24_PREFLIGHT_RECEIPT" VERIFY_EVIDENCE=1
```

预检只解锁 05，不发布完整 V1 passed。05 将在完成的 2.6.4 revision 上建立新的身份，
受版本/制品输入变化影响的检查不可换绑到 04 回执。

真实验证预计 90 分钟、累计上限 180：必要构建/环境 30/60、浏览器及短预检 30/60、
fixture/证据负例诊断 15/30、核验及清理 15/30。一次一个 owned stack，
一次 preflight 项目，就绪最多 7 分钟、短链路最多 60 秒、清理最多 5 分钟；
无本阶段正式 60 秒故障矩阵或容量实验。最多两次各 10 分钟最小诊断计入总额。
剩余不足或基础设施失败时停止，不能以开发新工具重新计时。

完成条件：页面、权限、精确关联和所有直接检查通过；V1 工具/validator 已具备
05 全部动作和失败语义；短预检与清理通过，同名日志记录实际候选而非未来版本。
成功后同步 `2.6.4`、治理校验并提交；完整 V1 能力仍待冻结正式验收。
