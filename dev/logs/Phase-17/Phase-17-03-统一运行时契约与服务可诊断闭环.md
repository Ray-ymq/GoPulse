# Phase-17-03：统一运行时契约与服务可诊断闭环开发记录

## 范围、版本和状态

- 批次版本 `1.14.3`，分支 `develop/1.14.3`；从已拉取的 `upstream/main` (`5599369`) 开工。
- 最终候选提交 `307258b`（Go 运行时代码与已验证的 `d26e2eb` 相同）（前序基础提交 `489b17a`，完整合同提交 `688cdb6`）。
- 环境：真实 Linux amd64 Docker server；仅本批独立 Compose projects、随机端口、私有环境文件。未触碰既有 `gopulse-p13-local-*` 等资源，未做全局 prune。
- `Management_Center/` 为用户预先存在的参考目录，未修改或提交；本地 Git exclude 与 Docker build context 排除它。
- **实施与固定门禁已完成，无本批验收阻断项。最终代码、版本元数据与本记录由本批提交交付；不声明已合入 main 或外部镜像发布。**

## 已实施内容与基线差异

### 闭合的十二进程合同

新增 runtime contract/schema v1；合同产品版本为 1.14.3。静态校验实际 typed loader、Compose-owned keys、示例环境、敏感性、私有端口、Probe、检查/关停预算和官方 plugin catalog。配置仍从既有环境变量读取，没有第二套配置中心、必要 key 重命名或兼容别名。

| 进程 | 首要 Probe 端口 | 硬依赖 | 软依赖 | 退出预算 |
| --- | --- | --- | --- | --- |
| Backend | 8080 | MySQL / 当前 Schema / bootstrap | Redis / RabbitMQ / ES / VM / Monitor | 5s |
| Business Worker | 19102 | MySQL / 有效 Rabbit consumer session | 无 | 10s |
| Search Indexer | 19103 | MySQL / 有效 Rabbit consumer session / ES | 无 | 10s |
| Router | 9091 | Kafka/topic/producer | 无 | 10s |
| Marshaller | 9093 | Kafka / VM / ES logs 与 events stores | 无 | 10s |
| Monitor | 9090 | 插件本地状态与 catalog | Router 发布 / Exporter targets | 10s |
| Redis Exporter | 9121 | 无 | Redis | 5s |
| MySQL Exporter | 9122 | 无 | MySQL | 5s |
| RabbitMQ Exporter | 9123 | 无 | RabbitMQ | 5s |
| Kafka Exporter | 9124 | 无 | Kafka | 5s |
| Elasticsearch Exporter | 9125 | 无 | Elasticsearch | 5s |
| VictoriaMetrics Exporter | 9126 | 无 | VictoriaMetrics | 5s |

其余已有 metrics listener、环境默认/必填/敏感字段及 Compose grace 详见机器合同；不新增宿主端口。Exporter 的 managed grace 继承 Monitor 的同一 deadline。Router readiness 最大 5s、Marshaller 最大 2s，其余 1s；共享 250ms 缓存和单 checker slot。

### 运行时、错误与诊断

- 共享 `/startup`、`/live`、`/ready`、`/health`（live alias），GET-only、拒绝 query/body、稳定 JSON/no-store；panic/卡死 readiness checker 有界且不会不断新建 goroutine。
- Backend readiness 不再把可观测或缓存故障当社交入口硬故障，当前 schema 检查不新增 migration。Worker/Indexer 使用实际 consumer session 原子状态。
- 第一信号撤 readiness，HTTP、consumer、metrics、shipper 和插件消费同一 shutdown deadline；Monitor 并发停止六 slot。超时非零，第二信号恢复 OS 默认终止。Worker 预留取消/requeue 收尾时间，不延长总预算。
- Edge 替换外部 Request ID；内部客户端传递合法 32 位小写 hex ID；HTTP headers、日志和安全 error envelope 相关联。双前端未知错误仍显示通用文案。
- JSON logger 统一 schema、UTC 时间、服务/模块、有限 event、版本/revision/runtime metadata；依赖重复失败限速、恢复清理抑制。当前官方插件日志送到 Monitor stdout，历史包保留原行为。
- Monitor 与 Marshaller 日志 vocabulary 同步；严格 ES daily index 对运行时 keyword 字段做有限 additive mapping 后按原 document ID 重试，不能靠丢弃不兼容日志完成验收。
- 既有 edge `/health`、`/ready` 兼容入口保留，新增 `/startup`、`/live` 和 `/internal/` 被阻断。开发状态页接受 v1 Probe，不把缺失的依赖细节臆造为 up。
- Release manifest/Bundle >=1.14.3 收录 contract/schema/checksum；lifecycle 在加载 manifest 时校验合同 identity 与 payload；旧版本兼容。
- 运维说明见 `docs/runtime-contracts.md`。

## 已通过的直接验证

以下成功结果按最终相关代码变化增量复用，不因继续对话或上下文切换重跑。

| 固定命令或直接受影响检查 | 结果与捕获文件（`.run/phase17-03/`） |
| --- | --- |
| `python3 scripts/ci/verify_runtime_contracts.py --contract deploy/runtime-contracts.json --compose deploy/compose.yaml --env .env.example` | 通过；十二组件、配置、Probe、预算、catalog 无漂移 |
| componentmetrics `go test -count=1 ./... && go test -race -count=1 ./...` | 通过；`componentmetrics-signals-final.log`，含真实子进程第二次信号、在途 HTTP 正常/超时、checker 限并发与错误/日志 |
| backend `go test -count=1 ./cmd/server ./cmd/business-worker ./cmd/search-indexer ./internal/config ./internal/http/...` | 通过；`backend-shutdown-final.log`；日志查询新增 metadata 回归见 `backend-logquery-fixed.log` |
| backend Worker/Indexer `go test -race -count=1 ./internal/worker ./cmd/business-worker ./cmd/search-indexer` | 通过；`worker-final-fixed.log`，保护实际修改的 consumer 并发/超时/requeue 行为 |
| router `go test -count=1 ./...` | 通过；`router-completion.log` |
| marshaller `go test -count=1 ./...` | 通过；`marshaller-completion.log`，包含严格 daily mapping 的直接回归 |
| monitor `go test -count=1 ./... && go test -race -count=1 ./internal/plugin` | 通过；`monitor-health-fixed.log`，含新/历史 package health 合同和 plugin 退出 |
| 六 Exporter 各自 `go test -count=1 ./... && go test -race -count=1 ./...` | 通过；`*-exporter-gates.log`；Kafka 请求时限修复后为 `kafka-exporter-timeout.log` |
| `python3 -m unittest discover -s scripts/ci -p test_runtime_contracts.py` | 通过；合同遗漏/重复/Secret/alias/Probe/grace/版本/未登记 key 负向 |
| lifecycle `go test` release/control 与 release manifest Python 自测试 | 通过；`lifecycle-gates.log` ；release manifest Python 自测试沿用此前已执行的成功结果 |
| frontend `npm test -- --run && npm run build` | 18 files / 67 tests 通过，构建成功；`frontend-final.log` |
| admin-frontend `npm test -- --run && npm run build` | 11 files / 42 tests 通过，构建成功；`admin-frontend-final.log` |
| `python3 scripts/ci/validate_versions.py` | 通过，元数据为 1.14.3 |
| `python3 scripts/ci/validate_branch.py --branch develop/1.14.3 --base-ref upstream/main` | 通过 |
| `git diff --check` | 通过 |

## 真实执行失败与最小修复

1. 基础阶段 Backend metric budget 测试滞后于主线新增路由：原 677 改正为 687；本批增加 startup/live 后为 707。
2. 原始 `nc` HTTP helper 半关闭连接使 readiness 请求 context 提前取消，换为正常 HTTP sidecar；host CGO binary 不适用于 Alpine，helper 改为 `CGO_ENABLED=0`。
3. SignalContext 替换后一次 Monitor unused import 导致构建失败；最小修复后 RuntimeReady callback 合法使用 context。
4. Monitor 日志 validator 漏掉新字段造成 `permanent_rejection`；同步 Monitor 与 Marshaller 白名单，不吞掉记录。最终 scanner 显式拒绝该原因码。
5. edge 负向错误纳入 Phase 16 已存在 `/health`、`/ready`：断言修正为只禁止新增/内部入口，兼容路径单独验证。
6. Kafka broker pause 时 Exporter 请求超过 helper 8s：限制公共客户端 request overhead/retry timeout，目标源故障 matrix 后续通过。
7. Worker deadline 后 requeue 收尾与测试读取发生真实 race：从同一预算预留取消收尾时间，等待 handler 或原 deadline；最终 race 通过。一次检查命令误写两个不存在的包路径，随后按真实包路径更正，未把失败当通过。
8. Monitor 仍比较历史 health JSON 字面量，导致新插件启动后误报失败：按已校验 package version 选择 runtime v1/历史合同，保持进程 ownership 检查；模块与 race 通过。初版 Bundle 因该修复不作为最终候选，另建最终 Bundle。

失败项目均只清理自身 label 标识的 containers/networks/volumes；保留本地日志，不提交 Secret 环境文件。未读取第三方依赖源码，Kafka timeout 仅修改现有公开 client options。

9. 首轮完整 Compose 的日志页读不到记录：Backend 存储读取层使用严格 JSON 解码，未登记新增 runtime metadata。显式登记这些私有存储字段，保留原 API 分页投影和未知字段拒绝；直接成功/非法字段测试与 Backend 固定包通过。
10. 完整 Compose 信号检查暴露日志链路未恢复时 Worker 非零退出。独立受控诊断确认这是有界日志排空超时，不能改回“超时仍成功”。诊断同时发现 Backend 另开日志预算并忽略超时，已改为贯穿 main 的同一预算，超时 exit 1；真实容器证明 5.413s 非零退出后可重启。
11. 受控诊断重建 Kafka 后遗漏既有 Topic initializer，故 Router/Marshaller 正确不 ready；该诊断失败不能当产品恢复证据。最终完整门禁同步等待既有 initializer 完成，并用全新 Request ID 关联到新入库日志，再验证正常 idle drain，避免旧记录掩盖未恢复的链路。失败时先捕获 Go 服务日志再清理。
12. 一次两路候选构建在 Alpine APK 网络下载停滞；只中断本批构建客户端后重试成功，未修改依赖版本或系统 Docker daemon。初版/中间候选均不作为最终证据。

13. 新日志恢复断言仍失败，最终定位到 Kafka 默认 `log.dirs=/tmp/kraft-combined-logs` 与已声明命名卷 `/var/lib/kafka/data` 不一致；强制替换后 Topic 被重新创建。先在本实施记录写明持久化风险和必要范围扩展，再显式对齐数据目录。未修改 commit/rebalance 或迁移已有用户数据；在既有替换场景新增 Topic ID 不变断言，最终完整 Compose 对该持久化修复作直接验证。

## 最终运行时证据

- `scripts/verify-runtime-contracts.sh --candidate 1.14.3` 首轮的六 Exporter package/race 和合同自测试通过；失败的真实容器阶段用 `python3 scripts/ci/runtime_acceptance.py --candidate 1.14.3` 修复后续跑。Kafka 直接代码改变后补跑其 package/race；不重复其他未受影响成功检查。
- 最终 runtime 阶段退出 0：`.run/phase17-03/runtime-drain-final.log`，project `gopulse-runtime-96b15249cc0c`，源 revision `d26e2eba64c5052827211b85b94d43ec6f476aab`。
- 41 项场景：十二组件四路径/方法/query/body/缓存语义，配置负向，edge/内部 Request ID 与角色边界，六数据源与 Monitor 故障恢复，真实管理六插件，断开源/日志链路退出，24 次正常 SIGTERM/SIGINT 和重启，日志 schema/Secret/userinfo/path 扫描。
- 545 条实际 JSON 日志、十二服务全部校验通过，无 `permanent_rejection`；没有靠丢弃不兼容日志使验收通过。正常信号全部 exit 0，耗时 0.119–0.430s；日志链路断开的 Backend exit 1 / 5.413s，Redis 源断开的 Exporter exit 0 / 0.262s。详细分组件时间和 log SHA256 见最终 JSON evidence。
- checker 并发/卡死、在途 HTTP 排空与超时、在途 consumer 取消/requeue 和第二次真实 OS signal 使用最低有效 Go 测试层；十二组件 idle、依赖故障及 managed plugin 生命周期使用真实容器层。未复制每个组合到全部层。

## 最终 Compose 与 Bundle 证据

- 最终 `scripts/verify-compose.sh` 使用 `GOPULSE_RELEASE_MANIFEST=.run/phase17-03/release-persistent/release-manifest.json`，**退出 0**；独立 project `gopulse-accept-adfbef8a67bd`，完整输出 `.run/phase17-03/compose-persistent-final.log`。
- 通过拓扑、OCI/non-root/权限、唯一 edge 与鉴权、初始化幂等、业务读写、Redis 降级、Worker/Indexer 恢复、管理端真实操作、VM/Monitor/Router 故障隔离、Kafka 替换保留 Topic ID、新 Request ID 日志入库、所有自建服务正常信号退出与重启、整栈 down/up 持久化、独立 Exporter、双前端响应式状态、三来源规则创建及六插件真实采集/单源隔离。
- 此前三次完整门禁分别暴露日志查询字段、未恢复链路的退出、Kafka 数据卷错配；只在所需修复后重跑失败门禁，未追加历史升级或跨架构矩阵。
- Runtime evidence 的 Go 源 revision 为 `d26e2eb`；其后 `307258b` 仅对齐 Kafka Compose 数据目录并添加对应持久化断言/说明，Go 运行时代码未变。直接受影响的存储与恢复行为由最终完整 Compose 证明，未重复未受影响的 Probe/错误/日志单元门禁。
- 实际 Bundle 构建命令：`python3 scripts/ci/release_artifacts.py build --registry 127.0.0.1:44659/gopulse --output .run/phase17-03/release-persistent --platform linux/amd64`；源码来自干净提交的 `git archive`。构建完成后调用 `release_artifacts.verify_bundle` 成功核对资产/checksums/归档。
- Bundle 与镜像都是本地验收候选，未 external promote。验收完成后删除本批临时 loopback registry 及其匿名卷，保留本地镜像、Bundle 和校验证据；归档中的本地 registry 引用不代表一个仍在线的公开发行源。
- 所有 acceptance project 的容器/网络/卷已清理，既有资源与 Git 快照检查通过；未修改 `Management_Center/`。

| 资产 | SHA256 |
| --- | --- |
| runtime contract | `sha256:0be585517349b44eed47d6a826ecb727e87371014d697c94664bb8fc581264a7` |
| runtime schema | `sha256:de0d89b1d2a9e872b6a1ad54f5fe606cb65d0f10fd01b940e94a3936f2070f45` |
| Bundle payload | `sha256:756c957c1b76c180e56d73bcd106cefb0adda755ded6bcb0ea1b575423a7ad2e` |
| release manifest | `sha256:b9a3d6b54bff0c3ce45f02d884e5d93fa82e3fd94be6a4d219ad9f1ae9a1df7e` |
| Bundle tar.gz | `sha256:4975c6e0ced975b453719d79e01e33de1c9adfdf0825512371bc2f72aafe0db6` |

最终候选 revision：`307258baadd60c1f49b470aa976d4be85d947b6d`。机器可读的安全摘要随提交保存在：

- `dev/logs/Phase-17/evidence/Phase-17-03-runtime.json`
- `dev/logs/Phase-17/evidence/Phase-17-03-bundle.json`
- `dev/logs/Phase-17/evidence/Phase-17-03-compose.json`

## 偏差、兼容性与后续边界

1. 唯一范围扩展是固定门禁实际暴露的 Kafka 数据目录错配修复；风险依据与修改前事实已先写入本记录的前序提交。只将已有命名卷契约落实到 Kafka 配置，不改变消息 commit/rebalance、不新增 Schema migration。
2. **旧安装升级注意：** 原 Kafka 默认目录可能使数据位于容器可写层，而不在命名卷。旧容器不能盲目重建；应先保全原数据并制定离线迁移方案。本批未在用户旧环境执行迁移，候选未声明历史自动升级兼容。这是后续升级工作的明确限制，不是本批新建 Linux amd64 Compose/runtime 验收的未完成项。
3. 既有 `/health`、`/ready` edge 入口保留，Probe body 统一为 v1；新探针/内部指标不增加浏览器或宿主入口。无 key 重命名/兼容别名。日志存储新增 metadata，但公共日志页投影保持既有格式。
4. 未执行或声称 Kubernetes、arm64、Windows/macOS、历史版本升级、backup/recovery 或性能矩阵。Phase-17-04 的 Schema 与消息状态机工作未提前实施。
5. 第二信号与在途 HTTP/consumer 在最低有效 Go 层验证；idle、依赖故障、进程重启和真实插件回收在容器层验证。成功门禁按直接影响复用，不因上下文继续而重跑。

## 实际修改文件

<details><summary>本批相对开工基线的路径清单</summary>

- `.dockerignore`
- `.env.example`
- `VERSION`
- `admin-frontend/package-lock.json`
- `admin-frontend/package.json`
- `admin-frontend/src/services/http.test.ts`
- `admin-frontend/src/services/http.ts`
- `backend/cmd/business-worker/main.go`
- `backend/cmd/search-indexer/main.go`
- `backend/cmd/search-indexer/main_test.go`
- `backend/cmd/server/main.go`
- `backend/internal/alert/count/count.go`
- `backend/internal/config/config.go`
- `backend/internal/config/search_indexer.go`
- `backend/internal/config/worker.go`
- `backend/internal/eventquery/eventquery.go`
- `backend/internal/exporterplugin/client.go`
- `backend/internal/http/middleware/request_logging.go`
- `backend/internal/http/middleware/request_logging_test.go`
- `backend/internal/http/response/response.go`
- `backend/internal/http/router.go`
- `backend/internal/http/router_test.go`
- `backend/internal/logquery/logquery.go`
- `backend/internal/logquery/logquery_test.go`
- `backend/internal/metricquery/alert_points.go`
- `backend/internal/metricquery/metricquery.go`
- `backend/internal/observability/logging/logging.go`
- `backend/internal/observability/logship/shipper.go`
- `backend/internal/platform/elasticsearch.go`
- `backend/internal/platform/runtime_schema.go`
- `backend/internal/search/elasticsearch.go`
- `backend/internal/worker/runtime.go`
- `backend/internal/worker/runtime_test.go`
- `componentmetrics/config.go`
- `componentmetrics/endpoint.go`
- `componentmetrics/http.go`
- `componentmetrics/logging.go`
- `componentmetrics/probe.go`
- `componentmetrics/probe_test.go`
- `componentmetrics/registry_test.go`
- `componentmetrics/routes.go`
- `componentmetrics/runtime.go`
- `componentmetrics/runtime_test.go`
- `componentmetrics/serve.go`
- `componentmetrics/serve_test.go`
- `componentmetrics/signals.go`
- `componentmetrics/signals_test.go`
- `deploy/compose.yaml`
- `deploy/docker/acceptance.Dockerfile`
- `deploy/docker/frontend/nginx.conf`
- `deploy/docker/observability.Dockerfile`
- `deploy/release/release-manifest.schema.json`
- `deploy/runtime-contracts.json`
- `deploy/runtime-contracts.schema.json`
- `dev/logs/Phase-17/Phase-17-03-统一运行时契约与服务可诊断闭环.md`
- `dev/logs/Phase-17/evidence/Phase-17-03-bundle.json`
- `dev/logs/Phase-17/evidence/Phase-17-03-compose.json`
- `dev/logs/Phase-17/evidence/Phase-17-03-runtime.json`
- `docs/runtime-contracts.md`
- `exporters/elasticsearch/cmd/elasticsearch-exporter/main.go`
- `exporters/elasticsearch/go.mod`
- `exporters/elasticsearch/internal/runtime/runtime.go`
- `exporters/kafka/cmd/kafka-exporter/main.go`
- `exporters/kafka/go.mod`
- `exporters/kafka/internal/runtime/runtime.go`
- `exporters/mysql/cmd/mysql-exporter/main.go`
- `exporters/mysql/go.mod`
- `exporters/mysql/internal/runtime/runtime.go`
- `exporters/rabbitmq/cmd/rabbitmq-exporter/main.go`
- `exporters/rabbitmq/go.mod`
- `exporters/rabbitmq/internal/runtime/runtime.go`
- `exporters/redis/cmd/redis-exporter/main.go`
- `exporters/redis/go.mod`
- `exporters/redis/internal/config/config.go`
- `exporters/redis/internal/config/config_test.go`
- `exporters/redis/internal/logging/logging.go`
- `exporters/victoriametrics/cmd/victoriametrics-exporter/main.go`
- `exporters/victoriametrics/go.mod`
- `exporters/victoriametrics/internal/runtime/runtime.go`
- `frontend/e2e/compose-observability.spec.ts`
- `frontend/package-lock.json`
- `frontend/package.json`
- `frontend/src/services/connectivity.test.ts`
- `frontend/src/services/connectivity.ts`
- `frontend/src/services/http.test.ts`
- `frontend/src/services/http.ts`
- `frontend/src/types/connectivity.ts`
- `frontend/src/views/DevStatusView.vue`
- `lifecycle/internal/control/control.go`
- `lifecycle/internal/release/manifest.go`
- `marshaller/cmd/marshaller/main.go`
- `marshaller/internal/config/config.go`
- `marshaller/internal/elasticsearch/client.go`
- `marshaller/internal/elasticsearch/client_test.go`
- `marshaller/internal/httpserver/server.go`
- `marshaller/internal/httpserver/server_test.go`
- `marshaller/internal/logging/logging.go`
- `marshaller/internal/logs/validation.go`
- `marshaller/internal/victoriametrics/client.go`
- `monitor/cmd/monitor/main.go`
- `monitor/internal/config/config.go`
- `monitor/internal/httpserver/server.go`
- `monitor/internal/logs/logs.go`
- `monitor/internal/metrics/collector/collector.go`
- `monitor/internal/metrics/collector/components.go`
- `monitor/internal/metrics/publisher/publisher.go`
- `monitor/internal/plugin/manager.go`
- `monitor/internal/plugin/process.go`
- `monitor/internal/plugin/process_test.go`
- `monitor/internal/plugin/runtime.go`
- `router/cmd/router/main.go`
- `router/internal/config/config.go`
- `router/internal/httpserver/server.go`
- `router/internal/httpserver/server_test.go`
- `scripts/ci/release_artifacts.py`
- `scripts/ci/release_manifest.py`
- `scripts/ci/runtime_acceptance.py`
- `scripts/ci/test_runtime_contracts.py`
- `scripts/ci/testdata/runtime-http.go`
- `scripts/ci/verify_runtime_contracts.py`
- `scripts/verify-compose-observability.sh`
- `scripts/verify-runtime-contracts.sh`

</details>

## PR 门禁失败修复（2026-09-16）

首次 push 的 Actions run `35006301154` 有三个失败 job，自动建 PR 因前置门禁失败而跳过，并非已创建 PR 的合并冲突：

- Branch governance：`test_verify_business.py` 的 migrate 最小环境精确集合未包含本批新增的非敏感 `GOPULSE_VERSION`、`GOPULSE_REVISION`。仅补齐这两个允许字段，保留敏感配置禁止断言。
- Integration：Worker 阻塞处理器耗尽 drain 预算后按本批契约返回 `shutdown_timeout`，旧集成测试仍要求 nil。更新该失败路径预期，继续验证处理器已退出、通知未落库以及新 Worker 重投后通知正常落库；正常退出路径仍要求 nil。
- Full-stack Compose：构建 frontend 时 `SearchView` 测试在并行 Go 镜像构建负载下触发默认 5 秒超时（同次独立 Frontend job 全通过）。Dockerfile 将 Vitest worker 限制为 1、单测预算设为 15 秒；不跳过测试或削弱业务断言。

本地验证实际完成：

- `python3 -m unittest discover -s scripts/ci -p 'test_*.py'`：51 tests 通过（修改前已复现治理失败）。
- `python3 scripts/ci/validate_versions.py`、`python3 scripts/ci/validate_branch.py --branch develop/1.14.3 --base-ref origin/main`：通过。
- `cd frontend && npm test -- --run --maxWorkers=1 --testTimeout=15000`：18 files / 67 tests 通过。
- 独立创建带唯一名称与归属标签、仅 loopback 动态端口发布的 MySQL/RabbitMQ；执行 `go run ./cmd/migrate up` 与 `go test -count=1 -tags=integration ./internal/worker`：通过（7.492s）；随后删除本次容器及匿名卷，未使用既有用户环境。
- `gofmt`、`git diff --check`：通过。

版本保持 `1.14.3`，同分支 follow-up。远端完整门禁将在本次 push 后重新执行；本段不将尚未运行的远端检查记为通过。本地诊断与验证输出保存在 `.run/phase17-03/pr-fix/`（不提交原始 CI 日志）。
