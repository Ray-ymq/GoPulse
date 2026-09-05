# Phase-10-04：实现 Review 整改与再验收实施记录

## 1. 实施信息

- 实施日期：2026-09-05。
- 目标版本：`1.7.4`。
- 开发分支：`develop/1.7.4`。
- 基线：最新 `origin/main` 提交 `2882d7e`；分支原有提交 `8f95e3d` 仅新增 Phase 10 实现 Review 报告。
- 实施环境：WSL2 Linux filesystem `/home/ray/GoPulse-1.7.4`。
- 状态：已完成本地实现、固定门禁与真实 Events 再验收。

## 2. 实际完成内容

### 2.1 Plugin runtime generation fencing

- 将 unexpected-exit 处理与 install/start/stop/update 生命周期操作通过 Manager operation semaphore 串行。
- 在删除 process record、关闭 Metrics、提交 failed 状态或记录 exited event 前先确认 `m.runtimes[id]` 仍指向 watcher 捕获的 runtime。
- process record 删除增加完整 `processRecord` identity 比较，仅允许退出 generation 删除自身记录。
- 新增 stale watcher 回归测试，固定 replacement runtime、record、Metrics lifecycle 和 running 状态不被旧 watcher改变。

### 2.2 EventMonitor 有界关闭

- `Close` 在调用方 context 到期后执行 cancel/stop 并立即返回 `ctx.Err()`，不再同步无界等待 sender/worker。
- 新增忽略 cancellation 的 sender 测试，验证 deadline 返回、sender 后续释放时 worker 最终退出以及重复 `Close` 安全；既有正常 drain 测试继续通过。

### 2.3 Events 查询证据与过滤合同

- Backend 新增合法签名 cursor 两页用例，验证 filters、limit、PIT 更新、`search_after` 和 terminal PIT close。
- 新增 handler 空页 `200` 与 Elasticsearch unavailable 映射 `503/events_unavailable` 的响应级测试。
- 修复 cursor 查询参数被通用 256-byte 限制拒绝的问题；cursor 独立使用既有 8192-byte 上限。
- event/error 校验改为按 event specification 收敛，拒绝 `exporter_plugin_failed/publish_failed`、`metrics_collection_failed/start_failed` 等不可能组合。
- `scripts/verify-events.sh` 在 Elasticsearch outage 中固定验证 `503/events_unavailable`，并通过 `limit=1` 执行真实两页 cursor 查询。

### 2.4 治理、文档与版本

- Phase 10 总方案增加权威 Phase-10-04 → `1.7.4` / `develop/1.7.4` 分配和批次验收合同。
- 新增同名拆分实施方案，并校正 Phase-10-03 实施记录中过度声明的 PIT/cursor、空页和 `503` 证据。
- 根 `VERSION`、Frontend package 元数据同步更新为 `1.7.4`。

## 3. 变更文件

- `monitor/internal/plugin/manager.go`
- `monitor/internal/plugin/manager_test.go`
- `monitor/internal/events/monitor.go`
- `monitor/internal/events/monitor_test.go`
- `backend/internal/eventquery/eventquery.go`
- `backend/internal/eventquery/eventquery_test.go`
- `scripts/verify-events.sh`
- `dev/imple/Phase-10/Phase-10-总实施方案.md`
- `dev/imple/Phase-10/Phase-10-04-实现Review整改与再验收.md`
- `dev/logs/Phase-10/Phase-10-03-集成验收与阶段收口.md`
- `dev/logs/Phase-10/Phase-10-04-实现Review整改与再验收.md`
- `VERSION`
- `frontend/package.json`
- `frontend/package-lock.json`

## 4. 实际验证与结果

以下固定检查通过：

```bash
(cd monitor && go test -count=1 ./internal/events ./internal/plugin)
(cd monitor && go test -race -count=1 ./internal/events ./internal/plugin)
(cd monitor && go vet ./...)
(cd backend && go test -count=1 ./internal/eventquery ./internal/http/...)
(cd backend && go test -race -count=1 ./internal/eventquery ./internal/http/...)
(cd backend && go vet ./...)
bash -n scripts/verify-events.sh
scripts/verify-events.sh --self-test
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/1.7.4 --base-ref origin/main
git diff --check
```

真实集成验收：

```bash
scripts/verify-events.sh
```

- 首次随机项目 `gopulse-events-521f8c89ef1b` 在新增真实 cursor 翻页处返回 HTTP `400`。诊断确认 `ParseOptions` 在 cursor 专用上限检查前错误应用了通用 256-byte 参数上限；该失败直接促成 cursor 长度修复和解析回归测试，失败运行不作为完成证据。
- 修复后随机项目 `gopulse-events-8a55e7e55d11` 首次通过；完成 Monitor watcher 可测试性重构后，又在最终随机项目 `gopulse-events-929dcda8a8a7` 复验通过，输出：`Failure, recovery, replay, offset, and mixed Events query closed end to end through index gopulse-events-v1-2026.09.05.`
- 脚本退出后随机 Compose 资源按既有 cleanup 清理。

## 5. 与计划的偏差

- Review 建议的最小两页测试最初在 Service 层通过，但真实脚本暴露了 HTTP 参数解析层的 cursor 长度缺陷；按实际失败依据将修复扩展到 `ParseOptions`，未扩展到其他查询功能。
- 未改变 MetricsLifecycle 公共接口；通过 lifecycle operation 串行、runtime identity 检查和 record identity 条件删除满足 replacement generation 隔离。
- 未执行全仓覆盖率、依赖审计、一般代码 Review或 Phase 11 前端工作。

## 6. 已知限制与后续事项

- EventMonitor sender 接口仍要求生产实现响应 context；本批保证调用方 `Close` deadline，不强制终止任意第三方阻塞 goroutine。生产 HTTP sender已有请求超时和 context cancellation。
- EventMonitor 仍是有界内存 best-effort source queue；该既定产品边界未改变。
- Phase 10 整改完成后，后续功能应使用新的权威开发批次，不继续在 `develop/1.7.4` 上实施。
