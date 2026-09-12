# Phase-15-07 Review 整改

## 范围与验收

依据 `dev/review/2026-09-13-Phase-15实现Review报告.md`，仅关闭以下四项：
- P2-01：rules/current/history 接受 metrics/logs/events，拒绝未知 source；真实 repository 返回对应来源，签名 cursor 保留过滤条件。
- P2-02：Monitor failed 优先于新鲜 up=1，返回 degraded 与固定安全原因；running/up=1 保持 healthy。
- P2-03：catalog/rule/incident/managed-user/role-change/audit 接入严格运行时 validator，验证完整字段、枚举及安全嵌套结构；合法响应成功，缺失/额外/非法响应安全失败且不进入页面操作状态。
- P3-01：追加第六批主线事实，保留历史日志，登记第七批。

## 固定完成门禁

- `cd backend && go test ./internal/alert/... ./internal/adminoverview ./internal/user`
- 在受控 MySQL 中执行三源列表与 cursor HTTP/repository 集成回归（记录实际启动和执行命令）。
- `cd admin-frontend && npm test && npm run build`
- `cd frontend && npm run build`（版本元数据同步）。
- `git diff --check`、暂存区检查；同步 VERSION、双前端与既有环境版本元数据，编写对应实施日志。

不重跑未受影响的完整 Compose、故障注入、跨平台矩阵或依赖审计。所有上述验收通过、无阻断失败且日志与版本已提交，方为完成。
