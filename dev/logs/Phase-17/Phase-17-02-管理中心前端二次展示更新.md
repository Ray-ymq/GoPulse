# Phase-17-02：管理中心前端二次展示更新实施记录

> **2026-09-15 视觉验收更正**：第 1–6 节是提交 `a242855` 的首轮记录，其“视觉验收完成”结论被用户复核否定，不再作为视觉完成依据。功能测试通过不等于参考图还原通过。本轮在同一 `develop/1.14.2` 分支补正；最新实际结果见第 7 节。版本仍为 `1.14.2`。

## 1. 完成范围与基线

- 日期：2026-09-15，WSL/Linux 工作区 `/home/ray/GoPulse`。
- 开工执行 `git fetch origin`，核对当前 `develop/1.14.2` 为本批已创建分支；基线 `fb14875` 含本批规划，其前序为已合入 main 的 `3e13a2b`（1.14.1）。未重建、重命名或覆盖分支。
- 原有未跟踪 `Management_Center/` 是用户原图，未改动、未加入提交。逐张读取规划归档的第二组五张 PNG 作为视觉输入。
- 完成共享管理壳层及系统概览、Metrics、Logs、Alerts、Exporter 展示更新；目标版本 `1.14.2`。未修改 Backend、API、Schema、消息处理、Compose 或 release 合同。
- 首次在读取直接页面、DTO 和既有验收工具后即修改共享壳层及 Logs；未读取第三方依赖源码、未开展独立审查或扩大覆盖率活动。

## 2. 五图到真实 DTO 的映射及已接受差异

共同基准：深蓝侧栏、蓝色激活导航、白色顶栏、浅灰蓝工作区、四列摘要、紧凑边框及列表/详情。导航改为中文分组与紧凑符号；保留统一登录、返回社交、账号身份、退出和权限撤销分流。没有虚构全局搜索、通知数量或 Production 标签。

| 第二组基准 | 实际数据及实现 | 相对 1.14.1 的更新与 API 差异 |
| --- | --- | --- |
| `(1).png` 系统概览 | `getOverview()` 的 components、key_metrics、plugins、alerts、events、logs 六分区 | 组件行横跨内容区，其余分区成双列，摘要仍明确健康组件/返回组件、已安装插件、15 分钟日志、触发告警。保留真实分区 degraded/unknown。Overview 只有快照和计数，不具备 CPU/内存、版本/运行时长、双历史趋势或逐条事件 DTO，因此用快照、安全采样详情和时间窗计数替代，不伪造曲线。 |
| `(2).png` Metrics | 固定目录与 `MetricResult.series[].points`、labels、unit、step_seconds、from/to | 四卡分别为首条非空序列最新值、返回窗口全部采样最大值/算术平均值、返回 Series 数；保留真实图表、最近采样，增加 Series 表。图表标题绑定最后成功响应的 `result.metric`，未提交的新选择不会冒充图中指标。不提供缺失的 CPU 辅图、同比、任意实例或跨指标汇总。 |
| `(3).png` Logs | 已验证日志 DTO 和原有精确字段查询/游标 | 卡片流改为时间/级别/来源/消息表；行按钮可选择，右侧显示当前记录详情与安全标签。摘要是已加载记录而非窗口总量。筛选/刷新清除选中详情；失败保留旧列表并显示失败，不泄露原始错误。没有全文检索、自动实时流、Trace 或原始文档入口。 |
| `(4).png` Alerts | 原 catalog/rules/current/history 及严格 `Incident` | 保留 rules 默认入口和创建/编辑/启停/删除/冲突处理；current/history 增加四卡、已加载记录的严重程度/生命周期/名称筛选、选择表与详情。`warning/critical` 严重程度与 `firing/recovered/closed` 生命周期分开展示；unknown/stale 非恢复。详情含规则修订、选择器、触发/恢复/关闭、评估次数与最近值。无静默规则、手工确认、虚构关联 Trace 或历史趋势。 |
| `(5).png` Exporter | 原插件目录、已安装状态列表、配置 schema、安全错误与生命周期 DTO | 增加已安装总数/running/stopped/failed 四卡；六插件改为左侧选择列表，右侧状态、配置、包更新，窄屏单列。计数不是目录可交付数量；安装数不会虚构为六个。保留现有连接测试、安装、替换配置和启动/停止/更新。目标不预填内部地址，Secret 密码输入不回填、无明文显隐、操作后清空；不添加历史版本/运行日志等无 API 页签。 |

截图中的长字段、安全说明和真实记录数造成的高度与换行差异已接受；不是像素级静态图复刻。统计缺失显示未知，不以 0 或 healthy 代替。保留既有分区安全状态标签、加载/空/错误状态和 reduced-motion 样式。

## 3. 实际修改文件

- `admin-frontend/src/components/AdminLayout.vue`：导航分组、名称、符号与品牌展示。
- `admin-frontend/src/styles.css`：第二组配色、卡片、组件行、局部滚动表格、选中态、详情和三尺寸布局。
- `admin-frontend/src/views/DashboardView.vue`：标题与分区布局顺序。
- `admin-frontend/src/views/ObservabilityMetricsView.vue`：真实采样统计、Series 表、响应绑定标题。
- `admin-frontend/src/views/ObservabilityLogsView.vue`：选择表、详情、焦点返回与刷新清除选择。
- `admin-frontend/src/views/AlertsView.vue`：已加载统计和筛选、列表/详情、选择清除与焦点返回，保留原规则操作。
- `admin-frontend/src/views/ObservabilityExportersView.vue`：状态摘要、选择列表/详情布局。
- `admin-frontend/src/views/ObservabilityLogsView.test.ts`：选择成功后刷新失败，详情清除、旧数据保留和错误脱敏的代表性验证。
- `frontend/e2e/dashboard.spec.ts`：原告警卡片定位同步为表格，关闭原因从选中详情验证。
- `frontend/e2e/admin-visual.spec.ts`：五页、1440/768/390 三尺寸、详情选择/关闭/焦点、最终两页变化的精确补验。
- `scripts/ci/verify_admin_visual.py`：真实生命周期先于截图；补跑入口复用明确成功回执；最终 Metrics/Exporter 变化只补受影响场景，其余结果标明来源。
- `VERSION`、`.env.example`、双 frontend 的 `package.json`/`package-lock.json`：统一 `1.14.2`。
- 本记录及同批/总实施计划状态。

## 4. 检查与真实结果

### 本地固定门禁

| 命令 | 结果 |
| --- | --- |
| `(cd admin-frontend && npm test && npm run build)` | 最终 11 个文件、40 个测试通过；vue-tsc、tsc、Vite 生产构建通过。 |
| `./scripts/test-frontends.sh` | 最终普通前端 18 文件/65 测试、管理前端 11 文件/40 测试通过；双前端类型检查和生产构建通过。 |
| `python3 scripts/ci/validate_versions.py` | Version metadata matches root VERSION。 |
| `python3 scripts/ci/validate_branch.py --branch develop/1.14.2 --base-ref origin/main` | Branch governance passed。 |
| `git diff --check` | 通过。 |

最终本地测试回执：`.run/gopulse-p1401-f83d282c31b2/{admin-unit-build.log,frontends.log}`。

首次开发中构建发现 Exporter 包裹层起始标签未替换（Invalid end tag），补齐后构建通过。最终新增日志测试单独执行 `npx vitest run src/views/ObservabilityLogsView.test.ts`，1 passed；之后固定测试包含此测试。没有读取 Vue 编译器实现来排查。

### 真实 Linux amd64 Compose 浏览器门禁

使用现有验收 runner 构建工作树候选，启动独立真实 Compose、管理员/普通用户/可降权用户，从唯一同源入口运行 Playwright。不是 mock API，也不是开发服务器。所有轮次结束均完成归属清理并保留既有资源。

1. 首轮：`python3 scripts/ci/verify_admin_visual.py`
   - 证据 `.run/gopulse-p1401-fec96021f75a/`。
   - `admin-browser.log`：`npx playwright test e2e/admin-frontend.spec.ts`，**4 passed**。包含认证、同源请求、普通用户分流、六插件目录、真实 Redis 启停、指标/日志/事件、角色撤销清除 Secret 和 DOM。
   - dashboard/visual 联合执行：**1 passed、2 failed**。成功的是本人降权及新管理页普通用户隔离。失败原因分别是视觉用例早于真实告警创建、旧 `article` 定位与新表格不一致；均为本批展示变化直接影响的验收适配。
2. 补跑：`python3 scripts/ci/verify_admin_visual.py --resume-presentation .run/gopulse-p1401-fec96021f75a`
   - 证据 `.run/gopulse-p1401-8dc16fcc7dce/`。
   - `dashboard-browser.log`：`npx playwright test e2e/dashboard.spec.ts -g 'real overview, catalog rule lifecycle'`，**1 passed**。真实三源规则创建/修改/409 冲突/启停、当前告警、关闭详情、用户角色、bootstrap 防降级和审计通过。
   - `visual-browser.log`：`npx playwright test e2e/admin-visual.spec.ts`，**1 passed**；三尺寸八路由，Logs/Alerts 详情及关闭焦点、导航、局部表格、六插件选择、日志过滤/空结果/分页、Metrics 查询/范围、skip link、返回社交和退出通过。
   - `partial-browser.log`：`npx playwright test e2e/dashboard-partial.spec.ts`，**1 passed**。仅停止本轮自有 VictoriaMetrics，验证受影响分区 unavailable/degraded 与告警分区独立；不扩展为全产品故障矩阵。
   - 双 bundle 的内部地址/token 标识/参考图示例地址/测试 Secret 扫描通过。
3. 最终两页补验：`python3 scripts/ci/verify_admin_visual.py --resume-presentation .run/gopulse-p1401-8dc16fcc7dce --changed-pages`
   - 证据 `.run/gopulse-p1401-f83d282c31b2/`，候选版本 **1.14.2**。
   - 截图对照发现 Exporter 顶部缺状态统计，补齐；Metrics 标题改为最后成功响应指标，防止未提交选择误标旧曲线。两处变化是补验原因，不是扩展到无关范围。
   - `changed-pages-browser.log`：**1 passed**。三尺寸 Metrics/Exporter、真实已安装数量、六插件选择，以及“选择新指标但不提交仍显示原响应标题，提交后才切换”通过。重新扫描最终双 bundle 通过。
   - 最终 `result.json` 明确 `changed_pages_only` 及 `reused_receipts`；其他页面、权限、规则和 partial 使用上一轮未受变化影响的成功回执，不冒称全部重跑。前两轮尚未升级版本元数据，按其真实 `1.14.1` 工作树候选记录保留，未回写证据。

这些分阶段实际命令共同覆盖计划要求的 `admin-frontend.spec.ts`、`dashboard.spec.ts`、`dashboard-partial.spec.ts`。由于 partial 需要不同真实依赖状态、降权用例修改测试账号，未将三份 spec 强行放在同一环境状态连续执行。成功且无相关变化的项目不重复运行。

## 5. 视觉、响应式与可访问性证据

最终汇总目录：`.run/gopulse-p1401-f83d282c31b2/screenshots/`。

- 五页 `{dashboard,metrics,logs,alerts,plugins}-{1440,768,390}.png` 共 15 张。
- 视口为 **1440×1000、768×1024、390×844**，保存 full-page PNG；图片高度可大于视口。
- Dashboard/Logs/Alerts 沿用前两轮一次成功采集的图片，最终目录复制保留；Metrics/Exporter 因实际改动重新采集，其前序图片保留在旧证据目录，没有删除或伪装成同一候选。
- 桌面五页分别人工查看并与第二组原图对照；另查看 768 的概览和 390 的日志，三尺寸交互由浏览器检查全部执行。超长截图使用已安装 Chromium canvas 对已有 PNG 裁切首屏查看，没有新增图片依赖或重采应用截图。
- Logs/Alerts 截图是在选择详情后采集，focus 的自动滚动会让 full-page 中 sticky 侧栏出现滚动偏移；这不是顶部空白占位布局。关闭详情返回原行按钮的焦点，三尺寸均通过。
- 浏览器逐路由断言文档 `scrollWidth <= innerWidth`；宽表局部滚动。390 导航展开/关闭，列表详情关闭、键盘焦点、skip link 到 main 通过。所有新操作使用原生 button/select/input，详情有可访问名称、Escape 关闭和文本状态。

## 6. 完成结论与边界

本批固定门禁及直接验收通过，无未解决阻断失败；受管元数据统一 `1.14.2`。实现提交在 `develop/1.14.2` 创建并按用户要求推送，不代替合并 main，也不宣称 Phase 17 整体 Milestone 4 完成。

已知非阻断边界：保留现有规则默认入口；概览不是历史趋势接口；Logs/Alerts 统计为已加载范围；示例验收环境仅 Redis 插件实际安装；真实数据的长文本与行数改变页面高度；参考图中不受 API 支持的功能不呈现为可用按钮。截图、Compose 回执保留在本地 `.run/`，不作为生产资源提交。未开展 Migration、运行时收口、跨架构、Kubernetes 或全面韧性验收。

## 7. 用户复核后的同分支视觉补正（2026-09-15）

### 7.1 纠正标准与实际实现

用户指出首轮平台与参考图并不相同，经重新查看实际截图确认问题成立。首轮没有充分还原图标、统计卡、表格密度、操作位置和详情结构；不能把这些视觉欠缺归因于 API。继续当前分支，未新建批次、未升级 VERSION、未修改用户原图。

本轮直接修改：

- 新增 `components/AdminIcon.vue`：统一描边 SVG 图标及插件类型图形，不再用字符代替导航图标。所有 SVG 是代码原生资产，不使用参考图截图作运行时 UI。
- 新增 `components/AdminStat.vue`：彩色圆角图标块、标签、主数值和统计口径的四卡结构，五页复用。
- 新增 `components/AdminTrend.vue`：带纵轴数值、横轴时间、网格、图例和多序列色彩的真实 SVG 曲线；空采样明确不可用。不会插值制造业务数据；短时间窗显示秒级坐标。
- `components/AdminLayout.vue`、`styles.css`：重做 224px 深蓝侧栏、58px 白色顶栏、品牌、导航分组/选中态、字号/间距、卡片和局部滚动表格。移除前两版冲突的 admin 展示覆盖层；保留普通管理页所需基础样式。菜单图标为装饰，不伪造搜索和通知功能；手机使用真实导航按钮。
- `DashboardView.vue`：主视区变为四卡 → 横跨的组件表 → 双趋势图 → 告警/事件双栏；额外三分区放入可展开的“更多运行快照”，仍保留原六分区数据与错误边界。通过现有 Metrics API 查询 `gopulse_backend_outbox_pending`、`gopulse_monitor_event_queue_length`，提供实际存在的双趋势，不把“overview 无曲线”当作无法形成双图布局的理由。两个请求独立处理成功/失败，离开页面取消请求；不增加后端接口。
- `ObservabilityMetricsView.vue`：统计四卡、主多序列图、最近采样/Series 双栏和查询信息；不再为每个序列堆叠大图和样本表。保留固定指标、时间窗、刷新、响应绑定标题和真实单位。
- `ObservabilityLogsView.vue`：横向主筛选条，高级精确字段折叠；列表增加 Request ID 列和紧凑时间，桌面默认显示首条详情、手机由用户选择。所有长文保留详情及 title；宽表局部滚动、列表高度受控。四卡是真实已加载数量、error/warn 和服务数量。刷新失败清除详情并保留旧列表的测试保持通过。
- `AlertsView.vue`：默认进入 current，规则仍通过 rules 页签直接可达；统计卡、列表内筛选、选中态、详情头部状态、基本信息与标签芯片按参考层级调整。严重程度仍独立于生命周期，未新增 Warning 状态；筛选改变清除选中详情。
- `ObservabilityExportersView.vue`：左侧紧凑插件图标/名称/状态/版本列表、真实本地名称/状态筛选；右侧同一面板内顶部生命周期操作、锚点导航、基本信息网格、中文配置标签与底部连接测试/配置按钮；包更新通过明确更新按钮展开，旧版升级仍自动显示。没有回显 Secret，也没有添加明文显隐。配置字段、确认、文件校验及回滚合同保持原有实现。
- 时间展示调整为中文 24 小时制，避免 AM/PM 把日志行挤成双行。

### 7.2 直接测试调整与验证边界

- `views/DashboardView.test.ts`：新增一例真实趋势返回与另一趋势失败的独立展示；原摘要与六分区测试保留。仅新增这一项测试，无覆盖率扩张。
- `services/management.test.ts`：告警默认页改 current 后，原规则 DTO 测试显式切换 rules，再验证非法变更响应被拒绝。
- `frontend/e2e/dashboard.spec.ts`：组件卡定位改为表格；规则操作显式切换 rules；停止规则后的历史详情按真实被停止的规则名称选择，不能假定它始终排第一。
- `frontend/e2e/dashboard-partial.spec.ts`：展开更多运行快照后检查折叠分区的独立失败，保持原 partial 检查。
- `frontend/e2e/admin-visual.spec.ts`：要求概览两条实际曲线、桌面移动菜单隐藏、五页三尺寸、截图前回到顶部；增加新插件筛选的匹配/空结果/恢复与状态筛选检查。
- `scripts/ci/verify_admin_visual.py`：增加两条概览趋势的真实采样前置条件。沿用真实 Compose 和原门禁，不使用 API mock。

共享壳层、默认告警路由和概览请求/图表都已变化，因此本轮原浏览器回执不能代替这些受影响行为的验证。本轮固定门禁针对新工作树重新执行。没有扩展 Backend 测试、依赖源码排查、Migration 或全产品韧性门禁。

### 7.3 实際执行与失败修正

1. 最小检查 `(cd admin-frontend && npx vitest run src/views/DashboardView.test.ts src/views/ObservabilityExportersView.test.ts src/views/ObservabilityLogsView.test.ts)`：3 文件、6 tests passed。
2. 首次固定管理端测试和 Compose 镜像构建同时发现：旧规则测试默认请求 rules，页面已默认 current，导致 DTO 校验不匹配。修正该测试显式进入 rules，不修改安全校验。失败构建证据 `.run/gopulse-p1401-57a9d9668bee/build.log`，未启动产品。
3. `python3 scripts/ci/verify_admin_visual.py`，证据 `.run/gopulse-p1401-380dc08920e6/`：管理员 4 passed、dashboard 2 passed、visual 1 passed、partial 1 passed，双 bundle 扫描通过。随后实际逐张查看五页截图，发现桌面菜单误显、统计图标颜色被旧样式覆盖、时间换行和小图坐标字号过小；**这一轮功能通过仍不视为视觉完成**，修正明确问题后进入最终候选。
4. 最终候选本地固定命令：
   - `(cd admin-frontend && npm test && npm run build)`：11 文件、41 tests passed；vue-tsc、tsc、Vite 构建通过。回执 `/tmp/p1702-fidelity-final-admin.log`。
   - `./scripts/test-frontends.sh`：普通前端 18 文件/65 tests、管理前端 11 文件/41 tests 通过，双类型检查和构建通过。回执 `/tmp/p1702-fidelity-final-frontends.log`。
   - `python3 scripts/ci/validate_versions.py`：通过，仍为 1.14.2。
   - `python3 scripts/ci/validate_branch.py --branch develop/1.14.2 --base-ref origin/main`：通过。
   - `git diff --check`：通过。
5. 最终候选浏览器 `python3 scripts/ci/verify_admin_visual.py`，证据 `.run/gopulse-p1401-c155a6dd67d5/`：管理员 **4 passed**、dashboard 本人降权用例通过，但业务用例误选了历史列表第一行（恰好为另一个仍在 firing 的规则），导致关闭原因断言失败。将定位修正为实际停止的规则名称；没有修改生产代码，也没有更改排序/告警数据来迎合测试。
6. 同候选补跑：`python3 scripts/ci/verify_admin_visual.py --resume-presentation .run/gopulse-p1401-c155a6dd67d5`，证据 `.run/gopulse-p1401-e39aeef4d783/`。
   - `dashboard-browser.log`：失败的业务用例 **1 passed（23.4s）**，按真实目标规则验证关闭原因；不重复成功的管理员和本人降权用例。
   - 视觉用例实际完成全部 24 个路由/尺寸组合、15 张截图、详情/关闭/焦点、插件名称筛选的匹配/空结果/恢复，随后在 `getByLabel('插件状态', {exact:true})` 超时。失败快照明确存在名为“插件状态”的 combobox；修正为 `getByRole('combobox', {name:'插件状态', exact:true})`。不是产品控件缺失，不改生产代码或标签来迎合选择器。
7. 仅补剩余控制与 partial：
   ```bash
   python3 scripts/ci/verify_admin_visual.py \
     --resume-presentation .run/gopulse-p1401-c155a6dd67d5 \
     --completed-dashboard .run/gopulse-p1401-e39aeef4d783
   ```
   - 最终证据 `.run/gopulse-p1401-b8624b469e06/`。
   - `visual-browser.log`：**1 passed**。跳过已完成的布局/截图循环，继续验证插件筛选、日志精确过滤/空结果/分页、Metrics 范围查询、skip link/主区焦点、返回社交和退出。
   - `partial-browser.log`：**1 passed**。停止本轮自有 VictoriaMetrics 后，受影响分区 unavailable/degraded、告警分区 healthy 且计数仍可读；这是原计划固定 partial，不是新增韧性矩阵。
   - 最终双生产 bundle 内部地址/token 标识/示例地址及测试 Secret 扫描通过。
   - `result.json` 记录版本 **1.14.2**、候选基线 `a242855` 加工作树、Linux `x86_64`、三个视口，以及明确的 `reused_receipts`、`reused_dashboard_and_layout_steps`；不声称最后一轮重跑了所有项目。
   - `runner.log` 包含实际 PASS 和归属清理结果。所有轮次均只清理自有资源，保留预存容器/网络/卷。

最终本地回执已复制到 `.run/gopulse-p1401-b8624b469e06/{admin-unit-build.log,frontends.log}`，不只依赖 `/tmp`。本轮固定门禁完成，无尚未解决的必需测试失败；已通过且未受后续代码变化影响的项目没有再次运行。

### 7.4 最终视觉对照（与功能门禁分开）

最终实际截图：`.run/gopulse-p1401-c155a6dd67d5/screenshots/{dashboard,metrics,logs,alerts,plugins}-{1440,768,390}.png`，共 15 张。由 `e39aeef4d783` 运行的相同最终产品候选采集到既定续跑目录；`b8624b469e06/result.json` 指向此目录。不是首轮 `a242855` 的旧图，也没有用修图替换产品画面。

- 桌面逐张实际查看五张 PNG，并与第二组五张基准对照。桌面移动菜单误显、图标颜色覆盖、时间折行等首轮问题已消除；截图采集前滚回顶部，旧版 sticky 导航偏移问题不再混淆对照。
- 768×1024、390×844 的五页顶部和底部以已有 PNG 制作联系图人工查看（`/tmp/p1702-fidelity-{768,390}-contact.png`），不重新采集应用或改变原图。确认手机导航、统计卡、查询、详情和配置表单未挤出页面，宽表局部滚动；浏览器已逐页断言 document 宽度无溢出，详情关闭能返回原行焦点。
- 图片为 full-page，不将文件高度当作 viewport 高度。真实数据和必要安全说明影响页面长度，不宣称逐像素相同。

| 页面 | 最终还原的视觉结构 | 保留并明确说明的差异 |
| --- | --- | --- |
| 系统概览 | 四张带图标摘要、横跨组件状态表、两张有坐标的真实趋势、告警/事件双栏 | 现有组件是 GoPulse 六组件，不冒充图中的基础设施列表；曲线为现有 Backend 待发送消息与 Monitor 事件队列，不伪造 CPU/内存或 HTTP 趋势；事件仍为窗口计数并提供详情入口。 |
| Metrics | 横向查询、彩色四卡、统一主图与图例、双栏辅助区域 | 当前 API 按单个固定指标查询，未增加任意实例/PromQL；辅助区域用真实最近采样代替缺失的 CPU 使用率；实际一个序列就绘制一个序列，不填充示例三条。 |
| Logs | 横向精确检索/筛选、四卡、紧凑表和右侧详情 | 搜索是 Request ID 精确匹配而非全文/Trace；使用真实安全字段、游标加载更多而非虚假的总页数；详情不回显原始文档。 |
| Alerts | 当前告警入口、四卡、列表内筛选、详情标题/状态/标签层级 | 保留 rules/current/history 的实际语义；Warning 是严重程度，Closed 替代未提供的静默统计；没有不存在的确认/静默/关联趋势按钮。真实仅三条记录时列表不补示例行。 |
| Exporter | 类型 SVG 图标、紧凑左列表、顶部真实启停/更新操作、右侧基本信息与紧凑配置 | 仅 Redis 实际安装，其余未安装，不伪造版本/时间/状态；锚点为实际信息/配置/指标，未添加历史版本或日志空页签；Secret 不回填、不提供明文显隐。 |

视觉改动已落实到上述可见结构，而不是用“API 差异”解释原有图标、留白或卡片结构欠缺。剩余可见差异与真实 DTO、可用操作、安全/响应式约束有关；统一描边图标是本地实现，不宣称与参考图中的第三方品牌美术逐像素一致。

### 7.5 本轮交付

最终代码、固定门禁和视觉证据均已完成；继续同一 `develop/1.14.2` 分支提交本次补正，产品版本仍为 **1.14.2**。不修改 Backend/API/Schema/Compose/release 合同，不扩展 Phase-17-03 工作，不创建 PR 或自动合并 main。用户原始 `Management_Center/` 保持未跟踪且不纳入提交。
