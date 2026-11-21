# Redis Key

GoPulse 的业务 API 使用 Redis 保存读取缓存和共享限流状态。所有业务键使用 `gopulse:` 前缀。`REDIS_DB` 默认值为 `0`。下文用 `{...}` 表示占位符，实际键不包含花括号。 [服务组装](../backend/business/cmd/server/roles.go) · [环境变量示例](../.env.example)

`BACKEND_REDIS_DISABLED=true` 时，业务 API 不创建 Redis 客户端，读取缓存停用。若限流仍开启，配额由各进程独立计算。`BACKEND_RATE_LIMIT_ENABLED` 默认开启。 [配置读取](../backend/business/internal/platform/config/config.go) · [限流配置](../backend/business/internal/platform/config/protection.go)

## 读取缓存

Redis 数据类型均为 String，值为 JSON。`{epoch}` 是 MySQL `business_runtime_state.data_epoch` 中的数据世代。切换数据世代后，请求使用新键，旧键按各自的有效期过期。 [MySQL 表](mysql%20表.md) · [通用缓存键](../backend/business/internal/platform/readcache/cache.go)

| 键格式 | 内容 | 有效期（TTL） | 启用条件 |
| --- | --- | --- | --- |
| `gopulse:post:body:v2:epoch:{epoch}:post:{post_id}:rev:{content_revision}` | `version` 为 `2`，`body` 含帖子 ID、作者 ID、标题、正文、正文版本和时间字段。帖子详情与发现页批量读取共用此格式。 | 由 `REDIS_POST_DETAIL_TTL` 配置，默认 5 分钟。详情回填加减最多 10% 的随机偏移。发现页批量回填按配置时长计时，包含回源用时。 | 启用 Redis 后使用。发现页批量读取还需启用发现页缓存。 |
| `gopulse:post:counts:v1:epoch:{epoch}:{post_id}` | `comments`、`likes`，分别为评论数和点赞数。 | 5 秒 | `BACKEND_POST_COUNTS_CACHE_ENABLED=true`，默认关闭。 |
| `gopulse:post:discovery:v1:epoch:{epoch}:first` | `ids`、`count`。仅用于默认 20 条、无游标、无筛选的发现页首屏。最多保存 21 个候选帖子 ID。 | 5 秒 | `BACKEND_DISCOVERY_CACHE_ENABLED=true`，默认关闭。 |
| `gopulse:comment:first-page:v1:epoch:{epoch}:{post_id}` | `comments`、`next_cursor`。仅用于默认 20 条、无游标的评论首屏。 | 5 秒 | `BACKEND_COMMENT_PAGE_CACHE_ENABLED=true`，默认关闭。 |
| `gopulse:user:public-profile:v1:epoch:{epoch}:{username}` | `id`、`username`、`display_name`、`bio`、`created_at`。键中的用户名去除首尾空白后转为小写。访问者的关注状态由 MySQL 查询。 | 30 秒 | `BACKEND_PUBLIC_PROFILE_CACHE_ENABLED=true`，默认关闭。 |
| `gopulse:search:posts:v1:epoch:{epoch}:index:{generation}:q:{query_digest16}:page:{page}:offset:{offset}:limit:{limit}` | `post_ids`，仅保存首轮 Elasticsearch 查询返回的非空候选帖子 ID。 | 30 秒 | 启用 Redis 后使用。需要有效的数据世代，只用于未使用 Elasticsearch 时间点快照（PIT）的前 10 页。 |

`{content_revision}` 来自 `posts.content_revision`。编辑帖子后，读取使用新版本的键。帖子详情读取时先从 MySQL 确认帖子和版本。发现页批量读取时也从 MySQL 取版本。 [正文键和详情回填](../backend/business/internal/platform/postcache/post_body.go) · [发现页批量回填](../backend/business/internal/modules/post/repository_discovery.go)

搜索键的 `{generation}` 是 Elasticsearch 索引世代。`{query_digest16}` 取搜索词去除首尾空白后的 SHA-256 十六进制摘要前 16 个字符。`{page}` 从 `1` 开始，`{offset}` 从 `0` 开始，`{limit}` 是每页请求条数。命中缓存后，服务仍从 MySQL 加载候选帖子详情。 [搜索读取流程](../backend/business/internal/modules/search/service.go) · [搜索缓存格式](../backend/business/internal/modules/search/result_cache.go)

### 主动失效

| 成功写入的操作 | 应用尝试删除的缓存 |
| --- | --- |
| 点赞或取消点赞 | 对应帖子的互动计数。 |
| 新增评论 | 对应帖子的互动计数和评论首屏。 |
| 新建帖子 | 发现页首屏。 |
| 删除帖子 | 对应帖子的互动计数、评论首屏和发现页首屏。 |
| 更新公开资料 | 对应用户名的公开资料。 |

应用仅对已启用的缓存执行上述删除。正文缓存按数据世代和正文版本区分。搜索缓存按数据世代和索引世代区分。搜索候选 ID 按 TTL 过期。加载搜索结果时，服务会跳过 MySQL 中已不存在的帖子。 [失效实现](../backend/business/internal/platform/readcache/cache.go) · [点赞](../backend/business/internal/modules/like/service.go) · [评论](../backend/business/internal/modules/comment/service.go) · [发帖](../backend/business/internal/modules/post/repository_command.go) · [删帖](../backend/business/internal/modules/post/repository_delete.go) · [资料更新](../backend/business/internal/modules/user/service_profile.go)

## 共享限流

Redis 数据类型均为 Hash。每个 Hash 保存剩余令牌数 `tokens` 和上次补充令牌的 Redis 时间戳 `last_refill_ms`。时间戳以毫秒为单位。Lua 脚本使用 Redis 时间计算令牌，并在更新桶状态时刷新 TTL。 [令牌桶脚本](../backend/business/internal/platform/ratelimit/limiter.go)

| 键格式 | 对象 | 容量 | 补充速率（令牌/秒） | 空闲 TTL |
| --- | --- | --- | --- | --- |
| `gopulse:rl:v1:auth_source:ip:ip:{source_ip}` | 注册等公开认证写入的来源 IP。策略前缀和身份标识各包含一次 `ip:`。 | 30 | 0.5 | 10 分钟 |
| `gopulse:rl:v1:login_attempt:source:subject:{source_ip}:{username_sha256}` | 登录来源 IP 与用户名摘要组合。 | 10 | 0.2 | 10 分钟 |
| `gopulse:rl:v1:login_attempt:source:ip:{source_ip}` | 登录请求无法提取有效用户名时使用的来源 IP。 | 10 | 0.2 | 10 分钟 |
| `gopulse:rl:v1:post_read:{identity}` | 受限业务读请求（`GET`、`HEAD`）。 | 60 | 10 | `REDIS_POST_DETAIL_TTL` 加 1 分钟，默认 6 分钟 |
| `gopulse:rl:v1:fact_write:{identity}` | 其余受限业务写请求。 | 20 | 2 | 10 分钟 |
| `gopulse:rl:v1:interaction_like:{identity}` | 点赞和取消点赞请求。 | 10 | 1 | 10 分钟 |
| `gopulse:rl:v1:creation:{policy}` | 每种策略的身份桶创建配额。仅当身份桶缺失时扣减。 | `post_read` 为 5,000，其余为 1,000 | `post_read` 为 100，其余为 10 | 10 分钟 |

`{identity}` 的格式为 `user:{user_id}` 或 `ip:{source_ip}`。已识别的业务用户使用用户 ID。公开认证请求使用来源 IP。`{username_sha256}` 是用户名去除首尾空白并转为小写后的完整 SHA-256 十六进制摘要。`{policy}` 取 `auth_source`、`login_attempt`、`post_read`、`fact_write` 或 `interaction_like`。 [路由与身份计算](../backend/business/internal/transport/http/middleware/protection.go) · [策略参数](../backend/business/cmd/server/protection.go)
