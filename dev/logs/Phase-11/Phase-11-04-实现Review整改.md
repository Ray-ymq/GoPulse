# Phase-11-04 实现 Review 整改记录

## 1. 批次信息

- 日期：2026-09-05
- 分支：`develop/1.8.4`
- 目标/完成版本：`1.8.4`
- 依据：`dev/review/2026-09-05-Phase-11实现Review报告.md`

## 2. 实际完成

- 将 VictoriaMetrics clone transport 绑定到实际 `http.Client`，使 64 KiB response header 上限真实生效，并增加 oversized header 回归测试。
- 将 Monitor 非 2xx 响应收紧为固定 HTTP status/error code 配对；合法业务错误继续映射，配对不符统一返回 `monitor_unavailable`。
- Frontend Metrics validator 增加规范 UTC、固定 range/step、窗口顺序、点窗口/递增、series 唯一和容量检查；Event validator与 Backend 固定 message、SemVer、operation/error、state 和 metadata 组合保持一致。
- `permission_denied` 保留 authenticated 会话和用户身份，但立即把缓存 admin capability 降为普通用户；真实降权浏览器用例确认返回社交域后不再显示“可观测”入口。
- admin 未知管理子路由改为重定向可观测总览，并增加路由测试。
- 修正 Phase-11-03 远程门禁“仍待观察”的过期描述；在总方案分配 Phase-11-04；新增本批方案与记录。
- 隔离浏览器脚本的更新包版本改为读取根 `VERSION`，E2E 通过显式环境变量断言当前目标版本，避免版本批次硬编码。
- 根版本、Frontend package metadata 和 README 当前版本更新为 `1.8.4`。

## 3. 变更文件

- Backend：`backend/internal/metricquery/metricquery.go`、`metricquery_test.go`、`backend/internal/exporterplugin/client.go`、`client_test.go`。
- Frontend：`frontend/src/services/observability.ts`、`observability.test.ts`、`frontend/src/composables/useAuth.ts`、`frontend/src/main.ts`、`frontend/src/router/index.ts`、`index.test.ts`、`frontend/e2e/observability.spec.ts`、package metadata。
- 验收与文档：`scripts/verify-observability-ui.sh`、`README.md`、`VERSION`、Phase 11 总方案、Phase-11-04 方案、本记录及 Phase-11-03 记录。

## 4. 验证结果

最终 diff 上通过：

```text
(cd backend && go test -count=1 ./...)
(cd backend && go vet ./...)
(cd backend && go test -race -count=1 ./internal/metricquery ./internal/exporterplugin ./internal/http/...)
(cd frontend && npm test -- --run)             # 11 files / 58 tests
(cd frontend && npm run build)
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/1.8.4 --base-ref origin/main
scripts/verify-observability-ui.sh --self-test
scripts/verify-observability-ui.sh              # 7 passed；read-only verify、bundle scan、生命周期清理通过
git diff --check
```

真实浏览器固定门禁在最终成功前出现四次非最终失败，均被限定整改并记录：第一次 7 个浏览器用例通过后，read-only verify 发现更新包仍硬编码 `1.8.3`；随后两次分别发现 E2E 的两个 `1.8.3` 显示断言；改为由根版本传递后关闭。再一次运行遇到既有 Logs 异步索引分页暂态（首屏 50 条后未在 5 秒内出现更多记录），未修改产品行为，按同一最终 diff 重试后 7 个用例与全部清理通过。

## 5. 计划偏差、限制与后续

- 无范围扩展；只为版本 `1.8.4` 修正浏览器验收中的旧版本硬编码。
- 未执行通用依赖审计、覆盖率活动或无关回归矩阵。
- Review 报告作为历史评审证据保留原 Fail 结论；本记录与整改提交构成关闭证据。
- 当前无已知阻断项；复杂可视化、告警、任意查询等仍按 Phase 11 非目标处理。
