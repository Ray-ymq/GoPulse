# Phase-02-06：实现 Review 整改

## 1. 批次目标

在 `develop/0.3.6` 上关闭 `dev/review/2026-09-02-Phase-2实现Review报告.md` 记录的 4 项 P2 与 1 项 P3，使 Phase 2 异步基础具备已发布 Outbox 有界保留、整批租约预算、受控 Worker shutdown、权威阶段状态和无内容 PR 防护。目标版本为 `0.3.6`。

## 2. 实施范围

1. 为 Backend Outbox Dispatcher 增加可取消的 published cleanup 循环、保留期、清理周期和 batch 配置；清理失败只记录收敛日志，不阻断投递。
2. 在配置与 Dispatcher 构造边界校验 `claim batch × publish timeout + safety margin <= lease duration`，并调整默认租约。
3. Worker 停止接收新消息后给予在途处理 grace period；超时即取消 processing context，并等待 handler 退出后才关闭 AMQP/MySQL 生命周期。
4. 将 Phase-02-05、README 和总方案更新为 PR #39 已合入且远程门禁通过；新增本批权威版本/分支分配。
5. 自动 PR workflow 在创建 PR 前比较 `main` 与 head 的 tree diff；无文件变化时跳过 PR/merge，避免新的单父 no-op squash 提交。既有提交历史不重写。
6. 更新根与 Frontend 版本元数据到 `0.3.6`，并创建对应实施记录。

## 3. 非目标

- 不改变至少一次投递语义、消息契约、通知业务规则或 RabbitMQ 拓扑。
- 不实现通用定时任务平台、Outbox 管理后台、dead queue 自动重放或 Phase 3 搜索。
- 不重写或删除已经合入 `main` 的历史提交。
- 不修改冻结的 `scripts/*.ps1`。

## 4. 验收标准

- Backend 生产入口真实启动 published cleanup；配置有默认值和上下限，删除条件仍只命中超过保留期的 `published` 行，每批有上限并在满批时退让。
- Dispatcher 构造与环境配置都拒绝整批最坏 publish budget 不足的租约；默认配置满足预算。
- `Runtime.Run` 返回时没有该 runtime 启动的 handler goroutine 存活；shutdown grace timeout 后 processing context 被取消，当前消息可重新投递。
- Phase 2 总方案、README、Git 合并事实、远程门禁事实与版本时间语义一致；Phase-02-05 实施记录保留当时历史表述。
- 自动 PR workflow 对无文件差异的 branch 明确跳过 PR 创建和自动合并；正常有差异分支行为不变。
- `VERSION`、`frontend/package.json` 和 `frontend/package-lock.json` 均为 `0.3.6`。

## 5. 固定验证与必要回归

1. `test -z "$(gofmt -l .)"`、`go test -count=1 ./...`、`go vet ./...`、`go test -race -count=1 ./...`（`backend`）。
2. `npm test -- --run`、`npm run build`（`frontend`）。
3. `python3 -m unittest discover -s scripts/ci -p 'test_*.py'`。
4. `python3 scripts/ci/validate_versions.py`。
5. `python3 scripts/ci/validate_branch.py --branch develop/0.3.6 --base-ref origin/main`。
6. `bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-business.sh`。
7. `bash scripts/verify-business.sh --self-test`。
8. `docker compose --env-file .env.example --file deploy/compose.yaml config`，并确认 4 个 published port 均绑定 `127.0.0.1`。
9. `git diff --check`。

完整 Chromium 与十项故障矩阵不默认重跑：本批不改变用户链路、RabbitMQ 拓扑或验收脚本；只有上述定向/固定门禁暴露跨组件回归时才扩大验证并在实施记录说明原因。

## 6. 完成条件

上述验收全部通过且无 P0/P1 阻塞，实施记录如实完成，版本元数据更新为 `0.3.6`，本批变更提交到 `develop/0.3.6` 后停止。
