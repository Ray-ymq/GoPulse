# 业务代际、搜索完成与异步留存

Phase-24-01 建立业务阶段的持久基础，Phase-24-02 以 migration 15 扩展 Outbox
约束；`backend/migrations` 是唯一结构来源。
`business_environment` 保存唯一当前 `data_epoch`、`ready/restoring` 状态、
`legacy_v1/v2` 模式和 v1 兼容许可。普通重启调用 `Ensure` 只补齐缺失的预算行，
不会重新生成 epoch。

Phase-24-02 的 migration 15 将已有 Outbox 的 schema-version 约束扩展为
`1/2`，使受控激活后的 v2 事件可以仍在同一 Outbox 事务边界内落库；它不回填
历史 command 或完成证据。

## 消息边界

Phase-24-02 的业务角色在持久 DB drain gate 通过后启用 v2；新发帖使用带
`command_id` 的 v2 `post.created`，其他现有业务事件由共享 Outbox 在同一事实事务内补齐
当前 epoch。Envelope v2 保留六类现有事件；所有 v2 事件必须携带 canonical UUID
`data_epoch`，`post.created` 还必须携带 `command_id` 和正 `content_revision`。
历史 v1 仍分类为 `legacy_unknown`，不产生 v2 搜索完成承诺；activation 不自动探测
RabbitMQ 的 unacked/retry/dead 队列或 ES generation，它们仍是 rollout 前的停排、排空/停放和核验步骤。

Worker/Indexer 在处理副作用前读取持久环境。旧 epoch、恢复期间的消息、未启用 v1
兼容许可，以及缺少 `search` 必要义务的 v2 搜索事件，只有在原 payload 成功写入
`business_message_parking` 后才确认 Broker；停放失败保留原消息并重试。数据库查询失败
也按暂时失败处理，不以缓存的 epoch 放行。

## 搜索完成依据

`business_event_requirements`、`business_application_results` 和
`business_event_failures` 分别保存必要消费者义务、实际投影完成和有限失败摘要。
完成结果按 `(data_epoch,event_id,consumer,routing_contract_revision,projection_generation)`
唯一；搜索修订使用 `post-created-search/1`。ES 写入返回真实物理索引、文档 ID、版本，
并核验索引上的 epoch 设置；409 只有在实际文档、版本、epoch 和规范投影均查证后才可视为
已完成。缺失事实的删除副作用记录 `no_effect_resource_absent`。

`business_async_budget` 限制未闭合义务 10,000、停放 10,000 条/160 MiB、
保留组 100,000/1 GiB；每组另计 1 KiB 逻辑元数据。首次完成、停放和到期清理在同一
领域事务内计费，清理只释放本次实际删除的 Outbox 事件。未闭合必要义务或未到保留期的
命令/完成引用都会阻止 `CleanupPublished`。

## 受限阶段查证

业务角色提供私有的 `GET /internal/v1/business-stages`，平台角色提供管理员保护的
`GET /api/v1/observability/business-stages`。两者都要求 canonical `epoch` 和且仅一个
`command_id`、`event_id`、`post_id`；公共接口可用 `include=logs,trace` 指定来源，并在
`from/to` 中提供不超过 24 小时的 UTC 窗口。未知参数、重复来源、模糊对象或越界窗口均为
固定校验错误。公共接口只转发当前管理 Cookie；独立 platform 通过最多两个配置的私网
`BUSINESS_STAGE_ENDPOINTS` 选择一个上游，RPC 最多 2 秒、总请求最多 3 秒、并发槽最多 4，
不重试、不 hedge，也不接受用户提供的地址。business 查询每实例最多两个槽，槽满立即返回
`503 business_stage_busy`。

私有接口同时要求专用 `BUSINESS_STAGE_SERVICE_CREDENTIAL` 和管理 Cookie；业务端重新从
MySQL 读取当前角色，不能信任 user/role header。凭据由 lifecycle 私有环境随机生成、备份保护
和诊断脱敏，Compose 不发布业务私网端口，edge 的 `/internal/` 仍返回 404。platform 不创建
业务 Elasticsearch、业务完成 Repository 或阶段查询数据库池；combined 直接复用本地领域
service，不绕 loopback HTTP。

响应只返回请求/当前 epoch、解析后的技术身份、固定的命令/Outbox/消费者/完成/失败摘要、
预算水位以及逐来源 `quality`。每类摘要最多 20 条，不含用户 ID、正文、digest 或 payload。
当前可见性只在当前 epoch 且 MySQL 事实仍存在时，解析 current alias 的物理 generation，
用一次 typed exact-object ES 查询核验 `data_epoch/post_id/content_revision`；不使用 GET-by-id、
refresh、全表扫描或关键词兜底。Logs 关联只接受 typed identity，单次后端请求最多 100 条、
窗口最多 24 小时；旧日志缺 `data_epoch` 只能是 `legacy_unknown` 线索，alias 缺失为
`expired`，无命中为 `no_evidence`，后端故障为 `backend_error`。Trace 只回传已有日志中的
真实 OTLP ID；未配置查询能力时返回 `not_configured`，不生成伪造链接或 ID。各 source 的
失败不会覆盖已经取得的其他 source，也不会把未知填成零或 false。

Logs v1 的 `command_id`、`data_epoch`、`projection_generation`、`routing_contract_revision`、
`consumer` 是可选扩展。Monitor/Marshaller 对它们做严格格式和有限枚举校验，template、现有
index mapping 与 retention allowlist 保持一致；缺字段的旧日志仍可读，不回填到当前 epoch。

## 恢复准备

`search-reindex` 的普通入口仍是 `--if-missing`。恢复准备必须同时提供：

```text
--restore-operation 64 位小写 hex
--expected-old-epoch canonical UUID
--new-epoch canonical UUID
```

入口会持久设置 `restoring`，在真实 alias/索引 epoch 核验通过后才切换 `ready`；旧 epoch
被拒绝、新 epoch 被接受。lifecycle 将 operation、恢复来源和预先生成的新 epoch 写入
私有 `restore-pending.json`，恢复接续复用原值，失败不发布 ready。Compose 的
`search-init` 只在收到这三个临时变量时进入恢复分支。

恢复后的 Redis 帖子缓存键带 epoch；搜索 cursor 同时绑定 epoch 和物理 generation。
旧缓存、旧 cursor 和旧消息不能混入新代际。重建扫描只复制恢复事实，不伪造任何事件的
应用完成记录。上述能力是同 Bundle、已排空备份的恢复准备，不是跨版本或任意时点恢复保证。
