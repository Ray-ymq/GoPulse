# Phase 20 容量诊断方法

本批修订验收基础设施。被测产品基线为 `2.1.4`，实施完成版本为 `2.2.1`；两者不能互换。
Phase 19 的 profile、原始回执、执行状态和容量边界保持历史事实。

## 输入与归属

`loadtest/phase20-capacity-profile.json` 保留原配方、业务比例、1,024 虚拟用户、
50/100/150/200 RPS、15 秒预热、60 秒测量和三个重复。正式运行是十二个独立阶梯项目，
每个项目重新物化配方、重建索引、完成初始化收敛，再执行本阶梯。预检不是正式重复。

候选 manifest 绑定产品版本、源码 revision、自研镜像与依赖镜像的不可变本地 image ID。
本地 image ID 是此 Docker 引擎中的内容寻址证据，不能当作跨宿主可拉取的发布 Bundle。
独立记录验收工具源码字节 digest、profile 原始字节 digest、recipe 与原始记录 digest。
产品候选镜像不会在修复验收工具时重标记或替换。工具变更使其受影响的预检证据失效。

每个项目必须在归属检查后才能使用或清理，只执行该项目的 `down --volumes --remove-orphans`。
恢复检查和采样线程都退出后才允许清理；清理成功后才启动下一阶梯。
不进行全局 Docker 清理，不携带上一阶梯的数据卷或消息积压。

## 请求与事实

Phase 20 负载报告使用 `gopulse.phase20.load.v1`，不改写 Phase 19 报告语义。
到达台账逐槽保存到达与实际终态；未派发槽明确记为 dropped，缺终态明确记为 incomplete。
终态包含 actor、路由模板、对象键、operation_id、响应 ID/revision、request_id、
调度/发送/完成时刻及内容摘要。发布时不能包含密码、cookie、业务正文或私有业务标识。
新 profile 按实际公开接口把关注/取消关注成功码固定为 200；旧 profile 的 204 保留，不事后改写历史结论。
登录及读请求不产生推断业务事件。超时不是已确认接受，也不等于确定未提交。

停止调度与排空分别记录 `t_stop`、`t_drain`，排空最多 30 秒。排空后取得同一
REPEATABLE READ 一致性快照，保存帖子、评论、关系和本阶梯新事件集合。
Outbox ID 范围只用于发现新集合，不能用于推断并发事务顺序或代替对象/revision 关联。

新帖、评论和编辑核对响应 ID/revision、最终事实与实际 Outbox Envelope。
同一对象可以用事实组关联多条幂等请求；实际事件必须全部归属，不要求每条幂等请求产生事件。
关系按同一 actor 的实际发送顺序、操作前状态和最终事实核对；不按跨请求响应到达顺序猜测事务顺序。
编辑以最终 content_revision 和内容摘要核对；删除以 MySQL 与搜索投影均不存在核对。
所有相关实际事件必须 published。通知按 source_event_id 核对唯一性、类型、actor、recipient，
并核对删除对象的 tombstone；自操作不要求通知。搜索使用真实查询结果，而非写入 ack。
快照与确认接受的响应或持久不变量矛盾时，记录 product_failure。

## 四个恢复维度

业务以 `t_drain` 为起点；Metrics、Logs、Events 各自以选择/触发标记前的 `t_origin` 为起点。
每个维度独立拥有 120 秒截止和首次观察达标时刻，慢探针的结束时刻单独保存。
观察耗时是恢复完成时间的上界。超时项 first_success 为 null，elapsed_to_deadline 为 120 秒。
某个维度超时不能覆盖其他维度已经记录的首次达标时刻。

- Metrics：选择排空后 Monitor 正常采集的 Backend 快照，绑定 Envelope、series、值、源时刻、
  partition/offset；VictoriaMetrics 必须查询到相同值和量化时刻（1ms 误差）。
- Logs：真实认证读请求的响应 request_id 与请求完成日志、Kafka Envelope、ES 文档和管理查询唯一匹配。
- Events：真实归属插件 stop → start，保存前后状态和成功响应，并恢复原期望状态。生命周期事件的
  插件、版本、操作、状态变化及时间窗必须唯一匹配 Kafka Envelope。管理查询返回的记录与
  Elasticsearch `_id=message_id` 唯一匹配；现有管理 API 不新增 message_id 字段。

Kafka offset 是按 partition 的集合。持续生产时不要求整个消费组瞬时 lag=0。
没有产生或接受证据属于 incomplete；完整证据中的已产生标记尚未可查询属于能力边界。
Logs 管理查询仅比较公开 Entry 字段；Envelope 中的构建/运行字段仍与原始 ES 文档完整核对，
不能因为管理接口未公开这些字段就把已可查询日志判为失败。
所有探针与业务负载统计分开，不能把验收插件操作计作业务写入。

单进程耗时使用 monotonic 时钟；跨 Go/Python 的 `t_drain` 使用同宿主 UTC 与单调时钟映射，
profile 记录 10ms 时钟误差前提。实际时钟偏差或跳变超出前提时不能声明证据完整。
业务 ID 和 marker_id 不注入产品指标标签。

## 观察者开销与环境

验收工具使用外置、固定版本 `kafka-python==2.2.15` 长驻客户端；手动分配 partition，
禁止自动提交 offset、创建 topic 或加入产品消费组的动态成员分配。每个线程独占客户端。
Docker broker 主机名只在验收进程中映射到当前归属项目的 IP，不修改宿主 hosts/DNS。
该客户端的手动分配行为见 [KafkaConsumer 官方文档](https://kafka-python.readthedocs.io/en/2.2.15/apidoc/KafkaConsumer.html)。

Kafka CLI 的一次实际耗时在负载窗口外保存；持续采样保存开始/结束、调度滞后、逐探针耗时、
失败和缺失信号，采样调度滞后不能命名为负载调度滞后。资源包括宿主 CPU/内存、负载进程
CPU/RSS、容器 CPU/RSS、组件指标、Outbox、RabbitMQ 队列和按分区的 Kafka 水位。
同宿主、同一认证读请求与到达配方分别执行采样关闭/启用的有界对照，保留每条请求和原始
资源记录。它只说明有限窗口的观察者成本，不能凭 Kafka CPU 峰值认定产品根因。

在仓库外安装验收依赖后设置 PYTHONPATH，例如：

```bash
python3 -m pip install --target /tmp/gopulse-phase20-tooldeps kafka-python==2.2.15 python-snappy==0.7.3 cramjam==2.11.0
PYTHONPATH=/tmp/gopulse-phase20-tooldeps scripts/verify-phase20-diagnostic.sh \
  --preflight --manifest <候选manifest> --work <新预检目录>
```

机器必须满足冻结 profile 的宿主下限，并且没有竞争的 Compose 项目。
无法构造安全归属、真实依赖或完整证据时不启动正式矩阵。

## 状态与原因

execution_status 与 capability_status 分开。工具/证据/归属缺失属于 incomplete，不能冒充
boundary_found。完整执行的同步门禁、业务终态及可查询时限决定 target_met 或 boundary_found。
逐阶梯记录 product_failure、acceptance_failure、event_not_generated 或 unresolved。
相关现象、已证明因果与未解决项分别说明；unresolved 不能据此解锁 Phase-20-03 产品优化。

本文件不预先宣称可信基线、容量达标或任何单一产品根因；实际结果以实施日志与经过严格
校验的选定脱敏工件为准。私有原始证据保留在仓库外，发布摘要保留其来源 digest 与候选绑定。

发布入口使用 `--publication dev/logs/Phase-20/Phase-20-01-evidence`，先重算私有基线，再严格比较
公开摘要、固定文件清单与来源 digest；额外文件或任何内容差异都使校验失败。观测 Elasticsearch
在每个单元负载前及恢复后记录真实 docs/store 大小，供后续生命周期批次参考；短窗口增量包含
索引与压缩行为，不外推多日增长或保留期。

## 本批实际结果

最终工具候选通过 `preflight-r10`，正式入口在 `baseline-r2` 调用一次，完成十二个独立单元；
原始重算和选定脱敏发布校验结果为 `complete / target_met`。此前工具候选的预检及首次正式
入口失败均保留，失败记录已无损归档，不复用其通过单元作为最终候选的重复。
四个阶梯三次 achieved RPS 均等于目标；P95 最大 50.69ms，P99 最大 220.93ms，
负载调度滞后最大 62.46ms，最长排空约 0.017 秒，最长独立恢复观察上界 44.97 秒。
采样启用/停用各执行 250 次同配方读取：P95 分别 23.14/19.85ms，验收进程 CPU 时间
分别 3.656/3.259 秒。此单次五秒对照量化有限窗口开销，不证明产品瓶颈或统计显著收益。
逐阶梯原值、中位数、范围、CV 和数据增长见
[选定脱敏基线](../dev/logs/Phase-20/Phase-20-01-evidence/baseline.json)。
新 profile、逐阶梯隔离、独立恢复和采样方式同时变化，本结果不构成产品优化 A/B，
不能单凭本次通过定位旧超时的单一根因。Trace、生命周期、预算和持续运行仍待后续批次。
