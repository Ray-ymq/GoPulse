MySQL 是 GoPulse 的核心关系型数据库，负责持久化存储业务数据、用户关系、消息通知、告警规则以及系统管理数据（ 末尾有数据库表的字段，自行查看 ）
- 数据库：MySQL 8.4.0
- 存储引擎：InnoDB
- 字符集：主要使用 utf8mb4
- 业务数据表：20 张
- 数据库迁移：15 个版本
- 迁移工具：golang-migrate

不管是什么项目，数据存储始终是非常重要的组成部分数据库不仅负责保存业务数据，还直接影响系统的数据一致性、可靠性和性能当然这里不会讲解这些概念，而是通过常见的八股的形式，来讲解这些设计的思路

# 数据库表
字段约束说明：
- PK：主键
- FK：外键
- UK：唯一约束或唯一索引
- NN：不允许为空（NOT NULL）
- AI：自增（AUTO_INCREMENT）
- CHECK：数据检查约束
- GENERATED：数据库生成列
- —：无显式默认值或无额外约束

## 一、用户与业务数据

### 1. users（用户表）

用于存储用户账号、身份权限及个人资料

| 字段            | 类型                         | 约束    | 默认值                  | 说明          |
| ------------- | -------------------------- | ----- | -------------------- | ----------- |
| id            | BIGINT UNSIGNED            | PK、AI | —                    | 用户 ID       |
| username      | VARCHAR(32)                | UK、NN | —                    | 用户名         |
| password_hash | VARCHAR(255)               | NN    | —                    | 密码哈希        |
| role          | ENUM('user','super_admin') | NN    | user                 | 用户角色        |
| display_name  | VARCHAR(64)                | NN    | ''                   | 用户显示名称      |
| bio           | VARCHAR(160)               | NN    | ''                   | 用户个人简介      |
| created_at    | DATETIME(6)                | NN    | CURRENT_TIMESTAMP(6) | 注册时间        |
| updated_at    | DATETIME(6)                | NN    | CURRENT_TIMESTAMP(6) | 最近更新时间，自动更新 |

### 2. user_follows（用户关注表）

用于记录用户之间的关注关系

| 字段          | 类型              | 约束       | 默认值                  | 说明         |
| ----------- | --------------- | -------- | -------------------- | ---------- |
| follower_id | BIGINT UNSIGNED | PK、FK、NN | —                    | 发起关注的用户 ID |
| followed_id | BIGINT UNSIGNED | PK、FK、NN | —                    | 被关注的用户 ID  |
| created_at  | DATETIME(6)     | NN       | CURRENT_TIMESTAMP(6) | 关注时间       |

联合主键：`(follower_id, followed_id)`

外键：
- `follower_id` → `users.id`，ON DELETE CASCADE
- `followed_id` → `users.id`，ON DELETE CASCADE

检查约束：`follower_id <> followed_id`，禁止关注自己

### 3. posts（帖子表）

用于存储用户发布的帖子及内容编辑状态

| 字段 | 类型 | 约束 | 默认值 | 说明 |
|---|---|---|---|---|
| id | BIGINT UNSIGNED | PK、AI | — | 帖子 ID |
| author_id | BIGINT UNSIGNED | FK、NN | — | 作者用户 ID |
| title | VARCHAR(120) | NN | — | 帖子标题 |
| content | TEXT | NN | — | 帖子正文 |
| created_at | DATETIME(6) | NN | CURRENT_TIMESTAMP(6) | 创建时间 |
| updated_at | DATETIME(6) | NN | CURRENT_TIMESTAMP(6) | 最近更新时间，自动更新 |
| edited_at | DATETIME(6) | NULL | NULL | 最近一次编辑时间 |
| content_revision | BIGINT UNSIGNED | NN | 1 | 帖子内容版本号 |

外键：`author_id` → `users.id`，ON DELETE RESTRICT

### 4. comments（评论表）

用于存储帖子评论及其所属关系

| 字段 | 类型 | 约束 | 默认值 | 说明 |
|---|---|---|---|---|
| id | BIGINT UNSIGNED | PK、AI | — | 评论 ID |
| post_id | BIGINT UNSIGNED | FK、NN | — | 所属帖子 ID |
| author_id | BIGINT UNSIGNED | FK、NN | — | 评论作者 ID |
| content | VARCHAR(2000) | NN | — | 评论正文 |
| created_at | DATETIME(6) | NN | CURRENT_TIMESTAMP(6) | 评论创建时间 |

外键：
- `post_id` → `posts.id`，ON DELETE RESTRICT
- `author_id` → `users.id`，ON DELETE RESTRICT

### 5. post_likes（帖子点赞表）

用于记录用户对帖子的点赞关系，防止重复点赞

| 字段 | 类型 | 约束 | 默认值 | 说明 |
|---|---|---|---|---|
| post_id | BIGINT UNSIGNED | PK、FK、NN | — | 被点赞帖子 ID |
| user_id | BIGINT UNSIGNED | PK、FK、NN | — | 点赞用户 ID |
| created_at | DATETIME(6) | NN | CURRENT_TIMESTAMP(6) | 点赞时间 |

联合主键：`(post_id, user_id)`

外键：
- `post_id` → `posts.id`，ON DELETE RESTRICT
- `user_id` → `users.id`，ON DELETE RESTRICT

### 6. post_bookmarks（帖子收藏表）

用于存储用户收藏的帖子

| 字段 | 类型 | 约束 | 默认值 | 说明 |
|---|---|---|---|---|
| post_id | BIGINT UNSIGNED | PK、FK、NN | — | 被收藏帖子 ID |
| user_id | BIGINT UNSIGNED | PK、FK、NN | — | 收藏用户 ID |
| created_at | DATETIME(6) | NN | CURRENT_TIMESTAMP(6) | 收藏时间 |

联合主键：`(post_id, user_id)`

外键：
- `post_id` → `posts.id`，ON DELETE CASCADE
- `user_id` → `users.id`，ON DELETE CASCADE

### 7. notifications（消息通知表）

用于持久化评论、点赞、关注产生的用户通知，并管理已读状态

| 字段 | 类型 | 约束 | 默认值 | 说明 |
|---|---|---|---|---|
| id | BIGINT UNSIGNED | PK、AI | — | 通知 ID |
| source_event_id | CHAR(36) | UK、NN | — | 来源业务事件 ID |
| type | VARCHAR(64) | NN、CHECK | — | 通知类型 |
| recipient_id | BIGINT UNSIGNED | FK、NN | — | 接收通知的用户 ID |
| actor_id | BIGINT UNSIGNED | FK、NN | — | 触发通知的用户 ID |
| post_id | BIGINT UNSIGNED | FK、NULL | NULL | 关联帖子 ID |
| comment_id | BIGINT UNSIGNED | FK、NULL | NULL | 关联评论 ID |
| created_at | DATETIME(6) | NN | — | 通知创建时间 |
| read_at | DATETIME(6) | NULL | NULL | 通知阅读时间 |

通知类型：
- `comment.created`：帖子收到评论
- `post.liked`：帖子收到点赞
- `user.followed`：用户收到关注

外键：
- `recipient_id` → `users.id`
- `actor_id` → `users.id`
- `post_id` → `posts.id`
- `comment_id` → `comments.id`

上述外键均采用 ON DELETE RESTRICT

另外，`post_id`、`comment_id` 与通知类型之间存在 CHECK 组合约束

## 二、权限及系统管理

### 8. bootstrap_super_admin（超级管理员表）

用于记录系统初始超级管理员，防止其身份被普通角色变更操作破坏

| 字段 | 类型 | 约束 | 默认值 | 说明 |
|---|---|---|---|---|
| singleton | TINYINT UNSIGNED | PK、CHECK | — | 单例标识，固定为 1 |
| user_id | BIGINT UNSIGNED | FK、UK、NN | — | 初始超级管理员用户 ID |

外键：`user_id` → `users.id`，ON DELETE RESTRICT

### 9. management_audit_events（管理审计表）

用于记录系统管理操作、执行阶段、执行结果及相关审计信息

| 字段 | 类型 | 约束 | 默认值 | 说明 |
|---|---|---|---|---|
| id | BIGINT UNSIGNED | PK、AI | — | 审计记录 ID |
| operation_id | CHAR(32) | NN | — | 管理操作标识 |
| occurred_at | DATETIME(6) | NN | CURRENT_TIMESTAMP(6) | 操作发生时间 |
| actor_kind | ENUM('user','system') | NN | — | 操作者类型 |
| actor_user_id | BIGINT UNSIGNED | NULL | NULL | 操作者用户 ID |
| action | VARCHAR(48) | NN | — | 操作名称 |
| resource_type | VARCHAR(24) | NN | — | 操作资源类型 |
| resource_id | VARCHAR(128) | NN | — | 操作资源 ID |
| phase | ENUM('requested','completed') | NN | — | 操作阶段 |
| outcome | ENUM('succeeded','failed','unknown') | NN | — | 操作结果 |
| request_id | VARCHAR(128) | NN | — | 请求追踪标识 |
| details_json | JSON | NN | — | 操作详情 |

## 三、业务异步与一致性

### 10. business_outbox（业务事件发件箱表）

使用 Transactional Outbox 模式持久化业务事件，保证业务数据提交后，事件能够可靠地发布到 RabbitMQ

| 字段 | 类型 | 约束 | 默认值 | 说明 |
|---|---|---|---|---|
| id | BIGINT UNSIGNED | PK、AI | — | 自增主键 |
| event_id | CHAR(36) | UK、NN | — | 业务事件唯一 ID |
| event_type | VARCHAR(64) | NN、CHECK | — | 业务事件类型 |
| schema_version | SMALLINT UNSIGNED | NN、CHECK | — | 消息结构版本，支持 1、2 |
| payload | JSON | NN | — | 事件消息内容 |
| status | ENUM('pending','leased','published') | NN、CHECK | pending | 事件发布状态 |
| available_at | DATETIME(6) | NN | — | 下一次允许发布的时间 |
| attempt_count | INT UNSIGNED | NN | 0 | 发布尝试次数 |
| lease_owner | VARCHAR(128) | NULL | NULL | 当前领取事件的工作实例 |
| lease_expires_at | DATETIME(6) | NULL | NULL | 事件租约过期时间 |
| published_at | DATETIME(6) | NULL | NULL | 成功发布时间 |
| last_error | VARCHAR(128) | NULL | NULL | 最近一次发布错误 |
| created_at | DATETIME(6) | NN | — | 创建时间 |
| updated_at | DATETIME(6) | NN | — | 更新时间 |

状态约束：
- pending：不存在租约及发布时间
- leased：必须存在租约持有者及过期时间
- published：必须存在发布时间，且不能保留租约

### 11. business_environment（业务环境状态表）

用于管理业务数据代际、系统运行模式及数据恢复状态

| 字段 | 类型 | 约束 | 默认值 | 说明 |
|---|---|---|---|---|
| singleton | TINYINT UNSIGNED | PK、CHECK | — | 单例主键，固定为 1 |
| data_epoch | CHAR(36) | UK、NN、CHECK | — | 当前业务数据代际 ID |
| status | ENUM('ready','restoring') | NN | ready | 业务环境状态 |
| mode | ENUM('legacy_v1','v2') | NN | legacy_v1 | 当前业务运行模式 |
| v1_compatibility_enabled | BOOLEAN | NN | TRUE | 是否启用旧版本兼容 |
| activation_revision | VARCHAR(64) | NN | phase-24-01 | 当前模式激活版本标识 |
| restore_operation_id | CHAR(64) | NULL、CHECK | NULL | 数据恢复操作 ID |
| created_at | DATETIME(6) | NN | CURRENT_TIMESTAMP(6) | 创建时间 |
| updated_at | DATETIME(6) | NN | CURRENT_TIMESTAMP(6) | 更新时间，自动更新 |

### 12. business_commands（业务命令表）

用于持久化业务命令的执行结果，通过幂等键及请求摘要识别重复请求，支持业务重试

| 字段 | 类型 | 约束 | 默认值 | 说明 |
|---|---|---|---|---|
| id | BIGINT UNSIGNED | PK、AI | — | 命令记录主键 |
| data_epoch | CHAR(36) | NN、CHECK | — | 所属业务数据代际 |
| command_id | CHAR(36) | NN、CHECK | — | 业务命令唯一 ID |
| user_id | BIGINT UNSIGNED | NN | — | 命令发起用户 ID |
| command_type | VARCHAR(48) | NN | — | 业务命令类型 |
| idempotency_key_hash | BINARY(32) | NULL、CHECK | NULL | 幂等键 SHA-256 哈希 |
| request_digest | BINARY(32) | NN、CHECK | — | 请求内容摘要 |
| post_id | BIGINT UNSIGNED | NULL | NULL | 关联帖子 ID |
| event_id | CHAR(36) | NULL、CHECK | NULL | 关联业务事件 ID |
| status | ENUM('succeeded','failed','unknown') | NN | — | 命令执行结果 |
| committed_at | DATETIME(6) | NULL | NULL | 命令提交时间 |
| retained_until | DATETIME(6) | NULL | NULL | 记录保留截止时间 |
| created_at | DATETIME(6) | NN | CURRENT_TIMESTAMP(6) | 创建时间 |
| updated_at | DATETIME(6) | NN | CURRENT_TIMESTAMP(6) | 更新时间，自动更新 |

联合唯一索引：
- `(data_epoch, command_id)`
- `(data_epoch, user_id, command_type, idempotency_key_hash)`

### 13. business_event_requirements（事件处理要求表）

用于记录业务事件需要由哪些消费者处理，以及处理版本、完成状态和证据保留期限

| 字段 | 类型 | 约束 | 默认值 | 说明 |
|---|---|---|---|---|
| data_epoch | CHAR(36) | PK、NN、CHECK | — | 业务数据代际 |
| event_id | CHAR(36) | PK、NN、CHECK | — | 业务事件 ID |
| consumer | VARCHAR(32) | PK、NN | — | 目标消费者名称 |
| routing_contract_revision | VARCHAR(64) | PK、NN | — | 消息路由契约版本 |
| applicable_object_version | BIGINT UNSIGNED | NN | 0 | 适用业务对象版本 |
| payload_bytes | INT UNSIGNED | NN、CHECK | 0 | 消息载荷字节数 |
| closed_at | DATETIME(6) | NULL、CHECK | NULL | 处理要求关闭时间 |
| retain_until | DATETIME(6) | NULL、CHECK | NULL | 证据保留截止时间 |
| retention_released | BOOLEAN | NN | FALSE | 是否已释放保留额度 |
| created_at | DATETIME(6) | NN | CURRENT_TIMESTAMP(6) | 创建时间 |
| updated_at | DATETIME(6) | NN | CURRENT_TIMESTAMP(6) | 更新时间，自动更新 |

联合主键：
`(data_epoch, event_id, consumer, routing_contract_revision)`

### 14. business_application_results（事件应用结果表）

用于记录事件在消费者侧的应用结果，支持结果核对、幂等处理和版本追踪

| 字段 | 类型 | 约束 | 默认值 | 说明 |
|---|---|---|---|---|
| id | BIGINT UNSIGNED | PK、AI | — | 记录主键 |
| data_epoch | CHAR(36) | NN、CHECK | — | 业务数据代际 |
| event_id | CHAR(36) | NN、CHECK | — | 业务事件 ID |
| consumer | VARCHAR(32) | NN | — | 消费者名称 |
| routing_contract_revision | VARCHAR(64) | NN | — | 路由契约版本 |
| projection_generation | VARCHAR(255) | NN、CHECK | — | 消费侧投影代际标识 |
| result | ENUM('applied','no_effect_resource_absent') | NN | — | 事件应用结果 |
| actual_version | BIGINT UNSIGNED | NN | 0 | 实际应用的对象版本 |
| completed_at | DATETIME(6) | NN | — | 应用完成时间 |

联合唯一索引：
`(data_epoch, event_id, consumer, routing_contract_revision, projection_generation)`

### 15. business_event_failures（事件失败记录表）

用于追踪异步事件处理过程中的失败、重试和死信情况

| 字段 | 类型 | 约束 | 默认值 | 说明 |
|---|---|---|---|---|
| data_epoch | CHAR(36) | PK、NN、CHECK | — | 业务数据代际 |
| event_id | CHAR(36) | PK、NN、CHECK | — | 业务事件 ID |
| consumer | VARCHAR(32) | PK、NN | — | 消费者名称 |
| attempt_count | INT UNSIGNED | NN、CHECK | 0 | 处理尝试次数 |
| retry_count | INT UNSIGNED | NN | 0 | 重试次数 |
| dead_count | INT UNSIGNED | NN | 0 | 死信次数 |
| last_reason | VARCHAR(64) | NN | '' | 最近一次失败原因 |
| last_failed_at | DATETIME(6) | NULL | NULL | 最近失败时间 |
| last_retry_at | DATETIME(6) | NULL | NULL | 最近重试时间 |
| last_dead_at | DATETIME(6) | NULL | NULL | 最近进入死信状态的时间 |
| retry_summary | VARCHAR(128) | NN | '' | 重试摘要 |
| dead_summary | VARCHAR(128) | NN | '' | 死信摘要 |

联合主键：`(data_epoch, event_id, consumer)`

### 16. business_message_parking（异常消息隔离表）

用于保存无法正常处理的业务消息，避免异常消息持续影响正常消费流程

| 字段 | 类型 | 约束 | 默认值 | 说明 |
|---|---|---|---|---|
| id | BIGINT UNSIGNED | PK、AI | — | 隔离记录主键 |
| data_epoch | CHAR(36) | NN、CHECK | — | 当前业务数据代际 |
| source_epoch | CHAR(36) | NULL、CHECK | NULL | 消息来源数据代际 |
| event_id | CHAR(36) | NULL、CHECK | NULL | 业务事件 ID |
| event_type | VARCHAR(64) | NN | '' | 业务事件类型 |
| schema_version | INT UNSIGNED | NN、CHECK | 0 | 消息结构版本 |
| routing_key | VARCHAR(128) | NN | '' | 消息路由键 |
| source_kind | VARCHAR(32) | NN | — | 消息来源类别 |
| payload | VARBINARY(16384) | NN、CHECK | — | 原始消息内容 |
| payload_sha256 | BINARY(32) | NN | — | 原始消息 SHA-256 摘要 |
| reason | VARCHAR(64) | NN | — | 隔离原因 |
| attempt_count | INT UNSIGNED | NN、CHECK | 0 | 消息处理尝试次数 |
| parked_at | DATETIME(6) | NN | — | 隔离时间 |

联合唯一索引：`(payload_sha256, reason)`

### 17. business_async_budget（异步资源预算表）

用于统计异步业务中未完成的处理要求、证据保留量及隔离消息占用，避免资源无界增长

| 字段 | 类型 | 约束 | 默认值 | 说明 |
|---|---|---|---|---|
| data_epoch | CHAR(36) | PK、CHECK | — | 业务数据代际 |
| open_requirements | BIGINT UNSIGNED | NN、CHECK | 0 | 尚未完成的事件处理要求数量 |
| retained_groups | BIGINT UNSIGNED | NN、CHECK | 0 | 已保留的处理证据组数量 |
| retained_bytes | BIGINT UNSIGNED | NN、CHECK | 0 |

[Mysql 数据库设计.md]()