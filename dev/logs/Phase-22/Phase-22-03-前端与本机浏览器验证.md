# Phase-22-03：前端与本机浏览器验证

> 实际状态：已完成。目标版本 `2.4.3`，分支 `develop/2.4.3`，基线 `upstream/main` 的 `96c83f0`。

## 实际完成

- 增加 `make e2e [SCOPE=business|observe]` native 入口，复用 Phase 22 test Compose、锁、源码进程归属、端口预检和有界清理；默认 scope 为 business，未知 scope/端口明确失败。
- business scope 启动 test Backend、Worker、Indexer 与用户 Vite，复用现有业务 Playwright；observe scope 启动 test Backend、Router、Marshaller、Monitor、两套 Vite，使用 test 数据库内的管理员、普通用户和可降级管理员夹具。
- 通过 bootstrap `admin-role` 引导首个管理员，再使用真实管理 API 设置可降级管理员；退出时按外键顺序清理测试账号关联的通知、书签、点赞、评论、帖子和角色记录，保留 test 命名卷。
- 为两套 Vite 增加测试端口配置；用户 Vite 的 `/admin` 代理跟随 `ADMIN_FRONTEND_PORT`，并复用现有 Nginx 的 `/admin` 与旧观测路径 308 兼容映射。
- 将观测 Playwright 的合法 origin 改为 `GOPULSE_BASE_URL`，保留容器默认地址兼容性；增加 `verify-admin-frontend.sh --native` 委托两前端检查。
- 版本元数据同步到 `2.4.3`，显式准备对应 Monitor 开发镜像。

## 实际变更文件

- `Makefile`
- `frontend/vite.config.ts`
- `frontend/vite.config.test.ts`
- `frontend/e2e/compose-observability.spec.ts`
- `admin-frontend/vite.config.ts`
- `admin-frontend/vite.config.test.ts`
- `scripts/ci/local_development.py`
- `scripts/ci/test_local_development.py`
- `scripts/verify-admin-frontend.sh`
- `dev/validation/local-development-tests.md`
- `.env.example`
- `VERSION`
- `frontend/package.json`、`frontend/package-lock.json`
- `admin-frontend/package.json`、`admin-frontend/package-lock.json`
- 本日志文件

## 实际命令与结果

- `git fetch upstream`：通过；从最新 `upstream/main` 创建独立 worktree 和 `develop/2.4.3`。
- `python3 -m py_compile scripts/ci/local_development.py scripts/ci/test_local_development.py`：通过。
- `python3 -m unittest discover -s scripts/ci -p test_local_development.py`：通过，15 个测试通过。
- `python3 scripts/ci/validate_versions.py`：通过；前端、环境和 root `VERSION` 均为 `2.4.3`。
- `python3 scripts/ci/validate_branch.py --branch develop/2.4.3 --base-ref upstream/main`：通过。
- 两前端 `npm ci --no-audit --no-fund`：通过。
- 最终版本下 `make test MODULE=frontend`、`make test MODULE=admin-frontend`：通过，分别 18/72、13/54 文件/测试。
- 最终版本下两前端 `npm run typecheck` 与 `npm run build`：通过。
- `bash -n scripts/verify-admin-frontend.sh scripts/test-frontends.sh`：通过。
- `scripts/verify-admin-frontend.sh --native`：通过；两前端 Vitest/build 均通过。
- `make help`、`make -n e2e SCOPE=business`、`make -n e2e SCOPE=observe`：通过。
- `docker compose ... config --quiet`（business/observe test Compose）：通过。
- `make monitor-image`：通过；镜像为 `gopulse/monitor:local-4e6fb55df5c18773`。
- `make e2e SCOPE=business`：通过；最终 test 项目为 `gopulse-67b79bd5e2e1-test-243`，3 个 Playwright 用例通过并清理自有进程/容器。
- `make e2e SCOPE=observe`：通过；最终同版本 test 项目中管理权限 3 个用例和观测 admin 场景 1 个用例通过，实际指标、日志、运行事件、插件状态及权限断言成立，并完成关联数据清理。

## 偏差、修正与限制

- 首次 observe 夹具准备误将第二个管理员也交给 `admin-role promote`；核对本地命令契约后改为首个 bootstrap 加管理 API 角色设置。
- native Vite 初始只处理 `/admin`，随后按现有 Nginx 合同补齐 `/admin/observability` 及其 metrics/logs/events/exporters 的 exact 308 映射。
- 首次观测场景通过后，账号清理被帖子关联评论外键阻止；补充按关联数据依赖顺序的清理，并在最终候选重新通过。
- business 套件明确排除两条需要外部 search seed 的用例；未以 skip 或空数据替代观测数据门禁。
- 未修改页面业务实现、正式部署合同或历史专项验收；test 依赖卷保留，未执行全局 Docker prune。
