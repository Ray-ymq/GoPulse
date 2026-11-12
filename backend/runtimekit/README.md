# 共享运行时能力

`runtimekit` 为业务 API、Worker 和 Indexer 提供配置校验、结构化日志、HTTP 支持、健康探针、信号处理和有界退出。

指标接口、采集器和压测资源采样已删除。Worker 和 Indexer 的私有 HTTP 端口仅提供健康检查。

在仓库根目录执行 `make verify MODULE=backend/runtimekit`。Go module 名称仍为 `github.com/Ray-ymq/GoPulse/runtimekit`。
