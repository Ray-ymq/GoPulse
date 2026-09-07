# Phase-13-02：关注关系与 Following 信息流闭环实施方案

> 当前状态：本地实施与验收完成（2026-09-07），尚未合入 main；实际证据见同名 `dev/logs/Phase-13/` 开发记录。本文档定义 Phase 13 第二个执行批次的范围与验收合同；目标版本 `1.10.2`、开发分支 `develop/1.10.2` 和执行顺序以 `Phase-13-总实施方案.md` 为准。实际开工前从最新 `origin/main` 创建本批分支。

## 1. 批次目标

在 Phase-13-01 的资料、用户搜索和 UserAppShell 基础上，交付权限清晰、并发幂等、可通知、可分页的关注关系闭环：

```text
搜索/作者/资料页
  → 关注或取消关注
  → 本人关注/粉丝列表
  → Following 时间线
  → user.followed outbox
  → Business Worker 通知中心
```

批次完成必须证明：重复或并发关注只产生一条关系和一条关注通知；取消关注幂等且不通知；任意用户不能通过 API 或 UI 读取别人的关系列表；Following 只使用当前登录用户的关注事实。

## 2. 前置条件

- Phase-13-01 已合入 `main`，资料 API、用户搜索、帖子 author 摘要、UserAppShell 和帖子时间线基线可用。
- 从最新 `origin/main` 创建 `develop/1.10.2`，确认根版本和 Frontend 版本为 `1.10.1`。
- 核对现有 outbox、RabbitMQ publisher、Business Worker、notification repository/schema、认证中间件和帖子分页边界。
- 保存 Phase-13-01 普通用户与管理员回归证据；本批只扩展受影响路径。

## 3. 实施范围

### 3.1 关注关系 schema 与领域合同

- 新增 migration `user_follows`，字段至少为 `follower_id`、`followed_id`、`created_at`；`(follower_id, followed_id)` 主键或等价唯一约束。
- 为 follower lookup 和 followed lookup 建立符合分页排序的索引，使用 `created_at DESC` 加稳定用户 id 消除同秒歧义。
- 外键约束保证用户删除不残留关系；应用层和数据库约束共同拒绝 self-follow。
- 所有写入在事务内完成；不得以“先查是否存在、再插入”替代唯一约束和冲突处理。
- 关系查询只能返回当前登录用户自己的 following/followers，禁止任意用户公开关系列表。

### 3.2 API 与授权

新增并接入 protected routes：

- `PUT /api/v1/users/:userId/follow`
- `DELETE /api/v1/users/:userId/follow`
- `GET /api/v1/users/me/following`
- `GET /api/v1/users/me/followers`
- `GET /api/v1/posts/following`

并扩展已有公开资料、帖子 author 和搜索用户响应：

- 对目标用户显示当前 viewer 的 `following` 状态，但不泄漏 target 的完整关系集合。
- Following feed 使用登录用户 id 从 DB 查询关注作者，稳定倒序分页；不得从客户端传入 author id 或“关注列表快照”。
- `PUT` 重复调用返回关系仍存在的幂等成功；并发调用最终只有一条关系和一个可消费事件。
- `DELETE` 重复调用返回“不关注”的幂等成功；取消关注不产生 `user.unfollowed` 通知。
- self-follow、未知 target、非法 limit/cursor 使用既有明确错误结构；不得因关系状态差异泄漏非当前 viewer 列表。

### 3.3 Outbox、总线和通知

- 引入 `user.followed` 事件类型，关系事实与 outbox event 在同一 MySQL transaction 提交。
- 复用现有 publisher/worker retry、ack、source event id 去重机制；为 `user.followed` 定义最小安全 payload（follower/followed id、事件 id、创建时间），不放 token 或密码。
- Notification 表/模型/DTO 泛化为可表达无 post/comment 的关系通知：actor、recipient、type、read、created、source event 必填；post/comment 可空。
- 关注通知接收者为被关注用户；关注者不能给自己制造通知。
- Notification repository 与 list handler 对新类型有确定文本、actor 资料和无资源链接语义；旧 comment/like 通知行为保持。
- 相同 source event id 的重放、worker retry 或 publisher duplicate 只落一行通知。

### 3.4 Frontend 交互

- 资料页、用户搜索结果、帖子作者摘要提供可访问关注/取消关注按钮；按钮只在目标用户不是本人时出现。
- 首页 tabs 变为真实 `全部 / Following`；Following tab 进入新 feed，保留 loading、empty、error、load-more 状态。
- 私有关系页提供本人关注和粉丝列表；刷新/直接访问仍受认证守卫保护。
- 通知页增加关注通知；关系 API 失败时回滚乐观状态，并展示可理解错误。
- 继续遵守 X-inspired 用户端合同：连续时间线、1px 分隔线、只显示已实现动作，不引入推荐流或公开关系列表。

## 4. 不在本批范围

- 收藏、收藏数、编辑、删除、删除墓碑。
- 任意用户公开 followers/following 列表、私密账号、审批、拉黑或举报。
- 推荐算法、排序热度、转发、私信、头像媒体。
- 管理端视觉改造或通知系统完全重写。

## 5. 建议实施顺序

1. 新增 user_follows migration、repository 和事务 service，覆盖 self-follow、duplicate/concurrent、idempotent delete。
2. 扩展用户/帖子 DTO，增加当前 viewer following 状态和仅 `/me` 关系查询。
3. 接入 follow/unfollow/feed HTTP routes 与游标错误合同。
4. 泛化 notification schema/DTO/repository，接通 `user.followed` outbox、总线和 Worker 消费。
5. 接入资料、搜索、作者行、Following tab、私有关系页和通知 UI。
6. 运行受影响 package 测试、Frontend tests/typecheck/build、代表性浏览器路径和治理检查。
7. 更新版本至 `1.10.2`，建立实施记录并提交。

## 6. 预计直接影响文件

- `backend/migrations/*`
- `backend/internal/user/*`
- `backend/internal/post/*`
- `backend/internal/bus/*`
- `backend/internal/outbox/*`
- `backend/internal/notification/*`
- `backend/internal/worker/*`
- `backend/internal/http/*`
- `backend/internal/platform/*`（仅直接受影响的 publisher/cache 适配）
- `frontend/src/services/api.ts`
- `frontend/src/types/api.ts`
- `frontend/src/router/*`
- `frontend/src/components/*`
- `frontend/src/views/*`
- `frontend/e2e/*`
- `VERSION`、Frontend 版本元数据及 `dev/logs/Phase-13/Phase-13-02-关注关系与Following信息流闭环.md`

实际 diff 必须保持在本批闭环；不得提前实现收藏、编辑、删除。

## 7. 批次验收标准

### 7.1 关系事实与授权

- A 不能关注自己；未知 user id 返回 not-found；不存在任意用户公开关系列表端点。
- B 关注 A 后关系表恰有一条记录；重复请求和至少一次并发代表性请求仍恰有一条记录、一条 outbox 事件和一条通知。
- B 取消关注后关系最终不存在；重复取消仍为幂等成功且没有新增通知。
- Following feed 只包含关注作者的现存帖子，分页稳定，不接受客户端伪造 viewer 或 author 集合。

### 7.2 通知

- A 的通知列表出现关注通知，显示关注者公开资料；通知不要求 post/comment id，不产生错误链接。
- 旧评论/点赞通知继续可读；通知分页、已读和 source event 去重行为不回归。
- Worker、消息重放和短暂重试不会重复通知；积压恢复后最终收敛。

### 7.3 Frontend

- 搜索结果、作者行、资料页关注按钮在成功后状态一致；失败时可靠回滚。
- `全部 / Following` tabs 可访问；Following 空状态、错误和分页可操作。
- 关注/粉丝列表只在当前用户的私有入口出现；其他用户资料页没有公开关系链接或数量。
- 通知页可以区分关注、评论和点赞；管理员路由和 UserAppShell 隔离。

### 7.4 完成条件

上述数据、权限、通知、Following、UI 和回归项全部通过，实施记录真实完整，版本为 `1.10.2`，提交只包含本批文件时，Phase-13-02 才可标记完成。推荐、公开关系列表和社交扩展记录为后续并停止本批工作。

## 8. 固定验证命令与回归范围

```bash
cd backend
go test ./internal/user ./internal/post ./internal/notification ./internal/outbox ./internal/worker ./internal/http
go test -tags=integration ./internal/user ./internal/post ./internal/notification ./internal/worker

cd ../frontend
npm test
npm run typecheck
npm run build
npm run test:e2e -- --grep "follow|Following|notification"

cd ..
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/1.10.2 --base-ref origin/main
git diff --check origin/main...HEAD
```

- 集成测试必须按仓库白名单设置 `INTEGRATION_TESTS=1`，使用独立 test DB/Redis；实施记录写实际环境和结果。
- 回归至少包括注册/登录、帖子创建、评论/点赞、旧通知、帖子搜索、一条管理员页面访问。
- 只有修改共享消息/通知基础设施或认证/CSS 边界时，才基于具体风险扩大到 Compose 全栈检查，并在实施记录写明。
- 成功检查在相关输入未变化时不重复执行。

## 9. 实施记录要求

完成前创建：

`dev/logs/Phase-13/Phase-13-02-关注关系与Following信息流闭环.md`

记录实际关系 schema、事务/事件去重、通知兼容方案、Frontend 路由和所有实际验证命令；把交给 Phase-13-03 的 following 状态字段、通知合同和游标行为写清楚。
