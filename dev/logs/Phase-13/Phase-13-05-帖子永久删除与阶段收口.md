# Phase-13-05 开发记录

## 状态

2026-09-08：实施与验收进行中；目标分支 `develop/1.10.5` 从最新 `origin/main` (`0dfbd9a`) 创建。当前完成版本暂保留 `1.10.4`，不得将本批或 Phase 13 声称为完成。为了满足容器验收要求的干净构建源码，先提交实现，验收后再提交实际结果及完成版本。

## 已实施

- 作者 DELETE、行锁授权、显式事务清理评论/点赞/收藏/帖子，同事务 outbox `post.deleted`；重复请求为 404，不重复 event。Redis 失败不影响删除事实。
- 通知 FK 改为 SET NULL，DTO `resource_deleted`，延迟消息在资源缺失时创建可去重墓碑；通知读取与已读仍可用。
- 详情缓存 revision/existence 拒绝陈旧 key 并尝试清理；列表依赖 MySQL，搜索水合跳过已删除帖子。
- Indexer 在 MySQL 行锁期间读取与写入 ES，删除与旧快照串行；缺失事实统一幂等删除。删除与 reindex 共用 MySQL advisory lock，避免 alias 切换/旧 bulk snapshot 复活；重建占锁超过 10 秒时删除返回可重试失败，不修改事实。
- 作者菜单原生 dialog：不可恢复说明、标题、取消/Escape/焦点、提交中、失败重试；成功重新加载首页以丢弃所有本地列表快照。通知墓碑不提供帖子链接。
- 扩展业务验收脚本的必需 Compose 变量与隔离 host 网络覆盖，并明确选择业务 spec；新增 `verify-compose.sh --phase13` 在完整容器产品上运行当前阶段社交路径与管理员代表性页面，不执行完整 Phase 12 故障矩阵。

## 已取得证据

制品目录 `/tmp/gopulse-p1305/`（本机临时目录，不作为已提交制品承诺）：

- 受影响 Go packages + bus/platform/migrations：`go-final.log` 通过。
- 固定 integration packages：notification/search/worker 在 `integration2.log` 通过；post/http 在修正 fixture 后 `integration4.log` 通过。
- 新 ES 实际删除后旧 create/update 重放：`search-delete.log` 通过。
- 延迟通知墓碑、唯一性与 mark-read：`notification-late.log` 通过。
- Frontend `npm test`：17 文件 65 测试通过；typecheck/build 已通过（最终元数据更新后仅重做受版本输入影响的构建）。
- profile/follow/bookmark/edit/notification 浏览器路径通过；新增移动端删除取消/失败恢复/墓碑闭环 `e2e-delete3.log` 通过。管理员尚未验收，不把 skip 算通过。
- shell 语法、Compose 脚本 self-test、`git diff --check` 通过。

## 实际失败及修正

- MySQL 同条 ALTER 删除并重建同名 FK 失败，改用新的外键名；仅对本批新建隔离 schema 的失败迁移标记重置后重跑成功。
- 初次 integration 在 MySQL 初始化期间执行失败；等待 ready 后只重跑失败的检查。新 fixture 的 timestamp/字段与清理顺序错误已修复，未调整业务表来迁就测试。
- 删除浏览器检查发现 Search 对已缺失 ID 返回 500，修复 FindMany 跳过缺失事实和 service 的数量相等假设，新增直接回归。
- 旧 verify-business 与当前 Compose 必需变量、internal network 不兼容，补齐隔离环境及仅本脚本的 host 网络覆盖后进入真实业务验收。
- 旧脚本默认运行所有 spec，误运行容器专用场景；文件名非锚定过滤又匹配 compose-business 并造成同 TOKEN 账号冲突。已用精确业务 spec regex 修正。完整 business gate 尚待最终通过。

## 剩余必须完成

- 完整 Compose 产品上的 Phase 13 链与管理员代表性回归。
- 修正后的完整 `verify-business.sh`、最终版本/分支治理检查。
- 门禁全部通过后更新版本与计划状态；Phase 13 总里程碑还须 05 合入 main，不提前宣称 5 批均已合入。
