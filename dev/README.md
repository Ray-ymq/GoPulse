# GoPulse 工程入口

这里组织改代码、编写实施方案、选择验证和维护文档所需的材料。
产品阅读与使用从 [docs](../docs/README.md)进入；组件源码入口见根
[仓库地图](../AGENTS.md#repository-map-and-reading-order)。

## 按任务阅读

| 要做什么 | 先读什么 | 接下来查什么 |
| --- | --- | --- |
| 修改产品代码 | [仓库规则](../AGENTS.md)、[能力状态](status/capability-status.md) | [阶段索引](phases/README.md)中的已分配总方案和分方案；下方代码约定 |
| 修改规则或整理文档 | [仓库规则](../AGENTS.md) | 本页的目录职责与文档维护；对应事实来源 |
| 新建或修订实施方案 | 下方实施方案说明、[工具复用规则](../ci/AGENTS.md) | [生效设计](design/README.md)与已有覆盖 |
| 选择本机测试 | [原生入口登记](validation/local-development-tests.md) | 模块 Makefile、直接相关用例及当前分方案的固定门禁 |
| 修改 API、消息、指标或持久状态 | [技术契约](contracts/README.md) | 所属模块、调用方和对应合同测试 |
| 排查部署、启停或恢复问题 | [操作说明](operations/README.md)、[Bundle 指南](../deploy/release/BUNDLE-README.md) | [生命周期实现](../lifecycle/internal/control/)、实际日志及能力边界 |
| 查找实际做过的工作 | [实施记录](logs/README.md) | 同名日志、候选、回执和原始证据 |
| 阅读整体工程改造 | [工程稳定性方案](design/GoPulse-工程稳定性改造方案.md) | 已完成的规则/文档整理与待排期的工具实现分别标注 |

## 目录职责与事实来源

| 位置 | 负责什么 | 维护边界 |
| --- | --- | --- |
| 根 [VERSION](../VERSION) | 已完成产品版本 | 只在成功批次的完成提交改变 |
| [status](status/capability-status.md) | 当前已验证、未验证、失败边界与退役事实 | 只登记实际证据，不从目标设计推导完成 |
| [design](design/README.md) | 生效基线登记、详细设计、决策和实施映射 | 三份冻结文件保留；新增草案注明状态，不预留批次 |
| [imple](imple/) | 阶段总合同与单批实施合同 | 分配、文件、门禁与预算的权威来源 |
| [phases](phases/README.md) | 阶段阅读顺序和历史导航 | 链接实施合同，不另写分配表或当前完成状态 |
| [contracts](contracts/README.md) | 运行、数据、消息、指标与协议正文 | 修改合同同时核对生产方、消费方和验证边界 |
| [operations](operations/README.md) | 使用、部署、排障与恢复步骤 | 命令有实际入口，支持范围引用能力状态 |
| [validation](validation/README.md) | 当前原生验证入口与历史方法 | 已退役执行器不作为当前可运行命令；门禁由实施合同引用 |
| [logs](logs/README.md) | 实际实施、命令、偏差与原始证据 | 同名镜像实施方案；只写真实执行结果 |
| [rules](rules/) | 执行预算及有日期的维护记录 | 根 AGENTS 优先；旧维护记录按其日期理解 |
| [releases](releases/) / [review](review/README.md) | 发布快照与历史评审 | 保留当时的候选、范围与结论 |

## 编写实施方案

总方案写清阶段结果、批次依赖、版本/分支分配、跨批集成和里程碑条件。
分方案写清单一结果、允许文件、直接相关的已有覆盖、固定门禁和完成条件；公开接口、
命令或配置改变时列出相应文档。待实现入口标明状态，不能作为已可执行的前置条件。
昂贵构建与真实/长时验收按[成本与接续](rules/implementation-execution-budget.md)约定预算；
普通任务不另填时间台账。原已分配计划的预算和门禁继续按其合同执行。

总方案位于 `dev/imple/Phase-XX/Phase-XX-总实施方案.md`，分方案是同目录下的
`Phase-XX-NN-<名称>.md`，实际日志同名镜像到 `dev/logs/Phase-XX/`。
总方案的分配表每行以批次、目标版本、开发分支三列开头，可续接交付与进入条件列：

```text
| Phase-XX-NN | `x.y.z` | `develop/x.y.z` | <主要交付> | <进入条件> |
```

里程碑发布行用同一格式，批次写 `Milestone-XX-Release`。治理脚本按该格式读取批次、
目标版本和开发分支并核对一致性，新增或重排批次时保持表格可解析。

## 代码改动约定

下列要求用于本次改变的边界。最终 diff 再复核一次，把实际结论与必要检查记入对应日志；
按实施合同选择门禁，同候选的有效检查不重复运行。

| 边界 | 具体复核内容 | 验证入口 |
| --- | --- | --- |
| Go 公开 API | 新导出符号是否必要；优先扩展已有接口；说明调用方、错误/默认值及兼容影响 | 所属模块与受影响消费者的 Go 测试 |
| HTTP 与权限 | API 状态码/错误码、请求限制、角色装配与实时授权；私有凭据和上游原文不进入公开 DTO | Backend 测试及受影响 integration/browser scope |
| 数据库与迁移 | MySQL 事实、事务边界、锁、dirty/ahead 状态及失败恢复；缓存/投影不能成为事实源 | 既有真实 MySQL 与业务 integration |
| 消息与重试 | 持久化和 ack/commit 顺序、重复投递、幂等、终态、所有权与有界重试 | 所属模块测试及必要真实链路 |
| 并发与进程 | goroutine/子进程归属、取消传播、状态锁、关停时间及归属清理 | race、工具行为测试、必要实际启停 |
| 配置与序列化 | 默认值、未知/非法值、字段兼容、schema 与生产/消费双方 | 对应[合同](contracts/README.md)和模块测试 |
| 双前端 | 共用同源会话和 HttpOnly Cookie；管理权限由 Backend 执行；DTO 与 API 同步 | Vitest、既有类型检查及改变行为的 Playwright 用例 |
| 工程工具 | 参数校验、子进程非零退出、资源身份、诊断和清理失败分别保留 | [工具规则](../ci/AGENTS.md)要求的最低层行为测试与真实边界 |

## 文档维护

1. 先确定正文归属。事实写 status，接口写 contracts，步骤写 operations/validation，
   执行合同写 imple，实际结果写 logs；导航只链接，避免建立第二份版本、状态或门禁清单。
2. 每份新设计或方案说明是草案、已分配合同还是已实施记录。历史材料按日期和候选阅读；
   图必须区分目标与实现快照。当前架构入口不能把旧版本图标为当前运行拓扑。
3. 命令从 Makefile、package scripts 和实际 CLI 核对；前置环境、工作目录、模块/scope
   要能定位。待实现接口明确标为拟新增；示例用当前已分配分支或注明占位符。
4. 改公开行为、配置、命令或恢复步骤时，同批维护直接相关正文与验证；没有相关影响时
   不为收口同步所有 README。能力状态只接受真实完成结果。
5. 合并重复正文或规则时保留独有要求、更新有效引用，再删除多余文件。祖先 AGENTS
   已覆盖的要求不在子目录重抄；三份冻结文件及已完成合同、日志、评审和原始证据按根规则保留。
6. 提交前检查改动文档的本地链接、锚点、源码路径、命令和权威关系。报告结果时注明统计范围
   （本次改动还是全量）；三份冻结原文与历史日志、评审、已取消方案中的断链按原样保留，
   不计入本次回归。自动文档门禁仍是
   [改造方案](design/GoPulse-工程稳定性改造方案.md)中的待实现项，当前不宣称 CI 已会拦截。

## 移除的旧入口

以下转发页和规则已移除。历史记录中的文件名保留当时含义；查原文使用其 Git revision，
查目前正文使用下列入口。转发页无有效链接或代码调用；旧规划规则的有效引用已更新。

| 旧路径 | 当前正文 |
| --- | --- |
| `dev/imple/AGENTS.md`、`dev/phases/AGENTS.md` | [根开发指南](../AGENTS.md)、本页的实施方案说明 |
| `docs/runtime-contracts.md` | [运行契约](contracts/runtime-contracts.md) |
| `docs/component-metrics.md` | [组件指标](contracts/component-metrics.md) |
| `docs/events-v1.md` | [Events v1](contracts/events-v1.md) |
| `docs/releases/backup-restore.md` | [备份恢复](operations/backup-restore.md) |

[Plan.md](phases/Plan.md)、[早期全局发展总纲](phases/GoPulse-高并发与可观测后续路线图.md)
和[历史候选设计](../docs/future/kubernetes/README.md)用于追溯各自研究基线。
当前架构权威只从设计索引登记的生效基线读取。
