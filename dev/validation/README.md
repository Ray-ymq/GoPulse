# 验证与验收入口

## 当前原生入口

| 需要验证什么 | 查询位置 |
| --- | --- |
| 模块测试、真实依赖、浏览器、Compose 与生命周期 | [原生入口登记](local-development-tests.md) |
| 本批必须执行哪些检查、使用什么候选与预算 | [阶段索引](../phases/README.md)链接的总方案和分方案 |
| 是否需要新测试或验收工具 | [工具复用规则](../../ci/AGENTS.md) |
| 哪些结果已证明、哪些能力尚未验证 | [能力状态](../status/capability-status.md) |

先选择改变边界的最低有效层级，再执行分方案的固定完成门禁。
命令存在、模块测试通过和完整产品验收分别理解。

## 历史矩阵与方法

下列文件保留原候选、窗口和证据范围。Phase 16–21 的正式矩阵和 Phase 18–20
容量/长期执行器的退役边界见能力状态；其中的旧执行命令不直接作为当前入口。
仍有效的方法或验收合同由当前分方案明确引用，再使用实际原生入口执行。

| 阶段 | 历史材料 |
| --- | --- |
| Phase 16 | [Linux 最终产品矩阵](Phase-16/phase16-linux-matrix.md)、[当前版本恢复验收](Phase-16/phase16-current-recovery.md) |
| Phase 17 | [Compose 验收与后续交接](Phase-17/phase17-compose-matrix.md) |
| Phase 19 | [容量方法](Phase-19/capacity-methodology.md) |
| Phase 20 | [容量方法](Phase-20/phase20-capacity-methodology.md)、[优化对照](Phase-20/phase20-optimization.md) |

实际执行结果从 [logs](../logs/README.md)及其引用的 CI、证据或归档读取；按用途保留，
不要求所有输出常驻源码目录。查阅文档不授权重跑、恢复暂停验收或改变候选；
执行按实施合同和预算进行。
