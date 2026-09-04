# Phase-09-01：Backend 日志端到端查询闭环实施方案

> 权威目标版本与开发分支以 `Phase-09-总实施方案.md` 第 3.2 节为准：本批对应 `1.6.1` / `develop/1.6.1`。
>
> 当前状态：已完成；Pull Request #81 已于 2026-09-04 通过远程门禁并 squash 合入 `main`。

## 1. 批次目标

以真实 Backend API 日志为第一条产品纵向切片，一次性交付非阻塞日志 Push、LogMonitor 接收和第一次清洗、Router logs 路由、Marshaller logs 二次处理、Elasticsearch 独立索引以及 Backend admin 查询：

```text
真实 API → Backend Schema v1 stdout + 有界异步 Push
        → LogMonitor → Router → Kafka → Marshaller
        → gopulse-logs-v1-* → Backend admin 查询
```

本批完成后，管理员必须能使用一次真实请求响应中的 `X-Request-ID` 查询到对应 HTTP 完成日志和业务成功日志；未登录与普通用户必须在 Elasticsearch 调用前分别得到 `401` 和 `403`。本批不是模块骨架批次，不能把写入、查询、授权或真实链路留给 Phase-09-02。

本批仅接入 `service=backend` 进程中的 HTTP、业务、cache、Outbox和 lifecycle 日志。Business Worker、Search Indexer、search-reindex 的远程 sink 接线以及完整故障/运维矩阵属于 Phase-09-02；它们现有 stdout 行为不得回归。

## 2. 前置条件

- 开工前成功 fetch 主远程，确认 Phase 8 全部能力已合入最新 `main`，根与 Frontend 版本为 `1.5.4`；若基线不一致，先更新 Phase 9 总/拆分方案。
- 从最新 `upstream/main` 创建 `develop/1.6.1`，不沿用 `update` 或 Phase 8 分支。
- 核对 Phase 4 Backend Schema v1、request ID、保留/敏感字段，Phase 7 Router Envelope/Topic，以及 Phase 8 Marshaller ownership/commit/replay实现和测试。
- 在 WSL2 Linux filesystem 中实施并使用唯一 Docker daemon；保存 Git、日常进程、端口、Compose project/container/network/volume、插件根和日志快照。
- 仅阅读 Backend logging/config/server/auth、Monitor HTTP/Publisher、Router Envelope/routing、Marshaller Envelope/processor/writer、Elasticsearch与直接测试；不得以本批为由进行一般代码审计。

## 3. 实施范围

### 3.1 Backend 非阻塞日志 sink

- 在 Backend 日志基础包附近建立可注入的异步 shipper，使同一 Schema v1 JSON bytes先写 stdout，再尝试非阻塞加入内存队列。
- 每个队列项生成一个 32 位小写十六进制 message ID，并在全部重试中复用；POST body保持原始单条日志，`Idempotency-Key` 使用该 ID。
- 单 worker、单队首在途、固定容量、HTTP timeout、有限指数退避、redirect拒绝、响应上限和有界 shutdown 必须可确定性测试。
- queue full只丢远程副本；LogMonitor/Router不可用、鉴权失败或关闭超时不得改变 HTTP响应、Outbox状态或 Backend进程主要退出语义。
- 发送状态通过 stdout-only logger记录固定状态转换，不能经同一 sink递归发送；不得记录日志正文、token、URL、响应 body或底层错误。
- `LOG_MONITOR_URL` 为空时保持 stdout-only；配置非空时严格校验 loopback HTTP URL、专用 token、timeout、queue、retry与 shutdown范围。

### 3.2 LogMonitor 接收与第一次清洗

- 在现有 Monitor HTTP Server 增加 `POST /internal/v1/logs`，使用独立 `LOG_MONITOR_INGEST_TOKEN`，不复用插件管理 token。
- 只接受单条 `application/json`、无 `Content-Encoding`、最多 64 KiB、唯一且合法的 `Idempotency-Key`。
- 本批的 `service` 只允许 `backend`；module、message和可选字段按总方案第 7.2 节严格校验，重复/未知字段、任意嵌套对象、控制字符和敏感哨兵均拒绝。
- 用 header message ID、日志 timestamp和 canonical payload生成 `type=logs,source=backend` 的 Envelope v1，并通过既有 HTTP Publisher调用 Router。
- 只有 Router `202` 才向 Backend返回 `202`；明确无效输入返回安全 `4xx`，Router/Kafka不可用返回 `503 transport_unavailable`。
- Monitor health和插件管理接口保持兼容；LogMonitor故障不得停止 MetricsMonitor或 Plugin Manager。

### 3.3 Router logs 顶层路由

- Router validator 接受既有 `metrics/redis` 与新增 `logs/backend`，继续严格要求 Envelope唯一字段、UTC时间、payload object和 key/ID一致。
- `logs` 继续路由到 `gopulse-observability-v1`；Router不解析 payload、不重新序列化、不增加 Topic。
- 请求鉴权、body limit、Kafka确认、超时不确定写入和安全错误保持 Phase 7 语义。
- 增加代表性测试证明 unsupported source/type仍返回 `422`，合法 logs value写入Kafka后与HTTP body逐 byte一致，metrics 路由无回归。

### 3.4 Marshaller typed handler 与 Elasticsearch writer

- 把公共 Envelope 校验与 metrics payload校验解耦，通过显式 registry分派 `metrics/redis` 和 `logs/backend`；保持 metrics decoder/transformer现有全部负向测试。
- logs handler 对 payload 再次执行递归重复 key、未知/缺失字段、service/source、timestamp、module/message、可选字段类型/范围和敏感边界校验。
- 把合法 payload确定性映射为严格日志 Document；`timestamp` 改名为 `@timestamp`，其余只保留白名单字段。
- 新增 Elasticsearch client/writer，幂等确保 `gopulse-logs-v1-template`，按 Envelope UTC日期写 `gopulse-logs-v1-YYYY.MM.DD`，以 message ID作为 `_id`。
- writer只接受有界成功响应；网络、timeout、非成功、模板不兼容或结果不确定均不提交。相同 record重放生成完全相同 index、ID和正文。
- 复用 Phase 8 Processor 的 ownership/commit状态机：成功写入且 lease有效后提交；永久无效不调用 writer并安全提交；暂时失败不提交并退避。
- Marshaller `/ready` 增加 Elasticsearch 状态，日志中区分 metrics/VM与logs/ES但不输出文档、URL或响应；`/health` 保持纯 liveness。

### 3.5 Backend 管理员日志查询

- 新建独立 logs package，包含 contract、options/cursor、Elasticsearch repository、service和handler；不复用帖子 Document、alias或可变 index输入。
- repository只读 `gopulse-logs-v1-read`，使用 PIT + `search_after`，排序固定 `@timestamp desc,_shard_doc desc`；`_shard_doc` 仅作为PIT游标内部值，解析响应时再次校验index前缀、字段类型和最大响应大小。
- `GET /api/v1/observability/logs` 必须位于 Authentication 与 RequireAdmin之后；未登录/普通用户测试使用 fake repository断言调用次数为零。
- 首次默认最近 15 分钟，最大 24 小时；支持总方案固定 exact filters、`limit=1..100` 与 2 分钟签名 cursor。服务端把实际时间窗、filters、limit、PIT 和最后 sort 固化进独立 domain key签名的cursor；翻页请求只能单独携带cursor。
- 成功只返回安全 DTO与 `meta.next_cursor`；空 alias返回空页；存储不可用返回 `503 logs_unavailable`，无底层错误。
- 本批至少支持按 `request_id` 查询真实 API 日志，并证明一个 request ID下的 HTTP完成记录和业务成功记录可关联。

### 3.6 最小生命周期、验收与 CI

- `.env.example`、`scripts/dev.sh`、`down.sh`、`verify.sh` 增加本批必要配置、传递、启动/关闭和只读检查；完整后台源与故障资源矩阵留给下一批。
- 建立 `scripts/verify-logs.sh --self-test` 的配置/资源/查询负向检查与默认的最小真实 API纵向模式。
- 默认验收使用随机独立基础设施、端口、凭据、数据库、Kafka观察和日志文件；真实注册/admin提升/登录/发帖或评论产生日志，再经 Backend查询。
- 增加或扩展 CI，使四个 Go module直接测试、脚本/Compose静态门禁和代表性真实 Logs纵向验收成为固定检查。
- 更新根 README、Monitor/Router/Marshaller说明及当前 HTTP契约，创建实施记录并同步版本。

## 4. 接口与数据合同

### 4.1 LogMonitor 请求

```http
POST /internal/v1/logs
Authorization: Bearer <LOG_MONITOR_INGEST_TOKEN>
Content-Type: application/json
Idempotency-Key: <32-lowercase-hex>
```

请求正文就是一条 `service=backend` 的 Phase 4 Schema v1日志。成功响应固定为：

```json
{"status":"accepted"}
```

不提供 batch、GET logs、删除、重放或状态修改接口。

### 4.2 Backend 查询

```http
GET /api/v1/observability/logs?request_id=<id>&limit=50
Cookie: gopulse_session=<admin-session>
```

响应示意只表达形状，真实字段按记录省略：

```json
{
  "data": [
    {
      "timestamp": "2026-09-04T12:00:00Z",
      "level": "info",
      "service": "backend",
      "module": "http",
      "message": "http request completed",
      "request_id": "0123456789abcdef0123456789abcdef",
      "method": "POST",
      "route": "/api/v1/posts",
      "status": 201,
      "duration_ms": 3,
      "response_bytes": 100
    }
  ],
  "meta": {"next_cursor": null}
}
```

响应不包含 transport message ID或 Elasticsearch `_id`；幂等证据从受控内部验收读取，不扩展产品 DTO。

## 5. 实施边界与非目标

- 不接入 `business-worker`、`search-indexer`、`search-reindex` 远程 sink；只保证其 stdout与构建不受影响。
- 不实现 EventMonitor、Frontend日志页、全文检索、聚合、告警、ILM、日志删除、磁盘 spool或多 Topic。
- 不修改 RabbitMQ业务 Envelope、Outbox migration、帖子搜索 mapping/alias、VictoriaMetrics指标 mapping或普通用户 API。
- 不把 HTTP `202` 前的内存队列描述为 durable；不为本批建立跨进程恢复或 crash后重放。
- 不以增加日志为由记录用户名、帖子/评论内容、搜索词、header、Cookie/JWT、连接URL、底层错误或服务器路径。
- 不修改冻结 PowerShell，不增加 Windows验收，不创建应用容器镜像。

## 6. 预计文件与交付物

```text
backend/internal/observability/logging/**
backend/internal/observability/logship/**（或同等清晰边界）
backend/internal/config/**
backend/internal/logquery/**（或 backend/internal/logs/**）
backend/internal/http/api.go
backend/internal/apperror/**
backend/cmd/server/**
monitor/internal/logs/**
monitor/internal/httpserver/**
monitor/internal/config/**
monitor/cmd/monitor/**
router/internal/envelope/**
router/internal/routing/**
router/internal/httpserver/**（仅必要接线/测试）
marshaller/internal/envelope/**
marshaller/internal/consumer/**
marshaller/internal/logs/**
marshaller/internal/elasticsearch/**
marshaller/internal/config/**
marshaller/internal/httpserver/**
marshaller/cmd/marshaller/**
.env.example
deploy/compose.yaml（仅 template/health 阻断所需；不新增服务）
scripts/dev.sh
scripts/down.sh
scripts/verify.sh
scripts/verify-logs.sh
.github/workflows/quality-gates.yml
scripts/ci/**（仅脚本与治理测试）
README.md
monitor/README.md
router/README.md
marshaller/README.md
VERSION
frontend/package.json
frontend/package-lock.json
dev/logs/Phase-09/Phase-09-01-Backend日志端到端查询闭环.md
```

预计文件是允许边界，不要求制造无意义改动。目录命名若与预计不同，必须保持职责清晰并在实施记录说明；不得改变总方案冻结的外部契约。

## 7. 详细实施步骤

1. fetch并核对 Phase 4/7/8最终实现、记录和远程门禁，创建 `develop/1.6.1`，保存Git与日常资源快照。
2. 先以最低层测试固定 log shipper 的 stdout优先、message ID复用、non-blocking enqueue、queue full、timeout/redirect、重试取消和 drain行为。
3. 把 shipper仅接入 Backend server logger；确认未配置时 stdout bytes与 Phase 4测试保持一致，配置远程sink后业务线程不等待HTTP。
4. 实现 LogMonitor endpoint、专用身份、严格单条log validator、Envelope构造和 Router Publisher错误映射；覆盖重复key、未知字段、敏感哨兵与无效header。
5. 扩展 Router支持 `logs/backend` 并保持原始bytes；执行 Router unit/race与真实Kafka代表性读取。
6. 重构 Marshaller公共Envelope和typed handler registry，先保持全部metrics测试通过，再实现logs validator、Document和ES writer。
7. 建立固定 template/mapping/alias与幂等 `_id` 写入，在真实ES上验证合法写入、同ID覆盖、永久无效不写和暂时失败不提交。
8. 实现 Backend logs repository、filter/cursor/service/handler和admin路由；定向证明 `401/403` 不调用repository，admin查询只返回白名单。
9. 更新最小生命周期与 `verify-logs.sh`，执行真实API → Kafka → ES → Backend查询；用request ID关联HTTP与业务日志。
10. 运行直接组件和脚本门禁，只有真实失败时做最小修复；相关实现变化后只重跑受影响项。
11. 更新README、版本到 `1.6.1`和实施记录，仅暂存本批文件并提交。
12. push、创建Pull Request并记录真实远程checks/合入状态；未合入或失败时保持本批未完成。

## 8. 风险与控制

- **日志网络调用拖慢业务**：调用线程只non-blocking enqueue；用阻塞fixture证明API响应先完成，网络worker独立退避。
- **队列满导致进程失控**：容量固定且无每条goroutine；队列满只丢远程副本并输出节流的本地状态。
- **发送状态日志递归**：状态logger只写stdout，不进入shipper；测试连续失败时队列和日志数量有界。
- **源端重试制造重复**：同一队列项复用ID，ES以ID作 `_id`；真实重放查询只有一份。
- **Marshaller重构破坏metrics**：先固定公共Envelope兼容，再运行现有metrics全套unit/race和 `verify-marshaller.sh`代表回归。
- **毒日志阻断partition**：两层严格validator；Kafka层永久无效在不调用ES后提交，合法后继继续。
- **任意索引访问**：template/prefix/alias是代码常量，repository不接受index参数；负向测试尝试帖子alias和通配符。
- **admin查询越权**：路由中间件顺序与repository调用计数共同证明；角色只查MySQL当前值。
- **查询成为无界ES代理**：固定参数、24h范围、100上限、固定排序/PIT、响应上限，无DSL/聚合/任意文本。
- **日志泄密**：Phase 4源约束、LogMonitor/Marshaller双检、strict mapping、DTO与真实哨兵测试同时成立。

## 9. 固定验证命令与必要回归

最终diff上每项执行一次；失败后只重跑受修复影响的命令或场景：

```bash
(cd backend && test -z "$(gofmt -l .)")
(cd backend && go test -count=1 ./...)
(cd backend && go vet ./...)
(cd backend && go test -race -count=1 ./...)
(cd monitor && test -z "$(gofmt -l .)")
(cd monitor && go test -count=1 ./...)
(cd monitor && go vet ./...)
(cd monitor && go test -race -count=1 ./...)
(cd router && test -z "$(gofmt -l .)")
(cd router && go test -count=1 ./...)
(cd router && go vet ./...)
(cd router && go test -race -count=1 ./...)
(cd marshaller && test -z "$(gofmt -l .)")
(cd marshaller && go test -count=1 ./...)
(cd marshaller && go vet ./...)
(cd marshaller && go test -race -count=1 ./...)
python3 -m unittest discover -s scripts/ci -p 'test_*.py'
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/1.6.1 --base-ref upstream/main
bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-business.sh \
  scripts/verify-monitor.sh scripts/verify-router.sh scripts/verify-marshaller.sh scripts/verify-logs.sh
docker compose --env-file .env.example --file deploy/compose.yaml config --quiet
scripts/verify-logs.sh --self-test
scripts/verify-logs.sh
scripts/verify-marshaller.sh --self-test
scripts/verify-marshaller.sh
scripts/verify-router.sh --self-test
scripts/verify-monitor.sh --self-test
scripts/verify-business.sh --self-test
git diff --check
```

`scripts/verify-logs.sh` 是本批真实API纵向闭环、request ID关联、admin权限、独立索引和同ID幂等的主证据。完整后台日志、所有故障恢复、日常生命周期和完整社交回归由 Phase-09-02/03执行；本批只在接线暴露共享基础设施风险时扩大。

## 10. 验收标准

- Backend每条范围内日志仍为原Schema v1 stdout JSON；远程sink不可用或queue full不改变API/Outbox结果。
- LogMonitor只接受专用Bearer、合法header与 `service=backend` 白名单日志，生成固定logs Envelope并在Router/Kafka确认后返回 `202`。
- Router接受 `logs/backend` 并保持Kafka value原始bytes；既有 `metrics/redis` 路由、错误和安全边界不变。
- Marshaller公共Envelope与typed handler清晰；metrics全部原契约通过，logs二次严格校验后才写ES。
- 合法logs record仅在ES成功且ownership有效后提交；永久无效不写存储并继续；暂时失败不提交。
- template、日索引和read alias名称固定，mapping strict，帖子搜索alias与文档无变化；同message ID重放只有同一 `_id`。
- 真实API的request ID可查询到HTTP完成与业务成功日志；字段、级别、route template和数值ID正确且无敏感值。
- 未登录稳定 `401`、普通用户稳定 `403`、admin受限查询成功；拒绝请求repository调用为零。
- 内部组件保持loopback，Cookie/JWT不能替代服务token，响应/日志不泄漏内部地址、凭据、Envelope或ES字段。
- 本批固定门禁通过，实施记录真实完整，根与Frontend版本均为 `1.6.1`。

## 11. 明确完成条件

只有第10节全部满足、真实API主链路通过、Phase-09-01 Pull Request已合入主远程 `main`、远程固定门禁成功且实施记录与提交一致，本批才完成。任一日志接收、Kafka、ES写入、offset、查询、`401/403`、索引隔离或Metrics回归证据缺失时不得标记完成。

达到条件后立即停止，不在本批接入后台进程、执行完整故障矩阵、实现Frontend或扩展日志分析。

## 12. Phase-09-02 交接

- 已验证的 Backend stdout + 异步shipper、专用LogMonitor身份、请求/错误/重试语义和有界内存限制。
- `logs/backend` Envelope、Router单Topic原始bytes、Marshaller typed handler与ES `_id`幂等契约。
- 固定template/index/read alias、Backend admin查询、filter/cursor和安全DTO。
- 真实API request ID链路、权限隔离与Metrics回归证据。
- 明确待完成项：Business Worker、Search Indexer、search-reindex接线，短时依赖恢复、queue/drain、混合消息、日常生命周期和资源清理矩阵。
