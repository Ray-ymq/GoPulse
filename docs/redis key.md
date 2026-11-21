# Redis Key

`{...}` 为占位符。`{epoch}` 取自 `business_runtime_state.data_epoch`。 [缓存开关](../.env.example)

## 读取缓存（String）

| 键格式 | 用途 | TTL |
| --- | --- | --- |
| `gopulse:post:body:v2:epoch:{epoch}:post:{post_id}:rev:{content_revision}` | 帖子标题和正文 | 默认 5 分钟 |
| `gopulse:post:counts:v1:epoch:{epoch}:{post_id}` | 评论数和点赞数 | 5 秒 |
| `gopulse:post:discovery:v1:epoch:{epoch}:first` | 发现页首屏帖子 ID | 5 秒 |
| `gopulse:comment:first-page:v1:epoch:{epoch}:{post_id}` | 评论首屏 | 5 秒 |
| `gopulse:user:public-profile:v1:epoch:{epoch}:{username}` | 用户公开资料 | 30 秒 |
| `gopulse:search:posts:v1:epoch:{epoch}:index:{generation}:q:{query_digest16}:page:{page}:offset:{offset}:limit:{limit}` | 搜索候选帖子 ID | 30 秒 |

## 共享限流（Hash）

| 键格式 | 用途 | 空闲 TTL |
| --- | --- | --- |
| `gopulse:rl:v1:auth_source:ip:ip:{source_ip}` | 注册、退出登录限流 | 10 分钟 |
| `gopulse:rl:v1:login_attempt:source:subject:{source_ip}:{username_sha256}` | 按 IP 和用户名限制登录 | 10 分钟 |
| `gopulse:rl:v1:login_attempt:source:ip:{source_ip}` | 无有效用户名时按 IP 限制登录 | 10 分钟 |
| `gopulse:rl:v1:post_read:{identity}` | 业务读取限流 | 默认 6 分钟 |
| `gopulse:rl:v1:fact_write:{identity}` | 其他业务写入限流 | 10 分钟 |
| `gopulse:rl:v1:interaction_like:{identity}` | 点赞和取消点赞限流 | 10 分钟 |
| `gopulse:rl:v1:creation:{policy}` | 限制各策略创建身份桶 | 10 分钟 |

`{identity}` 为 `user:{user_id}` 或 `ip:{source_ip}`。 [限流策略](../backend/business/cmd/server/protection.go)
