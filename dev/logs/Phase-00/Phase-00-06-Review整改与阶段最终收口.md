# Phase-00-06：Review 整改与阶段最终收口开发记录

## 1. 执行信息

- 日期：2026-09-01
- 分支：`develop/0.1.6`
- 目标版本：`0.1.6`
- 依据：`dev/review/2026-09-01-GoPulse项目Review报告.md`
- 执行范围：关闭 Phase 0 Review 的 P1-01 至 P1-04，并完成直接相关的仓库卫生与自动门禁

## 2. 实际完成工作

1. 新增 `.gitattributes`，强制 `*.sh` 与 `*.go` 使用 LF；将当前工作区对应文件恢复为 LF，Windows `core.autocrlf=true` 下的 Bash 语法和 Go 格式检查恢复可信。
2. 新增可复用 GitHub Actions 质量门禁与 PR CI，自动执行：
   - Backend 格式、测试、vet 和 race test；
   - Frontend 测试、类型检查与 production build；
   - Linux Bash 语法与 LF 检查；
   - Windows PowerShell AST 解析；
   - Compose 解析与四个发布端口的回环绑定断言。
3. 新增 `scripts/ci/validate_branch.py` 及 7 项单元测试，落实：
   - 只接受 `develop/x.x.x` 或 `update`；
   - `develop/x.x.x` 必须唯一映射到 Phase 总实施方案，且根 `VERSION` 与分支版本一致；
   - `update` 只能修改规划、架构、文档、工作流和仓库规则范围，明确拒绝 Backend、Frontend 与 `VERSION` 变更。
4. 调整自动 PR/merge 工作流：质量门禁成功后才创建或复用 PR，并只启用 auto-merge；删除“先立即合并，失败后再启用 auto-merge”的路径。`update` 继续使用 merge commit，普通开发分支继续使用 squash 并删除分支。
5. 新增 Phase-00-06 权威分配。`develop/0.1.6` 现唯一映射到 `Phase-00-06`，根 `VERSION` 更新为 `0.1.6`。
6. Compose 的 MySQL、Redis、RabbitMQ AMQP 与管理端口默认通过 `PUBLISHED_HOST=127.0.0.1` 发布；Backend 默认 `HTTP_HOST` 改为 `127.0.0.1`。需要远程访问时必须在本地配置中显式选择非回环地址。
7. 更新 `.env.example`、跨平台 `dev` 脚本默认值、Backend 配置测试与 README；删除已被 `.gitignore` 忽略但仍受跟踪的 `.DS_Store`，并忽略 Python 测试缓存。
8. 在 Review 报告追加整改结果，保留原始发现和证据，同时将 Phase 0 最终判定记录为通过。

## 3. 实际变更文件

- 工程与配置：`.gitattributes`、`.gitignore`、`.env.example`、`deploy/compose.yaml`、`VERSION`
- GitHub Actions：`.github/workflows/quality-gates.yml`、`.github/workflows/ci.yml`、`.github/workflows/auto-pr-merge.yml`
- 治理实现与测试：`scripts/ci/validate_branch.py`、`scripts/ci/test_validate_branch.py`
- 本地脚本：`scripts/dev.ps1`、`scripts/dev.sh`
- Backend：`backend/internal/config/config.go`、`backend/internal/config/config_test.go`
- 文档与记录：`README.md`、Phase 0 总实施方案、本批实施方案、本记录、Review 报告
- 删除：`.DS_Store`

## 4. 验证命令与结果

### 4.1 换行、脚本与配置

- `git ls-files --eol -- 'scripts/*.sh' 'backend/**/*.go'`：通过，全部目标文件为 `w/lf` 且命中 `eol=lf` 属性。
- `bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh`：通过。
- `gofmt -l .`（`backend/`）：通过，无输出。
- PowerShell AST 解析 `dev.ps1`、`down.ps1`、`verify.ps1`：通过，无解析错误。
- `docker compose --env-file .env.example --file deploy/compose.yaml config --quiet`：通过。
- Compose 渲染检查：通过，MySQL、Redis、RabbitMQ AMQP 与管理端口共 4 项均为 `host_ip: 127.0.0.1`。
- PyYAML `BaseLoader` 解析三个 workflow 文件：通过；本地未安装 actionlint，因此未执行 actionlint。

### 4.2 Backend 与 Frontend

- `go test -count=1 ./...`：通过，4 个 package 全部成功。
- `go vet ./...`：通过。
- `go test -race -count=1 ./...`：通过，4 个 package 全部成功，未报告数据竞争。
- `npm test -- --run`：通过，2 个测试文件、12 项测试。
- `npm run build`：通过，包含 `vue-tsc --noEmit` 与 Vite production build。

### 4.3 仓库治理

- `python -m unittest discover -s scripts/ci -p 'test_*.py'`：通过，7 项测试全部成功。
- 测试覆盖有效 `develop/0.1.6`、非法分支、未分配版本、错误 `VERSION`、合法 `update` 范围、`update` 应用代码/版本越界、重复权威分配。
- `python scripts/ci/validate_branch.py --branch develop/0.1.6`：在 `VERSION=0.1.6` 后通过并唯一映射到 Phase-00-06。

### 4.4 Windows 实际集成闭环

1. 先执行 `scripts/down.ps1` 清理旧运行状态，成功且保留数据卷。
2. 因本地未跟踪 `.env` 保留了历史端口配置，本次通过调用者环境显式设置 `PUBLISHED_HOST=127.0.0.1`、`HTTP_HOST=127.0.0.1` 后执行 `scripts/dev.ps1`。
3. MySQL、Redis、RabbitMQ 均进入 healthy；Backend 日志确认监听 `127.0.0.1:8080`；Frontend 启动成功。
4. `scripts/verify.ps1`：6 项全部通过，包括三项 Compose health、`/health`、`/ready` 和 Frontend。
5. `docker ps`：MySQL `13306`、Redis `16379`、RabbitMQ `5672/15672` 均显示 `127.0.0.1` 发布；非默认 MySQL/Redis 端口来自本地未跟踪 `.env`。
6. 中断前台开发进程后执行 `scripts/down.ps1`：容器与网络成功停止，`gopulse_mysql_data`、`gopulse_redis_data`、`gopulse_rabbitmq_data` 均保留。

## 5. 与计划的偏差

- Review 建议逐步引入 ShellCheck、PSScriptAnalyzer、actionlint、govulncheck、Pester/Bats。本批未安装额外工具，也未扩展为脚本全量单元测试；先以 Bash 语法、PowerShell AST、治理脚本单元测试和双平台 CI 关闭进入 Phase 1 的阻塞门禁。
- 本地 `.env` 是用户拥有且被 Git 忽略的历史配置，未自动改写；新 clone 或缺少 `.env` 时会从更新后的 `.env.example` 获得回环默认值。现有开发者若要采用安全默认值，需要自行将本地 `PUBLISHED_HOST`、`HTTP_HOST` 调整为 `127.0.0.1`。
- 未人为向远程推送失败测试或非法分支；对应完成条件由可复现的治理单元测试和工作流依赖关系验证，实际远程 branch protection/required checks 仍需仓库设置启用。

## 6. 已知限制与后续项

- P2-01 readiness goroutine 生命周期、P2-02 HTTP Server 完整超时、P2-03 `HTTP_PORT`/Vite/`APP_ENV` 配置契约按 Review 建议留给 Phase-01-01。
- P2-04 已建立基础双平台门禁和治理测试，但 dotenv、端口冲突、陈旧记录、进程身份等脚本逻辑仍缺少 Pester/Bats 级别的细粒度自动化测试。
- GitHub 仓库需要在 `main` 分支保护中把 CI 质量门禁配置为 required checks，形成工作流之外的第二层保护。

## 7. 完成结论

Phase-00-06 的 P1 阻塞项、相关仓库卫生、自动质量门禁、默认回环绑定、版本与权威批次映射均已完成并通过本批规定验证。根 `VERSION` 为 `0.1.6`，未引入 Phase 1 业务能力；Phase 0 最终关闭，可按权威计划从 `develop/0.2.1` 开始 Phase-01-01。
