# GoPulse Phase 11 实现 Review 报告

## 1. Review 信息

| 项目 | 内容 |
| --- | --- |
| Review 日期 | 2026-09-05 |
| 用户指定权威 Review 分支 | `develop/1.8.4` |
| Review 分支创建方式 | fetch 后远端不存在 `origin/develop/1.8.4`；从最新 `origin/main` 创建本地 `develop/1.8.4`，未推送 |
| Review 基线 | `5503b00f62c92a6315910ba059a188696419f534`（PR #101，Review 开始时与 `origin/main` 一致） |
| Phase 11 计划合入点 | `52a43a7a52e6150fb9cf711eeec5159e7b18553c`（PR #95） |
| Phase 11 已合入实现提交 | `eb57c97`（Phase-11-01 / PR #96）、`c710dc0`（Phase-11-02 / PR #98）、`ae267b6`（Phase-11-03 / PR #100） |
| Phase 11 完成记录 | `e0c8bd5`、`fd4ed8b`、`ce7ae5a`，最后一项通过 PR #101 合入 Review 基线 |
| 当前完成版本 | 根 `VERSION`、Frontend `package.json` 与 lockfile 均为 `1.8.3`；本次只新增 Review 文档，不修改版本 |
| `develop/1.8.4` 治理状态 | Phase 11 总实施方案只分配到 `1.8.3`；`validate_branch.py` 对 `develop/1.8.4` 返回 1，当前分支尚不能按仓库规则推送 |
| 实际执行环境 | WSL2 Linux filesystem `/home/ray/GoPulse`，Go `1.26.7`，Node.js `24.20.0`，npm `11.19.0`，Docker `29.7.2` / Compose `v5.5.0`，Python `3.12.3` |
| Review 范围 | Phase 11 总/拆分方案与实施记录、Backend Metrics 查询、Monitor Exporter 代理 trust boundary、Frontend 管理路由/授权状态/runtime validator/页面、隔离浏览器验收、生命周期脚本、版本与分支治理 |
| Phase 11 变更规模 | 相对计划基线 `52a43a7` 至阶段验收提交 `ae267b6`：61 个文件，2966 行新增、206 行删除 |
| 结论 | **不通过（Fail）** |

本次 Review 重点判断：

1. Backend Metrics API 是否只构造固定查询，并对 VictoriaMetrics 的超时、redirect、header/body、字段、标签、时序和容量执行真实有界 trust boundary。
2. Backend Exporter 代理是否同时验证成功 DTO 与错误 HTTP status/code 配对，避免 Monitor 故障伪装成稳定业务结果。
3. Frontend 是否在浏览器边界拒绝缺字段、未知字段、非法时间、未知词汇和不可能组合，并在运行中降权后清除管理能力呈现。
4. 管理路由、真实浏览器查询/分页/插件操作、局部依赖故障、社交回归和资源清理是否满足 Phase 级验收合同。
5. Phase 11 实施记录、版本与用户指定 `develop/1.8.4` 是否满足后续整改批次治理要求。

本次 Review 未修改产品代码。用于复现 findings 的三个临时测试文件均在执行后删除，未纳入提交；原工作区未跟踪文件 `使用指南.md` 未被读取、暂存或提交。

## 2. 总体结论

Phase 11 已交付主体完整、可真实运行的 Milestone 3 可观测管理体验：

- 管理员与普通用户复用现有身份和 Cookie，Backend 对 Metrics、Logs、Events、Exporter API 继续执行数据库实时 admin 授权。
- Backend 新增固定 Redis Metrics 查询目录，服务端生成 selector、时间窗与 step，并对 VictoriaMetrics matrix、固定 provenance、family label、点数和有限数值进行验证。
- Backend Exporter 代理已显著收紧成功响应：拒绝未知/重复字段、异常状态和时间组合，并把 Monitor 数据重新构造成公共 DTO。
- Frontend 已提供独立管理壳层、总览、Metrics、Logs、Events、Exporter 页面，以及查询分页、显式刷新、局部故障和安全错误文案。
- 本次重新执行 `scripts/verify-observability-ui.sh`，7 个真实 Chromium 用例全部通过；真实 install/start/stop/update、三类数据查询、Logs/Events 分页、VM/Monitor/ES 故障恢复、运行中降权、社交回归、bundle 扫描、只读 verify 和资源清理均成功。
- Backend 全量测试/vet、受影响包 race test、Frontend 56 个单元测试与构建、Python CI 测试、脚本语法/self-test、Compose 渲染和版本校验均通过。

但是，当前实现仍不能通过 Phase 11 Review：

1. VictoriaMetrics client 创建了 64 KiB header 限制的 transport，却没有把该 transport 交给 `http.Client`；声明的上游 header 边界实际未生效。
2. Exporter 代理只按 Monitor error code 映射公共错误，不验证 HTTP status/code 是否匹配；Monitor `500` 可以被降级成公共 `404/409/422` 业务结果。
3. Frontend 所谓“严格” runtime validators 会接受 Backend 当前合同明确拒绝的不可能 Metrics/Event 数据，并将这些已知字段继续交给页面渲染。
4. 运行中收到 `403 permission_denied` 后只跳转 `/forbidden`，全局认证状态仍缓存 `role=admin`，普通社交导航继续显示管理入口。
5. 用户指定的 `develop/1.8.4` 尚未获得 Phase 11 权威批次分配，分支治理门禁失败。
6. 管理域未知子路由和最终实施记录分别存在与权威方案不一致的低风险问题。

本次未发现 P0 或 P1 问题；共记录 **5 个 P2、2 个 P3**。真实主闭环通过不抵消 trust boundary、运行时角色状态和分支治理缺口，因此结论为 **Fail**。

## 3. Findings 摘要

| ID | 级别 | Finding | 影响 |
| --- | --- | --- | --- |
| P2-01 | P2 | VictoriaMetrics 64 KiB response header 限制未绑定到实际 HTTP client | 不可信上游可以突破方案声明的 header 资源边界，现有测试未发现 |
| P2-02 | P2 | Monitor error HTTP status 与 code 未做固定配对验证 | 上游故障或畸形响应可被错误呈现为稳定业务 `4xx`，影响 UI 决策与故障语义 |
| P2-03 | P2 | Frontend runtime validators 接受不可能 Metrics/Event 合同 | 浏览器 trust boundary 与实施方案不符，已知字段中的错误/异常数据仍可被渲染 |
| P2-04 | P2 | 运行中降权后仍缓存 admin role 并显示管理导航 | 服务端安全边界仍有效，但普通用户态呈现错误且可反复进入已拒绝入口 |
| P2-05 | P2 | `develop/1.8.4` 无权威批次分配 | 当前 Review 分支无法通过治理门禁，后续整改不能合规推送 |
| P3-01 | P3 | 已授权管理员访问未知管理子路由时跳转 `/forbidden` 而非总览 | 与明确路由合同不一致，造成错误的权限语义 |
| P3-02 | P3 | Phase-11-03 记录在已确认远程成功后仍写“最终结果仍待观察” | 实施记录内部矛盾，降低里程碑证据可审计性 |

## 4. 详细 Findings

### P2-01：VictoriaMetrics response header 上限未应用到真实请求

**位置**

- `backend/internal/metricquery/metricquery.go:171-182`
- 对照 `dev/imple/Phase-11/Phase-11-总实施方案.md:194-201`

**问题**

`NewClient` 克隆默认 transport 并设置：

```go
transport := http.DefaultTransport.(*http.Transport).Clone()
transport.MaxResponseHeaderBytes = 64 << 10
```

但返回的 `http.Client` 没有设置 `Transport: transport`：

```go
client: &http.Client{
    Timeout: timeout,
    CheckRedirect: ...,
}
```

因此实际请求继续使用全局默认 transport，64 KiB 上限完全没有生效。代码和实施记录宣称使用“有界 timeout/header/body”，但当前只有 timeout 与 2 MiB body 限制真实生效。

本次临时定向测试让本地 VictoriaMetrics 伪服务返回 128 KiB response header 和小 body，`QueryRange` 仍成功读取响应，确认不是纯静态推断：

```text
ok github.com/Ray-ymq/GoPulse/backend/internal/metricquery
```

**影响**

- VictoriaMetrics 是 Backend 需要防御的不可信内部响应边界；异常或被替换的上游可以使用明显超过方案限制的 header。
- Go 默认 transport 仍有自身上限，所以这不是无界内存问题，但项目声明的 64 KiB 收敛边界和验收证据不成立。
- 当前正式测试只覆盖 POST、Basic Auth 和 redirect，没有覆盖 oversized header，因此该回归可持续存在。

**建议整改**

- 把 clone 后的 transport 显式赋给 `http.Client.Transport`。
- 增加一成功一失败的定向测试：正常 header 成功，超过 64 KiB 的 header 映射为 `503 metrics_unavailable`。
- 保留现有 2 MiB body、timeout 与 redirect 测试，不需要扩展成通用 HTTP client 审计。

### P2-02：Exporter 代理忽略 Monitor error status/code 配对

**位置**

- `backend/internal/exporterplugin/client.go:154-180`
- `backend/internal/exporterplugin/client.go:443-457`
- `backend/internal/http/response/response.go:67-81`
- 对照 `dev/imple/Phase-11/Phase-11-02-Exporter管理与可观测总览闭环.md:37-45`

**问题**

所有非 `2xx` Monitor 响应都先解析 `error.code`，然后只按 code 映射：

```go
if response.StatusCode < 200 || response.StatusCode >= 300 {
    code, decodeErr := decodeErrorCode(payload)
    ...
    return nil, response.StatusCode, mapMonitorError(code)
}
```

`response.StatusCode` 被返回给内部调用者后没有参与任何决策。于是以下畸形响应：

```http
HTTP/1.1 500 Internal Server Error
Content-Type: application/json

{"error":{"code":"plugin_not_found","message":"internal"}}
```

会被 Backend 映射成 `CodePluginNotFound`，随后公共 HTTP 层输出 `404 plugin_not_found`，而不是方案要求的 `503 monitor_unavailable`。

本次临时定向测试已确认 `Client.Get` 对上述 `500 + plugin_not_found` 返回 `CodePluginNotFound`。

**影响**

- Monitor 故障、代理错误或畸形响应可以伪装成“未安装”“冲突”或“操作失败”等稳定业务状态。
- Frontend 可能据此提示用户安装、改变操作路径或隐藏真实依赖故障；总览也无法准确表达 Monitor unavailable。
- 成功响应 trust boundary 已很严格，但错误响应仍没有形成同等级别的固定合同。

**建议整改**

- 建立固定 status/code 配对并同时校验，例如：`400/plugin_package_invalid`、`404/plugin_not_found`、`409/plugin_conflict|plugin_operation_in_progress`、`422/plugin_operation_failed`。
- code 未知、status 未知或二者不匹配时统一返回 `503 monitor_unavailable`。
- 增加一个合法业务错误成功映射测试和一个 status/code mismatch 测试即可，不需要枚举所有网络错误组合。

### P2-03：Frontend runtime validators 未拒绝 Backend 当前合同中的不可能组合

**位置**

- `frontend/src/services/observability.ts:69-110`
- Backend 对照：`backend/internal/eventquery/eventquery.go:475-538`
- 对照 `dev/imple/Phase-11/Phase-11-总实施方案.md:219-225`

**问题**

方案明确要求 runtime validator 拒绝“非法时间、未知词汇和不可能组合”，并与 Backend 当前合同一致。当前实现只验证了部分结构：

- `isMetricResult` 不验证 `from < to`、range 对应的固定 step、点时间递增、点位于窗口内、series label 唯一或重复 series。
- `isEventEntry` 只验证 event name、severity、operation 和部分 error/scrape 字段；不验证固定 message、plugin version SemVer、previous version、from/to state 及各事件的完整 metadata 组合。
- 时间只使用 `Date.parse`，不要求 Backend 合同中的规范 UTC 形式。

本次临时 Vitest 复现证明以下数据均被 validator 返回 `true`：

1. `exporter_plugin_started` 使用任意 message，且缺少 `plugin_version/from_state/to_state`。
2. Metrics 使用 `from > to`、任意 `step_seconds=999`、重复 series，并包含倒序时间点。

```text
Test Files  1 passed (1)
Tests       2 passed (2)
```

这些文档会被 Backend event/metric 边界拒绝，却能穿过浏览器边界并交给页面渲染。

**影响**

- 当前健康 Backend 会先过滤底层数据，因此正常真实闭环不受影响；问题位于计划明确要求的第二道浏览器 trust boundary。
- 一旦 Backend DTO 回归、错误网关返回已知字段的畸形成功 body，Frontend 不能按承诺 fail closed。
- 现有浏览器“畸形响应”用例只注入未知字段，无法发现已知字段内部合同不一致。

**建议整改**

- 复用前端受版本控制的固定事件 message、state、SemVer 和 metadata 组合表，使其与 Backend `validDocument` 等价。
- 对 Metrics 校验固定 range/step、规范 UTC、窗口顺序、series 唯一性和点时间严格递增；是否校验点落在窗口内应与 Backend 最终合同保持一致。
- 增加一个合法 DTO 与一个代表性不可能组合测试；无需把 Backend 全部 validator 用例复制到 Frontend。

### P2-04：运行中降权后全局认证状态仍保留 admin 能力呈现

**位置**

- `frontend/src/main.ts:15-17`
- `frontend/src/composables/useAuth.ts:8-22,45-53,73-83`
- `frontend/src/components/AppNav.vue:25-30`
- 对照 `dev/imple/Phase-11/Phase-11-总实施方案.md:112-116`

**问题**

可观测请求收到 `403 permission_denied` 时，公共 HTTP 层会调用 forbidden handler。当前 handler 只执行：

```ts
if (router.currentRoute.value.path.startsWith('/admin/')) {
  await router.replace('/forbidden')
}
```

它没有刷新 `/users/me`，也没有使缓存用户失去 admin 能力。`useAuth` 只有 `401` 会清空身份；`AppNav` 则继续按缓存的 `auth.user.role === 'admin'` 显示“可观测”入口。

本次临时 Vitest 先恢复一个 admin 用户，再返回 `403 permission_denied`，结果确认：

```text
auth.user.value.role == "admin"
auth.status.value == "authenticated"
```

真实浏览器用例只验证了跳转、管理页数据清理和社交会话保留，没有断言降权后的社交导航不再显示管理入口。

**影响**

- Backend 实时授权仍会拒绝后续请求，因此没有服务端越权。
- 用户已经是普通用户，Frontend 却继续展示管理员入口；反复点击后才由路由 guard 的 `/users/me` refresh 修正缓存，造成错误使用态和多余拒绝请求。
- 不满足“只有当前 admin 显示入口”以及运行中 role 变化后进入普通用户态的产品合同。

**建议整改**

- 为 `permission_denied` 增加“保留 authenticated 会话但立即移除 admin capability”的认证状态更新，或在跳转前执行一次受控 `/users/me` refresh。
- 不应复用 `clear()`，否则会把已登录普通用户误判为登出。
- 扩展现有 demotion 浏览器用例：返回 `/posts` 后断言“可观测”导航消失，且不会自动重发管理查询。

### P2-05：`develop/1.8.4` 没有 Phase 11 权威批次分配

**位置**

- `dev/imple/Phase-11/Phase-11-总实施方案.md:54-72`

**问题**

Phase 11 权威分配表只包含：

- Phase-11-01 → `1.8.1` / `develop/1.8.1`
- Phase-11-02 → `1.8.2` / `develop/1.8.2`
- Phase-11-03 → `1.8.3` / `develop/1.8.3`

本次用户指定的 `develop/1.8.4` 在权威表中不存在。实际执行：

```text
python3 scripts/ci/validate_branch.py --branch develop/1.8.4 --base-ref origin/main
ERROR: develop/1.8.4 must map to exactly one authoritative allocation; found 0
```

**影响**

- 当前本地分支可以承载 Review 文档，但不能通过推送前治理门禁。
- 后续整改提交无法在不更新权威总实施方案的情况下合规推送或创建普通开发 PR。

**建议整改**

在开始代码整改前，把 Review 整改作为明确批次加入 Phase 11 总实施方案，例如 Phase-11-04 → `1.8.4` / `develop/1.8.4`，并补充与本报告 findings 对应的验收标准。仅新增 Review 报告不修改 `VERSION`；真正完成整改批次时再把版本更新为 `1.8.4`。

### P3-01：未知管理子路由使用了错误的权限语义

**位置**

- `frontend/src/router/index.ts:34-45`
- 对照 `dev/imple/Phase-11/Phase-11-总实施方案.md:114-116`

**问题**

总实施方案明确规定：“管理域未知子路由只在通过 admin 守卫后重定向总览”。当前 child catch-all 为：

```ts
{ path: ':pathMatch(.*)*', redirect: '/forbidden' }
```

因此一个已通过 admin 守卫的管理员访问 `/admin/observability/unknown` 时会看到“无权访问”，而不是回到 `/admin/observability` 总览。

**影响**

没有权限绕过，但把“路由不存在”错误表达成“管理员无权限”，与信息架构合同不一致。

**建议整改**

把 child catch-all 重定向目标改为 `/admin/observability`，并增加一个 admin 未知子路由测试；普通用户仍应先被父级 admin guard 阻止。

### P3-02：最终实施记录仍保留已过期的“待观察”结论

**位置**

- `dev/logs/Phase-11/Phase-11-03-集成验收与Milestone-3收口.md:87-93`
- `dev/logs/Phase-11/Phase-11-03-集成验收与Milestone-3收口.md:104-109`

**问题**

同一记录第 4.3 节已经确认运行 `33970855739` 第 2 次尝试全部成功、PR #100 已合入；但第 6 节最后仍写：

```text
本批将 Vite 明确绑定 127.0.0.1 后再次触发远程门禁，最终结果仍待观察。
```

这显然是合入最终结果后未同步更新的旧状态。

**影响**

不会影响运行时，但实施记录对同一远程门禁同时给出“全部成功”和“仍待观察”，不满足“只记录真实已执行状态”的审计要求。

**建议整改**

把该段改成最终运行结果和已完成结论，或删除与第 4.3 节重复的过程描述。

## 5. 已通过的关键检查

### 5.1 代码、静态检查与构建

以下命令在本次 Review 基线上实际执行并通过：

```text
(cd backend && go test -count=1 ./...)
(cd backend && go vet ./...)
(cd backend && go test -race -count=1 ./internal/metricquery ./internal/exporterplugin ./internal/http/...)
(cd frontend && npm test -- --run)        # 11 files / 56 tests
(cd frontend && npm run build)
python3 -m unittest discover -s scripts/ci -p 'test_*.py'  # 25 tests
python3 scripts/ci/validate_versions.py
bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-business.sh \
  scripts/verify-exporter.sh scripts/verify-monitor.sh scripts/verify-router.sh \
  scripts/verify-marshaller.sh scripts/verify-logs.sh scripts/verify-events.sh \
  scripts/verify-observability-ui.sh scripts/package-redis-exporter.sh
docker compose --env-file .env.example --file deploy/compose.yaml config --quiet
scripts/verify-observability-ui.sh --self-test
git diff --check
```

`validate_branch.py --branch develop/1.8.4` 是唯一预期失败项，已记录为 P2-05，而不是误报为通过。

### 5.2 真实浏览器与隔离生命周期

本次实际执行：

```text
scripts/verify-observability-ui.sh
```

结果：

```text
7 passed (1.4m)
[verify-observability-ui] Isolated real-browser observability acceptance,
read-only verify, bundle scan, and lifecycle cleanup passed.
```

通过内容包括：

- 普通用户无管理导航，全部管理页面在组件请求前被阻止，全部可观测/Exporter API 返回 `403`。
- 管理员从直接管理 URL 登录后返回原目标页。
- 浏览器真实安装、停止、启动、更新 Redis Exporter，并查询真实 Metrics、Logs、Events。
- Logs/Events 真实筛选、超过 50 条分页、cursor 损坏恢复和 DTO-only 展示。
- VictoriaMetrics、Monitor、Elasticsearch 逐项故障与恢复；其他总览区域及非搜索社交行为保持可用。
- 窄屏、键盘焦点、已知 malformed-response 用例、运行中数据库降权和社交会话保留。
- Frontend production bundle 内部地址/身份/alias/Topic/token/path 扫描。
- 隔离 `dev.sh → verify.sh → down.sh` 及 Compose container/network/volume、进程清理。

Review 后再次核对随机项目 `gopulse-observability-d093b093b70e`，未残留 container、network 或 volume。

### 5.3 定向 Finding 复现

三个临时测试均只用于证明已定位的问题，执行后删除：

```text
# Backend：128 KiB VM response header 仍被接受；Monitor 500 + plugin_not_found 被映射为业务错误
(cd backend && go test -count=1 ./internal/metricquery ./internal/exporterplugin)

# Frontend：不可能 Event/Metric DTO 被 validator 接受
(cd frontend && npm test -- --run src/services/review-phase11-temp.test.ts)

# Frontend：permission_denied 后缓存 role 仍为 admin
(cd frontend && npm test -- --run src/services/review-phase11-role-temp.test.ts)
```

这些临时测试通过表示问题被稳定复现，不表示对应行为正确。

## 6. Review 结论与整改顺序

### 6.1 结论

Phase 11 的真实可观测主闭环、管理员最终授权、页面功能、局部故障恢复和隔离生命周期均已成立；现有实现不是整体不可用状态。但 4 个生产行为/边界问题和 1 个分支治理问题仍未满足权威实施方案，因此本次 Review 结论为：

> **Fail：不得把当前状态视为 Phase 11 Review 已关闭。**

### 6.2 建议整改顺序

1. 先在 Phase 11 总实施方案中分配 Phase-11-04 → `1.8.4` / `develop/1.8.4`，使分支治理合法。
2. 修复 VictoriaMetrics transport 绑定与 Monitor error status/code 配对，并添加最小定向 Backend 测试。
3. 收紧 Frontend Metrics/Event runtime validators，补一个合法与一个代表性非法组合测试。
4. 修复运行中降权后的 auth capability 状态和导航呈现，扩展现有 demotion 浏览器断言。
5. 修正未知管理子路由和 Phase-11-03 实施记录矛盾。
6. 在最终 diff 上运行受影响 Backend/Frontend 检查、`validate_versions.py`、`validate_branch.py --branch develop/1.8.4`、`scripts/verify-observability-ui.sh` 和 `git diff --check`；通过后更新 `VERSION` 为 `1.8.4` 并停止，不扩展为无关审计。

## 7. 非阻断说明

- 本次没有发现普通用户可绕过 Backend admin 授权、浏览器直接访问 VictoriaMetrics/Elasticsearch/Monitor、任意 MetricsQL/DSL 注入或 raw JSON/HTML 渲染。
- 真实浏览器主闭环及资源清理本次通过，无需因为 Review findings 否定已有的端到端能力；整改应聚焦本报告列出的边界。
- 页面不自动轮询、单节点 VictoriaMetrics 复用内部 Basic Auth、Elasticsearch 同时承担 search/readiness 与 Logs/Events 查询，均是计划已记录边界，不在本次 findings 中重复升级。
