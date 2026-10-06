# Phase-21-01 实施日志

## 实际完成

- 在 `develop/2.3.1`（基于 `upstream/main` 的 `80b0852`）完成 Backend 运行角色装配：`combined` 保持兼容行为，`business` 负责业务接口、Redis、业务 Elasticsearch、RabbitMQ 与 Outbox，`platform` 负责管理/观测/告警接口及其客户端。
- 增加 `BACKEND_SERVICE_ROLE` 配置解析。未使用角色的依赖配置不再成为启动前置；实际使用的依赖、地址、secret 和固定别名仍严格校验。
- 保留共享 MySQL 用户库、JWT/Cookie 会话、探针、结构化日志和私有指标；平台角色通过同一数据库实时读取管理授权。
- 为业务与平台路由补充互斥性和独立准入测试；平台可在没有 Outbox dispatcher 的情况下完成 HTTP 生命周期。
- 增加三角色最小真实进程集成测试，覆盖就绪、业务 current-user、平台管理授权、错误角色 `404`、combined 并集和有界 SIGTERM 退出。
- 同步 `2.3.1` 完成版本元数据、Backend/运行时说明和能力状态。本批没有修改 Compose 双服务部署，未声明 Phase 21 的最终 S01～S07 收口。

## 实际变更文件

- `backend/cmd/server/main.go`
- `backend/cmd/server/roles.go`
- `backend/cmd/server/roles_test.go`
- `backend/cmd/server/roles_integration_test.go`
- `backend/internal/config/config.go`
- `backend/internal/config/service_role.go`
- `backend/internal/config/service_role_test.go`
- `backend/internal/http/router_roles_test.go`
- `backend/README.md`
- `dev/contracts/runtime-contracts.md`
- `dev/status/capability-status.md`
- `dev/imple/Phase-21/Phase-21-01-运行角色与接口装配分离.md`
- `VERSION`
- `.env.example`
- `frontend/package.json`
- `frontend/package-lock.json`
- `admin-frontend/package.json`
- `admin-frontend/package-lock.json`
- `deploy/runtime-contracts.json`
- 本日志文件

## 实际命令与结果

- `git fetch upstream --prune`：成功。
- `git switch -c develop/2.3.1 upstream/main`：成功；基线工作树干净。
- 计划命令 `rtk go -C backend test ./cmd/server ./internal/config ./internal/http` 与 `rtk go -C componentmetrics test ./...`：未能启动，shell exit `127`，环境中没有 `rtk`。按计划偏差改用等价的直接 Go 命令，未重试 `rtk`。
- `go -C backend test ./cmd/server ./internal/config ./internal/http && go -C componentmetrics test ./...`：通过。
- `go -C backend test ./cmd/server ./internal/config ./internal/http`：通过（R01 直接等价命令）。
- `go -C componentmetrics test ./...`：通过（R02 直接等价命令）。
- `go -C backend test -race ./cmd/server ./internal/config ./internal/http`：通过（R03 直接等价命令）。
- 新增准入测试第一次运行因测试夹具释放错误的 channel 导致失败；修正测试夹具后，`go -C backend test ./internal/http -run 'TestServiceRole' -count=1`：通过。
- R04 第一次运行 `go test -count=1 -tags=integration ./cmd/server -run '^TestIntegrationBackendServiceRoles$' -timeout=180s -v`：失败于迁移连接，退出原因 `connection_failure`；三角色未启动。该次归类为 acceptance-infrastructure failure。
- 使用独立临时 MySQL 容器 `mysql:8.4.0`，仅绑定 loopback `127.0.0.1:13306`，并以 `mysqladmin ping` 验证 `mysqld is alive`；未使用现有项目数据。
- 纠正夹具后再次运行同一 R04 命令：通过，三角色真实启动、路由/会话断言和 SIGTERM 退出均完成，测试 `PASS`。
- `docker stop gopulse-phase21-01-mysql-20261006` 及同名容器过滤检查：通过，临时 MySQL 已清理，无残留。

## 偏差、限制与后续

- `rtk` 不可用，因此固定门禁以直接等价命令执行；最终 R05 也使用仓库脚本的直接 `python3`/`git diff --check` 入口。
- R04 是最小真实角色装配检查，不启动完整 Redis、RabbitMQ、Elasticsearch、VictoriaMetrics 或 Monitor 集群；代码验证了 business/platform 对其各自客户端的角色化构造，完整双服务部署及管理查询/插件/告警链路留给 Phase-21-02/03。
- 当前 Compose 和运行合同的组件拓扑仍是默认 `combined`；本批只登记角色配置与产品版本，不提前实现双服务代理、采集映射或冻结候选。

## 门禁状态

- R01：通过。直接 `go test ./cmd/server ./internal/config ./internal/http`，并在最终准入夹具清理调整后复跑。
- R02：通过。直接在 `componentmetrics` 执行 `go test ./...`。
- R03：通过。直接执行 backend race 测试，并在最终准入夹具清理调整后复跑。
- R04：通过。独立 MySQL 夹具下三角色真实进程测试通过；夹具与子进程均已清理。
- R05：通过。`validate_versions.py`、`validate_branch.py` 和 `git diff --check` 均通过；另行执行 `verify_runtime_contracts.py --candidate 2.3.1` 通过。由于环境没有 `rtk`，固定命令使用等价直接入口。

## PR 集成回归修正

- PR #216 的第一次 GitHub Integration job 在迁移阶段失败，原因是集成测试无条件把 MySQL 端口改为本地隔离端口 `13306`，覆盖了 GitHub Actions 既有服务的 `3306` 配置；不是 Backend 角色装配失败。
- 修正 `roles_integration_test.go`：GitHub Actions 沿用现有服务环境，本地执行仍使用显式 loopback 非默认端口；增加默认环境变量填充，避免覆盖 CI 的隔离凭据和端口。
- 修正后执行普通 Backend 编译测试，以及使用 `GITHUB_ACTIONS=true`、CI 端口映射和新鲜 MySQL 夹具的 R04 兼容性复现：均通过；临时容器已清理。远程 PR 的新一轮 Integration job 待本修正提交触发。
