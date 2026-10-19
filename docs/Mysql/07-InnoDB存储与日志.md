# 07. InnoDB 存储与日志

> 学习目标：理解 Buffer Pool、Undo Log、Redo Log、Binlog 的职责，能够从存储引擎角度解释 GoPulse 的事务持久化和故障恢复边界。

## 一、八股文基础

### 1. InnoDB 的数据怎么组织？

InnoDB 以页作为核心的磁盘与缓冲管理单位，表数据通过聚簇索引组织。默认常见页大小为 16 KiB，但可以配置，不能把 16 KiB 当成所有实例必然不变的值。

**Buffer Pool** 缓存经常访问的数据页和索引页，读请求命中缓存时可以减少磁盘读；修改数据时，内存中的页可能先成为脏页，之后再刷写到表空间文件。

### 2. Undo、Redo、Binlog 的职责

| 机制 | 所属层次 | 关键用途 | 能否直接替代另一个日志 |
|---|---|---|---|
| Undo Log | InnoDB | 回滚未提交修改、构造 MVCC 历史版本 | 不能代替 Redo |
| Redo Log | InnoDB | 记录可用于崩溃恢复的页修改信息；支撑持久性 | 不能代替 Undo |
| Binlog | MySQL Server 层 | 记录逻辑变更事件，常用于复制及增量恢复 | 不能代替 Redo |

理解事务提交流程，关键不在于“把整张表立即刷到磁盘”，而在于日志持久化与崩溃恢复协议。对于开启 Binlog 的 InnoDB 事务，还存在两阶段提交协调机制，以减小两种日志提交状态不一致的风险。

## 二、GoPulse 中的实际应用

GoPulse 使用 InnoDB：建表迁移显式声明 `ENGINE=InnoDB`，或在相关表中使用该引擎。帖子、评论、Outbox、命令证据、告警状态等最终都是 MySQL 表，而不只是应用内存里的对象。

创建帖子时存在这样的操作路径：

```text
INSERT INTO posts
        +
INSERT INTO business_outbox
        |
        v
  InnoDB 本地事务
        |
        v
     COMMIT
        |
        +-- 逻辑上一起提交
        +-- 物理持久性依赖日志/刷盘配置
```

即使 `posts` 的数据页尚在 Buffer Pool 中处于脏页状态，InnoDB 仍可使用 Redo 进行崩溃恢复。对于提交后的耐久保障，还必须结合实例的 `innodb_flush_log_at_trx_commit`、底层存储和（如启用）`sync_binlog` 等配置判断，不能只凭应用调用 `Commit()` 就推断任何硬件断电场景下都绝对不丢数据。

## 三、项目源码分析

**1. 存储引擎声明**

[`backend/migrations/000001_phase1_schema.up.sql`](https://github.com/Ray-ymq/GoPulse/blob/reset/docs-zero-base/backend/migrations/000001_phase1_schema.up.sql)：

```sql
CREATE TABLE posts (
    id BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    author_id BIGINT UNSIGNED NOT NULL,
    ...
    PRIMARY KEY (id)
) ENGINE=InnoDB DEFAULT CHARACTER SET utf8mb4
  COLLATE utf8mb4_0900_ai_ci;
```

上面为概念摘录，`...` 省略真实字段与约束，实际建表应以迁移文件为准。

**2. 事务入口**

[`backend/internal/post/repository.go`](https://github.com/Ray-ymq/GoPulse/blob/reset/docs-zero-base/backend/internal/post/repository.go) 使用 `BeginTx`、`ExecContext`、`Commit`，由 MySQL/InnoDB 处理日志记录与崩溃恢复。应用代码本身不会显式调用 `WriteRedoLog()` 或 `WriteUndoLog()`。

**3. 连接配置**

[`backend/internal/platform/mysql.go`](https://github.com/Ray-ymq/GoPulse/blob/reset/docs-zero-base/backend/internal/platform/mysql.go) 显式设置最大连接数、空闲连接数和连接生命周期，并使用 `ParseTime=true`、`Loc=time.UTC`。这是 Go 数据库连接层的配置，**不是** InnoDB 的 Buffer Pool、Redo 刷盘或 Binlog 参数。

## 四、将八股文映射到故障场景

| 场景 | 主要相关机制 | 不能误解为 |
|---|---|---|
| SQL 在提交前失败，需要撤销修改 | Undo + 事务回滚 | 应用手工删除刚插入的所有行 |
| 普通查询看到旧版本 | Undo 版本链 + Read View | 每次读都需要加行锁 |
| 已提交事务后 MySQL 进程崩溃 | Redo + 恢复流程 | 每次 COMMIT 都已将所有数据页刷盘 |
| 主从复制或按时间点恢复 | Binlog（需实例实际启用/保留） | GoPulse 已经部署主从复制 |
| 热点数据反复读取 | Buffer Pool | Redis 就是 InnoDB Buffer Pool |

**项目事实边界：** 迁移与源码可以证明 GoPulse 使用 InnoDB，本次分支核对不能证明某一运行环境实际开启了 Binlog、主从复制或特定刷盘参数。这些都需要实例配置与运行时证据。

## 五、设计思考

1. **为什么数据库不能每次修改都立即刷完整数据页？** 随机写成本高；WAL（Write-Ahead Logging）允许先使必要日志达到持久化要求，再异步刷新脏页。
2. **为什么 Undo 和 Redo 必须区分？** Undo 面向修改撤销和版本可见性，Redo 面向崩溃后重做已记录的页变更。
3. **Binlog 能直接代替 Redo 吗？** 不能，它们处在不同层次，记录目的和恢复能力不同。
4. **什么是刷盘策略权衡？** 较强持久性配置通常伴随更多同步 I/O 成本；必须结合故障模型和 SLA 判断。
5. **为什么不能把 Redis 命中率当作 Buffer Pool 命中率？** Redis 属于应用层缓存，Buffer Pool 是 InnoDB 引擎内部缓存，管理对象和一致性机制不同。

## 六、关联面试题

1. **Undo Log、Redo Log、Binlog 有什么区别？** 分别侧重撤销/版本、引擎崩溃恢复、逻辑变更/复制。
2. **MySQL 提交事务时必须先把数据页刷盘吗？** 不必；WAL 通过必要日志保障恢复能力。
3. **Buffer Pool 有什么作用？** 缓存数据/索引页，减少 I/O，维护脏页并按机制刷盘。
4. **为什么 Redo Log 可以支持崩溃恢复？** 它保存用于重做持久化页修改的必要日志信息。
5. **为什么不能仅根据 Go 代码判断 Binlog 已启用？** Binlog 是 MySQL 实例级功能，需要实际配置和服务器状态证据。
