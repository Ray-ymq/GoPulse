# Phase-24-02：发帖幂等与安全重试

> 状态：已完成（2026-10-12）。目标版本 `2.6.2`，分支 `develop/2.6.2`。
> 结果：发帖命令可在约定窗口内安全重试，用户端自动复用命令身份，新事件及其必要消费义务开始生效。
> 共同字段、资源与留存以[总方案](Phase-24-总实施方案.md)为准。

## 1. 进入条件与允许文件

01 已完成并进入主远端 main。必须先核对新 schema、v1/v2 消费者、真实索引代际、
完成屏障和恢复准备的有效检查，才从最新主远端 main 创建本批分支。
旧二进制/new schema 的不兼容不能用“字段可选”绕过。

| 允许修改 | 文件/目录与具体目的 |
| --- | --- |
| 创建与命令结果 | `backend/internal/post/{handler,service,repository,model,validation}.go`、对应测试；`backend/internal/businessstate/` 内命令/预算/清理 Repository 及测试；`backend/internal/apperror/` 的本批错误码 |
| API 与装配 | `backend/internal/http/api.go`、`router_post_test.go`、`post_integration_test.go`、`business_flow_integration_test.go`；`backend/cmd/server/roles.go` 及测试；必要的 response helper 及行为测试 |
| 新事件启用 | `backend/internal/bus/`、`outbox/`、`worker/`、`config/`；`post/{edit,delete}.go`、`comment/repository.go`、`like/repository.go`、`user/follow.go` 及直接测试，只补其他事件的 epoch 身份 |
| 用户端 | `frontend/src/views/NewPostView.vue`、`BusinessViews.test.ts`、`services/api.ts`、`types/api.ts`；拟新增 `frontend/src/composables/usePostCommand.ts` 及测试；现有 auth/logout 接线仅清理本用户草稿 |
| 实际浏览器路径 | `frontend/e2e/business.spec.ts`、`compose-business.spec.ts`；`devtools/internal/testenv/` 仅必要配置/用例选择 |
| 启用与恢复接线 | `backend/cmd/search-reindex/` 中 epoch 维护参数；`lifecycle/internal/control/recovery.go` 及测试；`.env.example`、`deploy/compose.yaml`、`deploy/runtime-contracts.json` / `.schema.json`、`devtools/internal/envfile/`，不新建常驻进程 |
| 合同与结果 | `dev/contracts/business-stages.md`、`runtime-contracts.md`、`migration-state.md`、`backend/README.md`、`dev/operations/frontend-product-experience.md`、`backup-restore.md`；同名日志、能力状态、总/分方案结果标记和成功后的版本元数据 |

不扩展评论/点赞/关注的命令幂等，不改现有编辑/删除外部契约。
本批不新增日志字段生产；下游严格日志兼容与关联在 03 先准备后启用。

## 2. 公开接口与结果合同

以下接口/字段为本批拟新增，正文、DTO 和真实调用方同批交付。

| 接口/情形 | 固定行为 |
| --- | --- |
| 原 `POST /api/v1/posts`，只有 title/content | 保持原校验和首次 `201 {data: Post}`；可增加 meta。每请求创建独立 command/event，不保证重试去重 |
| `GET /api/v1/post-commands/context` | 已登录用户读取当前 ready 的 data_epoch 与 7 日重试窗口，不返回私有配置；用户端在建立一次新命令前使用 |
| 带 `Idempotency-Key` 与 `X-GoPulse-Data-Epoch` 的创建 | 键为规范 UUID，epoch 必须等于当前 ready epoch；两者一起校验。非法/缺少配对字段 400，过时代际 `409 data_epoch_mismatch` |
| 同用户/epoch/类型/键、同规范摘要 | 返回已提交的同一 command_id、post_id、event_id、原成功状态和 committed_at；不再写帖子/Outbox/义务 |
| 同作用域键、不同规范摘要 | `409 idempotency_conflict`，不修改原命令；不同用户的相同键互不影响 |
| 成功响应 meta | 包含 command_id、event_id、data_epoch、post_id、committed_at、replayed；`data` 按当前授权读取 Post。成功状态仍为 201，结果身份稳定，不承诺正文快照不变 |
| 已提交帖后来删除 | 同键仍查到最小成功结果；`data: null` 与 `resource_state: unavailable`，不重建帖子或返回已删除正文 |
| `GET /api/v1/post-commands/result` | 同两项 header，只允许查询当前用户的该命令最小结果；200 为已提交；404 `command_result_unknown` 只表示无可用结果，不能解释为未执行 |
| COMMIT 返回错误/连接断开，无法确认结果 | 有界尝试查证；仍未知返回 `503 command_outcome_unknown`，提示使用同键同摘要查证/重试，不宣布确定失败或自动换键 |
| 已提交重试但当前调用者不再有效 | 重新认证并校验所有权；沿用 401/403/资源遮蔽语义。持久命令不构成越权入口 |

每次结果查询最多一个命令、500 ms 数据库期限，复用当前业务池与现有认证/请求准入；
响应不暴露 key hash、request digest 或依赖原文。未知/过期不自动代入当前 epoch 重新执行。
历史无命令记录的帖子不能由标题相同等启发式方法推断为同一次创建。

## 3. 创建事务、水位与重试窗口

现有 trim、Unicode 字符数及 title/content 校验继续作为规范化事实源。
摘要版本固定为 1，对规范化后的二字段确定编码做 SHA256；禁止依赖 JSON 原始字节、
字段顺序、进程随机值或 Redis 锁决定等价性。

同一 MySQL 事务先验证当前 epoch/ready，以唯一键争取命令范围，
再原子预留预算、写 posts、命令成功结果、v2 Outbox 和 search 必要义务，最后提交。
唯一冲突读已提交结果并比对摘要；未提交的竞争有界等待，
锁超时/死锁不能转成已成功，也不能另生成一个已提交命令。
事务内没有 Redis、RabbitMQ、ES、Logs/OTLP I/O。

创建成功后缓存或响应读取失败不能被包装为“创建未执行”；
至少给出稳定的最小提交结果，正文可标为暂不可读。
提交未知的查证使用独立的有界读 context；原请求取消不能让查证或后台任务无限运行。

预算由数据库跨 API 实例原子执行：当前 epoch 最多 10,000 个未闭合 search 义务，
跨全部 epoch 最多 100,000 组保留命令/事件、1 GiB 计费；
每组计费为实际编码 payload 字节加固定 1 KiB 元数据，按全局保留行→epoch 义务行的固定锁顺序预留。
计费及约束修订写入合同，不能当作实际磁盘占用。80% 水位判断供 03 只读查证；
本批不扩展共享指标目录或提前生产新日志属性。
达到硬限的新创建返回 `503 business_async_backlog`，事实/命令/Outbox 全部不写。
已经提交的重放先返回已有结果，不因当前水位再被拒；无键创建同样受水位约束。

首次完成只释放未闭合计数；完成后满 7 日且所有引用满足屏障才按批回收保留组/字节。
命令至少保留提交后 7 日，pending/dead 未闭合义务及其命令引用不能按超时直接删除。
结果保留超过最低窗口不扩大公开保证；删除的最小命令结果不保存正文快照。

## 4. 用户端交互与启用顺序

NewPostView 为每个新草稿取得当前 epoch、创建随机键，绑定当前 user_id、规范摘要和首次发送时刻。
会话内重试/刷新使用 sessionStorage 的同一草稿身份；按用户隔离，退出或更换账号清除，
不存 Cookie、token 或密码。提交结果确定前，不能修改正文后仍复用旧键。

传输失败或提交未知显示“提交结果尚未确认”，提供查证和同内容重试；提交确定后跳转帖子。
查证无记录仍保持未知，允许同键重试；epoch 改变或超过 7 日停止自动重试。
另起创建必须是用户明确的新操作，不能把超时自动变成第二篇帖子。
普通用户页面只说明提交/重试结果，不显示 Outbox、消费者、代际或遥测诊断流程。

启用步骤为：暂停写入 → 清点并排空可处理 v1 Outbox/队列 → 持久停放无法处理的历史意图 →
核验 v2 消费者及 current epoch 索引 → 激活生产/恢复修订 → 恢复写入。
retry/dead/正在处理项也在清点范围，不只检查 ready 或 published。
旧未升级进程必须停止；每种业务事件生产者统一携带 epoch，
post.created 同时写 command 和义务。恢复新协议在此 activation 后正式生效。
未知历史意图保留原 payload/出处，不补造新 command、epoch 或完成；不能无声丢掉后宣称已排空。

启用后关闭 v1 执行许可；历史结果仍可分类展示。
回退只使用接受 v2/新 schema 的兼容构建；重新部署不改变 epoch。
恢复/重置创建新 epoch 后，旧 header、旧事件、缓存和游标不得自动转换成新请求。

## 5. 已有覆盖、直接检查与固定门禁

| 行为 | 已有覆盖/缺口 | 本批必须证明 |
| --- | --- | --- |
| 原创建与原子性 | post/HTTP integration 已覆盖事实+Outbox；缺命令/义务 | 真实 MySQL：成功四项同事务；每个失败点全部回滚；无键原请求仍成功 |
| 并发重复与冲突 | 唯一事实测试可复用；缺跨实例命令唯一键 | 两业务实例同键 10 次并发，只有一个 command/post/event/义务；同摘要复用，不同摘要 409、跨用户隔离 |
| 提交/响应未知 | 既有请求/仓库测试；缺 commit 后丢结果 | 测试内驱动/代理在真实提交后返回错误、丢 HTTP 响应；同键可查证，不产生第二份事实；取消/锁超时有界 |
| 权限/删除/保留 | 原 auth/delete integration | 重放重授权，删除后只给最小结果；时间控制证明 7 日/未闭合屏障，不等待真实七天 |
| 水位 | 01 已有原子计数基础 | 真实数据库低限 fixture 跨实例争最后配额；拒绝无部分写入；完成/清理释放一次，重放不占新配额 |
| 用户重试 | 现有 BusinessViews 与两套业务浏览器 | Vitest 证明键生命周期/账号隔离/过期；Playwright 真实刷新、丢响应后查证/重试，只出现一篇帖子 |
| 新事件激活 | 01 双版本消费者 | 六类新事件携带 epoch；仅新发帖产生 command/必要义务；恢复后旧事件拒绝，既有业务回归通过 |

固定命令均为现有入口，在仓库根执行；新用例必须进入实际选择列表。

```bash
rtk proxy make verify MODULE=backend BASE_REF=upstream/main
rtk proxy make verify MODULE=frontend BASE_REF=upstream/main
rtk proxy make verify MODULE=lifecycle BASE_REF=upstream/main
rtk proxy make verify MODULE=devtools BASE_REF=upstream/main
rtk proxy make integration SCOPE=business
rtk proxy make e2e SCOPE=business
rtk proxy make check-compose
rtk proxy make docs-check BASE_REF=upstream/main
```

真实验证预计 60 分钟、累计上限 120：环境/必要构建 20/40、integration/浏览器/激活预检
25/50、诊断和清理 15/30。一次一个 owned 项目，启动最多 7 分钟，
激活/排空短预检最多 10 分钟、每个异步结果最多 60 秒、清理最多 5 分钟。
最多两次各 10 分钟最小诊断包含在上限；第一次基础设施失败即停整组。

完成条件：表中行为与固定门禁通过，所有实际生产者/消费者兼容，新协议启用及恢复停点有记录；
同名日志写明只有带配对身份的发帖有安全重试承诺。
成功后同步 `2.6.2` 元数据并完成治理校验/提交，不提前声明管理端阶段查证已交付。

## 6. 实际完成结果

- 已完成 migration 15：将既有 Outbox 的 schema-version 约束扩展为 `1/2`；新增命令、预算、完成证据和迁移故障路径的测试保持在真实 MySQL 入口内。
- 业务角色启动时执行当前环境的持久 activation gate；数据库中的 v1 Outbox `pending/leased` 未排空时拒绝切换。切换后发帖命令把规范摘要、帖子事实、v2 `post.created`、Outbox 行和 `search` 义务放入同一事务，其他已有业务事件通过共享 Outbox 补齐当前 epoch。
- 发帖 HTTP/API 和用户端已交付配对身份、稳定结果、冲突、未知结果、删除后最小结果、7 日窗口和按用户隔离的 sessionStorage 草稿；未保存 token、key hash 或依赖配置，本地 digest 仅用于检测正文变更，不作为服务端凭据。
- 真实业务集成和浏览器 E2E 已证明 10 路同 key 并发只产生一条 command/post/event/义务；同摘要重放、摘要冲突、跨用户隔离、删除后查证、Outbox→RabbitMQ→Search 完成及既有通知闭环均通过。
- 固定门禁已通过：backend/frontend/lifecycle/devtools verify、business integration、business E2E、Compose 静态检查和 docs-check；版本元数据已同步为 `2.6.2`，两套前端依赖已按锁文件重新安装。
- activation 不代替外部 rollout 编排对 RabbitMQ unacked/retry/dead 队列和 ES 当前 generation 的检查；上线前仍须按本方案停止旧进程、排空或持久停放历史意图、核验当前搜索代际，再启动本批构建。未完成 Phase-24-03/04/05 的管理查证、平台适配和完整 V1 冻结矩阵。
