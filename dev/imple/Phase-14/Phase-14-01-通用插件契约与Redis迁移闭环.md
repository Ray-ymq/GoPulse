# Phase-14-01：通用插件契约与 Redis 迁移闭环实施方案

> 当前状态：待实施。本文档定义 Phase 14 第一个执行批次的范围与验收合同；目标版本 `1.11.1`、开发分支 `develop/1.11.1` 和执行顺序以 `Phase-14-总实施方案.md` 的权威分配表为准。

## 1. 批次目标

把只能管理一个 Redis Exporter 的纵向实现收敛为六类官方单实例可共用的安全基座，并以 Redis 作为本批唯一实际启用类型证明全链路：

```text
Phase 13 Redis Manifest v1 / Registry / desired state
  → 通用 Registry + Manifest v2 + 独立 Config/Secret
  → 管理员连接测试 / 安装 / 启停 / 更新
  → Redis metrics 新多来源 envelope
  → Router → Kafka → Marshaller → VictoriaMetrics
  → Backend 固定目录查询
```

批次完成必须证明旧 Redis 状态无需清卷即可迁移，空卷也可安全配置和安装，新契约下的真实 Redis 指标仍能从 Backend 查询，而且代码结构已能在后续批次增加其他固定 ID，不再需要复制一套 Plugin Manager。

## 2. 前置条件

- Phase 13 全部批次已合入 `upstream/main`，根与 Frontend 版本为 `1.10.6`。
- fetch 主远程，从当时最新 `upstream/main` 创建 `develop/1.11.1`，不在 `update` 上实现功能。
- 核对 `monitor/internal/plugin`、metrics collector/envelope、Router allowlist、Marshaller decoder/transformer、Backend exporter/metric query、Frontend Exporter 页、Monitor 镜像嵌入包和 `monitor_plugin_data` 现有布局。
- 保存一份真实 Phase 13 running/stopped Redis 卷升级 fixture，但 fixture 不得包含真实凭据或提交运行时 Secret。
- 保存 Phase 13 Redis metrics 查询、Metrics/Events 和管理页代表性成功证据，用于确认兼容而不重新审计无关业务代码。

## 3. 实施范围

### 3.1 官方 catalog 与 Manifest v2

- 在 Monitor 内建六个固定 `plugin_id/source/target_id/entrypoint/port/schema` 目录；本批只为 Redis 提供可运行包，其他五类只可以明确 `available=false`/未交付状态出现，不伪造已安装。
- 实现严格 Manifest v2，固定 schema/runtime metrics contract、ID/source/kind、SemVer、Linux/当前架构、入口 SHA-256、`/health`、`/metrics` 与 config Schema path/digest。
- 延续 archive 大小、entry 数/类型/路径、重复、链接、权限、staging 和安全根目录验证，只为唯一 Schema 文件扩展明确允许集。
- 包构建输出可复现；为 Redis 生成 v2 包时不将凭据、配置实例或脚本放入 archive。

### 3.2 通用 Registry、配置和 Secret

- 将单常量 ID 改为以官方 plugin ID 为 key 的 Registry/state/runtime map，各 ID 有独立操作串行化、目录、current、release、process record 和 collector lifecycle。
- 只为一个 ID 保存一个当前版本、desired state 和配置修订；不增加 instance/target 列表。同 ID 重复 install 冲突，未知 ID 拒绝。
- 实现非敏感配置与 Secret 的分离、原子持久化；文件权限、父目录/symlink 验证和失败回滚继承现有 storage boundary。
- Redis Schema 只允许受控 host/port/db/timeout/username-or-password 字段，拒绝原始 URL/DSN、未知字段、数组和非安全 container origin。
- Secret 仅在请求和 Monitor 受信运行边界内使用，不返回原值；公共 DTO 只返回是否已配置、修订号和固定摘要。

### 3.3 连接测试与生命周期

- 提供按 ID 的无持久化 connection-test，运行候选 Redis 官方制品的受限 check 路径，设置独立超时且只返回固定成功/安全失败。
- list/get/install/start/stop/update/configure 全部按 path plugin ID 执行；install/configure/update 在提交前重新验证包、Schema、config、Secret、进程健康和首次真实采集。
- start/stop 幂等；update 严格版本上升并保留 desired state；新包或新配置失败恢复旧包、旧配置/Secret 和原状态。
- 将进程意外退出、目标不可达和 metrics 发布失败保持为不同安全错误，并将事件 metadata 中的 plugin ID 从 Redis 常量改为六类固定 allowlist。

### 3.4 Redis v1 迁移

- 对 Phase 13 真实 registry/layout 建立显式、可重入的 v1→通用状态迁移，验证 current/release/digest/process ownership 后才写新状态。
- 从受信 Redis 运行配置中导入唯一 config/Secret，保留 installed/updated time、desired state 和可安全继承的 scrape time。
- running 迁移在新 v2 进程健康且真实采集成功后提交；stopped 迁移不启动进程。中断后重试不重复 release/registry 或丢失状态。
- 损坏边界只将 Redis 标记为可解释的 repair-required/安全失败，不删 volume，不随意停止无法确认归属的 PID。

### 3.5 metrics 新契约与端到端兼容

- 定义 metrics 新 schema/payload，显式区分 `exporter_plugin` 与后续 `component`，包含固定 producer/source/target/scrape status/samples；Redis 继续使用 `source=redis,target_id=redis-exporter-local`。
- Router 同时允许历史 Redis metrics v1 与新 metrics 版本，继续原样写入唯一 Kafka Topic。Logs/Events 契约不改。
- Marshaller 对两版 Redis 执行严格验证并输出相同 Redis provenance series，新契约中的保留标签由 Marshaller 注入，不信任 sample 自带值。
- Backend metric catalog 改为显式带 source/target/producer 契约，保留 Redis 10 families 的原查询和公共 DTO；新 catalog 只读 API 只返回安全展示信息。
- 建立强归属的聚焦验收入口（计划名 `scripts/verify-plugin-metrics.sh`，如实施采用等价名称必须记录），可按 source 验证真实目标到 Backend 查询，并提供不触及 Docker 的安全自测。

### 3.6 Backend 与 Frontend

- Backend 代理将 Redis 常量 ID 改为六类 allowlist，严格验证 catalog、config status、connection result、lifecycle status 与 safe error；无论 Monitor 返回什么，都不向浏览器透传 Secret/路径/进程信息。
- API 继续先认证、再查数据库当前 admin 角色，普通用户的请求在访问 Monitor 前被拒绝。
- Frontend Exporter service/type/validator 支持六种固定 ID 与列表上限，页面提供六卡片、Redis 配置/连接测试/安装/启停/更新操作和安全状态。
- Secret 表单不回填、不序列化到 URL/local storage，操作成功或组件卸载时清空。不改 UserAppShell 或普通用户导航。

## 4. 不在本批范围

- MySQL、RabbitMQ、Kafka、Elasticsearch 或 VictoriaMetrics Exporter 实现与假完成状态。
- 同 ID 多实例、多目标、target CRUD、插件市场或第三方包。
- 自研组件 metrics 端点与指标目录；本批只为后续 schema 保留正确 producer kind。
- 告警、管理端重构、`super_admin`、跨平台产品化或 Kubernetes。

## 5. 建议实施顺序

1. 先固定六 ID catalog、Manifest v2 和 Redis config Schema，用 parser/storage 聚焦测试锁定拒绝边界。
2. 将 Registry、process、collector 和操作锁改为按 ID 独立，保留 Redis 现有成功/回滚语义。
3. 实现 config/Secret、connection-test 和 v1 状态迁移，先验证 stopped/running/中断重试。
4. 扩展 metrics schema、Router、Marshaller 和 Backend catalog，对历史 v1 与新 Redis 记录做代表性兼容测试。
5. 扩展 Backend/Frontend 管理闭环和安全响应验证。
6. 建立聚焦强归属脚本，以真实 Redis 跑迁移、空卷、失败/恢复和 Backend 查询。
7. 更新版本为 `1.11.1`，创建同名实施记录，运行固定门禁并提交。

## 6. 预计直接影响文件

- `monitor/internal/plugin/*`
- `monitor/internal/metrics/*`
- `monitor/internal/httpserver/*`
- `monitor/internal/config/*`
- `monitor/cmd/monitor/*`
- `router/internal/envelope/*`
- `marshaller/internal/envelope/*`
- `marshaller/internal/metrics/*`
- `marshaller/cmd/marshaller/*`
- `backend/internal/exporterplugin/*`
- `backend/internal/metricquery/*`
- `backend/internal/eventquery/*`
- `backend/internal/http/*`
- `frontend/src/services/exporters.ts`
- `frontend/src/types/exporter.ts`
- `frontend/src/views/ObservabilityExportersView.vue`
- 直接相关 Frontend tests/styles
- `scripts/package-redis-exporter.sh`、新聚焦插件验收脚本与直接调用路径
- `deploy/docker/monitor.Dockerfile`、`deploy/compose.yaml`、`.env.example`（仅新契约必需变更）
- `VERSION`、Frontend 版本元数据、相关 README/契约文档
- `dev/logs/Phase-14/Phase-14-01-通用插件契约与Redis迁移闭环.md`

实际 diff 以必需实现为准；不重写无关业务、Logs/Events 或用户端文件。

## 7. 批次验收标准

### 7.1 契约、持久化与安全

- Manifest v2 接受唯一合法 Redis 形状，拒绝未知/重复字段、不兼容契约、错 ID/source/entrypoint/schema/digest/platform 和非安全 archive。
- Registry/config/Secret 原子且分离，Secret 文件权限和安全父目录成立；API、日志、Events、Registry、Frontend DOM 与 metrics 不出现故意注入的 Secret 特征串。
- 同 Redis ID 第二次 install 被拒绝，不存在接受多 target 的请求形状或持久结构。
- connection-test 成功/失败均不创建 Registry/release/process record 或保留候选 Secret。

### 7.2 Redis 迁移与生命周期

- Phase 13 running/stopped 两类状态都可迁移，保留版本/desired state/时间；重试无重复记录，损坏状态不停无归属进程。
- 空 volume 上管理员可从 Redis Schema 配置、连接测试到安装启动；start/stop 幂等，目标不可达后同 Exporter 进程可恢复。
- 新包或新配置试启动失败时，Redis 回到原包/配置/Secret/desired state，没有半更新 current 或遗留子进程。

### 7.3 metrics 链路、Backend 与 Frontend

- 历史 Redis metrics v1 与新 metrics 记录都可被 Router/Marshaller 处理，存储为同 `source/target_id` series；非法 producer/target/label 不写存储且不阻塞下一条。
- 真实 Redis 的 `up=1` 和至少一个实际 INFO 数值经完整链路后由 Backend 查询；只有旧关键词的历史 series 仍可按原契约读取。
- 普通用户所有管理/metric catalog/query 被拒绝；管理员在 Frontend 完成 Redis 代表性闭环，其他五卡片不显示伪造 running 状态。
- Phase 13 代表性登录/帖子/搜索路径、Logs/Events 和 AdminLayout 不回归。

### 7.4 完成条件

以上验收全部通过，无阻断问题，同名实施记录真实完整，根与 Frontend 版本为 `1.11.1`，最终提交只包含本批文件时，Phase-14-01 才可标记完成。其他 Exporter、组件指标或 UI 重构记为后续并停止本批。

## 8. 固定验证命令与回归范围

实施中先跑直接受影响 package；最终 diff 的固定门禁为：

```bash
(cd exporters/redis && go test ./...)
(cd monitor && go test ./...)
(cd router && go test ./...)
(cd marshaller && go test ./...)
(cd backend && go test ./internal/exporterplugin ./internal/metricquery ./internal/eventquery ./internal/http)

(cd frontend && npm test)
(cd frontend && npm run typecheck)
(cd frontend && npm run build)

bash scripts/verify-plugin-metrics.sh --self-test
bash scripts/verify-plugin-metrics.sh --sources redis --migration
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/1.11.1 --base-ref upstream/main
git diff --check upstream/main...HEAD
```

- 聚焦验收必须使用随机强归属 Compose project/端口/volume，同时覆盖旧卷迁移、空卷、真实 Redis、Backend 查询、target failure/recovery 和 Secret 特征串扫描。
- 回归固定为原 Redis 10 families、一条 Logs、一条 Events、管理授权和一条 Phase 13 社交/搜索主路。
- 只有共享 envelope、Registry/Secret 或 Frontend 全局路由出现具体失败时才扩大检查，原因必须先记录。

## 9. 实施记录与下批交接

完成前创建：

`dev/logs/Phase-14/Phase-14-01-通用插件契约与Redis迁移闭环.md`

记录必须包含实际 Manifest/Schema/API/envelope 形状、Registry/Secret 布局、v1 迁移决策、实际文件和命令/结果、故障注入、偏差和限制，并把给 Phase-14-02 的 exporter catalog、config adapter、metric family registration 和聚焦验收扩展点写清。
