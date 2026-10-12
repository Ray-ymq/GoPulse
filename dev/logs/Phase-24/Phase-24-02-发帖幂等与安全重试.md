# Phase-24-02 实施日志：发帖幂等与安全重试

## 执行身份与阶段状态

- 执行分支：`develop/2.6.2`；从 `upstream/main` 基线 `3af89b7` 创建。
- 目标版本：`2.6.2`；migration target 从既有 `14` 扩展为 `15`，旧 migration 1～14 未修改。
- 收口状态：本批实现、真实业务验证和固定门禁已完成；版本元数据已同步。

## 实际交付

- 新增 migration 15，将既有 Outbox 的 schema-version 约束扩展为 `1/2`，并保留迁移 dirty/ahead、权限失败和显式恢复路径的真实 MySQL 覆盖。
- 新增 command、规范摘要、结果/义务读取、保留预算和同事务预留；发帖成功时把 command、post、v2 `post.created`、Outbox 行和 `search` 义务一起提交。
- 生产业务角色启动时执行持久 activation gate；数据库中的 v1 Outbox `pending/leased` 未排空时拒绝切换。切换后共享 Outbox 为既有业务事件补齐当前 epoch。
- 同一用户、epoch、类型和幂等键只产生一条 command/post/event/义务；同摘要重放返回稳定结果，摘要冲突返回 409，跨用户隔离，提交未知使用有界独立查证，删除后只返回最小不可用结果。
- 用户端通过配对的幂等键和 data epoch 实现按用户隔离的 sessionStorage 草稿、刷新/查证/安全重试；正文首次发送后锁定，退出或更换账号清理草稿。未保存 token、key hash 或依赖配置，本地 digest 仅用于检测正文变更，不作为服务端凭据。
- 搜索处理器补充失败原因关联日志；真实 Outbox→RabbitMQ→Search 完成、通知闭环及现有业务回归均通过。

## 固定验证

| 门禁 | 实际结果 |
| --- | --- |
| `rtk proxy make verify MODULE=backend BASE_REF=upstream/main` | 通过；测试、race、build 通过，lint 存量 107 条，新增 0 条 |
| `rtk proxy make verify MODULE=frontend BASE_REF=upstream/main` | 通过；typecheck、lint、76 个 Vitest、build 通过，新增 lint 0 条；版本同步后已重跑 |
| `rtk proxy make verify MODULE=lifecycle BASE_REF=upstream/main` | 通过；测试、race、build 通过，lint 存量 46 条，新增 0 条 |
| `rtk proxy make verify MODULE=devtools BASE_REF=upstream/main` | 通过；测试、race 通过，lint 存量 24 条，新增 0 条 |
| `rtk proxy make integration SCOPE=business` | 通过；真实 MySQL/RabbitMQ/Redis/Elasticsearch、migration 15、10 路并发、冲突、删除结果和搜索收敛均通过 |
| `rtk proxy make e2e SCOPE=business` | 通过；3/3 Playwright business 场景通过，覆盖发帖、评论/点赞、通知、登出/登录及双用户隔离路径 |
| `rtk proxy make check-compose` | 通过；Compose 静态检查通过 |
| `rtk proxy make docs-check BASE_REF=upstream/main` | 通过；合同、链接和引用检查通过 |
| 版本、分支和前端依赖 | `sync_version_metadata.py --version 2.6.2`、显式同步 `deploy/runtime-contracts.json` 的 `product_version`、frontend/admin-frontend 两套 `npm ci` 已通过；npm 保留仓库已有审计告警，未执行依赖升级 |

## 诊断、偏差与处理

- 计划进入基线的 schema target 为 14；旧 migration 仍把 `business_outbox.schema_version` 限制为 `1`，因此按连续迁移合同新增 migration 15，而不是修改既有 SQL。
- 初次业务集成暴露迁移 fixture 偏移、旧 HTTP fixture 缺少无键兼容、搜索 lookup 复用严格写入校验、命令时间扫描和并发连接池自锁等问题；均已修复并以真实 business integration 重跑确认。
- 并发竞争中唯一键冲突事务会回滚后重新查证获胜命令；持有环境锁时先提交只读查证，避免复用同一连接池导致自锁。
- npm `ci` 输出 `glob` 既有弃用提示和 3 个高危审计告警；本批未改变依赖版本或执行自动修复。

## 清理与限制

- 真实 integration/E2E 使用单一 owned Compose 项目；测试结束后仅清理本批实际创建的项目资源，未执行全局 prune。开发栈停机保留其正常卷。
- activation gate 只自动检查 MySQL 持久 Outbox 的 v1 `pending/leased` drain；RabbitMQ unacked/retry/dead 队列与 ES 当前 generation 仍须由 rollout/restore 步骤停排、排空或停放并核验。
- 未交付 Phase-24-03/04 的受限阶段查证、平台适配和管理页面，也未执行 Phase-24-05 的完整 V1 冻结矩阵；本批不声明完整 V1、跨版本或任意时点恢复能力。
