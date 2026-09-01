# Phase-01-07：Review 整改与阶段最终收口

## 1. 批次目标

以 `develop/0.2.7` 作为 Phase 1 Review 整改的权威分支，执行 `dev/review/2026-09-01-Phase-1实现Review报告.md`，关闭 P2-01、P2-02 和 P3-01，补全远程质量门禁证据，并在不扩大 Phase 1 业务范围的前提下完成阶段最终收口。

Phase 1 核心业务在 `0.2.6` 已完成。本批只修复阶段治理、Frontend 首次认证恢复故障体验和版本元数据一致性，不引入 Phase 2 RabbitMQ 业务能力。

## 2. 前置条件

- Review 基线 `592897b1080eb78483a2eeb49141671f16cfc8fe` 已进入远程 `main`。
- `update` 上的 Review 报告已通过 merge commit `40a7944` 进入远程 `main`。
- 已获取远程最新状态，并从 `origin/main` 创建用户指定的 `develop/0.2.7`。
- 工作树在开始实现前无未提交变更；不覆盖或提交用户拥有的 `.env`、`.run` 或其他无关文件。
- WSL2 Linux、Bash、Node.js/npm、Go 和 Docker Compose 继续作为本批实施与验收环境；冻结的 PowerShell 脚本不在范围内。

## 3. 实施范围

### 3.1 权威计划与阶段验收状态

- 将 Phase-01-02 至 Phase-01-06 的权威状态更新为“已完成”。
- 增加 `Phase-01-07 → 0.2.7 → develop/0.2.7` 的唯一分配，并明确它是 Review 整改批次。
- 在总实施方案中记录 Phase 1 核心交付版本、Review 整改版本、最终验收入口、远程 CI 证据和已接受的 cache-aside TTL 有界陈旧限制。
- 更新 Review 报告的增量整改结论，保留原始 Review 证据和“有条件通过”结论，不改写历史判断。

### 3.2 Frontend 认证恢复

- 认证状态增加可重试的恢复错误状态；仅合法的 `401 authentication_required` 将用户判定为匿名。
- 网络错误、5xx、非法 JSON/响应结构和非认证语义的 401 不清除为匿名状态。
- 路由守卫不得吞掉恢复异常后导航登录页；临时故障导航到独立恢复页，并保留原目标路由。
- 恢复页提供用户触发重试；服务恢复后使用现有 HttpOnly Cookie 恢复当前用户并返回原目标路由，真实 401 仍进入登录页。
- 删除应用启动时与路由守卫重复、且可能产生未处理 rejection 的独立初始化调用。
- 增加 composable、HTTP 和 router/component 自动化测试，覆盖网络错误、5xx、非法响应、重试成功及真实 401。

### 3.3 产品版本元数据

- 根 `VERSION` 更新为本批目标版本 `0.2.7`。
- `frontend/package.json` 与 `frontend/package-lock.json` 的根包版本同步为 `0.2.7`。
- 增加版本元数据校验脚本和单元测试，以根 `VERSION` 为唯一完成产品版本来源，并将校验接入质量门禁。
- README 说明版本同步规则和认证恢复行为。

### 3.4 远程证据补全

- 通过 GitHub API 查询 PR #27 及其 head commit 的 check runs。
- 只记录实际可读取的状态、commit 和检查名称；不得把 merge commit 上不存在的 check runs 写成已执行。

## 4. 不在本批范围

- Phase 2 的 Producer、Consumer、Outbox、RabbitMQ 重试或幂等消费。
- 修改已接受的 Redis cache-aside TTL 有界陈旧语义。
- 用户资料、刷新令牌、多设备会话或新的认证产品能力。
- 更新冻结在 `0.2.1` 能力基线的 PowerShell 脚本。
- 与本次 Review finding 无直接关系的 Backend 重构或 Frontend 视觉改版。

## 5. 验收标准

### 5.1 Frontend 直接行为

- `npm test -- --run` 通过，且测试明确证明：
  - 网络错误不会进入 `anonymous`；
  - 500/503 和非法响应不会触发假退出；
  - 服务恢复后可用原 Cookie 重试成功并回到原受保护路由；
  - 合法 `401 authentication_required` 仍清除认证状态并进入登录页；
  - 非法 401 响应不会触发全局未授权处理器。
- `npm run typecheck` 与 `npm run build` 通过。
- `scripts/verify-business.sh` 从隔离环境完整通过，确认注册、认证恢复和既有业务闭环无回归，且临时资源清理完成。

### 5.2 治理、版本与仓库检查

- `python3 -m unittest discover -s scripts/ci -p 'test_*.py'` 通过。
- `python3 scripts/ci/validate_versions.py` 通过。
- `python3 scripts/ci/validate_branch.py --branch develop/0.2.7 --base-ref origin/main` 通过并唯一映射到 Phase-01-07。
- `.github/workflows/quality-gates.yml` 可由 YAML 解析器读取，新增版本校验在 governance job 中执行。
- `bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-business.sh`、`scripts/verify-business.sh --self-test` 和 `git diff --check` 通过。

### 5.3 文档与完成条件

- 总实施方案、七份拆分方案、七份实施记录、Review 增量整改结论和根 `VERSION=0.2.7` 一致。
- Phase 1 权威计划明确说明核心业务在 `0.2.6` 完成、Review 整改在 `0.2.7` 完成，Phase 2 可读取无歧义的已关闭状态。
- PR #27 的远程质量门禁结果只按 GitHub API 实际返回内容记录。
- 已接受的 cache-aside 限制继续明确登记，不被误写为本批已消除。
- 本批直接验收和必要回归全部通过、无阻塞问题，实施记录只包含实际工作和实际结果。
