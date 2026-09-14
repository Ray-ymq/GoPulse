# Phase-17-01：统一运行时契约与服务可诊断闭环实施方案

> 目标版本：`1.14.1`
> 开发分支：`develop/1.14.1`
> 运行与验收平台：开发期最小包检查可在当前环境运行；Compose/信号/候选门禁使用真实 Linux `amd64`

## 1. 批次目标

本批在 Phase 16 完整 Compose 产品上建立所有长运行 Go 组件共享、可机器校验的运行时合同，并关闭配置、Probe、优雅退出、Request ID、结构化日志和 API 错误的跨组件差异。

```text
typed environment configuration
              │
              ▼
machine-readable runtime contract
              │
              ├─ startup / live / ready / health
              ├─ bounded graceful shutdown
              ├─ request id + JSON log
              └─ stable safe API error
              │
              ▼
diagnosable Compose service lifecycle
```

本批只完成“进程如何安全启动、对外表态、处理 HTTP、诊断和退出”。Schema 演进、Rabbit/Kafka 状态提交和告警可靠性留给 Phase-17-02。

## 2. 前置条件

- 从 `1.13.6` 已合入的最新 `upstream/main` 创建 `develop/1.14.1`，不得从 `update` 或旧开发分支起步。
- Phase 16 的 release manifest、Bundle、共享 lifecycle、唯一 edge、双 Frontend 和 Linux `amd64` evidence 可读取。
- 开工时枚举真实长运行组件：Backend、Business Worker、Search Indexer、Router、Marshaller、Monitor、Redis/MySQL/RabbitMQ/Kafka/Elasticsearch/VictoriaMetrics Exporter。
- 核对当前 `.env.example`、Compose anchors/environment、各 config loader、HTTP/private listener、Docker healthcheck、plugin manifest 和 `stop_grace_period`；记录差异后直接进入实现，不开展一般性代码审计。
- 确认 Linux `amd64` Docker server、独立 acceptance project 和不冲突端口；不要求 Kubernetes。

## 3. 实施范围

### 3.1 机器可读运行时合同

- 新增版本化合同（建议 `deploy/runtime-contracts.json`）及 JSON schema，逐组件记录环境变量归属、类型、敏感性、默认/必填、兼容别名、监听器、Probe、硬/软依赖、检查超时、退出预算和版本字段。
- 新增校验器，将合同与 `.env.example`、`deploy/compose.yaml`、Docker healthcheck、plugin manifest 和已登记组件目录对照。
- 校验器拒绝：组件遗漏、重复 ID/端口、未登记的 Compose-owned key、Secret 被标为非敏感、Probe 路径冲突、`stop_grace_period` 小于退出预算、兼容别名无期限或实现/合同版本不一致。
- release manifest/Bundle 收录合同和 checksum，使 Phase 17 evidence 与 Phase 18 直接消费同一份合同。

合同用于校验实现，不在运行时引入第二套动态配置中心，也不允许 acceptance runner 重写产品配置语义。

### 3.2 配置收口

- 保留现有环境变量为唯一输入层；各进程在监听或消费前一次性完成 typed load、范围、URL/host、runtime mode、超时关系和跨字段校验。
- 复用已有 loader 和安全验证函数，抽取的公共 primitive 只处理确实相同的字符串、整数、duration、host/URL、Secret-presence 等规则，不把不同组件的业务默认值强行合并。
- 已有 key 无冲突时不重命名。确需 canonicalize 时支持至多一个兼容别名；两者值冲突时失败，日志只输出 key 名与 `invalid_configuration`。
- 缺失/非法 Secret、DSN、URL userinfo、token 复用和容器内 loopback 误配必须在启动前失败，不打印原值。
- 启动日志只记录合同版本、组件/产品版本、runtime mode 和非敏感监听边界；不转储环境或有效 Secret hash。

### 3.3 三类 Probe

- 为 12 个长运行 Go 组件提供无 query/body 的 `GET /startup`、`GET /live`、`GET /ready`；Phase 16 的 `GET /health` 保留为 `/live` 兼容别名。
- 响应使用稳定 JSON、HTTP `200/503`、`Cache-Control: no-store` 和 JSON content type；非 GET 返回稳定 `405`，query/body 返回 `400`，未知路径返回 `404`。
- `/live` 不调用外部依赖；`/ready` 使用有界、限并发检查和缓存/状态机，不能由高频探测创建无界 goroutine 或日志风暴。
- Backend readiness 只把 MySQL、当前 Schema 和 bootstrap 约束视为硬依赖；Redis/RabbitMQ/Elasticsearch/VictoriaMetrics/Monitor 按来源降级，不因管理或可观测故障阻断社交入口。
- Worker/Indexer 在既有私有组件监听器上暴露 Probe；readiness 分别绑定所需 DB/store 与有效 Rabbit consumer session。
- Router readiness 绑定 Kafka/topic/producer；Marshaller 绑定 Kafka 和三类目标存储；Monitor 绑定插件状态目录/catalog 与可恢复的 Router 发布路径。
- Exporter 在目标源不可用时保持 live/ready 并以既有 `up=0`/安全状态表达来源故障；避免 Monitor 将来源故障误判为插件进程崩溃。
- Docker healthcheck 转向 `/ready`；不新增宿主发布端口。新增 Probe 不经过唯一 edge 暴露给浏览器。

### 3.4 统一优雅退出

- 在现有 `componentmetrics` 共享预算上收口，而非为每个资源串联独立完整 timeout；如需扩展公共 primitive，保持各 Go module 的最小依赖面。
- 信号到达后先把 readiness 置为 `stopping/503`，再停止接收新 HTTP、delivery、fetch、采集轮次、告警轮次和插件操作。
- HTTP server 使用有界 `Shutdown`；Worker/Indexer 停止 delivery 并完成或 requeue 当前消息；Marshaller 取消 poll/ownership 后才关闭；Monitor 停止新 plugin operation 并回收子进程。
- 限时关闭内部 metrics/probe listener、log/event shipper、producer/consumer 和存储连接；相同 deadline 不得被重复延长。
- 正常 SIGTERM/SIGINT 完成退出 `0`；超时、server/consumer 不可恢复错误退出非零。Compose `stop_grace_period` 大于应用预算并留出固定余量。
- 关停测试覆盖 idle、一个在途 HTTP、一个在途 consumer、依赖已断开和第二次 signal；验证无新工作进入、无 goroutine/子进程遗留和最终退出码正确。

### 3.5 Request ID 与 HTTP 链路

- edge 生成或替换 32 位小写十六进制 ID，防止外部调用者注入任意日志关联字段；Go 服务校验内部传播值，缺失时自行生成。
- Backend、Monitor、Router、Marshaller 的响应头、请求上下文、访问日志和下游 HTTP 调用使用同一 ID；认证/授权不依赖该值。
- Backend 错误包络追加同一 `request_id`；Monitor/Router/Marshaller 的 JSON 错误统一采用 `{error:{code,message,request_id}}`。
- 404、405、无效 JSON、认证失败、权限拒绝、上游不可用、panic 和内部错误均映射到固定状态/code；已提交响应的 panic 只写安全日志。
- 两个 Frontend 继续按 status/code 处理，并为未知 code、网络失败和过期会话显示通用安全状态，不展示 raw response 或服务端异常。

### 3.6 结构化日志与安全原因码

- Backend 现有 logging、Monitor/Router 的 `slog` 和 Marshaller logging 对齐单行 JSON schema；统一 `log_schema_version`、UTC timestamp、level、service、module、message、event、version/revision。
- HTTP、依赖状态切换、启动/停止、consumer 和插件操作使用有限枚举；请求/消息/operation ID 仅在存在时追加。
- 替换可能包含凭据、URL、路径或 payload 的直接 `err.Error()` 日志，使用 safe error/reason code；详细原因只在不含敏感数据且有长度上限时进入私有诊断附件。
- 为持续 dependency down、Probe 失败和重试日志增加状态变化/限速语义，保留一次恢复事件。
- 更新 Marshaller 严格日志 mapping/vocabulary，保证新增字段在发送前通过；禁止最终 acceptance 通过丢弃不兼容日志来“通过”。

### 3.7 Compose 与产品集成

- 更新 Compose healthcheck、环境变量和 stop grace，保持唯一 edge、internal network、read-only filesystem、Secret 和 project ownership 不变量。
- lifecycle `doctor/verify/status/logs` 读取并校验运行时合同；状态只展示安全枚举，不泄露 Secret 或内部 URL。
- acceptance 增加配置负向、Probe 状态转换、HTTP 错误关联、SIGTERM 排空、日志 schema/Secret scan 和内部端口负向场景。
- 对任一观测软依赖停止/恢复时，固定验证社交代表流程仍可运行；与该来源直接相关的页面/API 返回 partial/unavailable 并能恢复。

## 4. 不在本批范围

- 新 migration、Migration CLI 扩展或直接前序数据升级。
- 改变 RabbitMQ retry/dead、Kafka commit/rebalance 或告警 lease 的持久状态语义。
- 新业务 API、页面、插件、指标或告警来源。
- Kubernetes manifest、Probe 配置、Service 或 Secret 对象。
- 一般性模块合并、日志平台重写、配置中心、热重载或全仓依赖升级。

## 5. 建议实施顺序

1. 固定组件/配置/端口/依赖/预算清单和 schema，先让校验器在当前差异上失败。
2. 对齐 config loader 与安全失败，再实现共享 Probe 状态机和组件 readiness adapter。
3. 对齐信号、单一退出预算和 Compose grace，完成 idle 与在途退出测试。
4. 对齐 edge/Go Request ID、错误包络、Frontend 映射和 JSON 日志 schema。
5. 更新 Compose、Bundle/lifecycle 合同和 acceptance 场景。
6. 在最终 diff 上运行固定门禁一次，更新实施记录和版本并停止。

## 6. 预计直接影响文件

- `deploy/runtime-contracts.json` 及 schema、release manifest/Bundle 收录逻辑
- `componentmetrics/` 中共享 Probe 状态、私有 listener 和关停预算
- Backend、Router、Marshaller、Monitor、六 Exporter 的 config/runtime/http/logging 代码与直接测试
- Business Worker、Search Indexer 的 main/runtime 状态 adapter 与直接测试
- `deploy/compose.yaml`、Frontend/Admin Frontend edge/error 处理及直接测试
- `scripts/ci/verify_runtime_contracts.py`、Phase 17 runtime acceptance 入口和自测试
- 运行时、配置、Probe、错误和诊断文档
- `dev/logs/Phase-17/Phase-17-01-统一运行时契约与服务可诊断闭环.md`
- `VERSION`、`.env.example` 和双 Frontend 版本元数据

实际文件可按现有目录等价调整，但不得为统一而复制第二套 listener、logger 或生命周期实现。

## 7. 批次验收标准

### 7.1 合同与配置

1. 12 个组件全部被机器合同覆盖；合同、Compose、`.env.example`、plugin manifest 和代码登记无漂移。
2. 代表性缺失、非法范围、错误 runtime host、Secret 重用和 alias 冲突在监听前失败，退出非零且输出不含输入值。
3. 合法 Phase 16 配置继续启动；无必要 key 重命名，兼容期和冲突行为有测试。

### 7.2 Probe 与退出

1. 四个路径的方法/请求/响应/缓存语义一致；`/health == /live`，`/live` 无依赖 I/O。
2. 每个组件的硬/软依赖矩阵通过：硬依赖故障 `ready=503`，软依赖故障仍能执行其代表性主流程。
3. readiness 检查限时限并发；高频/卡死 checker 不造成无界 goroutine、资源或日志增长。
4. SIGTERM/SIGINT 先撤 readiness，再于预算内排空退出；超时非零、Compose 可重启，无 orphan plugin/进程。

### 7.3 错误、日志与产品边界

1. 同一 Request ID 可从 edge 响应头关联到 Backend/内部 HTTP 日志和错误 body；畸形外部 ID 不被信任。
2. 所有 JSON API 代表错误、404/405/invalid JSON/panic 使用稳定 envelope/code，两个 Frontend 安全展示未知错误。
3. 全部 Go 组件的启动、请求、依赖变化和关停日志通过 schema；Secret、内部 userinfo、绝对路径和 payload 扫描无命中。
4. 新 Probe 未增加宿主或浏览器可达入口，`user/super_admin` 授权与唯一 edge 不变。

完成条件：以上全部通过、无阻断问题、同名实施记录如实完成、版本元数据为 `1.14.1`，本批提交已创建。

## 8. 固定验证命令与回归范围

以下为本批固定门禁；计划新增入口由本批实现，等价改名必须先同步计划：

```bash
python3 scripts/ci/verify_runtime_contracts.py --contract deploy/runtime-contracts.json --compose deploy/compose.yaml --env .env.example
(cd componentmetrics && go test -count=1 ./... && go test -race -count=1 ./...)
(cd backend && go test -count=1 ./cmd/server ./cmd/business-worker ./cmd/search-indexer ./internal/config ./internal/http/...)
(cd router && go test -count=1 ./...)
(cd marshaller && go test -count=1 ./...)
(cd monitor && go test -count=1 ./...)
scripts/verify-runtime-contracts.sh --candidate 1.14.1
(cd frontend && npm test -- --run && npm run build)
(cd admin-frontend && npm test -- --run && npm run build)
scripts/verify-compose.sh
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/1.14.1 --base-ref upstream/main
git diff --check
```

`verify-runtime-contracts.sh` 必须包含六 Exporter 的直接 Go package 测试和真实容器 Probe/配置/信号场景；若直接修改某个 Exporter，额外在其 module 运行 `go test -race -count=1 ./...`。`scripts/verify-compose.sh` 只在最终 diff 上运行一次，因为共享 runtime、edge、Compose 和全部长运行组件均被直接影响。

不运行 Phase 16 backup/recovery、历史升级、Kubernetes、跨架构或性能矩阵。本批通过后，Phase-17-02 若未修改这些 runtime 文件，可复用该成功结果而不重跑。

## 9. 实施记录与交接

完成前创建 `dev/logs/Phase-17/Phase-17-01-统一运行时契约与服务可诊断闭环.md`，至少记录：

- 真实组件/配置/端口/硬软依赖/预算清单及与开工基线的差异；
- 实际修改文件、合同/schema 版本和 Bundle digest；
- 每条固定命令、结果、失败轮次和最小修复；
- Probe 转换、signal 耗时、错误/Request ID、日志 schema 与 Secret scan 证据；
- 偏差、兼容别名、已知限制和非阻断后续项。

交给 Phase-17-02 的固定输入是已合入主线的 `1.14.1` 运行时合同、三类 Probe、统一关停、Request ID/错误/日志语义和直接通过证据。达到完成条件后停止，不提前修改 Migration 或消息状态机。
