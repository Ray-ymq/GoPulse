# GoPulse Phase 13 实现 Review 报告

## 1. Review 信息

| 项目 | 内容 |
| --- | --- |
| Review 日期 | 2026-09-08 |
| 用户指定权威 Review 分支 | `develop/1.10.6` |
| 远端分支核对 | 执行 `git fetch --prune origin` 和 `git ls-remote --heads origin develop/1.10.6` 后，远端不存在 `origin/develop/1.10.6` |
| Review 分支创建方式 | 从最新 `origin/main` 创建本地 `develop/1.10.6`；未推送 |
| Phase 13 实现基线 | `7fe41b1c4165b28695ec926af12647a4b79c2f0b`（PR #118，Review 开始时与 `origin/main` 一致） |
| Phase 13 开工前基线 | `300140d`（Phase 13 规划合入后的主线） |
| Phase 13 主线提交 | `229a766`（Phase-13-01 / PR #114）、`5a6a3b7`（Phase-13-02 / PR #115）、`be38523`（Phase-13-03 / PR #116）、`0dfbd9a`（Phase-13-04 / PR #117）、`7fe41b1`（Phase-13-05 / PR #118） |
| 当前完成版本 | 根 `VERSION`、Frontend `package.json` 与 lockfile 均为 `1.10.5`；本次只新增 Review 文档，不修改版本 |
| `develop/1.10.6` 治理状态 | Phase 13 总实施方案只分配到 `1.10.5`；`validate_branch.py` 对 `develop/1.10.6` 返回 1，当前分支不能按仓库治理规则推送 |
| Review 环境 | WSL2/Linux 工作区 `/home/ray/GoPulse`；Go、Node/npm、Python、Docker Engine、Docker Compose 与 Chromium Playwright 均可用 |
| Review 范围 | Phase 13 总/拆分方案与实施记录、资料与用户发现、关注/Following/通知、收藏隐私、编辑/Redis/Elasticsearch 一致性、永久删除/墓碑/防复活、Frontend 用户端闭环、验收脚本、版本和分支治理 |
| 变更规模 | 相对 `300140d` 至实现基线：115 个文件，4727 行新增、207 行删除 |
| 结论 | **不通过（Fail）** |

本次 Review 重点判断：

1. 资料、关注、收藏、编辑和删除是否保持会话身份、作者权限及 `/me` 私有边界。
2. Redis 陈旧详情、Elasticsearch 陈旧命中、延迟或乱序事件是否会让已编辑/已删除内容重新可见。
3. 搜索在过滤已删除 MySQL 事实后是否仍保持可继续分页的完整用户体验。
4. 通知墓碑是否同时具备应用语义与数据库持久化形状约束。
5. Backend 新增错误码是否被 Frontend 完整识别并保留明确失败语义。
6. Phase 13 完成状态、版本分配和用户指定 `develop/1.10.6` 是否符合仓库治理合同。

本次没有修改产品代码。为稳定复现两个问题，Review 临时创建了一个 Go 测试和一个 Vitest 测试，执行后均已删除；它们未进入最终 diff、暂存区或提交。

## 2. 总体结论

Phase 13 已交付主体完整的基础社交产品闭环：

- 用户可以维护显示名称和个人介绍，按 username/display name 搜索用户，并从帖子、搜索和资料页完成用户发现。
- 关注关系使用唯一键、事务和 Outbox；Following 信息流和本人关注/粉丝列表使用当前会话身份，未暴露任意用户的公开关系列表。
- 收藏事实与公共计数分离，收藏列表和 `bookmarked_by_me` 按 viewer 水合，收藏游标还额外绑定当前用户。
- 帖子编辑将内容、`edited_at`、`content_revision` 和 `post.updated` Outbox 原子提交；详情缓存按 MySQL revision 校验，搜索投影采用外部 revision 防止旧更新覆盖新内容。
- 永久删除在同一事务中清理评论、点赞、收藏和帖子事实，并写入 `post.deleted`；历史通知通过 `ON DELETE SET NULL` 墓碑化，搜索 Indexer 对缺失 MySQL 事实执行幂等删除。
- 删除与 search reindex 使用同名 MySQL advisory lock，增量 Indexer 又在外部索引写入期间持有帖子行锁，已覆盖主要乱序和重建复活风险。
- `scripts/verify-business.sh` 在本次 Review 中完整通过，包含真实 Chromium 用户闭环、search rebuild/live、Outbox/Worker/Indexer 故障恢复及隔离资源清理。
- 后端全量 unit tests、`go vet ./...`、Frontend 65 项测试、typecheck、build、脚本安全自测和版本一致性检查均通过。

但是，当前实现仍不能通过 Phase 13 独立 Review：

1. 搜索先把 Elasticsearch 的 `limit` 个命中截断，再用 MySQL 过滤已删除事实；如果一页被陈旧删除命中占满，API 会返回“空数据 + next cursor”，而 Frontend 在空数据时隐藏“加载更多”，使后续有效帖子不可达。
2. `000010_post_deletion.up.sql` 为支持墓碑删除了通知形状 CHECK，却没有加入允许“活动资源或完整墓碑”的替代约束，持久层可以接受业务类型与 post/comment 组合不一致的通知。
3. Backend 新增的稳定错误码 `user_not_found` 未加入 Frontend 已知错误码和类型联合；Frontend 会保留 code/status，却把安全的明确消息替换为通用“操作失败”。
4. 用户指定的 `develop/1.10.6` 在远端不存在，也没有 Phase 13 权威批次分配，分支治理门禁失败。
5. Phase 13 总实施方案仍写着 Phase-13-05 待合入、未推送/未创建 PR，与 `origin/main` 已于 PR #118 合入的事实冲突。
6. 关注路由的计划合同使用 `:userId`，实际 Gin route 和参数读取却命名为 `:username`，虽然 Frontend 传数字 id 时可运行，但接口命名、错误字段和实现语义不一致。

本次未发现 P0 或 P1 问题；共记录 **4 个 P2、2 个 P3**。这些问题不否定已经能够运行的主闭环，但会破坏 Phase 13 明确要求的删除后搜索可用性、持久化通知合同、明确错误语义和权威分支治理，因此结论为 **Fail**。

## 3. Findings 摘要

| 编号 | 级别 | 问题 | 主要影响 |
| --- | --- | --- | --- |
| P2-01 | P2 | 陈旧删除搜索命中可产生空页并隐藏 continuation | Elasticsearch 尚未收敛时，仍存在的有效帖子可能在 UI 中不可达，用户被错误告知“没有找到” |
| P2-02 | P2 | 删除 migration 移除了通知形状 CHECK 且未替换 | 数据库可持久化 type/post/comment 不一致的通知，墓碑 DTO 和 Frontend 解析依赖不再由 schema 保证 |
| P2-03 | P2 | Frontend 未识别 `user_not_found` | 公开资料、未知关注目标等 404 丢失明确语义，Backend/Frontend 公共错误合同漂移 |
| P2-04 | P2 | `develop/1.10.6` 无远端引用和权威版本分配 | Review/整改分支无法通过 `validate_branch.py`，不能合规推送 |
| P3-01 | P3 | Phase 13 总实施方案的合入状态已过期 | 权威计划仍声称 Phase-13-05 未合入，与主线和完成条件不一致 |
| P3-02 | P3 | Follow route 参数名为 `username`，实际要求数字 user id | 路由自描述、错误字段和计划 API 合同不一致，增加调用方与维护者误用风险 |

## 4. Findings 详情

### P2-01：删除后的陈旧搜索命中可让有效结果在 UI 中永久不可达

**位置**

- `backend/internal/search/service.go:181-217`
- `backend/internal/post/repository.go:325-367`
- `backend/internal/search/service_test.go:235-242`
- `frontend/src/views/SearchView.vue:130-146`

**问题**

Phase-13-05 为防止删除后的 Elasticsearch 陈旧文档重新公开内容，允许 `FindMany` 跳过 MySQL 中已经不存在的 post id。这一方向正确，但分页顺序存在缺口：

1. Elasticsearch 每次只返回 `limit + 1` 个 hit。
2. `Search` 在 MySQL 水合前先把 hit 截断为 `limit`。
3. `FindMany` 再过滤掉缺失的 MySQL 事实。
4. API 可能因此返回零个 `posts`，同时返回非空 `next_cursor`。
5. `SearchView` 在 `resultCount === 0` 时显示“没有找到”，且整个加载更多区域受 `v-if="resultCount > 0"` 控制；即使 `nextCursor` 存在，也没有继续入口。

现有 `TestServiceFiltersDeletedSearchHits` 只覆盖单个陈旧 hit 被过滤，没有覆盖“陈旧 hit 填满当前页、后面仍有有效 hit”的情况。

本次临时 Go 测试构造 21 个有序 Elasticsearch hit、`limit=20`，令前 20 个已从 MySQL 删除、ID 21 仍存在。测试稳定失败：

```text
=== RUN   TestReviewPhase13RefillsPageAfterDeletedHits
    review_phase13_temp_test.go:23: empty visible page hides a reachable live hit; next_cursor_present=true
--- FAIL: TestReviewPhase13RefillsPageAfterDeletedHits (0.00s)
```

临时测试失败表示问题被成功复现；文件已删除。

**影响**

- 当 Search Indexer 暂停、重试积压或 Elasticsearch 删除尚未收敛时，多条刚删除的高排名结果可以占满首屏。
- API 虽没有泄漏已删除内容，但 Frontend 会错误显示“没有找到相关帖子”，并隐藏实际存在的 continuation。
- 位于下一批 hit 中的有效帖子无法通过当前 UI 到达；用户只能等待索引收敛、修改 query 或重新发起搜索。
- 这削弱了 Phase 13 “MySQL 是业务事实、投影延迟时仍安全且可解释”的搜索闭环。

**建议整改**

- Backend 在同一个 PIT 中循环读取后续 hit，直到收集到 `limit + 1` 个仍存在的 MySQL 事实或 Elasticsearch 已耗尽。
- continuation 应基于“已实际消费的最后一个 Elasticsearch hit”，不能基于被过滤后不存在的 `Post`。
- 对每轮读取设置明确上限或总扫描预算，避免在大量陈旧索引下形成无界请求；达到预算时仍应返回可继续 cursor。
- Frontend 也应在 `nextCursor` 非空时提供继续加载入口，即使当前页可见结果为零，作为防御性退化。
- 增加一条代表性测试：至少 `limit` 个陈旧删除 hit 后紧跟一个有效 hit，首个可操作响应不能形成无 continuation 的假空状态。

### P2-02：通知墓碑 migration 删除了持久化形状约束

**位置**

- `backend/migrations/000007_user_follows.up.sql:12-18`
- `backend/migrations/000010_post_deletion.up.sql:1-8`
- `backend/internal/notification/repository.go:118-158`
- `backend/internal/notification/model.go:35-46`

**问题**

Phase-13-02 的 `chk_notifications_comment_shape` 明确保证：

- `comment.created`：post/comment 均存在；
- `post.liked`：post 存在、comment 为空；
- `user.followed`：post/comment 均为空。

Phase-13-05 为允许帖子删除后把历史通知转成墓碑，在 `000010` 中删除了该 CHECK，并把 post/comment 外键改成 `ON DELETE SET NULL`。但是 migration 没有增加替代 CHECK。

因此最终 schema 可以接受例如：

- `comment.created`，`post_id IS NULL`，但 `comment_id IS NOT NULL`；
- `post.liked` 携带非空 `comment_id`；
- `user.followed` 携带 post 或 comment；
- 非 follow 类型只清空一个资源字段的半墓碑。

应用正常写入路径会先验证 `bus.Envelope`，所以现有 E2E 不会主动制造这些行；但数据库已经不再保护这个持久化不变量。`ListByRecipient` 只用“非 follow 且 post_id 为空”推导 `resource_deleted`，并不会再次验证 type/post/comment 的完整组合。

**影响**

- 数据修复脚本、未来 migration、管理工具或应用缺陷可以留下永久的半墓碑或类型错配行。
- Public notification DTO 可能输出与类型不一致的 post/comment id；Frontend 严格解析器会拒绝部分形状，导致整个通知分页响应变成 `invalid_response`。
- Phase 14 及后续若增加通知消费者，将无法把 schema 当作可信业务合同。
- 删除闭环依赖应用写入路径“永远正确”，低于 Phase 13 对持久数据和墓碑可解释性的要求。

**建议整改**

增加新的最终形状 CHECK，至少表达：

```text
comment.created:
  (post_id IS NOT NULL AND comment_id IS NOT NULL)
  OR (post_id IS NULL AND comment_id IS NULL)

post.liked:
  (post_id IS NOT NULL AND comment_id IS NULL)
  OR (post_id IS NULL AND comment_id IS NULL)

user.followed:
  post_id IS NULL AND comment_id IS NULL
```

同时：

- 在 migration 集成测试中实际插入代表性合法活动通知、合法墓碑和三种非法组合。
- `000010` down 的目标是保留历史墓碑还是恢复旧约束，应在 migration 注释和测试中明确；不能留下未声明的无 CHECK 状态。
- Repository 读取时可增加轻量形状校验，避免既有异常数据拖垮整页通知，但这不能替代数据库约束。

### P2-03：Frontend 把 `user_not_found` 降级成通用错误

**位置**

- `backend/internal/apperror/error.go:8-17`
- `backend/internal/http/response/response.go:67-77`
- `frontend/src/services/http.ts:26-45,89-101`
- `frontend/src/types/api.ts:59-78`
- `frontend/src/views/ProfileView.vue:26-42,71-72`

**问题**

Backend 已将 `user_not_found` 定义为稳定公开错误码，并映射为 HTTP 404。Phase-13-01/02 又要求未知 username/target 返回明确 not-found。

但是 Frontend：

- `knownErrorCodes` 没有 `user_not_found`；
- `ApiErrorCode` 联合类型也没有 `user_not_found`，并且还漏掉已在 runtime set 中存在的 `notification_not_found`；
- HTTP client 对未知 code 会保留 code/status，却把 Backend 安全消息替换为“操作失败，请稍后重试。”。

本次临时 Vitest 让 `/users/missing` 返回：

```json
{"error":{"code":"user_not_found","message":"user not found"}}
```

测试稳定失败，实际结果为：

```text
ApiError {
  code: "user_not_found",
  message: "操作失败，请稍后重试。",
  status: 404
}
```

临时测试失败表示前后端合同缺口被成功复现；文件已删除。

**影响**

- 访问不存在的资料页时，用户看到的是类似暂时性服务器故障的通用提示和“重试”，而不是资源不存在。
- 对已不存在 target 的关注操作也无法给出明确原因。
- TypeScript 的公开错误码定义与实际 Backend/runtime client 集合不一致，后续按 code 分支处理容易漏项。
- Phase 13 关于 unknown username/target 明确失败语义和可理解错误反馈的验收没有完整落到用户端。

**建议整改**

- 把 `user_not_found` 加入 `knownErrorCodes` 和 `ApiErrorCode`；同步把 `notification_not_found` 加入类型联合。
- ProfileView 对 `user_not_found` 显示稳定的“用户不存在”空状态，避免提供无意义的暂时性重试提示。
- FollowButton 可保留通用回滚，但应针对 `user_not_found` 提示目标已不存在或资料已失效。
- 增加 HTTP client 最小合同测试，逐个覆盖 Phase 13 新增公开错误码，不需要复制所有页面测试。

### P2-04：`develop/1.10.6` 没有权威分配，不能作为可推送开发分支

**位置**

- `dev/imple/Phase-13/Phase-13-总实施方案.md:69-76`
- `scripts/ci/validate_branch.py`
- 根 `VERSION`

**问题**

用户指定使用权威分支 `develop/1.10.6`，但实际核对结果为：

```text
git fetch --prune origin
# origin/develop/1.10.1 至 origin/develop/1.10.5 已删除；没有 origin/develop/1.10.6

git ls-remote --heads origin develop/1.10.6
# 无输出

python3 scripts/ci/validate_branch.py --branch develop/1.10.6 --base-ref origin/main
ERROR: develop/1.10.6 must map to exactly one authoritative allocation; found 0
```

Phase 13 总实施方案只定义五个执行批次，最终版本为 `1.10.5`。根 `VERSION` 也为 `1.10.5`，没有任何权威文件把 `1.10.6` 分配给 Review 或整改批次。

本次为了执行用户指定任务，只从最新 `origin/main` 创建了本地 `develop/1.10.6`，但不能把该本地分支描述为仓库已经存在的权威开发批次，也不应直接推送。

**影响**

- 当前 Review 分支无法通过仓库分支门禁。
- 若直接推送，会违反“每个 develop/x.x.x 必须映射唯一权威执行批次”的版本管理规则。
- 后续整改提交的目标版本、分支归属和是否更新 `VERSION` 都不明确。

**建议整改**

二选一明确治理方式：

1. 如果本次仅保存 Review 文档，将其作为 planning/documentation 工作提交到 `update`，不新增产品版本。
2. 如果要实施 Phase 13 Review 整改，在 `update` 上先更新权威总实施方案，明确新增整改批次、目标版本 `1.10.6`、分支 `develop/1.10.6`、验收范围和完成条件；然后从最新 `origin/main` 重新建立可验证分支。

在权威分配完成前，不推送当前本地 `develop/1.10.6`。

### P3-01：Phase 13 总实施方案仍记录“待合入”状态

**位置**

- `dev/imple/Phase-13/Phase-13-总实施方案.md:3`
- `dev/imple/Phase-13/Phase-13-总实施方案.md:323-326`
- `dev/imple/Phase-13/Phase-13-总实施方案.md:340-346`

**问题**

Review 基线 `origin/main` 已包含 PR #118，提交为 `7fe41b1`，五个 Phase 13 批次均已合入，根版本为 `1.10.5`。

但总实施方案仍同时写着：

- “Phase-13-05 待合入 main”；
- “本轮未推送或创建 PR”；
- 不能宣称五批均合入主远程。

这与同一文档第 17 节完成条件及当前主线事实冲突。

**影响**

- 后续 Phase 14 读取权威方案时无法确定 Phase 13 是“业务验收通过但未正式完成”还是“已经完成”。
- 自动或人工里程碑检查可能依据过期状态得出错误结论。
- Review/整改版本分配更容易在错误基线上继续扩展。

**建议整改**

- 在 planning 分支更新总实施方案状态，记录 PR #118、合入提交 `7fe41b1` 和最终版本 `1.10.5`。
- 删除或明确标注第 18 节最后一条为“合入前历史记录”，避免它继续作为当前状态陈述。
- 不因纯文档状态同步提升 `VERSION`。

### P3-02：Follow route 参数名称与实际 user-id 合同不一致

**位置**

- `dev/imple/Phase-13/Phase-13-02-关注关系与Following信息流闭环.md:37-53`
- `backend/internal/http/api.go:52-59`
- `backend/internal/http/user_handler.go:104-119`
- `frontend/src/services/api.ts:148-150`

**问题**

权威 split plan 定义：

```text
PUT /api/v1/users/:userId/follow
DELETE /api/v1/users/:userId/follow
```

Frontend 也实际传递数字 target id。但是 Gin route 使用 `:username`，Handler 又通过 `params.PositiveID(c, "username")` 把它解析为正整数。

因此运行路径在传数字时可用，但路由的自描述名称和错误字段声称它是 username；如果调用方按 route 名称传真实 username，会得到数字参数校验错误。

**影响**

- API route、计划合同和实现参数语义不一致。
- 日志、错误消息、路由测试和未来 API 文档生成可能把 target id 错写成 username。
- 维护者可能错误复用公开资料的 username 路由语义。

**建议整改**

- 将 route 改为 `/users/:userId/follow`，Handler 使用 `params.PositiveID(c, "userId")`。
- 增加一条 route-level 测试，证明数字 id 成功、username 字符串返回明确 validation error。
- Frontend URL 无需改变实际值，只需保持参数命名合同一致。

## 5. Phase 13 验收合同复核

| 领域 | Review 结论 | 证据/说明 |
| --- | --- | --- |
| 用户资料与发现 | 主闭环通过，但错误语义不完整 | Profile schema、trim/rune 校验、签名搜索游标、显示名称水合成立；P2-03 影响 unknown user UX |
| 关注关系与隐私 | 通过，存在低级合同漂移 | 唯一键、事务、Outbox、`/me` 私有列表、Following feed 均成立；P3-02 为参数命名不一致 |
| 收藏与 viewer 状态 | 通过 | 私有 bookmark 表、viewer-specific 水合、无公共计数、用户绑定 cursor、真实 E2E 均通过 |
| 帖子编辑 | 通过 | 作者行锁、revision、edited_at、Outbox、Redis revision 检查和 ES external version 合同成立 |
| 永久删除事实 | 通过 | 作者授权、同事务依赖清理、删除 Outbox、缓存不权威、重复/非作者语义均有实现和验收 |
| 搜索删除与防复活 | **部分不通过** | 乱序事件不复活成立；P2-01 使大量陈旧 hit 下的有效后续结果不可达 |
| 通知墓碑 | **部分不通过** | `ON DELETE SET NULL` 和 UI 墓碑成立；P2-02 缺少最终 schema shape CHECK |
| UserAppShell/Admin 隔离 | 通过 | Desktop/Mobile 用户路径与代表性管理员页面 E2E 通过 |
| Compose/业务验收 | 通过 | 本次 `scripts/verify-business.sh` 完整通过；实施记录另有 `verify-compose.sh --phase13` 成功证据 |
| 版本与分支治理 | **不通过** | `1.10.5` 元数据一致；`develop/1.10.6` 无权威分配，branch validator 失败 |
| 阶段文档完成状态 | **不通过** | P3-01：总方案仍保留合入前状态 |

## 6. 实际执行的验证

### 6.1 通过项

```text
git fetch --prune origin
# PASS；确认 origin/main = 7fe41b1，并确认远端 develop/1.10.6 不存在

cd backend
go test ./...
# PASS：全部 Backend command/package tests

go vet ./...
# PASS

cd ../frontend
npm test -- --run
# PASS：17 files / 65 tests

npm run typecheck
# PASS

npm run build
# PASS：Vite production build

cd ..
bash -n scripts/verify-business.sh scripts/verify-compose.sh scripts/verify-compose-observability.sh
# PASS

bash scripts/verify-business.sh --self-test
# PASS：1 个安全目标接受，6 个不安全目标在未访问 Docker 前被拒绝

bash scripts/verify-business.sh
# PASS
# - 真实 Chromium Phase 13/既有业务：10 tests，7 passed，3 skipped（环境条件型管理员用例）
# - targeted search-rebuild：1 passed
# - targeted search-live：1 passed
# - Phase 2 reliability fault matrix：10/10 passed
# - Backend/Worker/Indexer/Reindex 进程日志验证通过
# - 隔离 acceptance 资源按强归属规则清理

python3 scripts/ci/validate_versions.py
# PASS：版本元数据与根 VERSION 1.10.5 一致

git diff --check
# PASS
```

### 6.2 预期失败并形成 Finding 的检查

```text
python3 scripts/ci/validate_branch.py --branch develop/1.10.6 --base-ref origin/main
# FAIL：develop/1.10.6 authoritative allocation found 0
# 对应 P2-04
```

临时 Go 复现：

```text
go test ./internal/search -run TestReviewPhase13RefillsPageAfterDeletedHits -count=1 -v
# FAIL：empty visible page hides a reachable live hit; next_cursor_present=true
# 对应 P2-01；临时文件已删除
```

临时 Frontend 复现：

```text
npx vitest run src/services/review_phase13_temp.test.ts
# FAIL：user_not_found 的 message 从 "user not found" 被替换为“操作失败，请稍后重试。”
# 对应 P2-03；临时文件已删除
```

### 6.3 未重复执行项

本次没有再次运行 `scripts/verify-compose.sh --phase13`。原因是：

- 本次已独立执行并通过 `scripts/verify-business.sh` 的真实浏览器、搜索和可靠性完整门禁；
- 实现基线未在 Review 期间改变；
- `dev/logs/Phase-13/Phase-13-05-帖子永久删除与阶段收口.md` 已记录相同实现提交上的完整 Compose Phase 13 验收成功；
- 当前 Findings 可由代码和最小直接测试稳定复现，重复完整 Compose 不会改变结论。

这不表示本次声称重新执行了 Compose Phase 13 gate；报告只把它作为已有实施记录证据。

## 7. 建议整改顺序

1. **先完成治理决策**：Review 文档走 `update`，或先在总实施方案中正式分配 Phase 13 整改批次 `1.10.6` / `develop/1.10.6`；未分配前不要推送当前本地分支。
2. **修复 P2-01**：Backend 在同一 PIT 中补取可见 MySQL 事实，并让 Frontend 在空页但有 cursor 时仍可继续；用“整页陈旧 hit + 后续有效 hit”直接测试锁定。
3. **修复 P2-02**：为 notification 增加活动资源/完整墓碑二态 CHECK，并补 migration 正负集成测试。
4. **修复 P2-03**：同步 Backend/Frontend 错误码集合和类型，资料页明确处理不存在用户。
5. **同步 P3-01**：把 Phase 13 总实施方案更新为 PR #118 已合入、最终版本 `1.10.5`。
6. **修复 P3-02**：统一 Follow route 参数名为 `userId`。
7. 整改后运行受影响 Go tests、Frontend tests/typecheck/build、`verify-business.sh --self-test`、`verify-business.sh`、版本/分支治理和 `git diff --check`；只有通知 migration、搜索公共分页合同或 Compose 直接变化时，再按具体风险运行对应集成/Compose gate。

## 8. Review 边界与已知限制

- 本次按用户明确请求执行独立 Review，没有修改产品实现，也没有开展一般依赖升级、覆盖率活动或 Phase 14 功能审计。
- 未读取第三方依赖源码；所有判断来自项目方案、项目源码、实际命令和最小复现。
- 本次没有宣称 `develop/1.10.6` 是远端既存或已获权威分配的分支；它只是为满足 Review 工作建立的本地分支。
- 未执行远端 push、PR 创建、merge 或分支删除。
- 现有运行中的 `gopulse-p13-local` 资源未被手工修改；`verify-business.sh` 使用随机强归属 acceptance project，并在结束时完成自身资源清理和开发状态不变检查。
