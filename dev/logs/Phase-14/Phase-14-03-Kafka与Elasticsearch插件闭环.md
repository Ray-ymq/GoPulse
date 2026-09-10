# Phase-14-03：Kafka 与 Elasticsearch 插件闭环开发记录

## 状态

**未完成：Kafka 单 broker 部分异常的真实验收注入路径尚未成立。**

本次仅提交未接入产品的两类 Exporter 基础实现与前置探测记录，不代表交付两个插件。
目标仍为 `1.11.3` / `develop/1.11.3`；根 VERSION、Frontend 与环境版本保持
已完成产品版本 `1.11.2`，不为使分支检查通过而提前提升版本。没有推送或创建 PR。

## 分支与范围

- 开始时工作树干净，位于上一批 `develop/1.11.2`。
- 实际执行 `git fetch origin`，从 `origin/main` 的 `e2385f1` 创建
  `develop/1.11.3`。origin/upstream 均指向同一仓库；随后执行 `git fetch upstream`，
  确认 `upstream/main` 同为 `e2385f1`，其 VERSION 为 `1.11.2`。
- 直接读取本批/总方案、前批记录、既有 RabbitMQ Exporter 与插件 adapter/schema、
  采集目录及既有强归属 Compose harness。没有委派子代理、读取第三方依赖源码、
  审计无关业务或修改现存容器。
- 使用本地 `go doc` 阅读锁定 franz-go/kmsg 的公开 API；没有新增依赖版本。

## 实际实现（未集成）

### Kafka

新增 `exporters/kafka/go.mod`、`go.sum`、`README.md`、
`cmd/kafka-exporter/main.go`、`internal/collector/collector.go`、
`internal/collector/collector_test.go`、`internal/runtime/runtime.go`。

- 沿用 franz-go `v1.21.0` / kmsg `v1.13.1`，固定 Kafka 2.8 请求形状以使用 OffsetFetch v7；
  目标 Compose 镜像仍为 `apache/kafka:4.3.1`。
- 只读 Metadata、ListOffsets、OffsetFetch，固定 topic/group，不自动建 topic、
  不生产/消费消息、不提交 offset；缺 committed offset 返回安全失败。
- 无标签 7 gauge families / 7 samples，lag 按固定 topic 分区求和。
- container 目标限 `kafka:19092`，host 为 loopback:9092，拒绝其他 advertised dialing origin；
  监听回环 9124，提供 `/health`、`/metrics` 与安全 `--check`。
- 单测验证只读请求、缺 offset 失败及有效 offset 后 lag 汇总；**不计为真实冷启动或部分异常证据**。

### Elasticsearch

新增 `exporters/elasticsearch/go.mod`、`README.md`、
`cmd/elasticsearch-exporter/main.go`、`internal/collector/collector.go`、
`internal/collector/collector_test.go`、`internal/runtime/runtime.go`。

- 固定 GET `/_cluster/health` 与 `/_stats/docs,store?level=cluster`；documents/store
  取 `_all.primaries`，不读 replica-inclusive totals，不写 index/alias/template。
- 12 gauge families / 14 samples，仅 health 使用三个 one-hot status；失败为 503 与唯一 up=0。
- container 目标限 `elasticsearch:9200`，host 为 loopback:9200；可选认证必须成对，
  禁止重定向/环境代理，响应上限 1 MiB，监听回环 9125。
- 单测区分 primary/total，验证 yellow 可读、缺字段失败及恢复；**不计为真实 9.5.2 验收**。

未生成 Manifest/可信 digest，未注册 Monitor/Router/Marshaller/Backend/Frontend，
未修改 Compose，未启用产品目录。现有三个插件运行路径保持原样。

## 真实 Kafka 前置探测与合同缺口

使用现有 `Acceptance` 的随机 project、私有 env、资源快照、归属校验和 cleanup，
仅启动该次新建的 Kafka。未操作运行中的 `gopulse-kafka-1` 或其他既有资源。

临时驱动：`.run/p1403/probe.py`；直接协议 helper 源码/二进制：
`.run/p1403/reassign.go` / `.run/p1403/reassign`。临时资料不纳入提交。

实际步骤：

1. 强归属新环境启动锁定 `apache/kafka:4.3.1` 并等待健康。
2. 在该空环境创建固定 topic `gopulse-observability-v1`，1 partition / replication factor 1。
   这是隔离验收准备，不是 Exporter 的行为。
3. 以官方 `kafka-reassign-partitions.sh --execute` 提交
   `{"version":1,"partitions":[{"topic":"gopulse-observability-v1","partition":0,"replicas":[1,999]}]}`。
4. 工具拒绝：`Unknown broker id 999`，来源 `verifyBrokerIds`。
5. 为区分 CLI 限制与服务端限制，在第二个新建隔离环境使用锁定 kmsg 的公开
   `AlterPartitionAssignmentsRequest` 直接提交同样分配（仅探测 helper 使用写 API，
   Exporter 不包含此请求）。顶层 ErrorCode=0，但 partition ErrorCode=39，消息为：
   `The manual partition assignment includes broker 999, but no such broker is registered.`
6. 两次 describe 均显示 ReplicationFactor=1、Leader=1、Replicas=1、ISR=1，未形成部分异常。
7. 两次均完成强归属清理，harness 报告原有资源保留。

实际输出：

- `.run/p1403/probe.log`，project `gopulse-p1401-3ec54c235b76`。
- `.run/p1403/probe-api.log`，project `gopulse-p1401-2c1232db415a`。
- 对应 `.run/<project>/` 保留 harness 证据。

结论严格限于：**该未注册副本注入路径已被真实服务端否定，当前没有已证实可满足合同的
单 broker 部分异常步骤**；没有声称穷尽所有 Kafka 故障机制。停止 broker 或让唯一 leader
不可读不能代替“完整 metadata/offset/lag 可读取”的部分异常验收。

依本批 §2.1 / 总方案 §16.1，不用 fixture 冒充真实部分异常，不扩建现有生产集群，
不静默降低验收合同。需要先在 `update` 明确可执行的合同修订或补充可信单 broker 注入步骤，
再恢复该批实现。候选修订可讨论仅在强归属验收中临时允许第二 broker（不增加产品多 broker
能力），但**本次未采纳、未修改合同、未启动第二 broker**。

## 实际验证

| 命令/检查 | 结果 |
| --- | --- |
| `(cd exporters/elasticsearch && go test ./...)` | 基础编译通过；添加直接验收单测后再次通过 |
| `(cd exporters/kafka && go test ./...)` | 基础编译通过；引入只读接口及直接验收单测后再次通过 |
| `python3 .run/p1403/probe.py`（CLI 注入） | 驱动正常退出并清理；注入被拒绝，不能计为部分异常验收通过 |
| `CGO_ENABLED=0 go build -o ../.run/p1403/reassign ../.run/p1403/reassign.go`（router 模块内） | 公开协议探测 helper 构建成功 |
| `python3 .run/p1403/probe.py`（增加直接协议探测） | 驱动正常退出并清理；服务端 partition ErrorCode=39 |
| `python3 scripts/ci/validate_versions.py` | 通过：已完成版本元数据一致为 1.11.2 |
| `python3 scripts/ci/validate_branch.py --branch develop/1.11.3 --base-ref upstream/main` | **未通过**：VERSION 为 1.11.2，门禁要求目标 1.11.3；本批未完成，不提前提升版本 |

未执行产品集成测试、Frontend npm 门禁、`verify-plugin-metrics.sh --sources kafka,elasticsearch`、
真实 ES、正式 Marshaller offset 初始失败/恢复、完整 metrics 链路、五插件隔离、浏览器与业务回归。
原因是尚未接入产品且真实 Kafka 部分异常合同前提未解决；不将未运行检查写为通过。

## 后续恢复位置

1. 先解决上述真实 Kafka 部分异常合同/注入路径；必要时在 `update` 修订总/分方案。
2. 验证真实 Elasticsearch 9.5.2 primary 聚合、yellow/red、不可达恢复及权限；
   对已有基础实现补齐真实采样发现的问题，不把当前单测当作产品验收。
3. 完成本批原定制品、schema adapter、链路双层目录、管理与 Frontend 接入。
4. 执行本批固定完成门禁和必要真实回归；全部通过后才提升到 1.11.3 并标完成。

Phase-14-04 当前仍只能依赖上一批已验收的三个插件，不能依赖本批五插件闭环。

提交前 `git diff --check` 与 `git diff --cached --check` 均通过；仅暂存上述新增模块与本记录。
本次为未完成批次的基础实现提交，不是发布提交，不触发推送/PR。
