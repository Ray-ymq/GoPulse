# GoPulse 贡献说明

提交说明、PR 描述和自动校验规则统一维护在[CI 与提交规范](docs/CI.md#提交规范)。创建提交或 PR 前请阅读该规范，并使用仓库提供的模板。

- 每项完整改动单独提交，标题遵循 Conventional Commits。
- 提交正文包含非空的背景、改动和验证；PR 正文还需填写影响与风险。
- 验证结果按实际执行情况填写，未执行的检查不得记录为通过。
- 产品版本以根目录 `VERSION` 为准。

项目通过源码交付。接收方安装 Go、Node.js 和 Docker 后，在仓库根目录执行 `make doctor` 检查环境、`make bootstrap` 安装开发依赖，再执行 `make dev` 启动开发环境。需要监控和管理前端时使用 `make dev-observe`；结束后执行 `make stop`。可用命令见 `make help`。

仓库不再提供产品发布包、独立安装管理工具及其备份恢复命令。`make build` 和 `make stack-up` 等源码构建、容器运行命令继续保留。
