# 技术契约

这些文档解释已实现技术的边界。标题中的 Phase、版本和明确限制应与正文一起阅读；
完整产品状态见[能力状态](../status/capability-status.md)，批次门禁见对应实施方案。

| 文档 | 内容 |
| --- | --- |
| [运行契约](runtime-contracts.md) | 配置、就绪与进程运行边界 |
| [Migration 与消息状态](migration-state.md) | 迁移、检查点与消息处理可靠性 |
| [组件指标](component-metrics.md) | 指标目录、私有端点与低基数约束 |
| [Metrics 告警](alerts-metrics.md) | Phase-15-02 的告警 API 与评估范围 |
| [Events v1](events-v1.md) | 事件词汇及投递边界 |
| [Trace 与新鲜度](phase20-trace-and-freshness.md) | Phase-20-02 的单业务链路关联 |
| [观测数据生命周期](observability-retention.md) | 保留、删除、查询与回收限制 |
| [备份格式 v1](backup-format-v1.md) | 格式层阶段记录，不单独证明产品恢复能力 |
| [离线插件状态](backup-plugin-state.md) | 备份内部组件与状态传输 |

完整备份操作边界见[备份与恢复](../operations/backup-restore.md)。
