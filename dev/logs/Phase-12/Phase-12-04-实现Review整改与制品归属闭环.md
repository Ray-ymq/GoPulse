# Phase-12-04：实现 Review 整改与制品归属闭环记录

## 1. 批次信息

- 日期：2026-09-06
- 分支：`develop/1.9.4`
- 目标/完成版本：`1.9.4`
- 代码基线：`origin/main` 提交 `dc211ce`
- 产品整改提交：`a00cd1060c0b96703d6e00c70248bee6c5ef5680`
- 依据：`dev/review/2026-09-06-Phase-12实现Review报告.md`

## 2. 实际完成

### 2.1 P2-01 / P2-02：可重建制品、唯一 tag 与日常镜像一致性

- 权威全栈 runner 在首次 Docker invocation 前检查 Git 工作树；除被根 `.dockerignore` 明确排除的 `dev/**/*.md` 外，tracked 或普通 untracked 变更均拒绝进入验收。
- 每次全栈验收生成 `${VERSION}-accept-${TOKEN}` 唯一 image tag，并通过 `GOPULSE_IMAGE_TAG` 驱动 Compose。验收不再重写 `gopulse/*:<VERSION>` 日常 tag。
- runner 保存全部既有 image ID 与 repository:tag→image ID 映射，正常/失败/signal cleanup 只删除本随机 project 与唯一 tag，随后验证既有映射未改变。
- `dev.sh --no-build` 在 `compose up` 前检查九类自研 tag 的 OCI version/revision/source；同版本旧 revision、缺失镜像或错误 source 均拒绝启动。
- `verify.sh` 同时检查运行容器 image ID、当前 tag image ID、数字 UID:GID、version、revision 与 source；migration/search-init 也必须运行当前 Backend tag。

### 2.2 P2-03：Backend VictoriaMetrics runtime mode

- `BACKEND_VICTORIAMETRICS_URL` 接入既有 `validateOriginHost`，host mode 继续限定 loopback，container mode 只接受 service DNS。
- 显式端口增加 `1..65535` 校验。
- 代表性测试覆盖合法 `victoriametrics:8428`，并拒绝 loopback、`host.docker.internal`、固定 IP 和 `99999` 端口。

### 2.3 P2-04：workload 最小环境身份

- Compose 拆分 MySQL-only、Backend、Worker、Indexer 与 search-init 环境块。
- migration/admin-role 改用新增的 `config.LoadMySQL`，只读取 runtime mode 与 MySQL 配置。
- Worker 只取得 MySQL、RabbitMQ、log ship 与自身参数；Indexer 在此基础上取得 Elasticsearch 与自身参数；两者不再取得 JWT、Monitor 管理 token、Redis 或 Backend VictoriaMetrics 查询身份。
- migration/admin-role 只含六个 MySQL/runtime 变量；search-init 只含 MySQL、Elasticsearch 与 reindex 参数。
- Compose JSON 渲染测试锁定各 workload 的允许/禁止变量集合。

### 2.4 P2-05 / P2-06：启动前发布边界与治理

- `dev.sh` 在首次 Docker invocation 前解析最终 env；`PUBLISHED_HOST` 必须精确为 `127.0.0.1`，`HTTP_PORT`/`FRONTEND_PORT` 必须是 `1..65535` 的单个十进制端口。
- fake-Docker 测试证明 `0.0.0.0`、`localhost`、`::` 和非法端口在 Docker access 前失败。
- `.env.example` 删除远程访问暗示，并增加与根版本一致的 `GOPULSE_IMAGE_TAG`。
- Phase 12 总方案分配 `Phase-12-04 → 1.9.4 → develop/1.9.4`；根、Frontend package/lockfile 与示例环境版本同步到 `1.9.4`，分支治理通过。

### 2.5 P3-01：官方服务凭据 argv

- MySQL healthcheck 改为无需密码参数的本机 liveness ping。
- Redis healthcheck 通过 `REDISCLI_AUTH` 环境认证，不再使用 `-a` 参数。
- VictoriaMetrics 密码由 Compose environment-backed secret 只读挂载，主进程使用 `file:///run/secrets/victoriametrics_password`；公共 `/health` liveness 不再构造 Basic header。
- Compose JSON 测试扫描 MySQL/Redis/VictoriaMetrics 的最终 command 与 healthcheck，确认实际密码、Basic header、`--password` 和 `-a` 未进入 argv。

## 3. 变更文件

- Backend 配置与命令：`backend/internal/config/config.go`、`runtime_mode_test.go`、`search_test.go`、`backend/cmd/migrate/main.go`、`backend/cmd/admin-role/main.go`。
- Compose 与生命周期：`deploy/compose.yaml`、`.env.example`、`scripts/dev.sh`、`scripts/verify.sh`、`scripts/verify-compose.sh`、`scripts/verify-compose-observability.sh`。
- 治理与行为测试：`scripts/ci/validate_versions.py`、`test_validate_versions.py`、`test_verify_business.py`。
- 版本与记录：`VERSION`、`frontend/package.json`、`frontend/package-lock.json`、Phase 12 总/拆分方案与本记录。

## 4. 验证结果

最终产品整改提交 `a00cd10` 上通过：

```text
(cd backend && test -z "$(gofmt -l internal/config cmd/migrate cmd/admin-role)" && go test -count=1 ./internal/config ./cmd/migrate ./cmd/admin-role && go vet ./internal/config ./cmd/migrate ./cmd/admin-role)
# PASS

(cd backend && go test -race -count=1 ./internal/config)
# PASS

python3 -m unittest discover -s scripts/ci -p 'test_*.py' -v
# PASS：32 tests

python3 scripts/ci/validate_versions.py
# PASS：Version metadata matches root VERSION.

python3 scripts/ci/validate_branch.py --branch develop/1.9.4 --base-ref origin/main
# PASS：Branch governance passed for develop/1.9.4.

bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-compose.sh scripts/verify-compose-observability.sh
# PASS

GOPULSE_VERSION=1.9.4 GOPULSE_IMAGE_TAG=1.9.4 GOPULSE_REVISION="$(git rev-parse HEAD)" \
  docker compose --profile operations --env-file .env.example --file deploy/compose.yaml config --quiet
# PASS

scripts/verify-compose.sh --self-test
# PASS：6 unsafe project names rejected before Docker access.

git diff --check
# PASS
```

P3-01 定向真实容器检查使用随机 project 启动 MySQL、Redis 与 VictoriaMetrics，三项均达到 `running|healthy`；VictoriaMetrics container command/healthcheck 未包含示例密码，检查后 project containers/networks/volumes 全部删除。

权威完整门禁在 clean 产品提交 `a00cd10` 上实际执行：

```text
scripts/verify-compose.sh
# PASS
# project: gopulse-accept-a90a47d66a3a
# unique image tag: 1.9.4-accept-a90a47d66a3a
# final: Phase 12 authoritative full-stack Compose acceptance passed.
```

该运行完成全部业务、搜索、Metrics/Logs/Events、Exporter 管理、故障隔离、容器替换、持久恢复和 signal 矩阵。退出码为 0；随机 project 的 containers/networks/volumes 与九个唯一 tag 均已清理，runner 的既有 image ID/tag 映射保护断言通过。随后只修改本实施记录与总方案状态；两者位于 `.dockerignore` 排除的 `dev/`，不影响已验证 build/runtime source，依照 Execution Efficiency Rule 不重复完整矩阵。

## 5. 实施偏差、限制与后续

- 无产品范围扩展；未增加远程开发入口、TLS、Kubernetes、供应链、多架构或高可用工作。
- `scripts/verify-compose.sh --business` 作为历史聚焦诊断入口继续使用版本 tag；Phase 12 唯一权威无参数入口已使用随机唯一 tag，不再覆盖日常 tag。
- Redis 主服务仍通过既有环境变量启动认证；本批按 finding 移除了 Redis healthcheck argv 密码，未扩展为 Redis credential storage 重构。
- Review 报告作为历史 Fail 证据保持不变；本记录、`1.9.4` 产品提交和权威完整门禁构成整改关闭证据。
- 本地无已知阻断项；分支尚未推送，远程 Pull Request/quality-gates 结果由后续推送流程补充。
