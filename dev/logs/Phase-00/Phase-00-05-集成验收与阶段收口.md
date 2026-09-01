# Phase 0-05：集成验收与阶段收口实施记录

- 完成日期：2026-08-31
- 分支：`develop/0.1.5`
- 目标版本：`0.1.5`
- 前置批次：Phase 0-01 至 Phase 0-04（`0.1.1` 至 `0.1.4`）
- 执行范围：Phase-00-05 集成验收、开发入口文档与 Phase 0 阶段收口

## 1. 实际完成内容

1. 新增 `scripts/verify.ps1` 与 `scripts/verify.sh`。两套脚本均从自身路径定位仓库，读取受限 dotenv 中的 `HTTP_PORT`，检查 MySQL、Redis、RabbitMQ 各自恰好存在一个运行且 healthy 的 Compose 容器，并结构化验证 `/health`、`/ready` JSON 合约与 Frontend HTTP 可访问性。
2. `verify` 聚合所有失败项后以非零状态退出，输出具体检查名与诊断入口；脚本只读检查现有环境，不启动或停止应用、容器，不修改 `.run` 记录，也不删除具名卷。
3. 新增根 `README.md`，记录 Phase 0 能力与边界、工具版本基线、`.env` 初始化、Windows/Unix 的 `dev`、`verify`、`down` 命令、服务地址、API 合约、端口与健康检查排障、数据卷保留语义以及 Phase 1 复用入口。
4. 完成 Windows PowerShell 与 WSL Bash 的冷启动、正常检查、故障矩阵、恢复、重复启动、端口冲突、陈旧记录、停止和幂等性验收；从非仓库目录调用 Bash/PowerShell 入口验证了路径解析。
5. 在 Bash `Ctrl+C` 验收中发现 Phase-00-04 遗留缺陷：应用记录使用 `ps -o lstart` 的本地化文本作为进程启动身份，记录值与清理时读取值可能不一致，导致记录被判为陈旧且应用进程遗留。
6. 修复 `scripts/dev.sh` 与 `scripts/down.sh`：应用记录改用 Linux `/proc/<pid>/stat` 第 22 字段的稳定启动 ticks；旧格式、损坏或身份不匹配记录仍按陈旧记录安全移除。`dev.sh` 清理过程中即使刚写入记录校验失败，也会使用当前会话直接持有的启动 PID 作为安全回退，确保本次启动的进程不会成为孤儿。
7. 根 `VERSION` 从 `0.1.4` 更新为 `0.1.5`，完成 Phase 0 最终批次版本收口。

## 2. 本批变更文件

- `README.md`
- `scripts/verify.ps1`
- `scripts/verify.sh`
- `scripts/dev.sh`
- `scripts/down.sh`
- `dev/logs/Phase-00/Phase-00-05-集成验收与阶段收口.md`
- `VERSION`

本批未新增业务 API、数据库 Schema、迁移、种子数据、RabbitMQ 业务拓扑、应用 Dockerfile、CI 或 Phase 1 业务能力。`.env`、`.run/`、`frontend/node_modules/` 与 `frontend/dist/` 均未纳入版本控制。

## 3. 验证环境

实际观察到的工具版本：

- Windows Go `1.26.1`；WSL Go `1.26.4`
- Windows Node.js `24.14.1`；WSL Node.js `24.16.0`
- Windows npm `11.11.0`；WSL npm `11.13.0`
- Docker `29.4.0`
- Docker Compose `5.1.1`
- PowerShell `7.6.4`
- Bash `5.2.21`

Windows 是主集成环境。WSL 中 Bash、Go、Node.js、npm、进程组、HTTP 请求与运行记录均真实执行；Compose 通过忽略目录 `.run/` 内的临时 Docker Desktop CLI 路径转换桥接器执行，该桥接器不是提交内容。

## 4. 自动化检查与结果

### 4.1 Backend

执行：

```powershell
cd backend
go test ./...
go vet ./...
```

结果：所有 Backend 包测试通过，`go vet ./...` 通过。

### 4.2 Frontend

执行：

```powershell
cd frontend
npm ci
npm test
npm run typecheck
npm run build
```

结果：`npm ci` 成功；2 个测试文件、12 个测试全部通过；TypeScript 类型检查和 Vite 生产构建通过。组件测试覆盖全健康、依赖异常、Backend 不可达和加载状态。

### 4.3 脚本语法与静态检查

执行：

```powershell
# dev.ps1、down.ps1、verify.ps1
[System.Management.Automation.Language.Parser]::ParseFile(...)

bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh
git diff --check
```

结果：三个 PowerShell 脚本 AST 解析无错误，三个 Bash 脚本语法检查通过，Git 空白检查通过。

## 5. 集成验收结果

### 5.1 冷启动与正常状态

- 从应用和 Compose 容器均停止的状态执行单条 `dev` 命令，MySQL、Redis、RabbitMQ 最终均为 healthy，Backend 与 Frontend 成功启动。
- Windows `verify.ps1` 从仓库外目录调用并通过：三个 Compose 服务 healthy，`/health` 返回 HTTP 200 且为 `status=ok, service=backend`，`/ready` 返回 HTTP 200 且三项 checks 均为 `up`，Frontend 返回 HTTP 200。
- WSL 从 `/tmp` 调用 `dev.sh` 与 `verify.sh` 并通过同一组检查。
- 浏览器自动化访问 `http://localhost:5173/`，实际 DOM 中 Backend、MySQL、Redis、RabbitMQ 均渲染为“正常”；该结果不是仅以首页 HTML 200 代替。
- 对运行环境执行 `verify` 前后比较，容器 ID/状态、`.run` 记录及具名卷列表保持不变，确认验收脚本无破坏性。

### 5.2 依赖故障与恢复矩阵

在 Backend 保持运行的情况下实际执行：

- 单独停止 MySQL、Redis、RabbitMQ。
- 同时停止 MySQL 与 Redis。
- 每种故障期间重复请求 `/ready`。
- 恢复对应容器并等待 healthcheck。

结果：

- 所有场景中 `/health` 始终返回 HTTP 200。
- `/ready` 在故障期间返回 HTTP 503，并准确将实际失败依赖标记为 `down`，未失败依赖保持 `up`。
- 多依赖故障时所有失败项均被同时报告。
- 重复 readiness 请求均在既定超时范围内结束。
- 恢复容器后 `/ready` 无需重启 Backend 即恢复 HTTP 200；Backend PID 在整个故障与恢复流程中保持不变。

### 5.3 生命周期与可靠性场景

- 连续执行第二次 `dev.ps1` 和 `dev.sh` 均以退出码 1 安全失败，原 Backend/Frontend PID 保持不变，未产生重复容器或孤儿应用进程。
- 使用独立监听进程占用 8080 后执行 `dev.sh`，脚本提前失败并明确报告 Backend 所需端口及占用进程 PID。
- 制造指向无关存活进程、但启动身份不匹配的 Frontend 记录后执行 `down.sh`，记录被移除，无关进程仍存活，证明不会按复用 PID 误杀。
- 在活动 `dev.sh` 会话中故意破坏刚生成记录的启动身份后发送终止信号，脚本先报告陈旧记录，再通过当前会话 PID 回退停止 Frontend；Backend、Frontend、记录与锁均被清理，验证孤儿进程修复有效。
- 使用有效 `/proc` 启动 ticks 的正常 `Ctrl+C` 流程也能停止两项应用并清理记录和锁，同时保持 Compose 容器运行。
- `down` 停止应用与 Compose 项目；连续第二次执行仍返回成功。停止后 Frontend、`/health`、`/ready` 均不可访问。
- `gopulse_mysql_data`、`gopulse_redis_data`、`gopulse_rabbitmq_data` 在故障、恢复、`Ctrl+C` 与重复 `down` 前后均保留。
- 环境停止时分别执行 PowerShell 与 Bash `verify`，两者均聚合报告 3 个缺失容器和 3 个不可达 HTTP 项，并以非零状态退出。

## 6. 偏差、限制与后续项

- Phase-00-05 原计划原则上不新增基础能力，但完整验收暴露了 Phase-00-04 的真实 Bash 进程身份缺陷。按照实施方案“发现缺陷时回到对应批次修复后重新验收”的规则，本批对 `dev.sh`、`down.sh` 做了最小必要修复，并重新执行了正常与异常清理场景。
- WSL 原生 Docker 路径在一次重验时出现 Docker Desktop host-service socket 不可用；为避免修改用户其他 Docker 环境，最终按 Phase-00-04 已记录的方法使用临时 Docker Desktop CLI 桥接完成 Bash 验收。桥接器位于忽略目录，不影响交付物。
- 混合平台场景中，Windows Vite 仅监听 Windows 本机的 `::1:5173` 时，WSL 内 `verify.sh` 无法访问该 Frontend；这不是支持的同一平台生命周期组合。以 WSL `dev.sh` 启动 Frontend 后，WSL `verify.sh` 完整通过。
- 本次未再单独执行浏览器中的 Backend 不可达演示；Frontend 自动化组件测试已覆盖 Backend 不可达，浏览器端到端验收已覆盖四项健康状态，故按比例化验收规则停止扩展验证。
- Phase 1 可复用现有配置加载、MySQL/Redis/RabbitMQ 客户端、Compose project、健康/就绪合约、Frontend 状态页以及跨平台 `dev`、`verify`、`down` 入口。

## 7. 完成条件

Phase-00-05 的验收脚本、集成矩阵、开发入口文档、缺陷修复、实施记录与 `VERSION=0.1.5` 已完成；所有计划内阻塞项已通过，未引入 Phase 1 越界能力。Phase 0 达到可交付给 Phase 1 的工程骨架里程碑。
