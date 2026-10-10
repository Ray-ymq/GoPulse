# 原生开发与验收入口登记

本文件登记当前工作区的原生入口，区分模块测试、真实依赖集成和 Compose 验收。能力状态与历史矩阵的结论见[能力状态](../status/capability-status.md)；本文件不重建已退役的阶段矩阵。

## 日常开发入口

| 能力 | 原生承接 | 实际门禁 |
| --- | --- | --- |
| 业务源码与用户端 | `devtools` 的源码进程、依赖 Compose 和 Vite | `make dev`、`make integration SCOPE=business`、`make e2e SCOPE=business` |
| 可观测源码与管理端 | `devtools` 的观测栈、Monitor 镜像和管理端 Vite | `make dev-observe`、`make integration SCOPE=observe`、`make e2e SCOPE=observe` |
| 安全停止 | `devtools` 的进程身份与 Compose 项目归属 | `make stop`、`make stack-down` |
| 栈只读核验 | `devtools/internal/stack` | `make stack-verify` |
| 构建缓存 | `devtools/internal/buildcache` | `make build-images CACHE=none|gha`、`devenv build-cache --print` |

`make deps`、`make dev`、`make dev-observe`、`make stop`、`make integration` 和 `make e2e`
只操作当前工作区归属的进程、项目、网络和测试资源。未知 scope、未归属端口、缺失依赖和子进程失败均以非零退出并保留可操作诊断；停止入口不删除命名卷。

## 模块与工具门禁

每个 Go 模块使用根 Makefile 的统一调度：`make test MODULE=<name>`、`make race MODULE=<name>`、
`make check MODULE=<name>` 和 `make build MODULE=<name>`。全量模块检查使用 `make check-all`；发布
Bundle 使用 `make package PLATFORM=linux/amd64 RUNTIME=1`，生命周期验收使用
`make verify-lifecycle INSTALL=clean|reuse`。

保留的治理工具位于 `ci/`，其 Python 实现只迁移路径而未重写规则：

```bash
python3 -m unittest discover -s ci -p 'test_*.py'
python3 ci/validate_versions.py
python3 ci/validate_branch.py --branch develop/2.5.5 --base-ref origin/main --mode development
python3 ci/quality_scope.py --changed-file acceptance/internal/scenario/compose.go --format json
```

`ci/` 的治理检查只校验版本、分支、变更范围和工作流合同。产品验收由 `acceptance` Go 模块负责，
不把治理自测当作真实依赖或浏览器验收。

## 原生验收入口

| 能力 | 入口 | 断言边界 |
| --- | --- | --- |
| 全栈 Compose 闭包 | `make verify-compose`；观测闭包使用 `SCOPE=observability` | 项目归属、网络/端口、凭据、业务/观测 Playwright、插件生命周期和清理 |
| 业务矩阵 | `make verify-business` | 注册、登录、帖子、评论、通知、搜索、权限和失败恢复 |
| 可观测专项 | `make verify-observe SCOPE=exporter|monitor|router|marshaller|logs|events` | Redis exporter、插件传输、Kafka、Marshaller、日志和事件链路 |
| 插件与组件 | `make verify-plugins` | 六类插件、组件指标、账号解和持久状态 |
| 告警 | `make verify-alerts` | 告警目录、规则、评估状态、历史、租约和恢复 |
| 角色 | `make verify-roles` | 管理员角色、降级和会话边界 |
| 管理页面 | `make verify-pages` | dashboard、Metrics/Logs/Events、插件页面和响应式浏览器 |
| 产品生命周期 | `make verify-lifecycle INSTALL=clean|reuse` | Bundle 安装、只读 verify、HTTP edge、重复启停、恢复身份和清理 |

上述入口均由 `acceptance` Go 模块编排，使用带归属的随机项目和临时凭据；默认结束时只清理本次资源。
`make stack-up`、`make stack-verify`、`make stack-down` 是日常 Compose 栈入口，down 保留命名卷。

失败注入使用 `make verify-lifecycle FAILURE_MATRIX=1`。生命周期安装的失败矩阵覆盖锁、无守护进程、
Bundle 篡改、磁盘、端口、权限、错误确认、外部卷、信号和依赖未就绪；它与正常安装回执分开记录。

## 前端与真实依赖

用户端和管理端的本地检查仍使用各自 package 文件声明的 `npm ci`、`npm test`、`npm run typecheck`
和 `npm run build`。真实业务浏览器使用 `make e2e SCOPE=business`，管理与观测浏览器使用
`make e2e SCOPE=observe`；完整页面验收使用 `make verify-pages`。

业务注册、Outbox、RabbitMQ、通知、搜索、告警状态、角色和审计由 `make integration SCOPE=business`
覆盖；Redis exporter → Monitor → Router → Kafka → Marshaller → 查询 API 和观测权限由
`make integration SCOPE=observe` 覆盖。迁移 CLI 的状态、并发锁、dirty/ahead、恢复、退出码和脱敏
由 Backend 集成用例覆盖，并由业务 integration scope 纳入。

## CI 选择

`ci/quality_scope.py` 使用目标分支与主线共同祖先的完整 diff，按路径选择模块、集成、浏览器、Compose
和工具门禁。空 diff 与文档-only diff 只运行治理检查；`acceptance/**` 选择 acceptance、Compose、
业务/观测 integration、业务/观测 e2e 与 tools；`devtools/**` 选择本机环境及两类 integration/e2e；
未知产品路径保守选择完整产品检查。

`.github/workflows/quality-gates.yml` 的产品 job 统一调用 Make 目标。治理 job 只调用 `ci/` Python
工具；Compose job 调用 `make verify-compose`；原生验收 job 运行 `make check MODULE=acceptance`、
`make test MODULE=acceptance` 和 Compose 渲染断言。

Phase 16–20 的正式容量、恢复、长期评估、运行时矩阵、交付和证据执行器已按能力状态退役。历史文档、
实施日志和原始回执保留原路径；新的产品能力只由本文件列出的原生入口或相应实施计划声明。
