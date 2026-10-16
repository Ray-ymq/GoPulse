# 03. 事务与 ACID

> 学习目标：解释事务的 ACID、提交/回滚和本地事务边界，理解 GoPulse 的 Transactional Outbox 如何减少业务数据与异步事件不一致的问题。

## 一、八股文基础

**什么是事务？** 一组数据库操作作为一个工作单元提交或回滚。在 InnoDB 中，事务需要考虑四种特性：

| 特性 | 含义 | 主要机制/约束 |
|---|---|---|
| 原子性 Atomicity | 同一事务中的修改作为整体成功或撤销 | Undo Log、事务管理 |
| 一致性 Consistency | 事务前后满足业务规则和数据库约束 | PK / UNIQUE / FK / CHECK、应用逻辑 |
| 隔离性 Isolation | 并发事务按照隔离级别相互隔离 | 锁、MVCC |
| 持久性 Durability | 成功提交后的修改具有持久性保障 | Redo Log 及相关刷盘策略 |

**注意：** ACID 中的一致性不等于“缓存与数据库始终同步”，也不等于“数据库和 RabbitMQ 组成同一事务”。

## 二、GoPulse 中的实际应用

**场景：创建帖子后需要发布业务事件。**

```text
用户创建帖子
    |
    v
BeginTx(ReadCommitted)
    |
    +-- INSERT posts
    |
    +-- INSERT business_outbox
    |
    +-- （V2 命令路径还会写入幂等命令、消费要求等记录）
    |
    v
Commit
    |
    +-- 成功：帖子和本次事务内的事件记录一起提交
    +-- 提交前失败：整个事务回滚
    |
    v
Outbox 异步投递 RabbitMQ
```

如果先保存帖子、再直接发送 RabbitMQ，可能出现“帖子已保存但消息没送达”或“消息已发但 MySQL 事务失败”。MySQL 本地事务**不能**把 RabbitMQ 发送纳入自身的原子提交。项目因此将“待发送事件”先写入同一个数据库，再异步投递。

## 三、项目源码分析

项目位置：[`backend/internal/post/repository.go`](https://github.com/Ray-ymq/GoPulse/blob/reset/docs-zero-base/backend/internal/post/repository.go) 与 [`backend/internal/outbox/repository.go`](https://github.com/Ray-ymq/GoPulse/blob/reset/docs-zero-base/backend/internal/outbox/repository.go)。

下面是基于仓库 `Create` 的**简化流程**，省略 tracing、错误返回以及事件初始化：

```go
transaction, err := starter.BeginTx(ctx, &sql.TxOptions{
    Isolation: sql.LevelReadCommitted,
})
if err != nil { return Post{}, err }
defer transaction.Rollback()

record, err := createPost(ctx, transaction, authorID, title, content)
if err != nil { return Post{}, err }

event, err := bus.NewPostCreated(repository.clock().UTC(), authorID, record.ID)
if err != nil { return Post{}, err }

_, err = insertOutbox(ctx, transaction, repository.outbox, event)
if err != nil { return Post{}, err }

if err := transaction.Commit(); err != nil { return Post{}, err }
```

关键是 `createPost` 和 `insertOutbox` 都通过 **同一个 `*sql.Tx`** 执行。如果 Outbox 插入失败，延迟执行的 `Rollback` 会撤销尚未提交的帖子写入。

V2 命令路径 `CreateWithCommand` 还在同一事务中调用 `LockCurrentTx`、`FindCommandTx` 和 `ReservePostCreateTx`，把环境状态、业务事实、幂等命令及事件要求纳入事务边界。

**幂等与提交结果未知：** V2 路径对唯一键竞争会回滚失败一方，再读取已提交的赢家；当 `Commit()` 返回错误时，也可能无法仅凭返回值判断数据库是否已经提交，因此代码会在具备幂等键时查询持久化命令结果。不要把“Commit 返回错误”简单等价于“数据库必然未提交”。

## 四、设计思考

### 1. `defer Rollback()` 在提交成功后会怎样？

提交成功后事务已经结束，再调用 `Rollback()` 通常返回 `sql.ErrTxDone`；这里的延迟回滚是清理保护，不会撤销已成功提交的数据。

### 2. Outbox 能保证消息只发一次吗？

不能。典型故障是：RabbitMQ 实际已接收消息，但投递者在更新 Outbox 状态前崩溃，下一次可能再次发送。因此它通常提供**至少一次投递**的实现基础，消费端还需要事件去重或幂等应用。

### 3. 事务是不是越大越安全？

不是。事务持有锁、Undo 历史版本和连接资源；事务越长，锁等待和清理压力往往越大。不要把网络请求、消息投递等不可控外部操作简单包含在数据库事务中。

### 4. 为什么还需要数据库唯一约束？

事务只能控制同一工作单元的提交/回滚，并不能自动阻止两个并发请求各自插入一条“逻辑重复”的数据。`business_commands` 的联合唯一键承担最终竞态仲裁。

## 五、关联面试题

1. **ACID 四个特性分别是什么？** 原子性、规则一致性、隔离性、持久性，不能相互替代。
2. **Undo Log 和 Redo Log 分别保障什么？** Undo 帮助回滚并支持历史版本；Redo 记录物理页修改以恢复已提交变更。
3. **为什么创建帖子与写 Outbox 要在同一事务？** 避免业务事实与待投递事件出现一边成功一边失败的状态。
4. **MySQL 事务能控制 RabbitMQ 消息提交吗？** 普通 MySQL 本地事务不能；Outbox 用持久化事件和异步投递衔接两者。
5. **事务提交失败就必然没写入吗？** 不一定，通信故障下可能存在提交结果未知，需要幂等键和数据库证据确认。
