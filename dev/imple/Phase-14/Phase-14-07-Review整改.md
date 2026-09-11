# Phase-14-07：Review 整改实施方案

## 范围与基线

按用户要求继续使用权威分支 `develop/1.11.7`，基于 Review 提交 `d030bd5` 和已合入前六批的主线 `d16ed4a`，完整落实 `dev/review/2026-09-12-Phase-14实现Review报告.md` 的三项整改。版本与分支分配以 Phase 14 总方案为准。

- P2-01：shutdown 收集单插件 lock/stop 失败，继续遍历，最终汇总错误；沿用调用者 context，不创建新的总超时，不绕过 blocked slot、路径及进程归属校验。
- P3-01：包更新成功后重取可信 catalog，同步配置门禁与 revision；目录刷新失败单独提示更新已成功，避免重复上传。
- P3-02：总方案、第六批方案和记录增加准确的合入后状态，保留合入前历史；追加第七批分配与记录。
- 完成后同步 VERSION、Frontend package/lock 和 `.env.example` 为 `1.11.7`。既有 acceptance-only 插件版本和第六批验收 fixture 不随产品版本改写。

## 验收与固定完成门禁

1. Go 回归：损坏 Redis active revision 仍被阻断，shutdown 返回原安全错误，同时 MySQL collector 收到 Disable；保留已有正常停止与恢复测试。
2. Vue 组件回归：v1 更新到 v2 后无需手动刷新即可解除配置禁用与旧包提示；目录刷新失败时保留成功 DTO 和明确提示。
3. 最终代码下执行：
   - `cd monitor && go test ./internal/plugin ./internal/httpserver`
   - `cd frontend && npm test -- src/views/ObservabilityExportersView.test.ts src/services/exporters.test.ts`
   - `cd frontend && npm run typecheck`
   - 核对主线包含 `d16ed4a`、版本分配及受管版本一致；`git diff --check`、`git diff --cached --check`。
4. 不改变真实进程停止实现、总超时或制品合同，不重复完整 Compose、六插件安装、Browser E2E、全仓库测试和跨平台验收。仅遇具体回归时扩大范围并记录原因。

完成条件：三项整改落实、固定门禁通过、同名开发记录记录实际结果、版本同步、提交并推送指定分支。不得将推送分支等同于合入主线。

## PR 跟进验收补充

首次推送后的权威 CI 在空库迁移出现 `invalid connection`，因此本批追加仅针对 migration 连接读预算的修复及验证：普通业务超时不变；真实 MySQL 下超过 1 秒的响应能够完成；空库全套迁移、重复 up、最终 `dirty=0` 通过。固定本地检查为 platform/cmd-migrate/migrations 包测试及 vet，定向 platform integration 测试；推送后既有 CI 重新运行完整 Compose，不降低门禁。失败日志、复现与实际结果见同名开发记录。此跟进仍属于本批，版本不变。
