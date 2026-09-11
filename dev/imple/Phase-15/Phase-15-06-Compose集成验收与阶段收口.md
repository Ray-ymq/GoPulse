# Phase-15-06：Compose 集成验收与阶段收口实施方案

> 当前状态：待实施。本文档定义 Phase 15 第六个执行批次及阶段收口的范围与验收合同；目标版本 `1.12.6`、开发分支 `develop/1.12.6` 和执行顺序以 `Phase-15-总实施方案.md` 为准。

## 1. 批次目标

不新增产品能力，把 Phase-15-01 至 05 的单批闭环组合成一次完整 Compose 产品验收，并完成 Phase 15 状态、版本、记录与 Phase 16 交接：

```text
Phase 14 role/data volume upgrade + clean install bootstrap
  → user / super_admin + protected bootstrap + audit
  → one-origin user Frontend / admin Frontend
  → real dashboard + existing Metrics/Logs/Events/6 plugins
  → real Metrics/Logs/Events alert trigger/continue/recover
  → VM/ES/evaluator/Monitor representative failures and recovery
  → Phase 13/14 regression + strong-ownership cleanup
  → Phase 15 version/document closure
```

本批只为固定阶段验收接线，或修复真实验收暴露的最小阻断问题。不做默认独立 Review、依赖审计、覆盖率活动、新告警语法或 Phase 16 产品化。

## 2. 前置条件

- Phase-15-01 至 05 全部已合入最新 `upstream/main`，根、用户 Frontend 和管理 Frontend 版本为 `1.12.5`，五份实施记录与真实提交一致。
- fetch 后从最新 `upstream/main` 创建 `develop/1.12.6`。
- 逐项读取总方案第 17 节和前五批记录，只列出尚无有效证据的跨批项；已成功且输入未变的 package 检查不重跑。
- 保存现有 Docker containers、networks、volumes、Compose projects、宿主端口与 Git 状态快照；不停止、删除或复用非本次强归属资源。
- 准备两类受控数据输入：包含两个旧 `admin` 与活动用户数据的 Phase 14 升级 fixture，以及空库/新插件卷的 clean-install 路径。

### 2.1 最终验收输入，不重新设计

- 角色迁移/bootstrap 严格使用 Phase-15-01 已实现契约，不在收口改为环境变量硬编 admin 或首用户自动提权。
- 规则、窗口、cutoff/freshness、unknown、closed/recovered 与重复抑制严格使用 Phase-15-02/03 契约，不在最后一批为加快验收增加后门或直接写 incident。
- 两个 Frontend、`/admin/` base、同源 Cookie、overview 六区与旧路由重定向严格使用 Phase-15-04/05 输入，不临时合并 bundle 或使用第二 host origin。
- 三源真实触发必须经 Phase 14 指标/日志/事件生成与存储链路，不直接写 VictoriaMetrics、Elasticsearch 或 MySQL alert tables。

## 3. 实施范围

### 3.1 固定 Phase 15 Compose 入口

- 扩展 `scripts/verify-compose.sh` 为 `--phase15` 模式，或提供实施记录注明的等价唯一入口；复用现有随机 project、动态宿主端口、snapshot、ownership validation、trap cleanup 与 `--self-test` 边界。
- 验收镜像包含两个 Frontend 的 browser 流程与受控 API/内部检查；宿主只访问唯一 frontend origin，不直连 admin-frontend、Backend 或基础设施。
- 验收中为 Cookie、JWT、内部 token、插件凭据与特征串生成随机隔离值，不打印、不提交、不带入日常卷。

### 3.2 升级与 clean install

- 从包含两个旧 `admin`、普通用户、业务数据、六插件 desired state、可观测历史的 Phase 14 强归属 fixture 升级，验证角色转换、最小 ID bootstrap、旧会话和所有其他数据不变。
- 对升级 fixture 重跑 migration/生命周期一次，证明不重复 bootstrap/audit/alert state，不删卷或重建插件状态。
- clean install 在空库先启动社交注册，确认管理路径显示 setup 不可用，再使用规范 operations 命令声明唯一 bootstrap，其后完成管理登录。
- 升级和 clean install 均不借助直接 SQL 赋权作为产品操作；直接 SQL 只允许用于验收中读取不变式或受控外键拒绝证据。

### 3.3 双角色与双 Frontend 浏览器矩阵

- 使用 bootstrap super_admin、非 bootstrap super_admin 和普通 user 三个真实账号，验证登录默认落点、同源 Cookie、直达 deep link、页面刷新和旧路由重定向。
- 普通 user 直达每类管理页不发起管理数据请求，对 overview、alerts、users、audit、observability 与 plugins API 的直接请求均 403。
- bootstrap 降级被 Backend 409 与外键/事务不变式拒绝；非 bootstrap 账号先提升再降级，降级后同 Cookie 管理请求 403、管理 DOM 清理、社交会话继续。
- 超级管理员从默认大屏进入 Metrics、Logs、Events、Plugins、Alerts、Users 和 Audit，页面只通过 Backend 请求数据。

### 3.4 真实三源告警闭环

- 在管理 Frontend 中从 catalog 创建 Metrics、Logs、Events 各一条规则，记录精确 selector/reducer/operator/window/for/revision，不使用隐藏 API 字段。
- Metrics 通过强归属的真实目标/数值异常越界，Logs 通过真实 Backend 请求/业务流生成，Events 通过真实 Monitor 插件/采集活动生成，三者均经正式存储与 Backend adapter 评估。
- 每条规则等待至少三次真实 true 评估，确认只有一条 incident/一次 trigger 审计、last-triggered/evaluation-count 更新；在 firing 时替换 Backend 仍不重复。
- 恢复 metric 真实数值，并等待 log/event 滑出窗口，三条原 incident 转 recovered；当前页清空、历史保留规则/严重度/对象/首次/最近/恢复字段。
- 另对一条 firing 规则执行 disable 或 update，确认历史为 closed reason 而不是 recovered，并且有对应审计。

### 3.5 大屏、审计与脱敏

- 在正常状态核对大屏六组件、六插件、六个 key metric、logs/events 三严重度计数和 alerts 汇总与真实专项 API/运行状态一致。
- 在升级、角色调整、规则 CRUD/转移和六类插件代表性操作后，审计页能以稳定 cursor 查到正确 actor/action/resource/phase/outcome。
- 使用包含特征串的密码、token、插件 Secret/候选 username 和上游错误后，扫描 API DTO、MySQL alert/audit 公共字段、结构化日志、Events、metrics labels、两个 Frontend DOM/bundle、镜像元数据和验收输出，均不含该特征串。

### 3.6 代表性故障矩阵

只选择能证明不同边界的故障，不对 32 条规则、12 个观测 target 或全部页做全排列：

1. 停止 VictoriaMetrics：Metrics 规则 unknown/stale、大屏 Metrics 相关区降级，Logs/Events 规则、MySQL 管理数据与社交业务继续；恢复后从新样本继续，不伪造停机期数据。
2. 停止 Elasticsearch：Logs/Events 规则 unknown/stale、对应大屏区降级，Metrics 规则、角色/规则/历史与 MySQL 业务继续；停机不以 count=0 伪恢复。
3. 以 `ALERT_EVALUATION_ENABLED=false` 替换 Backend：已 firing 不改写，规则/历史与两 Frontend 可用，社交 readiness 不变；再开启后从 MySQL 继续。
4. 停止一个非存储插件目标或使一个 Exporter 退出：沿用 Phase 14 指标/事件/插件状态语义触发相关观测，其他插件、大屏其他区和社交业务继续；恢复后状态收敛。

每个故障必须恢复并核对产品状态后再进入下一个。若因共享依赖实际停机影响已知业务路径，按 Phase 14 真实依赖边界记录，不伪称“所有功能一直可用”。

### 3.7 Phase 13/14 与平台合同回归

- 使用至少两个普通用户完成资料/用户搜索、关注与 Following、收藏、发帖、评论、点赞、编辑后缓存/搜索收敛、通知、删除/墓碑/防复活代表性主路。
- 使用 super_admin 验证六插件列表、一个代表性配置/启停、六插件 + 六组件 Metrics、Logs 和 Events 查询；既有单实例、Secret 和固定 catalog 契约不回归。
- 验证两个 Frontend image 和全部自研镜像版本/revision、numeric user、read-only filesystem、内部端口/网络、无 source map/工具链/凭据、SIGTERM 与强归属清理。

### 3.8 阶段文档收口

- 仅在固定门禁全部通过后，将根、两个 Frontend 与受管版本元数据更新为 `1.12.6`，创建同名实施记录，并更新 Phase 15 总方案的实际状态/证据。
- 核对六份 split plan 均有同名 log，版本、分支和合入记录符合真实 Git 状态；未合入批次不写成已合入。
- 记录 Phase 16 可依赖的双 Frontend、角色、告警、审计、Compose 和升级起点，以及真实阻断/非阻断后续项。

## 4. 不在本批范围

- 新告警 source/reducer/operator/window、外部通知、ack/silence/escalation 或自动修复。
- 第三种角色、新用户管理功能、RBAC 或 bootstrap 转移。
- 第七类插件、更多 component/metric、同类多实例或多目标。
- 默认独立 Review、严重性报告、依赖升级、覆盖率补齐或机会性重构。
- macOS/Windows 真实宿主、多架构镜像、统一产品生命周期、升级/备份/恢复产品化或 Kubernetes。
- 对 32 条规则、12 个 target、全部路由或全部依赖故障做全排列；本批只执行边界覆盖明确的代表性矩阵。

## 5. 建议实施顺序

1. 核对总方案验收与前五批证据，只列出未满足的跨批项。
2. 扩展 Phase 15 Compose 固定入口与安全 self-test，先验证不触及非归属资源。
3. 完成 Phase 14 升级、clean install bootstrap 和双角色/双 Frontend 浏览器矩阵。
4. 创建三源规则，完成真实 trigger/continue/restart/recover/close 与大屏/审计核对。
5. 按第 3.6 节串行运行代表性故障，每个边界恢复后再进入下一个。
6. 运行 Phase 13 业务、Phase 14 可观测/插件、镜像/网络/信号/脱敏和强归属清理回归。
7. 全部门禁通过后更新版本至 `1.12.6`，完成 log 和总方案收口证据，提交并停止。

## 6. 预计直接影响文件

- `scripts/verify-compose.sh`、Compose/observability 验收编排和 Phase 15 browser scenario
- `scripts/verify-role-management.sh`、`scripts/verify-alerts.sh`、`scripts/verify-admin-frontend.sh` 仅用于接入最终门禁或修复实际阻断
- `deploy/compose.yaml`、acceptance image/config、`.env.example` 或直接运行说明，仅当最终验收暴露必要接线缺失
- 真实阻断修复直接涉及的最小生产文件与测试
- `VERSION`、两个 Frontend package/lockfile 与受管镜像/环境版本元数据
- `dev/imple/Phase-15/Phase-15-总实施方案.md`，仅用于实际完成状态和证据
- `dev/logs/Phase-15/Phase-15-06-Compose集成验收与阶段收口.md`

如验收未暴露生产阻断，本批不修改前五批生产实现或新增状态边界测试。

## 7. 批次与阶段验收标准

### 7.1 批次完成条件

- 第 3.2 至 3.7 节的升级/clean install、角色/双 Frontend、三源告警、大屏/审计/脱敏、故障恢复、Phase 13/14 与平台回归全部通过。
- 六份 split plan 均有同名真实实施记录；记录中的命令确已执行，偏差与未执行项未被伪写为完成。
- 根、两 Frontend 与受管版本元数据为 `1.12.6`，分支为 `develop/1.12.6`；最终提交只包含本批接线、必要阻断修复、验收、记录和阶段收口文件。
- 所有强归属测试资源已清理，测试前存在的容器、网络、volume、端口与源码工作树不变。

### 7.2 Phase 15 完成与停止条件

Phase 15 仅在总方案第 17 节全部满足、6 个执行批次已按顺序合入 `upstream/main`、主线根版本为 `1.12.6` 且无阻断问题时完成。本批本地验收通过但尚未合入时，应如实写“待合入”，不提前宣称主线阶段完成。

达到条件后停止。外部通知、细粒度 RBAC、更多大屏、跨平台产品化、升级/备份/恢复与 Kubernetes 只作后续项，不延长本阶段。Milestone 4 仍到 Phase 17 才最终收口。

## 8. 固定验证命令与回归范围

已成功且相关输入未变的 package 测试从前五批实施记录引用。本批最终 diff 固定运行：

```bash
bash scripts/verify-compose.sh --self-test
bash scripts/verify-role-management.sh --self-test
bash scripts/verify-alerts.sh --self-test
bash scripts/verify-admin-frontend.sh --self-test
bash scripts/verify-compose.sh --phase15

python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/1.12.6 --base-ref upstream/main
git diff --check
git diff --cached --check
```

- `--phase15` 必须包含两个 Frontend 的 tests/typecheck/build 制品、真实 browser/API 验收、自研镜像构建、完整 Compose、升级/clean install、固定故障矩阵和强归属清理；若实际脚本不包含，必须在记录列出并运行等价命令。
- 若本批修改某个生产 package 或 Frontend view，先重跑该直接 package/view tests，再运行最终 `--phase15`；未修改 package 不因收口重复全量测试。
- 任何门禁未运行、失败或清理不完整都是阻断项，不得标记批次或阶段完成。
- 提交后补充运行 `git diff --check upstream/main...HEAD`，并用真实 Git 状态更新合入描述。

## 9. 实施记录与 Phase 16 交接

完成前创建 `dev/logs/Phase-15/Phase-15-06-Compose集成验收与阶段收口.md`。

记录必须包含实际 Compose project/端口与前后资源快照、Phase 14 升级与 clean install bootstrap、三账号/双应用矩阵、三源规则与 incident/audit 行数、大屏、故障矩阵、Phase 13/14 回归、镜像/脱敏/清理、全部命令与结果、偏差、限制和真实合入状态。交给 Phase 16 的内容至少包括：

- 两个独立 Frontend 工程/镜像、同源 `/admin/` 路由、安全 redirect 和实时数据库角色分流。
- `user|super_admin` 最终角色、bootstrap 保护、按 ID 调整与管理审计契约。
- Metrics/Logs/Events 受限告警 catalog、状态机、重复抑制、unknown/closed/recovered 与持久历史。
- 完整 Compose 中的大屏、六插件、六组件、业务和局部故障边界。
- Phase 16 仍需独立完成多架构制品、Linux/macOS/Windows 真实宿主、共享生命周期、从 `1.9.4` 升级与最小一致备份/恢复；不得从 Phase 15 证据直接宣称已完成。
