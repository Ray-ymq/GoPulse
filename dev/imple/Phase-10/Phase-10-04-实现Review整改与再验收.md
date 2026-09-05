# Phase-10-04：实现 Review 整改与再验收实施方案

## 1. 批次信息

- 目标版本：`1.7.4`。
- 开发分支：`develop/1.7.4`。
- 输入：`dev/review/2026-09-05-Phase-10实现Review报告.md` 的 P1-01、P2-01、P2-02、P2-03、P3-01。
- 范围：只修复 Review 已复现问题并补齐直接固定证据，不开展全仓审计或 Phase 11 功能。

## 2. 实施内容

1. Plugin Manager watcher 在任何 process record、Metrics、状态和事件副作用前完成 runtime identity fencing，并与生命周期操作串行；删除 process record 时校验完整 record identity。
2. EventMonitor `Close` 在调用方 context 到期后 cancel/stop 并立即返回，worker 可在 sender 最终返回后后台结束，重复 `Close` 安全。
3. Backend 补齐签名 cursor 两页、PIT/search_after 传递、terminal PIT close、空页和 `503 events_unavailable` 测试；真实 Events 脚本执行一次小页 cursor 翻页并在 Elasticsearch outage 确认 API `503`。
4. Events 查询按 event specification 校验 event/error 组合，拒绝不可能组合。
5. 更新 Phase 10 权威批次分配、历史实施记录的证据表述、版本和本批实施记录。

## 3. 验收与验证

- stale watcher 回归测试证明 replacement runtime、process record、Metrics lifecycle 和状态均不受影响。
- EventMonitor 正常 drain 仍通过；忽略 cancellation 的 sender 下，`Close` 按 deadline 返回，sender 释放后 worker 结束且重复关闭成功。
- Backend 测试证明合法 cursor 续页、terminal PIT close、空 `data`、HTTP `503/events_unavailable`，并拒绝 `exporter_plugin_failed/publish_failed` 与 `metrics_collection_failed/start_failed`。
- 固定完成门禁：

```bash
(cd monitor && go test -count=1 ./internal/events ./internal/plugin)
(cd monitor && go test -race -count=1 ./internal/events ./internal/plugin)
(cd monitor && go vet ./...)
(cd backend && go test -count=1 ./internal/eventquery ./internal/http/...)
(cd backend && go test -race -count=1 ./internal/eventquery ./internal/http/...)
(cd backend && go vet ./...)
bash -n scripts/verify-events.sh
scripts/verify-events.sh --self-test
scripts/verify-events.sh
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/1.7.4 --base-ref origin/main
git diff --check
```

## 4. 回归范围与完成条件

只回归直接受影响的 Monitor plugin/events、Backend eventquery/http 和 Events 真实链路。全部固定门禁通过、`VERSION` 与 Frontend 版本一致为 `1.7.4`、同名实施记录准确完成且无阻断问题时，本批完成。
