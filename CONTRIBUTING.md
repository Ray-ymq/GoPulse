# GoPulse 贡献说明

提交说明、PR 描述和自动校验规则统一维护在[CI 与提交规范](tools/ci/CI.md#提交规范)。创建提交或 PR 前请阅读该规范，并使用仓库提供的模板。

- 每项完整改动单独提交，标题遵循 Conventional Commits。
- 提交正文包含非空的背景、改动和验证；PR 正文还需填写影响与风险。
- 验证结果按实际执行情况填写，未执行的检查不得记录为通过。
- 产品版本以根目录 `VERSION` 为准。

项目通过源码交付。安装 Go、Node.js、Python、Git 和 Make 后，执行 `make help` 查看构建与验证入口。前端依赖通过 `make -C frontend/web install` 安装。部署配置、本地开发编排和压测工具已移除，运行环境需自行准备。

## 目录与模块入口

业务后端位于 `backend/business/`，共享运行时能力放在 `backend/runtimekit/`。用户前端位于 `frontend/web/`，启动与验证步骤见[前端说明](frontend/web/README.md)。CI 和共享构建规则位于 `tools/`。完整目录说明见 [AGENTS.md](AGENTS.md#项目结构)。

`MODULE` 和 `ci:<模块目录>` 标签使用迁移后的完整目录，例如：

```sh
make test MODULE=backend/business
make build MODULE=backend/runtimekit
make ci-plan LABELS="ci:backend/business ci:backend/runtimekit"
```

模块目录调整不改变 Go module 名称、import 路径。旧模块目录参数和旧 CI 标签需要改为新路径。PR 按完整差异自动选择模块，并在检查通过后同步 `ci:*` 标签；所需远端标签由流程按需创建。手动运行仍通过模块标签指定检查范围。根目录保留模块构建、测试和 CI 入口。
