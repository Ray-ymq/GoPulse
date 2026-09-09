# Phase-14-05：自研组件基础运行指标闭环实施方案

> 当前状态：待实施。本文档定义 Phase 14 第五个执行批次的范围与验收合同；目标版本 1.11.5、开发分支 develop/1.11.5 和执行顺序以 Phase-14-总实施方案.md 为准。

## 1. 批次目标

在六类官方插件已完成的多来源 metrics 链路上，为 GoPulse 六个自研长运行组件提供受保护、有界、低基数的基础运行指标：

    Backend / Business Worker / Search Indexer / Monitor / Router / Marshaller
      → 内部认证 metrics endpoint
      → Monitor 固定 component targets
      → Router → Kafka → Marshaller → VictoriaMetrics
      → Backend 服务端指标目录

批次完成必须证明每个组件都可查询其职责相符的处理结果、延迟、队列或消费进度、最近成功和依赖降级；指标中不存在业务 ID、业务内容、原始路径或任意基础设施名称维度；新端点或采集故障不改变业务 readiness 和处理结果。

## 2. 前置条件

- Phase-14-04 已合入 upstream/main，版本 1.11.4；六插件、metrics 新 schema、Monitor 多 target collector 和 Router/Marshaller/Backend 多 source 契约均通过。
- fetch 后从最新 upstream/main 创建 develop/1.11.5。
- 只核对六个组件的实际请求、消费、队列、依赖交互与退出点，不做一般代码审计或全仓覆盖率扫描。
- 以总方案第 11 节为目录边界；实施时在生产代码、Monitor 与 Marshaller 双层白名单、Backend catalog 和文档中同步固定精确 family、kind、unit、label 与 count。

### 2.1 指标目录冻结与初始值

- 按总方案 §11.3 固定零/未知/尚无成功语义；label tuple 惰性产生，count/duration 成对，无 label、dependency、last-success family 始终存在。严格解析不得把合法“尚无 tuple”视为缺 family。
- 对每个组件在同名 log 固定实际 allowlist、计数点、精确 family 数、最大 sample/body 数。它们由当前路由/消息合同推导，不预留任意新 label；依 §16.1 确认后才接入 Monitor/Marshaller。
- Monitor 自身作为 producer 的来源身份与被采集对象是两个维度，所有层保持新 scraped 标签命名；Redis 的旧存储标签例外继续有效。

## 3. 实施范围

### 3.1 共同 endpoint 与安全契约

- 六组件按总方案 §11.3 增加独立 internal listener 和固定 `GET /internal/v1/metrics`；端口依次为 Backend 19101、Business Worker 19102、Search Indexer 19103、Monitor 19104、Router 19105、Marshaller 19106。所有 listener 与既有主职责共用 root context 和 shutdown deadline，不复用 Backend 公共端口。
- 全部端点要求独立且至少 32 bytes 的内部 Bearer token；无认证、多 Authorization header、query/body、非 GET 和未知 path 均按总方案 §11.3 的状态码和优先级拒绝。
- host mode 仅绑定和访问回环；container mode 只使用固定 service DNS 与 internal network。Compose 不发布包括 Backend 在内的六个 metrics 宿主端口；公共 Backend listener 不注册/转发此路由。
- 成功响应使用有界 Prometheus text exposition；禁止配置 dump、环境变量、goroutine 栈、文件路径、凭据和原始错误。
- metrics endpoint 失败不进入业务 readiness；记录指标不得在请求或消费热路径上产生网络 I/O、阻塞或无界分配。

### 3.2 Backend 指标

- 请求总数按受控 method、route template 和 status class 计数，不使用原始 URL、query、user ID 或 request ID。
- 请求处理时间固定使用总方案 duration counter total 加对应 count，同 tuple 同完成时刻记录，不改成 last-duration gauge，不默认引入大量 histogram bucket。
- 按总方案 §11.3 后台采样暴露固定 outbox pending/oldest age、最近成功以及 MySQL、Redis、RabbitMQ、Elasticsearch 固定 dependency 降级；只暴露聚合数，不读取事件 payload。

### 3.3 Business Worker 与 Search Indexer 指标

- Business Worker 暴露固定 event type 的消费 success/retry/failure/ack 计数、处理时间、in-flight/prefetch 摘要、最近成功以及 RabbitMQ/MySQL 降级。
- Search Indexer 暴露 create/update/delete 的 success/retry/failure 计数、处理时间、in-flight/retry 摘要、最近成功以及 RabbitMQ/MySQL/Elasticsearch 降级。
- event type、operation、result 和 dependency 必须是代码固定枚举，禁止 event/message/post/user ID、重试错误原文、index 名和 payload 内容。
- 最近成功时间固定 Unix seconds gauge；无成功事实输出 0，不省略、不伪造当前时间。dependency_up 初始 -1，实际交互失败 0、成功 1，不生成随时间变化的 label。

### 3.4 Monitor、Router 与 Marshaller 指标

- Monitor 按 6 plugin 加 6 component 固定 target ID 暴露 scrape/publish result、count、duration、last success，观测对象标签固定为 `scraped_producer_kind`、`scraped_target_id`，不得使用来源保留键；另暴露事件队列长度与丢弃、running plugin count 和 Router 降级。
- Router 暴露固定 envelope `type/message_source` 的 accepted/rejected/produced result、produce duration、buffer records/bytes、last Kafka ack 与 Kafka 降级；不使用 message/request ID 或任意 source label。
- Marshaller 暴露固定 `type/message_source/stage/result` 的 consumed/validated/stored/committed/retried 计数、处理时间、当前 in-flight/retry、last storage success/commit 和 Kafka/VM/Elasticsearch 降级。
- 自采集 Monitor 端点不得产生递归 family/label 增长；metrics route 自身若被统计，只能落入固定 route template。

### 3.5 Monitor 固定 component targets 与完整链路

- 在 Monitor 注册六个非插件固定 target，producer_kind=component；producer/source/target 来自服务端 catalog，不复用 plugin ID 或 Registry。
- component target 不提供 install/start/stop/update API；其存在由 Compose 服务 catalog 决定。采集失败只更新对应内部采集状态与安全事件或日志，不修改 plugin Registry。
- Monitor 严格校验每个组件 family/label/count/value并构建 metrics 新 schema；Router、Marshaller 与 Backend 只扩展六个固定 component source/producer/target。
- Backend catalog 按组件职责提供固定指标项，Frontend 现有 Metrics 页可选择查询；不增加任意 PromQL 或管理大屏。
- 建立聚焦验收入口 scripts/verify-component-metrics.sh 或记录中的等价名称；安全自测不访问 Docker，真实模式通过业务、消费、传输活动生成可核对数值。

### 3.6 退出、就绪与故障隔离

- Worker/Indexer 的新内部 HTTP 服务启动失败应给出明确启动错误；运行期采集失败不停止消费者。退出时在既有 shutdown timeout 中共同收敛，不留端口或进程。
- Monitor 无法访问一个 component endpoint 时，其他组件和插件采集继续，被采集组件的主职责继续。
- 指标状态使用内存中有界原子值或有界映射，不建立本地历史数据库，不因指标写入失败回滚业务处理。

## 4. 不在本批范围

- 第七个自研组件、任意 runtime/process exporter、pprof/goroutine 对外暴露或通用 Prometheus scrape proxy。
- 业务 ID、原始 route/query、SQL、queue/topic/index 任意名称或 error message 标签。
- 告警、SLO、管理大屏、独立管理 Frontend、自动扩缩或 Kubernetes。
- 为了指标而重写消费者、发布者、就绪契约或业务数据模型。

## 5. 建议实施顺序

1. 固定六个 component ID/source/target、内部认证和精确 family/label/count 基数预算。
2. 实现 Backend 请求、outbox、dependency 指标与受保护端点，以最小代表性请求验证语义。
3. 实现 Worker/Indexer 指标与内部 HTTP 生命周期，验证 ack/retry/index 和共同退出。
4. 实现 Monitor/Router/Marshaller 指标，证明自采集无递归且热路径无网络 I/O。
5. 注册 Monitor 固定 targets，扩展 Router/Marshaller/Backend/Frontend 目录与聚焦真实验收。
6. 验证未认证、不可直达、单 endpoint 失败隔离、低基数和六插件回归。
7. 更新版本至 1.11.5，完成实施记录、固定门禁和提交。

## 6. 预计直接影响文件

- backend/internal/http、outbox、platform 与新增指标组件
- backend/cmd/server、business-worker、search-indexer
- Worker/Indexer 消费与 shutdown 直接相关 package
- monitor/internal/httpserver、metrics 与 cmd/monitor
- router/internal/httpserver、kafka 与 cmd/router
- marshaller/internal/httpserver、consumer、存储 clients 与 cmd/marshaller
- backend/internal/metricquery、Frontend observability metric catalog/service/view 及直接 tests
- deploy/compose.yaml、.env.example、相关 Dockerfiles/READMEs
- 新 scripts/verify-component-metrics.sh 与直接 Compose 调用路径
- VERSION、Frontend 版本元数据与指标契约文档
- dev/logs/Phase-14/Phase-14-05-自研组件基础运行指标闭环.md

## 7. 批次验收标准

### 7.1 信号语义与完整链路

- 六个组件各自以真实请求、消费、路由或存储活动产生处理结果、延迟、适用的队列/消费进度、最近成功和依赖降级信号。
- 每个组件至少一个处理结果和一个最近成功或进度指标经 Monitor、Router、Kafka、Marshaller、VictoriaMetrics 由 Backend 查询；source/target/producer 不串号。
- 注入一个固定 dependency 故障后，受影响组件的降级指标发生可验证变化，主职责按既有容错合同处理；恢复后指标收敛。

### 7.2 基数与安全

- 用至少两个用户、多个帖子、请求和事件运行代表性闭环后，series 只按固定 route template、event type、operation、stage、result、dependency 集合增长，不出现业务 ID、内容、原始 path/query/error。
- Monitor 与 Marshaller 分别拒绝额外 family、label key/value、重复 series、超限 sample 和非有限值；非法记录不写存储且不阻塞后续。
- 全部 endpoint 未认证访问被拒绝，宿主或浏览器不直达非公开组件；内部 token 不进入日志、API、metrics 或 Frontend。

### 7.2.1 listener 与初始状态断言

- Backend 公共端口不能访问内部 metrics 路由；六个内部端口均无宿主发布，受控内部客户端凭各自 token 访问成功，错 token/重复 Authorization 被拒绝。
- 初始时间戳 0、dependency_up=-1 合法，真实交互驱动 0/1；count/duration tuple 成对，outbox 未知时不伪造空队列。
- Monitor scrape 指标通过自己的完整链路时 scraped 标签保留，producer 来源仍为 Monitor；Router/Marshaller 的消息来源使用 message_source，伪造来源保留标签继续被拒绝。
- 任一 component endpoint 失败不合成业务零值、不修改 Registry，不将 metrics 作为业务 readiness 条件。
- 将上述断言纳入既有直接 package 与 component-focused 命令，不重复做全目标故障排列。

### 7.3 运行隔离和回归

- 单个 component endpoint 停止、超时或返回非法内容时，Monitor 仅将对应 target 标记失败，其他五组件与六插件继续采集。
- metrics 采集、发布或存储故障不让 Backend/Worker/Indexer 业务 readiness 降级，不回滚已提交业务事实或 ack。
- Worker/Indexer 在 SIGTERM 下 consumer、log shipper、internal HTTP 有界共同退出，无遗留端口或进程；替换后消费继续。
- 六插件、Metrics/Logs/Events、Phase 13 完整业务与管理授权代表性回归通过。

### 7.4 完成条件

六组件指标语义、完整链路、低基数、内部认证、退出与隔离以及必要回归全部通过，实施记录完整，版本为 1.11.5，提交只包含本批文件时才完成。非阻断更多指标、histogram、SLO 或大屏只记后续。

## 8. 固定验证命令与回归范围

    (cd backend && go test ./...)
    (cd monitor && go test ./...)
    (cd router && go test ./...)
    (cd marshaller && go test ./...)

    (cd frontend && npm test)
    (cd frontend && npm run typecheck)
    (cd frontend && npm run build)

    bash scripts/verify-component-metrics.sh --self-test
    bash scripts/verify-component-metrics.sh
    python3 scripts/ci/validate_versions.py
    python3 scripts/ci/validate_branch.py --branch develop/1.11.5 --base-ref upstream/main
    git diff --check upstream/main...HEAD

- Backend module 包含 server、Business Worker 和 Search Indexer，其全 module 测试是本批直接影响范围；Monitor、Router、Marshaller 同理。
- 真实验收必须产生代表性业务、消费、路由和存储活动，从 Backend 查询六 component 数值，扫描 label 基数和敏感特征串，并注入一个 endpoint 故障。
- 回归固定为六插件代表性查询、Logs/Events、Worker/Indexer 消费、Phase 13 业务/搜索和管理授权。
- 只有修改共享 shutdown/readiness 后出现具体风险才扩大到 Compose 替换或信号矩阵，并先记录理由。

## 9. 实施记录与下批交接

完成前创建 dev/logs/Phase-14/Phase-14-05-自研组件基础运行指标闭环.md。

记录必须列出六组件最终 family/kind/unit/labels/count、指标更新点、endpoint/token/port/退出契约、真实数值与基数证据、实际命令与结果、偏差和限制，并写清 Phase-14-06 最终 Compose 门禁使用的固定 source/target 和代表性查询。
