# Phase-24-01 实施日志：代际身份与搜索完成基础

## 执行身份与阶段状态

- 执行分支：`develop/2.6.1`；从主线基线 `68958d7` 创建。
- 目标版本：`2.6.1`；migration target 从既有 `13` 扩展为 `14`，既有 migration 1～13 未修改。
- 收口状态：本批实现和固定门禁已完成；生产仍为 `legacy_v1`。本日志不把 Phase-24-02～05 或完整 V1 验收写成完成。

## 实际交付

- 新增 migration 14 的业务环境、命令、事件必要义务、应用结果、失败摘要、消息停放和异步预算表，并补齐 target/dirty 状态验证及真实 MySQL 覆盖。
- 新增 `businessstate`，持久化 epoch、ready/restoring、v1/v2 准入、有限停放、完成/失败摘要、预算和留存约束；worker 与 bus 在副作用前执行当前 epoch 准入，v1 仍是生产默认。
- Envelope 增加现有六类事件的 v2 fixture 及严格 epoch/command/revision 校验；v2 只用于 fixture、消费者和恢复准备，不启用 v2 生产者或命令幂等。
- 搜索写入、删除、重建和完成记录绑定真实 index generation 与 epoch；Elasticsearch 代际标记写入 mapping `_meta`，冲突按实际存储事实核验，重建不伪造事件完成。
- Outbox 清理受事件义务、完成结果、命令引用和保留期屏障约束；Redis 帖子缓存、搜索游标和 lifecycle 恢复 journal 按 epoch 隔离。恢复失败保留 restoring/pending，不提前 ready。
- 增加真实 MySQL/Elasticsearch/RabbitMQ 恢复准备 integration，并同步业务阶段、迁移状态、runtime contract、Backend 和 Bundle 文档。

## 固定验证

| 门禁 | 实际结果 |
| --- | --- |
| `rtk proxy make verify MODULE=backend BASE_REF=upstream/main` | 通过；测试、race、build 通过，lint 存量 107 条，新增 0 条 |
| `rtk proxy make verify MODULE=lifecycle BASE_REF=upstream/main` | 通过；测试、race、build 通过，lint 存量 46 条，新增 0 条 |
| `rtk proxy make verify MODULE=devtools BASE_REF=upstream/main` | 通过；测试、race、build 通过，lint 存量 24 条，新增 0 条 |
| `rtk proxy make integration SCOPE=business` | 通过；migration 14、既有业务集成及真实恢复准备用例均通过 |
| `rtk proxy make check-compose` | 通过；版本元数据同步后的受影响检查已复核 |
| `rtk proxy make docs-check BASE_REF=upstream/main` | 通过；合同/链接/引用检查通过 |
| 版本、分支和前端依赖 | `sync_version_metadata.py --version 2.6.1`、两套 `npm ci`、版本校验和 completion 校验通过 |

固定门禁运行期间使用单一 owned Compose 项目。最终业务 integration 通过后，只删除了该项目的四个精确命名卷，未执行全局 prune。

## 偏差与处理

- 计划进入基线的 schema target 为 13；本批按合同新增连续 migration 14，旧 migration SQL 保持不变，迁移集成随后改为验证 target dirty 拒绝及显式 v12 恢复路径。
- 真实 Elasticsearch 拒绝将自定义代际值放在 index setting；实现改为 mapping `_meta`，并让读取逻辑兼容旧 setting 解析，最终真实恢复准备用例通过。
- 重用测试卷曾使业务 fixture 互相污染；清理确认为本项目专属后定向删除四个卷，再以干净环境重跑固定 business integration 并通过。
- 版本元数据按合同在产品固定门禁通过后同步；npm 未升级依赖，`npm ci` 的现有审计告警未改变本批依赖输入。

## 明确边界

- 本批没有启用 v2 发帖生产、command idempotency 或安全重试；这些属于 Phase-24-02。
- 本批没有交付受限状态接口、平台查证适配或管理端阶段页面；这些属于 Phase-24-03/04。
- 本批没有执行 Phase-24-05 的完整 V1 冻结矩阵，也不声明跨版本、任意时点或完整 Bundle 恢复能力；恢复验证限于同 Bundle、已排空备份的代际准备和失败停点。
