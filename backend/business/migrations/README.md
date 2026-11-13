# 业务数据库表

执行全部迁移后，目标版本为 `22`。数据库包含 18 张业务及辅助表；迁移工具另行维护 `schema_migrations`。

| 表 | 职责 |
| --- | --- |
| `users` | 用户账号、资料和角色 |
| `posts` | 帖子内容及版本 |
| `comments` | 帖子评论 |
| `post_likes` | 帖子点赞关系 |
| `user_follows` | 用户关注关系 |
| `post_bookmarks` | 帖子收藏关系 |
| `notifications` | 用户通知及已读状态 |
| `bootstrap_super_admin` | 唯一受保护的初始超级管理员 |
| `management_audit_events` | 管理操作审计 |
| `business_outbox` | 待发布事件及发布状态 |
| `business_runtime_state` | 数据代次、恢复状态及事件协议模式 |
| `business_commands` | 命令执行结果及幂等请求摘要 |
| `event_consumer_obligations` | 事件消费者的处理义务及保留状态 |
| `event_consumer_results` | 事件消费者在指定投影代次上的处理结果 |
| `business_event_failures` | 事件消费者的重试与死信累计摘要 |
| `business_message_parking` | 隔离消息的原始载荷及原因 |
| `business_async_budget` | 异步处理容量占用计数 |
| `post_search_tombstones` | 已删除帖子的搜索版本记录 |

## 版本 22 的迁移与回滚

`000022_clarify_async_table_names` 仅重命名以下表，保留数据、索引和约束：

- `business_environment` → `business_runtime_state`
- `business_event_requirements` → `event_consumer_obligations`
- `business_application_results` → `event_consumer_results`

旧程序使用旧表名，不能与版本 22 的数据库混用。升级前停止访问这些表的 API、Worker、索引器及维护命令。使用新版本迁移命令执行 `up`，完成后再启动新程序。本地验证回滚时，先停止新程序，使用 `down` 回退迁移 22，再启动匹配旧表名的程序。迁移命令仅允许在本地开发环境执行 `down`；产品环境恢复仍按备份恢复流程执行。

迁移 1 至 21 保持不变。`alert_rules`、`alert_incidents` 和 `alert_rule_states` 已由迁移 21 删除，不属于最终表清单。
