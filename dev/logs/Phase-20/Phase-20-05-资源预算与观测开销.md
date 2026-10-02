# Phase-20-05：资源预算与观测开销实施记录

> 2026-10-02：本记录覆盖用户授权的 U2 事务阻断修复与定向回归。
> 批次状态仍为未完成；未运行新的完整 B07/B01～B06，不更新 VERSION（仍为 2.2.4）。

## 实际完成工作

- 在 update 修订本批 2.6 节和总方案，规划提交 `773f7a8` 经 PR #212 合入 main；
  develop/2.2.5 以 `a44a704` 同步该合同，保留既有版本/分支分配。
- 只读核对 r6 原始台账：60,000 次测量请求中 18 次 HTTP 500，实际路由分布为
  4 次评论、14 次 bookmark。bookmark 不产生 Outbox 事件，原窗口 3/15 汇总不准确。
- 使用独占 MySQL 与受控协议代理复现：提交已完成、COMMIT 应答丢失时，原评论接口
  返回 500，但同一评论事实和事件已持久化；提交前连接失败和 bookmark 提交应答丢失
  也会返回 500。这证明具体代码失败边界，不宣称已查明 r6 全部 18 条错误的 SQL 原因。
- 评论仅在确切 comment ID、帖子、作者、正文及应有 Outbox event ID 可读核对后恢复
  成功响应；提交结果未知时不重放 INSERT。提交前暂态失败可在事务收尾后重试一次。
- bookmark 幂等关系事务最多重试一次，保留父对象锁、重复收藏/取消行为和零事件副作用。
- 两类事务共享最多 3 秒的恢复上下文，继承调用方取消/更短期限；普通 MySQL 1 秒
  读写超时和既有负载/验收阈值保持。永久错误、不安全重放和取消不触发重试。
- 真实 HTTP/MySQL 回归验证 8 个场景：评论提交应答丢失、提交前应答丢失、实际回滚、
  自评论、应有事件缺失，以及收藏提交前/提交时和取消收藏提交时的应答丢失。
- 本次独占容器与一个匿名卷已按精确归属删除；未使用 global prune。

## 实际变更文件

- `backend/internal/platform/mysql_transaction.go`
- `backend/internal/platform/mysql_transaction_test.go`
- `backend/internal/comment/repository.go`
- `backend/internal/bookmark/repository.go`
- `backend/internal/integrationtest/mysql_proxy.go`
- `backend/internal/http/transaction_recovery_integration_test.go`
- 本实施记录；规划两个文件的修改已由上述独立规划提交交付。

## 已执行命令与结果

| 命令/操作 | 实际结果 |
| --- | --- |
| fetch origin、规划文档 validate_versions/validate_branch/diff 检查、提交/推送 update、PR #212 merge、开发分支 merge origin/main | 成功；update 使用 merge commit 合入 main，保留 update |
| `go test ./internal/platform ./internal/comment ./internal/bookmark ./internal/http`（backend） | 修复前后通过；bookmark 没有普通单元测试，其真实 HTTP/数据库行为由下述集成回归验证 |
| 独占 MySQL `docker run`、`go run ./cmd/migrate up`（backend，私有白名单环境） | 成功；migration 实测约 3.01 秒 |
| `go test -tags integration ./internal/http -run '^TestIntegrationTransactionRecovery$' -count=1 -timeout=90s -v`（修复前首次） | Go 退出 1：5 个产品断言失败，另有测试 fixture 外键清理错误；总命令约 7.31 秒 |
| 同一故障回归，在修正 fixture 清理后、修改产品前执行 | Go 退出 1：5 个产品断言失败，实际未提交负例通过，无 fixture 清理错误；约 7.29 秒 |
| 同一故障回归，产品修复后执行 | 6 个场景全部通过；约 7.31 秒 |
| `go test -race -tags integration ./internal/http -run '^TestIntegrationTransactionRecovery$' -count=1 -timeout=90s -v`（补齐两种事件边界后） | 8 个场景全部通过，无竞争报告；含构建约 14.45 秒 |
| `gofmt` / `gofmt -l` 变更 Go 文件 | 首次在 backend 目录误用 backend/ 前缀，4 个 lstat 错误；改用 internal/ 路径后成功，最终检查无输出 |
| `python3 scripts/ci/validate_versions.py`、`git diff --check` | 通过；未提前运行要求完成版本 2.2.5 的最终分支验收 |
| 归属标签/独占卷引用核对、`docker rm --force --volumes <本次归属容器>`、再次检查容器/卷 | 通过：删除 1 个容器和 1 个匿名卷，无本次归属残留；清理约 0.66 秒 |

## 偏差、成本与证据

- 首次故障测试的 fixture 删除顺序没有处理 comments 的 RESTRICT 外键；在第二次复现
  前改为先删除事件、评论、收藏，再删除帖子/用户。旧 fixture 最终随独占测试卷一并清理。
- 为验证新增代理的并发状态与提交核对边界，最终回归增加竞争检测、自评论及事件缺失
  两个场景；没有扩大到项目审计或完整验收矩阵。
- 本次额外预算为目标 60/上限 90 分钟；从首次操作至定向验证和归属清理约 16.71 分钟。
  提交/推送成本继续追加私有台账；历史接续没有精确跨命令活跃台账，不伪写为零或重置。
- 原始 r6 证据仍在 `/var/tmp/gopulse-phase20-05-candidate-r6-EY3t06`，不修改或换绑。
- 本次私有回归、源码 digest、执行成本与 cleanup 回执在
  `/var/tmp/gopulse-phase20-u2-repair-c8bb3507`。这些是定向回归证据，不是冻结候选 B07。

## 已知限制与后续

- 此修复与回归不能证明 r6 原有 U2 已通过。仍须为新源码建立新候选，按实际身份、配置
  与依赖列出失效检查并完成必要预检；不能把旧候选 S1/U1 回执拼成新候选完整通过。
- 当前 closure CLI 无 --resume。恢复完整预检/预算前，按 05 的 2.6 节登记真实执行粒度
  和剩余成本；本次修复推送不自动启动该矩阵。
- 提交已完成但事实/应有事件不可证明、或调用方已取消/恢复预算耗尽时，仍返回错误；
  不以假成功掩盖未知终态，也不进行无限重试。
- U3/U4、正式预算和发布收口尚未完成。本记录及修复提交不构成批次完成或阶段认证。
