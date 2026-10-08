# Phase-22-01：本机启动与依赖隔离

> 实际状态：已完成，目标版本 `2.4.1`，分支 `develop/2.4.1`。

## 实际完成

- 增加本机开发生命周期命令：`dev`、`dev-observe`、`test`、`stop`、`monitor-image`，并保留 `integration` 与 `e2e` 的后续批次边界。
- 增加标准库实现的本机开发编排器，支持 dotenv 合并、工作区身份、独立 Compose project、端口与依赖隔离、进程组生命周期、PID 身份校验、重复启动幂等、停止清理和状态记录。
- 增加 Linux 监控覆盖 Compose 文件；监控镜像只构建现有 `monitor` target，并按输入摘要复用镜像。
- 将本批次版本元数据同步为 `2.4.1`。
- 通过隔离端口完成 `dev` 首次启动、重复启动和停止验收；通过 `dev-observe` 完成 Router、Marshaller、业务进程、前端、管理前端和 Monitor 联合启动验收，健康端点均返回 200；停止后保留命名依赖卷。

## 实际变更文件

- `Makefile`
- `scripts/ci/local_development.py`
- `scripts/ci/test_local_development.py`
- `deploy/compose.local.yaml`
- `deploy/compose.local-linux.yaml`
- `VERSION`
- `.env.example`
- `frontend/package.json`
- `frontend/package-lock.json`
- `admin-frontend/package.json`
- `admin-frontend/package-lock.json`
- `dev/logs/Phase-22/Phase-22-01-本机启动与依赖隔离.md`

## 实际命令与结果

- `git fetch upstream`：通过；在独立 worktree 中从 `upstream/main` 创建 `develop/2.4.1`。
- `python3 -m py_compile scripts/ci/local_development.py scripts/ci/test_local_development.py`：通过。
- `python3 -m unittest discover -s scripts/ci -p test_local_development.py`：通过，9 个测试通过。
- `make help`：通过。
- `make -n dev dev-observe test MODULE=backend stop monitor-image`：通过。
- `docker compose -f deploy/compose.local.yaml config --quiet`：通过。
- `docker compose -f deploy/compose.local.yaml -f deploy/compose.local-linux.yaml config --quiet`：通过。
- `make test MODULE=backend`：通过。
- `python3 scripts/ci/validate_versions.py`：通过。
- `python3 scripts/ci/validate_branch.py --branch develop/2.4.1 --base-ref upstream/main`：通过。
- `GOPULSE_ENV_FILE=.run/local/phase22-01/dev-alt.env make dev`：通过；重复执行返回 `dev is already running`，`make stop` 通过并保留四个命名依赖卷。
- `make monitor-image`：通过；在版本元数据更新后重新构建，最终镜像标签为 `gopulse/monitor:local-a7f38c18a39fb86a`，镜像版本为 `2.4.1`。
- `GOPULSE_ENV_FILE=.run/local/phase22-01/alt.env make dev-observe`：通过；最终后端、前端、Router、Marshaller、Monitor、管理前端状态码均为 200，状态摘要与运行时摘要一致；随后 `make stop` 通过并保留命名依赖卷。
- `make integration`、`make e2e`：按本批次边界显式失败并提示保留给后续 Phase-22 批次。
- `git diff --check`：通过。

## 偏差、限制与后续项

- 当前执行环境的默认 `127.0.0.1:3306` 已被占用；两次默认 `make dev` 均在 Compose 绑定阶段安全失败，未触碰既有进程。运行验收使用调用方环境文件切换到隔离端口，端口冲突的 fail-closed 行为已验证。
- `integration`、`e2e`、L07 和 L08 属于后续拆分批次，本批次未实现；对应入口已保持明确的后续批次失败语义。
- 本批次只构建 Monitor 镜像，没有构建或推送业务镜像。
