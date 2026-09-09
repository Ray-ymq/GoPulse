# Phase-14-02：MySQL 与 RabbitMQ 插件闭环实施方案

> 当前状态：待实施。本文档定义 Phase 14 第二个执行批次的范围与验收合同；目标版本 `1.11.2`、开发分支 `develop/1.11.2` 和执行顺序以 `Phase-14-总实施方案.md` 为准。

## 1. 批次目标

在 Phase-14-01 已完成的通用单实例契约上，交付第二、第三种官方插件，分别从真实 MySQL 状态和 RabbitMQ Management HTTP 取得聚合快照：

```text
MySQL 最小读权限账号 → mysql-exporter ─┐
                                               ├→ 通用 Monitor/Router/Kafka/Marshaller/VM → Backend
RabbitMQ 受限 monitoring 账号 → rabbitmq-exporter ─┘
```

批次完成必须证明两种插件都可被管理员配置、测试、安装和独立管理，各自指标经完整链路由 Backend 查询；任一个目标或插件失败不影响 Redis、另一个新插件或社交业务。

## 2. 前置条件

- Phase-14-01 已合入 `upstream/main`，版本为 `1.11.1`，Redis v1 迁移、Manifest v2、config/Secret、multi-source metrics 和聚焦验收入口均已通过。
- fetch 后从最新 `upstream/main` 创建 `develop/1.11.2`。
- 核对 Compose 锁定的 MySQL 8.4.0、RabbitMQ 3.13.3 management 真实接口和可建立的最小读权限账号，不以第三方 Exporter 源码审计作为前置。
- 复用 Phase-14-01 的 catalog、包、进程、collector、Backend DTO 和 Frontend 扩展点，不为两个新类型复制 Plugin Manager。

## 3. 实施范围

### 3.1 MySQL 官方插件

- 新增独立 `mysql-exporter` 源码/模块、固定 `/health`/`/metrics`、结构化安全日志、有界超时和 SIGTERM 退出。
- 使用结构化 host/port/database/username/password/timeout 配置，禁止原始 DSN 和任意 SQL；container mode 只允许固定 `mysql` 目标。
- 使用专用最小读权限账号读取锁定 MySQL 版本的 server/global status 与必需聚合字段；不运行业务表查询，不采集 SQL/schema/table/user/host label。
- 固定至少 up、uptime、current/max connections、running threads、queries、slow queries、transaction/rollback 与 buffer-pool 摘要。精确 family/kind/count 同步写入 Exporter、Monitor、Marshaller、Backend 目录和 README。
- 任一连接/认证/超时/解析失败固定 `503` 加唯一 `gopulse_mysql_up 0`，不返回部分/旧值或原始 server error。

### 3.2 RabbitMQ 官方插件

- 新增独立 `rabbitmq-exporter` 源码/模块和与 MySQL 同等的运行边界。
- 配置只接受结构化 host/management port/username/password/vhost-or-fixed-scope/timeout，不接受完整 URL、userinfo 或自定义 API path；container mode 只访问固定 `rabbitmq` service。
- 使用专用 monitoring 账号从锁定 Management API 取得总体快照，固定 up、connections、channels、queues、consumers、ready/unacked、published/delivered/acked 聚合信号。
- 不输出 queue/vhost/consumer/channel/user 名为 label，不将 Management API 原始 JSON 、URL 或认证错误写入日志/事件。
- 失败固定 `503` 加唯一 `gopulse_rabbitmq_up 0`；目标恢复时同进程恢复完整快照。

### 3.3 制品、生命周期与 Compose

- 为两个插件生成可复现 Manifest v2 `tar.gz`，添加到 Monitor 固定嵌入包目录和空卷独立调和，验证入口 digest。
- 为 Monitor Compose 配置最小读权限 MySQL/RabbitMQ 账号和 Secret，不使用 MySQL root，不在 image/label/log 中持久凭据。
- 两种插件使用独立回环端口、process record、collector 和状态；install/start/stop/update/configure 只影响指定 ID。
- 同 volume/Monitor 重启后恢复两种 desired state，一个包/配置损坏不阻止 Redis 和另一个新插件恢复。

### 3.4 链路、Backend 与 Frontend

- 在 Monitor/Marshaller 双层注册 MySQL/RabbitMQ 严格 family/label/count 契约，Router 只放行新固定 source，Marshaller 注入正确 provenance。
- Backend catalog 新增两组固定 metric definitions，查询表达式固定对应 source/target/producer，严格验证返回 labels 和上限。
- Frontend 启用 MySQL/RabbitMQ 卡片的 Schema form、connection-test、install/start/stop/update 和指标入口；Secret 仍不回填。
- 扩展聚焦验收入口，每种 source 都使用真实 Compose 目标证明 Exporter 数值与 Backend 查询一致。

## 4. 不在本批范围

- Kafka、Elasticsearch、VictoriaMetrics Exporter 或自研组件 metrics。
- MySQL 业务数据查询/慢 SQL 明细，RabbitMQ queue/vhost 明细，任意自定义 label。
- 同类第二实例/目标、账号管理 UI、告警、管理端重构或 Kubernetes。

## 5. 建议实施顺序

1. 固定两种 Schema、最小账号权限和指标 family/kind/count/label 目录。
2. 先实现 MySQL Exporter 与真实 success/failure/recovery，再接入通用 Manager 和完整链路。
3. 实现 RabbitMQ Exporter 及聚合边界，复用同一接入点。
4. 增加确定性包、Monitor 镜像嵌入、Compose 最小账号与重启恢复。
5. 启用 Backend/Frontend catalog 与管理闭环，扩展 source-focused 真实验收。
6. 更新版本为 `1.11.2`，完成实施记录、固定门禁和提交。

## 6. 预计直接影响文件

- 新 `exporters/mysql/*`、`exporters/rabbitmq/*`
- 新官方包构建脚本或通用 packager 的受限扩展
- `monitor/internal/plugin/*`、`monitor/internal/metrics/*`、Monitor README/镜像
- `router/internal/envelope/*`
- `marshaller/internal/envelope/*`、`marshaller/internal/metrics/*`
- `backend/internal/exporterplugin/*`、`backend/internal/metricquery/*`、`backend/internal/http/*`
- `frontend/src/services/exporters.ts`、`frontend/src/types/exporter.ts`、`frontend/src/views/ObservabilityExportersView.vue` 及直接 tests
- `deploy/compose.yaml`、相关 Dockerfile、`.env.example`
- `scripts/verify-plugin-metrics.sh` 及直接聚焦验收辅助文件
- `VERSION`、Frontend 版本元数据、README/指标契约文档
- `dev/logs/Phase-14/Phase-14-02-MySQL与RabbitMQ插件闭环.md`

## 7. 批次验收标准

### 7.1 MySQL

- 最小读权限账号可完成 connection-test 和真实采集，无业务表写权限；错密码、目标停止和超时只返 `up=0` 安全快照。
- Backend 可查询 `gopulse_mysql_up` 和至少一个与真实 MySQL status 一致的运行值，series 的 source/target/producer 完全正确。
- 不存在 SQL/schema/table/user/host 高基数 label，响应/日志/事件不包含账号、密码或 DSN。

### 7.2 RabbitMQ

- monitoring 账号可完成 connection-test 和真实聚合采集；错凭据、Management API 停止和超时只影响 RabbitMQ plugin status/`up=0`。
- 创建一条受控业务消息或队列状态变化后，Exporter 聚合数值变化且 Backend 可查；不暴露 queue/vhost 名。
- target 恢复后同进程重新产生完整快照，无需重装或清 volume。

### 7.3 隔离、管理与回归

- Redis、MySQL、RabbitMQ 三种同时 running 且各有唯一进程/目标；停止、损坏或错误配置其中一种，其他两种采集和社交业务继续。
- Monitor 替换/重启恢复三种 desired state，指定 stopped 类型不静默启动；一个损坏记录不阻塞其他类型。
- 管理员在 Frontend 完成两类代表性操作，普通用户被 Backend 拒绝；Secret 在 DOM/API/log/event/registry 中不可见。
- Redis 新/历史指标、Logs/Events、Phase 13 代表性业务与搜索不回归。

### 7.4 完成条件

两种真实目标闭环、故障隔离、Secret 安全与必要回归全部通过，实施记录完整，版本为 `1.11.2`，提交仅包含本批文件时才完成。不继续扩展明细指标或后续插件。

## 8. 固定验证命令与回归范围

```bash
(cd exporters/mysql && go test ./...)
(cd exporters/rabbitmq && go test ./...)
(cd monitor && go test ./...)
(cd router && go test ./...)
(cd marshaller && go test ./...)
(cd backend && go test ./internal/exporterplugin ./internal/metricquery ./internal/http)

(cd frontend && npm test)
(cd frontend && npm run typecheck)
(cd frontend && npm run build)

bash scripts/verify-plugin-metrics.sh --self-test
bash scripts/verify-plugin-metrics.sh --sources mysql,rabbitmq
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/1.11.2 --base-ref upstream/main
git diff --check upstream/main...HEAD
```

- 真实验收必须为两种 source 分别证明 connection-test、install/start、完整指标快照、Backend 查询、不可达/认证失败/恢复和互相隔离。
- 回归固定为 Redis 全链路、一条 Logs/Events、管理授权和 Phase 13 一条社交/搜索主路。
- 只在实际观测到共享 Manager/envelope/Compose 回归时扩大检查，先记录风险理由。

## 9. 实施记录与下批交接

完成前创建：

`dev/logs/Phase-14/Phase-14-02-MySQL与RabbitMQ插件闭环.md`

记录实际上游字段映射、最小账号权限、Schema/Secret、family/label/count 目录、制品 digest、真实目标命令/结果、故障注入、偏差和限制，并写清 Phase-14-03 可复用的 cluster adapter、source registration 和验收参数边界。
