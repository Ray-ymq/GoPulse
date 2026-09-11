# Phase 14 实现 Review 报告

## 1. 结论

**结论：Phase 14 六批交付及既有验收记录已进入主线；本次 review 发现 1 项 P2 和 2 项 P3，不能给出“无问题通过”的结论。**

- **P2-01：单个插件恢复受阻会中断整个 Plugin Manager 的关闭遍历，后续正常插件未执行清理。** 已通过最小 Go 测试复现。
- **P3-01：Redis v1 → v2 更新成功后，管理页面未刷新 catalog，配置替换按钮仍被旧 `upgrade_required` 状态禁用。** 已通过 Vue 组件测试复现；手动刷新可恢复。
- **P3-02：Phase 14 权威总方案及第六批当前状态仍写“待合入主线”，与本次主线基线不一致。** 已通过 Git 和文档核对确认。

建议优先修复 P2-01，并将两项 P3 一并纳入后续整改。本报告不修改实现，不宣布新的批次完成，不将历史 Compose 验收等同于本次重新验收。未在本次实际检查范围内确认 P0/P1，不意味着对未检查路径作无缺陷保证。

优先级约定：P0 为紧急阻断，P1 为高优先级严重问题，P2 为应修复的功能/可靠性缺陷，P3 为有明确绕过方式或文档一致性问题。

## 2. 权威基线、分支与工作区

| 项目 | 本次事实 |
| --- | --- |
| Review 日期 | 2026-09-12（本地日期） |
| 最初指定分支 | `develop/1.11.7`；初次核对时本地及远程均不存在 |
| 用户补充指示 | 直接从上游 `main` 分支构建 |
| 实际基线 | 成功 fetch 后的 `upstream/main` |
| 基线完整提交 | `d16ed4a03051658903b43e83e4f6b4cdd97ccf78` |
| 基线提交说明 | `test(compose): complete Phase 14 acceptance and release 1.11.6 (#129)` |
| 本次创建分支 | 从上述提交创建 `develop/1.11.7` |
| 当前完成产品版本 | `VERSION` 为 `1.11.6`，本次不变更 |
| 阶段差异定位 | `8caf8d4..d16ed4a`，即总方案记录的 Phase 13 基线至本次上游主线；用于定位变更，不代表逐行审计全部文件 |
| 项目文件变更 | 仅新增本报告；复现用临时测试已删除 |
| 原有用户文件 | 未跟踪文件 `~` 原样保留，不纳入提交 |

`develop/1.11.7` 是本次按用户指示创建的 review 分支，不是远程已完成的 `1.11.7` 产品。Phase 14 总方案仍只有六个执行批次，终版为 `1.11.6`；本次没有新增第七批或重新分配版本。第六批记录中的 acceptance-only `1.11.7` 插件成功包也不代表产品版本升级。

Git 最初因本机代理不可用而 fetch 失败；随后使用仅对命令生效的无代理环境成功获取上游，没有修改全局代理设置。

## 3. 范围与方法

以 `dev/imple/Phase-14/Phase-14-总实施方案.md`、分批计划及 `dev/logs/Phase-14/` 六份开发记录为验收依据，采用风险导向的代码抽查、相关包测试和最小缺陷复现：

| 阶段范围 | 本次关注点与证据 |
| --- | --- |
| Phase-14-01 | 固定插件目录、Schema/Secret 请求边界、受信制品、active revision、迁移/回滚/恢复、每插件操作锁和关闭逻辑；Monitor/Backend 对应测试 |
| Phase-14-02 | MySQL/RabbitMQ 只读采集实现、完整快照处理、固定配置；对应 Exporter 测试。真实账号权限沿用开发记录，不在本次重新调和 |
| Phase-14-03 | Kafka 固定 topic/group、offset 缺失处理、只读初始化边界，以及 Elasticsearch primary 统计映射；对应测试。未重新搭建 follower 拓扑 |
| Phase-14-04 | VictoriaMetrics 固定映射、有限标签和完整快照、六插件隔离中的关闭路径；对应测试。未重复真实存储停机 |
| Phase-14-05 | 私有指标认证、固定端点、Registry 低基数和快照、Monitor 解析、Router/Marshaller/Backend 相关合同测试；未重跑六组件故障注入 |
| Phase-14-06 | 组合验收入口、清理回归、历史 Compose 证据、管理界面配置与升级交互、阶段状态一致性 |

本次不是全仓库审计、依赖审计或跨平台认证；没有阅读第三方依赖实现，没有扩展 Kubernetes、macOS 或原生 Windows 验收。

## 4. Findings

### P2-01：恢复受阻的单个插件使后续正常插件跳过关闭清理

**位置**

- `monitor/internal/plugin/runtime.go:147-165`：`lock` 对 `blocked` slot 返回错误。
- `monitor/internal/plugin/runtime.go:918-930`：`shutdown` 遇到任意 lock/stop 错误立即返回。
- `monitor/internal/plugin/catalog.go:19-20`：关闭遍历采用固定顺序，Redis 在前。
- `monitor/cmd/monitor/main.go:131-145`：收到退出信号后调用 Manager shutdown，并将错误返回上层。

**触发条件和执行路径**

1. Redis 的 `active.json` 损坏，启动恢复将 Redis slot 标记为 `blocked`；其他插件仍可正常恢复和工作。这是新 Manager 明确支持的局部拒绝路径。
2. Monitor 收到退出信号，调用 `core.shutdown`。
3. 遍历到 Redis 时，`c.lock` 因 `blocked` 返回错误。
4. `shutdown` 立即返回，MySQL、RabbitMQ、Kafka、Elasticsearch 和 VictoriaMetrics 均没有机会执行其 `stopProcess`。
5. 相同提前返回也发生在一个插件的 observer 停止或进程清理报错时。

**本次复现**

临时测试复用 `runtimeFixture`：在 Redis 目录写入 `{"revision":"broken"}`，构造真实 `newRuntimeCore` 并确认 Redis slot 被阻断；给 MySQL 挂接已有 `countingMetricsLifecycle`，调用 shutdown，断言 MySQL 的 `Disable` 应被调用。该断言失败：

```text
=== RUN   TestReviewPhase14ShutdownContinuesPastBlockedPlugin
    review_phase14_temp_test.go:21: healthy MySQL collector was not disabled after damaged Redis state; shutdown error=plugin operation failed
--- FAIL: TestReviewPhase14ShutdownContinuesPastBlockedPlugin
```

命令：`(cd monitor && go test ./internal/plugin -run '^TestReviewPhase14' -count=1 -v)`，退出码 1。该失败证明缺陷，不是原有测试套件失败。临时文件已删除。

**影响与边界**

- 一个损坏插件把退出清理影响扩大到其他正常插件，削弱 Phase 14 的多插件故障隔离与有界生命周期合同。
- 后续插件的 collector Disable、受控 terminate/wait、process record 清理会被跳过，不能把 `Manager.Shutdown` 视为已尝试收口所有插件。
- 进程创建设置了 `Pdeathsig: SIGTERM`（`monitor/internal/plugin/process.go:119`），因此**本报告不声称子进程一定永久遗留**。父进程退出时的信号是兜底，不等价于 Manager 完成所有插件的有界停止和归属记录清理。
- 复现验证到 Manager/collector 层，没有执行真实 Docker 退出场景，也没有据此声称已观测到容器残留。

**建议**

遍历全部插件，对单个 slot 的失败进行收集而不是立即中断；在同一总 deadline 内继续尝试其他可安全清理的 slot，最后汇总返回错误。对 blocked slot 不能绕过原有进程归属验证，更不能按不可信 PID 强杀。

**整改验收**

保留一条“Redis 恢复被拒绝、后续正常插件仍收到 Disable/停止尝试”的代表性回归；保证函数仍返回原失败信息。只在整改涉及真实进程停止/总超时合同的情况下补对应运行时验证，不要求重新执行所有业务用例。

### P3-01：v1 更新到 v2 后，页面配置门禁保留旧 catalog 状态

**位置**

- `frontend/src/views/ObservabilityExportersView.vue:64-74`：`run('update')` 仅更新 status/statuses，没有刷新 catalog。
- `frontend/src/views/ObservabilityExportersView.vue:91,101`：旧版提示及“替换配置”禁用条件来自 `selectedItem.summary`。
- 同文件 `load` 会读取 catalog，而 `configure` 保存成功后也会读取；包更新路径没有相应同步。

**问题**

迁移后的 v1 Redis catalog 为 `upgrade_required`。管理员上传受信 v2 包并成功更新后，页面版本已经变为 v2 版本，但 `selectedItem` 仍来自更新前的 catalog：旧版提示继续显示，“替换配置”仍禁用。必须额外点击“刷新状态”或重新进入页面才可操作。

**本次复现**

Vue 组件测试 mock 初次 list/catalog 为已安装的 v1 Redis，mock update 成功返回 `1.11.6`；触发“确认更新”，确认更新 API 被调用且页面显示 `v1.11.6`，随后检查配置按钮：

```text
AssertionError: successful v2 update must clear the stale upgrade_required gate: expected '' to be undefined
Test Files  1 failed (1)
Tests       1 failed (1)
```

命令：`(cd frontend && npm test -- src/views/review_phase14_temp.test.ts)`，退出码 1。使用 mock 隔离状态同步问题，未上传真实制品，也不冒充浏览器 E2E。临时文件已删除。

**影响、建议与验收**

这是迁移后管理员操作链路的 UI 一致性缺陷，不是服务端升级失败；存在明确手动刷新绕过方式，定为 P3。更新成功后应重新获取可信 catalog，并同步配置门禁和 revision；刷新失败时应明确区分“更新已成功”和“目录刷新失败”，避免提示用户盲目再次上传相同版本。

以一条组件测试验证 v1 → v2 成功后不需人工刷新即可解除 `upgrade_required` 门禁，不需要为此重新跑六插件全部真实安装测试。

### P3-02：阶段当前状态未随第六批合入同步

**位置**

- `dev/imple/Phase-14/Phase-14-总实施方案.md:3`。
- `dev/imple/Phase-14/Phase-14-06-Compose集成验收与阶段收口.md` 开头的当前状态。
- `dev/logs/Phase-14/Phase-14-06-Compose集成验收与阶段收口.md:5` 及交接段。

**问题与证据**

以上文字仍将“第六批待合入”“主线阶段完成条件待第六批合入”描述为当前状态；本次成功 fetch 的 `upstream/main` 已是 `d16ed4a`，提交说明明确为第六批完成并发布 `1.11.6`，根版本亦为 `1.11.6`。

合并前记录本身是真实历史，不应删除；但权威总方案的当前状态未同步，会使后续 Phase 15 开工判断和里程碑状态相互矛盾。

**建议与验收**

增加合入后的当前状态，引用 `d16ed4a`，保留原“待合入”文字作为有时间/上下文的历史记录，并关联本次 review 的未整改项。不能简单将“已经合入”改写成“本次复核无缺陷”。文档检查和提交基线核对足够，不需重跑应用验收。

## 5. 实际执行的检查

### 5.1 原有相关测试和静态检查

下列命令全部退出 0；**部分 Go package 使用 Go test 缓存**，没有声称它们全部重新执行。未使用 `-count=1` 强制重复已成功的原有测试。

| 工作目录 | 命令 | 结果 |
| --- | --- | --- |
| `componentmetrics` | `go test ./...` | 通过 |
| `monitor` | `go test ./internal/plugin ./internal/httpserver ./internal/metrics/...` | 通过 |
| `backend` | `go test ./internal/exporterplugin ./internal/metricquery ./internal/http` | 通过 |
| `router` | `go test ./internal/envelope ./internal/httpserver ./internal/kafka` | 通过 |
| `marshaller` | `go test ./internal/envelope ./internal/metrics ./internal/consumer` | 通过 |
| `exporters/redis` | `go test ./...` | 通过 |
| `exporters/mysql` | `go test ./...` | 通过；runtime 无测试文件 |
| `exporters/rabbitmq` | `go test ./...` | 通过；runtime 无测试文件 |
| `exporters/kafka` | `go test ./...` | 通过 |
| `exporters/elasticsearch` | `go test ./...` | 通过；runtime 无测试文件 |
| `exporters/victoriametrics` | `go test ./...` | 通过 |
| 根目录 | `bash scripts/verify-compose.sh --self-test` | 通过；6 个不安全 project name 被拒绝，不访问 Docker |
| 根目录 | `bash scripts/verify-plugin-metrics.sh --self-test` | 通过；source/归属静态自检，不访问 Docker |
| 根目录 | `bash scripts/verify-component-metrics.sh --self-test` | 通过；六身份/预算/浏览器合同静态自检，不访问 Docker |
| 根目录 | `python3 -m unittest discover -s scripts/ci -p test_phase14_cleanup.py` | 2 tests 通过；mock 清理回归，不是真实资源删除 |
| `frontend` | `npm test -- src/services/exporters.test.ts src/services/observability.test.ts` | 2 files、11 tests 通过 |
| `frontend` | `npm run typecheck` | 通过 |

### 5.2 定向缺陷复现

| 命令 | 结果与解释 |
| --- | --- |
| `go test ./internal/plugin -run '^TestReviewPhase14' -count=1 -v`（monitor） | 退出 1；P2-01 最小复现断言失败 |
| `npm test -- src/views/review_phase14_temp.test.ts`（frontend） | 退出 1；P3-01 组件状态同步断言失败 |

本次检查输出保存在本机 `/tmp/gopulse-phase14-review/` 的 `go-tests.log`、`checks.log`、`reproduction-go.log` 和 `reproduction-ui.log`。这些是临时辅助证据，不是仓库交付物，可能被系统清理；本报告已保留复现前提、路径、命令和关键结果。

### 5.3 历史证据与未执行范围

- 第六批开发记录记载 `bash scripts/verify-compose.sh --phase14` 最终退出 0，项目为 `gopulse-accept-81d5cf3c48ce` 与 `gopulse-p1401-652faf4f2dd5`，覆盖真实迁移、空卷、六插件/六组件、故障恢复和 Phase 13 回归。
- 同一记录注明完整门禁后发现的旧卷清理缺口已独立修复并实证。本次只重跑对应 mock 单测，没有重新确认真实 Docker 库存。
- **本次没有运行完整 `--phase14`、真实 Docker 故障注入、真实旧卷迁移、Browser E2E、镜像重建或 release build。** 当前任务是 review，既有全面验收记录与定向复现已足以支持上述发现；不为没有相关修改的范围重复完整验收。
- 没有运行 Go race、全仓库测试或跨平台支持矩阵，不对这些项目给出通过结论。
- 文档交付提交前检查使用 `git diff --check`、`git diff --cached --check`，并核对暂存区仅包含本报告。

## 6. 后续处理与停止条件

1. 先修复 P2-01，并以不降低归属校验为前提验证失败 slot 不阻断其他插件清理。
2. 修复 P3-01 的更新后目录同步；更新 P3-02 的主线当前状态。
3. 整改应按实际影响选择回归范围；本报告不自动要求再做全阶段实现、全量审计或全部 Compose 故障组合。
4. 本次 review 到此结束，报告与已执行证据完整交付；发现项保持未修复状态，不能因原有测试全绿而自动关闭。

## 7. 后续整改状态（2026-09-12）

用户随后要求在 `develop/1.11.7` 完整落实本报告并推送。三项发现已在追加的 Phase-14-07 整改批次修复并通过定向验收：

| 发现 | 整改与验证 |
| --- | --- |
| P2-01 | shutdown 汇总单插件错误并继续遍历；损坏 Redis 仍被阻断、MySQL Disable 得到执行且安全错误被保留；Monitor plugin/httpserver 回归通过 |
| P3-01 | 更新成功后重新读取 catalog；组件成功/刷新失败两例通过，刷新失败明确保留更新成功结果；Frontend 服务回归及 typecheck 通过 |
| P3-02 | 总方案、第六批方案/记录增加 `d16ed4a` 合入后状态，保留原历史并关联本批整改 |

目标版本/分支已在总方案正式登记为 `1.11.7` / `develop/1.11.7`，详见 `dev/imple/Phase-14/Phase-14-07-Review整改.md` 与 `dev/logs/Phase-14/Phase-14-07-Review整改.md`。这是后续整改结论，不改写 §§1–6 的原始 Review 事实；本分支通过不代表 main 已包含整改，也不代表本次重跑完整 Compose 验收。
