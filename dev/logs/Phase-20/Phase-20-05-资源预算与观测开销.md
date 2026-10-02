# Phase-20-05：资源预算与观测开销实施记录

> 2026-10-02：本记录覆盖用户授权的 U2 事务阻断修复及 B06 关停恢复编排修复。
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

## B06 关停恢复编排修复（追加授权窗口）

实际交付：修订 05 的 2.7 节和总方案，规划提交 `2b7f0e6` 已经 PR #213 以
merge commit `f4249ba` 合入 main；开发分支以 `417df99` 同步。后续执行本文件从
2.7 的 r10 接续检查点核对，不从历史 2.5/2.6 默认重跑。本窗口没有启动产品矩阵。

实际修改文件为 `scripts/ci/phase20_budget.py`、
`scripts/ci/test_phase20_budget.py` 和本实施记录。未修改产品实现或 Compose 配置。

- B06 在任何信号前核对三个唯一容器 ID、项目/服务归属、Running 和 OOM 状态。
  用 SIGSTOP 临时暂停两个存活 worker 消费，四条 HTTP 201 写入后查询真实
  Outbox/Rabbit 积压；辅助暂停和查询共用 60 秒期限，查询逐次限制剩余时间。
- 向原 ID 发送 SIGTERM，再以 SIGCONT 释放 worker 的待决信号。发送、释放、
  退出轮询共用 30 秒期限；不再先 stop worker，也不以单次 inspect 判定退出。
  `shutdown.json` 在恢复前保存原 ID、信号/退出时刻、积压、最后观测及退出状态；
  失败也保留该私有证据。清理前释放仍归属且存活的辅助暂停目标。
- 显式 `compose start phase20-collector`，确认原容器存活后启动两个 worker，
  有 healthcheck 时等到 healthy；整体启动期限 30 秒。启动失败不进入水位检查。
  原 `_wait_empty(stack, 120)` 恢复判据及归属清理保持。
- 定向测试验证身份/归属、OOM、真实计数解析、共享期限、异步退出、健康等待、
  停止与恢复顺序、启动失败、失败证据和收尾，未扩大为全项目审计。

| 实际命令/操作 | 结果 |
| --- | --- |
| `fetch origin`、规划 validate_versions/validate_branch、diff 检查及 `push origin update` | 通过；规划提交已在远端 main/update。额外 `gh pr create` 返回 main/update 无差异，查询确认 PR #213 已合入，没有重复 PR |
| `git merge --no-edit origin/main`（develop/2.2.5） | 成功，提交 `417df99`；继续同一未完成批次 |
| `python3 -m unittest scripts.ci.test_phase20_budget scripts.ci.test_phase20_closure scripts.ci.test_phase20_evidence` | 最终 29 项通过；首次新增 fixture 缺少 api 字段导致 4 项 KeyError，补齐后通过；后续随新增期限、归属断言运行受影响固定检查 |
| `timeout --signal=TERM --kill-after=5s 150s python3 /var/tmp/gopulse-b06-protocol-5it0ajp4/protocol.py` | 小型真实 Compose 协议验证通过；补充最后退出观测后直接重验通过，三个目标退出码均为 0，最终关停约 1.28 秒；Collector restart=no，显式恢复后原 ID 保持，运行状态稳定；未退出时的有界等待负例正确失败 |
| fixture `compose down --volumes --remove-orphans --timeout 10`、Docker inventory 归属复核 | 通过；仅清理本次三个容器和项目网络，未使用 global prune；全部容器/网络/卷 inventory 前后一致 |
| `python3 scripts/ci/validate_versions.py`、`git diff --check` | 通过；VERSION 保持 2.2.4，未创建批次完成提交 |

该协议项目使用缓存固定 Collector digest 和 Alpine image ID，原始操作及私有
`protocol.json`/`shutdown.json` 位于 `/var/tmp/gopulse-b06-protocol-5it0ajp4`；
明确 `formal=false`、`product_acceptance=false`。fixture worker 仅验证进程信号/
Compose 启动协议，不能证明业务事实、lease、offset 或产品 B06 已通过。
原 r10 manifest 的 SHA-256 前后相同；没有改写或换绑候选回执。

本次追加授权预计 30/上限 45 分钟，从 2026-10-02 14:48:14 UTC 起累计。
定位、合同同步约 24 分钟，超出原阶段 8 分钟估计；总上限没有延长。
完成定向验证和记录时约 31 分钟，提交/推送成本继续追加上述私有成本台账。
历史各窗口成本另计，本次不重置文件累计预算。

剩余工作仍按 2.7：用户再次明确执行 05 后，先建立受影响新候选身份，单独运行
真实 B06，登记实际成本和有限剩余预算。该脚本修复影响 B06/U3/B07 及共享工具
指纹绑定的回执；旧原始证据和仍有效的直接检查/构建缓存保留，不能拼接旧候选
通过回执。当前 closure 无 --resume，当前候选完整 B07 未通过时不解锁 S3。
本窗口不自动恢复完整矩阵，不更新 VERSION，不宣称 Phase-20-05 完成。

## 2.7 实际 B06 定向接续（新候选）

本次用户明确要求从 2.7 接续。当前分支为 `develop/2.2.5`，修复提交为
`c1cc85e29e779de14ff3db650ac2b6e491925e82`，根 `VERSION` 保持 `2.2.4`。
修复只涉及预算案例分发和对应单元测试；未修改产品代码、Compose restart 策略、负载、quota
或恢复门限。修复后重新构建了新候选，未使用或改写 r10 及此前候选。

候选 manifest 位于仓库外
`/var/tmp/gopulse-phase20-05-candidate-c1cc85e/manifest.json`，版本为 `2.2.5`，
manifest digest 为
`sha256:aafab50d7e3cc3c84b3f1c5238bb363f4e26b514a2b7e1083a86325666a1d61e`；10 个自研镜像
的 OCI version/revision、镜像 ID、固定第三方 digest 和产品 tree 均已核对。真实 B06 原始
目录为 `/var/tmp/gopulse-phase20-05-candidate-c1cc85e/b06-r2`。

### 实际命令与结果

| 命令/操作 | 实际结果 |
| --- | --- |
| `python3 -m unittest scripts.ci.test_phase20_budget scripts.ci.test_phase20_closure scripts.ci.test_phase20_evidence`（修复前） | 29 项通过；随后 `--case B06` 暴露未定义分发键 `case`，在 Compose 启动前失败，原始目录 `/var/tmp/gopulse-phase20-05-candidate-77a2d04/b06` 保留 |
| `python3 -m unittest scripts.ci.test_phase20_budget scripts.ci.test_phase20_closure scripts.ci.test_phase20_evidence`（修复后） | 30 项通过；新增案例分发回归覆盖 B04/B05/B06 |
| `python3 -m py_compile scripts/ci/phase20_budget.py scripts/ci/test_phase20_budget.py`、`git diff --check` | 通过 |
| `git commit -m "fix(phase20): dispatch budget fault cases"`、`git push origin develop/2.2.5` | 提交 `c1cc85e` 已推送 |
| `scripts/verify-phase20-closure.sh --build-manifest /var/tmp/gopulse-phase20-05-candidate-c1cc85e/manifest.json --revision c1cc85e29e779de14ff3db650ac2b6e491925e82` | 新候选构建及第三方/Collector 身份绑定通过；一次错误 SHA 调用在写 manifest 前失败，未用于证据 |
| `scripts/verify-phase20-budget.sh --case B06 --manifest /var/tmp/gopulse-phase20-05-candidate-c1cc85e/manifest.json --work /var/tmp/gopulse-phase20-05-candidate-c1cc85e/b06-r2` | 返回 `execution_status=complete`、`case_id=B06`、`formal=false`；四条业务写入均为 HTTP 201，Outbox `outbox_pending=4` |
| B06 `shutdown.json` 独立核对 | SIGSTOP 两个 worker 后发送 SIGTERM；三个原容器 ID 均退出、exit code 0、无 OOM，`stop_seconds=0.528114`；随后 Collector 和两个 worker 显式 start，原 ID 保持 Running，水位恢复 `2.270385` 秒 |
| B06 `cleanup.json` 独立核对、`docker ps` | `status=passed`、`owned=true`、`global_prune=false`；前后 inventory 一致、容器数均为 0；未使用 global prune |
| `python3 scripts/ci/validate_versions.py`、`git diff --check` | 通过；根版本仍为 `2.2.4`，批次未完成 |

本次 `--case B06` 结果是 2.7 的定向协议/业务关停证据，明确 `formal=false`，不产生 B06
正式通过回执。没有运行 B07 预检、完整 B01～B06、B03 十二单元、S3/S4 或 Phase-20-06；
不能用本次结果解锁完整矩阵，也不能更新 `VERSION` 或宣称 Phase-20-05 完成。旧候选、首个
分发错误目录和错误 SHA 构建事实均保留，未换绑任何历史回执。

## 2.8 实际 B07 与 S3 接续结果（当前候选）

在用户要求继续执行后，先以包含 2.7 日志提交的 `8a4d7805492392ae7632b11b3a615ea50d018ca8`
重建候选。manifest 位于
`/var/tmp/gopulse-phase20-05-candidate-8a4d780/manifest.json`，digest 为
`sha256:d2b0e1a87486365b194771a4b52f2af95c324bb9a127c0b0c798ef504784b76c`。构建、第三方
固定 digest、10 个自研镜像 OCI 身份和当前候选绑定均已核对；dry-run 任务图为 16 个任务。

### B07 当前候选

| 命令/操作 | 实际结果 |
| --- | --- |
| `scripts/verify-phase20-closure.sh --preflight --manifest .../manifest.json --work .../b07-preflight` | U1/U2/U3/U4 全部 `pass`，`execution_status=complete`，`formal=false`；真实短预检清理 inventory 通过 |
| `python3 scripts/verify-phase20-evidence.py --preflight .../b07-preflight` | 独立候选、manifest、证据 digest 校验通过 |
| 运行时合同 `verify_runtime_contracts.py --candidate 2.2.5 --skip-version`、`validate_versions.py`、`git diff --check` | 通过；根 `VERSION` 仍为 `2.2.4` |

此前以 `c1cc85e` 构建的预检在 U3/U4 因当前 checkout 已前进到日志提交而拒绝，目录
`.../candidate-c1cc85e/b07-preflight-r2` 保留，未复用其回执；随后以当前候选完整重跑并通过。

### S3 正式预算的两个停止点

正式入口使用固定命令和 B07 外部 evidence：
`scripts/verify-phase20-budget.sh --manifest .../manifest.json --work <new> --preflight-evidence .../b07-preflight`。
两次均先完成 B01，B02 在真实 200 RPS、15 秒预热、60 秒测量窗口停止；没有启动 B03、B04、B05 或
B06 正式单元，也没有改写失败目录。

| 证据目录 | 实际边界 |
| --- | --- |
| `/var/tmp/gopulse-phase20-05-candidate-8a4d780/budget-s3` | B01 通过；B02 有 13,500 条终端记录，其中 4 个 PATCH 返回 HTTP 500，数据库事实和对应 Outbox `post.updated` 已存在；关联器按未知响应排除后报告 `Incomplete: outbox event lacks unambiguous request group`；`budget.json` 为 `formal=true`、`execution_status=incomplete` |
| `/var/tmp/gopulse-phase20-05-candidate-8a4d780/budget-s3-r2` | B01 通过；B02 有 13 个 HTTP 500（7 个 DELETE、6 个 POST），同样观察到事实/Outbox 副作用并停止，原因仍为上述 incomplete；`budget.json` 保留原始失败分类 |

两个失败窗口的非接受请求均约在 1 秒完成，错误类型随写路由变化；这不是可以通过证据关联器
改写成成功的结果。`python3 scripts/verify-phase20-evidence.py --budget .../budget-s3-r2`
按合同返回 `execution_status=incomplete`、`budget case set is incomplete`，没有生成预算通过证据。

### 有界诊断与停止决定

按 2.4 的失败边界执行了两个新的 `--case B02` 有界诊断目录：

* `.../diagnostic-b02-r1`：12,000 测量请求、0 业务错误、清理 inventory 前后一致；
* `.../diagnostic-b02-r3`：12,000 测量请求、0 业务错误、清理 inventory 前后一致；旁路归属容器日志保存在
  `.../diagnostic-b02-r3-monitor/owned-error-log.txt`，未发现测量窗口 HTTP 500。

诊断通过不能替代两个正式 B02 失败回执。当前事实表明候选在相同冻结窗口下存在高并发写入时
“业务事实已提交但 HTTP 返回 500”的产品行为；本批 2.7 允许集合只包含预算工具、测试和日志，
明确禁止在预算批次泛化修复产品、继续放宽 quota/阈值或反复重跑寻找好结果。因此依规则停止
S3，不更新 `VERSION`，不宣称 Phase-20-05 完成。后续必须先在 update 修订并合入适用的
产品/合同修复计划，再由新候选重新执行受影响的 B07 与完整 S3；现有失败目录和诊断目录均为
只读历史证据。最终执行时确认无残留 GoPulse 容器，未使用 global prune。
