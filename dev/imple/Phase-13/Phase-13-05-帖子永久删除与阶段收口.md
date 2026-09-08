# Phase-13-05：帖子永久删除与阶段收口实施方案

> 当前状态：本批实现与验收已完成（2026-09-08）。实际证据见 `dev/logs/Phase-13/Phase-13-05-帖子永久删除与阶段收口.md`；Phase 13 总里程碑仍待本批合入 `main`。本文档定义 Phase 13 第五个执行批次和阶段收口的范围与验收合同；目标版本 `1.10.5`、开发分支 `develop/1.10.5` 和执行顺序以 `Phase-13-总实施方案.md` 为准。实际开工前从最新 `origin/main` 创建本批分支。

## 1. 批次目标

在 Phase-13-04 的 author edit、revision、缓存和搜索收敛合同上，交付不可恢复的作者永久删除，并完成 Phase 13 跨批端到端验收：

```text
作者在 UI 二次确认永久删除
  → DELETE /posts/:postId 作者授权
  → MySQL 删除 post + comments + likes + bookmarks
  → 历史通知保留为墓碑
  → post.deleted outbox（同事务）
  → Redis 无论是否残留 key 都不可返回帖子
  → Search Indexer 幂等删除
  → 乱序 create/update 不得复活
```

批次完成必须证明删除不存在半完成状态，陈旧缓存/索引/消息不能重新公开内容，历史通知仍可解释且不泄漏已删标题或正文；同时完成资料、关注、Following、收藏、编辑、删除的阶段总闭环和既有产品回归。

## 2. 前置条件

- Phase-13-04 已合入 `main`，根与 Frontend 版本为 `1.10.4`；post revision、`post.updated`、cache freshness 和 search hydration 已通过。
- 从最新 `origin/main` 创建 `develop/1.10.5`。
- 核对 comments、post_likes、post_bookmarks、notifications 的 FK/索引，现有删除/清理顺序，outbox payload 和 Search Indexer idempotent delete 能力。
- 核对 `scripts/verify-business.sh`、Compose business Playwright、普通用户/管理员身份准备和强归属资源清理合同。
- 保存 Phase-13-01 至 04 已成功且未受本批输入变化影响的检查证据，不因阶段收口无条件重复全部检查。

## 3. 实施范围

### 3.1 永久删除 API 与事务

新增 protected route：

- `DELETE /api/v1/posts/:postId`

具体合同：

- 只有帖子作者可以删除；非作者 `403 permission_denied`，不存在 `404`。不得接受 body 中的 owner/actor id。
- 删除是物理永久删除，不提供恢复或软删除状态。
- 帖子、评论、点赞、收藏的清理在一个 MySQL transaction 中完成，可通过 FK cascade 或明确 delete 顺序实现；不能留下可见孤儿数据。
- `post.deleted` outbox 与删除事实在同一 transaction 提交。删除失败则事实和 event 均回滚；成功后不依赖 Redis/Elasticsearch 在线。
- 对网络重试后的第二次 delete 使用明确 not-found 或幂等 empty success，实施时固定并测试；无论选择何种 HTTP 表现，都不得重复产生 delete event 或破坏墓碑。

### 3.2 历史通知墓碑

- 调整 notification schema，使关联帖子/评论删除时保留 notification row；优先将相关 FK 改为 nullable + `ON DELETE SET NULL`。
- 通知保留 type、recipient、actor、created/read 和 source event；删除后不保留或返回已删标题、正文、comment body 的投影。
- DTO 提供显式 `resource_deleted`、resource state 或等价稳定字段，Frontend 统一渲染“原内容已删除”。
- 已删除资源不生成链接、router navigation 或可点击标题；关注通知不受帖子删除影响。
- notification list/mark-read 分页和 source event uniqueness 保持；migration 必须安全处理已有数据。

### 3.3 Redis 删除不可见合同

- delete 成功后主动删除相关 Redis keys；失败可记录/重试，但不能成为 HTTP 成功后的正确性前提。
- 帖子详情先以 MySQL existence/revision 判定权威事实；即使故意保留旧 Redis key，也必须返回 not-found 并清理 stale key。
- 全部时间线、Following、用户帖子和收藏列表都以现存 MySQL post 为准，不从 cache id 集合返回已删内容。
- 评论/点赞/收藏 viewer state hydration 对缺失 post 不产生响应对象。

### 3.4 Elasticsearch 删除、防乱序复活与水合

- `post.deleted` 消费执行幂等 document delete；文档已不存在视为成功。
- 所有 `post.created`、`post.updated` 事件消费前重读 MySQL；post 不存在时统一收敛为 delete，不从 event payload 重建。
- 构造至少一条乱序代表性测试：先完成 delete，再处理旧 create/update event，最终 Elasticsearch 仍无文档。
- Search API 对命中但 MySQL 已缺失的 stale hit 过滤，不向浏览器返回标题/正文；可触发 best-effort cleanup/可观察记录。
- Indexer 恢复和队列重放后自动收敛，不要求人工 delete index、清 alias 或全量 reindex 才能完成验收。

### 3.5 Frontend 删除交互

- 作者帖子菜单增加危险操作“删除”；非作者没有入口。
- 第一次点击只打开 modal，不立即请求；modal 明确说明永久、不可恢复及将清理互动数据，并显示可识别的帖子标题或摘要。
- 只有明确“永久删除”按钮发请求；取消、Escape、返回焦点和提交中状态正确。删除按钮使用危险色但颜色不是唯一提示。
- 成功后从当前时间线/详情/收藏等视图移除并进入可解释页面；失败时保留内容和 modal 状态，允许重试。
- 通知墓碑显示“原内容已删除”，无失效链接，不显示已删内容。

### 3.6 阶段总 E2E 与收口

在真实 Compose 产品环境建立至少用户 A、用户 B 和管理员，完成：

1. A 修改 display name/bio；B 通过 username 和 display name 找到 A。
2. B 关注 A，A 收到唯一关注通知；B 在 Following 看到 A 新建的帖子。
3. B 收藏帖子并在本人收藏列表看到；A/其他用户不能读取 B 的私有关系列表或收藏列表。
4. A 编辑标题/正文；详情、列表、缓存和搜索水合显示最新内容，新/旧关键词按收敛合同变化。
5. B 编辑和删除均被拒绝；A 通过二次确认永久删除。
6. 删除后 comments/likes/bookmarks 已清理，详情 not-found，stale Redis 不可见，Elasticsearch 最终删除且旧事件不复活。
7. 历史帖子相关通知仍存在并墓碑化，关注通知正常。
8. 既有注册、登录、评论、点赞、通知已读、帖子搜索和管理员代表性可观测页面不回归。

## 4. 不在本批范围

- 软删除、回收站、恢复、数据导出、管理员代删除、批量删除。
- 历史版本、删除后匿名化用户、账号删除。
- 推荐、媒体、私信、转发、举报或管理端改版。
- Kubernetes 或 Phase 14 插件扩展。

## 5. 建议实施顺序

1. 调整 notification FK/nullable schema，先验证已有 comment/like/follow 通知兼容。
2. 实现 author delete service/transaction、依赖清理和 `post.deleted` outbox。
3. 接入 Search Indexer delete 与 missing-fact convergence，增加乱序不复活测试。
4. 接入 Redis stale-key rejection 和所有列表/搜索水合过滤。
5. 完成 delete modal、成功导航、失败恢复和通知墓碑 UI。
6. 增加本批直接测试，运行最终 Compose 跨批 E2E、既有业务和管理员代表性回归。
7. 更新根/Frontend 版本至 `1.10.5`，完成本批实施记录并同步总实施方案阶段状态（仅在实际验收全部通过后）。
8. 提交并停止 Phase 13；范围外项记录交给 Phase 14/后续。

## 6. 预计直接影响文件

- `backend/migrations/*`
- `backend/internal/post/*`
- `backend/internal/comment/*`
- `backend/internal/like/*`
- `backend/internal/notification/*`
- `backend/internal/outbox/*`
- `backend/internal/search/*`
- `backend/internal/worker/*` 或 Search Indexer 消费位置
- `backend/internal/platform/redis/*`
- `backend/internal/http/*`
- `frontend/src/types/api.ts`
- `frontend/src/services/api.ts`
- `frontend/src/components/*`
- `frontend/src/views/*`
- `frontend/e2e/compose-business.spec.ts` 或 Phase 13 专用等价 spec
- `scripts/verify-business.sh`（仅实际需要扩展 Phase 13 产品门禁时）
- `VERSION`、Frontend/Compose 受管版本元数据
- `dev/logs/Phase-13/Phase-13-05-帖子永久删除与阶段收口.md`
- `dev/imple/Phase-13/Phase-13-总实施方案.md`（仅真实阶段完成状态与证据）

不得把独立代码审查报告、一般依赖升级、管理端重构或 Phase 14 工作加入本批默认范围。

## 7. 批次验收标准

### 7.1 删除事实与权限

- 作者删除成功；非作者明确拒绝；不存在/重复请求符合固定语义。
- post、comments、likes、bookmarks 在同一最终事务边界内被清理；失败注入不留下半删除。
- delete event 与事实原子提交并可去重；Worker/Indexer 暂停时 MySQL 仍立即不再返回帖子。

### 7.2 墓碑、缓存和搜索

- 删除前产生的 comment/like 通知行保留，响应只显示“原内容已删除”和允许元数据，无失效链接或正文泄漏；follow 通知不受影响。
- 有意制造 stale Redis detail key 后，详情仍 not-found；所有列表、Following、收藏和搜索都不返回该帖子。
- Elasticsearch 删除幂等；delete 后重放旧 create/update，最终仍无文档。Indexer 恢复后自动收敛。

### 7.3 UI 与阶段闭环

- 只有作者看到删除入口；modal 具备不可逆说明、二次确认、焦点管理、提交中和失败恢复。
- 第 3.6 节完整业务链在 Desktop 主路径通过，并对 Mobile 关键导航、编辑/删除 modal 和 bottom nav 做代表性验证。
- UserAppShell X-inspired 设计合同成立，AdminLayout 和至少一条管理员 Metrics/Logs/Events/Exporter 访问路径未回归。

### 7.4 完成条件

只有以下条件全部满足，Phase-13-05 和 Phase 13 才能完成：

- 本批数据、权限、缓存、搜索、通知墓碑和 UI 验收全部通过。
- Phase-13-01 至 05 的跨批 E2E 与必要既有业务/管理员回归通过，无阻断问题。
- 根/Frontend/受管版本元数据为 `1.10.5`。
- 5 个 split plan 均有真实实施记录，总实施方案只在证据齐全后更新为完成。
- 最终提交只包含本批和真实阶段收口文件。

达到完成条件后停止；非阻断动画、暗色主题、更多设备像素、推荐/媒体/公开关系能力只记 follow-up。

## 8. 固定验证命令与回归范围

实施中先跑直接受影响 package；最终 diff 的固定门禁为：

```bash
cd backend
go test ./internal/post ./internal/comment ./internal/like ./internal/notification ./internal/search ./internal/outbox ./internal/worker ./internal/http ./internal/platform/redis
go test -tags=integration ./internal/post ./internal/notification ./internal/search ./internal/worker ./internal/http

cd ../frontend
npm test
npm run typecheck
npm run build
npm run test:e2e -- --grep "Phase 13|profile|follow|Following|bookmark|edit|delete|notification"

cd ..
bash scripts/verify-business.sh --self-test
bash scripts/verify-business.sh
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/1.10.5 --base-ref origin/main
git diff --check origin/main...HEAD
```

- `scripts/verify-business.sh` 使用 Phase 12 规定的强归属 Compose project、测试身份和清理机制；不得连接非白名单产品数据。
- 如果脚本本批没有变化，先前已成功的脚本自测在环境/依赖未变化时可引用；但最终真实 Phase 13 Compose business verify 必须运行并记录。
- 管理员回归只选本方案要求的代表性路径，不默认展开独立 review、全依赖审计或覆盖率活动。
- 若修改 notification FK、共享 Redis、消息协议或 search alias 引发具体跨模块风险，可以扩展相应检查；实施记录先写风险原因。
- 任何命令因环境不可用未执行或失败，都必须如实记录，不能将批次标为完成。

## 9. 实施记录要求与 Phase 14 交接

完成前创建：

`dev/logs/Phase-13/Phase-13-05-帖子永久删除与阶段收口.md`

记录必须包括：

- 实际 delete transaction、FK/migration、notification tombstone、Redis existence check 和 Search Indexer 防复活实现。
- 实际跨批 E2E 用户、数据和顺序，实际命令、结果及制品位置。
- 与本方案偏差、故障注入、已知限制和非阻断 follow-up。
- Phase 14 可依赖的最终 API/event/cache/search/UI 边界，以及 `1.10.5` 已完成产品基线。
