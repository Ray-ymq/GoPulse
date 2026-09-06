# Phase-12-02：可观测系统容器运行闭环实施记录

## 1. 批次信息

- 日期：2026-09-05（主体实施）；2026-09-06（远程门禁修复）
- 分支：`develop/1.9.2`
- 开工基线：最新 `upstream/main` 提交 `3add2b8`，产品版本 `1.9.1`
- 目标/本地完成版本：`1.9.2`
- 实施方案：`dev/imple/Phase-12/Phase-12-02-可观测系统容器运行闭环.md`
- 当前结论：已完成。主体提交 `37fcb38` 的首次自动 PR 工作流 run #125 因跨 package 集成夹具清理竞态在 Integration job 失败；修复后最终分支提交 `e289053` 的远程运行 `34010783067` 全部 14 项检查成功，Pull Request #106 于 2026-09-06 合入主远程 `main` 为提交 `1e63bdd`，满足实施方案第 10 节完成条件。

## 2. 实际完成

### 2.1 显式运行模式与服务 DNS

- Monitor、Message Router、Marshaller 和 Redis Exporter 统一支持 `GOPULSE_RUNTIME_MODE=host|container`：未设置时保持 host，未知值立即失败，不通过 `APP_ENV` 隐式放宽网络边界。
- host 模式继续要求 loopback 监听与 loopback 下游；container 模式只接受受控通配监听、合法服务 DNS 和明确端口，拒绝 loopback/fixed-IP/`host.docker.internal` 下游、控制字符、HTTP userinfo、额外 path、query/fragment 及不受支持的 scheme。
- Compose 中 Router 使用 `kafka:19092`，Monitor 使用 `router:9091` 和 `redis:6379`，Marshaller 使用 `kafka:19092`、`victoriametrics:8428` 与 `elasticsearch:9200`。Monitor 受管 Exporter 仍绑定同容器内 `127.0.0.1:9121`。
- Marshaller 的 Metrics、Logs 和 Events Elasticsearch client 增加显式 container 构造入口，保留 host 构造器的 loopback 保护，并用直接测试证明 service DNS 接受与 loopback 容器下游拒绝。

### 2.2 镜像、确定性 package 与 Monitor bootstrap

- 新增 `deploy/docker/observability.Dockerfile`，使用固定 `golang:1.26.0-alpine3.23` 与 `alpine:3.23.3` 多阶段构建，交付：
  - `gopulse/router:1.9.2`，数字用户 `10002:10001`；
  - `gopulse/marshaller:1.9.2`，数字用户 `10003:10001`；
  - `gopulse/redis-exporter:1.9.2`，数字用户 `10004:10001`；
  - `gopulse/monitor:1.9.2`，数字用户 `10005:10001`。
- 四个镜像使用明确 entrypoint、内部 exposed port 和统一 version/revision/source OCI label；最终层不含 Go 工具链或源码。Router、Marshaller、Exporter 使用只读根文件系统；Monitor 仅通过专用插件卷写入运行事实，未挂载 Docker socket、未启用 privileged 或额外 capability。
- `scripts/package-redis-exporter.sh` 增加 `--binary` 与 `--arch`，可直接将已构建二进制打入确定性 package。Monitor image build 使用与独立 Exporter 镜像相同的二进制产物；容器验收交叉比较 package entrypoint 与镜像二进制 SHA-256。
- Plugin Manager 新增镜像内 package bootstrap：空卷首次启动通过既有 install/start 流程安装并启动；同版本重启保留 desired state；较新内置版本通过既有 update/rollback 路径升级；旧镜像面对较新插件卷时明确拒绝降级。启动、提取、digest、存储、进程所有权、watcher 和失败安全摘要均复用既有实现。

### 2.3 完整 Compose、生命周期与持久化

- 默认 `deploy/compose.yaml` 纳入 Kafka、`kafka-init`、VictoriaMetrics、Router、Marshaller 和 Monitor；独立 Redis Exporter 仅位于显式 `exporter` profile，避免默认栈绕过 Monitor 再启动第二个运行时所有者。
- 网络收敛为：Frontend 仅 `edge`；Backend 跨 `edge/business/observability`；Worker、Indexer、Elasticsearch 和 Monitor 跨必要的 `business/observability`；Kafka、VictoriaMetrics、Router、Marshaller 仅 `observability`。`business` 与 `observability` 均为 internal。
- 默认仅 Frontend/Backend 发布 IPv4 loopback 宿主端口；MySQL、Redis、RabbitMQ、Elasticsearch、Kafka、VictoriaMetrics、Router、Marshaller、Monitor 和 Exporter 均无宿主端口。Frontend Nginx 保留普通 API 的 `1m` 请求体上限，仅对 Exporter install/update 两个精确路由放宽到 `65m`，覆盖 Backend 的 64 MiB package 加 multipart 开销而不扩大其他 API。
- Backend、Worker 和 Indexer 的 Schema v1 日志通过 `http://monitor:9090` 发送；Backend 通过内部 Bearer/Basic identity 调用 Monitor 与 VictoriaMetrics。Kafka Topic 初始化、VictoriaMetrics Basic Auth、Marshaller storage identity、Monitor 插件卷和 Kafka/VM 持久卷全部由 Compose 声明。
- `scripts/dev.sh` 构建并启动完整默认栈；`scripts/verify.sh` 只读核对全部服务/作业、镜像版本/数字用户、内部端口和受管 Exporter desired state；`scripts/verify-compose.sh --observability` 委派给新的强归属容器验收入口。

### 2.4 浏览器闭环、故障矩阵与 CI

- `frontend/e2e/compose-observability.spec.ts` 在生产 Frontend 中完成：
  - 普通用户路由与 Backend API 双层 `403 permission_denied` 隔离；
  - 管理员社交读写、Metrics、Logs、Events 与 Exporter status/stop/start；
  - VictoriaMetrics、Monitor、Router 故障时的局部降级；
  - 保留卷与服务替换后的历史数据和插件状态；
  - 空插件卷上的 install/stop/start/update。
- `scripts/verify-compose-observability.sh` 生成随机 `gopulse-observe-<token>` project 和临时凭据，验证强归属、开工资源快照、镜像/权限/网络/端口/package 合同、完整冷启动、真实浏览器闭环、VM/Monitor/Router 故障、Monitor/Marshaller/VM/Kafka/Elasticsearch 替换、保留卷 down/up、独立 Exporter success/`up 0`/认证失败/恢复/SIGTERM，以及最终空卷管理流。独立 Exporter 首次断言会等待真实 scrape-ready；资源快照只排除本次明确重建的九个精确版本 GoPulse tag，仍验证全部其他既有镜像。正常、失败和 signal 清理均只作用于该随机 project，不执行 Docker prune。
- `deploy/docker/acceptance.Dockerfile` 在 acceptance image 内生成当前安装包和下一 patch 更新包，因此容器主验收不依赖宿主 Go、Node、npm 或 Python。
- 质量门禁用 container-only observability job 替代旧 host-process browser job；Scripts/Compose job纳入新脚本 Bash/LF 检查、动态版本镜像断言、Kafka/VM 和双 internal network 静态合同。Python 治理测试增加新脚本 executable/LF/Bash、强归属、无 prune、真实浏览器和内部端口断言。
- 真实容器故障矩阵暴露 Monitor status 中固定 metrics `last_error` 未被 Backend/Frontend Exporter DTO allowlist 接受的问题；Backend 与 Frontend 现同步接受 Monitor 已定义的精确 scrape/publish code/message 组合，同时继续拒绝任意错误码、路径或自由文本。

## 3. 变更文件

- 镜像与 Compose：`.dockerignore`、`.env.example`、`deploy/compose.yaml`、`deploy/docker/observability.Dockerfile`、`deploy/docker/acceptance.Dockerfile`、`deploy/docker/frontend/nginx.conf`。
- Backend/Frontend 边界：`backend/internal/exporterplugin/client.go` 及测试，`frontend/src/types/exporter.ts`、`frontend/src/services/exporters.ts` 及测试，`frontend/e2e/compose-observability.spec.ts`，Frontend package metadata。
- Monitor：`monitor/internal/config/**`、`monitor/internal/plugin/**`、`monitor/cmd/monitor/main.go`。
- Router：`router/internal/config/**`。
- Marshaller：`marshaller/internal/config/**`、`marshaller/internal/elasticsearch/**`、`marshaller/cmd/marshaller/main.go`。
- Redis Exporter：`exporters/redis/internal/config/**`。
- 生命周期与 CI：`scripts/dev.sh`、`scripts/verify.sh`、`scripts/verify-compose.sh`、`scripts/verify-compose-observability.sh`、`scripts/package-redis-exporter.sh`、`scripts/ci/test_verify_business.py`、`scripts/ci/test_auto_pr_workflow.py`、`.github/workflows/quality-gates.yml`。
- 文档与治理：根及各组件 README、Phase 12 总方案、本批方案、本记录、`VERSION`、Frontend package metadata。
- 远程门禁跟进：`backend/internal/http/comment_like_integration_test.go`、`backend/internal/http/post_integration_test.go`、`backend/internal/like/integration_test.go`、`backend/internal/integrationtest/mysql_lock.go`。

## 4. 验证结果

最终 diff 的固定本地门禁全部通过：

```text
(cd backend && test -z "$(gofmt -l .)")                                      # PASS
(cd backend && go test -count=1 ./...)                                        # PASS
(cd backend && go vet ./...)                                                   # PASS
(cd backend && go test -race -count=1 ./internal/config ./internal/observability/logship ./internal/exporterplugin ./internal/metricquery)  # PASS

(cd monitor && test -z "$(gofmt -l .)" && go test -count=1 ./... && go vet ./...)  # PASS
(cd monitor && go test -race -count=1 ./internal/config ./internal/plugin ./internal/metrics/... ./internal/events/...)  # PASS

(cd router && test -z "$(gofmt -l .)" && go test -count=1 ./... && go vet ./...)  # PASS

(cd marshaller && test -z "$(gofmt -l .)" && go test -count=1 ./... && go vet ./...)  # PASS
(cd marshaller && go test -race -count=1 ./internal/config ./internal/consumer ./internal/victoriametrics ./internal/elasticsearch)  # PASS

(cd exporters/redis && test -z "$(gofmt -l .)" && go test -count=1 ./... && go vet ./...)  # PASS
(cd exporters/redis && go test -race -count=1 ./...)                            # PASS

(cd frontend && npm test -- --run)                                             # PASS：11 files / 58 tests
(cd frontend && npm run build)                                                 # PASS

python3 -m unittest discover -s scripts/ci -p 'test_*.py'                      # PASS：27 tests
python3 scripts/ci/validate_versions.py                                        # PASS
python3 scripts/ci/validate_branch.py --branch "$(git branch --show-current)" --base-ref upstream/main  # PASS
bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-compose.sh scripts/verify-compose-observability.sh scripts/package-redis-exporter.sh  # PASS
docker compose --env-file .env.example --file deploy/compose.yaml config --quiet  # PASS
scripts/verify-compose.sh --self-test                                           # PASS：6 个不安全 project name 均在 Docker 访问前拒绝
scripts/verify-compose.sh --observability                                       # PASS：完整 Phase-12-02 容器矩阵及强归属清理通过
git diff --check                                                                # PASS
```

### 4.1 自动 PR run #125 的 Integration 修复

2026-09-06，提交 `37fcb38` 触发的 `Auto PR and Merge` run #125 中，Branch governance、Backend、Router、Marshaller、Monitor、Redis Exporter、Frontend、Scripts/Compose 和 container-only observability acceptance 均通过；仅 Integration job `101422896209` 失败，后续 `Open PR and enable auto-merge` 被跳过，因而没有创建 Pull Request。失败输出为：

```text
--- FAIL: TestIntegrationPostRepositoryReadModelStablePaginationAndQueryPlan
integration_test.go:63: post[0].ID=5, want 9
```

原因是会提交全局可见 post 的 HTTP 与 Like 集成测试虽然共享 MySQL named lock，但 `t.Cleanup` 注册的夹具删除晚于 defer 的 named-lock 释放执行。另一 package 的测试二进制可能在锁释放后、旧 post 删除前取得锁并读取该已提交事实，导致 feed 分页断言偶发看到陈旧 post。修复将相关夹具删除改为显式 defer，并保证 defer 的 LIFO 顺序为“删除已提交夹具 → 释放 named lock → 关闭数据库”；named-lock helper 注释同步明确该调用约束。

在随机命名、仅回收自身资源的 MySQL 8.4、Redis 7.2、RabbitMQ 3.13 和 Elasticsearch 9.5.2 容器中使用动态 loopback 宿主端口完成验证：

```text
(cd backend && go run ./cmd/migrate up)                                         # PASS
(cd backend && for attempt in 1 2 3; do go test -count=1 -tags=integration ./internal/http ./internal/like ./internal/post; done)  # PASS：3/3
(cd backend && go test -count=1 -tags=integration ./...)                        # PASS
(cd backend && test -z "$(gofmt -l .)")                                        # PASS
(cd backend && go test -count=1 ./...)                                          # PASS
(cd backend && go vet ./...)                                                     # PASS
git diff --check                                                                 # PASS
```

验证容器、网络均由强归属 trap 清理，未执行 prune。由于该跟进只修改集成测试夹具生命周期及说明，且 run #125 的 container-only observability acceptance 已通过，未重复执行完整可观测容器矩阵。

最终 `--observability` 从头构建九个 GoPulse `1.9.2` 镜像，完成完整栈冷启动、管理员/普通用户浏览器矩阵、VM/Monitor/Router 故障隔离、容器替换、保留卷 down/up、独立 Exporter 与空卷 install/stop/start/update，并输出 `Phase-12-02 complete observability container acceptance passed`。随机 project `gopulse-observe-204ea671d796` 的容器、网络、卷和临时目录随后均确认清理。

真实容器主验收的非最终失败与修复：

1. 首次完整冷启动中 VictoriaMetrics 已运行但 healthcheck 持续 `400`。BusyBox `base64` 对较长 Basic identity 自动换行，导致 Authorization header 非法；healthcheck 改为删除编码换行后返回 `200`。
2. 首次完成服务替换后的 persistence browser 场景中，Monitor 正确返回 `publish_failed` 安全状态，但 Backend 与 Frontend 的旧 allowlist 将其视为不受信任响应，页面无状态卡片。同步纳入 Monitor 固定 metrics safe-error 合同后，直接 Backend/Frontend 测试和保留项目的 persistence 浏览器复现通过。
3. 独立 Exporter 的 target-down 检查最初使用 BusyBox `wget` 读取预期 `503` 响应；该客户端在非 2xx 时丢弃响应体，脚本无法观察真实的 `gopulse_redis_up 0`。改用容器内 BusyBox `nc` 发出有界 HTTP/1.0 请求并只提取 body 后，target-down、恢复、错误认证和 SIGTERM 矩阵通过。
4. 一次完整重跑在独立 Exporter 刚健康时立即读取到首次 Redis scrape 的短暂失败，只出现 `up 0` 而触发 metric family 数量误报。验收改为有界等待真实 `gopulse_redis_up 1` 后再断言 10 个 family 与 11 个样本，最终冷启动矩阵通过。
5. 空插件卷的管理员安装首次返回 Nginx `413`：约 5.5 MiB 的确定性 package 被 Frontend 原有全局 `1m` 限制提前拒绝。仅对 install/update 精确路由设置 `65m` 后，保留项目中的浏览器 manage 场景与最终完整矩阵均通过。
6. 连续重跑会由 Compose build 替换同版本 GoPulse tag 的旧 image ID，原快照将该预期替换误判为用户镜像丢失。快照现只排除本次明确重建的九个精确版本 tag，其他既有 image/container/network/volume 仍全部受保护，最终清理检查通过。

修复后的早期一次重跑因验收脚本正在执行时发生文件更新而由 Bash 中止；该次无效运行的随机资源已由 trap 清理。随后在静态最终 diff 上重新从头执行完整命令并成功，因此未将该中止计为产品验收结果。

## 5. 计划偏差、限制与后续

- 未新增第二个默认 Exporter 服务；独立镜像只在专用 profile 验证，默认运行时仍完全由 Monitor Plugin Manager 所有。
- 为证明浏览器 install/update，在 acceptance image 内生成当前版本安装包和下一 patch 包；`GOPULSE_UPDATE_VERSION` 只属于验收数据，不改变产品版本。
- 为关闭真实故障矩阵暴露的 DTO 合同不一致，额外修改 Backend/Frontend 固定 safe-error allowlist；未放宽自由文本、内部路径、原始错误或任意 code。
- 未修改冻结的 PowerShell 脚本，未增加宿主端口、Docker socket、特权容器、Kubernetes、生产身份系统、SBOM/签名、多架构发布、容量测试或独立 Review。
- 首次推送的自动 PR run #125 因 Integration 夹具清理竞态失败而未创建 Pull Request；该缺陷修复后，远程运行 `34010783067` 的 Branch governance、各组件、双容器验收、Scripts and Compose、Integration 与自动合入共 14 项检查全部成功。Pull Request #106 已合入主远程 `main`，本批无已知阻断项并标记为已完成。
