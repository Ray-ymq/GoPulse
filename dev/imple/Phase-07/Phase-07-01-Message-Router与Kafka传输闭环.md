# Phase 7-01：Message Router 与 Kafka 传输闭环实施方案

> 权威目标版本与开发分支以 `Phase-07-总实施方案.md` 第 3.2 节为准：本批对应 `1.4.1` / `develop/1.4.1`。
>
> 当前状态：本地实现与固定验收完成，待远程门禁和 Pull Request 合入。

## 1. 批次目标

在 Phase 6 已交付 HTTP Publisher 和 metrics Envelope v1 的基础上，一次性交付可运行的 Message Router、单节点 Kafka、显式单 Topic、验证 Consumer 和 Monitor 正式接线，完成：

```text
真实 Redis → Redis Exporter → MetricsMonitor
          → Message Router → Kafka → 验证 Consumer
```

本批必须是可独立运行和验证的纵向能力；不得只实现 HTTP 接口、只启动 Kafka、只手工 produce，或把真实 Monitor/Consumer 完整性推迟给 Phase-07-02。

## 2. 前置条件

- Phase 6 全部实现和整改已合入主远程 `main`，根与 Frontend 版本为 `1.3.4`。
- Phase 6 实施记录确认 Publisher Endpoint、Bearer、Idempotency-Key、Envelope v1、超时和无历史重试语义未发生未记录变化。
- 从最新主远程 `main` 创建总方案分配的本批分支，并确认分支名、版本目标和工作区资源快照。
- 实施和真实验收在 WSL2 Linux filesystem 执行；Docker daemon 唯一且可确认归属。

## 3. 实施范围

### 3.1 Router module 与生命周期

- 建立独立 Go 1.26 module、`cmd/router`、配置、HTTP server、Envelope Validator、路由表和 Kafka Producer 包。
- Router 使用 JSON 结构化日志，固定 `service=router`；实现信号处理、HTTP shutdown、在途请求等待和 Kafka client 有界关闭。
- `GET /health` 只表达进程存活；受 Bearer 保护的 `GET /ready` 有界检查 broker 和 `gopulse-observability-v1`。
- 配置合法但 Kafka 暂不可用时 Router 仍可监听并恢复；配置非法则在监听前安全失败。

### 3.2 内部发布接口与输入边界

- 正式实现 Phase 6 已交接的 `POST /internal/v1/messages`，仅接受 `application/json`、无 Content-Encoding、1 MiB 以内正文。
- `ROUTER_API_TOKEN` 至少 32 bytes，Bearer 常量时间比较；用户/admin Cookie、JWT、query token 或浏览器请求不构成服务身份。
- 严格拒绝非唯一 JSON object、无效 UTF-8、重复/未知/缺失顶层字段、尾随 token、非法 schema/message ID/timestamp/payload 和 Idempotency-Key 不匹配。
- 当前只允许 `schema_version=1`、`type=metrics`、`source=redis`；未支持组合不写 Kafka。
- 成功路径保留原始 body bytes，不 marshal 或改写；错误统一为总方案固定安全 JSON。

### 3.3 路由与 Kafka Producer

- 显式映射 `metrics → gopulse-observability-v1`，请求不能指定或覆盖 Topic。
- 使用 `github.com/twmb/franz-go v1.21.0`；禁止自动建 Topic，启用 `acks=all`、客户端幂等生产、有界取消、256 records/8 MiB buffer 和 3s 默认生产窗口。
- record key 使用 `message_id`，value 使用收到的原始 body；不修改 Envelope timestamp，不把 partition/offset 写入消息。
- Kafka 成功确认后才返回 `202`；超时、broker、Topic 或 buffer 失败返回 `503 kafka_unavailable`。
- 不增加后台重试、磁盘队列、应用级去重或 transaction；明确记录调用超时可能产生不确定写入和潜在重复。

### 3.4 Kafka Compose 与 Topic

- `deploy/compose.yaml` 增加 `apache/kafka:4.3.1` 单节点 KRaft、internal/external/controller listeners、健康检查和 `kafka_data` volume。
- external listener 只发布到 `127.0.0.1:${KAFKA_PORT}`；初始化步骤经 internal listener 幂等创建单 Topic。
- Topic 本地参数固定为 1 partition、replication factor 1；不自定义长期 retention/compaction，不启用自动建 Topic。
- `.env.example` 增加 Kafka、Router 和 Monitor 正式接线配置，示例 token 明确仅供本地开发。

### 3.5 Bash 生命周期与日常验证

- `dev.sh` 等待 Kafka 健康和 Topic 就绪，构建并以强 PID 归属启动 Router，验证 readiness 后才启动 Monitor。
- `verify.sh` 只读验证 Kafka project/container/volume/Topic、Router PID/health/readiness 和 Monitor/Exporter 状态，不消费 Kafka record。
- `down.sh` 先停止 Monitor/Exporter，再停止 Router，最后清理本项目 Compose；所有操作先验证归属。
- 端口占用、遗留 PID、未知 container/volume、非法 Topic 或 token 时安全失败，不抢占或删除用户资源。

### 3.6 验证 Consumer、隔离验收与 CI

- 增加只用于验收的有限 Consumer，使用唯一测试身份和显式 offset 范围读取目标 Topic，输出可机器校验的 key/value/partition/offset 证据。
- 新增 `verify-router.sh`；默认模式使用随机隔离 Kafka、Redis、数据库、端口、插件根和进程目录，启动真实 Exporter/Monitor/Router 链路。
- 完成原始 body 与 Kafka value 逐 byte 对比，并覆盖成功、target unavailable、非法请求不入 Topic、Kafka 停止/恢复和资源清理。
- 增加 Router CI job，并在脚本/Compose 门禁加入 Router LF、脚本语法、自检、Kafka loopback 和 Topic 配置检查。
- 更新根 README、Router README、Monitor README 和必要配置说明；创建本批实施记录并更新版本元数据。

## 4. 实施边界与非目标

- 不修改 Envelope v1 业务字段，不对 samples 做校验、清洗、映射、聚合或存储转换。
- 不接受 logs/events，不创建多 Topic、死信 Topic、Schema Registry 或重放 API。
- 不为 Router 增加普通用户、管理员或 Backend 公共入口；Backend readiness 不依赖 Router/Kafka。
- 不引入应用级持久去重、exactly-once、磁盘 spool、跨请求事务或无限重试。
- 不把 Kafka 接入 Business Worker、Notification Worker、Search Indexer 或其他 RabbitMQ 路径。
- 不实现 Kafka SASL/TLS、多 broker、生产容灾、Router 容器镜像、Marshaller 或 VictoriaMetrics。
- 不修改冻结 PowerShell，不增加 Windows runner 或原生 Windows 验收。

## 5. 预计文件与交付物

```text
router/go.mod
router/go.sum
router/cmd/router/**
router/internal/config/**
router/internal/envelope/**
router/internal/httpserver/**
router/internal/routing/**
router/internal/kafka/**
router/README.md
monitor/**（仅正式接线或已复现契约问题所需的最小调整）
deploy/compose.yaml
.env.example
scripts/dev.sh
scripts/down.sh
scripts/verify.sh
scripts/verify-router.sh
.github/workflows/quality-gates.yml
README.md
VERSION
frontend/package.json
frontend/package-lock.json
dev/logs/Phase-07/Phase-07-01-Message-Router与Kafka传输闭环.md
```

预计文件是允许边界，不要求制造无意义修改；若实现采用同等清晰的内部目录，可在实施记录中说明，但不得改变总方案公共契约。

## 6. 详细实施步骤

1. 核对 Phase 6 最新实施记录和真实 Publisher/Envelope 代码，保存 Git、端口、Compose、volume、进程和插件根快照。
2. 创建 Router module 和严格配置加载，先完成 token、地址、超时、body/buffer 上限、brokers 和固定 Topic 的定向测试。
3. 实现 Envelope 顶层有界解析、重复 key 检测、Idempotency-Key 校验和原始 body 保留；以 fake Producer 验证拒绝请求不会调用下游。
4. 实现健康/就绪/发布 HTTP 接口、安全错误映射、请求超时和结构化日志；以阻塞/失败 fake Producer 验证 `202` 只在确认后返回。
5. 接入 franz-go Producer，固定 record key/value、acks、幂等、有界 buffer/timeout 和关闭；加入真实 Kafka 定向集成验证。
6. 在 Compose 中加入单节点 KRaft、loopback external listener、健康检查、volume 和显式建 Topic流程。
7. 更新 `.env.example` 和 Monitor 日常配置，使 Monitor 通过正式 Router URL/token 发布；确认 Monitor module 仍无 Kafka 依赖。
8. 更新 `dev.sh`、`verify.sh`、`down.sh` 的启动顺序、PID/端口/资源归属和安全失败路径。
9. 建立有限 Consumer 与 `verify-router.sh` 自检/默认模式，执行真实 success、target unavailable、非法请求、Kafka 故障恢复和逐 byte 完整性矩阵。
10. 加入 Router CI 和脚本/Compose 门禁，更新 README；最终 diff 稳定后执行第 8 节固定验证一次。
11. 更新根与 Frontend 版本为 `1.4.1`，创建本批实施记录，只暂存本批文件，提交并创建 Pull Request。
12. 查询并记录真实远程 checks 与合入状态；未合入或失败时保持本批未完成。

## 7. 风险与控制

- **解析后重新编码掩盖字段改写**：Validator 只提取路由元数据，Producer value 始终引用完整原始 body；验收逐 byte 比较。
- **返回 `202` 早于 Kafka 写入**：HTTP handler 必须等待 Producer delivery result，异步 callback 未完成不得响应成功。
- **超时变成无限等待**：生产、元数据检查、HTTP 和 shutdown 都有独立上限；测试阻塞 Producer 和 broker 停止。
- **把客户端幂等误当应用去重**：README 和交接明确相同 message ID 仍可能有多条 record，Phase 8 按 ID 处理潜在重复。
- **Topic 自动创建掩盖配置错误**：禁止 auto-create，初始化显式创建，readiness 验证固定 Topic。
- **浏览器利用管理员 Cookie 直连**：Router 忽略 Cookie/JWT，只接受独立 token；负向验收覆盖普通用户和 admin Cookie。
- **Kafka 故障拖垮业务**：Router/Kafka 不进入 Backend readiness，Monitor 发布失败继续下一 scrape；故障矩阵同时验证社交 API。
- **验收脚本误删日常 Kafka volume**：随机 project 与 volume label、container ID、端口、PID 全部归属校验，清理前后快照对比。
- **Kafka/RabbitMQ 职责混合**：代码和依赖检查确认 Backend workers 不引入 Kafka、Router 不导入 RabbitMQ。

## 8. 固定验证命令与必要回归

最终 diff 上每项执行一次；失败修复后只重跑受影响命令或场景：

```bash
(cd router && test -z "$(gofmt -l .)")
(cd router && go test -count=1 ./...)
(cd router && go vet ./...)
(cd router && go test -race -count=1 ./...)
(cd monitor && test -z "$(gofmt -l .)")
(cd monitor && go test -count=1 ./...)
(cd monitor && go vet ./...)
(cd monitor && go test -race -count=1 ./...)
(cd exporters/redis && go test -count=1 ./...)
python3 -m unittest discover -s scripts/ci -p 'test_*.py'
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/1.4.1 --base-ref upstream/main
bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-business.sh scripts/verify-exporter.sh scripts/verify-monitor.sh scripts/verify-router.sh scripts/package-redis-exporter.sh
docker compose --env-file .env.example --file deploy/compose.yaml config --quiet
scripts/verify-router.sh --self-test
scripts/verify-router.sh
scripts/verify-monitor.sh --self-test
scripts/verify-exporter.sh --self-test
git diff --check
```

Router 单元测试固定覆盖：合法消息原始 bytes/key、错误 token、非法/重复 JSON、Idempotency-Key 不匹配、超限、未支持类型、Producer 成功/失败/阻塞、health 与 readiness 分离和有界 shutdown。更多字段排列仅在真实失败证明需要时增加。

`verify-router.sh` 是本批真实纵向链路和 Kafka 故障恢复主证据；`verify-monitor.sh --self-test` 与 `verify-exporter.sh --self-test` 只保护已有安全边界。本批未修改 Backend 业务代码时不重复完整 `verify-business.sh`，跨域回归留给 Phase-07-02。

## 9. 验收标准

- Router 是独立 Go module，可健康启动、有界关闭并在 Kafka 暂不可用时保持进程存活。
- 无/错 token、用户/admin Cookie、非法 Content-Type/body/Header/Envelope 和未支持类型均被安全拒绝且不写 Kafka。
- 合法 `metrics/redis` Envelope 只能路由到显式 `gopulse-observability-v1`，客户端不能指定 Topic。
- 只有 Kafka 确认后返回 `202`；失败在固定时间内返回安全非 `202`，无后台或磁盘历史重试。
- Kafka record key 等于 `message_id`，value 与 Router 接收原始 HTTP body 逐 byte 一致，业务 timestamp 未改写。
- 真实 Redis success 和 target unavailable 消息都经过 Exporter、MetricsMonitor、Router、Kafka 并由 Consumer 读取。
- Kafka 停止后 `/health` 仍成功、`/ready` 和发布失败；恢复后无需重启 Router/Monitor即可收到新消息。
- Monitor 不含 Kafka SDK/Topic，Router 不含清洗/存储，RabbitMQ 业务路径不含 Kafka。
- 日常与隔离生命周期不误杀、不误删、不遗留进程、端口、container、network、volume、plugin root 或临时文件。
- 第 8 节固定验证与远程门禁通过，根与 Frontend 版本为 `1.4.1`，实施记录真实完整。

## 10. 明确完成条件

只有真实 Monitor 输入、服务鉴权、严格有界校验、显式路由、Kafka 确认、原始 bytes 完整性、Consumer 读取、Kafka 故障恢复、职责隔离和资源安全全部通过，且本批 Pull Request 已合入主远程 `main`、远程门禁成功，才可标记 Phase-07-01 完成。手工 curl、直接 Kafka produce、mock broker 或只通过 unit test 均不足以完成本批。

## 11. 下一批交接

- 可独立运行和恢复的 Router，以及固定 health/readiness/POST、安全错误和服务身份契约。
- 显式单 Topic `gopulse-observability-v1`，record key=`message_id`、value=原始 Envelope bytes。
- franz-go Producer 的确认、超时、buffer、关闭和潜在重复语义。
- 真实 Redis success/target unavailable 经 Monitor 到 Kafka Consumer 的证据。
- Kafka/Router/Monitor 日常生命周期、`verify-router.sh`、CI 和资源归属边界。
- Phase-07-02 只需在最终构建上完成权限、故障、业务隔离、阶段固定门禁、文档/版本和远程状态收口，不得扩展新功能。
