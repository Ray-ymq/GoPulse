# GoPulse 文档入口

按要完成的事情选择入口。完成版本读取 [VERSION](../VERSION)，已验证与未验证范围读取
[能力状态](../dev/status/capability-status.md)；本页不复制产品版本、完成状态或验收数值。

## 理解、运行和维护产品

| 要做什么 | 入口 |
| --- | --- |
| 第一次了解 GoPulse | [项目导读](../项目导读.md) |
| 启动、登录、使用两个前端 | [使用手册](../使用手册.md) |
| 安装 Linux Bundle | [Bundle 指南](../deploy/release/BUNDLE-README.md) |
| 维护、排障与备份恢复 | [操作说明](../dev/operations/README.md) |
| 查 API、消息、指标和数据合同 | [技术契约](../dev/contracts/README.md) |
| 改代码、规划、选择测试 | [工程入口](../dev/README.md)、[仓库地图与规则](../AGENTS.md) |
| 查当前可运行验证命令 | [原生入口登记](../dev/validation/local-development-tests.md) |
| 查批次、版本、分支和验收合同 | [阶段索引](../dev/phases/README.md) |

## 理解目标架构

生效基线登记在[设计索引](../dev/design/README.md)。两份设计与落地大纲冻结为
2026-10-06 基线；目标能力成为产品事实须有实施和验收证据。

| 材料 | 职责 |
| --- | --- |
| [高并发架构设计](<GoPulse 高并发架构设计.md>) | 业务模块、事实、一致性、异步执行、容量与故障 |
| [可观测架构设计](<GoPulse 可观测架构设计.md>) | 观测、控制、查询、诊断实验，以及共同身份和环境资源合同 |
| [目标图解](../dev/design/diagrams/target-architecture/README.md) | 三张辅助阅读图；按两份设计正文理解 |
| [目标架构落地大纲](../dev/design/GoPulse-目标架构落地大纲.md) | 冻结的专题分解、迁移与硬依赖 |
| [工程稳定性方案](../dev/design/GoPulse-工程稳定性改造方案.md) | 规则、文档、工具链、验证、CI 和诊断改造的具体文件与完成条件 |

目标调整与实施排期按设计索引和根规则执行。草案、索引或历史路线不覆盖生效设计和已分配合同。

## 历史与证据

| 材料 | 怎样使用 |
| --- | --- |
| [实施日志](../dev/logs/README.md) | 查实际命令、候选、结果、偏差与原始证据 |
| [验收方法](../dev/validation/README.md) | 区分当前原生入口与历史矩阵；执行以有效分方案为准 |
| [实现图快照](../dev/design/diagrams/README.md) | 按图的日期和版本阅读；当前角色与拓扑查源码和能力状态 |
| [历史评审](../dev/review/README.md)、[发布记录](../dev/releases/) | 按当时版本、日期与候选范围追溯 |
| [早期整体架构](<GoPulse 整体架构.md>)、[历史规划](../dev/phases/Plan.md) | 理解演进背景，按生效设计登记核对其适用性 |
| [Kubernetes 历史候选](future/kubernetes/README.md) | 保留设计输入，不据此分配 Phase、版本或分支 |

历史结论不随文档整理扩大。目录归属、正文合并和引用维护规则统一见
[文档维护](../dev/README.md#文档维护)。
