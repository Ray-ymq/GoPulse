# Phase-13-05：帖子永久删除与阶段收口开发记录

## 1. 完成状态

- 日期：2026-09-08；本批实现、固定门禁和完整 Compose 阶段业务验收已通过，无遗留阻断问题。
- 开工前执行 `git fetch origin`，从最新 `origin/main`（`0dfbd9a`，完成版本 `1.10.4`）创建 `develop/1.10.5`，未沿用已完成的 `develop/1.10.4`。
- 完成版本：根 `VERSION`、Frontend package/lockfile、`.env.example` 的受管版本已同步为 `1.10.5`。
- Phase-13-01 至 05 的镜像开发记录齐全。本批尚未推送、创建 PR 或合入 `main`；总方案标为“阶段业务验收通过，05 待合入”，不提前宣称五批均已合入或正式阶段里程碑已完成。

## 2. 实际实现与边界

### 永久删除与事务

- 新增 protected `DELETE /api/v1/posts/:postId`，只采用会话 actor 和数据库 author，非作者返回 `403 permission_denied`。
- 成功返回无正文 `204`；不存在和重复删除固定返回 `404 post_not_found`，不重复生成事件。
- MySQL 行锁串行化作者校验、编辑和删除；同一事务依次清理 bookmarks、likes、comments、posts，并写入 `post.deleted` outbox。outbox 失败时所有事实及 FK 墓碑变化回滚。
- 新事件只带稳定 ID/时间等元数据，不包含标题、正文或评论内容；补齐 bus 校验、routing key、RabbitMQ search/retry/dead 绑定及 publisher 分流。

### 通知墓碑

- `000010_post_deletion` 将通知 post/comment FK 改为 `ON DELETE SET NULL`，移除阻止资源归空的旧 shape CHECK；保留通知 type、actor、recipient、source event uniqueness、创建/已读时间。
- DTO 新增 `resource_deleted`；帖子相关通知缺失 post 时返回墓碑，关注通知仍为正常通知。通知本身不保存已删标题、正文或 comment body 投影。
- 延迟通知消费对 post 加共享锁；如果事实已删除，将 post/comment reference 置空后插入唯一墓碑，而不是因外键失败反复重试或丢失历史。
- Frontend 墓碑显示“原内容已删除”，不生成帖子链接，分页和 mark-read 保持可用。

### Redis 与 Elasticsearch

- 删除提交后 best-effort invalidation；缓存是否在线不影响删除事实。detail cache hit 必须通过 MySQL revision/existence 核验，不匹配或缺失时尝试清理 stale key。
- 列表、Following、用户帖子、收藏继续从 MySQL 现存 post 出发。搜索 `FindMany` 跳过已缺失事实，service 不再要求 ES 命中数量等于 MySQL 水合数量，避免陈旧命中泄漏或返回 500。
- Search Indexer 在 MySQL post 行锁期间完成权威读取与 ES 写入，避免旧快照在删除事实/事件之后写回。缺失事实的 create/update/delete 全部走幂等 `DeleteAlias`，不从 payload 重建内容。
- 删除和已有 reindex 使用相同 MySQL advisory lock `gopulse:post-search-reindex:v1`，使整个重建/alias 切换与物理删除互斥，避免旧 bulk snapshot 复活。不是软删除，也不保留可搜索的 ES 内容墓碑。
- 实际 ES 集成测试先索引帖子、删除 MySQL 事实，再处理 delete、旧 create、旧 update、重复 delete；每次直接 GET document 都是 404，不依赖人工删索引、清 alias 或事后全量 reindex。

### 用户端交互

- 仅作者菜单有删除入口；原生 dialog 提供不可恢复说明、帖子标题、取消/Escape、焦点返回、提交中禁用和失败重试。
- 多个帖子卡片使用唯一 dialog label ID。Desktop 验证取消与焦点，Mobile 验证失败恢复和真正删除。
- 成功导航至 `/posts?deleted=1` 并重新加载，清除本地列表/详情/收藏快照；首页明确显示“帖子已永久删除，无法恢复”。

## 3. 实际阶段闭环与环境

- `frontend/e2e/delete.spec.ts` 为同一组 A/B 验证：资料修改 → B 按 username/display name 找到 A → follow → Following → bookmark/本人私有列表 → comment/like 通知 → 编辑 → B 编辑/删除拒绝 → A 二次确认删除 → 所有读路径过滤 → comment/like 墓碑保留且 follow 通知正常。
- 同一完整 Compose 产品还执行 profile、follow、bookmark、edit、既有 compose-business，以及管理员用户壳层隔离场景；独立 admin 场景访问 Metrics、Logs、Events、Exporter/Plugin 代表性页面。
- 新增 `scripts/verify-compose.sh --phase13`，复用完整容器产品构建、冷启动、普通用户/管理员准备和严格归属清理，只执行当前阶段所需路径，不展开 Phase 12 全部故障矩阵。
- 最终完整产品项目为 `gopulse-accept-70212dd06b70`，应用代码提交 `3791d42`；7 条阶段/既有业务浏览器测试全部通过，管理员 setup 与 admin 场景各通过 1 条；最终资源和 Git 工作树不变检查通过，命令退出 0。
- Compose 在完成版本提升前使用 `1.10.4-accept-70212dd06b70` 独占镜像标签验证本批源码；验收后才按规则提升完成版本至 `1.10.5`，重新进行 Frontend build 和版本治理。未声称已发布 `1.10.5` 镜像或推送远程分支。
- 独立 integration 使用本批 `gopulse-p1305` 容器、白名单 `gopulse_integration` schema/user、loopback 端口和 Redis DB 15；最终全部清理。原有 `gopulse-p13-local-*` 容器未由本任务启动、停止或删除。

## 4. 验证命令与结果

本机命令输出保存在 `/tmp/gopulse-p1305/`；它是临时制品目录，不承诺随 Git 发布。下表均为实际执行，不把 skip、失败或计划动作算作通过。

| 命令/范围 | 最终结果与证据 |
| --- | --- |
| `cd backend && go test ./internal/post ./internal/comment ./internal/like ./internal/notification ./internal/search ./internal/outbox ./internal/worker ./internal/http ./internal/platform/redis ./internal/bus ./internal/platform ./migrations` | 全部通过，`go-final.log`。bus/platform/migrations 是此次消息协议、路由和 FK 直接影响范围。 |
| `go test -tags=integration ./internal/post ./internal/notification ./internal/search ./internal/worker ./internal/http` | 最终源码通过，`integration-final.log`；其中 worker/http 缓存命中，因此新建最终隔离环境后另用 `go test -count=1 -tags=integration ./internal/worker ./internal/http` 实际重跑通过，`integration-uncached.log`。 |
| `go test -tags=integration ./internal/search -run TestIntegrationDeletedPostOldEventsConvergeWithoutReindex` | 真实 ES 删除/乱序重放通过，`search-delete.log`；也包含在最终 integration package 检查中。 |
| `go test -tags=integration ./internal/notification -run TestIntegrationLateNotification` | 延迟通知墓碑、去重、读取和 mark-read 通过，`notification-late.log`；也包含在最终 package 检查中。 |
| `cd frontend && npm test` | 17 文件、65 测试通过，`frontend-final2.log`；包含新增通知墓碑与已读回归。 |
| `npm run typecheck`、`npm run build` | 通过，`frontend-final2.log`；完成版本更新后仅重新运行受 package 元数据影响的 build（包含 typecheck），`build-versioned.log`。 |
| `npm run test:e2e -- --grep 'Phase 13\|profile\|follow\|Following\|bookmark\|edit\|delete\|notification'` 及失败项定向重跑 | 宿主工具环境中直接业务路径通过，早期未准备管理员时其场景 skip 不计通过；全部阶段及管理员路径随后在最终完整 Compose 中实跑通过。 |
| `bash scripts/verify-business.sh --self-test` | 通过，`self-test-final2.log`，拒绝 6 个不安全目标。 |
| `bash scripts/verify-business.sh` | 完整门禁通过并退出 0，`business6.log`；包括真实业务浏览器、搜索重建/增量与队列重放、故障矩阵、缓存/Redis 故障恢复及进程日志检查。 |
| `bash scripts/verify-compose.sh --phase13` | 完整产品阶段门禁退出 0，`compose-phase13-complete.log`；7 个阶段/既有业务测试、管理员 setup/admin、严格清理和前置资源不变检查通过。 |
| `bash -n scripts/verify-business.sh scripts/verify-compose.sh scripts/verify-compose-observability.sh`、`bash scripts/verify-compose.sh --self-test` | 通过；后者 `compose-self-test.log`。 |
| `python3 -m unittest scripts.ci.test_verify_business` | 最终 12 项通过，`script-tests-final.log`。 |
| `python3 scripts/ci/validate_versions.py` | 通过，`version-check.log`。 |
| `python3 scripts/ci/validate_branch.py --branch develop/1.10.5 --base-ref origin/main` | 通过，`branch-check.log`。 |
| `git diff --check` 与 `git diff --check origin/main...HEAD` | 通过。 |

保留 Phase-13-01 至 04 开发记录中的未受影响检查，不因阶段收口重做全库测试、一般依赖审计或独立 review。未读取第三方依赖源码。

## 5. 实际失败、修正与重跑原因

1. MySQL 不接受同一 ALTER 删除再创建同名 FK：改用新 FK 名；仅重置本批新建隔离 schema 的失败迁移标记后重新应用成功。
2. 首轮 integration 在 MySQL 初始化未完成时执行失败；等待真正 TCP/database readiness 后重跑。新增 fixture 的 timestamp/字段和清理顺序问题已修复，未修改业务 schema 来迁就测试。
3. 删除 E2E 暴露陈旧 search hit 导致 500：修复 repository/service 的“所有 ID 必须存在”假设，增加直接 regression 并在真实浏览器与 ES 中通过。
4. 旧 verify-business 缺少当前 Compose 必需变量，且 host tooling 无法通过 internal network 访问依赖：补齐独占 acceptance 配置和仅该 host 脚本使用的网络覆盖；生产 Compose 网络没有修改。
5. 旧脚本运行全部 spec 会误执行容器专用场景；最初非锚定文件过滤又匹配 compose-business，复用 TOKEN 导致后续注册冲突。改为精确业务 spec regex 后，完整 business gate 通过。
6. 首轮完整产品的业务断言虽通过，但本任务在其运行期间清理另一套自有 integration 容器、继续编辑源码，触发严格收尾检查。该命令没有计为成功；最终串行运行，期间不编辑工作树或清理外部资源，检查通过。
7. 新成功提示的 E2E 发现根路径重定向丢失 query；改为明确导航 `/posts?deleted=1`。原 Following unit fixture 未提供 router，按新增页面依赖接入内存 router 后通过。最终完整产品验证了成功提示和焦点。
8. 脚本单测曾因当前目标分支 `1.10.5` 与尚未提升的完成版本 `1.10.4` 不匹配提前失败；按完成规则提升版本后 12 项通过，未改测试绕过治理。

## 6. 偏差、限制与 Phase 14 交接

- 不新增软删除表或可恢复内容；采用 MySQL 缺失事实、行锁和 reindex advisory lock 组合保证删除不被现有写入者复活。Phase 14 若新增投影写入者，须保持同一事实/串行化合同。
- reindex 长时间持锁时，删除最多等待 advisory lock 10 秒后返回可重试失败，事实不变；不承诺在重建期间所有删除都立即成功。
- `000010` down 仅恢复 restrictive FK，保留通知墓碑及历史事件；不恢复已删除事实，也不承诺旧应用可直接兼容新的墓碑/事件合同。正式降级需要单独制定兼容策略。
- 不提供管理员代删、批量删除、撤销、恢复、实时跨会话画面推送；不扩展 Kubernetes 或 Phase 14 插件能力。
- 页面成功后采用重新加载以清除本地所有视图快照，而非新增跨页面缓存事件系统。
- Phase 14 可依赖合入后的 `1.10.5` API/event/cache/search/UI 边界。剩余仓库生命周期事项只有按正常流程推送/PR/合入，再确认 Phase 13 正式里程碑；本轮未擅自执行这些远程动作。

## 7. 变更文件
- `.env.example`
- `VERSION`
- `backend/internal/bus/envelope.go`
- `backend/internal/http/api.go`
- `backend/internal/http/router_notification_test.go`
- `backend/internal/notification/integration_test.go`
- `backend/internal/notification/model.go`
- `backend/internal/notification/repository.go`
- `backend/internal/platform/rabbitmq_publisher.go`
- `backend/internal/platform/rabbitmq_topology.go`
- `backend/internal/platform/rabbitmq_topology_test.go`
- `backend/internal/post/delete.go`
- `backend/internal/post/edit_integration_test.go`
- `backend/internal/post/handler.go`
- `backend/internal/post/repository.go`
- `backend/internal/post/service.go`
- `backend/internal/search/delete_integration_test.go`
- `backend/internal/search/processor.go`
- `backend/internal/search/processor_test.go`
- `backend/internal/search/service.go`
- `backend/internal/search/service_test.go`
- `backend/migrations/000010_post_deletion.down.sql`
- `backend/migrations/000010_post_deletion.up.sql`
- `dev/imple/Phase-13/Phase-13-05-帖子永久删除与阶段收口.md`
- `dev/imple/Phase-13/Phase-13-总实施方案.md`
- `dev/logs/Phase-13/Phase-13-05-帖子永久删除与阶段收口.md`
- `frontend/e2e/delete.spec.ts`
- `frontend/package-lock.json`
- `frontend/package.json`
- `frontend/src/components/PostEditMenu.vue`
- `frontend/src/services/api.ts`
- `frontend/src/types/api.ts`
- `frontend/src/views/FollowingView.test.ts`
- `frontend/src/views/NotificationsView.test.ts`
- `frontend/src/views/NotificationsView.vue`
- `frontend/src/views/PostsView.vue`
- `scripts/verify-business.sh`
- `scripts/verify-compose-observability.sh`
- `scripts/verify-compose.sh`
