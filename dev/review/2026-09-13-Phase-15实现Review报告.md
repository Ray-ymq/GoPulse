# Phase 15 实现 Review 报告

## 1. 基线、范围与结论

- 审查时间：2026-09-12 至 2026-09-13（Asia/Shanghai）；报告按完成日期命名。
- 用户指定分支：`develop/1.12.7`。两次 fetch/远端 heads 查询均未发现已有同名分支；按用户随后“从远端创建”的要求，从最新 `origin/main` 创建本地 `develop/1.12.7`。不是从一个已存在的远端 `develop/1.12.7` 拉取。
- **权威代码基线：`5cf850b1aa82e2dc4a298c60e5614db53cdc9f49`**，提交标题 `feat: complete Phase 15 Compose integration acceptance (#137)`。创建后解除自动生成的 `origin/main` upstream 关联，避免后续误推主线；本次不推送。
- 当前完成产品版本仍为 **`1.12.6`**。`1.12.7` 是用户指定的本次 Review 分支名，不表示第七开发批次已经实施或完成；未改总方案版本分配、`VERSION` 或应用版本。
- 对照合同：`dev/imple/Phase-15/Phase-15-总实施方案.md` 及六个拆分方案；历史证据来自 `dev/logs/Phase-15/`。代码检查集中于角色/引导账号、告警目录与查询/状态机、管理大屏聚合、双应用入口、新管理页及审计接线。

**结论：发现 3 项 P2 实现问题，均有最小失败复现；另有 1 项 P3 文档状态滞后。不能据既有绿色验收记录认定 Phase 15 已无合同缺口。** 本次没有发现并证实 P0/P1 问题，但这不是全仓安全保证。已有完整 Compose 交付记录不被本报告否定，三项问题应在后续整改中关闭。

本次只交付报告，不修改生产实现或永久测试。临时复现文件执行后移出工作树，完整复现源码随本报告附录保存。未读取、修改或提交已有未跟踪文件 `~`。

## 2. 发现清单

| 编号 | 级别 | 问题 | 影响 |
| --- | --- | --- | --- |
| P2-01 | P2 | 列表接口仍只允许 Metrics 来源过滤 | Logs/Events 规则、当前告警和历史无法按来源查询 |
| P2-02 | P2 | 大屏忽略 Monitor 已确认的插件 failed 状态 | 最近一次 up=1 可使失败插件暂时被标记 healthy |
| P2-03 | P2 | 新告警/用户/审计页缺少字段级运行时 DTO 校验 | 不合法响应进入管理状态、展示和操作分支 |
| P3-01 | P3 | 总方案当前状态仍称第六批待合入 | 与权威主线提交和 VERSION 不一致 |

级别含义：P2 为应修复的正常优先级功能/合同缺陷；P3 为非阻断文档维护项。没有把未经复现的推测升级为缺陷，也没有据畸形响应测试宣称真实凭据泄露或权限绕过。

### P2-01：Logs/Events 来源过滤被统一校验拒绝

**位置：** `backend/internal/alert/handler.go:270-272`；关联 `backend/internal/alert/list.go` 的来源过滤 SQL。

总方案 §12 明确 rules 接受 status/source、current 接受 severity/source、history 接受 status/severity/source/rule 及时间。Phase-15-03 已扩展 catalog、规则创建和评估到三源，但共用 list handler 仍使用：

```go
q.Source != "" && q.Source != "metrics"
```

因此 `GET /api/v1/alerts/{rules,current,history}?source=logs` 和 `?source=events` 均在访问 repository 前返回 `400 validation_failed`。底层 repository 已能按 source 参数过滤，错误位于 HTTP 入口残留的单源限制；无过滤的列表和三源评估不因此失效。

**本次复现：** 在 Gin 路由直接注册真实 Handler，不配置 repository，因为请求应当尚未访问数据库；六种路径/来源组合全部返回 400，预期“合法来源不在校验层拒绝”的断言失败。只证明输入被拒绝，不声称已经验证数据库分页结果。

**建议修复与验收：** 共用允许值扩为 `metrics|logs|events`，继续拒绝未知 source。定向验证三源查询进入 repository、各列表返回对应来源，并保持已有签名 cursor 的过滤条件；不需要修改评估算法。

### P2-02：失败插件被新鲜的历史 up 样本覆盖为 healthy

**位置：** `backend/internal/adminoverview/sources.go:175-193`，`Plugins` 聚合器。

聚合器先从 metric 样本建立 `Status=healthy`，随后只复制 Monitor 的 desired/observed 状态并检查 `last_success_at` 新鲜度，没有根据 `observed_state=failed` 降级。最后仅在 up 不等于 1 时改为 degraded。

真实可达场景是插件刚完成一次成功采集即异常退出：Monitor 已返回 failed，但 VictoriaMetrics 最近一次 up=1 和 Monitor last_success_at 都仍在 90 秒新鲜度阈值内。返回对象会同时携带 `observed=failed` 与 `status=healthy, reason_code=ok`。这违反大屏以服务端聚合真实状态、不能把旧成功值当当前健康的合同（总方案 §14）。影响窗口取决于下一次采样/状态变化，最迟可持续到旧成功证据超过新鲜度门限；不是宣称失败插件永久 healthy。

**本次复现：** 使用真实 `exporterplugin.Client` 解析本地 HTTP fixture：Redis 插件 desired=running、observed=failed、固定安全错误 process_exited，最后成功时间为当前时间前 1 秒；Samples stub 返回同一时刻的 up=1。先确认客户端确实成功解析 failed，再调用真实聚合器。得到：

```text
failed plugin reported healthy: observed=failed status=healthy reason=ok up=1
```

**建议修复与验收：** 定义 Monitor 生命周期状态与采样状态的健康优先级；已确认 failed 不得被旧 up=1 覆盖，保留 observed/desired 和安全原因。用本次失败场景及正常 running/up=1 各一例验证，不扩展成所有插件状态组合审计。

### P2-03：新管理页以 TypeScript 泛型代替运行时数据验证

**位置：**

- `admin-frontend/src/services/http.ts:110-116,132-143`：`requestData<T>` 只检查 data 存在，`requestPage<T>` 只检查分页外壳，不验证 data/items 字段。
- `admin-frontend/src/views/AlertsView.vue:19,24-25`：catalog、规则/incident、变更结果使用上述非验证 helpers。
- `admin-frontend/src/views/UsersView.vue:7-8`：查询结果和角色变更结果直接赋给管理状态。
- `admin-frontend/src/views/AuditView.vue:7`：分页条目无字段级校验，模板直接显示 details_json。

Phase-15-05 §3.6 明确要求 overview/alerts/users/audit 使用字段精确类型与运行时 validator，额外/缺失/非法字段安全失败。现有 overview 已使用验证路径，三个新页面却未接入已有的 `requestValidatedData` / `requestValidatedPage`。泛型断言不能在运行时拒绝不合法枚举或额外字段。

**本次复现：** 挂载真实 UsersView，mock fetch 返回用户 role=`admin`（非法旧角色）并带额外字段 `unexpected`；提交精确 ID 查询。预期拒绝响应且不显示 article，实际 article 存在，旧角色和角色操作控件进入页面。该测试没有调用真实 Backend 或变更账号权限。

**影响边界：** 已直接证实用户页没有所需响应隔离；告警/审计页同类缺口由实际调用路径确认，未逐页做额外故障注入。未知字段不一定全被渲染；不声称额外 `unexpected` 字段已显示在用户 DOM，也不声称真实服务器已返回 Secret。服务端授权仍是权限边界。

**建议修复与验收：** 为 catalog/rule/incident/managed-user/role-change/audit DTO 建立与 Backend 一致的严格 validator，包含安全 nested object 与枚举，并在这些页面接入验证 helpers。以合法结果成功、畸形结果安全失败验证变更合同，不仅添加 TypeScript interface。

### P3-01：阶段“当前状态”未反映主线合入

**位置：** `dev/imple/Phase-15/Phase-15-总实施方案.md:3`。

总方案当前状态仍写第六批“待合入 upstream/main”，而本次 fetch 的权威 `origin/main` 已是第六批合入提交 `5cf850b`，根 VERSION 为 `1.12.6`。两个配置 remote 指向同一仓库，但本次没有用未 fetch 的 upstream tracking ref 冒充最新状态。

**建议：** 在总方案当前状态增加主线合入 SHA/版本以及本报告待整改项。第六批实施日志中的“本地完成、待合入”是当时历史事实，应保留并追加后续状态，不改写原始验收历史。本项不表示要无条件宣称所有合同均通过。

## 3. 范围映射与未报告为缺陷的事项

| 范围 | 本次检查与结论边界 |
| --- | --- |
| Phase-15-01 角色/引导账号 | 检查迁移中的两角色收敛、singleton/外键约束及管理事务；auth/user/http 定向测试通过。没有重新运行真实旧卷升级或并发 MySQL 管理事务 |
| Phase-15-02/03 三源告警 | 检查受限目录/计数适配、租约、revision 校验、unknown/stale 与事务状态路径；发现 P2-01。MySQL 状态/租约测试本次被显式跳过，不能以其他单测替代 |
| Phase-15-04 独立管理应用 | 检查管理 router、独立 Nginx `/admin/` 回退、两端认证恢复及服务端授权接线；没有重新构建镜像或实际浏览器验证 Cookie |
| Phase-15-05 大屏/操作/审计 | 检查聚合及 DTO 调用、角色操作和插件 requested/completed 审计路径；确认 P2-02/P2-03。没有对未复现的远程审计异常提出额外缺陷 |
| Phase-15-06 阶段收口 | 核对主线提交、版本、阶段日志的最终证据及四个入口 self-test；发现 P3-01。不将 self-test 当作真实 Compose 验收 |

新装缺少 Elasticsearch alias 时保守返回 unknown 而不是 0，已在 Phase-15-03 日志记为明确限制，不重复列为新增缺陷。Phase 16 的真实 Linux/macOS/Windows 支持矩阵不属于本次 Phase 15 Review 门禁。

## 4. 本次实际执行的检查

### 4.1 现有检查

| 工作目录 | 命令 | 实际结果 |
| --- | --- | --- |
| `backend` | `go test -json ./internal/alert/... ./internal/adminoverview ./internal/user ./internal/auth ./internal/exporterplugin ./internal/http ./internal/metricquery ./internal/logquery ./internal/eventquery ./migrations` | 退出 0；JSON 记录 155 个 pass 事件（含子测试）、1 个 skip；允许 Go 缓存，不声称全部强制重跑 |
| `admin-frontend` | `npm test` | 7 files / 24 tests 通过 |
| `admin-frontend` | `npm run typecheck` | 退出 0 |
| `frontend` | `npm test -- src/services/setup.test.ts` | 1 file / 1 test 通过 |
| `frontend` | `npm run typecheck` | 退出 0 |
| 根目录 | `bash scripts/verify-compose.sh --self-test` | 退出 0；不访问 Docker |
| 根目录 | `bash scripts/verify-role-management.sh --self-test` | 退出 0；不访问 Docker |
| 根目录 | `bash scripts/verify-alerts.sh --self-test` | 退出 0；不访问 Docker |
| 根目录 | `bash scripts/verify-admin-frontend.sh --self-test` | 退出 0；不访问 Docker |
| 根目录 | `python3 scripts/ci/validate_versions.py` | 退出 0；元数据与 VERSION=`1.12.6` 一致 |

唯一显式 skip 是 `alert.TestOwnedMySQLStateAndLease`，原因是没有提供 `ALERT_TEST_DSN` 指向验收工具拥有的隔离数据库。未为 Review 访问或改动现有业务数据库。

### 4.2 定向缺陷复现

| 命令 | 实际结果 |
| --- | --- |
| `(cd backend && go test ./internal/alert ./internal/adminoverview -run '^TestReviewPhase15' -count=1 -v)` | 退出 1；来源过滤六子例与 failed 插件健康断言失败，分别支撑 P2-01/P2-02 |
| `(cd admin-frontend && npm test -- src/views/review_phase15_temp.test.ts)` | 退出 1；非法角色结果仍被渲染，支撑 P2-03 |

这些失败是审查发现的预期反例，**并未修复，也未把它们记为通过**。运行现有测试时临时反例尚未加入，故“现有测试通过”与“新增反例失败”不矛盾。

本机辅助输出在 `/tmp/gopulse-phase15-review/`：`backend.jsonl`、`admin.log`、`frontend.log`、`repro-go.log`、`repro-ui.log` 与四个 self-test 日志；该目录可能被系统清理，不是仓库交付物。附录保留可重建的最小源码。

### 4.3 历史证据与未执行范围

第六批日志记载固定 `bash scripts/verify-compose.sh --phase15` 最终通过，包含旧 admin/会话升级、独立双应用、三源真实触发/持续/重启/恢复、故障隔离、Phase 13/14 回归、镜像与脱敏、资源清理。三源持续/重启后 incident ID 为 4/5/6，恢复仍为同一实例；这是**仓库日志中的既有结果**，不是本次重新执行或逐项核验临时制品的结果。

本次未执行：完整 Compose、真实 MySQL 集成、三源故障注入、浏览器 E2E、Docker 镜像重建、两端 release build、全仓库测试、race、外部依赖审计或跨平台支持矩阵。无需为报告文档重跑这些未变化范围，也不对其当前运行状态作通过保证。

## 5. 整改与交付边界

1. 修复 P2-01 的三源过滤、P2-02 的健康状态优先级、P2-03 的响应校验；各自采用直接相关回归。
2. 追加 P3-01 主线状态；如启动可执行第七批，由总方案正式登记版本/分支，再按批次完成条件更新 VERSION 与实施日志。本报告没有提前完成此分配。
3. 本次停止条件为证据足以支撑审查发现、报告和复现可追溯、文档检查通过并单独提交。不顺手修复生产实现或进行额外全面审计。
4. 提交前执行 `git diff --check`、`git diff --cached --check` 并核对暂存区只含本报告；现有 `~` 保持未跟踪。

## 附录：最小复现源码

以下是本次实际执行的临时文件内容。重新验证时分别写回标注路径，并执行 §4.2 命令；验证后移除临时文件，不要误提交为已修复测试。

### `backend/internal/alert/review_phase15_temp_test.go`

```go
package alert
import("net/http/httptest";"testing";"github.com/gin-gonic/gin")
func TestReviewPhase15CountSourceFilters(t *testing.T){
 for _,kind:=range []string{"rules","current","history"}{for _,source:=range []string{"logs","events"}{t.Run(kind+"/"+source,func(t *testing.T){
 g:=gin.New(); NewHandler(nil,"review-key").Register(g.Group("/alerts")); w:=httptest.NewRecorder(); g.ServeHTTP(w,httptest.NewRequest("GET","/alerts/"+kind+"?source="+source,nil));
 if w.Code==400 {t.Fatalf("valid source rejected before repository access: %d %s",w.Code,w.Body.String())}
 })}}
}
```

### `backend/internal/adminoverview/review_phase15_temp_test.go`

```go
package adminoverview
import("context";"fmt";"net/http";"net/http/httptest";"strings";"testing";"time";"github.com/Ray-ymq/GoPulse/backend/internal/metricquery";"github.com/Ray-ymq/GoPulse/backend/internal/exporterplugin")
type reviewFreshUp struct{at time.Time}
func(p reviewFreshUp)AlertPoints(context.Context,string,map[string]string,time.Time,time.Time)([]metricquery.Point,error){return []metricquery.Point{{Timestamp:p.at.Format(time.RFC3339Nano),Value:1}},nil}
func TestReviewPhase15FailedPluginNotHealthy(t *testing.T){
 now:=time.Now().UTC(); at:=now.Add(-time.Second).Format(time.RFC3339Nano)
 body:=fmt.Sprintf(`{"data":[{"id":"redis-exporter","name":"Redis Exporter","version":"1.12.6","kind":"metrics-exporter","source":"redis","desired_state":"running","observed_state":"failed","installed_at":%q,"updated_at":%q,"started_at":%q,"last_scrape_at":%q,"last_success_at":%q,"last_error":{"code":"process_exited","message":"plugin process exited unexpectedly","at":%q}}]}`,at,at,at,at,at,at)
 server:=httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter,r *http.Request){fmt.Fprint(w,body)}));defer server.Close()
 client,err:=exporterplugin.NewClient(server.URL,strings.Repeat("x",32),time.Second);if err!=nil{t.Fatal(err)}
 rows,err:=client.List(context.Background());if err!=nil||len(rows)!=1||rows[0].ObservedState!="failed"{t.Fatalf("invalid fixture: %v %v",rows,err)}
 s:=Plugins(client,reviewFreshUp{now.Add(-time.Second)})(context.Background(),now);p:=s.Items.([]Plugin)[0]
 if p.Status=="healthy"{t.Fatalf("failed plugin reported healthy: observed=%s status=%s reason=%s up=%v",p.Observed,p.Status,p.ReasonCode,*p.Up)}
}
```

### `admin-frontend/src/views/review_phase15_temp.test.ts`

```typescript
import { flushPromises, mount } from '@vue/test-utils'
import { afterEach, expect, it, vi } from 'vitest'
import UsersView from './UsersView.vue'
afterEach(() => vi.unstubAllGlobals())
it('rejects an illegal legacy role and extra response fields before displaying role controls', async () => {
 vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({data:{id:2,username:'legacy-role-response',role:'admin',created_at:'2026-09-12T00:00:00Z',is_bootstrap_super_admin:false,unexpected:'not-allowed'}}))))
 const wrapper=mount(UsersView)
 await wrapper.get('input').setValue('2');await wrapper.get('form').trigger('submit');await flushPromises()
 expect(wrapper.find('article').exists()).toBe(false)
 wrapper.unmount()
})
```
