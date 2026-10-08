# Phase-22-02：后端原生功能验证

> 实际状态：已完成。目标版本 `2.4.2`，分支 `develop/2.4.2`，工作树 `/home/ray/GoPulse/.worktrees/phase22-02`。

## 实际完成

- 增加 `make integration SCOPE=business|observe` 原生入口、test Compose 隔离、测试锁、固定端口预检、候选版本项目名和归属清理。
- 增加业务 HTTP 完整链路，实际验证注册/登录、发帖、评论、点赞幂等、Outbox/RabbitMQ、通知和搜索。
- 增加 O02 MySQL 集成入口，实际验证告警状态、租约、恢复和审计断言。
- 增加观测 HTTP 链路，实际验证管理员/普通用户权限、Redis 插件动作、指标、请求日志和运行事件查询。
- 将 Redis host-mode 测试端口和 Monitor Redis exporter 采集端口改为配置驱动，同时保持 container mode 的 `redis:6379` 合同。
- 将本批测试依赖端口改为 MySQL `23306`、Redis `26379`、RabbitMQ `25672`/管理端口 `25673`、业务 Elasticsearch `29200`、观测 Elasticsearch `29201`；开发端口仍为 `3306/6379/5672/9200/9201`。
- 版本元数据已同步到 `2.4.2`，Monitor 镜像已按最终候选输入重新构建。

## 实际变更文件

- `.env.example`、`VERSION`、`Makefile`
- `frontend/package.json`、`frontend/package-lock.json`
- `admin-frontend/package.json`、`admin-frontend/package-lock.json`
- `deploy/compose.local-linux.yaml`
- `backend/cmd/server/roles_integration_test.go`
- `backend/internal/alert/repository_test.go`、`backend/internal/alert/repository_integration_test.go`
- `backend/internal/http/business_flow_integration_test.go`、`backend/internal/http/observability_flow_integration_test.go`
- `backend/internal/integrationtest/environment.go`
- `monitor/cmd/monitor/main.go`、`monitor/cmd/monitor/main_test.go`
- `monitor/internal/plugin/contract_v2_test.go`、`monitor/internal/plugin/redis_config.go`
- `scripts/ci/local_development.py`、`scripts/ci/test_local_development.py`
- `scripts/verify-business.sh`
- `dev/validation/local-development-tests.md`
- `dev/imple/Phase-22/Phase-22-02-后端原生功能验证.md`
- 本日志文件

## 实际命令与结果

### 分支、工具和静态检查

- `git fetch origin`：通过；从 `origin/main` 创建 `develop/2.4.2` 独立 worktree。
- `python3 -m py_compile scripts/ci/local_development.py scripts/ci/test_local_development.py`：通过。
- `python3 -m unittest discover -s scripts/ci -p test_local_development.py`：通过，12 个测试通过。
- `bash -n scripts/verify-business.sh`：通过。
- `bash scripts/verify-business.sh --self-test`：通过。
- `git diff --check`：通过。
- `docker info --format '{{.ServerVersion}}'`：通过，Docker Server `29.7.2`。
- `docker compose version`：通过，Docker Compose `v5.5.0`。
- `docker compose ... config`（最终 test/observe Compose 文件）：通过。

### 模块门禁

以下命令各执行一次并通过：

```text
make test MODULE=backend
make test MODULE=monitor
make test MODULE=router
make test MODULE=marshaller
make test MODULE=componentmetrics
make test MODULE=exporters/redis
make test MODULE=exporters/mysql
make test MODULE=exporters/rabbitmq
make test MODULE=exporters/elasticsearch
make test MODULE=exporters/kafka
make test MODULE=exporters/victoriametrics
```

另有通过：`go -C backend test -tags=integration -run '^$' ./cmd/server ./internal/alert ./internal/http` 和 `go -C backend test -tags=integration,observability_integration -run '^$' ./internal/http`。

### 真实业务与观测门禁

- `make integration SCOPE=business`：最终候选通过。实际命令为 `go -C backend test -p 1 -tags=integration ./...`；Backend 全包、O02 告警集成和业务 HTTP 全链路均通过。测试项目为 `gopulse-62320475d701-test-242`。
- `make integration SCOPE=observe`：最终候选通过。实际命令为 `go -C backend test -p 1 -tags=integration,observability_integration ./internal/http -run '^TestObservabilityFlowIntegration$' -count=1 -timeout 6m`，耗时约 34.6 秒；指标、日志、运行事件及权限失败断言通过。测试项目为 `gopulse-62320475d701-test-242`。
- `make integration SCOPE=observe` 结束时仅清理本次创建的 `observe_admin_<workspace-id>` 和对应 `bootstrap_super_admin` 记录，随后 business 门禁从空 bootstrap 状态通过。
- 远端 GitHub Actions run `37758791665` 首次失败：Integration job 的 O02 用户名超过 `VARCHAR(32)`，且 CI 未准备搜索 alias、并行执行共享 Elasticsearch/MySQL 集成包，导致 business 搜索等待和 named lock 级联超时；其它质量门禁通过。
- 修正为短 O02 用户名、允许本机与 CI 两组隔离 RabbitMQ/Elasticsearch 端点，并在 CI migration 后执行 `search-reindex --if-missing`、以 `go test -p 1` 串行运行；本地已按同样命令完成全量通过。

## 失败、修正与偏差

- 首次 business 预检发现 `13306` 被 WSL2 宿主机 `WXWork.exe`（PID `22912`，`G:\WXWork\WXWork.exe -min -autorun`）占用。未停止该进程；按用户确认将测试依赖迁移到 `23306/26379/25672/25673/29200/29201`。
- observe 初次失败为 Monitor host-mode Redis 配置固定要求 `127.0.0.1:6379`；修正为只校验 loopback 和合法端口，并让 Monitor 使用配置的 Redis exporter HTTP 端口 `19121`。container mode 仍固定 `redis:6379`。
- 后续修正了 Monitor 启动等待 Redis 健康、观测测试公开指标标签边界、日志 JSON 字段标签和候选版本插件卷复用问题。
- observe 与 business 共用同一 test 数据库时，observe 创建的管理员 bootstrap 会干扰 roles 集成用例；最终在 observe 退出路径增加归属清理，只删除该测试生成的确定账号及 bootstrap 记录。
- 没有修改正式部署合同、开发端口或业务实现；没有删除测试卷，也没有执行全局 Docker 清理。

## L02 资源与数据核对

- 最终 `integration-state.json` 为 `status: passed`，`processes` 为空且记录了 `stopped_at`。
- 最终测试项目无运行容器；test/observe Compose 项目均执行 `down --remove-orphans`。
- `docker volume ls` 仍可见 `gopulse-62320475d701-test-242_*` 及历史 `gopulse-62320475d701-test_*` 命名卷，未删除数据卷。
- 没有启动或停止 dev 项目；开发依赖端口和开发卷未被 test 项目复用。

## 能力边界与后续项

- 本批固定门禁证明原生业务及短观测链路；完整浏览器、长期告警评估、故障恢复、容量、完整 Trace、插件制品和发布证据仍由 03/04 或保留专项工具承担。
- `dev/validation/local-development-tests.md` 登记了本批原生入口与保留专项入口，未把模块单测宣称为跨进程验收。
