# Phase-14-05：自研组件基础运行指标闭环开发记录

## 状态与基线

- 记录日期：2026-09-11。
- **实施中，未完成批次验收。当前只实现 Backend module 内的端点与内存状态基础代码，尚未接入生产进程。不能据此宣称六组件指标闭环可用。**
- 已 fetch `upstream`，从 `upstream/main` 的 `3c57a73`（版本 `1.11.4`）创建 `develop/1.11.5`。
- 目标版本为 `1.11.5`；批次未完成，根 `VERSION` 和其他产品版本元数据仍保持 `1.11.4`。
- 原 Git 配置代理 `127.0.0.1:5780` 不可连接；使用单次命令代理覆盖至 `127.0.0.1:7890` 成功 fetch，没有修改 Git 全局/仓库代理配置。另一次禁用代理的 fetch 也成功结束。
- 工作区原有未跟踪文件 `~` 未读取、未修改、不纳入提交。

## 实际变更

| 文件 | 已实现内容 |
| --- | --- |
| `backend/internal/componentmetrics/endpoint.go` | 独立 HTTP handler：Bearer token 至少 32 bytes、散列后常量时间比较、重复 Authorization 拒绝；按认证 → path → method → query/body 的顺序返回 401/404/405/400；安全固定错误、无缓存响应、exposition 大小门禁；Backend/Worker/Indexer 固定 host/container 地址；同步绑定错误、共享 root request context、调用者传入 shutdown context 的 listener 基础实现 |
| `backend/internal/componentmetrics/endpoint_test.go` | 请求拒绝优先级、合法 exposition、未知/超限快照 503、短 token、固定地址、真实端口占用启动失败和有界关闭测试 |
| `backend/internal/componentmetrics/backend.go` | Backend 内存指标状态；启动时冻结注册 method/template 对；固定 unmatched 桶；请求 count/duration 通过同一原子指针发布，热路径不执行网络 I/O、不持有 mutex、不保存客户端原始标签；outbox 未知/失败时快照不可用；依赖初始 -1；最近发布初始 0 |
| `backend/internal/componentmetrics/backend_test.go` | 初始值与失败恢复、同 tuple 成对累计、未知 method/path 不成为标签、并发更新不丢失及 race 验证 |
| 本记录 | 已执行工作、检查和后续未满足项 |

## 当前已实现的 Backend 状态接口

这些是内存状态接口的规则，**不是六组件已冻结并接入的生产目录**。实际 Gin 路由清单及全部组件精确 allowlist 仍须在接入 Monitor/Marshaller 前补齐。

| family | kind / unit | labels 与值 |
| --- | --- | --- |
| `gopulse_backend_http_requests_total` | counter / 次 | `method,route,status_class` |
| `gopulse_backend_http_request_duration_seconds_total` | counter / seconds | 与 requests 相同 tuple，同次完成原子发布 |
| `gopulse_backend_outbox_pending` | gauge / 条 | 无 |
| `gopulse_backend_outbox_oldest_age_seconds` | gauge / seconds | 无 |
| `gopulse_backend_outbox_last_publish_success_timestamp_seconds` | gauge / Unix seconds | 无 |
| `gopulse_backend_dependency_up` | gauge / 状态 | `dependency=mysql\|redis\|rabbitmq\|elasticsearch`；值 -1/0/1 |

- 共 6 families。HTTP tuple 惰性输出；没有请求时只输出 TYPE 元信息，不伪造请求事实。
- method 固定为 `GET,POST,PUT,PATCH,DELETE,HEAD,OPTIONS,CONNECT,TRACE,unknown`；`unknown` 只用于未匹配桶。
- route 仅接受构造时提供的注册模板；找不到 method/template 对时归到 `_unmatched`，不将原始 path/query 作为新标签。
- status class 为 `1xx,2xx,3xx,4xx,5xx`。
- 内存构造器至多接受 64 个注册项；这是防止无界构造的安全上限，不是预留 64 个生产路由。实际去重注册 method/template 对数为 R 时，精确最大 sample 数是 `2 × 5 × (R + 10) + 7`，最多 747；body 上限 262144 bytes。生产 R 与固定路由清单尚待实际装配时记录。
- outbox 没有成功快照或最近采样失败时，Snapshot 返回不可用，handler 返回 503；成功空队列才允许输出两个零。
- Redis/RabbitMQ/Elasticsearch 的初始 dependency 值不因构造器或 Snapshot 被改成成功；Outbox 成功采样接口会记录 MySQL 成功事实。
- 当前没有任何真实业务/依赖调用这些记录接口；测试中的交互不能充当生产计数点或真实闭环证据。

## 已执行验证

| 命令 | 结果与适用范围 |
| --- | --- |
| `(cd backend && go test ./internal/componentmetrics)` | 通过；端点基础实现阶段运行 |
| `(cd backend && go test -race ./internal/componentmetrics)` | 通过；包含最终 Backend 状态、认证边界、快照、并发与 listener 测试 |
| `git diff --cached --check` | 通过；提交前检查本次暂存改动 |
| `docker info --format '{{.ServerVersion}}'` | 返回 `29.7.2`；仅证明 Docker daemon 可访问，没有执行 Compose 验收 |

没有读取第三方依赖源码，也没有执行全仓审计或独立 review 门禁。

## 未完成项与继续执行位置

1. 在实际 Gin 路由注册完成后冻结 Backend 路由清单与精确 sample 数；接入真实请求完成点、5s/1s 有界 outbox 聚合采样、发布确认及 MySQL/Redis/RabbitMQ/Elasticsearch 实际交互。当前没有 SQL 聚合采样器或生产更新点。
2. 实现 Business Worker、Search Indexer、Monitor、Router、Marshaller 的实际指标状态与处理/队列/依赖更新点，并记录各自精确 family/kind/unit/labels/sample/body 预算。不能直接沿用 Backend 的预算。
3. 将内部 listener 接入六个进程的配置、独立 token、root context 与共享关闭 deadline；目前仅 Backend module 提供三个固定地址的基础函数，**尚无进程监听这些端口**。公共路由未增加 metrics 路由。
4. 完成 Monitor 固定 component targets、Monitor/Marshaller 双层严格校验、Router 来源契约、Backend/Frontend catalog 和 Compose 内部网络/token 配置。当前尚未修改这些路径。
5. 新建并执行 `scripts/verify-component-metrics.sh`，证明六组件真实活动、Backend 查询、source/target/producer 隔离、敏感标签扫描、dependency 故障恢复、endpoint 故障隔离和必要消费/业务/授权回归。当前没有可交接的 Phase-14-06 六组件查询证据。
6. 执行实施方案第 8 节全部固定完成门禁；当前仅运行上述受影响 package 检查，不代表四个 Go module、Frontend、Compose 或整个批次验收通过。
7. 所有完成条件通过后才更新版本至 `1.11.5`，补齐实际计数点、真实数值、标签基数、固定查询与最终命令结果，并将本记录状态改为完成。

当前没有发现需要修改验收合同的证据，也没有将未实现工作列为非阻断优化。本次提交是同一批次的中间实现提交，不是完整批次交付；继续执行应留在 `develop/1.11.5`，不打开完成 PR、不升级版本。
