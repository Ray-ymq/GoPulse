# Phase-19-01：探针隔离与尾延迟指标

> 目标版本：`2.1.1`
>
> 开发分支：`develop/2.1.1`

## 1. 目标

修复业务并发准入与启动/存活/就绪探针共享拒绝路径的问题；增加固定低基数的 HTTP 延迟分布和
并发拒绝指标，使产品自身能够解释容量测试中的尾延迟与饱和，而不依赖负载器单边数据。

## 2. 实施范围

- 业务准入只包围业务/管理 API；探针不占用业务并发槽，且不绕过自身依赖与停止语义。
- Compose Backend healthcheck 改用直接私有探针，不经过 Frontend、Nginx 或业务准入。
- 保留现有请求数与 duration total，增加固定桶分布、当前并发、并发上限和拒绝总数。
- 固定桶覆盖当前容量目标所需的毫秒到秒级范围；桶和标签在编译期冻结。
- 扩展共享指标目录、Monitor 校验、Envelope、Marshaller 转换和 Backend 查询目录。
- 重新生成管理前端组件指标合同，但本批不新增大屏或交互页面。

## 3. 允许变更文件与逐文件验收

| 文件 | 文件级验收条件 |
| --- | --- |
| `backend/cmd/server/main.go`、`backend/cmd/server/main_test.go` | 探针绕过业务准入；API 饱和仍返回固定 `backend_busy`；并发状态被观测 |
| `backend/internal/http/router.go`、`backend/internal/http/router_test.go` | 探针与 API 路由边界可单测且不改变认证/授权 |
| `deploy/compose.yaml` | 两个 Backend healthcheck 使用直接私有探针，端口仍不发布到宿主 |
| `deploy/runtime-contracts.json`、`deploy/runtime-contracts.schema.json` | 探针路径、监听器和新增容量信号与实际一致 |
| `docs/runtime-contracts.md`、`docs/component-metrics.md` | 说明准入/探针隔离、桶、标签、查询和兼容边界 |
| `componentmetrics/backend.go`、`componentmetrics/backend_test.go` | 无客户端动态标签；固定桶、当前并发、上限、拒绝计数并发安全且有界 |
| `componentmetrics/catalog.go`、`componentmetrics/registry.go`、`componentmetrics/registry_test.go`、`componentmetrics/validation.go` | 指标目录和快照严格表达分布族，拒绝缺桶、乱桶、非法 kind/label |
| `monitor/internal/metrics/collector/components.go`、`monitor/internal/metrics/collector/components_test.go`、`monitor/internal/metrics/collector/cluster_contract_test.go` | 完整接收合法分布族并拒绝不完整或越界样本 |
| `monitor/internal/metrics/envelope/envelope.go` | Envelope 保留固定分布样本且不放宽任意指标输入 |
| `marshaller/internal/envelope/envelope.go`、`marshaller/internal/envelope/components_test.go`、`marshaller/internal/envelope/cluster_test.go` | 第二次校验与共享目录一致，非法桶/标签永久拒绝 |
| `marshaller/internal/metrics/transform.go`、`marshaller/internal/metrics/transform_test.go` | 确定性转换并保留 VM 查询分位数需要的样本与标签 |
| `backend/internal/metricquery/metricquery.go`、`backend/internal/metricquery/metricquery_test.go`、`backend/internal/metricquery/components_test.go` | 固定 catalog 支持尾延迟查询且不接受任意 PromQL/标签 |
| `componentmetrics/cmd/catalog/main.go`、`admin-frontend/src/services/componentMetrics.ts` | 生成结果确定，管理端严格合同与 Go 目录一致 |
| `scripts/ci/verify_component_metrics.py` | 校验生成合同、完整链路和桶/标签负例 |
| `scripts/verify-plugin-metrics.sh` | 真实链路证明新 Backend 分布和饱和度指标可写入、可查询 |
| `README.md`、`docs/capability-status.md` | 只声明本批实际证明的诊断能力，不声明容量达标 |
| `dev/logs/Phase-19/Phase-19-01-探针隔离与尾延迟指标.md` | 记录实际文件、检查、结果、偏差和限制 |
| `VERSION`、`.env.example`、`frontend/package.json`、`frontend/package-lock.json`、`admin-frontend/package.json`、`admin-frontend/package-lock.json` | 完成时六处版本一致为 `2.1.1` |

如发现必须修改清单外文件，先在 `update` 修订本方案并合入 `main`；不得在开发分支事后补登记。

## 4. 验收标准

- 单元测试占满业务并发槽时，`/startup`、`/live`、`/ready`、`/health` 仍由 probe contract 响应。
- 普通 API 超过并发上限时立即得到 `503 backend_busy`，没有无界排队或 goroutine 增长。
- 延迟分布可计算固定 route template 的 P50/P95/P99；桶累计、count、sum 一致。
- URL、用户 ID、帖子 ID、request ID、instance ID 不进入延迟指标标签。
- 两个 Backend 的私有探针和完整 Metrics 链路在 Compose 中通过。
- 既有 Go、前端、runtime contract、组件指标和全栈 Compose 回归通过。

## 5. 验证命令

实现中先运行直接受影响的 Go/生成合同检查；最终候选固定门禁：

```text
go -C backend test -count=1 ./cmd/server ./internal/http ./internal/metricquery
go -C componentmetrics test -count=1 ./...
go -C monitor test -count=1 ./internal/metrics/collector ./internal/metrics/envelope
go -C marshaller test -count=1 ./internal/envelope ./internal/metrics
python3 scripts/ci/verify_component_metrics.py
python3 scripts/ci/verify_runtime_contracts.py --contract deploy/runtime-contracts.json --compose deploy/compose.yaml --env .env.example --candidate 2.1.1
scripts/verify-plugin-metrics.sh
scripts/verify-compose.sh
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/2.1.1 --base-ref upstream/main
git diff --check
```

## 6. 完成条件

全部验收标准和固定门禁通过，创建同名实施记录，同步版本 `2.1.1`，只提交本批文件后完成。
任何候选漂移、验收程序错误或必需门禁失败均使批次未完成。
