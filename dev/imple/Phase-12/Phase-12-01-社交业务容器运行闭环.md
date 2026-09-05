# Phase-12-01：社交业务容器运行闭环实施方案

> 当前状态：已完成。Pull Request #104 已于 2026-09-05 合入主远程 `main`，权威远程运行 `33982936997` 的全部适用固定 jobs 成功。本文档只定义首个执行批次的范围与验收合同；目标版本、开发分支和执行顺序以 `Phase-12-总实施方案.md` 的权威分配表为准。

## 1. 批次目标

将现有宿主 Frontend、Backend、Business Worker 和 Search Indexer 运行方式收敛为完整可启动的社交业务容器闭环：

```text
Frontend 静态容器
  → Backend 容器 + Outbox Dispatcher
      → MySQL / Redis / RabbitMQ / Elasticsearch
      → Business Worker 容器
      → Search Indexer 容器

MySQL health → migrate up 成功
MySQL + Elasticsearch health + migrate → search initialize 成功
→ 浏览器注册/登录/帖子/评论/点赞/通知/搜索
```

批次完成必须同时证明：

- 宿主无需 Go、Node.js 或 npm 即可构建和启动本批全部应用镜像、官方业务基础设施与初始化作业。
- 浏览器只访问 Frontend/Backend loopback 入口，完成真实 MySQL/RabbitMQ/Elasticsearch 支撑的社交和搜索闭环。
- 业务容器之间仅使用 Compose service name，一次性作业幂等，代表性容器重启和保留卷的 down/up 不丢失必要事实。
- 本批建立的镜像、网络、标签、作业、健康和验收结构可直接由 Phase-12-02 扩展，不需推倒重做。

只构建镜像、只容器化 Backend，或在宿主启动 Vite/Worker/Indexer 补齐闭环，均不构成本批完成。

## 2. 前置条件

- 从包含 Phase 11 最终能力的最新 `upstream/main` 创建总方案分配的本批开发分支，根与 Frontend 版本为开工基线版本。
- 重新核对 Backend module 中 `server`、`business-worker`、`search-indexer`、`migrate`、`search-reindex`、`admin-role` 命令的环境配置、工作目录、信号处理和退出码。
- 核对 Frontend Vite proxy/SPA 路由、Cookie 和 Backend HTTP 契约；核对 MySQL/Redis/RabbitMQ/Elasticsearch 当前官方镜像、健康检查和卷。
- 保存日常 Compose project/container/network/volume/image、端口、工作区改动和当前宿主进程快照，本批验收只使用随机强归属资源。
- 如发现 Phase 11 未合入、版本不一致、已有配置契约变化或现有业务回归失败，先处理前置问题，不用容器改造掩盖。

## 3. 实施范围

### 3.1 构建上下文与通用镜像契约

- 新增根 `.dockerignore` 或等价最小上下文，排除 `.git`、`.env`、`.run`、`node_modules`、dist、本地二进制、测试制品和无关文档，确保 VERSION/lockfile/source 可用。
- 固定 builder/runtime base version，使用 BuildKit cache mount 与 multi-stage build；最终层不含源码、VCS、Go/Node toolchain 或构建凭据。
- 统一接收 root VERSION 与 Git revision，将二者写入 OCI label；本地 tag 不使用 `latest`，同一批次不生成多个矛盾版本。
- 最终镜像使用非 root 数字用户、明确工作目录和 exec-form entrypoint，并验证 PID 1 可收到 SIGTERM。

### 3.2 Backend、Worker、Indexer 与工具镜像

- 从 Backend module 构建 `gopulse/backend`、`gopulse/business-worker` 和 `gopulse/search-indexer` 独立最终镜像，不在容器启动时 `go run`。
- Backend 镜像包含同提交、同版本的 `migrate`、`search-reindex` 和 `admin-role` 二进制，但默认 entrypoint 仍是 server；运维命令只由 Compose one-shot 显式覆盖。
- `HTTP_HOST=0.0.0.0`，MySQL/Redis 使用 `mysql`/`redis` service name，RabbitMQ URL 使用 `rabbitmq:5672`，Elasticsearch 使用 `elasticsearch:9200`；禁止 `localhost`、`127.0.0.1`、`host.docker.internal` 或固定容器 IP 作为容器下游。
- 本批不要求 Monitor 已容器化；社交闭环不依赖 log shipper 成功。如保留可观测配置占位，必须显式展示局部不可用，不把下游加入业务启动阻断条件。
- Worker 和 Indexer 无公共端口，不为了 Docker healthcheck 新增不必要 HTTP API。使用进程运行、实际队列收敛与安全日志作为验收证据。
- 容器停止必须触发 Backend Outbox/log shipper、Worker 和 Indexer 的既有有界 shutdown，不留宿主子进程或需要 PID file 清理。

### 3.3 Frontend 生产容器

- builder 使用当前 Node/npm 主版本和 `npm ci`，先执行 typecheck/test 中适合构建的固定门禁，再生成 production dist。
- 最终层使用固定静态 Web server，以非 root 运行，只监听非特权容器端口。不包含 Node/npm/node_modules 和本地 `.env`。
- 实现 Vue Router history fallback；对 `/api/v1`、`/health`、`/ready` 保留方法、body、Cookie、status 和必要 header 代理到 `backend:8080`，未知 API 不回退到 HTML。
- 从浏览器访问 Frontend loopback 入口时，现有认证 Cookie、注册/登录/退出、社交路由与搜索页契约保持。
- 对反向代理设置有界 connect/read/send timeout 和 request body 限制，但不修改 Backend 现有业务 body limit 或新增宽泛 CORS。

### 3.4 业务 Compose 拓扑与一次性作业

- 在现有官方 MySQL/Redis/RabbitMQ/Elasticsearch 服务上加入 Frontend、Backend、Worker、Indexer，不声明 `container_name`，使用 project-scoped network/volume。
- 建立 `edge`、`business` 网络：Frontend 只连接 edge；Backend 连接 edge/business；Worker/Indexer 和数据服务只连接 business。business 设为 internal，默认不发布数据服务端口。
- `migrate` 在 MySQL healthy 后执行且只有退出 0 才允许 Backend/Worker/Indexer 继续；`search-init` 在 migration 与 Elasticsearch healthy 后执行 `--if-missing`，不删除已有代际/别名。
- `admin-role` 作为手工/验收用 profile 或 `docker compose run --rm` 命令，不常驻、不发布端口、不将数据库凭据显示在命令帮助或普通日志。
- 长运行容器使用有界 restart 政策与 stop grace period；一次性作业 `restart: "no"`，失败可见且不通过无限重启掩盖。

### 3.5 持久化、重启与故障回归

- 保留 MySQL、Redis、RabbitMQ、Elasticsearch 命名卷；Frontend/Backend/Worker/Indexer 不新增本地业务状态卷。
- 在发布、评论、点赞、通知和搜索证据生成后，分别替换 Backend、Worker、Indexer 容器，确认 MySQL/RabbitMQ/ES 事实与消费恢复。
- 对整个随机 project 执行保留卷的 down/up，确认 migration/search-init 可重跑、用户/Cookie 重新登录后业务事实和搜索结果仍在。
- 停止 Redis 后代表性帖子读写仍通过 MySQL fallback，恢复 Redis 后无需替换应用镜像。
- 暂停 Worker 或 Indexer 创建代表性业务事实，恢复后通知/搜索收敛；只验证一个代表成功与一个代表恢复，不重跑 Phase 2/3 全部攻击矩阵。

### 3.6 容器化业务验收入口

- 建立无宿主 Go/Node/npm/curl 依赖的业务容器验收模式，可作为后续 `verify-compose.sh` 的 business 子集。
- 验收使用随机合法 project 名、随机 Frontend/Backend loopback 端口、临时 env 和全新命名卷，通过 project/service label 在停止、替换、清理前验证归属。
- API/浏览器客户端位于 one-shot acceptance 容器。验收不直接写业务表或 ES 伪造用户路径；admin 提升如需要，只用容器化运维 CLI。
- 失败/signal 路径只删除当前随机验收 project 容器/网络/卷，不删日常 project、本批以外镜像或用户工作区变更。

## 4. 实施边界与非目标

- 本批不容器化 Monitor、Router、Marshaller 和 Redis Exporter，不启动 Kafka/VM；它们由 Phase-12-02 接入同一拓扑。
- 不更改业务 API、MySQL schema、RabbitMQ topology/Envelope、搜索 mapping/alias/cursor、缓存契约或页面产品功能。
- 不把 Backend、Worker 或 Indexer 合并到同一长运行容器，不使用 supervisor 掩盖工作负载边界。
- 不为 Worker/Indexer 新增公共 HTTP 健康 API，不将业务数据库或 RabbitMQ 端口默认发布到宿主。
- 不改造为生产高可用、多环境 override 体系、开发热更新或 Kubernetes 资源。
- 不更新冻结 PowerShell，不以原生 Windows 或 macOS 应用运行作为验收条件。

## 5. 预计文件与交付物

```text
.dockerignore
.env.example
deploy/compose.yaml
deploy/docker/**
frontend/静态 Web server 配置

backend/internal/config/**（仅容器运行模式/直接配置契约）
frontend/src/**（仅容器代理暴露的阻断修复）

scripts/dev.sh
scripts/down.sh
scripts/verify.sh
scripts/verify-compose.sh（或等价的 business 容器验收入口）
scripts/ci/**
.github/workflows/quality-gates.yml

README.md
backend/README.md
frontend/README.md（若创建）
dev/imple/Phase-12/Phase-12-总实施方案.md（仅状态/真实偏差）
dev/imple/Phase-12/Phase-12-01-社交业务容器运行闭环.md（仅状态/真实偏差）
dev/logs/Phase-12/Phase-12-01-社交业务容器运行闭环.md
VERSION
frontend/package.json
frontend/package-lock.json
```

预计文件是允许边界，不要求全部发生变更。如最终 Dockerfile 布局与预计不同，只要满足总方案镜像契约，在实施记录如实说明。

## 6. 详细实施步骤

1. fetch 最新 `main`，核对 Phase 11 版本/远程门禁/实施记录，从主远程创建总方案分配的批次分支，保存资源快照。
2. 核对直接进程/entrypoint/信号/配置契约，先建立 `.dockerignore`、版本/revision label 和非 root 多阶段构建基线。
3. 构建 Backend、Worker、Indexer 和同源工具镜像，通过定向 config/unit 证明容器 service DNS 与 host 安全默认。
4. 构建 Frontend production 镜像、SPA fallback、Backend 反向代理和 liveness，运行深路由刷新、API 错误不回 HTML、Cookie 和 bundle 边界定向测试。
5. 扩展 Compose 业务拓扑、internal business 网络、命名卷、healthcheck、migration/search-init 完成依赖、restart/stop 参数，渲染并检查无内部宿主端口。
6. 实现容器化 Bash 启动/只读检查/停止和 business acceptance 子集，加入随机 project、标签归属、日常资源快照和负面 self-test。
7. 在干净卷中构建并启动业务拓扑，执行浏览器社交/搜索闭环、代表性 Redis fallback、Worker/Indexer 暂停恢复和容器替换。
8. 保留卷 down/up，重跑 migration/search-init，核对业务事实与搜索持久；执行正常/失败/signal 清理并对比快照。
9. 最终 diff 稳定后执行第 8 节固定门禁一次，更新 README、本批方案状态、根/Frontend 目标版本与实施记录。
10. 只暂存本批文件并提交；push、创建 PR，查询真实远程 checks 与合入状态。通过后立即停止并向下批交接。

## 7. 风险与控制

- **镜像成功但运行缺文件**：每个镜像执行真实 entrypoint/smoke，核对 CA/timezone/迁移嵌入资源和静态资源，不以 build exit 0 为唯一证据。
- **Frontend 反向代理改变契约**：用真实注册/登录/Cookie/POST/error/deep-link 证明，扫描 bundle 与 network；不开启宽泛 CORS。
- **migration/search-init 竞态**：只将成功退出的一次性作业作为前置，重跑两次证明幂等，不用 sleep 猜测完成。
- **Worker/Indexer 无 HTTP health**：不扩展公共面；用进程状态、日志和真实 queue 收敛联合证明。
- **Compose 改造破坏历史隔离脚本**：变更共享 Compose 调用时运行直接 self-test 与必要的业务回归；如需 host-port override，与默认内网拓扑分离。
- **误删日常卷/镜像**：任何 stop/down/volume 清理前联合校验 project label、service label、ID 和随机 token；unknown/mismatch 安全拒绝。
- **本批被扩展为全栈容器化**：只交付业务与搜索纵向闭环；可观测容器和其故障矩阵固定留给下批。

## 8. 固定验证命令与必要回归

最终 diff 上至少执行：

```bash
(cd backend && test -z "$(gofmt -l .)")
(cd backend && go test -count=1 ./...)
(cd backend && go vet ./...)
(cd backend && go test -race -count=1 ./internal/config ./internal/http/... ./internal/worker ./internal/search/...)
(cd frontend && npm test -- --run)
(cd frontend && npm run build)
python3 -m unittest discover -s scripts/ci -p 'test_*.py'
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch "$(git branch --show-current)" --base-ref upstream/main
bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-compose.sh
docker compose --env-file .env.example --file deploy/compose.yaml config --quiet
scripts/verify-compose.sh --self-test
scripts/verify-compose.sh --business
git diff --check
```

- 如最终脚本名或 `--business` 子命令因实际实现采用等价入口，可替换命令，但必须在实施记录写明真实命令与等价范围。
- 业务容器主闭环必须使用全新随机 project/卷、真实官方数据服务、真实自研镜像和浏览器；mock 只用于反向代理或配置负面单测。
- 只有修改了共享业务数据/消息语义，或容器闭环暴露真实回归时，才扩展运行 `verify-business.sh` 更广模式，并记录风险依据。

## 9. 批次验收标准

- Frontend、Backend、Business Worker 和 Search Indexer 为独立、非 root、带正确 version/revision label 的最终镜像，不含不必要工具链、源码或凭据。
- 宿主仅依赖 Docker/Compose 可完成干净构建、MySQL migration、search initialize 与社交业务拓扑启动，无 Go/Node/npm 业务进程。
- Frontend 生产静态服务、SPA deep-link、Backend 同源代理、Cookie 和 API 错误语义正确，bundle/network 无内部地址或凭据。
- 普通用户真实浏览器完成注册、登录、帖子、评论、点赞、通知与搜索代表闭环；业务事实来自真实 MySQL/RabbitMQ/Elasticsearch。
- 服务仅使用 Compose DNS 和容器端口，默认只 Frontend/Backend 以 loopback 发布；MySQL/Redis/RabbitMQ/ES 无默认宿主端口。
- migration/search-init 幂等且失败阻断相关长运行应用；Backend/Worker/Indexer 替换、Redis fallback、Worker/Indexer 暂停恢复和保留卷 down/up 的代表矩阵通过。
- 日常项目、本批随机项目与其他用户容器/卷/镜像隔离；正常、失败和 signal 清理只影响已证明归属的资源。
- 固定本地门禁、版本/分支治理、实施记录和远程 checks 通过，根/Frontend 版本等于总方案为本批分配的目标版本。

## 10. 明确完成条件

只有第 9 节全部满足、本批 Pull Request 已合入主远程 `main`、远程门禁成功，且同名实施记录与真实提交一致，本批才完成。

任一自研业务进程仍需宿主工具链、初始化不幂等、默认数据端口暴露、反向代理/Cookie 退化、业务闭环不真实、重启丢失必要事实或资源清理越界时不得标记完成。

完成后立即停止，不在本批追加可观测容器、Kubernetes、热更新、高可用或独立 Review。

## 11. Phase-12-02 交接

- 已验证的 Frontend、Backend、Worker、Indexer 镜像、version/revision label、非 root 用户和信号契约。
- `edge/business` 网络、Frontend/Backend loopback 发布、数据服务内网、服务 DNS 和调试 override 边界。
- migration、search initialize 和 admin-role one-shot 运行方式，以及 MySQL/Redis/RabbitMQ/ES 命名卷。
- 容器化 Bash 启动/只读验证/停止骨架、business acceptance 子集、强归属清理与 CI 镜像门禁。
- 无 Monitor 时的可观测局部不可用是本批预期状态；Phase-12-02 必须在不改变本批业务闭环的前提下加入全部可观测服务。
