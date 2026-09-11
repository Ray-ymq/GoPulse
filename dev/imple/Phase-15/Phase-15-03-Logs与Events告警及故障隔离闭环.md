# Phase-15-03：Logs 与 Events 告警及故障隔离闭环实施方案

> 当前状态：待实施。本文档定义 Phase 15 第三个执行批次的范围与验收合同；目标版本 `1.12.3`、开发分支 `develop/1.12.3` 和执行顺序以 `Phase-15-总实施方案.md` 为准。

## 1. 批次目标

在不放开任意 Elasticsearch 查询、不改变已验收状态机的前提下，将 Phase-15-02 的 Metrics 告警内核扩展到 Logs 与 Events，形成三源真实触发、重复抑制、恢复、重启与故障隔离闭环：

```text
fixed Log vocabulary   → bounded count [cutoff-window, cutoff] ─┐
fixed Event vocabulary → bounded count [cutoff-window, cutoff] ─┼→ one state machine / incidents
existing Metrics adapter                                  ─────┘
VictoriaMetrics or Elasticsearch unavailable → source-local unknown
```

本批完成 Backend 层的全部告警能力；独立管理 Frontend 和告警页仍属于后续批次。

## 2. 前置条件

- Phase-15-02 已合入最新 `upstream/main`，根与 Frontend 版本为 `1.12.2`，Metrics 规则、scheduler/lease、状态机、current/history 与审计已通过真实闭环。
- fetch 后从最新 `upstream/main` 创建 `develop/1.12.3`。
- Phase 14 Logs/Events 已经 Router/Kafka/Marshaller 写入 Elasticsearch 固定 read alias，并有 Backend 严格查询 vocabulary。
- 按总方案 §19 有界确认锁定 Elasticsearch 版本对 `_count` 或等价受限 count、UTC 边界、空 alias/index 与不可用的真实响应。

## 3. 实施范围

### 3.1 可复用受限 vocabulary

- 将现有 `logquery` 与 `eventquery` 中的 source/module/message/level/error 及 event/source/severity/plugin/operation/error 合法组合抽取为服务端权威目录，公共查询和告警验证共用同一来源。
- Logs 规则 selector 只允许 `service,module,level,message,error_code`，Events 只允许 `source,event_name,severity,plugin_id,operation,error_code`，每条至少选择一项。
- 拒绝请求 ID、业务 ID、自由文本、通配/正则、index/alias、sort/PIT、query DSL 和上游 URL。评估窗口只由规则 `window` 与服务端时钟生成。
- 扩展 `/api/v1/alerts/catalog` 以严格描述两类 selector 词表，在目录、规则服务和评估 repository 三层均重新验证。

### 3.2 Logs/Events 计数仓储

- 为两个现有 read alias 增加只返回非负整数 count 的内部 repository 边界，请求 body 只由服务端固定 builder 产生。
- 窗口固定为 `[now-15s-window, now-15s]` UTC，边界与现有 timestamp mapping 一致。请求超时上限 2s，响应 body/count 类型/范围被严格验证。
- alias 存在且查询成功时，0 是有效评估值；alias/index 尚未建立但系统明确为无数据新装状态时也可解释为 0。认证失败、超时、不可达、非预期 mapping/响应均为 unknown，不是 0。
- 不为评估保存或返回匹配文档，不读 log message 中的原始业务字段；incident 只保存受限 selector 和 count。

### 3.3 规则服务与状态机接线

- Logs/Events 的 reducer 固定 `count`，共用 Phase-15-02 的 operator/threshold/window/for、revision、CRUD、scheduler、state/incident 和审计事务。
- 成功 count=0 按规则正常比较，可使触发条件为假而恢复；unknown 保持 firing 并标记 stale，且中断 pending 的连续性。
- 一条文档在窗口内的多轮评估只更新同一 incident，不按每轮或每匹配文档创建历史行。文档滑出窗口且 count 不再越界时恢复。
- Events 规则只评估现有 Monitor 目录，不将 MySQL 审计或告警转移投影回同一规则输入，避免自触发递归。

### 3.4 三源故障隔离

- 将 Metrics、Logs、Events 三个 adapter 的错误映射为局部 safe code，单条规则失败只更新它的 `data_status/last_error_code`，不中止同轮其他规则。
- 分别停止 VictoriaMetrics 和 Elasticsearch，验证一类规则 unknown 时其他类型继续评估；已 firing 规则不伪恢复，历史和角色 API 继续从 MySQL 读取。
- 设置 `ALERT_EVALUATION_ENABLED=false` 并替换 Backend，证明不生成新评估或转移记录、社交业务与管理查询可用；再开启后从持久状态继续。
- 对代表性 adapter panic/非法响应使用单次评估恢复边界，不让 Backend 进程退出；不为每个错误做全组合。

### 3.5 真实三源闭环

- Metrics 复用 Phase-15-02 已验收的真实规则，在本批故障矩阵中确认其未回归，不重做无关的六插件全排列。
- Logs 使用真实 Backend 请求或业务流产生一条命中固定 vocabulary 的日志，经 LogMonitor/Router/Kafka/Marshaller/Elasticsearch 后由 count adapter 触发，待窗口滑出后恢复。
- Events 使用真实插件采集或生命周期操作产生一条 Monitor event，经既有链路后触发并在窗口滑出后恢复。
- 三类规则各至少连续三轮 true 而不产生重复 incident/audit，且在 firing 中替换 Backend 后仍保持单例。

## 4. 不在本批范围

- 独立管理 Frontend、告警表单/列表页、大屏或前端图表。
- 自由文本检索、正则、Elasticsearch DSL、PIT 分页匹配文档、任意 source/index 或跨源布尔表达式。
- 将告警/审计事件重新投入 Events 作为自己的评估输入。
- 外部通知、告警 ack/silence/escalation、自动修复或历史删除/归档工具。
- 借故障隔离扩展社交业务或 Phase 14 基础设施的新容错功能。

## 5. 建议实施顺序

1. 有界确认 logs/events alias 的 count、空数据与 unavailable 响应，固定两条真实验收 selector。
2. 抽取公共 vocabulary/catalog，确保旧 Logs/Events 查询的合法/非法组合不变。
3. 实现有界 count repositories 及严格响应/错误映射。
4. 扩展 catalog/rule validation 并接入已有 scheduler/state machine。
5. 运行两条真实数据触发/滑出恢复与三源重启/重复抑制。
6. 串行运行 VictoriaMetrics、Elasticsearch 和 evaluator-disabled 代表性故障，每个边界恢复后再进入下一个，最后更新版本/记录。

## 6. 预计直接影响文件

- `backend/internal/logquery/`、`backend/internal/eventquery/` 的可复用 vocabulary 与 count repository 边界
- Phase-15-02 创建的 alert catalog/rule/evaluator/adapter 直接文件
- `backend/internal/http/`、`backend/internal/apperror/` 及直接 contract tests
- `scripts/verify-alerts.sh` 及三源/故障隔离验收辅助文件
- 只在真实验收需要时调整 `scripts/verify-logs.sh`、`scripts/verify-events.sh` 或 Compose acceptance wiring
- `VERSION`、Frontend 版本元数据与同名实施记录

## 7. 批次验收标准

### 7.1 目录与评估语义

- alert catalog 中 Metrics/Logs/Events 均可用，所有 selector 值都来自服务端目录；任意文本、ID、wildcard、index 或非法组合被拒绝。
- 真实空窗口的 Logs/Events count 为 0，Elasticsearch 不可用、认证错或响应违约时为 unknown，两者不被混淆。
- Logs/Events 和 Metrics 共用相同状态机与 revision/incident 语义，没有 source-specific 路径绕过审计、重复抑制或 `RequireSuperAdmin`。

### 7.2 真实三源与重启

- Metrics、Logs、Events 各一条真实规则从数据产生、存储/查询、评估到 firing，并在真实数值恢复或时间窗口滑出后 recovered。
- 每条规则连续三次 true 只有一个 incident/一次 trigger 审计；在 firing 时替换 Backend 仍没有第二个 active incident。
- 告警历史保留三源的受限 object 快照、first/last trigger、recovered time 和最后 value/count，不包含原始 log/event 文档。

### 7.3 隔离、脱敏与完成条件

- 停 VictoriaMetrics 时只有 Metrics 规则 unknown，停 Elasticsearch 时只有 Logs/Events 规则 unknown；每次故障期间已 firing 不伪恢复，角色/规则/历史和代表性社交请求继续。
- 关闭 evaluator 后规则和历史可查、状态不变、Backend ready 与社交业务不变；再开启可继续。
- API、incident、audit、日志和验收输出不包含 Elasticsearch/VictoriaMetrics URL、basic auth、index/alias、query DSL 或原始错误。
- 根与受管版本元数据为 `1.12.3`，分支为 `develop/1.12.3`，同名实施记录与真实命令一致。

## 8. 固定验证命令与回归范围

实施中先运行 log/event vocabulary、count repository 与 alert adapters 的直接测试。本批最终 diff 固定运行：

```bash
(cd backend && go test ./internal/alert/... ./internal/logquery ./internal/eventquery ./internal/http)

bash scripts/verify-alerts.sh --self-test
bash scripts/verify-alerts.sh --sources metrics,logs,events --fault-isolation

python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/1.12.3 --base-ref upstream/main
git diff --check
git diff --cached --check
```

- 固定告警入口必须使用真实 VictoriaMetrics/Elasticsearch 和现有传输链路，并串行恢复每个故障资源；`--self-test` 不访问 Docker。
- 若 vocabulary 抽取修改现有查询逻辑，必须同时运行对应 Logs/Events 直接契约测试；不因此重跑与本批无关的全部 Compose 门禁。
- 提交后补充运行 `git diff --check upstream/main...HEAD`。

## 9. 实施记录与下一批交接

完成前创建 `dev/logs/Phase-15/Phase-15-03-Logs与Events告警及故障隔离闭环.md`。

记录必须包含实际 Logs/Events vocabulary 数量、count 查询形状、空/0/unavailable 证据、三条真实规则和数据产生路径、状态/incident/audit 行数、Backend 替换、三类故障与恢复、全部命令与偏差。交给 Phase-15-04 的固定输入至少包括：

- 已封闭的三源 alert catalog 与规则/current/history API。
- 已证明的 source-local unknown、重复抑制、重启和 evaluator-disabled 语义。
- 管理 Frontend 只需消费 Backend DTO，不得重做数据源查询或状态机。
