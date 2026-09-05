# Phase-11-04 实现 Review 整改

## 1. 目标与范围

仅关闭 `dev/review/2026-09-05-Phase-11实现Review报告.md` 的 P2-01～P2-05 与 P3-01～P3-02：绑定 VictoriaMetrics header 限制、校验 Monitor status/code 配对、收紧 Frontend Metrics/Event runtime validator、移除运行中降权后的 admin capability、修正未知管理子路由和 Phase-11-03 记录。目标版本 `1.8.4`，分支 `develop/1.8.4`。

不进行通用 HTTP client 审计、全量事件测试复制、无关重构或 Phase 12 工作。

## 2. 验收标准

1. VictoriaMetrics 正常响应可用，超过 64 KiB response header 的请求失败并由产品层映射为不可用。
2. Monitor 仅接受固定 HTTP status/error code 配对；合法 `404/plugin_not_found` 保持业务映射，`500/plugin_not_found` 收敛为 `monitor_unavailable`。
3. Frontend 接受合法 Metrics/Event DTO，拒绝非规范 UTC、错误 range/step/window、重复 series、非递增点及事件 message/SemVer/state/metadata 不可能组合。
4. `permission_denied` 保留登录会话但立即移除 admin capability；返回社交域后“可观测”入口消失，且管理页数据被清除。
5. admin 未知管理子路由重定向总览；普通用户仍先由 admin guard 拒绝。
6. Phase-11-03 记录无相互矛盾的远程结果；权威分配和版本校验通过。

## 3. 固定完成门禁

```bash
(cd backend && go test -count=1 ./...)
(cd backend && go vet ./...)
(cd backend && go test -race -count=1 ./internal/metricquery ./internal/exporterplugin ./internal/http/...)
(cd frontend && npm test -- --run)
(cd frontend && npm run build)
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/1.8.4 --base-ref origin/main
scripts/verify-observability-ui.sh
scripts/verify-observability-ui.sh --self-test
git diff --check
```

完成条件：上述验收标准和固定门禁全部通过，实施记录与实际命令一致，根及 Frontend 版本更新为 `1.8.4`；非阻断改进另记后续并停止。
