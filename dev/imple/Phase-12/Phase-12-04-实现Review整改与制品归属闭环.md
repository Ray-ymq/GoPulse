# Phase-12-04：实现 Review 整改与制品归属闭环

## 1. 批次定位

本批关闭 `dev/review/2026-09-06-Phase-12实现Review报告.md` 中的 P2-01 至 P2-06 与 P3-01。权威开发分支为 `develop/1.9.4`，目标版本为 `1.9.4`，代码基线为 2026-09-06 fetch 后的 `origin/main` 提交 `dc211ce`，并保留该分支上的 Review 报告提交。

本批只修复 Compose 制品归属、日常镜像一致性、运行模式配置、workload 最小身份、启动前端口边界和官方服务凭据 argv 暴露。不得扩展到 Kubernetes、TLS/远程开发入口、生产供应链、多架构、高可用或无关容器加固。

## 2. 实施范围

### 2.1 权威验收与镜像归属

- 权威全栈 runner 在任何 Docker 查询或构建前拒绝会进入任一产品 build context 的 tracked/untracked dirty 文件。
- 为每次验收生成运行级唯一 image tag，并通过 Compose 变量让全部自研 image/build/run/inspect 都使用该 tag；不得覆盖 `gopulse/*:<VERSION>` 日常 tag。
- 正常、失败和 signal 清理后，预先存在的 container、network、volume、image 与 image tag→ID 映射保持不变；只删除当前随机 project 和本次唯一-tag image。
- 增加无需 Docker daemon 的安全测试，证明 dirty 产品源码会在 Docker access 前失败，允许的非构建文档不会伪装产品源码。

### 2.2 日常生命周期镜像一致性

- `scripts/dev.sh --no-build` 在 `compose up` 前检查全部自研 tag 的 OCI version、revision、source；任一 tag 缺失、label 不一致或 revision 不是当前 `HEAD` 时拒绝。
- `scripts/verify.sh` 检查运行容器 image ID 等于当前 tag image ID，并检查 version、revision、source、数字 UID:GID；全部自研镜像必须来自当前 `HEAD`。
- 增加 fake-Docker 测试，覆盖相同 version、不同 revision 的启动前拒绝。

### 2.3 Backend VictoriaMetrics 运行模式

- `BACKEND_VICTORIAMETRICS_URL` 接入现有 `host|container` origin host 校验。
- 显式端口必须位于 `1..65535`。
- 以一个合法 service DNS 成功例和 loopback、`host.docker.internal`、固定 IP、非法端口代表性失败例证明合同。

### 2.4 Compose 最小环境身份

- 将 Backend、Business Worker、Search Indexer、migration、search-init、admin-role 环境拆为职责最小块。
- migration/admin-role 使用只读取 MySQL 的配置 loader；Worker 只取得 MySQL、RabbitMQ、必要 log ship 身份和 worker 参数；Indexer 在此基础上增加 Elasticsearch 与 indexer 参数。
- Worker、Indexer 和一次性作业不得取得 JWT、Monitor 管理 token 或 Backend VictoriaMetrics 查询身份；migration/admin-role 不取得 RabbitMQ、Redis、日志或搜索身份。
- 静态渲染测试断言各 service 的允许/禁止变量集合。

### 2.5 启动前发布边界

- `scripts/dev.sh` 在 Docker access、build 和 up 前解析最终 env，要求 `PUBLISHED_HOST` 精确为 `127.0.0.1`。
- `HTTP_PORT` 与 `FRONTEND_PORT` 必须是 `1..65535` 的单个十进制端口；权威验收直接使用 Compose 的随机发布端口 `0`，不经日常入口。
- 默认 `.env.example` 只描述 loopback 本地开发，不把 `PUBLISHED_HOST` 宣称为远程访问开关。
- fake-Docker 测试证明 wildcard、`localhost`、IPv6 wildcard 和非法端口均在 Docker access 前被拒绝。

### 2.6 官方服务凭据 argv

- VictoriaMetrics 主密码使用只读 file-backed secret/config，Compose command 不包含密码值。
- Redis healthcheck 使用 `REDISCLI_AUTH` 环境，不使用 `-a`；MySQL healthcheck 使用容器内 credential file 或等价不进入 probe argv 的方式；VictoriaMetrics liveness 不构造 Basic header argv。
- 静态渲染测试扫描最终 `command` 与 `healthcheck.test`，不得包含实际密码、Basic authorization 或 password 参数值。

## 3. 直接影响文件

- `deploy/compose.yaml`、必要的 `deploy/` credential helper/config 文件与 `.env.example`
- `scripts/dev.sh`、`scripts/verify.sh`、`scripts/verify-compose.sh`、`scripts/verify-compose-observability.sh`
- `scripts/ci/test_*.py`（仅 Review finding 的行为合同）
- `backend/internal/config/**`、`backend/cmd/migrate/**`、`backend/cmd/admin-role/**`
- 根/Frontend 版本文件、本方案、总实施方案及对应实施记录

## 4. 验收标准

1. P2-01：权威 runner 对 dirty 产品源码 fail closed；所有自研服务使用本次随机唯一 tag；预置日常同版本 tag 在成功/失败/signal 路径后仍指向原 image ID。
2. P2-02：`--no-build` 与只读 verify 对 version/revision/source/image-ID 全部 fail closed，同版本旧 revision 不能启动或通过。
3. P2-03：Backend VM URL 在 host/container mode 均遵守 origin host 合同，非法显式端口在配置加载阶段失败。
4. P2-04：最终 Compose 渲染中，六类 workload 只持有职责所需环境；最小 MySQL loader 被 migration/admin-role 实际使用。
5. P2-05：非字面 IPv4 loopback和非法 Frontend/Backend 端口在任何 Docker access 前失败；默认示例不承诺远程访问。
6. P2-06：总方案权威表唯一映射 `Phase-12-04 → 1.9.4 → develop/1.9.4`，完成时根与 Frontend 版本一致且分支门禁通过。
7. P3-01：最终 Compose command/healthcheck 不把 MySQL、Redis、VictoriaMetrics 密码或 Basic header 放入 argv，同时三项服务仍能健康启动。
8. 必要业务、搜索、Metrics/Logs/Events、Exporter 管理、持久恢复和强归属清理由唯一-tag全栈门禁继续覆盖，无直接回归。

明确完成条件：上述 8 项及固定完成门禁全部通过，无阻断失败；对应实施记录写明真实文件、命令、结果、偏差与限制；版本更新到 `1.9.4` 后提交并停止。

## 5. 固定完成门禁

```bash
(cd backend && test -z "$(gofmt -l internal/config cmd/migrate cmd/admin-role)" && go test -count=1 ./internal/config ./cmd/migrate ./cmd/admin-role && go vet ./internal/config ./cmd/migrate ./cmd/admin-role)
(cd backend && go test -race -count=1 ./internal/config)
python3 -m unittest discover -s scripts/ci -p 'test_*.py'
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/1.9.4 --base-ref origin/main
bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-compose.sh scripts/verify-compose-observability.sh
GOPULSE_VERSION=1.9.4 GOPULSE_REVISION="$(git rev-parse HEAD)" docker compose --env-file .env.example --file deploy/compose.yaml config --quiet
scripts/verify-compose.sh --self-test
scripts/verify-compose.sh
git diff --check
```

如果直接受影响测试揭示共享配置或全栈回归，才扩大到对应模块；记录扩展理由。已经成功的门禁在相关代码、配置、依赖或环境未变化时不重复执行。
