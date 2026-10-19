# 04. 隔离级别与 MVCC

区分四种隔离级别、RC/RR 的 Read View 行为，以及快照读和当前读在 GoPulse 中的实际用途。

## 一、八股文基础

### 1. 事务隔离级别

| 隔离级别 | 核心语义 | 典型现象 |
|---|---|---|
| Read Uncommitted（RU） | 可读取其他事务尚未提交的数据 | 可能脏读 |
| Read Committed（RC） | 不读取其他事务未提交的数据 | 同一事务两次快照读可能不同 |
| Repeatable Read（RR） | 同一事务内的普通一致性读通常复用快照 | 已有行的快照值保持稳定 |
| Serializable | 更严格地约束并发执行 | 锁冲突与等待代价更高 |

MySQL InnoDB 默认隔离级别通常是 **Repeatable Read**，但 GoPulse 的部分重要事务**显式指定 Read Committed**，不能用数据库默认值代替源码事实。

### 2. MVCC 怎么工作？

MVCC（多版本并发控制）允许普通一致性读通过行历史版本和 Read View 判断记录是否可见，而不是对每次读取都加排他锁。

理解三个基础概念：

- **Undo 版本链**：当记录被更新时，相关 Undo 信息帮助构造旧版本。
- **Read View**：描述当前读应该看见哪些事务版本的可见性边界。
- **一致性读**：普通 `SELECT` 通常依据适用的 Read View 返回可见版本。

在 InnoDB 中，RC 的一致性读通常**每条语句建立新 Read View**；RR 的一致性读通常**第一次一致性读建立 Read View，事务内后续复用**（不等于事务一开始必然创建快照）。

### 3. 快照读与当前读

| 读法 | 典型 SQL | 语义 |
|---|---|---|
| 一致性快照读 | `SELECT ...` | 根据可见版本读取，不以读取行为锁定其他事务的修改 |
| 锁定当前读 | `SELECT ... FOR UPDATE` | 获取当前符合条件的可锁定记录并加排他锁 |
| 共享锁定读 | `SELECT ... FOR SHARE` | 获取当前记录并加共享锁 |

`UPDATE`/`DELETE` 也属于需要读取当前状态并加锁的修改操作。实际锁范围仍取决于索引、隔离级别和执行计划。

## 二、GoPulse 中的实际应用

**帖子创建事务：**

```go
transaction, err := starter.BeginTx(ctx, &sql.TxOptions{
    Isolation: sql.LevelReadCommitted,
})
```

源码：[`backend/internal/post/repository.go`](https://github.com/Ray-ymq/GoPulse/blob/reset/docs-zero-base/backend/internal/post/repository.go)。

**V2 创建命令还会读取并锁定环境状态：**

```sql
SELECT data_epoch, status, mode, v1_compatibility_enabled, activation_revision, ...
FROM business_environment
WHERE singleton = 1 FOR UPDATE;
```

源码：[`backend/internal/businessstate/commands.go`](https://github.com/Ray-ymq/GoPulse/blob/reset/docs-zero-base/backend/internal/businessstate/commands.go)。省略号表示其他列，不是可直接执行的 SQL。

这里 `FOR UPDATE` 不是“更强的快照读”，而是锁定当前行。它用于确保后续命令判断与修改所依赖的状态在事务中有并发保护。

## 三、项目场景推导

考虑两个事务并发读取 `business_environment`：

```text
T1: BEGIN (RC)
T1: SELECT ... FROM business_environment WHERE singleton=1 FOR UPDATE
    -> 锁住当前环境行

T2: BEGIN (RC)
T2: UPDATE business_environment SET status='restoring' WHERE singleton=1
    -> 如果需要同一行的排他锁，可能等待 T1

T1: COMMIT
T2: 继续取得锁并执行（仍需检查自己的业务条件）
```

这个示例体现的是**行锁保护状态迁移**，不是 MVCC 快照本身；不能因为事务指定 RC 就认为不会发生锁等待。

另外，在 RC 下连续两次普通 `SELECT`，中间若有其他事务提交数据更改，第二次结果可能变化。对“读取并修改同一状态”的业务，单靠 RC 的普通快照读并不能实现互斥。

## 四、设计思考

1. **RC 与 RR 哪个更好？** 没有绝对答案。RC 更容易观察到别的事务已提交的新版本，并通常减少部分间隙锁行为；RR 提供更稳定的一致性读快照，但仍需要正确使用锁和业务约束。
2. **MVCC 能避免所有阻塞吗？** 不能。它主要优化一致性读与写的并发；锁定读、唯一键冲突、更新删除仍可能等待。
3. **RR 一定不存在幻读吗？** 不能脱离读类型下结论。RR 的一致性快照读有自身的可见性规则，当前读则涉及锁及并发插入，不能混为一谈。
4. **项目为何使用 RC？** 仓库能确认的是“明确选择 RC”；它有利于描述短事务与当前读的可见性模型，但没有基准测试或设计决策文档时，不应声称这是经过验证的性能最优解。

## 五、关联面试题

1. **RC 与 RR 最大区别？** RC 普通一致性读每条语句取新 Read View；RR 通常复用事务内第一次一致性读的快照。
2. **MVCC 是否能代替行锁？** 不能，写冲突和需要互斥的业务状态仍需要锁及唯一约束。
3. **`SELECT ... FOR UPDATE` 是快照读吗？** 不是，它是锁定当前读。
4. **Read View 何时创建？** 与隔离级别、具体一致性读语句有关，RR 并非事务开始就自动生成。
5. **为什么 GoPulse 使用 `LevelReadCommitted` 仍会阻塞？** RC 不是无锁模式；事务里的 `FOR UPDATE`、UPDATE 仍会获取锁。
