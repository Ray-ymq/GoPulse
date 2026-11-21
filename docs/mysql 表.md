# MySQL 表

截至迁移 `000023`，共 17 张业务表和 1 张迁移记录表。 [字段和索引](../backend/business/migrations/)

| 表名 | 用途 |
| --- | --- |
| `users` | 用户账户和资料 |
| `posts` | 帖子 |
| `comments` | 评论 |
| `post_likes` | 点赞 |
| `post_bookmarks` | 收藏 |
| `user_follows` | 关注关系 |
| `notifications` | 站内通知 |
| `bootstrap_super_admin` | 初始超级管理员 |
| `management_audit_events` | 管理操作审计 |
| `business_outbox` | 业务事件及发布状态 |
| `business_runtime_state` | 数据世代和运行状态 |
| `business_commands` | 命令结果与幂等记录 |
| `event_consumer_obligations` | 事件消费义务 |
| `event_consumer_results` | 事件消费结果 |
| `business_message_parking` | 隔离消息 |
| `business_async_budget` | 异步配额用量 |
| `post_search_tombstones` | 已删除帖子的搜索版本 |
| `schema_migrations` | 迁移版本和状态 |
