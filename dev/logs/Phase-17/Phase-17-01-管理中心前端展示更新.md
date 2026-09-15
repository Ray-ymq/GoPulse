# Phase-17-01：管理中心前端展示更新开发记录

## 1. 批次与范围

- 日期：2026-09-15（Asia/Shanghai）。
- 目标版本：`1.14.1`；开发分支：`develop/1.14.1`。
- 开工已 fetch `origin` 和 `upstream`，从 `upstream/main` 的 `9880dbb` 创建开发分支；起始完成版本 `1.13.6`。
- 根目录已有未跟踪 `Management_Center/` 未修改、未暂存。设计输入使用已跟踪的 `dev/imple/Phase-17/assets/Management_Center/` 四张 PNG。
- 只修改 Admin Frontend 展示、直接测试、版本元数据、使用说明及本记录。未修改 Backend/API、权限、数据库、消息、Compose 拓扑或任何 PowerShell 脚本。

## 2. 完成实现与真实数据映射

### 共享管理壳层

`AdminLayout.vue` 改为深蓝桌面侧栏、品牌区、三个导航分组、白色顶部工具栏和浅色内容背景。完整保留八个路由、原兼容重定向、当前账号、返回社交、退出、跳至主要内容和服务端授权后的挂载条件。退出操作增加重复提交保护。

窄屏导航通过按钮展开，提供 `aria-controls` / `aria-expanded`，选择路由后收起；当前链接使用 Vue Router 的 `aria-current` 和 exact-active 样式，避免大屏链接在子路由同时高亮。Admin-only token、表单/表格/卡片/状态文本统一；长字段换行，平板与手机降列，不以隐藏业务操作处理溢出。遵从减少动态偏好，无新图表/UI 运行时依赖。

### 四页与视觉基准逐页对照

| 视觉基准 / 页面 | 实际使用与 DTO 映射 | 对照差异及原因 |
| --- | --- | --- |
| `管理大屏.png` | 同样的侧栏、标题/刷新、四摘要、组件/告警、指标/事件分区。六个原 overview section 全部保留；组件/日志/事件/插件条目以状态与快照表达，采样详情可展开。 | 组件 DTO 是 backend 等六个业务进程，不是图片中的 MySQL 等六个基础设施；无延迟值，不画延迟条。overview 无历史序列，改用关键指标快照与 Metrics 入口。事件 DTO 只有级别计数，不伪造逐条最近事件。保留日志与插件两分区使桌面全页更长。告警采用真实计数字段而非混合当前/恢复时间窗口的环形比例。 |
| `Metrics.png` | 固定目录/范围筛选、四摘要、真实 SVG 趋势、最近五个采样点。图形 X 轴按采样时间，Y 轴按本时序 min/max 缩放；保留精确数值与文字描述。 | 真实默认 Redis up 序列为常量水平线，而不是示例 connected-clients 波形；无造点。保留原 DTO 类型/单位/步长/更新时间，未增加没有后端合同的可编辑数据源过滤器或 instance/job 示例值。真实时间字符串会自动换行。 |
| `Logs 页面.png` | 标题、完整筛选、级别摘要、白色日志流容器与浅底记录卡片、安全 metadata 标签。 | 保留八个既有精确筛选字段，筛选栏比图片高；request/error 分开，不改变语义。摘要是“已加载日志（非窗口总量）”，不冒充窗口聚合。真实页大小为 50 条且 metadata 更多，全页截图更长，不截掉数据或分页功能。 |
| `Exporter 管理.png` | 六插件目录、选中边框、配置/状态双列、下方安装包更新区域。配置字段仍由现有 schema 决定，密码不回填。 | 环境只有 Redis 已安装，其余显示未安装，不伪造六插件 running。显示真实 GoPulse 插件名称/版本；配置 host 不预填内部地址，按钮和原生文件输入保留可访问行为。安装/更新安全语义未变。 |

参考图的颜色、侧栏宽度、顶栏、浅底、圆角边框、分组层级及蓝色主操作已落实。保留文本状态、较大触控目标、完整安全字段和真实数据显示，因此不做逐像素复制。侧栏不显示缺少实时依据的“Platform Healthy 6/6”。

Events 原展示直接继承新壳层；Alerts/Users/Audit 仅应用共享表单、按钮、卡片 primitive，不改业务状态机或请求合同。

## 3. 实际修改文件

- `admin-frontend/src/components/AdminLayout.vue`：侧栏/工具栏、账号、响应式导航和退出防重复。
- `admin-frontend/src/components/MetricTrend.vue`、`MetricTrend.test.ts`：真实坐标 SVG、可访问图形描述、空值与负值比例直接验证。
- `admin-frontend/src/styles.css`：仅管理端视觉 token、布局与响应式样式。
- `admin-frontend/src/views/DashboardView.vue`、`DashboardView.test.ts`：六分区、摘要、真实快照、unknown 不转为 0。
- `admin-frontend/src/views/ObservabilityMetricsView.vue`：替换不表达真实比例的旧取模 sparkline，增加真实曲线与最近采样表。
- `admin-frontend/src/views/ObservabilityLogsView.vue`：已加载级别摘要、日志流卡片。
- `admin-frontend/src/views/ObservabilityExportersView.vue`：目录选中态、双列配置/状态、安装包区域。
- `admin-frontend/src/views/AlertsView.vue`、`UsersView.vue`、`AuditView.vue`：共享展示 primitive。
- `frontend/e2e/admin-visual.spec.ts`：真实三尺寸/八路由、四页截图、筛选/空结果/分页、插件选择、键盘和退出验收。
- `scripts/ci/verify_admin_visual.py`：复用现有独立 Compose 归属/清理实现，建立真实账号、数据及浏览器验收环境，不改变产品 Compose。
- `docs/admin/management-center.md`：使用方式、数据口径、视觉差异和验收入口。
- `VERSION`、`.env.example`、双 Frontend `package.json` / `package-lock.json`：同步 `1.14.1`。
- 本记录。

## 4. 验证执行与证据

### 本地固定门禁

- `(cd admin-frontend && npm test && npm run build)`：初次 8 文件 / 35 测试通过；最终增加两个直接测试文件后 10 文件 / 39 测试通过，类型检查与 Vite 构建通过。最终输出 `/tmp/gopulse-1701-admin-final.log`。
- `./scripts/test-frontends.sh`：通过，普通 Frontend 18 文件 / 65 测试，Admin 当时 9 文件 / 37 测试，双端类型检查/构建通过。输出 `/tmp/gopulse-1701-frontends.log`。随后只新增 Dashboard 两条直接测试，已由最终 Admin 门禁覆盖，未重复普通 Frontend 成功检查。
- `(cd admin-frontend && npx vitest run src/views/DashboardView.test.ts)`：2 测试通过，直接证明真实摘要和 unavailable 不当作 0。
- `python3 scripts/ci/validate_versions.py`：通过，版本元数据一致。
- `python3 scripts/ci/validate_branch.py --branch develop/1.14.1 --base-ref upstream/main`：通过。
- `python3 -m py_compile scripts/ci/verify_admin_visual.py`：通过。
- `git diff --check`：通过；提交前对包含本记录的最终差异再次检查。

### 真实 Compose / Playwright

执行入口 `python3 scripts/ci/verify_admin_visual.py`。使用真实 Linux `x86_64` Docker server、独立 Compose project、测试账号/凭据、真实双 Frontend 镜像与同源 edge；没有浏览器 API mock。

首轮证据 `.run/gopulse-p1401-c0032c1e85e1/`：

- `admin-browser.log`：`npx playwright test e2e/admin-frontend.spec.ts`，4 测试通过。包括管理员兼容路由/刷新、统一登录、cookie/localStorage、401、普通用户不挂载不发管理请求、六插件目录、Redis 停止/启动、真实 Metrics/Logs/Events、数据库降权清除候选密码与 DOM。
- `dashboard-visual-browser.log`：`npx playwright test e2e/dashboard.spec.ts e2e/admin-visual.spec.ts`，dashboard 的 2 测试通过；视觉用例首次因告警页同时存在两个 `nav`，过宽的 `getByRole('navigation')` 触发 strict mode。改为指定“可观测导航”的定位，不改产品代码。
- dashboard 通过项包括六分区、三源告警规则增改/冲突/启停及历史、用户角色、bootstrap 防降级、审计、本人降权和普通用户分流。
- 四页截图已在三尺寸采集，共 12 张；视觉脚本对已有截图不重复采集。桌面四页已逐页人工对照四张设计输入，手机四页已检查，截图差异见第 2 节。

第二轮使用 `--resume-presentation .run/gopulse-p1401-c0032c1e85e1`，证据 `.run/gopulse-p1401-af3f5fade226/`：仅补跑未完成视觉/partial，不重复已通过的管理员和 dashboard 用例。独立新环境中尚未进入时间范围采样点时，视觉用例提前断言 `.metric-value` 导致失败。修正 runner 的真实数据前置条件：等待 Redis up 与 connected-clients 实际可查询采样，不固定 sleep、不伪造响应。产品代码无需修改。

两个失败轮次均已执行归属检查和清理，保留既有容器/网络/卷。最终待完成项继续由同一候选运行，不扩展至备份、Migration、消息可靠性或全产品韧性。

### 截图路径与对照方法

`.run/gopulse-p1401-c0032c1e85e1/screenshots/{dashboard,metrics,logs,plugins}-{1440,820,390}.png`，分别使用 viewport `1440×1000`、`820×1000`、`390×844`，保存全页 PNG 以保留可滚动内容。日志 50 条导致图片较高，人工查看时从原图裁出首屏，不重新采集应用、不把截图加入运行时资源。

截图裁切最初尝试本机 Python Pillow，环境未安装 Pillow；随后用已安装 Playwright Chromium canvas 对既有 PNG 裁切成功，没有添加项目依赖。

## 5. 计划执行说明

- 浏览器固定命令按真实状态分阶段执行，而非把 normal 与 partial 混在同一个环境状态；两个降权用例之间恢复独立测试账号的管理员角色。实际执行的 spec 仍覆盖方案三份既有门禁，另加专用视觉 spec。
- 只为 partial 验收停止本次自有 VictoriaMetrics，证明受影响分区与告警分区隔离；未开展完整 VM/ES/Monitor 故障排列或其他 Phase 门禁。
- 所有镜像由带本批未提交展示差异的工作树构建；证据中的 Git revision 指向开工基线，不冒称已提交发布镜像。通过后本批代码与版本一起提交，正式 release 留给后续批次。
- 原生 PowerShell 冻结、Linux Compose 产品合同和普通用户 Frontend 行为保持不变。

第三轮补跑证据 `.run/gopulse-p1401-55ea7ac7bcc4/`：真实采样前置条件已满足，全部 24 个路由/尺寸组合、导航选中态、窄屏展开、四页真实内容和日志精确过滤/空结果/加载更多已走过；用例随后在 Metrics 原生 select 的精确 label 定位处超时。页面实际 combobox 与目标 option 均存在（失败快照可见），将测试改为按 `combobox` 的可访问名称选择，与浏览器语义一致，不修改页面或选项合同。此前通过的固定门禁不重跑；只补完该失败视觉 spec 和后续 partial。

## 6. 最终验收与完成结论

最终补跑命令：

```bash
python3 scripts/ci/verify_admin_visual.py --resume-presentation .run/gopulse-p1401-c0032c1e85e1
```

最终证据：`.run/gopulse-p1401-064ce58b4438/`。

- `visual-browser.log`：`npx playwright test e2e/admin-visual.spec.ts`，**1 passed（5.7s）**。真实三尺寸/八路由无水平溢出；导航当前态、手机展开/收起、六插件选择、日志精确 Request ID/空结果/恢复/分页、Metrics 指标与范围选择、真实趋势、键盘 skip link 到 main、返回社交和退出全部通过。
- `partial-browser.log`：`npx playwright test e2e/dashboard-partial.spec.ts`，**1 passed**。本次自有 VictoriaMetrics 确实停止后，key_metrics/components/plugins 显示 degraded/unavailable，alerts 仍 healthy，warning_firing 仍可读。
- 双 Frontend 生产 bundle 的内部 URL/管理 token 标识/参考图内部域名及实际测试 Secret canary 扫描通过。
- `result.json`、`results.json`：最终结果 passed，Linux `x86_64`、三种 viewport、候选基线 revision 和复用首轮通过证据均记录。
- `runner.log`：包含最终 PASS 及“Owned resources removed; pre-existing resources preserved”；所有轮次均完成归属清理，测试凭据和 override 文件已移除。
- 首轮管理员 **4 passed**、dashboard **2 passed** 与最终视觉 **1 passed**、partial **1 passed** 共同覆盖本批浏览器门禁；没有重复已成功且产品代码未变的测试。

版本元数据已全部同步 `1.14.1`。本批验收完成，无阻断问题；按仓库规则创建提交并推送 `develop/1.14.1`。未创建 PR、未合并 main、未宣称 Phase 17 总里程碑通过。

已知边界不是缺陷：overview 仅快照与计数、不提供历史趋势/延迟；日志摘要不是窗口总量；演示环境仅 Redis 插件安装运行；真实数据导致的分页长度和文本换行不同于静态基准。其余运行时合同、持久状态可靠性和 Phase 17 最终产品验收交后续批次，不在本批扩展。
