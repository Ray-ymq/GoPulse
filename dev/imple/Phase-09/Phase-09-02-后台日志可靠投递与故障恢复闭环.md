# Phase-09-02：后台日志、可靠投递与故障恢复闭环实施方案

> 权威目标版本与开发分支以 `Phase-09-总实施方案.md` 第 3.2 节为准：本批对应 `1.6.2` / `develop/1.6.2`。
>
> 当前状态：本地实施与固定门禁已完成；待推送、Pull Request、远程门禁与合入后完成批次状态收口。

## 1. 批次目标

在 Phase-09-01 已合入的 Backend API 日志纵向闭环上，把同一 stdout + 有界异步 Push能力接入 Business Worker、Search Indexer 和 search-reindex，并通过真实依赖故障、重复重放、永久异常、Metrics/Logs混合消费与日常生命周期验证，形成 Phase 9 最终实现能力：

```text
HTTP request_id + Outbox event_id + Worker/Indexer event_id + reindex lifecycle
  → LogMonitor → Router → Kafka → Marshaller → Elasticsearch
  → Backend admin filters / cursor query
```

本批不重新设计 Phase-09-01 的 HTTP、Envelope、index或查询合同。其重点是扩展完整 Phase 4 日志源，并证明“业务始终优先、发送有界、Kafka接受后可恢复、同ID可重放、坏消息可继续、Metrics仍正确”。只接上三个命令入口但不验证恢复和跨类型并存，不构成本批完成。

## 2. 前置条件

- Phase-09-01 已合入最新主远程 `main`，远程固定门禁成功，根与 Frontend版本为 `1.6.1`，实施记录与代码一致。
- 从包含 Phase-09-01 的最新 `upstream/main` 创建 `develop/1.6.2`，不继续使用 `develop/1.6.1`。
- 已核对 `business-worker`、`search-indexer`、`search-reindex` 的配置加载、logger构造、退出码、RabbitMQ状态机和真实验收入口。
- 已核对 Phase-09-01 shipper、LogMonitor、Router logs、Marshaller typed handler、ES writer和Backend查询的实际偏差；若公共合同已改变，先同步总方案和本文。
- 在 WSL2 Linux filesystem 使用强归属隔离资源；开始前保存Git、日常进程、端口、Compose、volume、Kafka group、插件根和日志文件快照。

## 3. 实施范围

### 3.1 Business Worker 与 Search Indexer 接入

- 两个常驻命令使用与 Backend server相同的日志shipper实现，不复制第二套HTTP client、queue或重试状态机。
- Worker专用配置加载必须包含可选 `LOG_MONITOR_URL` 和同一组 `LOG_SHIP_*`；未启用时保持 Phase 4 stdout-only，启用时严格校验但不要求 LogMonitor在启动瞬间可达。
- Business Worker 保留 `service=business-worker` 与 `lifecycle|worker|notification` module；Search Indexer保留 `service=search-indexer` 与 `lifecycle|worker|search` module。
- RabbitMQ delivery的 ack/nack、retry/dead-letter、reconnect和shutdown顺序不得依赖 enqueue或HTTP结果；阻塞/失败shipper fixture必须证明消息状态机结果与 Phase 2/3一致。
- event日志继续只包含 canonical UUID `event_id`、固定 `event_type`、attempt/reason和允许的 `post_id`；不发送AMQP body/header、通知正文或ES文档。
- 连接不可用/恢复等 lifecycle日志按原状态转换产生；shipper失败状态logger不能与Worker自己的重连日志形成递归或无界刷屏。

### 3.2 search-reindex 一次性命令接入

- `search-reindex` 使用同一shipper，但退出前只在 `LOG_SHIP_SHUTDOWN_TIMEOUT` 内drain；LogMonitor不可用、queue full或drain超时不得把成功重建改成失败，也不得把失败重建改成成功。
- 参数错误、配置错误发生在远程sink可安全建立之前时，保留现有stdout Schema/退出码，不要求为了发送失败日志而继续初始化业务依赖。
- 远程记录只包含 Phase 4 已允许的 `stage`、`reason`、`result`、`batch_size`、`document_count`和resource；不保存索引正文、alias列表或ES响应。
- 真实验收执行一次有变化或明确unchanged的reindex，并通过管理员API查询对应 lifecycle/result日志。

### 3.3 跨进程相关性

- 真实发帖产生 `post.created`，通过同一 `event_id` 关联 Backend Outbox发布和 Search Indexer处理日志。
- 真实评论或首次点赞产生通知事件，通过同一 `event_id` 关联 Outbox 与 Business Worker处理日志。
- request ID只关联单个HTTP请求；不得为了展示跨进程链路把 request ID新增到RabbitMQ Envelope或后台日志。
- Backend查询按 `event_id`、`service`、`module`、`level`和时间范围精确过滤，顺序固定；翻页只携带已固化filters/时间窗的签名cursor，且不重复/遗漏已冻结PIT内的结果。
- 查询结果中不同service的字段按canonical DTO省略不适用值，不返回 `null`、零值占位或内部transport ID。

### 3.4 源端发送故障与恢复

- 用可控HTTP fixture覆盖：立即 `202`、临时 `503` 后恢复、timeout结果不确定后 `202`、`400/413/422` 永久拒绝、`401` 持续降级、queue full和shutdown取消。
- 临时失败始终保留当前队首和message ID，不能越过到后续记录；恢复后按入队顺序继续。
- 永久输入拒绝只丢该远程副本并继续下一条，不让一条无法修复的日志永久卡住所有后续日志。
- queue容量和goroutine数量保持固定；长时间故障造成的drop数量可被有限本地状态看到，但不建立磁盘spool或重启恢复承诺。
- 真实短时故障分别停止 Monitor或Router，继续执行代表性业务，确认API/RabbitMQ结果成立；恢复原进程后，容量范围内已排队日志无需重启业务进程即可到达ES。

### 3.5 Kafka、Marshaller 与 Elasticsearch 恢复

- Router/Kafka故障期间LogMonitor返回 `503`，源端不误认成功；Kafka恢复后同一Router/Monitor/源进程继续。
- Elasticsearch故障发生在合法logs record到达Kafka之后时，Marshaller不推进正式group offset；恢复同一ES后无需手工offset或重启即可写入并提交。
- 在明确观察未提交offset后终止Marshaller，恢复/重启后从正式group committed offset重取，并以同一 `_id` 完成写入；不依赖shell时序猜测。
- 原样重放捕获的合法 key/value，证明index、document bytes和 `_id` 确定，查询只有一个文档。不同message ID的相同业务字段仍是独立日志。
- 通过受控fixture向Kafka注入一个logs payload永久错误；证明ES没有该 `_id`、offset安全越过且随后真实合法日志被查询。
- index template的重复确保、并发首次写、UTC日期选择和alias附加在最低有效层或真实ES定向集成中验证；不通过等待跨日做慢速验收。

### 3.6 Metrics 与 Logs 混合消费

- 在同一 `gopulse-observability-v1` partition中交替产生真实Redis metrics和真实Backend/Worker logs，记录有限offset范围。
- 证明 metrics handler只写VictoriaMetrics，logs handler只写Elasticsearch；两类永久错误reason和暂时存储错误不得互相误分类。
- Logs扩展后重新验证 Phase 8 的真实success、target_unavailable/recovery和代表性永久metrics异常后继续；不重做未变化的完整metrics排列。
- 在ES暂不可用且当前logs record阻塞时，明确观察同partition后继记录不被越过；ES恢复后logs与后继metrics按序完成。该行为记录为首版有序backpressure，不伪装为存储故障完全隔离。
- VictoriaMetrics故障时同理保持现有metrics offset语义，恢复后不得把metrics文档写入ES或放宽logs查询。

### 3.7 日常生命周期、只读验证与CI

- 完成 `scripts/dev.sh` 对 Backend、Business Worker、Search Indexer的日志环境传递；一次性search-reindex通过调用环境继承相同配置。
- `scripts/down.sh` 先停止并有界drain全部日志源，再停止Monitor/Marshaller/Router；失败/信号路径保留原强PID和Compose归属。
- `scripts/verify.sh` 只读检查四类源配置/进程（适用时）、LogMonitor endpoint存在性、Marshaller readiness、template/alias和受限admin查询，不产生测试日志或改offset。
- 扩展 `scripts/verify-logs.sh --self-test` 覆盖queue、URL/token、index/query和所有新增资源拒绝；默认模式执行完整后台源、故障恢复、重复、永久异常和混合链路。
- CI的 Logs pipeline job使用随机资源运行代表性矩阵；Backend/Monitor/Router/Marshaller jobs继续负责各自unit/vet/race。失败日志上传前必须扫描敏感哨兵并限制大小。
- 更新README与四类日志源运行说明，创建本批实施记录并同步 `1.6.2`。

## 4. 验收场景与证据

### 4.1 真实业务与后台链路

1. 启动随机隔离栈，创建普通用户与admin，并记录初始Kafka正式group offset、ES日志count和VM时间窗。
2. 普通用户发帖，捕获响应request ID及对应 `post.created` event ID；查询到Backend HTTP/业务/Outbox日志与Search Indexer处理日志。
3. 普通用户评论或首次点赞，查询到Backend HTTP/业务/Outbox与Business Worker处理日志，通知事实仍正确。
4. 运行search-reindex，查询到start和completed/skipped记录；命令退出码与实际reindex结果一致。
5. 每个查询只用admin Cookie；普通用户相同filters保持 `403`。

### 4.2 代表性故障矩阵

| 故障 | 必须观察的发送/offset结果 | 必须观察的业务结果 | 恢复结果 |
| --- | --- | --- | --- |
| Monitor或Router停止 | 源队首保留，同ID退避；容量内不丢 | API与RabbitMQ事实成立 | 原进程恢复后可查 |
| Kafka停止 | LogMonitor不返回成功，源端不出队 | 非搜索业务成立 | 同Topic恢复后继续 |
| Elasticsearch停止 | logs record已在Kafka但offset不推进 | 现有ES readiness退化；非搜索业务不新增失败 | 同ES恢复后写入/提交 |
| Marshaller停止 | Kafka保留已接受record | 业务与日志接收按容量继续 | 重启从committed offset继续 |
| queue full | 仅远程副本drop且状态节流 | 业务延迟/状态不变 | 新可用容量继续发送 |
| 永久坏日志 | 不写ES，坏offset安全越过 | 进程保持存活 | 后继合法日志可查 |

精确queue full、timeout/accept竞态和shutdown由确定性测试证明；真实端到端只执行一组代表，不用大量请求制造压力测试。

## 5. 实施边界与非目标

- 不改变 Phase-09-01 的 endpoint、Envelope字段、Topic、正式group、index名称、mapping、查询参数、DTO或状态码，除非真实阻断问题先同步规划。
- 不新增Frontend页面、EventMonitor、全文检索、聚合、告警、ILM、删除API、磁盘spool、多Topic或独立consumer group。
- 不为跨进程关联修改RabbitMQ Envelope或传播request ID；使用既有event ID。
- 不承诺超过配置queue容量或进程崩溃前未获 `202` 的日志可恢复。
- 不开展长时吞吐、磁盘满、ES容量、Kafka retention或生产SLA测试。
- 不修改冻结PowerShell，不增加原生Windows验收，不创建应用容器镜像。

## 6. 预计文件与交付物

```text
backend/internal/observability/logging/**
backend/internal/observability/logship/**（或09-01实际目录）
backend/internal/config/**
backend/cmd/business-worker/**
backend/cmd/search-indexer/**
backend/cmd/search-reindex/**
backend/internal/worker/**（仅logger接线直接回归）
backend/internal/logquery/**（仅filter/分页阻断修复）
monitor/internal/logs/**（仅可靠性/状态修复）
monitor/internal/httpserver/**（仅直接故障映射）
router/**（仅直接故障恢复阻断修复）
marshaller/internal/envelope/**（仅typed dispatch阻断修复）
marshaller/internal/consumer/**
marshaller/internal/logs/**
marshaller/internal/elasticsearch/**
marshaller/internal/httpserver/**
marshaller/cmd/marshaller/**
.env.example
scripts/dev.sh
scripts/down.sh
scripts/verify.sh
scripts/verify-logs.sh
scripts/verify-marshaller.sh（仅混合链路必要扩展）
scripts/verify-business.sh（仅事件关联/故障窗口复用）
.github/workflows/quality-gates.yml
scripts/ci/**（仅脚本与治理测试）
README.md
monitor/README.md
router/README.md
marshaller/README.md
VERSION
frontend/package.json
frontend/package-lock.json
dev/logs/Phase-09/Phase-09-02-后台日志可靠投递与故障恢复闭环.md
```

预计文件是允许边界。未发生阻断的09-01公共契约文件不应为制造diff而修改；所有偏差在实施记录说明。

## 7. 详细实施步骤

1. fetch最新main，核对09-01实施记录/远程结果和实际公共合同，创建 `develop/1.6.2` 并保存资源快照。
2. 为Worker/Reindex配置加载增加共享log shipping设置，保持未启用时输出与退出语义不变。
3. 把shipper注入Business Worker与Search Indexer，使用现有handler/runtime测试证明ack/nack/retry/dead/reconnect不依赖发送结果。
4. 接入search-reindex并定向验证成功、失败、参数错误、drain timeout下原退出结果保持。
5. 扩展Backend日志查询定向测试，固定跨service event ID、filter组合、PIT分页和省略字段。
6. 完成source HTTP fixture矩阵：临时/永久错误、同ID重试、queue full、恢复和shutdown。
7. 完成Kafka/Marshaller/ES恢复、永久异常后继续和同ID重放；只使用随机group观察与固定日志alias，不改变日常offset。
8. 交替产生真实metrics/logs，验证两种writer、offset、异常和有序backpressure；保持Phase 8主契约。
9. 完成dev/verify/down生命周期、资源归属、自检和CI；在失败/中断路径对比前后快照。
10. 执行真实发帖、评论/点赞、通知、索引和reindex矩阵，通过request/event ID查询后台日志，并执行代表性权限/敏感哨兵检查。
11. 只修实际阻断问题；最终diff稳定后执行第9节固定门禁一次。
12. 更新README、版本到 `1.6.2`和实施记录，仅暂存本批文件并提交。
13. push、创建Pull Request并记录远程checks/合入；未合入或失败时保持本批未完成。

## 8. 风险与控制

- **共享shipper改变Worker消息语义**：发送接口不返回给业务状态机；阻塞/失败fixture断言ack/nack与Phase 2/3一致。
- **一次性命令为发日志改变退出码**：业务结果先确定，drain是有界附加动作；测试成功/失败两侧。
- **短时故障测试误等于零丢失承诺**：只证明队列容量内恢复，明确记录长故障、crash和drain超时限制。
- **永久错误卡住队列/partition**：源端永久HTTP状态丢当前副本继续；Kafka永久payload错误提交当前offset继续。
- **同Topic跨存储故障越过消息**：坚持单partition顺序，不为保持metrics吞吐跳过未确认logs；文档明确延迟边界。
- **重放形成重复日志**：同队列项ID固定，Marshaller文档确定，ES `_id` 幂等；真实查询按ID/count证明。
- **查询跨进程字段混乱**：strict mapping与DTO按字段类型统一，不适用值省略；filter绑定cursor digest。
- **故障验收误伤日常环境**：停止任何进程/container前核对随机project label、PID、端口、volume和group；前后快照必须一致。
- **敏感fixture进入验收制品**：使用唯一哨兵后扫描stdout、ES、HTTP响应和上传文件；失败输出只保留有限元数据。
- **范围扩张**：只关闭本批固定矩阵失败；容量、Topic拆分、spool、Frontend和Events记录后停止。

## 9. 固定验证命令与必要回归

最终diff上按直接影响执行一次；09-01未变化且已有记录的公共合同测试可引用，但后台源、可靠性和混合链路必须实际执行：

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
(cd marshaller && test -z "$(gofmt -l .)")
(cd marshaller && go test -count=1 ./...)
(cd marshaller && go vet ./...)
(cd marshaller && go test -race -count=1 ./...)
python3 -m unittest discover -s scripts/ci -p 'test_*.py'
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/1.6.2 --base-ref upstream/main
bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-business.sh \
  scripts/verify-monitor.sh scripts/verify-router.sh scripts/verify-marshaller.sh scripts/verify-logs.sh
docker compose --env-file .env.example --file deploy/compose.yaml config --quiet
scripts/verify-logs.sh --self-test
scripts/verify-logs.sh
scripts/verify-marshaller.sh --self-test
scripts/verify-marshaller.sh
scripts/verify-business.sh --self-test
scripts/verify-business.sh
git diff --check
```

Backend测试固定覆盖三个命令入口的sink接线、业务状态机隔离和reindex退出语义；`verify-logs.sh` 负责真实后台源、短时故障、offset、重放、永久异常、权限和资源；`verify-marshaller.sh` 负责既有真实Metrics并存。除具体失败或共享基础设施变化外，不追加完整Frontend E2E或长时压测。

## 10. 验收标准

- `backend`、`business-worker`、`search-indexer`、`search-reindex` 四类生产日志源均保留Phase 4 stdout Schema，并使用同一有界shipper。
- shipper临时失败同ID重试、永久输入错误继续、queue/goroutine有界、shutdown可取消；任何结果不改变API、RabbitMQ或reindex业务语义。
- 真实post/comment/like事件能用既有event ID关联Backend Outbox与对应Worker/Indexer日志；不传播request ID到消息总线。
- Monitor/Router/Kafka短时故障在容量内恢复；ES故障不提交logs offset，同ES恢复或Marshaller重启后从正式group继续。
- 一个Kafka永久坏日志不写ES且不阻断后继；同ID合法record重放只有一个ES文档。
- Metrics和Logs在同Topic/Marshaller中交替处理并写各自存储，既有metrics严格校验、查询和故障恢复无回归。
- admin按service/event ID/time/filter/cursor查询结果稳定；未登录/普通用户与内部服务身份边界不变。
- dev/verify/down顺序、只读性、PID/Compose/volume/group/文件归属和失败清理通过，不误伤日常资源。
- 敏感哨兵不出现在远程链路、ES、Backend响应或验收制品；内部端口仍loopback。
- 本批固定门禁通过，实施记录真实完整，根与Frontend版本均为 `1.6.2`。

## 11. 明确完成条件

只有第10节全部满足、后台真实链路与固定故障矩阵通过、Phase-09-02 Pull Request已合入主远程 `main`、远程门禁成功且实施记录与提交一致，本批才完成。缺少任一后台源、故障恢复、重放、Metrics并存、业务隔离或资源证据时不得标记完成。

完成后立即停止功能扩展。Phase-09-03只在本批最终构建上做阶段级交叉验收和状态收口，不补计划外功能。

## 12. Phase-09-03 交接

- 四类真实日志源、统一shipper、准确的best-effort/at-least-once分界与已验证短时恢复。
- LogMonitor/Router/Kafka/Marshaller/ES完整logs能力、永久异常继续、同ID幂等和正式offset证据。
- request ID、event ID、service/filter/cursor管理员查询能力与权限负向结果。
- Metrics/Logs混合消费、有序backpressure和两种writer职责隔离的真实证据。
- 日常生命周期、隔离资源、自检和CI固定入口。
- 09-03只需执行最终阶段矩阵、必要回归、文档/版本/远程状态收口，并只修真实阻断问题。
