# Phase-15-02：告警规则与 Metrics 评估闭环实施方案

> 当前状态：已完成，固定批次验收与直接回归通过（`1.12.2`），详见同名开发记录。本文档定义 Phase 15 第二个执行批次的范围与验收合同；目标版本 `1.12.2`、开发分支 `develop/1.12.2` 和执行顺序以 `Phase-15-总实施方案.md` 为准。

## 1. 批次目标

在 Phase-15-01 的 `super_admin` 与审计基础上，交付告警规则、持久评估状态、当前/历史告警 API 和第一个真实 Metrics 评估闭环：

```text
super_admin 选择 Backend metric catalog 的精确 series
  → 创建受限规则
  → Backend scheduler claim + VictoriaMetrics 时间窗口
  → normal / pending / firing
  → 连续异常更新同一 incident
  → 真实数值恢复后 recovered
```

本批建立后续 Logs/Events 共用的状态机，但不提前宣称三源完成。

## 2. 前置条件

- Phase-15-01 已合入最新 `upstream/main`，根与 Frontend 版本为 `1.12.1`，同名实施记录可证明 migration、bootstrap、`RequireSuperAdmin` 和审计基础。
- fetch 后从最新 `upstream/main` 创建 `develop/1.12.2`。
- 现有 Phase 14 Metrics 从 Monitor 到 VictoriaMetrics 再到 Backend catalog 的真实链路可用，不以直接写 VictoriaMetrics 作为本批主验收输入。
- 按总方案 §19 有界确认锁定 VictoriaMetrics API 对单 series、空点、counter reset 和标签 tuple 的真实响应。

## 3. 实施范围

### 3.1 告警持久模型

- 使用下一个可用 migration 创建 `alert_rules`、`alert_rule_states` 和 `alert_incidents`，字段、外键、索引、soft-delete 和快照语义严格遵守总方案 §9–§11。
- 每条 rule 只有一行 state 和最多一个 active incident；数据库约束与事务行锁共同防止重复 firing。
- selector 写入前必须解码为 Metrics 专用结构、按 catalog 验证后 canonical 序列化；不保存未知字段或客户端传入的 source/target/producer。
- migration down 只移除本批告警实体，不修改 Phase-15-01 角色、bootstrap 或审计数据。

### 3.2 Metrics 可告警目录和规则验证

- 从现有 `metricquery`/`componentmetrics` 权威目录派生 Metrics 告警 catalog，显式返回 metric、kind、unit、必需 label keys/allowed tuples 和允许 reducer。
- 无 label metric 只接受空 labels；有 label metric 必须选定全部精确 tuple。一条规则最多对应一个 series，不接受缺省聚合、通配符或正则。
- gauge reducer 只允许 `last|max|min|avg`，counter 只允许 `increase`；operator、threshold、window、for、severity、名称和 32 条规则上限按总方案验证。
- 本批 `GET /api/v1/alerts/catalog` 明确标注只有 Metrics source 可创建；Logs/Events 在 Phase-15-03 之前不得被 API 接受。

### 3.3 规则、当前告警和历史 API

- 实现总方案 §12 中 rules catalog/list/create/get/update/enable/disable/delete 和 current/history 路由，全部使用 `RequireSuperAdmin`。
- update/enable/disable/delete 使用当前 revision 做乐观并发条件；竞争只有一个成功，失败方返回稳定 409 并可重新读取。
- 对评估字段的 update、disable 或 delete 在同事务内把 active incident 关闭为 `rule_updated|rule_disabled|rule_deleted`，不写 recovered。
- 规则变更和 incident trigger/recover/close 使用 Phase-15-01 受限 builder 写管理审计；持续 true 不重复写 trigger 审计。
- current/history/rules list 使用有界 limit 与签名 keyset cursor，响应不返回上游 query string、内部 URL 或 SQL 字段。

### 3.4 有界 scheduler 与生命周期

- 实现 30s tick、最多 4 并发、单次上游 2s 超时、不堆叠轮次和 MySQL claim/lease；评估运行不得占用 HTTP handler 无界 goroutine。
- 将 scheduler 纳入 Backend root context，关闭时先停 claim，再有界取消/等待已开始查询。scheduler 启动失败、panic 或上游故障不退出 Backend HTTP server。
- 增加 `ALERT_EVALUATION_ENABLED` 布尔配置，默认开启。关闭时不 claim/不评估/不修改 incident，但规则/历史 API 可用，Backend readiness 不变。
- 进程重启不重置状态，过期 lease 可回收，已提交 active incident 不重复创建。

### 3.5 Metrics 窗口评估与状态机

- 评估 cutoff、freshness、reducer、counter reset 和 no-data 语义固定为总方案 §9.2/§10；评估器复用内部 VictoriaMetrics client/repository，不请求自身公共 API。
- `disabled|normal|pending|firing` rule state 与 `firing|recovered|closed` incident 状态的关联转移只在单个 MySQL 事务中完成。pending 的 unknown 回 normal；firing 的 unknown 只标记 stale，不恢复。
- 以真实 Phase 14 目录中的精确 metric（首选 `gopulse_redis_up` 等可受控恢复 gauge）完成主闭环。若有界前置确认证明该 family 在 target unavailable 时不提供可用点，必须在开工记录选择另一个已有固定 gauge，不改写 no-data=unknown 合同。

## 4. 不在本批范围

- Logs 或 Events 规则创建/count 评估；它们属于 Phase-15-03。
- 告警页面、独立管理 Frontend、大屏或用户角色页。
- 外部通知、告警确认/静默/升级、多条件组合、任意 PromQL 或用户自定义评估周期。
- 为了测试告警而直接修改 incident/state 表、手工写 VictoriaMetrics 或伪造公共 API 响应作为主验收。
- 修改 Phase 14 metrics family、label 或采集频率；只可在具体证据证明原合同无法支撑时先修订规划。

## 5. 建议实施顺序

1. 有界确认 VictoriaMetrics 响应和目录 label tuple，固定主验收 metric。
2. 完成告警表 migration、repository 与状态转移事务，以可控时钟验证最小状态机。
3. 建立 Metrics catalog/selector/reducer 与 strict JSON 规则服务。
4. 交付 rules/current/history API、revision 冲突和审计接线。
5. 实现 scheduler claim/lease、VictoriaMetrics adapter、关闭与重启恢复。
6. 使用真实 metric 运行 trigger/continue/recover、重复抑制、unknown 和关闭评估验收，再完成版本/记录。

## 6. 预计直接影响文件

- `backend/migrations/` 的 alert rules/state/incidents migration 及集成测试
- 新的 `backend/internal/alert/` 或按规则、评估、仓储分层的等价最小边界
- `backend/internal/metricquery/` 的可复用目录/内部查询边界
- `backend/internal/http/`、`backend/internal/apperror/`、`backend/cmd/server/main.go` 和 `backend/internal/config/`
- Phase-15-01 审计结构的规则/incident action builder
- `componentmetrics/routes.go`、`.env.example`、`deploy/compose.yaml` 和必要运行说明
- 新 `scripts/verify-alerts.sh` 与直接的强归属验收辅助文件
- `VERSION`、Frontend 版本元数据与同名实施记录

## 7. 批次验收标准

### 7.1 规则与 API

- `super_admin` 可从真实 catalog 创建、查询、更新、启停和删除 Metrics 规则；普通用户和未登录请求在读任何规则数据前分别 403/401。
- 未知 metric、错误/额外 label、错误 reducer/kind、非有限 threshold、非法 window/for、第 33 条规则和未知 JSON 字段均被稳定拒绝。
- 两个管理员使用同 revision 竞争更新，恰好一个成功，另一个 409；成功规则变更有且只有对应审计。

### 7.2 Metrics 触发、抑制与恢复

- 以真实 Phase 14 metric 建立 `for=0` 和一条代表性 `for>0` 规则，受控异常后出现正确 pending/firing。
- firing 后至少连续三轮真评估仍只有一条 active incident 和一次 trigger 审计，但 last-triggered/evaluation-count 有进展。
- 恢复真实目标/数值后，原 incident 转为 recovered 并包含恢复时间，当前告警列表不再包含它；历史仍可查。
- 无数据、过期点或 VictoriaMetrics 不可用时状态为 unknown/stale，不以 0 触发或恢复；恢复上游后自动继续。

### 7.3 持久恢复、关闭和完成条件

- 在 pending 和 firing 各替换一次 Backend 进程：过期 lease 被回收，状态按 MySQL 继续，不重复创建 incident。
- disable/update/delete firing 规则分别留下正确 closed reason 而不是 recovered。`ALERT_EVALUATION_ENABLED=false` 不改写当前状态，规则/历史和社交 API 仍可用。
- 所有 alert 公共 DTO、审计、日志和验收输出不包含 VictoriaMetrics URL/basic auth、PromQL 或原始错误。
- 根与受管版本元数据为 `1.12.2`，分支为 `develop/1.12.2`，同名实施记录与真实命令一致。

## 8. 固定验证命令与回归范围

实施中优先运行新 alert package、metric adapter 与 HTTP 的直接测试。本批最终 diff 固定运行：

```bash
(cd backend && go test ./internal/alert/... ./internal/metricquery ./internal/http ./internal/apperror ./migrations)

bash scripts/verify-alerts.sh --self-test
bash scripts/verify-alerts.sh --sources metrics

python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/1.12.2 --base-ref upstream/main
git diff --check
git diff --cached --check
```

- 如实际将 alert 分为多个同级 package，Go 命令应包含全部直接 package 并在实施记录列出；不因路径变化省略它们。
- `verify-alerts.sh --sources metrics` 必须启动强归属的真实 MySQL/VictoriaMetrics/Phase 14 指标链路，覆盖规则 API、trigger/continue/recover、unknown、restart、disable 和清理。
- 如本批修改现有 metricquery 公共响应，补跑现有 Metrics 直接契约测试；不默认扩展到六插件 Compose 全回归。
- 提交后补充运行 `git diff --check upstream/main...HEAD`。

## 9. 实施记录与下一批交接

完成前创建 `dev/logs/Phase-15/Phase-15-02-告警规则与Metrics评估闭环.md`。

记录必须包含实际 schema/index、catalog 数量与 label tuple 来源、固定 metric/reducer、scheduler 参数、真实异常手段、每次状态转移和 incident/audit 行数、重启/unknown/关闭结果、全部命令与偏差。交给 Phase-15-03 的固定输入至少包括：

- 可复用的 rule validation、scheduler/lease、状态机、incident 和审计边界。
- Metrics 的 no-data/unknown/recovered 语义与已证明真实触发。
- Logs/Events 尚未可创建的 catalog 状态，不得在交接记录中写成三源完成。
