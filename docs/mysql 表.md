# MySQL 表

截至迁移 `000023`。 [迁移 SQL](../backend/business/migrations/)

PK 主键，AI 自增，UK 唯一键，FK 外键，NN 非空，CK 检查约束。多字段共用 PK 或同编号 UK 时构成联合键。默认值中的 — 表示无默认值。

## 1. users（用户表）

用于存储用户账号、身份权限及个人资料

| 字段 | 类型 | 约束 | 默认值 | 说明 |
| --- | --- | --- | --- | --- |
| id | BIGINT UNSIGNED | PK、AI | — | 用户 ID |
| username | VARCHAR(32) | UK、NN | — | 用户名 |
| password_hash | VARCHAR(255) | NN | — | 密码哈希 |
| role | ENUM('user','super_admin') | NN | user | 用户角色 |
| display_name | VARCHAR(64) | NN | '' | 用户显示名称 |
| bio | VARCHAR(160) | NN | '' | 用户个人简介 |
| created_at | DATETIME(6) | NN | CURRENT_TIMESTAMP(6) | 注册时间 |
| updated_at | DATETIME(6) | NN | CURRENT_TIMESTAMP(6) | 最近更新时间，自动更新 |

## 2. posts（帖子表）

用于存储帖子及正文版本

| 字段 | 类型 | 约束 | 默认值 | 说明 |
| --- | --- | --- | --- | --- |
| id | BIGINT UNSIGNED | PK、AI | — | 帖子 ID |
| author_id | BIGINT UNSIGNED | FK、NN | — | 作者 ID，引用 `users.id` |
| title | VARCHAR(120) | NN | — | 标题 |
| content | TEXT | NN | — | 正文 |
| created_at | DATETIME(6) | NN | CURRENT_TIMESTAMP(6) | 发帖时间 |
| updated_at | DATETIME(6) | NN | CURRENT_TIMESTAMP(6) | 最近更新时间，自动更新 |
| edited_at | DATETIME(6) | — | NULL | 最近编辑时间 |
| content_revision | BIGINT UNSIGNED | NN | 1 | 标题或正文变更时递增 |

## 3. comments（评论表）

用于存储帖子评论

| 字段 | 类型 | 约束 | 默认值 | 说明 |
| --- | --- | --- | --- | --- |
| id | BIGINT UNSIGNED | PK、AI | — | 评论 ID |
| post_id | BIGINT UNSIGNED | FK、NN | — | 帖子 ID，引用 `posts.id` |
| author_id | BIGINT UNSIGNED | FK、NN | — | 评论作者 ID，引用 `users.id` |
| content | VARCHAR(2000) | NN | — | 评论内容 |
| created_at | DATETIME(6) | NN | CURRENT_TIMESTAMP(6) | 评论时间 |

## 4. post_likes（点赞表）

用于记录用户对帖子的点赞

| 字段 | 类型 | 约束 | 默认值 | 说明 |
| --- | --- | --- | --- | --- |
| post_id | BIGINT UNSIGNED | PK、FK | — | 帖子 ID，引用 `posts.id` |
| user_id | BIGINT UNSIGNED | PK、FK | — | 点赞用户 ID，引用 `users.id` |
| created_at | DATETIME(6) | NN | CURRENT_TIMESTAMP(6) | 点赞时间 |

## 5. post_bookmarks（收藏表）

用于记录用户收藏的帖子

| 字段 | 类型 | 约束 | 默认值 | 说明 |
| --- | --- | --- | --- | --- |
| post_id | BIGINT UNSIGNED | PK、FK | — | 帖子 ID，引用 `posts.id` |
| user_id | BIGINT UNSIGNED | PK、FK | — | 收藏用户 ID，引用 `users.id` |
| created_at | DATETIME(6) | NN | CURRENT_TIMESTAMP(6) | 收藏时间 |

## 6. user_follows（关注表）

用于记录用户之间的关注关系

| 字段 | 类型 | 约束 | 默认值 | 说明 |
| --- | --- | --- | --- | --- |
| follower_id | BIGINT UNSIGNED | PK、FK、CK | — | 发起关注的用户 ID，引用 `users.id` |
| followed_id | BIGINT UNSIGNED | PK、FK、CK | — | 被关注用户 ID，不能与发起者相同，引用 `users.id` |
| created_at | DATETIME(6) | NN | CURRENT_TIMESTAMP(6) | 关注时间 |

## 7. notifications（通知表）

用于存储评论、点赞和关注通知

| 字段 | 类型 | 约束 | 默认值 | 说明 |
| --- | --- | --- | --- | --- |
| id | BIGINT UNSIGNED | PK、AI | — | 通知 ID |
| source_event_id | CHAR(36) | UK、NN | — | 来源事件 ID |
| type | VARCHAR(64) | NN、CK | — | 通知类型 |
| recipient_id | BIGINT UNSIGNED | FK、NN | — | 接收者 ID，引用 `users.id` |
| actor_id | BIGINT UNSIGNED | FK、NN | — | 触发者 ID，引用 `users.id` |
| post_id | BIGINT UNSIGNED | FK、CK | NULL | 关联帖子 ID，引用 `posts.id` |
| comment_id | BIGINT UNSIGNED | FK、CK | NULL | 关联评论 ID，引用 `comments.id` |
| created_at | DATETIME(6) | NN | — | 通知时间 |
| read_at | DATETIME(6) | — | NULL | 已读时间，空值表示未读 |

## 8. bootstrap_super_admin（初始超级管理员表）

用于记录初始超级管理员

| 字段 | 类型 | 约束 | 默认值 | 说明 |
| --- | --- | --- | --- | --- |
| singleton | TINYINT UNSIGNED | PK、CK | — | 固定为 1 |
| user_id | BIGINT UNSIGNED | UK、FK、NN | — | 超级管理员用户 ID，引用 `users.id` |

## 9. management_audit_events（管理审计表）

用于记录管理操作及结果

| 字段 | 类型 | 约束 | 默认值 | 说明 |
| --- | --- | --- | --- | --- |
| id | BIGINT UNSIGNED | PK、AI | — | 审计记录 ID |
| operation_id | CHAR(32) | NN | — | 操作 ID |
| occurred_at | DATETIME(6) | NN | CURRENT_TIMESTAMP(6) | 发生时间 |
| actor_kind | ENUM('user','system') | NN | — | 操作主体类型 |
| actor_user_id | BIGINT UNSIGNED | — | NULL | 操作用户 ID |
| action | VARCHAR(48) | NN | — | 管理动作 |
| resource_type | VARCHAR(24) | NN | — | 资源类型 |
| resource_id | VARCHAR(128) | NN | — | 资源 ID |
| phase | ENUM('requested','completed') | NN | — | 操作阶段 |
| outcome | ENUM('succeeded','failed','unknown') | NN | — | 操作结果 |
| request_id | VARCHAR(128) | NN | — | 请求 ID |
| details_json | JSON | NN | — | 审计详情 |

## 10. business_outbox（业务事件发件箱表）

用于存储业务事件及发布状态

| 字段 | 类型 | 约束 | 默认值 | 说明 |
| --- | --- | --- | --- | --- |
| id | BIGINT UNSIGNED | PK、AI | — | 发件箱记录 ID |
| event_id | CHAR(36) | UK、NN | — | 事件 ID |
| event_type | VARCHAR(64) | NN、CK | — | 事件类型 |
| schema_version | SMALLINT UNSIGNED | NN、CK | — | 事件格式版本，取 1 或 2 |
| payload | JSON | NN | — | 事件载荷 |
| status | ENUM('pending','leased','published') | NN、CK | pending | 发布状态 |
| available_at | DATETIME(6) | NN | — | 最早可发布时间 |
| attempt_count | INT UNSIGNED | NN | 0 | 发布尝试次数 |
| lease_owner | VARCHAR(128) | CK | NULL | 租约持有者 |
| lease_expires_at | DATETIME(6) | CK | NULL | 租约到期时间 |
| published_at | DATETIME(6) | CK | NULL | 发布时间 |
| last_error | VARCHAR(128) | — | NULL | 最近发布错误 |
| created_at | DATETIME(6) | NN | — | 创建时间 |
| updated_at | DATETIME(6) | NN | — | 最近更新时间 |

## 11. business_runtime_state（业务运行状态表）

用于记录当前数据世代和恢复状态

| 字段 | 类型 | 约束 | 默认值 | 说明 |
| --- | --- | --- | --- | --- |
| singleton | TINYINT UNSIGNED | PK、CK | — | 固定为 1 |
| data_epoch | CHAR(36) | UK、NN、CK | — | 数据世代 ID |
| status | ENUM('ready','restoring') | NN | ready | 就绪或恢复状态 |
| mode | ENUM('legacy_v1','v2') | NN | legacy_v1 | 事件运行模式 |
| v1_compatibility_enabled | BOOLEAN | NN | TRUE | 是否启用 v1 兼容 |
| activation_revision | VARCHAR(64) | NN | phase-24-01 | 激活版本 |
| restore_operation_id | CHAR(64) | CK | NULL | 恢复操作 ID |
| created_at | DATETIME(6) | NN | CURRENT_TIMESTAMP(6) | 创建时间 |
| updated_at | DATETIME(6) | NN | CURRENT_TIMESTAMP(6) | 最近更新时间，自动更新 |

## 12. business_commands（业务命令表）

用于记录命令结果和幂等信息

| 字段 | 类型 | 约束 | 默认值 | 说明 |
| --- | --- | --- | --- | --- |
| id | BIGINT UNSIGNED | PK、AI | — | 命令记录 ID |
| data_epoch | CHAR(36) | UK1、UK2、NN、CK | — | 数据世代 ID |
| command_id | CHAR(36) | UK1、NN、CK | — | 命令 ID |
| user_id | BIGINT UNSIGNED | UK2、NN | — | 发起用户 ID |
| command_type | VARCHAR(48) | UK2、NN | — | 命令类型 |
| idempotency_key_hash | BINARY(32) | UK2、CK | NULL | 幂等键哈希 |
| request_digest | BINARY(32) | NN、CK | — | 请求摘要 |
| post_id | BIGINT UNSIGNED | — | NULL | 关联帖子 ID |
| event_id | CHAR(36) | CK | NULL | 关联事件 ID |
| status | ENUM('succeeded','failed','unknown') | NN | — | 命令结果 |
| committed_at | DATETIME(6) | — | NULL | 提交时间 |
| retained_until | DATETIME(6) | — | NULL | 保留截止时间 |
| created_at | DATETIME(6) | NN | CURRENT_TIMESTAMP(6) | 创建时间 |
| updated_at | DATETIME(6) | NN | CURRENT_TIMESTAMP(6) | 最近更新时间，自动更新 |

## 13. event_consumer_obligations（事件消费义务表）

用于记录各消费者需要处理的事件

| 字段 | 类型 | 约束 | 默认值 | 说明 |
| --- | --- | --- | --- | --- |
| data_epoch | CHAR(36) | PK、CK | — | 数据世代 ID |
| event_id | CHAR(36) | PK、CK | — | 事件 ID |
| consumer | VARCHAR(32) | PK | — | 消费者标识 |
| routing_contract_revision | VARCHAR(64) | PK | — | 路由契约版本 |
| applicable_object_version | BIGINT UNSIGNED | NN | 0 | 消费义务对应的对象版本 |
| payload_bytes | INT UNSIGNED | NN、CK | 0 | 载荷字节数 |
| closed_at | DATETIME(6) | CK | NULL | 关闭时间 |
| retain_until | DATETIME(6) | CK | NULL | 保留截止时间 |
| retention_released | BOOLEAN | NN | FALSE | 是否已释放保留配额 |
| created_at | DATETIME(6) | NN | CURRENT_TIMESTAMP(6) | 创建时间 |
| updated_at | DATETIME(6) | NN | CURRENT_TIMESTAMP(6) | 最近更新时间，自动更新 |
| retention_reserved | BOOLEAN | NN | FALSE | 是否已预留保留配额 |

## 14. event_consumer_results（事件消费结果表）

用于记录消费者处理事件的结果

| 字段 | 类型 | 约束 | 默认值 | 说明 |
| --- | --- | --- | --- | --- |
| id | BIGINT UNSIGNED | PK、AI | — | 消费结果 ID |
| data_epoch | CHAR(36) | UK1、NN、CK | — | 数据世代 ID |
| event_id | CHAR(36) | UK1、NN、CK | — | 事件 ID |
| consumer | VARCHAR(32) | UK1、NN | — | 消费者标识 |
| routing_contract_revision | VARCHAR(64) | UK1、NN | — | 路由契约版本 |
| projection_generation | VARCHAR(255) | UK1、NN、CK | — | 投影世代 |
| result | ENUM('applied','no_effect_resource_absent') | NN | — | 消费结果 |
| actual_version | BIGINT UNSIGNED | NN | 0 | 实际处理的对象版本 |
| completed_at | DATETIME(6) | NN | — | 完成时间 |

## 15. business_message_parking（消息隔离表）

用于保存隔离消息及原因

| 字段 | 类型 | 约束 | 默认值 | 说明 |
| --- | --- | --- | --- | --- |
| id | BIGINT UNSIGNED | PK、AI | — | 隔离记录 ID |
| data_epoch | CHAR(36) | NN、CK | — | 当前数据世代 ID |
| source_epoch | CHAR(36) | CK | NULL | 来源数据世代 ID |
| event_id | CHAR(36) | CK | NULL | 事件 ID |
| event_type | VARCHAR(64) | NN | '' | 事件类型 |
| schema_version | INT UNSIGNED | NN、CK | 0 | 事件格式版本 |
| routing_key | VARCHAR(128) | NN | '' | 消息路由键 |
| source_kind | VARCHAR(32) | NN | — | 消息来源类型 |
| payload | VARBINARY(16384) | NN、CK | — | 原始消息载荷 |
| payload_sha256 | BINARY(32) | UK1、NN | — | 载荷 SHA-256 摘要 |
| reason | VARCHAR(64) | UK1、NN | — | 隔离原因 |
| attempt_count | INT UNSIGNED | NN、CK | 0 | 处理尝试次数 |
| parked_at | DATETIME(6) | NN | — | 隔离时间 |

## 16. business_async_budget（异步配额表）

用于记录各数据世代的异步配额用量

| 字段 | 类型 | 约束 | 默认值 | 说明 |
| --- | --- | --- | --- | --- |
| data_epoch | CHAR(36) | PK、CK | — | 数据世代 ID |
| open_requirements | BIGINT UNSIGNED | NN、CK | 0 | 未关闭的消费义务数 |
| retained_groups | BIGINT UNSIGNED | NN、CK | 0 | 保留组数 |
| retained_bytes | BIGINT UNSIGNED | NN、CK | 0 | 保留字节数 |
| parked_messages | BIGINT UNSIGNED | NN、CK | 0 | 隔离消息数 |
| parked_bytes | BIGINT UNSIGNED | NN、CK | 0 | 隔离消息字节数 |
| updated_at | DATETIME(6) | NN | CURRENT_TIMESTAMP(6) | 最近更新时间，自动更新 |

## 17. post_search_tombstones（搜索删除记录表）

用于记录已删除帖子的搜索版本

| 字段 | 类型 | 约束 | 默认值 | 说明 |
| --- | --- | --- | --- | --- |
| data_epoch | CHAR(36) | PK | — | 数据世代 ID |
| post_id | BIGINT UNSIGNED | PK | — | 已删除的帖子 ID |
| content_revision | BIGINT UNSIGNED | NN | — | 删除时递增后的正文版本 |
| deleted_at | DATETIME(6) | NN | — | 删除时间 |

## 18. schema_migrations（迁移记录表）

用于记录数据库迁移版本和状态

| 字段 | 类型 | 约束 | 默认值 | 说明 |
| --- | --- | --- | --- | --- |
| version | BIGINT | PK | — | 迁移版本 |
| dirty | BOOLEAN | NN | — | 是否存在未完成的迁移 |
