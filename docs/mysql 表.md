# MySQL 表

GoPulse 的业务数据库包含 17 张业务表和 1 张迁移记录表。本文列出应用仓库正向迁移至 `000023` 后的表结构。`.env.example` 中的数据库名为 `gopulse`，由 `MYSQL_DATABASE` 配置。各业务表使用 InnoDB。字段类型、默认值和完整约束见 [迁移 SQL](../backend/business/migrations/) 和 [环境变量示例](../.env.example)。

## 用户与帖子

| 表 | 用途和字段 | 主键、唯一约束和索引 |
| --- | --- | --- |
| `users` | 用户账户和公开资料。`id` 是用户 ID，`username` 是用户名，`password_hash` 是密码哈希，`role` 为 `user` 或 `super_admin`，`display_name` 是显示名称，`bio` 是简介。`created_at`、`updated_at` 记录创建和更新。 | 主键 `id`。`uq_users_username` 保证用户名唯一，用户名排序规则 `utf8mb4_0900_ai_ci` 忽略大小写和重音。 |
| `posts` | 帖子。`id` 是帖子 ID，`author_id` 是作者，`title` 长度上限为 120 个字符，`content` 类型为 `TEXT`。`created_at`、`updated_at`、`edited_at` 记录创建、更新和编辑。`content_revision` 从 `1` 开始，标题或正文变更时递增。 | 主键 `id`。`idx_posts_created_at_id` 支持全站列表，`idx_posts_author_id` 和 `idx_posts_author_created_id` 支持按作者查询。 |
| `comments` | 帖子评论。`id` 是评论 ID，`post_id` 是所属帖子，`author_id` 是评论作者，`content` 长度上限为 2,000 个字符，`created_at` 记录创建时间。 | 主键 `id`。`idx_comments_post_id_id` 支持按帖子和评论 ID 分页，`idx_comments_author_id` 索引作者。 |
| `post_likes` | 帖子点赞关系。`post_id` 是帖子，`user_id` 是点赞用户，`created_at` 记录点赞时间。 | 主键 `(post_id, user_id)` 防止重复点赞。`idx_post_likes_user_id_post_id` 按用户和帖子建立索引。 |
| `post_bookmarks` | 帖子收藏关系。`post_id` 是帖子，`user_id` 是收藏用户，`created_at` 记录收藏时间。 | 主键 `(post_id, user_id)` 防止重复收藏。`idx_bookmarks_user_created` 支持按用户和收藏时间分页。 |
| `user_follows` | 用户关注关系。`follower_id` 是发起关注的用户，`followed_id` 是被关注用户，`created_at` 记录关注时间。检查约束禁止关注自己。 | 主键 `(follower_id, followed_id)` 防止重复关注。`idx_follows_following` 和 `idx_follows_followers` 分别支持关注列表和粉丝列表。 |
| `notifications` | 站内通知。`id` 是通知 ID，`source_event_id` 是来源事件，`recipient_id` 是接收者，`actor_id` 是触发者。`type` 为 `comment.created`、`post.liked` 或 `user.followed`。`post_id`、`comment_id` 可为空。`created_at` 记录创建时间，`read_at` 为空表示未读。 | 主键 `id`。`uq_notifications_source_event_id` 按来源事件去重。`idx_notifications_recipient_created_id` 支持通知列表。`idx_notifications_actor_id`、`idx_notifications_post_id`、`idx_notifications_comment_id` 分别索引触发者、帖子和评论。 |

### 外键与删除行为

| 子表和字段 | 引用 | `ON DELETE` |
| --- | --- | --- |
| `posts.author_id`、`comments.author_id`、`post_likes.user_id`、`notifications.recipient_id`、`notifications.actor_id`、`bootstrap_super_admin.user_id` | `users.id` | `RESTRICT` |
| `comments.post_id`、`post_likes.post_id`、`notifications.post_id` | `posts.id` | `RESTRICT` |
| `notifications.comment_id` | `comments.id` | `RESTRICT` |
| `post_bookmarks.post_id` | `posts.id` | `CASCADE` |
| `post_bookmarks.user_id`、`user_follows.follower_id`、`user_follows.followed_id` | `users.id` | `CASCADE` |

评论通知的 `post_id` 和 `comment_id` 必须同时有值或同时为空。点赞通知的 `comment_id` 必须为空。关注通知的两个字段都必须为空。删除帖子时，业务代码在同一事务中写入搜索删除记录，清空相关通知的两个资源引用，删除收藏、点赞、评论和帖子，再写入删除事件。相关通知保留。 [通知约束](../backend/business/migrations/000011_notification_shape.up.sql) · [帖子删除事务](../backend/business/internal/modules/post/repository_delete.go)

## 管理与审计

| 表 | 用途和字段 | 主键、唯一约束和索引 |
| --- | --- | --- |
| `bootstrap_super_admin` | 记录初始化的超级管理员。`singleton` 只能为 `1`，`user_id` 引用对应用户。 | 主键 `singleton`。`user_id` 唯一。 |
| `management_audit_events` | 管理操作审计。`id` 是审计记录 ID，`operation_id` 标识操作，`occurred_at` 记录发生时间。`actor_kind` 为 `user` 或 `system`，`actor_user_id` 可为空。`action`、`resource_type`、`resource_id` 记录动作和资源。`phase` 为 `requested` 或 `completed`，`outcome` 为 `succeeded`、`failed` 或 `unknown`。`request_id` 关联请求，`details_json` 保存 JSON 详情。 | 主键 `id`。`idx_audit_time` 支持按发生时间和 ID 查询。 |

## 异步事件与搜索

| 表 | 用途和字段 | 主键、唯一约束和索引 |
| --- | --- | --- |
| `business_outbox` | 保存业务事件和发布状态。`id` 是记录 ID，`event_id` 是事件 ID，`event_type` 是事件类型，`schema_version` 为 `1` 或 `2`，`payload` 为 JSON。`status` 为 `pending`、`leased` 或 `published`。`available_at`、`attempt_count`、`last_error` 记录调度和尝试情况。`lease_owner`、`lease_expires_at` 记录租约，`published_at` 记录发布时间，`created_at`、`updated_at` 记录创建和更新。 | 主键 `id`。`uq_business_outbox_event_id` 保证事件唯一。`idx_business_outbox_pending`、`idx_business_outbox_lease_recovery`、`idx_business_outbox_published_cleanup` 分别支持待发布扫描、租约恢复和已发布记录清理。 |
| `business_runtime_state` | 业务数据运行状态。`singleton` 只能为 `1`，`data_epoch` 标识当前数据世代。`status` 为 `ready` 或 `restoring`，`mode` 为 `legacy_v1` 或 `v2`。`v1_compatibility_enabled` 控制 v1 兼容，`activation_revision` 记录激活版本，`restore_operation_id` 记录恢复操作，`created_at`、`updated_at` 记录创建和更新。 | 主键 `singleton`。`uq_business_environment_epoch` 保证 `data_epoch` 唯一。 |
| `business_commands` | 记录业务命令结果和幂等信息。`id` 是记录 ID，`data_epoch`、`command_id` 标识命令，`user_id`、`command_type` 标识用户和命令类型。`idempotency_key_hash` 可为空，`request_digest` 保存请求摘要。`post_id`、`event_id` 可关联帖子和事件。`status` 为 `succeeded`、`failed` 或 `unknown`。`committed_at`、`retained_until`、`created_at`、`updated_at` 分别记录提交、保留截止、创建和更新时间。 | 主键 `id`。`uq_business_command_id` 约束 `(data_epoch, command_id)`，`uq_business_command_idempotency` 约束 `(data_epoch, user_id, command_type, idempotency_key_hash)`。二级索引为 `idx_business_commands_retention`、`idx_business_commands_event`、`idx_commands_event_retention`、`idx_commands_expiry`。 |
| `event_consumer_obligations` | 记录各消费者需要处理的事件。`data_epoch`、`event_id`、`consumer`、`routing_contract_revision` 标识一项消费义务。`applicable_object_version` 记录对象版本，`payload_bytes` 记录载荷字节数。`closed_at`、`retain_until` 记录关闭时间和保留截止时间。`retention_reserved`、`retention_released` 记录保留配额状态，`created_at`、`updated_at` 记录创建和更新。 | 主键 `(data_epoch, event_id, consumer, routing_contract_revision)`。二级索引为 `idx_business_requirements_event`、`idx_business_requirements_cleanup`、`idx_requirements_event_retention`、`idx_requirements_expiry`、`idx_requirements_recent`。 |
| `event_consumer_results` | 记录消费者处理事件的结果。`id` 是记录 ID，`data_epoch`、`event_id`、`consumer`、`routing_contract_revision` 对应消费义务，`projection_generation` 标识投影世代。`result` 为 `applied` 或 `no_effect_resource_absent`，`actual_version` 是实际处理的版本，`completed_at` 记录完成时间。 | 主键 `id`。`uq_business_application_result` 约束 `(data_epoch, event_id, consumer, routing_contract_revision, projection_generation)`。`idx_business_application_event` 支持按事件及消费者查询。 |
| `business_message_parking` | 保存被隔离的消息。`id` 是记录 ID，`data_epoch` 是当前数据世代，`source_epoch` 是消息来源世代。`event_id`、`event_type`、`schema_version`、`routing_key`、`source_kind` 记录事件和来源。`payload` 保存原始字节，`payload_sha256` 保存摘要，`reason`、`attempt_count`、`parked_at` 记录隔离原因、尝试次数和隔离时间。 | 主键 `id`。`uq_business_message_parking` 按 `(payload_sha256, reason)` 去重。`idx_business_message_parking_time` 支持按隔离时间查询。 |
| `business_async_budget` | 按数据世代记录异步配额用量。`open_requirements` 是未关闭的消费义务数，`retained_groups`、`retained_bytes` 记录保留组数和字节数，`parked_messages`、`parked_bytes` 记录隔离消息数和字节数，`updated_at` 记录更新时间。 | 主键 `data_epoch`。检查约束限制各项计数和字节数。 |
| `post_search_tombstones` | 记录已删除帖子的搜索版本。`data_epoch`、`post_id` 标识帖子，`content_revision` 是删除时递增后的版本，`deleted_at` 记录删除时间。搜索消费和重建读取此表。 | 主键 `(data_epoch, post_id)`。 |

`business_outbox` 支持以下六种事件类型。`comment.created`、`post.liked`、`post.created`、`user.followed`、`post.updated`、`post.deleted`。应用按 `event_id` 关联 Outbox、消费义务和消费结果。消费义务与结果还共享 `data_epoch`、`consumer` 和 `routing_contract_revision`。这些关系由应用维护。 [事件类型约束](../backend/business/migrations/000010_post_deletion.up.sql) · [消费记录写入](../backend/business/internal/modules/businessstate/evidence.go)

## 迁移记录与历史表名

- `schema_migrations` 由 `golang-migrate` 管理。`version` 是主键，`dirty` 标记数据库是否处于未完成的迁移状态。 [迁移命令](../backend/business/cmd/migrate/main.go)
- `000021` 删除 `alert_rules`、`alert_incidents` 和 `alert_rule_states`。 [迁移 SQL](../backend/business/migrations/000021_remove_observability.up.sql)
- `000022` 将 `business_environment` 改名为 `business_runtime_state`，将 `business_event_requirements` 改名为 `event_consumer_obligations`，将 `business_application_results` 改名为 `event_consumer_results`。原索引和约束名称保留。 [迁移 SQL](../backend/business/migrations/000022_clarify_async_table_names.up.sql)
- `000023` 删除 `business_event_failures`。 [迁移 SQL](../backend/business/migrations/000023_remove_event_failure_summaries.up.sql)
