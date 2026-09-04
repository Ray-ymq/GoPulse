# Phase 8-01：Marshaller 与 VictoriaMetrics 最小指标闭环实施方案

> 权威目标版本与开发分支以 `Phase-08-总实施方案.md` 第 3.2 节为准：本批对应 `1.5.1` / `develop/1.5.1`。
>
> 当前状态：本地实现与固定验收已完成，待远程门禁、Pull Request 与合入确认。

## 1. 批次目标

在 Phase 7 已交付真实 Monitor → Router → Kafka record 的基础上，交付可运行的 Marshaller、正式 Kafka consumer group、完整 metrics Envelope v1 第二次校验、确定性 Prometheus text 转换、单节点 VictoriaMetrics 基本写入/查询和最小日常生命周期，形成：

```text
真实 Redis → Redis Exporter → MetricsMonitor → Message Router
          → gopulse-observability-v1 → Marshaller
          → VictoriaMetrics → 受控指标查询
```

本批必须是可独立运行、验证和安全合入的最小纵向能力。不得只建立 Consumer、只转换 fixture、只手工调用 VictoriaMetrics import、只证明容器健康，或把真实上游、手动 offset、永久异常继续、generation ownership fencing、安全 commit 和查询推迟给 Phase-08-02。真实 broker rebalance、Kafka/VM/进程故障恢复和运维加固属于 Phase-08-02。

## 2. 前置条件

- Phase-07-01、Phase-07-02 和 Phase-07-03 均已合入主远程 `main`；PR #71 的 9 项远程 checks 通过，合入提交为 `60f9aa8`，根与 Frontend 版本为 `1.4.3`。
- Phase 7 三份实施记录和真实代码确认 Topic 为 `gopulse-observability-v1`，record key 为 `message_id`，value 为 Router 未改写的原始 Envelope v1 JSON。
- 已确认 Phase 7 Router 的 Kafka 客户端版本、broker 配置、Topic 创建、record 大小、不确定写入和恢复语义；如与 Phase 8 规划输入不一致，先更新总方案和本文。
- 从最新主远程 `main` 创建 `develop/1.5.1`，不沿用 `update` 或 Phase 7 分支。
- 实施与真实验收在 WSL2 Linux filesystem 执行，Docker daemon 唯一且资源可确认归属。
- 开始前保存 Git 状态、日常 Compose project/container/network/volume、`.run` 进程、端口和插件根快照，不停止、删除、暂存或提交其他任务资源。

## 3. 实施范围

### 3.1 Marshaller module 与生命周期

- 建立独立 Go module `github.com/Ray-ymq/GoPulse/marshaller`，包含 `cmd/marshaller`、配置、Envelope、Consumer、metrics 校验/转换、VictoriaMetrics client 和 HTTP server。
- Go 版本与实施时仓库基线一致；Kafka 客户端与 Phase 7 Router 使用同一已锁定 franz-go 版本，不建立第二套客户端选型。
- 使用 Schema v1 单行结构化日志，固定 `service=marshaller`，module 至少为 `lifecycle`、`consumer`、`transform`、`storage` 和 `http`。
- 实现信号处理、停止新 poll、取消 ownership lease/退避/在途操作、只在 generation 仍有效时提交已接受结果、HTTP shutdown 和 Kafka client 有界关闭。
- `/health` 只表达进程存活；Bearer 保护的 `/ready` 有界检查 Kafka、Topic 和 VictoriaMetrics。依赖暂不可用时 health 保持成功、ready 返回有限 `503` 状态。
- 配置非法在监听或连接前安全退出；配置合法但依赖暂不可用时进程保持可恢复。

### 3.2 正式 Kafka Consumer 与 offset 状态机

- 固定消费 Topic `gopulse-observability-v1`，正式 group `gopulse-marshaller-metrics-v1`，初次无 committed offset 从 earliest 开始。
- 禁止自动建 Topic和自动提交；首版按 partition 顺序、单 record 在途处理，不增加无界 channel、goroutine fan-out 或本地 spool。
- record key 必须是 32 位小写十六进制并逐字等于 Envelope `message_id`；record value 再次限制在 1 MiB。
- 本批验收固定单 Consumer、每 partition 单 record 在途处理，但每条处理必须绑定当前 assignment generation 的 ownership lease。合法 record 只有在封闭校验/转换完成、VictoriaMetrics 返回 `204` 空响应且 lease 仍有效后才提交对应 offset；该响应只代表 HTTP transport acceptance，不宣称逐 sample 持久化确认。
- 永久无效 record 也只在 lease 仍有效时记录固定 reason code、提交并继续下一条。VictoriaMetrics 网络、timeout、认证和非成功响应属于暂时失败，不提交当前 record，在当前 lease 下有界退避重试或在取消时停止。Kafka 读取/提交失败不得伪装为成功。
- `OnPartitionsRevoked`/`OnPartitionsLost` 立即取消旧 lease、写入、退避和提交；旧 generation 即使稍后收到 `204` 也不得提交。HTTP acceptance 后 commit 失败时停止推进该 partition，不提交后续 record。Consumer、Committer、Writer 和 ownership 必须可注入确定性验证这些正确性；真实 broker restart/rebalance 恢复矩阵留给 Phase-08-02。
- 不得用 `BlockRebalanceOnPoll` 跨越 VictoriaMetrics 写入或无限退避，不得为维持 poll 丢弃已缓冲 record。本批不宣称 exactly-once。

### 3.3 Envelope v1 与 metrics payload 第二次校验

- 先用 token 级词法扫描递归拒绝顶层、payload、每个 sample 和 labels 中的重复 key，再用 `DisallowUnknownFields` typed decoder 拒绝固定 schema object 的未知/缺失字段，并拒绝非唯一 object、无效 UTF-8、尾随 token、null payload 和超限消息；普通 Go struct 解码不能单独兑现该契约。
- 顶层固定接受 `schema_version=1`、`type=metrics`、`source=redis`、合法 message ID、与 key 相等的 ID 和 UTC RFC3339Nano timestamp。
- timestamp 必须能转换为 Unix 毫秒且不得超前当前时间超过 5 分钟；不因 Kafka/存储积压而固定拒绝合法历史 record。
- payload 固定 `plugin_id=redis-exporter`、稳定三段 SemVer、`target_id=redis-exporter-local`、`success|target_unavailable` 和非 null、非空 samples；每个 labels 也必须是非 null object。
- `success` 固定 10 个 family、11 个 sample：无标签 gauge `up`、`uptime_seconds`、`connected_clients`、`used_memory_bytes`；无标签 counter `commands_processed_total`、`keyspace_hits_total`、`keyspace_misses_total`；带 `mode=user|system` 的两条 `cpu_seconds_total` counter；以及带同一十进制 `db` 标签的 `db_keys`、`db_expiring_keys` 两条 gauge。
- `target_unavailable` 固定只有无标签 gauge `gopulse_redis_up=0`；`success` 的 `up=1`。value 必须是有限 `float64` JSON number，counter 非负；family、kind、标签白名单、名称/标签长度和 canonical sample key 完整复验总方案第 8.2 节。
- 未支持 schema/type/source、key/ID 不符和任一 payload/sample 契约错误均为永久无效；在调用 VictoriaMetrics 前停止。

### 3.4 确定性指标转换

- 每个 sample 映射为一条 Prometheus text exposition import 行，保留 metric name/value 和 `mode`/`db` 标签，并固定增加 `source="redis"`、`target_id="redis-exporter-local"`。
- 不添加 `message_id`、plugin ID/version、scrape status、kind、Kafka partition/offset 或错误文本标签。
- 保留标签冲突时拒绝整条消息；labels 按 key 稳定排序并按 Prometheus text 规则转义。
- samples 按 canonical key 稳定排序，数值固定使用 `strconv.FormatFloat(value, 'g', -1, 64)`（允许其为极大/极小有限值选择科学计数法）；全部 sample 使用 Envelope timestamp 的 Unix 毫秒值，纳秒统一截断为毫秒。
- 一个 Envelope 形成一个以换行结尾的导入正文，输出上限 2 MiB；相同 record 重放必须逐 byte 产生相同正文。

### 3.5 VictoriaMetrics Compose、写入与查询

- `deploy/compose.yaml` 增加固定 `victoriametrics/victoria-metrics:v1.151.0` 单节点服务、健康检查和独立 `victoriametrics_data` volume。
- 端口只发布到 `127.0.0.1:${VICTORIAMETRICS_PORT}:8428`；固定 storage path、内部 Basic Auth 和 `-dedup.minScrapeInterval=1ms`。
- Marshaller 使用专用有界 HTTP client 向 `POST /api/v1/import/prometheus` 写入 `text/plain; version=0.0.4; charset=utf-8`，使用内部 Basic 身份、3s 默认 timeout、有限响应和不跟随 redirect。
- 该非 Pushgateway 路径只接受 `204 No Content` 且空 body 作为 HTTP transport acceptance；网络、timeout、认证、redirect、其他状态或非空/无法完整读取的响应不提交 offset，错误/日志不回显响应 body 或凭据。鉴于 import API 可能不返回逐行解析错误，封闭 transformer 必须在写入前保证语法合法，真实验收还要证明 `vm_rows_invalid_total` 不增加并查询到全部预期时序。
- 通过 `POST /prometheus/api/v1/query` 与 `/prometheus/api/v1/query_range` 执行固定内部验收查询，不新增 Backend/Frontend 产品查询入口，不向浏览器发放 VM 地址或凭据。
- 相同 Envelope 重放时依赖确定性时序/毫秒时间和 1ms 存储去重稳定查询点；明确记录这是 at-least-once 下的有限幂等，不是 Kafka/HTTP 事务。

### 3.6 Bash 生命周期、隔离验收与 CI

- `.env.example` 增加 VictoriaMetrics 和 Marshaller 固定配置及仅限本地的开发凭据；真实环境要求替换。
- `scripts/dev.sh` 在 Kafka/Topic/VM 健康后构建并以强 PID 归属启动 Router、Marshaller，再启动 Monitor/Exporter；输出有限内部地址提示。
- `scripts/verify.sh` 只读检查 Kafka/VM container、volume、Topic、Router/Marshaller/Monitor PID、health/readiness 和固定指标查询，不 produce、不提交 offset、不修复资源。
- `scripts/down.sh` 先停止 Monitor/Exporter，再停止 Marshaller、Router和其他应用进程，最后按 Compose project 归属停止基础设施并保留日常 volumes。
- 新增 `scripts/verify-marshaller.sh`：`--self-test` 只执行无 Docker 的配置、token、URL、Topic/group、查询白名单、PID 和最小资源归属负向测试；默认模式启动随机强隔离的最小全链路。
- 默认模式必须由真实 Redis 状态与命令产生 success 和 target unavailable 消息，并查询 VM；fixture producer 只用于一个代表性永久坏消息，不在本批执行重复、rebalance 或完整故障矩阵。
- 新增独立 Marshaller CI job，并在 Scripts and Compose 门禁加入 LF、Bash、自检、VM loopback/auth/dedup、固定镜像、volume 和 Marshaller 基本配置检查。故障恢复与强归属扩展由 Phase-08-02 完成。
- 更新根 README、Marshaller README 和必要的 Router/Monitor 交接说明；创建本批实施记录并同步版本元数据。

## 4. 实施边界与非目标

- 不修改 Phase 7 Topic、record key/value 或 Router 原始 bytes 契约，除非真实上游阻断且先更新规划。
- 不在 Monitor 中加入 Kafka/VM，不在 Router 中解析 payload，不让 Marshaller采集 Exporter 或处理 RabbitMQ。
- 不实现 Backend Metrics Query API、Frontend 页面、Dashboard、任意 MetricsQL 代理、告警、聚合、rate、降采样或长期容量设计。
- 不接受 logs/events，不写 Elasticsearch，不创建额外 Topic、DLQ、重放/offset 管理 API 或 Schema Registry。
- 不引入本地持久去重库、Kafka transaction、分布式事务、跨 record batch 或 exactly-once 声明。
- 不部署 VictoriaMetrics cluster、vmagent、vmauth、多租户、高可用、TLS 或公网入口。
- 不新增 Marshaller 容器镜像，不修改冻结 PowerShell，不增加 Windows runner 或原生 Windows 验收。

## 5. 预计文件与交付物

```text
marshaller/go.mod
marshaller/go.sum
marshaller/cmd/marshaller/**
marshaller/internal/config/**
marshaller/internal/envelope/**
marshaller/internal/consumer/**
marshaller/internal/metrics/**
marshaller/internal/victoriametrics/**
marshaller/internal/httpserver/**
marshaller/README.md
router/**（仅真实交接阻断所需最小修复）
monitor/**（仅真实交接阻断所需最小修复）
deploy/compose.yaml
.env.example
scripts/dev.sh
scripts/down.sh
scripts/verify.sh
scripts/verify-marshaller.sh
.github/workflows/quality-gates.yml
.github/workflows/reusable-quality-gates.yml
scripts/ci/**（仅门禁和脚本契约测试）
README.md
VERSION
frontend/package.json
frontend/package-lock.json
dev/logs/Phase-08/Phase-08-01-Marshaller与VictoriaMetrics指标闭环.md
```

预计文件是允许边界，不要求制造无意义修改。若实现使用同等清晰的内部目录，应在实施记录说明；不得改变总方案冻结的公共契约。

## 6. 详细实施步骤

1. 核对 Phase 7 三份实施记录、PR #71 / `60f9aa8`、远程 checks、真实 Kafka/Router 代码和 record 样本；保存 Git 与日常资源快照。
2. 创建 Marshaller module 和严格配置加载，优先完成 token、地址、超时、消息/输出上限、brokers、Topic/group 和 URL 的定向测试。
3. 实现有界严格 Envelope decoder、重复 key 检测、key/ID 一致性和完整 metrics payload validator；以 fake writer 证明永久错误不会调用存储。
4. 实现稳定 transformer、标签转义、数值/时间戳和正文上限；用 golden 测试证明 map 顺序及同一 record 重放不改变 bytes。
5. 实现 VictoriaMetrics client、成功状态判定、安全错误和 timeout/redirect/body 边界；以 HTTP fixture 定向验证。
6. 实现 franz-go 正式 Consumer、手动提交、generation ownership lease、成功/永久无效/暂时写入失败处理、revoke/lost 取消、commit 失败停止推进和有界 shutdown；用可注入 Consumer/Committer/Writer/ownership 确定性验证写入前不提交、旧 generation 不提交、暂时失败保留 offset 和后续 record 不被越过。
7. 在 Compose 加入固定 VM 服务、loopback、Basic Auth、dedup、健康检查和 volume；对真实 Kafka+VM 执行定向集成验证。
8. 实现 Marshaller `/health`、鉴权 `/ready` 和结构化日志，确认 Cookie/JWT 不构成内部身份且日志无 payload/凭据。
9. 更新 `.env.example`、`dev.sh`、`verify.sh`、`down.sh` 的最小配置、端口、启动/关闭顺序和 PID/container/volume 归属；恢复与异常清理语义在 Phase-08-02 加固。
10. 建立 `verify-marshaller.sh` 自检和默认模式，完成真实 Redis success、target unavailable/恢复、一个代表性永久坏消息后继续、`vm_rows_invalid_total` 与基本查询矩阵；解析器错误全集与精确 ownership/commit 竞态保留在 unit/定向集成层，真实重复、broker rebalance 和 Kafka/VM/进程故障矩阵由 Phase-08-02 执行。
11. 增加 Marshaller CI 和脚本/Compose 门禁，更新 README；最终 diff 稳定后执行第 8 节固定验证一次。
12. 更新根与 Frontend 版本为 `1.5.1`，创建本批实施记录，只暂存本批文件，提交并创建 Pull Request。
13. 查询并记录真实远程 checks 与合入状态；未合入或失败时保持本批未完成。

## 7. 风险与控制

- **自动提交造成写入前丢消息**：客户端显式禁用自动提交；测试写入阻塞/失败并断言 committed offset 不推进。
- **毒消息永久卡住分区**：永久错误与临时存储错误由固定分类分离；永久错误不调用 VM、记录一次后提交并继续合法 record。
- **暂时写入错误被错误跳过**：网络、timeout、认证和任何非成功响应均不进入永久分类；fake writer 证明 offset 不提交。
- **拆批造成可靠性假象**：本批必须用确定性测试关闭 commit 失败、revoke/lost 和延迟响应的正确性；只将真实 broker restart/rebalance 恢复与运维矩阵留给 Phase-08-02。
- **204 掩盖逐行解析失败**：运行时只把它视为 transport acceptance；封闭转换器以 unit/golden 保证正文语法，真实验收对比 `vm_rows_invalid_total` 并查询全部预期时序。
- **高基数标签污染存储**：转换器只允许 source、target_id 与上游 `mode|db`，明确拒绝保留标签冲突，不加入 message/offset/version。
- **历史积压被年龄规则丢弃**：只拒绝不合法或过度超前 timestamp，不以固定过去窗口永久跳过 Kafka 积压。
- **VM Basic 凭据泄漏**：URL 不含 credentials，日志/错误不打印 header/response，验收检查输出；Frontend/Backend 不持有该凭据。
- **readiness 绑死社交业务**：VM 只影响 Marshaller readiness，Backend readiness 不新增依赖，故障时同时验证代表性社交 API。
- **验收绕过主链路**：成功主证据必须从真实 Redis/Exporter/Monitor/Router 产生，fixture 只覆盖异常注入。
- **脚本误删日常 VM volume**：随机 Compose project、label、container ID、端口和 volume 全部强归属校验，前后快照对比。

## 8. 固定验证命令与必要回归

最终 diff 上每项执行一次；失败修复后只重跑受影响命令或场景：

```bash
(cd marshaller && test -z "$(gofmt -l .)")
(cd marshaller && go test -count=1 ./...)
(cd marshaller && go vet ./...)
(cd marshaller && go test -race -count=1 ./...)
(cd router && test -z "$(gofmt -l .)")
(cd router && go test -count=1 ./...)
(cd monitor && test -z "$(gofmt -l .)")
(cd monitor && go test -count=1 ./...)
(cd exporters/redis && go test -count=1 ./...)
python3 -m unittest discover -s scripts/ci -p 'test_*.py'
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/1.5.1 --base-ref upstream/main
bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-business.sh scripts/verify-exporter.sh scripts/verify-monitor.sh scripts/verify-router.sh scripts/verify-marshaller.sh scripts/package-redis-exporter.sh
docker compose --env-file .env.example --file deploy/compose.yaml config --quiet
scripts/verify-marshaller.sh --self-test
scripts/verify-marshaller.sh
scripts/verify-router.sh --self-test
scripts/verify-monitor.sh --self-test
scripts/verify-exporter.sh --self-test
git diff --check
```

Marshaller 单元/定向集成测试固定覆盖：严格 Envelope/key、完整 success/up0 集合、标签转义、时间/数值/排序、输出上限、三类 offset 决策、HTTP acceptance/commit 失败、revoke/lost ownership、延迟响应竞态、退避取消、health/readiness 与有界 shutdown。更多排列只在真实失败证明需要时增加。

`scripts/verify-marshaller.sh` 是本批真实最小纵向链路、一个代表性永久异常后继续和真实查询的主证据；commit/ownership 竞态由本批确定性测试证明。Phase 7/6/5 self-test 只保护已有交接和基本资源安全；真实重复投递、broker rebalance、Kafka/VM/进程恢复和完整运维资源归属由 Phase-08-02 执行，完整社交业务回归由 Phase-08-03 执行。

## 9. 验收标准

- Marshaller 是独立 Go module 和正式 Consumer，可在正常路径健康启动、处理并有界关闭；配置非法时在连接/监听前退出。
- key/ID、Envelope、payload、状态、family、kind、labels、values 和样本集合均经严格第二次校验；永久异常不调用 VM且不阻断后续合法消息。
- 合法 record 确定性转换为固定低基数时序、Envelope Unix 毫秒和有限 Prometheus text；同一 record 重放正文逐 byte 相同。
- 封闭转换完成、VictoriaMetrics HTTP 接受且 generation lease 仍有效后才提交合法 record；永久无效 record 不写入并只在 ownership 有效时安全继续；暂时写入失败、commit 失败或 lost ownership 不提交且不越过后续 record，验收窗口内 `vm_rows_invalid_total` 不增加。
- 真实 Redis success 和 target unavailable/recovery 均经过完整上游并可查询；至少看到状态、连接、命令请求、CPU、内存和 keyspace。
- 同一 record 的 transformer 输出逐 byte 相同，文档、代码和日志均未把 at-least-once 描述为 exactly-once；真实重复与去重查询由 Phase-08-02 验收。
- Kafka、Marshaller 和 VM 只限内部/loopback，Cookie/JWT 无法替代内部身份，日志和响应不泄漏凭据、payload 或内部连接信息。
- 最小日常与隔离生命周期不误杀、不误删既有资源，正常验收后不遗留本批进程、端口、container、network、group fixture 或临时凭据文件；完整失败/中断清理由 Phase-08-02 验收。
- 第 8 节固定验证与远程门禁通过，根和 Frontend 版本为 `1.5.1`，实施记录真实完整。

## 10. 明确完成条件

只有正式 Consumer、手动 offset、generation ownership fencing、commit 失败不越过、严格第二次校验、确定性转换、VM 基本写入/查询、真实上游、代表性永久异常后继续、暂时写入失败不提交、内部身份和最小资源安全全部通过，且本批 Pull Request 已合入主远程 `main`、远程门禁成功，才可标记 Phase-08-01 完成。直接 import、静态 JSON、mock Kafka/writer 或只通过 unit test 均不足以完成本批。

## 11. 下一批交接

- 可独立运行的 Marshaller 最小 lifecycle、health/readiness、配置、日志、PID 和 CI 契约。
- 正式 group `gopulse-marshaller-metrics-v1`、从 earliest 启动、手动 offset 和成功/永久无效/暂时写入失败的基本处理结果。
- 独立严格 metrics Envelope v1 decoder/validator，以及确定性 Prometheus text transformer。
- 单节点 VictoriaMetrics 固定镜像、loopback、内部 Basic、持久 volume、1ms dedup、写入和内部查询契约。
- 真实 success/up0/recovery、一个代表性坏消息后继续和基本查询的本批证据。
- 可注入 Consumer/Committer/Writer/ownership seam，以及已通过确定性测试的 generation lease、revoke/lost、commit 失败和延迟响应语义；尚未宣称完成的是真实 broker rebalance、Kafka/VM/进程恢复、重复和异常清理。
- Phase-08-02 在该正确性基线上交付真实故障恢复与运维实现；Phase-08-03 再执行完整业务/访问隔离和 Milestone 2 远程收口。
