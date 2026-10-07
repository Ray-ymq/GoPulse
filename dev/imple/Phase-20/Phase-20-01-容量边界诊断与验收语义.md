# Phase-20-01：容量边界诊断与验收语义

> 目标版本：2.2.1；开发分支：develop/2.2.1；当前状态：未开始。

## 1. 目标与范围

核实 Phase 19 已观察到的恢复边界，交付独立 Phase 20 测量工具与可信基线。本批修订验收
基础设施，不进行产品性能优化；历史候选、profile、回执和发布证据保持原样。

- 分别记录业务闭合、Metrics/Logs/Events 可查询的首次达到时刻和原因。
- 明确业务请求接受/终态水位及可观测数据水位；持续生产时不用瞬时全局 lag=0 证明新鲜度。
- 每阶梯使用独立空项目；恢复结束或有界超时后归档并清理，再从同一配方启动下一阶梯。
- 用验收专属、可恢复的真实插件操作产生一个受支持 Event，保存产生、接受、存储和查询证据；
  不能直接向存储插入数据冒充 Events 链路通过。
- 分开命名负载调度滞后与采样调度滞后；记录采样时长、失败与缺失，避免错归到产品。
- 量化 Kafka CLI 和直接探针等采样开销；用轻量公共接口或长驻客户端替代已确认昂贵的采样。
- 保持现有配方与四阶梯作为参考，新增 profile/digest；受支持环境与安全停止条件明确。

首版基线 profile 保留 Phase 19 的配方、1,024 虚拟用户、业务比例、四阶梯、15 秒预热、
60 秒测量及三个独立重复；每个重复的四阶梯各自初始化、预热和测量，共十二个阶梯单元。
停止调度后最多 30 秒收齐请求终态，再分别等待业务与观测恢复最多 120 秒，随后归档和归属清理。
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
| loadtest/internal/load/workload.go、loadtest/internal/load/workload_test.go | 台账保存 operation_id、对象键及返回 ID/revision，原业务比例与请求序列不变 |
| scripts/ci/phase20_diagnostic.py、scripts/ci/test_phase20_diagnostic.py | 产生真实 Event，隔离各类结束条件，保留首次达标及超时事实 |
| scripts/ci/phase20_sampler.py、scripts/ci/test_phase20_sampler.py | 采样与负载开销分离，有界资源/时间，原始记录连续可复核 |
| scripts/ci/phase20_evidence.py、scripts/ci/test_phase20_evidence.py | 原始事实重算，不接受合成通过或错窗引用 |
| scripts/verify-phase20-diagnostic.sh、scripts/verify-phase20-evidence.py | 固定入口，真实预检和安全清理，退出码与执行状态一致 |
| dev/validation/Phase-20/phase20-capacity-methodology.md | 明确新旧语义、观察者开销、基线事实与原因分类 |
| dev/logs/Phase-20/Phase-20-01-evidence/ | 保存候选绑定、脱敏逐阶梯事实、归属清理回执及严格校验结果；来源 digest 可复核；原始私有证据保留在仓库外，不发布密码、cookie、业务正文或私有业务标识 |

若确需调整现有 sampler 的公共复用边界，先在 update 增补具体文件及兼容门禁；不复制整个旧
编排后静默修改其历史语义。总方案的日志、状态及版本元数据规则同时适用。

## 3. 接受台账、终态与观测水位

### 3.1 接受台账

每个已调度请求记录 run_id、repeat、stage、slot_id、operation_id、actor_id、method、
route template、对象键、调度/发送/完成时刻、status、outcome、request_id 及响应中的 ID/revision。
密码、cookie、正文不进入发布台账；业务标识只用于私有证据，不进入指标标签。
accepted 仅指符合冻结路由合同的成功响应；拒绝、超时、传输失败和未预期响应分别记录。
超时请求可能已写入，必须保存 reconciliation 结果，不能计作已确认接受或直接认定未写入。

停止调度后记录 t_stop；收齐所有请求终态后记录 t_drain，并在同一一致性读取中保存受影响
MySQL 事实和本阶梯新 Outbox 事件集合。使用冻结路由表将请求对象键与 event_type、actor_id、
post_id/comment_id/recipient_id、content_revision 关联，保存 event_id 和 outbox_id。
不假设 HTTP 返回 event_id，也不只用 Outbox ID 上下界、响应到达顺序或时间戳推断事务顺序。
无法建立无歧义关联时标记 evidence_incomplete；不得回退到全表计数。
重复幂等请求可共同关联同一对象的 fact_group_id、请求集合、before/after 及唯一实际事件。
不要求判断哪条并发请求首先产生同一幂等效果，也不伪造每次请求都对应一条新事件；
集合关联本身无法消除的歧义才记为 incomplete。

| 操作/事实 | 本阶梯业务终态判据 |
| --- | --- |
| 新发帖、编辑 | 每个实际产生的事件均 published；搜索结果与 t_drain 快照中存活帖子的最终 ID、内容摘要及 content_revision 一致；旧版本被更新版本替代不要求重复旧投影 |
| 删除 | 对应事件 published；MySQL 事实和业务搜索投影均不存在，迟到创建/更新事件不能复活 |
| 评论、首次点赞/关注 | 实际产生的事件 published；需通知的事件按 source_event_id 恰有一条且收件人正确；自操作为冻结合同中的无通知终态，删除对象按现有 tombstone 合同核对 |
| 重复点赞/关注等幂等操作 | 以操作前状态和实际事件集合判定 no_op；不要求新事件或新通知，不允许重复持久效果 |
| 取消点赞/关注、收藏及其他同步写入 | 受影响关系与最终事实快照相符；只有路由合同明确产生的异步事件才纳入事件门禁 |
| 读/登录/会话请求 | 仅按同步路由合同判定，不凭成功读请求新增业务事件 |

路由表覆盖冻结 profile 的全部写路由，并保存 before/after 事实及关联来源。
最终状态以 MySQL 已提交事实为准；若快照自身与已确认请求/持久不变量矛盾，记录产品失败，
不把最终快照当成可以无条件接受错误写入的预期值。搜索通过实际查询核对，不把 ES 写入 ack
代替查询可见；通知按具体 source_event_id 核对，不使用通知总数增量。

### 3.2 观测水位与独立计时

三个通道均记录 marker_id、生成/接受证据、Envelope message_id、source、timestamp 和
Kafka topic/partition/offset。offset 水位是按 partition 的集合，不是跨 partition 的全局序号。
marker_id 仅为验收关联字段，不向产品 Envelope 或指标注入未支持属性。

| 通道 | 真实产生方式 | 可查询判据 |
| --- | --- | --- |
| Metrics | 从 Monitor 正常采集选择 t_drain 后一个真实 Backend 快照，保存其固定族/标签/值/源时间与 Envelope | VictoriaMetrics 可查询到相同 series、量化时间和预期值；量化误差写入 profile，禁止用 store 总数代替 |
| Logs | 执行一个正常认证读请求，以响应 request_id 匹配其 Schema v1 请求日志，经过 Monitor 接收 | Backend 管理日志查询返回该 request_id 对应记录，并匹配 Envelope/存储身份 |
| Events | 顺序执行归属插件的 stop → start，保存操作前后状态与响应；只在独占空项目中按操作、插件版本、时间窗及 offset 关联真实生命周期事件 | 管理事件查询返回合法状态变更，与 Kafka Envelope 及 Elasticsearch `_id=message_id` 唯一匹配；不要求管理 API 新增字段；操作响应失败或无法唯一关联不能算通过 |

观测门禁探针与业务压测统计分开；插件操作后恢复原期望状态。每个标记 t_origin 在触发操作/
选择采集快照前记录，不在慢查询后重新计时；选择/关联标记也计入其 120 秒门禁。
业务以 t_drain 为起点。保存每个维度首次满足判据的观察时刻和单调耗时、轮询间隔、
时钟误差及其上界；观察耗时是恢复上界，不声称捕获精确完成时刻。
截止时仍未达到的维度 elapsed_to_deadline=120s、first_success=null、timed_out=true；
慢采样的结束时刻另记，不能把 123 秒采样终点再冒充恢复时间或另一维度的首次完成时刻。
缺少生成/接受/读取证据属于 incomplete；证据完整但已产生标记未可查询属于能力边界。

## 4. 验收标准与固定门禁

- 最小测试证明“业务已完成但 Event 未产生”“已产生但未可查询”“持续采集但标记已闭合”
  三种情况被正确区分；恢复时间不被另一维度超时覆盖。
- 真实有界预检证明停止、各维度恢复和下一阶梯的因果顺序；不存在后台检查跨阶梯污染。
- 同配置采样启用/停用的有界对照保留实际开销，不以 Kafka CPU 峰值单独认定其为瓶颈。
- 在同一基线候选按四阶梯各三次重复形成测量基线；先通过工具自测和预检，再执行一次基线入口。
- 发布逐阶梯事实和原因分类 product_failure / acceptance_failure / event_not_generated /
  unresolved；unresolved 可留作 02 诊断需求，不允许据此解锁 03 产品优化。

最小测试按固定 case_id 保存：D01 业务先闭合但 Event 未产生；D02 已接受标记未可查询；
D03 持续生产但选定水位已闭合；D04 同帖编辑/删除及幂等操作；D05 一个维度超时不覆盖
其他维度首次时刻；D06 收齐请求终态和归属清理后才允许下一空项目启动。
verifier 用台账/事实/标记原始记录重算这些判据，拒绝缺事件、错误 revision、重复通知、
跨阶梯来源、跨 partition 假水位及摘要布尔值替代。

本批固定命令如下；phase20 命令是待实现接口，manifest/目录是唯一运行时绑定参数：

```bash
go -C loadtest test -count=1 ./...
PYTHONPATH=scripts/ci python3 -m unittest scripts.ci.test_phase20_diagnostic scripts.ci.test_phase20_sampler scripts.ci.test_phase20_evidence
PYTHONPATH=scripts/ci python3 -m unittest scripts.ci.test_phase19_capacity scripts.ci.test_phase19_sampler scripts.ci.test_phase19_evidence
scripts/verify-phase20-diagnostic.sh --preflight --manifest <基线候选manifest> --work <预检新目录>
scripts/verify-phase20-diagnostic.sh --baseline --manifest <同候选manifest> --work <基线新目录>
python3 scripts/verify-phase20-evidence.py --diagnostic <基线目录>
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/2.2.1 --base-ref origin/main
git diff --check
```

次数、负载、窗口和阈值从 profile 读取，不接受覆盖；预检明确 formal=false。
基线三个重复的所有阶梯单元均有独立配方/ledger/marker/清理回执。

## 5. 回归与完成条件

回归负载到达/分类/报告合同、旧 Phase 19 接口、安全停止和归属清理。工具错误必须修复并
形成新工具候选后重新预检；不能把错误报告成产品容量边界。
全部 D01～D06、固定兼容门禁和证据校验通过，执行状态 complete；保存可信基线及限制、
创建同名实际日志、更新 2.2.1 并提交，
本批完成；不预先声明完整容量达标或单一产品根因。
