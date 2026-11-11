# 共享运行时能力

`runtimekit` 为业务 API、Worker 和 Indexer 提供配置校验、结构化日志、HTTP 支持、健康探针、内部指标、信号处理和有界退出。

`containerstats` 与 `cmd/container-exporter` 用于压测期间的容器资源采样。产品插件协议和可观测数据传输契约已删除。

在仓库根目录执行 `make verify MODULE=backend/runtimekit`。Go module 名称仍为 `github.com/Ray-ymq/GoPulse/runtimekit`。
