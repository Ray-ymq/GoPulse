# Phase-13-03：收藏与私有内容列表闭环实施方案

> 当前状态：已完成（2026-09-07，`1.10.3`）；实际交付与验证见 `dev/logs/Phase-13/Phase-13-03-收藏与私有内容列表闭环.md`。本文档定义 Phase 13 第三个执行批次的范围与验收合同；目标版本 `1.10.3`、开发分支 `develop/1.10.3` 和执行顺序以 `Phase-13-总实施方案.md` 为准。实际开工前从最新 `origin/main` 创建本批分支。

## 1. 批次目标

在已交付资料、用户搜索、关注和 Following 的基础上，交付只属于当前 viewer 的收藏闭环，同时把 viewer-specific 状态正确接入所有帖子读取路径：

```text
帖子行/详情/搜索/Following/用户帖子
  → 当前用户收藏或取消收藏
  → GET /bookmarks 私有列表
  → 共享帖子缓存仍不含 viewer-specific 状态
```

批次完成必须证明收藏关系唯一、写入幂等、列表只属于本人、帖子删除时可清理，并且任何用户都不能通过缓存或 DTO 看到别人的收藏状态。

## 2. 前置条件

- Phase-13-02 已合入 `main`；following 字段、游标、用户壳层和通知模型可用。
- 从最新 `origin/main` 创建 `develop/1.10.3`，确认根和 Frontend 版本为 `1.10.2`。
- 核对帖子共享 Redis projection、帖子详情/列表/search hydration、现有 like viewer state 和 migration 外键策略。
- 保存 Phase-13-02 关注/Following/通知回归证据。

## 3. 实施范围

### 3.1 收藏 schema

- 新增 `post_bookmarks(post_id, user_id, created_at)`，主键或唯一键为 `(post_id, user_id)`。
- 帖子 FK 使用删除级联策略，使帖子永久删除时收藏事实不会残留；用户 FK 防止孤儿关系。
- 为当前用户列表创建 `(user_id, created_at DESC, post_id DESC)` 等价索引；游标必须签名/有界并能稳定处理同秒记录。
- 收藏不产生公共计数，不进入公开帖子 DTO 的 total count，不产生通知事件。

### 3.2 API 与 viewer-specific DTO

新增 protected routes：

- `PUT /api/v1/posts/:postId/bookmark`
- `DELETE /api/v1/posts/:postId/bookmark`
- `GET /api/v1/bookmarks`

扩展所有登录用户帖子 DTO（帖子列表、详情、作者帖子、Following、搜索）：

- 增加 `bookmarked_by_me: boolean`；未认证/不适用场景按既有 API 边界明确为 false 或不返回，不能模糊混用。
- bookmark/unbookmark 对已存在目标收敛到最终状态；未知 post 返回 not-found；重复调用不新增事实、不改变 created_at，错误响应不泄漏其他用户关系。
- 收藏列表只基于当前 session user id，不能接受 query 的 user id/username 作为读取主体。
- 读取收藏列表时过滤已删除/不存在帖子；不得把不存在内容渲染给浏览器。

### 3.3 缓存与批量读取

- 共享帖子缓存不写 `bookmarked_by_me`，也不写 `liked_by_me`、following 或权限按钮。
- 列表/搜索/详情在取得共享帖子 projection 后，按当前 user id 批量查询 bookmark relation，避免 N+1；为空 viewer 时不查询关系。
- 收藏列表可以复用帖子公共 projection，但必须再从事实库确认存在并水合当前 viewer 状态。
- cache hit、MySQL hit、search hit 三条路径都必须得到语义一致的 `bookmarked_by_me`。
- 收藏写入的 DB 成功不依赖 Redis 写成功；必要失效失败须可观察但不得泄漏其他 viewer 状态。

### 3.4 Frontend

- 所有已实现帖子动作行增加收藏按钮和稳定 accessible label；成功后同步按钮状态，失败时回滚乐观状态并展示错误。
- UserAppShell 收藏入口转为真实私有列表；空、加载、错误、继续加载和已删除跳过状态可解释。
- 收藏列表不显示收藏数，不为他人资料添加收藏入口；收藏关系没有通知。
- 资料页、Following、搜索和帖子详情复用同一 PostRow action contract，避免部分页面漏接状态。

## 4. 不在本批范围

- 帖子编辑、删除、历史版本、通知墓碑。
- 公共收藏数、收藏通知、收藏推荐或按他人查看收藏。
- 媒体、转发、私信、主题、管理员重构。

## 5. 建议实施顺序

1. 新增 bookmark migration、repository/service 和唯一约束测试。
2. 接入 bookmark routes、私有列表和通用 viewer-state hydration。
3. 检查 Redis/search/detail/list 各读取路径不把收藏状态放入共享 projection。
4. 接入 PostRow action、收藏列表和错误回滚 UI。
5. 增加成功/失败代表性单测、HTTP/集成测试、Frontend 测试和浏览器闭环。
6. 更新版本至 `1.10.3`，建立实施记录并提交。

## 6. 预计直接影响文件

- `backend/migrations/*`
- `backend/internal/post/*`
- `backend/internal/http/*`
- `backend/internal/platform/redis/*`
- `backend/internal/search/*`（仅需要调整结果水合时）
- `frontend/src/services/api.ts`
- `frontend/src/types/api.ts`
- `frontend/src/components/*`
- `frontend/src/views/*`
- `frontend/e2e/*`
- `VERSION`、Frontend 版本元数据及 `dev/logs/Phase-13/Phase-13-03-收藏与私有内容列表闭环.md`

## 7. 批次验收标准

### 7.1 数据、API 与隐私

- B 收藏 A 的帖子后只有一条 `(post_id, user_id)` 关系；重复/并发收藏仍一条，重复取消后最终未收藏。
- B 在所有帖子读取路径看到 `bookmarked_by_me=true`，取消后为 false；A 或未登录 viewer 不看到 B 的私有收藏事实。
- `GET /bookmarks` 只返回 B 自己收藏的现存帖子；不能通过参数读取 A 的列表；空列表和游标分页稳定。
- 未知帖子 bookmark 返回 not-found；权限和错误结构不泄漏其他用户关系；收藏不产生通知或公共计数。

### 7.2 Frontend 与缓存

- PostRow 收藏按钮在首页、Following、搜索、用户帖子、详情和收藏列表语义一致；失败请求回滚 UI。
- Redis 命中内容不包含 B/A 的收藏状态；两个不同 viewer 对同一帖子得到各自正确布尔值。
- 帖子删除前后（删除实现由后续批次完成）本批关系表和 API 设计已为 FK 清理准备，不用手工数据修复作为验收步骤。

### 7.3 完成条件

本节全部通过、实施记录真实完整、版本为 `1.10.3`、提交只包含本批文件时，Phase-13-03 才可标记完成。公共收藏数和收藏通知等范围外项必须停止扩展。

## 8. 固定验证命令与回归范围

```bash
cd backend
go test ./internal/post ./internal/http ./internal/platform/redis ./internal/search
go test -tags=integration ./internal/post ./internal/http

cd ../frontend
npm test
npm run typecheck
npm run build
npm run test:e2e -- --grep "bookmark|收藏"

cd ..
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/1.10.3 --base-ref origin/main
git diff --check origin/main...HEAD
```

- 集成测试使用 `INTEGRATION_TESTS=1` 和白名单依赖；记录实际命令。
- 回归至少覆盖登录、帖子列表/详情、点赞、评论、Following、通知和一条管理员路由。
- 只有 shared cache schema 或通用 DTO 出现具体跨页面风险时才扩大 Compose/全量 E2E，记录理由后执行。
- 成功检查在相关输入未变化时不重复执行。

## 9. 实施记录要求

完成前创建：

`dev/logs/Phase-13/Phase-13-03-收藏与私有内容列表闭环.md`

记录实际表结构、viewer-specific 水合和缓存边界、API/UI 文件、命令结果与交接给 Phase-13-04 的 bookmark 状态合同。
