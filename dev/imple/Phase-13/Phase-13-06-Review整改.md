# Phase-13-06：Review 整改

## 范围

执行 `dev/review/2026-09-08-Phase-13实现Review报告.md` 的 P2-01–04、P3-01–02；使用用户指定的既有 Review 分支继续同一任务，不重建或重命名分支。版本和分支以总方案分配表为准。

## 验收合同

- 搜索在同一 PIT 内按 MySQL 存在事实补页，有界扫描耗尽预算后保留基于已消费 hit 的续页；空页有 cursor 时 UI 可继续加载。直接测试整页陈旧 hit 后的有效结果及预算续页。
- 新增向前 migration 保护活动通知/完整墓碑形状；实际 MySQL 验证合法活动、墓碑、非法组合以及 up/down。保持删除事务原子性，禁止半墓碑。必要时根据数据库实际限制调整外键实现并记录。
- `user_not_found` 在 Frontend 错误类型、解析和资料页保持明确语义，关注 route 参数统一为 `userId`。
- 总方案记录原五批 PR 已合入及整改分配；完成版本同步为 `1.10.6`。

## 固定完成门禁及停止条件

受影响 Go package tests；Frontend tests/typecheck/build；真实 MySQL migration/删除和搜索集成；`bash scripts/verify-business.sh --self-test`；`bash scripts/verify-business.sh`；`python3 scripts/ci/validate_versions.py`；`python3 scripts/ci/validate_branch.py --branch develop/1.10.6 --base-ref origin/main`；`git diff --check`。

通知持久化约束和搜索公共分页合同直接变化，需要执行对应数据库/Elasticsearch 集成；无 Compose 配置改动，不额外重复全量 Compose 门禁。门禁通过且无阻断失败后更新镜像实施记录、版本，提交并推送，停止扩展调查。
