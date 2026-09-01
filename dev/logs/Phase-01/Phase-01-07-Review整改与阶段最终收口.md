# Phase-01-07：Review 整改与阶段最终收口开发记录

## 1. 执行信息

- 日期：2026-09-02
- 分支：`develop/0.2.7`
- 目标版本：`0.2.7`
- 起始基线：`origin/main` 的 `40a7944`（包含 Phase 1 Review 报告）
- Review 核心实现基线：`592897b1080eb78483a2eeb49141671f16cfc8fe`
- 依据：`dev/review/2026-09-01-Phase-1实现Review报告.md`
- 执行环境：WSL2 Linux、Bash、Node.js/npm、Go、Docker Compose

## 2. 实际完成工作

### 2.1 权威计划与 Review 收口

1. 将 Phase 1 总实施方案中 Phase-01-02 至 Phase-01-06 的状态由“待实施”修正为“已完成”。
2. 新增 `Phase-01-07 → 0.2.7 → develop/0.2.7` 的唯一权威分配，并新增对应拆分实施方案。
3. 将 Phase 1 描述修正为：前六批在 `0.2.6` 完成核心业务，第七批在 `0.2.7` 完成 Review 整改与阶段最终收口。
4. 在总实施方案中补充最终验收摘要、PR #27 远程质量门禁证据和继续接受的 cache-aside TTL 有界陈旧限制。
5. 在原 Review 报告末尾追加 Phase-01-07 增量整改结论，保留原始 Review 的“有条件通过”历史判断，并将 P2-01、P2-02、P3-01 标记为已关闭。

### 2.2 Frontend 认证恢复

1. `AuthStatus` 增加 `error` 状态。只有合法 `401 authentication_required` 将状态设为 `anonymous`；网络、5xx 和非法响应将状态设为可再次调用 `initialize()` 的 `error`。
2. HTTP service 先验证 401 错误 envelope，仅对明确的 `authentication_required` 调用全局未授权处理器；非法 401 不再清除认证状态。
3. 路由新增 `/auth-recovery`。路由守卫遇到临时认证恢复异常时导航到该页面并把原目标保存在 `redirect` query，而不是导航到 `/login`。
4. 新增恢复页面和“重试认证恢复”操作。服务恢复后，Frontend 使用仍存在的 HttpOnly Cookie 恢复当前用户，并返回原受保护路由；重试得到真实 401 时进入登录页。
5. 删除 `main.ts` 中与路由守卫重复的独立初始化调用，避免首次请求失败产生未处理 rejection。
6. 新增或扩展测试，覆盖并发初始化、真实 401、网络错误后重试、5xx、非法响应、非法 401、恢复页导航和返回原目标路由。

### 2.3 产品版本元数据与自动门禁

1. 根 `VERSION` 更新为 `0.2.7`。
2. `frontend/package.json`、`frontend/package-lock.json` 顶层版本和 lockfile 根包版本同步为 `0.2.7`。
3. 新增 `scripts/ci/validate_versions.py`，校验根版本使用三段 SemVer，且 Frontend npm 元数据与根 `VERSION` 一致。
4. 新增 4 项版本校验单元测试，覆盖一致状态、package 漂移、lockfile 根包漂移和非法根版本。
5. 将版本元数据校验接入 reusable quality gates 的 governance job。
6. README 更新当前产品版本、`/auth-recovery` 行为、版本元数据规则和本地验证命令。

### 2.4 远程质量门禁证据

本地未安装 GitHub CLI，`gh pr checks 27` 和 `gh pr view 27` 实际返回 `gh: command not found`。随后使用 GitHub REST API 实际读取：

- PR #27 已于 `2026-09-01T17:17:54Z` 合并，merge commit 为 `592897b1080eb78483a2eeb49141671f16cfc8fe`。
- PR head commit 为 `2505104503a8045dd97d1a60d413aa848ca71a2c`。
- 该 head commit 返回 6 个完成且结论为 `success` 的 check runs：Branch governance、Backend、Frontend、Scripts and Compose、Integration、Open PR and enable auto-merge。
- merge commit 自身返回 0 个 check runs；文档只把 head commit 的实际结果作为远程证据。

## 3. 实际变更文件

- `.github/workflows/quality-gates.yml`
- `README.md`
- `VERSION`
- `dev/imple/Phase-01/Phase-01-总实施方案.md`
- `dev/imple/Phase-01/Phase-01-07-Review整改与阶段最终收口.md`
- `dev/logs/Phase-01/Phase-01-07-Review整改与阶段最终收口.md`
- `dev/review/2026-09-01-Phase-1实现Review报告.md`
- `frontend/package.json`
- `frontend/package-lock.json`
- `frontend/src/composables/useAuth.ts`
- `frontend/src/composables/useAuth.test.ts`
- `frontend/src/main.ts`
- `frontend/src/router/index.ts`
- `frontend/src/router/index.test.ts`
- `frontend/src/services/http.ts`
- `frontend/src/services/http.test.ts`
- `frontend/src/styles.css`
- `frontend/src/views/AuthRecoveryView.vue`
- `scripts/ci/validate_versions.py`
- `scripts/ci/test_validate_versions.py`

未修改冻结的 `scripts/*.ps1`、Backend 业务代码、用户 `.env` 或 `.run`。

## 4. 验证命令与结果

### 4.1 Frontend

```bash
cd frontend
npm test -- --run
npm run build
```

结果：通过。Vitest 共 7 个测试文件、39 项测试全部通过；build 内含 `vue-tsc --noEmit`，Vite production build 成功。npm 输出版本为 `gopulse-frontend@0.2.7`。

### 4.2 治理、版本和工作流

```bash
python3 -m unittest discover -s scripts/ci -p 'test_*.py'
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py \
  --branch develop/0.2.7 \
  --base-ref origin/main
```

结果：通过。Python 共 15 项测试全部成功；版本元数据与根 `VERSION` 一致；分支治理将 `develop/0.2.7` 唯一映射到 Phase-01-07。

实际使用 Python/PyYAML 解析 `.github/workflows/quality-gates.yml`、`auto-pr-merge.yml` 和 `ci.yml`，三份 workflow 均成功解析。

### 4.3 Bash 与安全自测

```bash
bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-business.sh
scripts/verify-business.sh --self-test
```

结果：通过。安全自测接受 1 个合法目标，并在不访问 Docker 的情况下拒绝 6 个不安全目标。

### 4.4 隔离完整业务验收

```bash
scripts/verify-business.sh
```

结果：通过。隔离项目 `gopulse-acceptance-018ff92e4bf5` 完成：

- 从空数据库迁移和应用启动；
- API 注册、认证恢复、发帖、分页、评论、点赞和取消点赞；
- Playwright 真实 Chromium 业务流程 1/1 通过；
- Backend 重启后业务事实保持；
- Redis 清空后的缓存重建；
- Redis 故障时 MySQL 降级和 readiness 报告；
- Redis 恢复后无需重启 Backend 即恢复能力；
- 验收结束后只清理本项目资源。

验收结束后按 Compose project label 查询容器、网络和 volumes，结果均为空。

### 4.5 格式与提交前检查

```bash
git diff --check
```

结果：通过，无空白错误。

## 5. 与计划的偏差及原因

- 本地没有安装 `gh`，因此未使用 GitHub CLI 获取 PR #27 结果；改用公开 GitHub REST API读取同一远程事实，并明确区分 PR head checks 与 merge commit 上不存在的 checks。
- 未重复执行 Backend 单元、vet、race 和独立 integration 测试。原因是本批没有修改 Backend 代码、配置或依赖，2026-09-01 Review 已在相同核心基线上通过这些检查；根据验收范围规则，本批使用隔离完整业务验收验证受认证路由直接影响的必要跨组件回归。
- 未修改 cache-aside 实现；这是 Review 明确登记且被方案接受的限制，不属于 P2/P3 整改完成条件。

## 6. 已知限制与后续项

- `/auth-recovery` 当前提供用户触发重试，不实现自动指数退避；这已满足本批“自动或用户触发恢复路径”的完成条件。后续如增加全局离线/服务状态，可统一自动重试策略。
- Redis cache-aside 并发旧回填仍可能在 TTL 内提供旧公共计数；MySQL 保持唯一事实源，后续通过指标观察，不在本批引入强一致缓存复杂度。
- `develop/0.2.7` 推送后的新远程 CI 结果不在本地提交前记录中；本记录只声明本地实际执行结果和已读取的 PR #27 远程历史证据。

## 7. 完成结论

Phase-01-07 已关闭 Phase 1 Review 的 P2-01、P2-02 和 P3-01，补全 PR #27 远程质量门禁证据，保留已接受的 cache-aside 限制。Frontend、治理、版本、工作流解析、Bash 安全自测和隔离完整业务验收全部通过，根 `VERSION` 与 Frontend npm 元数据均为 `0.2.7`。Phase 1 最终通过阶段验收，可作为 Phase 2 的稳定输入基线。
