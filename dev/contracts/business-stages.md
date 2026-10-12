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
