# Phase-15-05：管理大屏与告警权限操作闭环实施方案

> 当前状态：已完成（2026-09-12）；Backend 和两端固定检查、真实同源大屏/告警/角色/审计、三上游分区故障与脱敏验收通过，证据见同名开发记录。本文档定义 Phase 15 第五个执行批次的范围与验收合同；目标版本 `1.12.5`、开发分支 `develop/1.12.5` 和执行顺序以 `Phase-15-总实施方案.md` 为准。

## 1. 批次目标

在已独立的管理 Frontend 中交付 Phase 15 的完整管理产品面：超级管理员登录后默认看到由真实 Backend 聚合的可观测大屏，并能在同一应用内完成告警规则、当前/历史、按 ID 角色调整和管理审计闭环：

```text
/admin/ → Backend partial overview → 六组件 + key metrics + logs/events
                              └→ 六插件 + 当前/近期告警

/admin/alerts → catalog-driven rules + current/history
/admin/users  → exact user ID lookup + role change + bootstrap protection
/admin/audit  → signed-cursor management audit
```

本批不用客户端多请求拼凑“看起来正常”的大屏，也不使用静态 fixture 代替真实管理数据。

## 2. 前置条件

- Phase-15-04 已合入最新 `upstream/main`，根与两个 Frontend 版本为 `1.12.4`，同源 `/admin/` 应用、既有四类管理页和角色失效处理已通过。
- fetch 后从最新 `upstream/main` 创建 `develop/1.12.5`。
- Phase-15-03 的 rules/catalog/current/history 和 Phase-15-01 的 users/audit API 为稳定 Backend 契约，Frontend 不重新定义状态机或权限。
- 按总方案 §19 有界确认大屏六个 key metric 的精确目录 tuple、新鲜度和 Monitor/plugin 部分失败响应。

## 3. 实施范围

### 3.1 Backend 部分可用 overview

- 实现 `GET /api/v1/admin/overview?range=15m`，严格返回总方案 §14 的 `components,key_metrics,logs,events,plugins,alerts` 六个 section，每区都有 status/observed_at/reason_code。
- `components` 只使用六个固定 component ID、Monitor 最近采集成功时间和固定 dependency metrics，不转发任意 component name/label。
- `key_metrics` 固定为 backend outbox pending、business-worker in-flight、search-indexer retrying、monitor event queue、router buffered records 和 marshaller retrying；每项带最新值、单位、时间与新鲜度状态。
- `logs/events` 使用已有受限 count repository 返回 15m 三严重度计数；`plugins` 合并 Monitor 安全状态与六个固定 up metric；`alerts` 从 MySQL 聚合 firing/pending/unknown/近期恢复和 evaluator status。
- fan-out 并发、子请求超时和总 3s 预算按总方案实现。某个上游失败只返回该 section 的 degraded/unavailable，不丢弃其他可用数据。
- overview 不返回 PromQL/DSL、内部 URL、凭据、原始错误、PID、内部路径或未管理 labels。

### 3.2 默认管理大屏

- 将 `/admin/` 改为真实大屏而非临时跳转，用六区明确呈现正常、降级、未知、空和过期状态。
- 高层卡片只展示 Backend 已推导结论，详情链接进入现有 Metrics/Logs/Events/Plugins 或新 Alerts 页，不在浏览器重做规则评估。
- 首次加载、整体失败、分区失败、无告警、无日志/事件和权限过期有分开的可理解状态；过期数据不使用绿色 healthy 样式。
- 页面显示 Backend 生成时间与本地时区，手动刷新有防重入；不在本批引入 WebSocket 或高频轮询。

### 3.3 告警管理页

- `/admin/alerts` 提供规则、当前告警和历史三个明确视图；使用 Backend catalog 生成 source-specific 表单，不提供自由表达式输入框。
- 创建/编辑表单只显示 catalog 允许的 metric/label tuple 或 log/event selector，reducer/operator/window/for/severity 只可选合法值；服务端仍重做所有验证。
- update/enable/disable/delete 带当前 revision；409 显示“已被其他管理员修改”并重新读取，不覆盖新版本。disable/delete 和会关闭 firing 的 update 需明确确认。
- 当前/历史展示 rule revision/name、severity、safe object、first/last trigger、value/count、status/recovery/close reason 和时间区。unknown/stale 不显示为 recovered。

### 3.4 用户角色页

- `/admin/users` 只提供精确正整数 user ID 查询，不自动列用户、模糊搜索或保存查询历史。
- 查到后显示 id/username/role/created_at/bootstrap 标记，且只提供 `user|super_admin` 的单账号变更。bootstrap 降级按钮禁用仅是提示，必须同时验证 Backend 409。
- 提升/降级有明确确认和返回状态。非 bootstrap 当前账号自降级成功后，不继续发管理请求，立即清理管理状态并转 `/posts`。

### 3.5 审计页与插件操作审计

- `/admin/audit` 使用签名 cursor 查询 action/resource/outcome/time 固定过滤，显示 actor ID、action、resource、requested/completed、outcome、request ID 和脱敏 details。
- 在现有 exporter-plugin Backend handler/service 周围接入总方案 §8 的 requested/completed 审计，覆盖 connection-test/install/config/start/stop/update；无 Secret/config/raw upstream error 进 details。
- 插件远程操作成功但 completed 审计不可写时，requested/unknown 保留，API 不伪称 Monitor 已回滚；页面明确显示未知结果并允许重新读实际插件状态。
- 角色、规则和告警转移已有审计与插件审计在同一页使用固定 action 标签，不使用 details 自由文本做过滤。

### 3.6 前端 DTO、安全与交互

- 为 overview/alerts/users/audit 建立字段精确的 TypeScript 类型与运行时 validator，响应多余/缺失/非法字段安全失败。
- 每个 view 的请求可取消，切换路由不留下过期响应；变更操作防重入，错误后保留非敏感选择但不保留 Secret。
- 完整键盘焦点、窄屏交付与统一设计令牌的最终产品化属于 Phase 16，但本批新表单/对话框不得存在阻断性键盘不可达或桌面主视图水平溢出。

## 4. 不在本批范围

- 修改告警规则语法、状态机、评估周期或三源存储合同，除非真实 UI 验收暴露阻断缺陷。
- 外部通知、ack/silence/escalation、大屏自定义、任意图表编辑或高频实时推送。
- 用户列表/模糊搜索/批量授权、用户删除或第三种角色。
- 对所有 metrics 做通用可视化工具，或让大屏成为 PromQL/DSL 代理。
- Phase 16 的跨平台、多架构、统一视觉系统、升级和备份/恢复。

## 5. 建议实施顺序

1. 有界确认 key metric tuple/freshness 与 Monitor/plugin 部分错误，先写 overview 严格 DTO 和服务单元测试。
2. 实现 overview 有界 fan-out 与六 section 部分降级，用真实 Backend API 验证快照。
3. 将 `/admin/` 替换为真实大屏，完成整体/分区/空/过期状态。
4. 依次实现 alerts、users 和 audit 页面，每页使用真实 API 与负向契约。
5. 为六类插件变更接入 requested/completed 审计，在页面验证 success/failure/unknown 和脱敏。
6. 完成默认落点、revision 冲突、bootstrap 保护、自降级、partial failure 和 DOM/bundle 扫描，然后更新版本/记录。

## 6. 预计直接影响文件

- 新的 Backend overview service/handler/DTO 与直接测试
- `backend/internal/metricquery/`、`logquery/`、`eventquery/`、`exporterplugin/` 的只读内部组合边界
- Phase-15-01 审计边界的插件 action 接线
- `backend/internal/http/`、`backend/internal/apperror/`、`componentmetrics/routes.go` 及直接 contract tests
- `admin-frontend/src/` 的 overview/alerts/users/audit types、services、views、router、styles 与 tests
- `admin-frontend/e2e/` 或现有 acceptance image 中的真实浏览器流程
- `scripts/verify-admin-frontend.sh` 与必要的强归属验收辅助文件
- `VERSION`、两个 Frontend package/lockfile 版本、受管镜像元数据和同名实施记录

## 7. 批次验收标准

### 7.1 大屏和部分降级

- super_admin 登录默认进入 `/admin/`，大屏显示六组件、六插件、六个 key metric、logs/events 三严重度计数和当前/近期告警；值均能对应真实 Backend DTO。
- 分别使 VictoriaMetrics、Elasticsearch 和 Monitor 不可用，只有相关 section 降级，其他 MySQL/告警/角色数据继续显示；页面不把过期数据标绿。
- 任一 overview 分区响应包含未知/多余字段时，Frontend 安全显示该区错误，不把未验证值插入 DOM。

### 7.2 告警、用户和审计页

- 通过告警页从 catalog 创建 Metrics、Logs、Events 各一条规则，完成查看、编辑、启停、revision 冲突、当前与历史展示，浏览器不产生任意查询语句。
- 按精确 user ID 查询、提升/降级非 bootstrap 账号，bootstrap 试图降级获得 Backend 409 并在页面可理解展示。
- 非 bootstrap 当前 super_admin 自降级后立即清理管理 DOM 并转到用户端，同 Cookie 的下一管理 API 403。
- 审计页可查角色、规则、告警转移与六类插件变更；插件 success/failure 有 requested/completed，受控完成写失败场景留下 unknown 而不伪造结果。

### 7.3 授权、脱敏与完成条件

- 未登录和普通用户直接请求 overview/alerts/users/audit API 分别 401/403，普通用户打开对应页不发起管理数据请求。
- 使用含特征串的插件 Secret/配置失败后，overview、alert object、audit details、日志、DOM、bundle 和验收输出均查不到该特征串。
- 两个 Frontend 的 tests/typecheck/build 通过，用户应用无管理页回归，现有 Metrics/Logs/Events/Plugins 专项页仍可用。
- 根、两个 Frontend 和受管版本元数据为 `1.12.5`，分支为 `develop/1.12.5`，同名实施记录与真实命令一致。

## 8. 固定验证命令与回归范围

实施中先运行 overview/plugin audit Backend packages 和各新 Frontend view 的直接测试。本批最终 diff 固定运行：

```bash
(cd backend && go test ./internal/adminoverview/... ./internal/exporterplugin ./internal/alert/... ./internal/http)

(cd admin-frontend && npm test)
(cd admin-frontend && npm run typecheck)
(cd admin-frontend && npm run build)
(cd frontend && npm test)
(cd frontend && npm run typecheck)
(cd frontend && npm run build)

bash scripts/verify-admin-frontend.sh --self-test
bash scripts/verify-admin-frontend.sh --dashboard-alerts-users

python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/1.12.5 --base-ref upstream/main
git diff --check
git diff --cached --check
```

- overview package 的实际名称若不是 `adminoverview`，在实施记录替换为真实直接 package；不因名称变化省略 Backend 测试。
- `verify-admin-frontend.sh --dashboard-alerts-users` 必须经唯一 edge origin 使用真实 Backend/VM/ES/Monitor/MySQL，覆盖六区大屏、新三页、插件审计、部分降级、授权和脱敏，最后强归属清理。
- 本批只需在浏览器中使用已有三源告警闭环，不因新页面重跑 Phase-15-03 全部进程重启/故障矩阵，除非相关代码改变。
- 提交后补充运行 `git diff --check upstream/main...HEAD`。

## 9. 实施记录与下一批交接

完成前创建 `dev/logs/Phase-15/Phase-15-05-管理大屏与告警权限操作闭环.md`。

记录必须包含 overview 六 section 实际 DTO/key metric tuple/新鲜度、fan-out 超时、大屏各状态、三源规则浏览器流程、revision 冲突、bootstrap/自降级、插件 requested/completed/unknown 审计、partial failure、DOM/bundle 扫描、命令结果、偏差和后续项。交给 Phase-15-06 的固定输入至少包括：

- 已使用真实 Backend 数据的默认大屏和全部管理页。
- 已完成的角色、规则、incident 和六插件管理审计合同。
- 前五批的精确验收入口与仍有效证据，供最终 Compose 只做跨批组合。
