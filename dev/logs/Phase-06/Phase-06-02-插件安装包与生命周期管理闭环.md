# Phase 6-02：插件安装包与生命周期管理闭环实施记录

- 实施日期：2026-09-03
- 开发分支：`develop/1.3.2`
- 目标版本：`1.3.2`
- 基线：执行前已 fetch `origin`，从当时最新 `origin/main`（`d78abaa`，产品版本 `1.3.1`）创建本批分支；开始前记录了 Git、日常 Compose project、`.run` 与相关端口快照，未清理或提交既有资源和未跟踪的用户文件 `使用指南.md`。

## 1. 实际完成内容

### 1.1 Monitor 基础运行时与内部认证

- 建立独立 `monitor` Go module、`cmd/monitor`、配置加载、JSON 结构化日志、HTTP server、信号关闭、`/health` 与 `/ready`。
- Monitor 默认监听 `127.0.0.1:9090`；除 `/health` 外的接口使用至少 32 bytes Bearer token，并使用常量时间比较。
- 配置要求受信绝对插件根，Exporter 只接收 Redis/Exporter 固定环境变量白名单；子进程直接执行已验证入口，不经过 shell。
- Monitor readiness 只表示配置、Registry 和 Plugin Manager 已初始化；单插件恢复失败通过安全状态表达，不使管理 API 整体失去 readiness。

### 1.2 安装包、安全解包与原子持久化

- 新增 `scripts/package-redis-exporter.sh`，使用固定 Manifest v1、稳定 JSON、`-trimpath`/空 build ID、固定 tar 排序/时间/owner 和无时间戳 gzip 生成确定性 `tar.gz`。
- Manifest 严格拒绝缺失/未知/重复字段、错误 schema/ID/kind/source/platform/路径/摘要和非三段 SemVer。
- Monitor 同时实施 64 MiB 上传上限，以及 32 entry、128 MiB 总解压量、96 MiB 单文件、64 KiB Manifest、240 bytes 路径限制；拒绝绝对/非规范/穿越/重复路径、链接和特殊文件。
- 解包只发生在插件根 `.staging`，入口 SHA-256 与平台校验通过后才进入 `releases/<version>`；`current` 使用相对临时链接加 rename 切换。
- `registry.json` 使用临时文件、fsync、rename 和目录 fsync 原子保存公开 Manifest 元数据、当前版本、时间和 desired state；PID 事实单独保存在 `runtime/process.json`。

### 1.3 生命周期、进程归属、回滚与恢复

- install 对未安装的 `redis-exporter` 自动启动真实 Exporter，只有固定 `/health` 返回成功后才提交 running 状态；首次启动失败会删除 Registry、current、release 和进程记录。
- start/stop 支持幂等语义并先持久化 desired state；进程使用独立进程组与 parent-death `SIGTERM`。
- 停止或强制退出前重新验证 PID、start ticks、绝对 executable、cwd 和 command marker；伪造 PID 记录返回安全失败且不会向无关进程发信号。
- update 只接受同 ID 的更高 SemVer。running 更新会停旧、原子切换、启新并验证 health；新版本失败时恢复旧 current/Registry 并重启旧版本。stopped 更新保持 stopped。
- Monitor 启动会核对 Registry/current/release/PID 事实，停止可验证遗留进程并按 desired state 恢复唯一实例；运行后意外退出会转为 `failed/process_exited` 安全状态。
- 状态 DTO 包含计划规定字段；本批 `last_scrape_at` 和 `last_success_at` 保持 `null`，不伪造 Phase-06-03 的采集事实。

### 1.4 Backend 管理代理与管理员边界

- 新增六个 `/api/v1/exporter-plugins` 公共路由，统一注册在既有认证中间件之后，并复用 Phase-06-01 `RequireAdmin`。
- Backend 以有界 multipart stream 转发唯一 `package` part，不使用上传文件名作为路径，也不把包完整读入内存。
- Backend Monitor client 使用内部 Bearer token、请求超时和安全错误映射；连接失败、超时、未知或畸形内部响应统一为 `503 monitor_unavailable`，不透传内部 URL/token/body/路径/PID/底层错误。
- 公共错误新增 `plugin_package_invalid`、`plugin_not_found`、`plugin_conflict`、`plugin_operation_in_progress`、`plugin_operation_failed` 和 `monitor_unavailable`，映射到计划规定的稳定 HTTP 状态。
- 路由测试覆盖六个普通用户拒绝路径，确认授权中间件 abort 后 Monitor 调用次数保持为零；隔离验收进一步使用真实注册、Cookie、管理员提升和 Backend 代理验证未登录 `401`、普通用户 `403`、管理员成功。

### 1.5 Bash 生命周期、CI、文档与版本

- `dev.sh` 现在构建 Monitor 和插件包，先启动 Monitor 并由 Plugin Manager 安装/恢复 Exporter，再启动 Backend；不再直接启动 Exporter。旧 Phase 5 record 只作为显式 legacy 安全迁移检查处理。
- `down.sh` 优先通过 Monitor 正常关闭其拥有的 Exporter，并保留对旧 record 的所有权校验清理；`verify.sh` 校验 Monitor 进程与 `/health`，继续校验受管 Exporter 的 health/metrics。
- 新增 `scripts/verify-monitor.sh` 的无 Docker self-test 和随机隔离 project 验收；真实矩阵包含 Backend 双用户态授权、内部 token、安装失败清理、真实安装启动、幂等启停、running 更新、失败回滚、伪造 PID 保护、stopped 更新和 Monitor 重启恢复。
- CI 新增 Monitor formatting/test/vet/race/真实生命周期 job，并把新 Bash 入口纳入语法与 self-test 门禁。
- README、Monitor README 和 `.env.example` 更新运行方式、安全边界与配置；根和 Frontend 版本同步到 `1.3.2`。

## 2. 实际变更文件

- `.env.example`
- `.github/workflows/quality-gates.yml`
- `README.md`
- `VERSION`
- `backend/cmd/server/main.go`
- `backend/internal/apperror/error.go`
- `backend/internal/config/config.go`
- `backend/internal/config/config_test.go`
- `backend/internal/exporterplugin/client.go`
- `backend/internal/exporterplugin/client_test.go`
- `backend/internal/exporterplugin/handler.go`
- `backend/internal/http/api.go`
- `backend/internal/http/response/response.go`
- `backend/internal/http/router_exporter_plugin_test.go`
- `frontend/package.json`
- `frontend/package-lock.json`
- `monitor/go.mod`
- `monitor/cmd/monitor/main.go`
- `monitor/internal/config/config.go`
- `monitor/internal/config/config_test.go`
- `monitor/internal/httpserver/server.go`
- `monitor/internal/httpserver/server_test.go`
- `monitor/internal/plugin/archive.go`
- `monitor/internal/plugin/archive_test.go`
- `monitor/internal/plugin/errors.go`
- `monitor/internal/plugin/manager.go`
- `monitor/internal/plugin/manifest.go`
- `monitor/internal/plugin/manifest_test.go`
- `monitor/internal/plugin/process.go`
- `monitor/internal/plugin/process_test.go`
- `monitor/internal/plugin/storage.go`
- `monitor/internal/plugin/types.go`
- `monitor/README.md`
- `scripts/ci/test_auto_pr_workflow.py`
- `scripts/dev.sh`
- `scripts/down.sh`
- `scripts/package-redis-exporter.sh`
- `scripts/verify-monitor.sh`
- `scripts/verify.sh`
- `dev/logs/Phase-06/Phase-06-02-插件安装包与生命周期管理闭环.md`

## 3. 验证命令与结果

### 3.1 Monitor 与 Backend 固定门禁

以下命令在最终生产代码和测试上执行并通过：

```bash
(cd monitor && test -z "$(gofmt -l .)")
(cd monitor && go test -count=1 ./...)
(cd monitor && go vet ./...)
(cd monitor && go test -race -count=1 ./...)
(cd backend && test -z "$(gofmt -l .)")
(cd backend && go test -count=1 ./...)
(cd backend && go vet ./...)
(cd backend && go test -race -count=1 ./...)
```

结果：Monitor 和 Backend 全量普通测试、vet 与 race 测试全部通过。

### 3.2 Frontend、脚本、Compose 与治理门禁

```bash
(cd frontend && npm test -- --run)
(cd frontend && npm run build)
bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-business.sh scripts/verify-exporter.sh scripts/verify-monitor.sh scripts/package-redis-exporter.sh
docker compose --env-file .env.example --file deploy/compose.yaml config --quiet
scripts/verify-business.sh --self-test
scripts/verify-monitor.sh --self-test
python3 -m unittest discover -s scripts/ci -p 'test_*.py'
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/1.3.2 --base-ref upstream/main
git diff --check
```

结果：

- Frontend 9 个测试文件、48 个测试通过；typecheck 和 Vite production build 通过。
- Bash 语法与 Compose 渲染通过。
- Business safety self-test 通过；Monitor token/SemVer self-test 通过。
- `scripts/ci` 24 个单元测试通过。
- 版本元数据、`develop/1.3.2` 权威分配和 Git whitespace 校验通过。

### 3.3 真实隔离生命周期与 Exporter 回归

```bash
scripts/verify-monitor.sh
scripts/verify-exporter.sh
```

结果：

- `verify-monitor.sh` 使用随机 `gopulse-monitor-<12 hex>` Compose project、随机回环端口、临时 MySQL/Redis、临时插件根、真实 Backend、真实 Monitor 和真实 Redis Exporter 通过。
- 验证了公共 API 的未登录 `401`、普通用户 `403`、管理员 Cookie 成功代理，内部接口无 token `401`，且合法包通过 Backend 安装后真实 Exporter `/health` 成功。
- 验证了失败安装无 Registry/current、start/stop、running 更新、失败更新回滚、伪造 PID 不误杀、stopped 更新和 Monitor 重启恢复；退出清理只删除随机 project、volume、进程和临时目录。
- `verify-exporter.sh` 继续通过真实 Redis INFO、目标停止、认证失败、超时、自动恢复、SIGTERM 和所有权清理回归。

## 4. 与方案的偏差

- 未修改 Compose 产品服务定义：Monitor 在本批按 WSL2/Bash 本地进程运行，隔离验收使用临时 Compose 只承载 MySQL/Redis；计划预计文件中的 `deploy/compose.yaml` 无直接功能需要，因此未制造修改。
- 未执行完整 `scripts/verify-business.sh`：本批没有修改数据库 schema、Compose 产品资源或共享进程记录验证函数；按方案和执行效率规则，只补跑了要求的 `--self-test`。真实 Monitor 验收已单独启动 Backend/MySQL/Redis 并覆盖直接受影响的授权与生命周期路径，没有观察到需要扩大到完整社交/搜索矩阵的回归证据。
- 未新增 Frontend 功能或测试；本批只同步版本，管理员插件管理页面明确留给 Phase 11。既有 Frontend 固定测试与 build 已通过。

## 5. 已知限制与后续项

- 远程 CI 尚未在本地执行环境中产生结果；需在本分支推送后由仓库平台运行，本记录未把未来远程结果写为已通过。
- 本批只支持固定 `redis-exporter`、Linux 当前架构、本地 `tar.gz` 上传和单实例；不支持插件市场、URL 下载、签名、通用 hook、多 target 或旧 release 回收。
- `last_scrape_at`、`last_success_at`、MetricsMonitor、Prometheus 解析、Envelope 和 Publisher 均留给 Phase-06-03。
- 旧 release 按方案保留用于回滚，尚无空间回收策略。
