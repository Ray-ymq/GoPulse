# Phase-12-01：社交业务容器运行闭环实施记录

## 1. 批次信息

- 日期：2026-09-05
- 分支：`develop/1.9.1`
- 开工基线：最新 `upstream/main` 提交 `eaebfcd`，产品版本 `1.8.4`
- 目标/完成版本：`1.9.1`
- 实施方案：`dev/imple/Phase-12/Phase-12-01-社交业务容器运行闭环.md`
- 当前结论：已完成。Pull Request #104 于 2026-09-05 合入主远程 `main`，权威远程运行 `33982936997` 的全部适用固定 jobs 成功，满足实施方案第 10 节完成条件。

## 2. 实际完成

### 2.1 镜像与运行配置

- 新增根 `.dockerignore`，排除 Git、`.env`、`.run`、依赖目录、构建/浏览器制品、无关模块与文档，同时保留 Backend/Frontend 源码、lockfile、Compose/Docker 配置和 `VERSION`。
- 新增固定基础镜像的多阶段构建：
  - `golang:1.26.0-alpine3.23` → `alpine:3.23.3`；
  - `node:24.20.0-alpine3.23` → `nginx:1.29.4-alpine3.23-slim`；
  - `mcr.microsoft.com/playwright:v1.62.1-noble` 作为一次性浏览器验收镜像。
- 交付 `gopulse/backend:1.9.1`、`gopulse/business-worker:1.9.1`、`gopulse/search-indexer:1.9.1`、`gopulse/frontend:1.9.1` 和 `gopulse/acceptance:1.9.1`。应用最终镜像使用数字用户，写入统一 version/revision/source OCI label；Backend 镜像同时包含 `migrate`、`search-reindex`、`admin-role`。
- Go 构建使用 BuildKit module/build cache、`CGO_ENABLED=0`、`-trimpath` 和剥离符号的最终二进制。容器网络无法访问默认 `proxy.golang.org` 后，将可覆盖的构建代理默认值固定为 `https://goproxy.cn,direct`；Go checksum 校验未关闭。
- 新增 `GOPULSE_RUNTIME_MODE=host|container`。默认 host 模式要求 loopback 监听/依赖；container 模式要求通配 IP 监听和合法服务 DNS，拒绝未知模式、loopback/fixed-IP/`host.docker.internal` 下游及带非法 path/query/fragment 的内部 HTTP origin。Backend、Worker、Indexer 和 reindex loader 均通过代表性正负测试。

### 2.2 Frontend 与 Compose 业务拓扑

- Frontend builder 通过 `npm ci`、Vitest、typecheck 和 production build；最终 Nginx 层无 Node/npm/node_modules/sourcemap，以 UID/GID `101:101` 监听容器 8080。
- Nginx 实现 Vue Router history fallback，只代理 `/api/v1`、`/health`、`/ready`，保留请求/响应语义并设置 body/连接/读写超时；未知 `/api/` 返回 JSON 404。Docker DNS 使用有界重新解析，Backend 容器替换后无需重启 Frontend。
- 重构 `deploy/compose.yaml`：
  - 默认业务长运行服务为 Frontend、Backend、Business Worker、Search Indexer；
  - 官方业务依赖为 MySQL、Redis、RabbitMQ、Elasticsearch；
  - `migrate` 与 `search-init` 是 `restart: "no"`、成功退出门控的幂等作业；`admin-role` 和 `acceptance` 使用显式 profile；
  - Frontend 仅在 `edge`，Backend 跨 `edge/business`，Worker/Indexer/数据服务仅在 `business`；`business` 为 internal；
  - 默认只发布 Frontend/Backend 的 IPv4 loopback 用户端口，数据服务无宿主端口；
  - MySQL、Redis、RabbitMQ、Elasticsearch 使用 project-scoped 命名卷；应用容器无业务状态卷。
- 保留 Kafka/VictoriaMetrics 的 observability profile 定义供下批扩展，但 Phase-12-01 默认启动与主验收不启动它们。新增 `deploy/compose.debug.yaml`，仅供既有 source-level `verify-business.sh` 显式发布随机 loopback 数据端口。

### 2.3 生命周期、验收与 CI

- `scripts/dev.sh` 改为 Docker/Compose 原生入口：不再构建/启动宿主 Go 进程或 Vite，不再写 `.run/*.json` PID 记录；构建镜像、启动依赖/初始化作业/应用并调用容器只读 smoke。
- `scripts/verify.sh` 改为 Compose project 的只读检查：验证 service/job 状态、project/service/working-directory label、应用镜像数字用户与版本、默认端口边界，并在 one-shot acceptance 容器中执行 HTTP/SPA smoke。
- `scripts/down.sh` 改为可重入、强归属 Compose 清理；默认只删除容器/网络并保留卷，删除卷需同时提供 `--volumes --confirm-project <name>`，不删除镜像。
- 新增 `scripts/verify-compose.sh`：
  - `--self-test` 在访问 Docker 前拒绝不合法 project/host；
  - `--business` 生成随机 project、临时 env、系统分配的 Frontend/Backend loopback 端口和全新卷；
  - 构建并检查镜像 label/user/entrypoint/runtime 内容，检查网络和宿主端口；
  - 使用容器内 Chromium 完成注册、登录/退出、Cookie、帖子、评论、点赞、通知、SPA deep-link、API JSON 错误与搜索；
  - 验证 migration/search-init 重跑、Redis fallback、Worker/Indexer 暂停恢复、Backend/Worker/Indexer 替换、三进程有界信号关闭，以及保留卷的全 project down/up；
  - 正常/失败/signal cleanup 仅删除随机 project，并核对开工前已有 container/network/volume ID 仍存在。
- 质量门禁新增 container-only business job；脚本/Compose job 增加 `verify-compose.sh --self-test`、两用户端口和内网静态契约。Phase-12-01 因明确尚未容器化 Monitor/Router/Marshaller/Exporter，对 `develop/1.9.1` 暂停旧的 host-lifecycle observability browser job；Phase-12-02 必须在完整容器拓扑上恢复该门禁。
- 根 `VERSION`、Frontend package metadata 和 `.env.example` Compose tag 更新为 `1.9.1`；版本校验器新增 `.env.example` 同步检查。

## 3. 变更文件

- 镜像/Compose：`.dockerignore`、`.env.example`、`deploy/compose.yaml`、`deploy/compose.debug.yaml`、`deploy/docker/backend.Dockerfile`、`deploy/docker/frontend.Dockerfile`、`deploy/docker/acceptance.Dockerfile`、`deploy/docker/frontend/nginx.conf`。
- Backend 配置：`backend/internal/config/config.go`、`worker.go`、`search_indexer.go`、`runtime_mode.go` 及对应测试。
- Frontend 验收：`frontend/e2e/compose-smoke.spec.ts`、`frontend/e2e/compose-business.spec.ts`、package metadata。
- 生命周期与 CI：`scripts/dev.sh`、`scripts/down.sh`、`scripts/verify.sh`、`scripts/verify-compose.sh`、`scripts/verify-business.sh`、`scripts/verify-observability-ui.sh`、`scripts/ci/**`、`.github/workflows/quality-gates.yml`。
- 文档与治理：`README.md`、`backend/README.md`、Phase 12 总方案、本批方案、本记录、`VERSION`。

## 4. 验证结果

最终运行行为未再变化后通过：

```text
(cd backend && test -z "$(gofmt -l .)")
(cd backend && go test -count=1 ./...)
(cd backend && go vet ./...)
(cd backend && go test -race -count=1 ./internal/config ./internal/http/... ./internal/worker ./internal/search/...)
(cd frontend && npm test -- --run)                # 11 files / 58 tests
(cd frontend && npm run build)
python3 -m unittest discover -s scripts/ci -p 'test_*.py'  # 26 tests
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/1.9.1 --base-ref upstream/main
bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-compose.sh
docker compose --env-file .env.example --file deploy/compose.yaml config --quiet
scripts/verify-compose.sh --self-test
scripts/verify-compose.sh --business
scripts/verify-business.sh --self-test
scripts/verify-observability-ui.sh --self-test
GOPULSE_VERSION=$(cat VERSION) GOPULSE_REVISION=$(git rev-parse HEAD) \
  docker compose --env-file .env.example \
    --file deploy/compose.yaml --file deploy/compose.debug.yaml config --quiet
git diff --check
```

`verify-compose.sh --business` 的最终成功矩阵实际完成：冷启动、镜像合同、网络/端口、初始化幂等、production SPA/API smoke、浏览器社交/通知/搜索、Redis fallback、Worker 恢复、Indexer 恢复、三应用容器替换、三应用 SIGTERM clean exit，以及保留卷 down/up 后事实恢复。结束后随机 project 的容器、网络和卷均不存在。

### 4.1 远程门禁、Pull Request 与合入

- 修正默认 Compose profile 的 internal network 静态计数后，权威 push 运行 `33982936997` 完成且结论为 success。Branch governance、Backend、Message Router、Marshaller、Backend log pipeline、Plugin lifecycle Events pipeline、Monitor、Redis Exporter、Frontend、Scripts and Compose、Integration、Container-only business acceptance 全部成功。
- `Observability browser acceptance` 按 Phase-12-01 明确边界在 `develop/1.9.1` 上跳过；Phase-12-02 恢复该门禁的责任保持不变。
- `Open PR and enable auto-merge` job 成功，自动化创建并 squash merge Pull Request #104；主远程 `main` 落点为 `d9a5602475eb95c5bcbdc7771e32d8e37fa6253a`，远程开发分支已删除。

实施期间出现并关闭的非最终失败：

1. 两次 Backend 镜像构建在 `proxy.golang.org` 下载 module 时 360 秒超时；验证 `goproxy.cn` 可达后改为可覆盖的固定代理并构建成功。
2. 首次容器主验收发现 Playwright 工作目录不可写，同时发现 Compose 的 authoritative `project.working_dir` 是 `deploy/` 且命令替换内拒绝逻辑必须显式 `return`；修正 acceptance image 权限和强归属函数后关闭。
3. 第二次主验收的 smoke 错把登录页 heading 断言为按钮文案“登录”；改为真实 heading“欢迎回来”后，最终完整矩阵通过。
4. 最终 Python 门禁首次运行发现新增 CI job 后 job-count 测试仍为旧值；只更新治理测试预期，随后 26 个测试全部通过。Backend 与 Frontend 已成功的门禁未因该纯测试变更重复运行。
5. 首次远程运行 `33982738914` 的 `Scripts and Compose` job 发现静态断言按两个 internal network 计数，但默认 profile 的 Compose 渲染会省略尚未启用服务引用的 `observability` network；将默认拓扑断言收敛为实际存在的一个 internal `business` network，未改变运行配置。

## 5. 计划偏差、限制与后续

- 方案预计的容器业务闭环、幂等作业、持久化/恢复、强归属清理和无宿主 Go/Node/npm/curl 运行目标均已实现；未容器化 Monitor、Router、Marshaller 和 Redis Exporter，也未启动 Kafka/VM，符合本批边界。
- 为让既有 source-level `verify-business.sh` 在默认数据端口移除后仍有明确入口，增加了 loopback-only `deploy/compose.debug.yaml`；它不被 `dev.sh`、`verify.sh` 或容器主验收加载。
- 未重复运行与本批无直接契约变化的历史全量 observability browser 矩阵；其 CI 暂停原因已显式写入 workflow/README，Phase-12-02 必须恢复。
- 未执行依赖审计、覆盖率扩张、Kubernetes、生产密钥管理、镜像签名/SBOM、多架构发布或可观测容器化。
- 本地与远程均无已知阻断项。权威远程运行 `33982936997` 成功，自动化创建并 squash merge Pull Request #104，主远程 `main` 落点为 `d9a5602475eb95c5bcbdc7771e32d8e37fa6253a`，远程 `develop/1.9.1` 已删除。
