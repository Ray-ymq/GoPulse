# Phase-17-03：统一运行时契约与服务可诊断闭环开发记录

## 状态

**进行中，未完成整批实施或验收，不可作为 Phase-17-04 的完成输入。**

- 记录日期：2026-09-15。
- 目标版本／工作分支：`1.14.3`／`develop/1.14.3`。
- 已获取 upstream 与 origin 最新状态，从 `upstream/main` 的 `5599369` 创建工作分支。
- 主线完成版本为 `1.14.2`；本批尚未完成，因此 `VERSION` 与双前端版本元数据保持 `1.14.2`，没有冒充完成版本。
- `docker info --format '{{.OSType}} {{.Architecture}}'` 返回 `linux x86_64`。尚未建立独立容器验收 project，也未运行容器验收。
- 开工时已有未跟踪目录 `Management_Center/`，未改动、未纳入本批。

## 本次实际完成的局部实现

### 共享 Probe 原语

新增 `componentmetrics/probe.go`：

- `/startup`、`/live`、`/ready`、兼容 `/health`，使用一致的 JSON、`no-store` 和 content type；`/health` 与 `/live` 使用同一响应逻辑。
- 拒绝非 GET、query（包括空 query）、请求 body 与 transfer encoding；独立 Probe handler 的未知路径返回 JSON 404。
- `/live` 不执行依赖检查；启动、就绪、停止状态分离。
- readiness 使用一个在途 checker 槽位、超时与缓存；不遵守 context 的 checker 最多占据一个后台槽位，不会因每次请求创建新的 checker。
- 根 context 取消或显式 Stop 后撤销 readiness；checker 失败信息不进入 Probe body。
- `RuntimeContractVersion = "1"` 目前仅标识新增 Probe 响应语义，**不表示本批机器合同已交付**。

扩展现有私有 listener：

- `StartConfiguredWithProbes` 在原 metrics handler 外组合 Probe，不新建第二套 listener。
- 未接入 Probe 的已有调用保留原有行为。
- metrics 路径仍要求独立认证；Probe 不改变其凭据边界。
- listener Shutdown 先调用 Probe Stop，再执行已有 HTTP shutdown。

### Worker 与 Indexer 接入

- Business Worker 在现有私有端口 `19102` 接入四条 Probe 路径；readiness 要求消费 session 已建立、MySQL 检查成功。
- Search Indexer 在现有私有端口 `19103` 接入四条 Probe 路径；readiness 另要求 Elasticsearch 检查成功。
- `worker.Runtime.Ready` 使用原子 session 状态，不通过额外 RabbitMQ 连接探测消费状态。
- 复用现有 signal context；启动完成后标记 startup，根 context 取消后 readiness 不再成功。
- 没有修改 Compose、发布宿主端口、edge、授权、Schema migration 或消息提交逻辑。

### 已观察到的基线测试修正

首次运行 componentmetrics 测试发现 Backend 指标预算实际为 687，而测试仍断言 677。
通过主线 `componentmetrics/routes.go` 的提交 `453b1db` 确认：新增 `/api/v1/admin/overview` 路由产生额外 10 个样本槽位。仅将测试预算断言修正为 687，未扩大运行时代码预算。

## 实际修改文件

- `componentmetrics/probe.go`
- `componentmetrics/probe_test.go`
- `componentmetrics/config.go`
- `componentmetrics/endpoint.go`
- `componentmetrics/registry_test.go`
- `backend/internal/worker/runtime.go`
- `backend/internal/worker/runtime_test.go`
- `backend/cmd/business-worker/main.go`
- `backend/cmd/search-indexer/main.go`
- 本开发记录。

## 已运行验证

| 命令 | 结果 |
| --- | --- |
| `(cd componentmetrics && go test -count=1 ./...)` 首次 | 失败：上述既存 Backend 预算断言不一致 |
| `(cd componentmetrics && go test -count=1 ./... && go test -race -count=1 ./...)` 修正后 | 通过 |
| `(cd backend && go test -count=1 ./internal/worker ./cmd/business-worker ./cmd/search-indexer)` | 通过 |
| `(cd backend && go test -count=1 ./cmd/server ./cmd/business-worker ./cmd/search-indexer ./internal/config ./internal/http/...)` | 通过 |
| `(cd backend && go test -race -count=1 ./internal/worker ./cmd/business-worker ./cmd/search-indexer)` | 通过；新增原子 session 状态直接影响并发行为，因此对受影响 worker 包与入口补充 race 验证 |
| `(cd componentmetrics && go test -count=1 ./... && go test -race -count=1 ./...)` 最终 Probe body 测试补充后 | 通过；只因直接测试文件改变重跑该模块 |
| `python3 scripts/ci/validate_versions.py` | 通过，元数据仍一致为完成版本 `1.14.2` |
| `python3 scripts/ci/validate_branch.py --branch develop/1.14.3 --base-ref upstream/main` | 未通过：完成版本仍为 `1.14.2`，不符合本批完成门禁要求的 `1.14.3`；未通过提前升版规避 |
| `git diff --check` | 通过 |

本地捕获输出位于 `.run/phase17-03/backend-foundation-tests.log` 和 `.run/phase17-03/componentmetrics-foundation-tests.log`，不作为可发布的容器 evidence。

## 未完成项与交接限制

以下是**仍未完成的必需工作**，不是非阻断后续优化：

1. 十二组件机器合同、schema、静态交叉校验器、环境变量完整登记与配置负向测试。
2. Backend、Router、Marshaller、Monitor、六 Exporter 的完整 Probe 接入及硬／软依赖矩阵。
3. 全组件统一退出语义、超时非零退出与 idle／在途 HTTP／consumer／第二次 signal 容器证据。
4. edge 与内部 HTTP Request ID、统一错误包络与双前端安全未知错误处理。
5. 全组件日志 schema、Marshaller mapping／vocabulary 对齐、状态变化限速与 Secret scan。
6. Compose healthcheck、退出余量、release manifest／Bundle 合同收录、checksum、lifecycle 合同消费。
7. `verify_runtime_contracts.py`、`verify-runtime-contracts.sh` 与独立真实 Linux amd64 容器验收。尚无 Bundle digest、信号耗时或内部端口负向 evidence。
8. 其余固定完成门禁：Router、Marshaller、Monitor、六 Exporter、双前端、Compose 与最终完成版本／分支门禁。
9. 全部门禁通过后才能更新 `VERSION`／前端元数据为 `1.14.3`，提交完成状态并推送。

无需因上下文切换重跑上述已成功且相关实现未变化的检查。本记录不声明 Phase-17-03 完成，也不声称任何尚未执行的测试已通过。
