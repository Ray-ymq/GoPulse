# GoPulse 贡献说明

提交说明、PR 描述和自动校验规则统一维护在[CI 与提交规范](../tools/ci/docs/CI.md#提交规范)。创建提交或 PR 前请阅读该规范，并使用仓库提供的模板。

- 每项完整改动单独提交，标题遵循 Conventional Commits。
- 提交正文包含非空的背景、改动和验证；PR 正文还需填写影响与风险。
- 验证结果按实际执行情况填写，未执行的检查不得记录为通过。
- 产品版本以根目录 `VERSION` 为准。

项目通过源码交付。接收方安装 Go、Node.js 和 Docker 后，在仓库根目录执行 `make doctor` 检查环境、`make bootstrap` 安装开发依赖，再执行 `make dev` 启动开发环境。需要监控和管理前端时使用 `make dev-observe`；结束后执行 `make stop`。可用命令见 `make help`。

仓库不再提供产品发布包、独立安装管理工具及其备份恢复命令。`make build` 和 `make stack-up` 等源码构建、容器运行命令继续保留。

Exporter 由 Monitor 作为二进制插件启动，插件包由 `tools/devtools/` 从 `backend/observability/exporters/` 源码构建；不再提供独立 Exporter 容器服务和镜像构建入口。

## 目录与模块入口

后端按系统分为 `backend/business/` 和 `backend/observability/`，共享运行时能力放在 `backend/runtimekit/`。前端位于 `frontend/`，开发、压测、CI 和共享构建规则位于 `tools/`。完整目录说明见 [AGENTS.md](../AGENTS.md#项目结构)。

`MODULE` 和 `ci:<模块目录>` 标签使用迁移后的完整目录，例如：

```sh
make test MODULE=backend/business
make build MODULE=backend/observability/monitor
make test MODULE=tools/devtools
make ci-plan LABELS="ci:backend/business ci:backend/observability/monitor"
```

模块目录调整不改变 Go module 名称、import 路径和 Compose 服务名。旧模块目录参数和旧 CI 标签需要改为新路径；远端 CI 标签由仓库维护者创建。根目录的开发、构建和部署命令继续使用原有入口。

压测方案与报告统一放在 `docs/performance/`；工具与配置放在 `tools/perflab/`。

Compose 部署入口位于 `deploy/compose/`，开发和调试配置位于 `deploy/dev/`。目录说明、部署命令和旧路径迁移要求见[部署说明](../deploy/README.md)。

公共压测入口、准备步骤和专用环境要求见[压测操作说明](../tools/perflab/flow/README.md)。历史测试方案和报告见[压测文档](performance/README.md)。原生 node_exporter 的打包与部署要求见[插件说明](../backend/observability/exporters/node/README.md)。
