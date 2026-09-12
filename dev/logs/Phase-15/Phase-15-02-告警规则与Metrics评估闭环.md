# Phase-15-02：告警规则与 Metrics 评估闭环开发记录

## 状态与基线

- 状态：已完成，固定运行验收、直接回归和不同管理员并发验收通过；完成版本 `1.12.2`。
- 2026-09-12 fetch `upstream`，从 `upstream/main` 的 `7b57151` 创建 `develop/1.12.2`。基线产品版本为 `1.12.1`。
- 原工作树未跟踪文件 `~` 保持原样，不读取、不暂存、不提交。

## 实现范围

- migration `000013_alerts`：`alert_rules` 软删除与 live-name 唯一索引；`alert_rule_states` 以 rule ID 主键持久化 pending/active/value/时间/lease；`alert_incidents` 存储规则快照和 firing/recovered/closed 历史。
- `uq_active_rule` generated-column 唯一约束保证每规则至多一条 firing incident；state 的 active incident 唯一外键；所有历史关联采用 RESTRICT，不级联删除。
- 规则创建在 bootstrap singleton 行锁下检查全局 32 条限制；更新使用 revision、规则/state 行锁、事务内关闭与成功审计；评估 claim token、revision 和 lease 三重检查后提交。
- `metricquery.AlertCatalog` 派生既有 Metrics 定义与 `componentmetrics.Catalog`。Redis database 使用 Monitor schema 既有 `0..15` 范围；其余单标签固定值由既有 metricquery validation 派生，组件 tuple 直接使用权威目录。
- `metricquery.Client.AlertPoints` 复用同一认证、HTTP client 和 2 MiB 响应边界，调用 VictoriaMetrics original-sample export；不能用 query_range 插值点冒充原始采样时间或新鲜度。限制 4096 个点，精确固定 selector，原始时间排序、重复点去重，冲突值拒绝。
- `alert` package 包含 typed selector/validation、strict JSON、规则/当前/历史 Handler、签名 keyset cursor、MySQL repository 和 scheduler。只有 Metrics 可创建，Logs/Events 不被当前 API 接受。
- scheduler 30s 周期、0..255ms jitter、4 workers、单上游请求 2s、claim lease 10s、每次评估 context 8s、单轮 context 25s；轮次不堆叠，root context 取消与最多 3s 等待；评估异常不退出 HTTP server。
- cutoff 为 now−15s，freshness 90s；gauge last/min/max/avg、counter reset-aware increase；unknown 打断 pending，firing unknown 只 stale；连续 true 原位更新 incident，false 才 recovered。
- `ALERT_EVALUATION_ENABLED` 默认 true；false 不 claim/不评估/不修改 incident，不改变 readiness 与管理查询。
- rules/current/history/catalog 全部接入现有 `RequireSuperAdmin`；审计扩展使用受限 builder、同事务写入、系统转移不记录原始 selector/query/URL。

## 直接影响文件与回归范围依据

- `backend/internal/alert/`、`backend/internal/metricquery/alert_*`、migration 13、HTTP 路由/响应、配置/server 生命周期、`user` 审计扩展。
- `.env.example` 与 `deploy/compose.yaml` 提供评估开关。
- `componentmetrics/routes.go` 添加 10 个固定告警路由 tuple；没有改变 family 名、label key 或采集频率。Backend 最大样本数从 Phase-15-01 的 577 增至 677。浏览器生成目录和已有组件 verifier budget 同步，避免新路由观测被当作未知 tuple。对这些共享公共合同改动，补跑 componentmetrics、Frontend observability 直接契约测试及 verifier self-test；不是全产品六插件回归。
- `scripts/verify-alerts.sh`、`scripts/ci/verify_alerts.py` 强归属真实链路验收；使用当前 checkout 编译的 Backend/migrate/admin-role 与受控测试二进制，复用未修改的 Phase 14 运行镜像，不伪装为已重新构建全产品镜像。

## 已观察到的失败与修复

1. 首次 Compose 启动 migration 失败；补充脱敏诊断后确认 MySQL 8.4 将 `last_value` 作为保留字。持久列改为 `observed_value`，公共 DTO 保留 `last_value`，不改变产品合同。
2. 验收工具对既有 exporter start 发送 `{}` 被原有严格接口 400 拒绝，改为无请求体；不放宽现有生产接口。
3. 受控 lease 重放测试发现已释放租约的 `lease_until > UTC_TIMESTAMP(6)` 返回 SQL NULL。使用 `COALESCE(...,FALSE)`，释放/过期租约均无权提交。

## 验证进度

- 初始直接 Go package 检查通过（alert/metricquery/http/apperror/migrations/config/server）。
- 新的 selector/reducer/strict JSON、原始点 adapter、HTTP 授权直接测试通过。
- componentmetrics 直接 package 测试通过，固定 Backend sample budget 为 677。
- 验收工具 self-test、真实 Compose 命令均已通过；最终证据与收口结果见下文。

## 偏差与后续交接

- 主验收选用 `gopulse_redis_connected_clients` / `last`，通过真实认证 Redis TCP 客户端增加/移除改变数值，而不是停止业务依赖或向 VictoriaMetrics 写入样本；这是同一 Phase 14 固定目录内的受控真实指标。
- Redis `CONFIG RESETSTAT` 只用于证明真实 counter reset 的原始 API 响应，不用于伪造主触发/恢复。
- 不交付外部通知、任意表达式、Logs/Events 评估、管理 Frontend 页面或六插件操作闭环。

## 最终直接验证（已执行）

- `(cd backend && go test ./internal/alert/... ./internal/metricquery ./internal/http ./internal/apperror ./migrations ./internal/user ./internal/config ./cmd/server)`：通过；无外部数据库的标准 Go 命令会显式 skip `TestOwnedMySQLStateAndLease`，该测试另在强归属 MySQL 中实际执行，不把 skip 算成数据库验收。
- `(cd componentmetrics && go test ./...)`：通过。
- `(cd frontend && npm test -- src/services/observability.test.ts)`：1 文件 / 7 测试通过。
- `bash scripts/verify-component-metrics.sh --self-test`：通过，生成目录与 Go 权威目录一致。
- `python3 -m py_compile scripts/ci/verify_alerts.py`、`bash -n scripts/verify-alerts.sh`：通过。
- 实际原始点探测不发布 VictoriaMetrics 宿主端口，改从专属 Backend 内通过已有认证执行；避免 Docker 对仅 expose 端口的 `compose port` 返回 `invalid IP:0` 被误当成可访问地址。该问题只影响验收探测入口，生产评估始终走内部 client。
- 强归属项目 `gopulse-p1401-7e3d8ba53e9f` 中重新编译并执行受控测试最后一项：过期 lease 被生产 scheduler 重新领取、提交并释放。实际命令为 `CGO_ENABLED=0 go test -c ... ./internal/alert`，确认 container project label 后 `docker cp` 专属测试二进制，在该 Backend 内以限定 `ALERT_TEST_PROJECT`/数据库执行 `-test.run TestOwnedMySQLStateAndLease -test.v`；通过，证据 `controlled-final.log`。这仅补齐持久 lease 断言，不修改主验收规则/incident。
- Backend 重建会重新分配 `HTTP_PORT=0` 的宿主端口；此前工具仍连接旧端口，尽管 SQL 已证明两条规则继续 firing。修复工具在重建后刷新客户端 origin，Cookie 保留；这是验收连接问题，不修改生产规则或状态。
- 在最终强归属项目 `gopulse-p1401-7083fad8cf04` 的两条真实 firing incident 上补充 current/history `limit=1` 签名 keyset 分页、顺序、不重复和 cursor 篡改 400 检查，实际通过，证据 `incident-pagination.json`；相同检查已收进 verifier 的 `incident_pages`，不以空列表冒充分页验证。
- 一轮真实恢复验证发现验收 shell 的 `trap` 在启动时提前展开 `jobs`，导致预期已移除的 20 条 BLPOP 客户端仍真实连接，规则正确维持 firing。确认专属 Redis label 后用 `CLIENT UNBLOCK` 移除这些真实故障客户端（不写 Metrics/incident/state），并修复 trap 延迟展开与显式解除专属阻塞客户端的清理路径。修复后已完整重跑通过，未把人工协助的一轮算成无干预脚本验收。


## 最终真实运行验收结果

2026-09-12，`bash scripts/verify-alerts.sh --sources metrics` 在项目 `gopulse-p1401-76126e8c7b46` **无人工故障干预完整通过**，实际时段 08:14:56–08:23:03 UTC（北京时间 16:14:56–16:23:03）。证据为 `.run/gopulse-p1401-76126e8c7b46/alert-evidence.json`；结束时专属容器/网络/卷/临时凭据均已清理，启动前存在的资源逐一确认仍在。

### 目录、原始点与 API

- 可告警目录实际 **95 个 metric**；唯一可创建 source 为 Metrics。模式标签真实选择 `mode=user`、database 标签 `db=0`；合法但未采集的 `db=15` 返回空 series，不混同数值 0。
- 真实 `gopulse_redis_commands_processed_total` 原始值依次为 `16,23,30,237,5`；Redis `CONFIG RESETSTAT` 造成真实 reset，按合同计算窗口 increase 为 `226`。未向 VictoriaMetrics 导入任何伪造点。
- 主闭环：`gopulse_redis_connected_clients`、空 labels、`last`、`gt 10`、window `5m`；正常值 `1`，20 个真实阻塞客户端接入后值 `21`，移除后恢复 `1`。
- 未登录/普通用户对全部 10 个新路由实际 401/403；未知 metric、额外/缺失 labels、错误 reducer、非法 window/for、超限 threshold、未知/重复 JSON 字段和无效 UTF-8 被拒绝；32 条未删除规则接受，第 33 条拒绝。
- rules 签名分页无重复，current/history 在两条真实 incident 上按各自稳定顺序翻页；篡改 cursor 返回 400。

### 真实状态与 incident / audit 证据

以下时间均为 UTC；数量针对相应规则的一次连续异常区间，不把后续第二次真实异常误认为重复。

| 验收动作 | 实际结果 | incident / audit |
| --- | --- | --- |
| for=0 触发 | rule 35 于 `08:16:37.230103` firing，值 21 | incident 3，新建 1 行 |
| for=1m 与 pending 进程替换 | rule 36 pending_since=`08:16:37.232802`，Backend 重建后于 `08:17:41.664799` firing | incident 4，新建 1 行；pending 状态来自 MySQL |
| firing 进程替换后继续至少三轮 | incident 3 evaluation_count 从 3 增至 6，last_triggered=`08:19:18.257983` | 仍仅 incident 3 一行；`alert.trigger` 审计仍 1 行 |
| VictoriaMetrics 停止 | firing 保持 firing/stale，另一个 pending 回 normal/unknown | 不新建恢复记录；该区间 `alert.recover` 仍 0 行 |
| 恢复上游并移除真实客户端 | incident 3 于 `08:21:23.126660` recovered，值 1，evaluation_count=9 | 原 incident 3 原位恢复；`alert.recover` 1 行；current 排除，history 保留 |
| 关闭评估并替换 Backend | 35 秒后所有 rule state、last_evaluated、active incident ID 与关闭前一致 | 无后台状态改写；规则/历史/社交 API 和 readiness 可用 |
| PUT firing 规则 | rule 39 / incident 6 为 closed，reason=`rule_updated` | `alert.close` 1 行，recovered_at=NULL |
| disable firing 规则 | rule 40 / incident 7 为 closed，reason=`rule_disabled` | `alert.close` 1 行，recovered_at=NULL |
| delete firing 规则 | rule 41 / incident 8 为 closed，reason=`rule_deleted` | `alert.close` 1 行，recovered_at=NULL，历史仍可查询 |
| 再 enable 且恢复评估 | rule 40 再次正常进入 firing | 新一段异常正常触发，不复用已 closed 的 incident |

受控 MySQL 测试同时实际证明 pending unknown 中断、连续 true 触发、firing stale、真实 false 的事务恢复、释放/过期 lease 不可提交、scheduler 可重领过期 lease，以及数据库拒绝第二条 active incident。这些是持久边界补充测试，不是主 Metrics 输入。

### 两个不同管理员的最终并发补验

完整指标运行成功后，仅收紧 API 用例的参与者身份：将普通测试账号通过真实角色 API 临时提升为另一个 super_admin，两个独立 Cookie/用户 ID 对同 revision 并发 PUT，再降回 user。没有修改生产实现，也没有重跑已成功的指标链路。

实际执行 `python3 /tmp/gopulse-alert-api-final.py`：该定向驱动导入仓库当前 `verify_alerts.AlertsAcceptance`，构建当前二进制，在独立项目只启动 Backend 及业务依赖、创建两个真实用户、声明 bootstrap 后执行**更新后的 `api_matrix()` 原函数**并清理。项目 `gopulse-p1401-cd8a028f100d` 通过，证据为同目录 `alert-evidence.json`，其中记录两个不同 `distinct_admin_ids`。结果恰好 `200/409`，失败 code=`alert_revision_conflict`，成功 update 审计恰好 1 行；原普通账号恢复为 user。相同双管理员用例已保留在正式 verifier 中。

### 最终命令与有效证据范围

- 固定 Go 命令及直接新增 user/config/server 包全部通过；后续仅补充 test 的过期 claim 断言时重新编译运行 alert package，其余包使用有效缓存结果，没有重新执行无关全量回归。
- `bash scripts/verify-alerts.sh --self-test`：最终脚本通过。
- `bash scripts/verify-alerts.sh --sources metrics`：完整无干预运行通过；最后双管理员身份这一 API 用例变化由上述同源定向执行补验通过。
- `python3 -W error -m py_compile scripts/ci/verify_alerts.py`、`bash -n scripts/verify-alerts.sh`：通过。
- `VERSION`、`.env.example` 的产品版本/镜像 tag、Frontend package/lock 根版本已同步 `1.12.2`；管理 Frontend 尚未在本批创建，不虚构其版本或页面验收。
- 版本、分支、Git 空白门禁在提交前执行；提交后再按计划运行 `git diff --check upstream/main...HEAD`。

## Phase-15-03 固定交接

已交付 MySQL rule/revision/admission、受限审计 builder、scheduler/lease 和 `Repository.apply` 事务状态机；Metrics 原始点、no-data/unknown/stale/recovered 语义已经真实链路证明。Logs/Events 尚未可创建；下一批应增加 source 专用 selector 和 adapter，但保留当前持久状态、签名分页、审计脱敏和显式关闭合同。本批不宣称 Phase 15 全阶段完成，不交付通知、管理 UI 或跨平台产品化。

提交前已执行 `python3 scripts/ci/validate_versions.py`（Version metadata matches root VERSION）、`python3 scripts/ci/validate_branch.py --branch develop/1.12.2 --base-ref upstream/main`（Branch governance passed）和 `git diff --check`，均通过。
