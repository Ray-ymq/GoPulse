# Phase-17-02：管理中心前端二次展示更新实施记录

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
