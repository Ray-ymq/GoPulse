# internal 目录与依赖边界

`internal` 顶层按职责组织，`modules` 内按业务功能组织。HTTP 协议处理与业务模块分包；业务模块内部用文件区分模型、流程和数据访问，保留原有事务边界。

## 目录结构

```text
internal/
├── modules/                  # 业务功能与共享业务契约
│   ├── auth/                 # 凭据验证、密码和令牌
│   ├── user/                 # 用户、资料、关注和角色管理
│   ├── post/                 # 帖子、命令、可见性和缓存接口
│   ├── comment/              # 评论
│   ├── like/                 # 点赞关系
│   ├── bookmark/             # 收藏关系
│   ├── notification/         # 通知查询与事件投影
│   ├── search/               # 搜索、索引投影和重建
│   ├── bus/                  # 事件信封、准入和处理结果契约
│   └── businessstate/        # 纪元、命令、完成记录和持久化预算
├── transport/http/           # HTTP 路由组装
│   ├── handlers/             # 按业务功能组织的 HTTP Handler
│   │   └── auth/ ... user/   # 认证、用户、帖子等八个适配包
│   ├── middleware/           # 认证、授权、请求保护和请求日志
│   ├── request/              # 请求体解析
│   ├── response/             # 响应与错误编码
│   └── params/               # 路径参数解析
├── async/
│   ├── outbox/               # 事务事件写入与可靠分发
│   └── worker/               # 消费运行、确认、重试和死信
├── platform/                 # 外部连接、健康检查和技术适配
│   ├── config/               # 配置读取与校验
│   ├── postcache/            # Redis 帖子正文缓存适配器
│   ├── ratelimit/            # 请求限流
│   └── resourcebudget/       # 进程资源预算
├── shared/
│   ├── apperror/             # 应用错误类型
│   └── correlation/          # 请求与异步处理关联信息
├── logging/                  # slog 日志与上下文
├── testkit/integrationtest/  # 集成测试环境校验和数据库锁
└── architecture_test.go      # 目录与导入约束
```

`cmd/*` 负责构造依赖、启动进程和处理退出信号。`transport/http` 负责路由组装，不承担业务规则。`logging` 与 `platform` 保留仓库约定的入口路径。

## 业务模块内的文件职责

| 文件 | 职责 |
| --- | --- |
| `model*.go` | 业务数据、输入和结果类型 |
| `validation*.go` | 输入规范化与校验 |
| `service*.go` | 业务流程、权限判定、错误转换和依赖协调 |
| `repository*.go` | 存储接口、SQL、事务及存储实现 |
| `pagination.go` 等 | 查询参数与游标规则 |
| `processor.go` | 将业务事件应用到通知或搜索投影 |
| `*_test.go` | 相邻代码的测试；真实依赖测试带 `integration` 标签 |

这些是文件职责约定，不要求每个模块都有相同数量的文件，也不将所有业务集中到全局 `service` 或 `repository` 包。存储接口和实现仍可在同一个业务包内维护。当前结构不是完全隔离存储技术的领域模型。

帖子模块按以下职责拆分数据访问：

| 文件 | 职责 |
| --- | --- |
| `repository.go` | 对外存储接口及未找到错误 |
| `repository_mysql.go` | MySQL 实现、依赖与构造函数 |
| `repository_create.go` | 普通创建流程 |
| `repository_command.go` | 幂等创建、命令结果及重放 |
| `repository_events.go` | 纪元检查、搜索义务与 Outbox 写入 |
| `repository_list.go` | 列表 SQL 与批量查询 |
| `repository_read.go` | 正文、可见性及详情投影读取 |
| `repository_hydrate.go` | 补全作者、关注和收藏数据 |
| `repository_update.go`、`repository_delete.go` | 编辑和删除事务 |

`service_command.go`、`service_update.go` 和 `service_delete.go` 保存对应业务流程。文件拆分不改变事务开始、提交、回滚和锁定顺序。

## 依赖方向

- `transport/http/handlers/*` 通过调用方定义的接口访问业务模块。Cookie 与收藏分页签名属于 HTTP 适配，分别放在认证和帖子 Handler 包。
- `modules/*` 不导入 Gin、`transport/*`、`async/worker` 或具体帖子缓存适配器。正文缓存接口归帖子模块，`platform/postcache` 实现该接口，命令入口负责注入。
- 业务事务可以调用 `async/outbox`。业务事实和 Outbox 记录必须继续使用同一事务，不能为了目录分层拆成两次提交。
- `async/*` 通过 `modules/bus` 和 `modules/businessstate` 连接业务，不导入具体业务模块。`worker` 依赖 Processor 接口；通知与搜索 Processor 由入口注入。
- `modules/bus` 不依赖其他内部包。`modules/businessstate` 只依赖事件契约，保留共同的一致性约束。
- `platform` 的通用能力不依赖具体业务。`platform/postcache` 是明确的适配器例外，只能依赖帖子业务接口。
- `shared` 和 `logging` 不依赖业务、协议入口或基础设施实现。生产代码不得依赖 `testkit`。

`architecture_test.go` 在普通模块测试中检查上述导入约束和顶层目录归属。测试不证明事务原子性，也不替代业务行为测试。

## 跨业务读取与兼容性

收藏关系写入归 `modules/bookmark`，帖子内容与可见性归 `modules/post`。收藏列表仍通过帖子查询完成。用户 Handler 通过 `Profiles`、`Posts`、`Follows` 接口协调资料、帖子和关注关系。

此次整理保留 HTTP 路径、请求与响应、事件格式、数据库结构、事务和缓存语义。Go module 名称不变，内部 import 路径已迁移。API 契约仍以 [OpenAPI](../api/openapi.yaml) 为准。

从仓库根目录执行 `make ci-check MODULE=backend/business`、`make ci-test MODULE=backend/business` 和 `make ci-build MODULE=backend/business`。真实依赖联调需另行准备隔离环境，不能以单元测试或集成测试编译结果代替。
