# Phase-14-03：Kafka 与 Elasticsearch 插件闭环开发记录

## 最终状态

**已完成：Phase-14-03，产品版本 `1.11.3`，分支 `develop/1.11.3`。**

最终固定真实命令退出码 0，软件检查、真实 Kafka/Elasticsearch（含认证）故障/恢复、
19 个新增 families 的 Backend 查询、五插件隔离/更新/同卷恢复、空卷浏览器安装与业务回归均通过。
生产仍为单 broker；临时 follower 与安全启用 ES 仅属强归属验收设施，已全部清理。
版本更新仅在完整门禁成功后执行。未推送、未创建 PR。

下列“基础实现/未完成/待继续”描述是首次尝试与后续失败轮次的历史记录，保留以解释合同修订
和观测驱动的修复；**最终交付与验证以本文末尾收口段为准**，不将历史失败轮次计为成功。

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

## 授权后继续实施（进行中，尚未完成）

- 用户批准仅在隔离验收环境临时增加 follower；`update` 计划提交 `2afea62` 已以
  `0ddd7c0` 同步至本批分支。只解决总方案状态段冲突，保留已批准的 §10.2 / 分方案 §2.2。
- `.run/p1403/follower-probe.py` 在新建强归属 project
  `gopulse-p1401-cf9215cc780d` 实际通过：原单副本 → 两副本 ISR 同步 → 仅停 follower 后
  leader=1、ISR=1 → 恢复 follower → 回到单副本；全部临时资源清理，既有资源保留。
  输出 `.run/p1403/follower-probe.log`。这只是拓扑探测，不替代正式 offset/Exporter/Backend 门禁。
- 已接入两类可选/无 Secret 配置 adapter、官方可用目录、Monitor/Marshaller 精确 family/label/count、
  Router source、Marshaller 实际写入 target map、Backend metric/event provenance、Frontend Schema form
  与健康标签，并扩展受信 Manifest 构建。完整验收入口正在执行，不提前标完成。
- 直接影响 event source allowlist，故将 Monitor/Marshaller/Backend 对应事件校验同步扩展为五插件；
  Backend 最终包门禁增加 `./internal/eventquery`，仅覆盖直接变更的事件公共契约。
- 新真实验收 `scripts/ci/verify_plugin_topology.py` 复用已有强归属 harness，纳入固定
  `--sources kafka,elasticsearch`。临时 follower 管理与清理纳入此入口；新增浏览器 spec。

### 已执行检查与观测到的问题

- `.run/p1403/config-monitor.log`：最初 Monitor 目录测试仍期待 3 个可用插件，失败；
  将直接受影响期望改为 5 后，`.run/p1403/local-monitor.log` 通过。
- `.run/p1403/local-{monitor,marshaller,backend,frontend}.log`：对应受影响包测试与
  Frontend typecheck 通过。
- `.run/p1403/topology-tests-{monitor,marshaller,backend}.log`：新增/扩展测试通过，证明
  Kafka 无 Secret、ES 可选认证成对与保留、双层 one-hot health、source/provenance 边界。
- Frontend 首次 npm test 因旧目录测试将 health/controller 布尔量误判为 count 失败；
  修正测试推导后 npm test、npm run build 通过，输出 `.run/p1403/build/frontend-check.log`。
- 原 Dockerfile 构建在解析 `docker/dockerfile:1.7` 时失败：daemon proxy
  `127.0.0.1:7890` 拒绝连接。沿用前批路径，以 `.run/p1403/build/` 临时 Dockerfile
  仅省略 syntax 指令、保留所有生产 stage/锁定 base image；未修改全局 Docker/代理配置。
  Monitor/Router/Marshaller/Backend 镜像已构建；Frontend 使用宿主构建静态产物与上一批相同
  nginx runtime。测试镜像另编译登记可信成功/失败包，生产镜像不含失败制品。
- 第一次完整真实门禁 `.run/p1403/acceptance.log` 发现 Monitor constructor 仍拒绝新 source，
  在空卷启动失败。已同步 constructor 及直接受影响的 Monitor event allowlist，并在已有
  source contract 测试增加 constructor 断言；`.run/p1403/monitor-start-fix.log` 通过。
  该次主动终止，执行 harness 清理，未记为验收成功；后续运行输出 `acceptance-2.log`。

成功证据不会因上下文重建重复执行；从实际未通过的真实门禁继续。最终完成后再更新本记录状态与 VERSION。

### 真实门禁驱动的修复（继续）

- `acceptance-2.log`：正式 offset 已存在但 Kafka connection-test 仍失败。最小公开 API
  options probe（`.run/p1403/kafka-options.go`，未读依赖源码）确认 franz-go 要求
  BrokerMaxReadBytes 不小于 FetchMaxBytes；原 1 MiB 响应上限与默认 50 MiB fetch 上限冲突。
  将无消费客户端的 FetchMaxBytes 固定为 512 KiB，保留 1 MiB 响应限制；新增 constructor
  回归测试并通过，结果 `checks/kafka.log`。
- `acceptance-3.log`：冷启动检查使 Topic 列表变化。实际原因是首次 FindCoordinator
  会让 Kafka 自动初始化内部 `__consumer_offsets`。修复为先用 AllowAutoTopicCreation=false
  的 Metadata 检查固定业务 Topic 与该内部 Topic；缺失则在任何 coordinator 请求前失败。
  新增对应冷启动防副作用测试；业务 Topic 指标范围不变，不暴露内部 Topic 明细。
- `acceptance-4.log`：connection-test 通过而 install 失败。实际启动进程的既有环境 allowlist
  漏了 Kafka 的固定 TOPIC/CONSUMER_GROUP 字段；只为 Kafka 增加这两个字段，不放宽任意环境
  注入。`process-env-fix.log` 中 plugin 包测试通过；重新构建 Monitor/acceptance 镜像，
  `acceptance-5.log` 为后续真实运行。每个失败运行均已强归属清理，未声明通过。
- 为直接覆盖本批可选认证/错误凭据合同，固定真实门禁增加独立私网的安全启用 Elasticsearch 9.5.2
  fixture 与同一生产包 Exporter：monitor-only 账号健康/stats 允许、建索引和搜索拒绝、
  服务端换密码后唯一 up=0、恢复密码后同进程恢复。该场景不改变正常业务 ES 的安全配置，
  不扩展全量权限审计。成功与否以最终真实命令结果为准。

- `acceptance-5` 已取得真实五插件正常采集与 19 个新增 families 的 Backend 查询证据；
  follower 停机期间真实 Metadata leader 留在原 broker、under-replicated=1，Backend
  新采样实际返回 1。仍继续执行恢复、ES、故障/浏览器门禁，不能仅凭该中间证据标完成。
- 从该次强归属 Monitor 读取两个生产包及其 binary，使用同一 packager/version/arch 重新打包；
  两个压缩归档逐字节一致。文件保留 `.run/p1403/packages/`，没有创建不归属验收的 Docker 资源。

- `acceptance-5.log` 完成了真实 Kafka 部分异常/Backend/恢复、ES yellow primary 聚合、
  两类共享依赖停机与业务局部降级、正式 offset 无写入及 alias/template 无改动；随后
  Kafka 失败包上传被 Nginx 提前返回 413，尚未到达信任校验/试启动，因此该轮未完成。
  精确原因：现有 65 MiB 上传上限只匹配 Redis update 路径，其余路径沿默认 1 MiB。
- 该观测属于共享管理上传公共契约风险：为五个已交付 source 的**精确** update 路径设置
  同等 65 MiB proxy 上限（Backend 仍执行 64 MiB archive 上限），保留普通 API 的 1 MiB
  限制，不开放任意 source/URL。因此同时补齐直接受影响的 MySQL/RabbitMQ update 路径。
  前端静态产物未变，复用已通过 npm test/build 的文件，仅重建 nginx 配置层；构建中 `nginx -t` 通过。
- 浏览器固定场景改用真实空插件卷，实际点击 Kafka/ES 无必填 Secret 的 connection-test、
  install、configuration、stop/start 与 metrics，再恢复原五插件 desired-state 卷；
  不是用 Backend install 替代发生改动的 Frontend 无密码安装按钮验收。
- 后续固定真实门禁输出 `acceptance-6.log`。当前已取得生产包可复现 SHA-256：
  Kafka `4c11b826bf4ee3076a9faf1abac4be608a2d1ae9dad9203aa859b557ac26b09c`；
  Elasticsearch `161ecdd6852d95f0fca3e852f1d0711c03ff97d9c576e67240d8e944ceda63ab`。

- `acceptance-6.log` 未进入产品断言，因临时 Frontend 构建层以 root 运行 `nginx -t` 创建
  root-owned `/tmp/nginx.pid`，实际非 root 容器无法启动。独立 `--rm --network none` 的
  同镜像非 root `nginx -t` 实证返回 permission denied；这不是业务或代理配置语义错误。
  仅修正 `.run` 临时构建层，在配置校验后移除该 pid 文件；不修改生产 Dockerfile 逻辑。
  `checks/nginx-runtime.log` 的同镜像非 root 校验已通过；后续真实门禁为 `acceptance-7.log`。

- `acceptance-7.log` 已通过真实数据/故障/恢复、可信失败包回滚与更高版本成功更新、
  同卷 desired-state 恢复。真实浏览器也已执行两类 connection-test/install/configuration/
  stop/start 和 up 查询，但最后 health 指标选择的 `getByLabel('指标', exact=true)`
  无法匹配既有 select 的实际标签，测试超时。仅改为表单内第一个 select 的精确位置，
  不改生产 UI、不跳过三条 health 时序断言。
- 独立 ES 认证 fixture 调整为 source-focused 入口最先执行的独立步骤，以尽早发现尚未执行的
  认证门禁错误；它的私网/数据卷与业务目标隔离，不影响 Kafka 的真实冷启动顺序。
  后续输出 `acceptance-8.log`，同样由固定命令运行并统一清理。
- 工具链实际值：宿主 `go1.26.7 linux/amd64`、Node `v24.20.0`、npm `11.19.0`；
  生产 Go 镜像使用仓库锁定 `golang:1.26.0-alpine3.23`。Frontend 使用宿主同版本 Node 构建
  并以既有锁定 nginx runtime 验收，不声称原始 Dockerfile syntax 拉取已修复。

## 最终固定软件检查证据（不单独代表真实门禁完成）

成功后未因文档整理、上下文恢复或版本元数据更新重复执行未受影响检查：

| 固定检查 | 实际结果 / 输出 |
| --- | --- |
| `(cd exporters/kafka && go test ./...)` | 通过；`checks/kafka.log`，包含真实失败驱动的冷启动和客户端选项回归 |
| `(cd exporters/elasticsearch && go test ./...)` | 基础实现阶段已通过；之后未改其 Go 源码，沿用本文前述成功结果 |
| `(cd monitor && go test ./...)` | 通过；`checks/monitor-final.log`，覆盖最终 process env 转发修复 |
| `(cd router && go test ./...)` | 通过；`checks/router.log` |
| `(cd marshaller && go test ./...)` | 通过；`checks/marshaller.log` |
| `(cd backend && go test ./internal/exporterplugin ./internal/metricquery ./internal/http ./internal/eventquery)` | 通过；`checks/backend.log`，eventquery 是直接受影响的既定扩展 |
| `(cd frontend && npm test)` | 17 个文件 / 70 项通过；`build/frontend-check.log` |
| `(cd frontend && npm run typecheck)` | 通过；`local-frontend.log`，同次 build 也执行类型检查 |
| `(cd frontend && npm run build)` | 通过；`build/frontend-check.log` |
| `bash scripts/verify-plugin-metrics.sh --self-test` | 通过；`checks/self-test.log` |
| `bash -n scripts/package-redis-exporter.sh scripts/verify-plugin-metrics.sh` | 通过 |
| `python3 -m py_compile scripts/ci/verify_plugin_topology.py scripts/ci/verify_plugin_metrics.py` | 通过，后续每次修改该驱动均检查语法 |
| 两类当前 Manifest v2 包用原二进制重新打包逐字节比较 | 通过，归档及重建产物见 `packages/` |
| 修复后的 Frontend 镜像以实际非 root 用户执行 `nginx -t` | 通过；`checks/nginx-runtime.log` |

以上相对输出目录均为 `.run/p1403/`。完整真实命令、版本/分支与最终提交结果另见收口段。

## 本批实际文件范围

- `exporters/kafka/`、`exporters/elasticsearch/`：独立模块、只读 collector/runtime/main、直接契约测试与指标映射 README。
- `monitor/internal/plugin/`：adapter、闭合配置/Secret 规则、可用目录、固定 Kafka 进程环境；复用原生命周期，不另建 Manager。
- `monitor/internal/metrics/collector/`、`monitor/internal/events/contract.go`：source constructor、精确指标/one-hot 契约与事件 ID。
- `router/internal/envelope/envelope.go`：仅扩展固定 metrics source allowlist。
- `marshaller/internal/envelope/`、`internal/metrics/transform.go`、`internal/events/events.go`、`cmd/marshaller/main.go`：重验、producer labels、事件及实际 consumer targets。
- `backend/internal/exporterplugin/`、`internal/metricquery/`、`internal/eventquery/eventquery.go`：配置/catalog DTO、固定指标/health labels 与事件公共边界。
- `frontend/src/services/`、`src/types/observability.ts`、`src/views/ObservabilityExportersView.vue`、`e2e/phase14-topology.spec.ts`：可选认证/无 Secret 表单、目录解析、三条 health 时序与真实空卷浏览器闭环。
- `deploy/docker/observability.Dockerfile`、`deploy/docker/frontend/nginx.conf`、`scripts/package-redis-exporter.sh`：官方/验收制品与闭合大包上传路径。
- `scripts/ci/verify_plugin_metrics.py`、`verify_plugin_topology.py`：现有入口下的强归属真实验收与临时拓扑清理。
- 根/Exporter/Monitor/插件部署 README、本批总/分方案与同名日志：合同修订、实际映射、故障证据和限制；完成后同步受管版本元数据。


## 最终收口与 Phase-14-04 交接

### 完整真实门禁

`bash scripts/verify-plugin-metrics.sh --sources kafka,elasticsearch` 最终退出码 **0**。

- 总输出：`.run/p1403/acceptance-8.log`。
- 强归属 project：`gopulse-p1401-072fd79e2811`。
- 脱敏结果：`.run/gopulse-p1401-072fd79e2811/results.json`、`topology-evidence.json`、`browser.log`。
- Playwright：`e2e/phase14-topology.spec.ts` **1 passed（4.6s）**，实际空插件卷完成两类
  connection-test/install/configuration/stop/start/metrics，health 三条时序可见。
- 最终 `cleanup` 成功：本次 Compose 容器、网络、数据卷及临时 follower/auth 设施均移除，
  所有开工前资源保留；私有 env/override/admin/account JSON 已按 harness 规则移除。

该次完整运行一次性通过七个阶段：真实 ES monitor-only 权限及错密码恢复；五插件新卷与正式
Marshaller 消费建立 offset；Kafka 真实 follower 异常、Backend 新样本与同进程恢复；
ES yellow primary 聚合；共享目标完全不可达及局部业务降级/同进程恢复；可信失败更新回滚、
成功高版本更新、单 ID 配置/进程故障隔离及同卷 desired-state 恢复；前三插件、Logs/Events、
Phase 13 帖子/搜索及空卷管理员浏览器回归。普通用户对两类管理操作均被拒绝。

### 真实数值证据摘录

- 实际启用认证的 ES：health/stats HTTP 200；monitor-only 账号建索引/搜索 HTTP 403；错误密码时 Exporter HTTP 503 与唯一 up=0，密码恢复后 HTTP 200、容器重启次数 0。
- Kafka 同步副本：brokers=2，under-replicated=0。
- 仅停 follower：up=1、under-replicated=1，完整 7 samples；原 leader/coordinator 与 offsets 分区仍在原 broker。
- Backend 部分异常新样本：`2026-09-10T16:34:15Z`，value=1；恢复后另验证新采样归零。
- Elasticsearch yellow：documents=164、store_size_bytes=257818；分别落在真实 primary 两次采样窗口 [164, 164] / [257818, 257818] 内。
- 暂停正式消费后：采集前 offset 行 `[[0, 705, 707, 2]]`，采集后 `[[0, 705, 724, 19]]`（partition / committed / log end / lag）；committed 未被采集改变，实际 lag=5 落在窗口界限内。

### 交付与限制

- 当前官方包 `1.11.3` 的 SHA-256 与字节级可复现证据见上文。Kafka 7 gauge families / 7 samples；
  ES 12 gauge families / 14 samples。README 已逐项列出 API 字段、单位、聚合与缺失语义。
- 生产 Monitor 编译期登记并嵌入五类包，数据链路与 public catalog 均已启用两类 source。
  `1.11.4` 成功包和 `1.11.90` 失败包仅用于 acceptance 镜像的受信升级；它们不是完成产品版本。
- 根 VERSION、Frontend package/lockfile、`.env.example` 当前版本统一为 `1.11.3`，示例升级版本
  改为严格更高的 `1.11.4`；总/分方案同步批次完成，Phase 14 整体仍未完成。
- 维护环境仅为 Linux amd64 / Bash / Compose。本批不宣称 macOS、原生 Windows、多架构或 Kubernetes 验收。
- 原始 Dockerfile syntax frontend 拉取仍受本机 daemon 代理拒绝连接限制；验收使用前述临时
  builtin-frontend 构建方式及宿主 Node 24.20.0 静态产物，不声称该环境问题或完整 CI 已修复。
  没有改全局代理/daemon 配置，没有运行超出本批固定范围的全栈 CI/Phase 17 验收。
- 没有新增 Kafka SASL/TLS 或多 broker 产品支持；正常 ES Compose 仍关闭认证，实际认证
  允许/拒绝与恢复证据来自同版本、同生产 Exporter 包的独立私网 fixture，不改业务 ES 安全设置。
- Phase-14-04 可依赖五插件独立 per-ID 生命周期、固定精确 metrics 目录和安全配置事务。
  必须保留 Redis 历史查询 labels；不得把实际共享 Kafka/ES 停机误解为可观测/搜索仍须一直成功。
  无本批阻断问题；未扩大为一般依赖审计、覆盖率或额外架构改造。

### 最终治理检查

完成版本更新后实际通过：`python3 scripts/ci/validate_versions.py`、
`python3 scripts/ci/validate_branch.py --branch develop/1.11.3 --base-ref upstream/main`、
`git diff --check` 与 `git diff --cached --check`。只暂存本批代码、计划、文档、日志及版本文件；
`.run` 临时驱动/凭据/运行证据不提交。提交后另执行 `git diff --check upstream/main...HEAD`
检查已提交范围，结果以本次终端输出为准。没有推送或创建 PR。
