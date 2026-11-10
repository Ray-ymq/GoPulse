# 共享运行时能力

`runtimekit` 提供后端服务共用的进程健康、内部指标、HTTP 支持、日志和退出流程，以及采集插件配置和指标传输契约。Go module 名称为 `github.com/Ray-ymq/GoPulse/runtimekit`，要求 Go 1.26。

本文中的命令均在仓库根目录执行。后端模块关系见[后端 README](../README.md)。

## 目录与能力

| 文件或目录 | 职责 |
| --- | --- |
| [config.go](config.go)、[runtime.go](runtime.go) | 运行模式、实例身份、凭据及进程运行配置校验 |
| [endpoint.go](endpoint.go)、[inventory.go](inventory.go) | 内部指标端点、组件清单和采集范围 |
| [probe.go](probe.go) | 启动、存活与就绪探针 |
| [http.go](http.go)、[logging.go](logging.go) | 请求关联 ID、错误响应、HTTP 日志和通用日志能力 |
| [signals.go](signals.go)、[shutdown.go](shutdown.go)、[serve.go](serve.go) | 进程信号处理、共享退出时间预算和 HTTP 服务退出流程 |
| [exportercontract/](exportercontract/) | Exporter 与进程管理器之间的配置协议、运行参数和配置校验结果 |
| [metriccontract/](metriccontract/) | 指标名称、类型、标签、样本集合和单位的校验与解释 |
| [containerstats/](containerstats/) | 读取 cgroup v2 和进程信息，提供容器资源快照与指标 |
| [cmd/container-exporter/](cmd/container-exporter/) | 容器资源采集器程序入口 |

## 进程生命周期

服务入口负责装配业务资源，`runtimekit` 提供共用机制。接入时按以下顺序组织生命周期：

1. 加载并校验配置，建立进程根 context。需要统一处理退出信号时使用 `SignalContext`。
2. 通过 `BindShutdown` 绑定共享退出时间预算，并保存返回的释放函数。
3. 通过 `NewProbes` 创建探针，为需要依赖检查的服务传入检查函数。检查函数必须响应 context 取消。
4. 完成本地初始化后调用 `Started`，开始对外提供服务。
5. 收到退出信号后调用 `Stop` 撤销就绪状态，再通过 `ShutdownContext` 在共享截止时间内关闭资源。
6. 调用绑定时返回的释放函数，结束退出预算的生命周期。

已有接入方式见[业务服务入口](../business/cmd/server/main.go)、[Monitor 入口](../observability/monitor/cmd/monitor/main.go)和[Marshaller 入口](../observability/marshaller/cmd/marshaller/main.go)。

### 健康探针

`Probes.Wrap` 将探针接到调用方已有的 HTTP listener，其他路由继续由原 handler 处理。

| 路径 | 判断范围 |
| --- | --- |
| `/startup` | 本地初始化是否已标记完成 |
| `/live` | 进程探针是否能响应 |
| `/ready` | 是否已启动、是否正在退出，以及调用方提供的依赖检查是否通过 |
| `/health` | 兼容健康探针路径 |

探针接受无查询参数、无请求体的 GET 请求。就绪检查包含超时、短期缓存和并发检查限制，具体语义见 [probe.go](probe.go)。

## 内部指标

进程内部指标路径为 `/internal/v1/metrics`，由 `NewHandler` 校验 Bearer token。`StartConfiguredWithProbes` 可按组件配置启动内部指标 listener，并挂接已有探针。

`Snapshot` 必须返回已经采集完成且不会再被修改的指标快照，不得在 HTTP 请求处理中访问依赖。快照缺失或超过大小限制时，端点返回指标不可用。

运行模式、组件监听地址、凭据和实例身份规则由本模块校验。环境变量示例见根目录[.env.example](../../.env.example)，部署侧的监听与运行约束见[运行契约](../../deploy/runtime-contracts.json)。

## 采集契约

`exportercontract` 负责解析管理请求及交给插件的运行配置。数据源配置和凭据由具体插件解释，共享协议维护进程管理器拥有的运行参数。

`metriccontract` 校验指标传输格式、标签和样本集合，并提供单位解释。调整契约时，需要同时核对数据生产方、Monitor 采集校验、Marshaller 转换以及查询方的兼容性。

插件模板见 [std_exporter](../observability/exporters/std_exporter/)，可观测数据链路见[可观测系统说明](../observability/README.md)。

## 容器资源采集器

`cmd/container-exporter` 使用 `containerstats.Collector`，根据目标清单中的容器身份和 PID 读取资源数据，提供 `/snapshot` 和 `/metrics`。

| 参数 | 用途 |
| --- | --- |
| `--targets-file` | 必填，指定容器身份与 PID 清单 |
| `--listen` | 回环监听地址，默认 `127.0.0.1:0`，端口由系统分配 |
| `--procfs` | proc 文件系统路径，默认 `/proc` |
| `--cgroupfs` | cgroup v2 文件系统路径，默认 `/sys/fs/cgroup` |

启动后，程序向标准输出写入包含监听 URL 和 schema 版本的 JSON。目标清单格式与资源读取逻辑见 [collector.go](containerstats/collector.go)，公共压测中的使用方式见[指标来源与责任](../../tools/perflab/diagnostics/README.md)。

## 检查与构建

```sh
make check MODULE=backend/runtimekit
make lint MODULE=backend/runtimekit
make test MODULE=backend/runtimekit
make race MODULE=backend/runtimekit
make build MODULE=backend/runtimekit
```

完整验证使用 `make verify MODULE=backend/runtimekit`。PR 按完整文件差异选择模块；仅修改本模块代码时，只检查本模块，并在检查通过后同步 `ci:backend/runtimekit` 标签。手动运行可通过该标签选择本模块。普通 push 仅修改本模块代码时，也只检查本模块，不会自动增加使用方。新分支或公共 CI 配置变更等情况会检查全部 Go 模块。修改共享行为后，需要按影响范围验证业务服务、Monitor、Marshaller 和采集插件。

本地模块引用由使用方 `go.mod` 中的 `replace` 指定。调整目录时保留 module 名称与 import 路径，并同步修改替换路径及根 Makefile 注册。贡献与提交规则见[贡献说明](../../CONTRIBUTING.md)和[CI 说明](../../tools/ci/CI.md)。
