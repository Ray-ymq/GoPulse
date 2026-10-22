# PT 指标来源与责任

组件状态由 exporter 提供，应用状态由应用 metrics 提供，PT 负责负载、独立探针、正确性核对、专项诊断及窗口计算。完整指标分工见 [PT 指标清单](../../PT/2026-10-22-PT测试指标汇总.md)。

| 来源 | 采集责任 | PT 读取方式 |
| --- | --- | --- |
| MySQL exporter | 通用状态、锁等待聚合、digest、语句事件、文件和表/索引 I/O | 独立 `mysql-metrics-source` 的 `/metrics` |
| 官方 node_exporter | 主机 CPU、内存、磁盘和网络 | `/metrics` |
| 公共 container-exporter | 容器 cgroup v2 原始资源计数 | 带版本的 `/snapshot`；同时支持 `/metrics` |
| 应用 metrics | 请求、连接池、缓存、Outbox、worker/indexer 和业务预算 | 应用内部 metrics 端点 |
| RabbitMQ 自带 Prometheus | PT6 当前使用的队列积压 | `/metrics` |
| PT 数据库诊断 | SQL 原文、事务与阻塞关系、有界历史和环境身份核对 | 只读诊断连接 |
| PT 请求记录 | 吞吐、客户端延迟、业务错误、取消和跨窗口请求 | k6 事件与汇总 |

`sources.py` 只管理独立进程、私有清单和 HTTP 协议。进程在采样开始前启动，启动失败或运行结束时回收。采集器启动响应、指标正文和容器响应均有大小及时间上限；HTTP 不使用环境代理，也不跟随重定向。PT 不导入 MySQL exporter 的 Go 包，不复制常规 SQL，也不自行读取采样用 cgroup 文件。设置和核对容器配额的准备步骤仍可读取配置文件。

`mysql-metrics-source` 位于 exporter 模块中，直接复用同一 collector 和输出契约。该命令读取私有只读 DSN，仅连接回环或私网 IP，监听系统分配的回环端口。现场诊断最多使用 2 个连接，exporter 最多使用 1 个连接；独立进程没有增加原有 4 连接账号上限。该命令用于 PT 直接运行，不替换 Monitor 的标准插件入口。

业务预算由 backend 的后台采样器每次成功采样后发布全部 epoch 的合计，使用已有 Outbox 后台连接池和同一轮查询超时。HTTP metrics handler 不访问业务表。预算表为空或查询失败时删除旧预算和时间戳，并置 `sample_success=0`。时间戳记录预算查询开始时间，避免把检查前开始的查询当作新观测。

`budget.py` 要求预算成功且不超过 15 秒。多个 backend 发布同一全局预算，只选择一个实例作为源，禁止相加。排空检查还要求时间戳不早于本次检查开始时间；最多等候 30 秒，随后拒绝开测或恢复。合计不包含 epoch 标签；按 epoch 的业务现场按需诊断。

Redis exporter 和 RabbitMQ exporter 已有的扩展指标仍需后续接入 PT6。完整事务 TPS、事务分段分布、消息大小分布等缺口应补到所属采集层。当前改动没有证明 278 个指标全部实现，也不补写历史轮次缺失的数据。
