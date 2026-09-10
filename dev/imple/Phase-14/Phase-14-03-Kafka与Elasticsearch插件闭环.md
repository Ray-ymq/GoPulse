# Phase-14-03：Kafka 与 Elasticsearch 插件闭环实施方案

> 当前状态：未完成；`develop/1.11.3` 已提交采集器基础实现，尚未接入产品或通过批次验收。2026-09-10 经用户批准修订 Kafka 部分异常验收拓扑，修订仅为计划，不表示真实验收通过。本文档定义 Phase 14 第三个执行批次的范围与验收合同；目标版本 `1.11.3`、开发分支 `develop/1.11.3` 和执行顺序以 `Phase-14-总实施方案.md` 为准。

## 1. 批次目标

交付 Kafka 与 Elasticsearch 两种具有集群/节点语义的官方插件，把真实拓扑与消费进度聚合为稳定、低基数快照：

```text
Kafka protocol/admin metadata + 固定 GoPulse topic/group → kafka-exporter ─┐
                                                                  ├→ 完整既有 metrics 链路 → Backend
Elasticsearch cluster health/stats                    → elasticsearch-exporter ─┘
```

批次完成必须证明两个 Exporter 不依赖单一动态 broker/node 标签，部分异常与整体不可达有明确区别，指标可经 Backend 查询，且不改变 Kafka 单 Topic、Marshaller offset 和 Elasticsearch 业务/日志/事件索引归属。

## 2. 前置条件

- Phase-14-02 已合入 `upstream/main`，版本 `1.11.2`，Redis/MySQL/RabbitMQ 三插件及通用生命周期已通过。
- fetch 后从最新 `upstream/main` 创建 `develop/1.11.3`。
- 核对 Compose 锁定 Kafka 镜像/单 broker/topic/group 与 Elasticsearch 9.5.2 health/stats 响应，仅阅读实现必需的公开协议/API。
- 保留现有 Router producer、Marshaller consumer group、Search Indexer、search alias 与 Logs/Events index 为不可被插件采集修改的基线。

### 2.1 正常、初始与部分异常合同

- 严格执行总方案 §10.1：Kafka group/topic 不存在或任一 partition 无有效 committed offset 是安全采集失败，不填零、不创建 topic、不提交 offset。必须通过真实可观测消息与正式 Marshaller 消费建立正常基线。
- 产品保持锁定单 broker 拓扑；仅部分异常真实验收允许按 §2.2 在强归属隔离环境临时增加一个同版本 follower broker。必须取得并恢复至少一种完整快照可读的部分异常，不直接改生产 broker，不扩展多 broker 产品能力；步骤未实证前不得标通过，不能用 fixture 替代。
- Elasticsearch 固定 primary docs/store 范围；对 health=yellow/red 且完整字段可得输出 up=1，不能把健康枚举等同连接成功与否。

### 2.2 Kafka 部分异常：仅限验收的临时第二 broker

#### 授权与不变边界

2026-09-10 的真实前置探测已确认：锁定 Kafka 4.3.1 的官方工具拒绝未注册 broker，
直接 AlterPartitionAssignments 请求也返回 partition ErrorCode=39；该路径不能产生
所需的可读 under-replicated 快照。用户已批准仅在强归属测试环境临时增加第二 broker。
这不是生产扩容、产品多 broker 支持或验收通过声明；探测事实见本批同名开发记录。

- 产品 `deploy/compose.yaml` 默认拓扑、Schema、一个 plugin ID/target/进程、唯一目标
  `kafka:19092`、固定 topic/group、生产客户端拨号 allowlist 和发布制品保持不变。
- 第二 broker 仅由验收 harness 的临时 Compose override 启动，使用与主 broker 相同的
  锁定镜像 `apache/kafka:4.3.1`，作为 broker-only follower，不加入 controller quorum。
  使用该次随机 project、专属 network/volume、唯一 node ID，不发布宿主端口，不共享生产数据。
  不提供用户配置入口、额外目标发现、生产环境开关或测试专用 Exporter 二进制。
- Exporter 始终只允许连接原目标；Metadata 中可见第二 broker 不代表允许拨号到它。
  固定 topic 所有 partition 的 leader、正式 group coordinator，以及该 group 必需的
  offsets 分区 leader 必须保持在原 broker。若协议库尝试其他 origin，保持拒绝；
  不得为通过验收扩大生产 allowlist 或跳过完整快照检查。
- 验收管理员可在本次隔离 topic 上调整 replica assignment；这是故障准备/恢复行为，
  与 Exporter 只读权限和代码严格分离。不得重置/伪造/手工提交正式 Marshaller offset，
  不新增业务 topic，不改变业务数据或持久 broker 配置。账号授权与所执行写命令需记录。

#### 必须实际执行的步骤与证据

1. 先按原单 broker 拓扑运行真实 Router → Kafka → 正式 Marshaller 消费，确认固定 topic
   每分区有效 committed offset；保留缺 offset 初始失败与正常恢复的原验收。
   记录 topic 列表、replica assignment、关键配置、leader/coordinator 和采集器进程身份。
2. 在同一强归属环境加入临时 follower；仅将固定 topic 的副本扩为两份，等待同步与
   reassignment 完成。验证 leader/coordinator 仍在原目标；若未满足则恢复并报告失败，
   不让采集器连接新 origin。保存正常 metadata/offset 与 7 families / 7 samples 快照。
3. 仅停止临时 follower，保持原 broker/controller、正式消费链路和 Exporter 运行；
   有界等待 metadata 的 under-replicated partition 数大于零。此时必须得到 HTTP 200、
   `gopulse_kafka_up=1` 和完整 7 families；对照真实 metadata/offset 验证 partition 数及
   `sum(max(log_end_offset - committed_offset, 0))`，并通过 Backend 查询 up 与该异常拓扑值。
   不用停止原 broker 或唯一 leader 的不可达结果冒充该项；不额外要求全部 offline 异常组合。
4. 重启同一 follower，等待 ISR 恢复、under-replicated=0；验证 Exporter 同一进程恢复
   正常快照，Backend 出现恢复后的新采样。记录采样时间，避免把历史值当作故障/恢复证据。
5. 将固定 topic 恢复为原 broker 单副本，等待 reassignment 完成，再移除临时 follower
   及其资源；核对回到产品单 broker 基线。分别在正常/故障/恢复稳定阶段对照采集前后
   topic/config/assignment 与正式 offset，区分验收管理员写操作、正常业务消费和采集器行为。
6. 在恢复后的单 broker 产品拓扑继续执行原定完全不可达/恢复、无副作用和业务回归。
   失败或中断也必须由强归属 cleanup 回收本次创建的全部资源，并证明原有资源未被修改。

固定入口仍为 `bash scripts/verify-plugin-metrics.sh --sources kafka,elasticsearch`；
临时第二 broker 的创建、采样断言、恢复与清理须由该入口管理，不作为手工跳过的额外门禁。
`--self-test` 应覆盖临时资源的归属/清理约束，但不能代替上述真实运行。
记录须包含实际镜像、harness 命令、受控写操作、时间窗口、脱敏数值、进程身份与清理结果。
此例外只解决真实部分异常的注入条件，不削减其余验收标准；实际步骤仍失败时按总方案 §16.1
报告具体原因，不能据本计划文字宣称可行或通过。

## 3. 实施范围

### 3.1 Kafka 官方插件

- 新增独立 `kafka-exporter` 源码/模块，使用当前项目已选协议库的公开 API 或等价最小客户端获取 broker/controller/partition 与消费位点摘要；不开启 topic 自动创建，不生产/消费业务记录。
- Schema 只接受受控 broker service/port、超时和代码固定的 `gopulse-observability-v1` topic / `gopulse-marshaller-metrics-v1` group 选择；不接受任意 broker 数组、topic/group/client ID。
- 固定总方案全部 Kafka families；partition 及 lag 仅该固定 topic，lag 缺 committed offset 的语义严格按总方案 §10.1；不使用 broker ID、partition ID、client ID 作标签。
- 部分状态（如 under-replicated/offline 非零）在可成功取得一致快照时仍是 HTTP 200 完整指标；连接/认证/超时/协议失败才固定 `503` 与唯一 `gopulse_kafka_up 0`。

### 3.2 Elasticsearch 官方插件

- 新增独立 `elasticsearch-exporter` 源码/模块，从锁定 REST health/stats 端点取得集群聚合；禁止自定义 path/query、搜索 body 或索引修改。
- Schema 只接受受控 host/port/可选认证/timeout，container mode 仅允许 `elasticsearch`；不接受完整 URL、index 名或 TLS 文件路径。
- 固定 up、health 枚举、node/data-node count、active primary/total shards、relocating/initializing/unassigned shards、pending tasks、primary docs/store 聚合，不重复计入 replica；不输出 node/index/shard 名。
- yellow/red 在 API 可正常返回时以完整 HTTP 200 指标表达；网络/认证/超时/解析失败固定 `503` 与唯一 `gopulse_elasticsearch_up 0`。

### 3.3 通用接入与全链路

- 为两类生成可复现 Manifest v2 包、嵌入 Monitor 镜像并在空卷独立调和；使用独立回环端口、process record、collector 和 per-ID lock。
- Monitor/Marshaller 双层注册精确 families/kinds/counts/labels；Router 只扩展固定 source allowlist；Backend 添加固定 source/target/producer catalog definitions。
- Frontend 启用 Kafka/Elasticsearch 的 Schema form、connection-test、install/start/stop/update、状态与 metrics 入口，不显示 broker/node/index 明细。
- 扩展聚焦验收，使用真实 Kafka/Elasticsearch 而非 fixture 证明数值、部分状态、整体不可达、恢复和 Backend 查询。

### 3.4 不可改变的共享系统边界

- Kafka Exporter 不创建/删除 Topic，不提交 Marshaller offset，不更改 broker configuration；采集账号若新增只给 metadata/group-offset 读权限。
- Elasticsearch Exporter 不创建/删除 index/template/alias，不触发 reindex，不读文档 `_source`。
- 这两种目标同时是 GoPulse 自身传输/存储依赖；采集不得改变它们的 readiness、容量或业务数据。

## 4. 不在本批范围

- VictoriaMetrics Exporter、自研组件 metrics、Kafka/Elasticsearch 配置管理。
- 多 broker 产品拓扑/target、任意 topic/group/index/node/shard 展示、采集目标发现或通用 cluster explorer；§2.2 的隔离临时 follower 仅是验收设施例外。
- 告警、独立管理前端、新数据库、Kubernetes 或业务搜索改造。

## 5. 建议实施顺序

1. 固定 Kafka/Elasticsearch 配置 Schema、读权限、上游 API 与指标目录。
2. 实现 Kafka Exporter，用真实 broker/topic/group 锁定正常、部分异常和不可达。
3. 实现 Elasticsearch Exporter，用真实 cluster 锁定 green/yellow-or-red/不可达。
4. 接入包、Monitor、Router、Marshaller、Backend/Frontend 与 Compose，证明不修改 Topic/offset/index/alias。
5. 运行真实 source-focused 验收和既有三插件/业务代表性回归。
6. 更新版本为 `1.11.3`，完成实施记录、固定门禁和提交。

## 6. 预计直接影响文件

- 新 `exporters/kafka/*`、`exporters/elasticsearch/*`
- 包构建脚本/通用 packager 目录
- `monitor/internal/plugin/*`、`monitor/internal/metrics/*`、Monitor README/镜像
- `router/internal/envelope/*`
- `marshaller/internal/envelope/*`、`marshaller/internal/metrics/*`
- `backend/internal/exporterplugin/*`、`backend/internal/metricquery/*`、`backend/internal/http/*`
- Frontend exporter service/types/view/tests
- `deploy/compose.yaml`、相关 Dockerfile、`.env.example`
- `scripts/verify-plugin-metrics.sh`
- `VERSION`、Frontend 版本元数据、README/指标契约文档
- `dev/logs/Phase-14/Phase-14-03-Kafka与Elasticsearch插件闭环.md`

## 7. 批次验收标准

### 7.1 Kafka

- 真实 broker/controller/topic/group 的固定聚合指标完整，Backend 可查 `gopulse_kafka_up` 和至少一个真实拓扑/滞后值。
- 部分异常用 200 完整快照表达，停 broker/错凭据/超时用唯一 `up=0`；恢复不重启 Exporter。
- 采集前后 Topic 列表、关键配置和 Marshaller 正式 committed offset 除正常业务消费外无采集器副作用。

### 7.2 Elasticsearch

- 真实 cluster 可查 `gopulse_elasticsearch_up`、health 与至少一个真实 shard/docs/store 聚合值，source/target/producer 不混淆。
- yellow/red 与整体不可达区分正确；停服务/错认证/超时后同进程恢复。
- 采集前后业务 search alias/document、Logs/Events template/index 与数据不被 Exporter 写入或删除。

### 7.3 隔离与回归

共享基础设施实际停机按总方案 §12 验证其真实依赖降级；以下“其他插件与业务不受影响”限定单 ID 的连接/进程/配置故障，不要求 Kafka 停机期间指标仍可写入或 Elasticsearch 停机期间搜索仍成功。

- 五种插件可同时 running，一种 target/process/config/update 失败不中断其他四种、历史 metrics 和社交业务。
- 空卷调和、同卷 Monitor 替换与各 desired state 恢复正确；不出现同 ID 第二进程/目标。
- 管理员 Frontend 闭环和普通用户拒绝正确，Secret/连接串/broker/node/index 明细不进入公共产物。

### 7.3.1 初始值与聚合断言

- 覆盖一次 Kafka 缺 committed offset 的真实初始失败，经正常消费形成 offset 后恢复；Exporter 未修改正式消费进度。
- 对照同一可解释采样窗口的真实 metadata/offset/primary stats，核对聚合公式和单位，不对动态计数要求两次读取完全相同。
- 一次真实 Kafka 部分异常须按 §2.2 完成临时 follower 停机、完整快照、Backend 查询、同进程恢复和回归单 broker；一次 Elasticsearch yellow/red 可读快照按前置记录恢复。复用该证据，不另做所有状态组合。
- Redis 查询仍遵循总方案 §9.3 的旧 label 例外，不能为统一新 source 而修改历史查询。

### 7.4 完成条件

两个真实目标的正常/部分异常/不可达/恢复、Backend 查询、无副作用和必要回归全部通过，实施记录完整，版本为 `1.11.3`，提交只包含本批文件时才完成。

## 8. 固定验证命令与回归范围

```bash
(cd exporters/kafka && go test ./...)
(cd exporters/elasticsearch && go test ./...)
(cd monitor && go test ./...)
(cd router && go test ./...)
(cd marshaller && go test ./...)
(cd backend && go test ./internal/exporterplugin ./internal/metricquery ./internal/http)

(cd frontend && npm test)
(cd frontend && npm run typecheck)
(cd frontend && npm run build)

bash scripts/verify-plugin-metrics.sh --self-test
bash scripts/verify-plugin-metrics.sh --sources kafka,elasticsearch
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/1.11.3 --base-ref upstream/main
git diff --check upstream/main...HEAD
```

- 真实验收必须捕获查询数值与 Kafka metadata/group offset、Elasticsearch health/stats 的一致证据，不提交凭据或原始大响应。
- 回归固定为前三插件代表性查询、Router/Marshaller offset 语义、业务搜索、Logs/Events 和 Phase 13 一条主路。
- 只有共享 Kafka/Elasticsearch 副作用或 envelope 失败证据才允许扩大验证，先在记录写明原因。

## 9. 实施记录与下批交接

完成前创建：

`dev/logs/Phase-14/Phase-14-03-Kafka与Elasticsearch插件闭环.md`

记录实际 Kafka/Elasticsearch 版本与 API、最小权限、family/label/count、制品、真实命令/结果、部分异常与副作用核对、偏差/限制，并写清 Phase-14-04 可依赖的五插件并行边界。
