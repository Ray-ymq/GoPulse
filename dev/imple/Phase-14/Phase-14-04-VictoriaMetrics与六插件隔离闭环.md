# Phase-14-04：VictoriaMetrics 与六插件隔离闭环实施方案

> 当前状态：待实施。本文档定义 Phase 14 第四个执行批次的范围与验收合同；目标版本 `1.11.4`、开发分支 `develop/1.11.4` 和执行顺序以 `Phase-14-总实施方案.md` 为准。

## 1. 批次目标

交付第六种官方插件，并首次证明六类单实例插件在同一 Monitor/Compose 中同时运行和独立失败：

```text
VictoriaMetrics 固定运行指标 → victoriametrics-exporter
  → Monitor → Router → Kafka → Marshaller → VictoriaMetrics → Backend

Redis + MySQL + RabbitMQ + Kafka + Elasticsearch + VictoriaMetrics
  → 6 个进程 / 6 个 collector / 6 个状态，单类故障局部化
```

本批必须明确处理“VictoriaMetrics 宕机时无法把它自己的 `up=0` 存入它自己”的回路限制：宕机期使用 Monitor 安全状态与可传递的生命周期/故障事件作实时证据，恢复后用新 `up=1` 和完整数值证明自动恢复，不伪造停机期持久点。

## 2. 前置条件

- Phase-14-03 已合入 `upstream/main`，版本 `1.11.3`，前五类插件的真实链路和故障恢复均通过。
- fetch 后从最新 `upstream/main` 创建 `develop/1.11.4`。
- 核对 Compose 锁定 VictoriaMetrics 版本、自身 metrics 家族、Basic Auth、import/query 路径和宕机时 Backend/Marshaller 当前失败语义。
- 实施记录中必须写明最终选择的上游 families 及锁定版本依据，不直接透传 VictoriaMetrics 原始 `/metrics`。

## 3. 实施范围

### 3.1 VictoriaMetrics 官方插件

- 新增独立 `victoriametrics-exporter` 源码/模块，从受保护的锁定上游端点读取快照，只映射服务端白名单 family 和固定 label value。
- Schema 只接受受控 host/port/username/password/timeout，container mode 仅允许 `victoriametrics`；不允许完整 URL、自定义 path/query 或任意 label matcher。
- 固定 up、ingest/query 结果摘要、active series/rows 或锁定版本可稳定取得的存储量、merge/retention 和磁盘可用摘要；对上游 family 改名/缺失采取整体严格失败。
- 连接/认证/超时/契约失败固定 `503` 与唯一 `gopulse_victoriametrics_up 0`，不返回原始上游样本、账号或 URL。

### 3.2 制品、全链路与查询

- 生成可复现 Manifest v2 包、嵌入 Monitor 镜像并加入空卷调和；使用独立回环端口、process record、collector 和 per-ID lock。
- Monitor/Marshaller 双层注册固定 family/label/count，Router 允许固定 source，Backend 增加精确 source/target/producer catalog 和严格 matrix 验证。
- Frontend 启用 VictoriaMetrics 配置、connection-test、lifecycle 与 metrics 入口，状态文案区分 target 不可达和 metrics storage/query 不可用。
- 采集和存储不得形成无界自增 label/family；Exporter 不把 GoPulse 写入的 `gopulse_*` series 再全量抓取回链路。

### 3.3 六插件并行与隔离

- 在一个 Monitor 中同时运行六个子进程和 collector，验证 ID、回环端口、process record、status 和 events 没有交叉覆盖。
- 一类 install/update/configure/start/stop 操作只占用自身锁；对另一类的读状态和采集不阻塞。同 ID 并发变更仍固定拒绝或串行。
- 注入代表性 target unavailable、unexpected process exit、invalid update/rollback 和 Router publish failure，验证错误仅落在相关 ID，其他五类产生新指标。
- Monitor 重启按稳定 ID 顺序调和六类 desired state，一类损坏不阻塞其他；所有 running 类型均恢复唯一进程。

### 3.4 VictoriaMetrics 故障的可解释验收

- VictoriaMetrics 运行时，真实指标通过它自身存储后由 Backend 查询，同时确认不存在自我抓取递归。
- 停止 VictoriaMetrics 后，Exporter/Monitor 将该 target 标记为安全失败，Backend metrics API 局部 unavailable，社交业务和其他五个 Exporter 进程仍运行。
- 不要求在 VictoriaMetrics 完全停机期查询到存储于其中的新 `up=0` 点；若 Events 链路也因依赖失败暂不可查，使用 Monitor 安全 status 作实时证据。
- 恢复 VictoriaMetrics 后，同 Exporter/Monitor/Marshaller 进程无人工修复即重新查到 `up=1` 与完整值，历史数据保留。

## 4. 不在本批范围

- 自研组件 metrics、告警规则、VictoriaMetrics 替换/集群化、备份或容量规划。
- 宕机期本地持久指标队列、伪造 `up=0` 历史点或绕过 VictoriaMetrics 的第二查询存储。
- 同类多实例/多目标、第三方插件、管理端重构或 Kubernetes。

## 5. 建议实施顺序

1. 固定锁定 VictoriaMetrics 版本的上游白名单、Schema 和安全认证边界。
2. 实现 Exporter 与真实正常/认证失败/不可达/恢复，确认不采集已存储 `gopulse_*` 全量。
3. 接入包、Monitor、Router、Marshaller、Backend/Frontend 和 Compose，完成 Backend 真实查询。
4. 建立六插件同时运行、per-ID 并发操作、进程崩溃/更新回滚/重启恢复验收。
5. 完成 VictoriaMetrics 宕机时安全状态和恢复后持久查询证据。
6. 更新版本为 `1.11.4`，完成实施记录、固定门禁和提交。

## 6. 预计直接影响文件

- 新 `exporters/victoriametrics/*`、包构建脚本/通用 packager
- `monitor/internal/plugin/*`、`monitor/internal/metrics/*`、Monitor README/镜像
- `router/internal/envelope/*`
- `marshaller/internal/envelope/*`、`marshaller/internal/metrics/*`
- `backend/internal/exporterplugin/*`、`backend/internal/metricquery/*`、`backend/internal/http/*`
- Frontend exporter service/types/view/tests
- `deploy/compose.yaml`、相关 Dockerfile、`.env.example`
- `scripts/verify-plugin-metrics.sh`
- `VERSION`、Frontend 版本元数据、README/指标契约文档
- `dev/logs/Phase-14/Phase-14-04-VictoriaMetrics与六插件隔离闭环.md`

## 7. 批次验收标准

### 7.1 VictoriaMetrics 真实闭环

- connection-test、安全安装与锁定 family 映射通过；Backend 可查 `gopulse_victoriametrics_up` 和至少一个真实 storage/ingest/query 摘要值。
- 错凭据/不可达/上游 family 缺失不泄漏原始信息，不产生部分快照或递归系列。
- 宕机期和恢复后证据符合第 3.4 节，不声称未实际写入的 `up=0` 持久点。

### 7.2 六插件隔离

- 六种 ID 同时各有唯一 running 进程/目标/采集状态，每种 `up=1` 和一个运行值均由 Backend 查询。
- 代表性 target failure、process exit、update rollback 和 publish failure 仅更改选定 ID；其他五类仍产生新指标，社交业务可用。
- 并发操作不被一把全局锁串行，同 ID 并发仍受控；Monitor 重启按六个 desired state 恢复且无多进程。
- 六类状态/events/metrics 的 plugin/source/target 不串号，Secret/主机/路径/PID 不出现在公共产物。

### 7.3 完成条件

VictoriaMetrics 真实闭环、六插件并行/隔离/重启恢复、安全扫描和既有回归全部通过，实施记录完整，版本为 `1.11.4`，提交只包含本批文件时才完成。

## 8. 固定验证命令与回归范围

```bash
(cd exporters/victoriametrics && go test ./...)
(cd monitor && go test ./...)
(cd router && go test ./...)
(cd marshaller && go test ./...)
(cd backend && go test ./internal/exporterplugin ./internal/metricquery ./internal/eventquery ./internal/http)

(cd frontend && npm test)
(cd frontend && npm run typecheck)
(cd frontend && npm run build)

bash scripts/verify-plugin-metrics.sh --self-test
bash scripts/verify-plugin-metrics.sh --sources redis,mysql,rabbitmq,kafka,elasticsearch,victoriametrics --fault-isolation
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/1.11.4 --base-ref upstream/main
git diff --check upstream/main...HEAD
```

- 六 source 验收只跑一次最终 diff，覆盖六类实值、Backend 查询、一类各代表性目标/进程/更新故障、恢复与重启。
- 回归固定为 Logs/Events、Kafka/Marshaller 正式 offset、VictoriaMetrics 历史数据、Phase 13 代表性业务/搜索和管理授权。
- 只有观测到数据库或共享传输损坏才扩大验证，原因必须先记录。

## 9. 实施记录与下批交接

完成前创建：

`dev/logs/Phase-14/Phase-14-04-VictoriaMetrics与六插件隔离闭环.md`

记录锁定 VictoriaMetrics 版本/上游 family 映射、自观测回路证据、六进程/端口/状态、并发操作、故障注入、实际命令/结果、偏差和限制，并写清 Phase-14-05 可依赖的 metrics producer/schema 与固定 target 注册边界。
