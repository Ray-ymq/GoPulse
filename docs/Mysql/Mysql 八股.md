## 一、阅读路径

| 顺序  | 主题     | 项目里能学到什么                          | 专题                                  |
| --- | ------ | --------------------------------- | ----------------------------------- |
| 1   | 表设计    | `posts` 外键、点赞联合主键、命令幂等唯一键         | [01-数据类型与表设计](01-数据类型与表设计.md)       |
| 2   | 索引     | 帖子排序索引、评论与通知联合索引、Outbox 任务索引      | [02-索引与执行计划](02-索引与执行计划.md)         |
| 3   | 事务     | `posts` + `business_outbox` 原子提交  | [03-事务与ACID](03-事务与ACID.md)         |
| 4   | MVCC   | `ReadCommitted`、快照读与 `FOR UPDATE` | [04-隔离级别与MVCC](04-隔离级别与MVCC.md)     |
| 5   | 锁      | Outbox 多 Worker 领取、租约、帖子删除时的锁     | [05-锁机制与并发控制](05-锁机制与并发控制.md)       |
| 6   | SQL 优化 | Keyset 分页、`EXISTS` 与 `COUNT`、执行计划 | [06-SQL与查询优化](06-SQL与查询优化.md)       |
| 7   | InnoDB | Buffer Pool、Undo、Redo、Binlog 的边界  | [07-InnoDB存储与日志](07-InnoDB存储与日志.md) |

**数据库表全集：** [Mysql 数据库设计](Mysql%20数据库设计.md)

## 二、表结构与数据类型

**Q01. 为什么 MySQL 业务表通常需要主键？**

主键标识记录，InnoDB 用聚簇索引组织数据；没有显式主键时可能采用其他唯一键或生成隐藏聚簇索引标识。GoPulse 的 `posts.id` 为 `BIGINT UNSIGNED AUTO_INCREMENT`。

**Q02. 主键与唯一索引的区别？**

一张表只有一个主键，可有多个唯一索引；主键列不可为 NULL，普通 UNIQUE 可以允许多条 NULL。GoPulse `business_commands` 的可空幂等键与复合 UNIQUE 必须结合这个规则理解。

**Q03. 什么是联合主键？**

多个列共同构成唯一标识。`post_likes(post_id,user_id)` 既表示关系，也阻止同一用户重复点赞同一帖子。

**Q04. `CHAR`、`VARCHAR`、`TEXT`、`JSON` 如何选择？**

按长度是否固定、查询模式、索引及结构化约束选。`event_id` 为 `CHAR(36)`，帖子正文用 `TEXT`，Outbox 载荷用 `JSON`。

**Q05. 外键和 CHECK 有什么作用？**

外键保护引用关系，CHECK 约束合法值或字段组合。`posts.author_id` 有 FK；`user_follows` 的 CHECK 拒绝自己关注自己。MySQL 8.0.16+ 才真正强制执行 CHECK。

详见：[01-数据类型与表设计](01-数据类型与表设计.md)。

## 三、索引与执行计划

**Q06. 为什么 InnoDB 使用 B+Tree？**

多路树扇出高、树高较低，叶子记录有序，兼顾等值查询、范围查询与排序。

**Q07. 什么是聚簇索引和二级索引？**

聚簇索引叶子保存行数据；二级索引叶子保存索引键及主键值，缺少需要的字段时一般需要回表。

**Q08. 什么是最左前缀原则？**

联合索引按列序组织键值，优先利用从最左列开始的连续条件；范围、排序和优化器会影响剩余部分能否利用。

**Q09. 什么是覆盖索引？**

查询需要的列能全部从所用索引获得，减少回表。`posts` 的排序索引 `(created_at,id)` 并不覆盖 `content`。

**Q10. `EXPLAIN` 重点看什么？**

看 `type`、`key`、`rows`、`Extra` 以及执行条件；`rows` 通常是估算。真实成本应使用 `EXPLAIN ANALYZE` 等证据进一步验证。

**Q11. 索引是不是越多越好？**

不是。每个索引占空间，且写入时需要维护。`business_outbox` 为待领取、租约恢复、清理分别设计索引，需要评估查询收益与写放大。

详见：[02-索引与执行计划](02-索引与执行计划.md)。

## 四、事务与 ACID

**Q12. ACID 分别是什么？**

原子性、一致性、隔离性、持久性。分别关注操作整体、约束成立、并发可见性和提交后的恢复保障。

**Q13. `COMMIT` 与 `ROLLBACK` 的作用？**

`COMMIT` 结束并提交事务，`ROLLBACK` 撤销尚未提交的修改；`Commit()` 返回错误在某些连接故障下可能意味着结果未知，不能一律判断未提交。

**Q14. 为什么 GoPulse 需要 Transactional Outbox？**

MySQL 本地事务不能直接控制 RabbitMQ 发送。项目在同一事务写 `posts` 与 `business_outbox`，提交后再异步投递。

**Q15. 使用事务就不需要幂等了吗？**

需要。并发重试仍可能创建重复业务效果，Outbox 重投也可能产生重复消费；`business_commands` 利用唯一约束和查询结果处理命令幂等。

详见：[03-事务与ACID](03-事务与ACID.md)。

## 五、隔离级别与 MVCC

**Q16. MySQL 有哪些隔离级别？**

RU、RC、RR、Serializable。InnoDB 默认常见为 RR，GoPulse 的帖子创建和 Outbox 领取显式使用 RC。

**Q17. RC 与 RR 在 MVCC 上的主要区别？**

RC 通常每条一致性读语句生成新 Read View；RR 通常在事务内复用首次一致性读的 Read View。

**Q18. 什么是 MVCC？**

通过历史版本与 Read View 规则实现一致性读，减少读写相互阻塞，但不消除写锁。

**Q19. 快照读与当前读区别？**

普通 `SELECT` 通常使用一致性快照；`SELECT ... FOR UPDATE` 是锁定当前读。GoPulse 读取 `business_environment` 时使用后者。

**Q20. RR 可以彻底避免所有幻读吗？**

不能不加限定地回答。必须分清一致性快照读、锁定读、索引范围与并发写入；不同读法遵循不同规则。

详见：[04-隔离级别与MVCC](04-隔离级别与MVCC.md)。

## 六、锁机制与并发控制

**Q21. 行锁、间隙锁、临键锁分别是什么？**

分别约束索引记录、索引区间、记录与相邻间隙；实际锁范围取决于 SQL、索引与隔离级别。

**Q22. `FOR UPDATE` 有什么作用？**

获取满足条件的当前记录的排他锁，适合“先读取当前状态，再修改”的事务流程。并不保证只锁一条记录。

**Q23. `SKIP LOCKED` 适合什么场景？**

任务领取时跳过已锁记录，减少消费者排队。GoPulse 的 `business_outbox` Worker 使用它，但因此不保证严格 FIFO。

**Q24. 有了租约为什么还需要行锁？**

行锁保证领取和更新的事务互斥；租约把领取状态持久化，用于崩溃后的超时恢复。

**Q25. 死锁如何处理？**

数据库检测后可能回滚受害事务；应用应有限重试并避免长事务、无索引范围操作和不一致的锁定顺序。

详见：[05-锁机制与并发控制](05-锁机制与并发控制.md)。

## 七、SQL 与查询优化

**Q26. 深分页为什么通常变慢？**

大 OFFSET 通常需要遍历并跳过许多有序结果；GoPulse 帖子列表使用游标分页而不是不断增大的 OFFSET。

**Q27. 为什么游标排序通常加上主键 ID？**

时间戳可能重复，`(created_at,id)` 形成确定的全序边界，防止相同时间记录被简单时间条件遗漏。

**Q28. `EXISTS` 与 `COUNT(*)` 区别是什么？**

`EXISTS` 判断有无，找到符合条件的记录即可返回；`COUNT(*)` 计算匹配数量。帖子投影同时使用二者分别服务点赞状态与互动数量。

**Q29. 如何分析慢 SQL？**

先获取真实 SQL 与参数，再看执行计划、数据分布、索引、慢日志与实测耗时，并进行正确性回归；不能只凭 SQL 文本认定优化成功。

详见：[06-SQL与查询优化](06-SQL与查询优化.md)。

## 八、InnoDB 存储与日志

**Q30. Buffer Pool 是什么？**

InnoDB 缓存数据页和索引页的内存区域；能减少磁盘读取，与应用层 Redis 缓存不是同一层。

**Q31. Undo Log 的作用？**

支持未提交修改回滚，并为 MVCC 历史版本提供信息。

**Q32. Redo Log 的作用？**

记录引擎恢复所需的页修改信息，使崩溃后能够恢复已按持久化策略提交的更改。

**Q33. Binlog 的作用？**

MySQL Server 层逻辑变更日志，常用于复制和时间点恢复。源码不能证明部署环境开启 Binlog 或主从复制。

**Q34. WAL 为什么可以减少同步刷数据页的压力？**

先满足必要日志持久化条件，再异步刷新脏页，崩溃时可利用日志恢复，不必每次提交立刻刷新全部修改页。

详见：[07-InnoDB存储与日志](07-InnoDB存储与日志.md)。

## 九、项目复盘顺序

可以依次追踪三条代码路径，把八股文串成完整的工程论证：

1. **创建帖子**：[`post/repository.go`](https://github.com/Ray-ymq/GoPulse/blob/reset/docs-zero-base/backend/internal/post/repository.go) → `posts`/`business_commands`/`business_outbox` → 事务、唯一性、提交结果未知。
2. **领取待投递事件**：[`outbox/repository.go`](https://github.com/Ray-ymq/GoPulse/blob/reset/docs-zero-base/backend/internal/outbox/repository.go) → 状态索引、RC、`FOR UPDATE SKIP LOCKED`、租约恢复。
3. **帖子列表**：[`post/repository.go`](https://github.com/Ray-ymq/GoPulse/blob/reset/docs-zero-base/backend/internal/post/repository.go) 与 [`post/pagination.go`](https://github.com/Ray-ymq/GoPulse/blob/reset/docs-zero-base/backend/internal/post/pagination.go) → 联合索引、游标、排序、`EXISTS`/`COUNT`。

未明确展示的 MySQL 能力（如分库分表、线上主从复制）只作为通用原理讨论，不能写成 GoPulse 已部署的功能。
