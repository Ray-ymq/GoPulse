# Phase-20-01：容量边界诊断与验收语义

> 目标版本：2.2.1；开发分支：develop/2.2.1；当前状态：未开始。

## 1. 目标与范围

核实 Phase 19 已观察到的恢复边界，交付独立 Phase 20 测量工具与可信基线。本批修订验收
基础设施，不进行产品性能优化；历史候选、profile、回执和发布证据保持原样。

- 分别记录业务闭合、Metrics/Logs/Events 可查询的首次达到时刻和原因。
- 明确业务请求接受/终态水位及可观测数据水位；持续生产时不用瞬时全局 lag=0 证明新鲜度。
- 每阶梯恢复结束或有界超时后再开始下一阶梯，记录真正的测量/停止/恢复边界。
- 用验收专属、可恢复的真实插件操作产生一个受支持 Event，保存产生、接受、存储和查询证据；
  不能直接向存储插入数据冒充 Events 链路通过。
- 分开命名负载调度滞后与采样调度滞后；记录采样时长、失败与缺失，避免错归到产品。
- 量化 Kafka CLI 和直接探针等采样开销；用轻量公共接口或长驻客户端替代已确认昂贵的采样。
- 保持现有配方与四阶梯作为参考，新增 profile/digest；受支持环境与安全停止条件明确。

首版基线 profile 保留 Phase 19 的配方、1,024 虚拟用户、业务比例、四阶梯、15 秒预热、
60 秒测量及三个独立重复；恢复改为本阶梯停止且有终态后最多等待 120 秒，下一阶梯随后开始。
同步阈值保留 achieved RPS ≥ 目标的 95%、P95 ≤ 1,000ms、P99 ≤ 2,000ms、
无超时/非预期错误/明确拒绝/丢调度槽、负载调度滞后 ≤ 100ms。
业务与观测使用独立 120 秒门禁及本阶梯事实/水位，事件必须确认产生；缺失证据不算通过。
宿主最低规格和归属/硬错误停止条件沿用冻结 Phase 19 基线，新增开销对照在同宿主执行。
以上修订形成新 digest，不放宽原请求门禁或事后追认历史结果。

## 2. 允许变更文件与验收

| 文件 | 验收要求 |
| --- | --- |
| loadtest/phase20-capacity-profile.json、loadtest/phase20-capacity-profile.schema.json | 独立 profile；窗口、门禁、时钟前提和事件生成合同可机读 |
| loadtest/report.schema.json、loadtest/internal/load/types.go、loadtest/internal/load/types_test.go | 新旧报告合同可区分；不破坏 Phase 19 字段含义 |
| loadtest/internal/load/runner.go、loadtest/internal/load/runner_test.go、loadtest/cmd/load/main.go、loadtest/cmd/load/main_test.go | 恢复与下一阶梯无交叉；已有调度与到达统计不退化 |
| scripts/ci/phase20_diagnostic.py、scripts/ci/test_phase20_diagnostic.py | 产生真实 Event，隔离各类结束条件，保留首次达标及超时事实 |
| scripts/ci/phase20_sampler.py、scripts/ci/test_phase20_sampler.py | 采样与负载开销分离，有界资源/时间，原始记录连续可复核 |
| scripts/ci/phase20_evidence.py、scripts/ci/test_phase20_evidence.py | 原始事实重算，不接受合成通过或错窗引用 |
| scripts/verify-phase20-diagnostic.sh、scripts/verify-phase20-evidence.py | 固定入口，真实预检和安全清理，退出码与执行状态一致 |
| docs/phase20-capacity-methodology.md | 明确新旧语义、观察者开销、基线事实与原因分类 |

若确需调整现有 sampler 的公共复用边界，先在 update 增补具体文件及兼容门禁；不复制整个旧
编排后静默修改其历史语义。总方案的日志、状态及版本元数据规则同时适用。

## 3. 验收标准与固定门禁

- 最小测试证明“业务已完成但 Event 未产生”“已产生但未可查询”“持续采集但标记已闭合”
  三种情况被正确区分；恢复时间不被另一维度超时覆盖。
- 真实有界预检证明停止、各维度恢复和下一阶梯的因果顺序；不存在后台检查跨阶梯污染。
- 同配置采样启用/停用的有界对照保留实际开销，不以 Kafka CPU 峰值单独认定其为瓶颈。
- 在同一基线候选按四阶梯各三次重复形成测量基线；先通过工具自测和预检，再执行一次基线入口。
- 发布逐阶梯事实和原因分类 product_failure / acceptance_failure / event_not_generated /
  unresolved；unresolved 可留作 02 诊断需求，不允许据此解锁 03 产品优化。

已知检查：go -C loadtest test ./...；python3 -m unittest scripts.ci.test_phase20_diagnostic
scripts.ci.test_phase20_sampler scripts.ci.test_phase20_evidence；原 Phase 19 受影响兼容测试。
待实现固定入口：scripts/verify-phase20-diagnostic.sh --preflight、--baseline；
python3 scripts/verify-phase20-evidence.py --diagnostic <目录>。
两个入口均绑定 --manifest <基线候选manifest> --work <新归属目录>，次数、负载和阈值从
本批独立 profile 读取，不接受运行时覆盖；预检明确 formal=false。
最终补充版本/分支治理和 git diff --check；本批不运行无关全栈功能矩阵。

## 4. 回归与完成条件

回归负载到达/分类/报告合同、旧 Phase 19 接口、安全停止和归属清理。工具错误必须修复并
形成新工具候选后重新预检；不能把错误报告成产品容量边界。
全部确定性门禁和证据校验通过，保存可信基线及限制、创建同名实际日志、更新 2.2.1 并提交，
本批完成；不预先声明完整容量达标或单一产品根因。
