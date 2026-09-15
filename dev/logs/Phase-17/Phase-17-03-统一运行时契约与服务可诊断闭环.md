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

## 继续实施进度（尚待最终验收）

已继续扩展十二进程 Probe、Request ID、日志公共原语、typed loader 公共边界以及合同／Bundle／lifecycle 消费。由于这次直接修改了 Backend 共享 logger 与多处内部 HTTP client，最小回归扩展为 Backend 全 module 测试；不是常规代码审计或覆盖率扩张。首轮全 module 测试通过；随后对 worker 超时退出码的语义修改另行验证。Redis 原有 Field(err) 断言与公共配置错误格式的适配、readiness 250ms 缓存引起的既有立即恢复断言、HTTP 错误增加 Request ID 引起的旧断言，均按新契约做直接修正。

## 当前候选实现（最终容器门禁进行中）

十二组件合同、Probe、配置、日志、Request ID、共享退出预算、六 Exporter、Monitor 子进程、Bundle/lifecycle 和双前端适配已落地，当前候选元数据为 1.14.3。此前“未完成项”是基础提交时的历史状态，不代表当前实现缺失。此提交为生成不可变候选 Bundle 所需的干净源码提交，**不声明最终验收完成**。

继续执行时发现并修复：edge 负向断言误包含既有 `/health` 与 `/ready`（保持原有入口，仅禁止新增入口）；开发状态页适配 runtime v1 且不臆造依赖状态；Kafka 请求重试超时；Worker 预算耗尽前取消并等待 requeue 收尾。第二次 signal 用真实子进程验证，HTTP 在途正常/超时和 consumer 在途 requeue 用最低有效测试层验证。

当前已通过：共享 runtime package/race、Backend 固定包、Worker/Indexer race、Router/Marshaller/Monitor module、Monitor plugin race、六 Exporter package/race、双前端 test/build、lifecycle release/control、合同自测试/静态检查、版本/分支检查。继续中的真实 runtime 故障注入、最终 Compose 和实际 Bundle 结果将在最终记录替换本历史进度说明。

真实插件启动场景发现 Monitor 仍按历史字面量比较 `/health` 响应，导致新的 runtime v1 Exporter 启动后被误判失败并停止。已改为按已校验的 package version 选择健康响应合同：历史包保留原 exact service JSON，1.14.3 起接受 v1 Probe，固定端口与进程 ownership 检查保持不变。直接兼容/不兼容测试、Monitor 全模块与 plugin race 通过。初版实际 Bundle 已构建并通过 checksum 校验；因本产品修复需另建最终候选，不复用旧 Monitor 镜像作最终验收。

## 必需门禁触发的范围扩展：Kafka 数据目录对齐

在最终 Compose 的强制容器替换后，新请求日志 90s 内始终不可见；Topic initializer 输出重新创建同名 Topic。针对该实测故障，只读取当前锁定 Kafka 镜像的实际配置，确认 `log.dirs=/tmp/kraft-combined-logs`，而 Compose 声明的持久卷挂载点为 `/var/lib/kafka/data`。这是现有声明持久化契约未落实，不能靠降低 Worker 退出码、只查旧日志、重启消费者或跳过固定门禁掩盖。

因此扩大本批的直接配置修复范围：显式指定 Kafka 使用已声明的数据卷目录；在既有 Kafka 替换场景增加前后 Topic ID 不变检查，并继续用新 Request ID 证明日志链路恢复。不会修改 Rabbit/Kafka commit、rebalance、业务 Schema、已有用户容器或数据，也不开展历史升级矩阵。对旧安装：旧 Kafka 的容器可写层可能持有尚未迁入命名卷的数据，不能直接重建容器；需先制定离线保全/迁移方案。本批候选不声明历史版本自动升级兼容。
