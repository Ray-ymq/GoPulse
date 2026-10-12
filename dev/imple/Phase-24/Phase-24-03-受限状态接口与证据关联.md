# Phase-24-03：受限状态接口与证据关联

> 状态：已完成。目标版本 `2.6.3`，分支 `develop/2.6.3`。
> 结果：业务领域提供受限状态查询，平台按对象/代际关联已有 Logs，并分别表达各来源的结果和质量。
> 来源为 B10/B7、M6/M7/M9，阶段字段及数值以[总方案](Phase-24-总实施方案.md)为准。

## 1. 进入条件与允许文件

02 的命令/义务和 v2 生产已启用、固定门禁及日志进入 main。
从最新主远端 main 创建本批分支，先读 API/roles、businessstate、search、logquery
及现有管理权限中间件。平台可以访问账号/角色事实，但不能直接读取业务阶段表。

| 允许修改 | 文件/目录与具体目的 |
| --- | --- |
| 领域状态 | `backend/internal/businessstate/`，新增有界只读 service/DTO、私有 handler/client 和直接测试；复用领域 Repository |
| 受限搜索可见性 | `backend/internal/search/{elasticsearch,contract,service}.go` 及测试，仅单 ID/代际查证，不暴露任意 query DSL |
| 平台适配/权限 | `backend/internal/http/api.go`、管理 handler、相关 router/integration 测试；`middleware/` 仅服务凭证与现有实时授权复用；`backend/cmd/server/roles.go` 及角色测试；`backend/internal/config/` |
| 关联生产与查询 | `backend/internal/logquery/`；`backend/internal/observability/{logging,tracing}/`；`post/handler.go`、`outbox/dispatcher.go`、`worker/handler.go`、`search/processor.go` 及直接日志/Span 测试 |
| 严格 Logs 通路 | `monitor/internal/logs/`；`marshaller/internal/logs/`、`internal/elasticsearch/{client,client_test}.go`、`internal/retention/{elasticsearch,elasticsearch_test}.go`；只补字段校验、映射与保留核验 |
| 凭据/运行接线 | `deploy/compose.yaml`、`deploy/runtime-contracts.json` / `.schema.json`、`deploy/docker/frontend/nginx.conf`、`.env.example`；`lifecycle/internal/control/` 内环境生成/保护及对应测试；`devtools/internal/{envfile,devrun,testenv}/` 的必要配置和真实链路选择 |
| 合同/实际结果 | `dev/contracts/business-stages.md`、`runtime-contracts.md`、`phase20-trace-and-freshness.md` 的新增适用字段、`backend/README.md`、`monitor/README.md`、`marshaller/README.md`；同名日志、能力状态、总/分方案结果标记和成功版本元数据 |

不修改 componentmetrics 或发布新库版本：现有 JSONLogger 已允许非保留属性。
Router 按原日志 Envelope 传输，无确认缺口不改其源码。
不装配新的 Trace 数据库、查询服务、后台扫描器或跨域 SQL join。

## 2. 领域只读接口与授权

本批拟新增：

- business 私网 `GET /internal/v1/business-stages`：epoch 必填，command_id/event_id/post_id 三选一；`check_visibility` 默认为 false。
- platform 公共 `GET /api/v1/observability/business-stages`：相同对象选择，增加受限 Logs 时间窗口与包含来源选择。

私网请求必须同时有专用随机服务凭证和原管理会话。凭证至少 32 字节熵、恒定时间比较，
只注入业务服务与平台适配器，lifecycle 私有环境生成并保护；公共 DTO/日志/回执不携带凭证。
business 根据转发的真实会话重新读取账号和管理权限，不信任任意 user_id/role header。
管理角色撤销后原 Cookie 下一次查询立即被拒；认证/授权依赖不可用时不能缓存授权继续返回业务详情。

platform 只调用业务接口；`BUSINESS_STAGE_ENDPOINTS` 是受信私网 origin 列表，最多两个，
一次请求只选一个上游，无重试/hedging 或用户控制的地址。私网端口不发布到宿主；
同源 nginx 的 `/internal/` 保持 404。Worker/Indexer 不装配查询路由。
combined 公共 handler 经同样授权调用本地领域接口，仍走相同槽位/期限，不依赖 loopback HTTP。
独立 platform 不创建业务 ES client、业务完成 Repository 或新的业务连接池。

字段格式、互斥选择、未知 query 参数、时间窗口与响应大小均严格校验。
私网凭证错误、未登录、普通用户、错误角色、缺 epoch/多对象各有真实拒绝测试。
错误响应沿用安全固定码，不返回 SQL、DSN、上游响应原文或帖子正文。

## 3. 状态读取与当前可见性

领域接口只按索引访问单对象关联的命令、Outbox、必要消费者、完成和最近失败摘要。
每次最多 20 条摘要，超出显式 truncated；数据库期限 500 ms。
读取结束释放数据库连接/事务后，才按需要请求 ES。
每 business 实例专用 2 个查询槽，平台业务 RPC 最多 4 并发；不排队等槽，
超限返回 `503 business_stage_busy`。RPC 2 秒、平台总期限 3 秒，取消传递到 SQL/HTTP。
这些预算复用总方案 52/60 连接合同，不新增诊断池或静态“余量”副本。

响应包含 requested/current epoch、resolved command/event/post、路由修订及逐来源 evidence。
附带当前 epoch 的未闭合数、跨 epoch 保留预算/上限及 80% 水位，读取固定预算行，不扫描统计；
水位属于当前运行状态，不用于推导某一历史对象阶段。
post_id 只按指定 epoch 关联创建命令；旧 epoch 不 join 同号的新帖子。
历史最小命令/完成可以按原 epoch 返回，标明 stale_epoch；不查询旧 epoch 的“当前正文/可见性”。
历史 v1 没有新记录时返回 legacy_unknown；不存在结果与已过保留窗口不能合并成“从未执行”。
expired 只在来源保留边界可核验时使用；不能证明清理原因的缺记录仍是 no_evidence。

受限可见性检查只针对已解析的一个 post_id，解析实际 current alias/物理 generation，
检查索引 epoch，通过 ES 查询而非实时 GET 证明此时搜索可见，并用 MySQL 确认当前事实仍可见。
最多一轮查询；不调用 refresh、不扫描全表、不退化为全库关键词搜索。
结果仅含 matched、epoch/generation、版本和读取时间，明确它是单对象本次索引查询的结果，
不保证所有关键词/分页结果或永久可见。缺失事实不返回正文。
alias 切换/epoch 不符/查询失败分别表达，旧 generation 的应用完成不填成新 generation 已完成。

持久数据、实时可见性、日志各自有 source/read_at/适用版本。
总体响应不是跨 MySQL/ES/Logs 的原子快照；某来源失败仍保留已经授权取得的其他来源，
但不能将 backend_error 填成零、false 或 recovered。
无法授权时拒绝整个请求，不能借“部分成功”绕过权限。

## 4. 关联字段与遥测质量

只为 V1 增加 `command_id`、`data_epoch`、`projection_generation`、
`routing_contract_revision`、`consumer` 等适用字段；保留既有 event/post/content_revision、
trace/span/attempt ID。UUID、索引名、路由修订和消费者均有格式/长度或有限枚举校验。
命令键及摘要不进日志，所有高基数关联身份不进入普通指标 label。
不扩展共享指标目录；新增 HTTP 路由沿用现有固定 `_unmatched` 指标分类，
查证预算和水位通过领域只读 DTO 表达，不借新增路由发版 componentmetrics。

按以下顺序安装：Monitor/Marshaller 字段读取和 strict mapping 兼容 →
更新已有 Logs index 的 mapping/retention 核验 →
业务日志/Span 生产启用 → 真链路查询。
新字段是 Logs v1 的可选扩展，旧日志不回填身份，不因缺字段被拒绝或强行归入当前 epoch。
同时修改 transform、template、existing-index mapping update 和 retention 精确字段检查，
不能只改某一 allowlist。

LogQuery 只接受 typed 的对象/epoch 过滤，单次最多 100 条、24 小时窗口和一次后端请求。
关联须用确切身份；旧日志缺 epoch 时列为历史相关线索，不能证明当前阶段。
Logs 后端错误/保留过期/未观察分别表达；无日志不等于无业务执行。
成功已提交但后续错误日志存在时，页面 DTO 不能退回“创建失败”的业务状态。

Span 沿用标准 OTLP、当前非阻塞有限 Batch 队列和采样合同。
当前 Collector 只有受控文件输出，公共接口不提供伪造 Trace 查询链接：
返回已知 Trace ID、查询能力 not_configured，以及适用采样/缺失说明。
应用完成不依赖日志入库或 Span 导出，诊断不创建逐遥测全局完成表。

## 5. 已有覆盖与新增直接检查

| 对象 | 复用与缺口 | 最低有效层和本批断言 |
| --- | --- | --- |
| 角色/实时授权 | roles、management、same-origin 测试；缺私网域接口 | Go + HTTP integration：凭证与管理 Cookie 缺一即拒；降级即时生效；错误角色/public internal 404 |
| 有界读取 | 原准入/取消助手；缺诊断专用限额 | 测试内阻塞 SQL/HTTP，2/4 槽满时拒绝、普通业务/探针仍能进展；取消释放，最多 10 秒阻塞 |
| 阶段语义 | 01/02 持久记录；缺查询 DTO | 真实 MySQL/ES：published≠完成、完成≠新代际可见、删除不暴露、old epoch 不同号关联、truncated/过期/未知 |
| 严格日志传播 | Monitor/Marshaller validation/template/retention 测试 | 字段从真实业务进 Logs/存储/LogQuery；非法字段拒绝、旧日志仍可读、已有 index mapping 与保留核验继续通过 |
| 来源部分失败 | 原 Logs/Trace 失败测试；缺跨来源质量 | 业务/Logs/ES 单侧替身失败返回准确来源质量；无 Span 和错误日志不推翻持久成功 |

确定性边界用 Go 测试；新增字段必须另有真实 integration 证明，
仅检查源码字符串或页面 mock 不构成关联完成。

## 6. 固定门禁、预算与完成

```bash
rtk proxy make verify MODULE=backend BASE_REF=upstream/main
rtk proxy make verify MODULE=monitor BASE_REF=upstream/main
rtk proxy make verify MODULE=marshaller BASE_REF=upstream/main
rtk proxy make verify MODULE=lifecycle BASE_REF=upstream/main
rtk proxy make verify MODULE=devtools BASE_REF=upstream/main
rtk proxy make integration SCOPE=business
rtk proxy make integration SCOPE=observe
rtk proxy make check-compose
rtk proxy make docs-check BASE_REF=upstream/main
```

均为仓库根现有入口；新增状态/日志真实检查必须接入实际执行范围。
模块入口包含 race，按原 baseline lint 口径报告；无需为新增属性发版 componentmetrics。

真实验证预计 75 分钟、累计上限 150：准备/必要构建 25/50、业务/观测真链路 30/60、
有限诊断及归属清理 20/40。项目串行，就绪最多 7 分钟、单次异步/字段入库最多 60 秒、
清理最多 5 分钟。最多两次各 10 分钟最小诊断计入上限；基础设施失败停整组。

全部直接检查与固定门禁通过后，同批维护 API/字段/权限/预算正文及同名日志，
登记只读查证已实现、UI/正式故障矩阵仍待后批。成功后同步 `2.6.3` 元数据、
治理校验并提交；不宣布未配置的 Trace 查询已可用。
