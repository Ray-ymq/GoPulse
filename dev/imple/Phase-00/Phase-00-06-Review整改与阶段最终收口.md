# Phase-00-06：Review 整改与阶段最终收口

## 1. 批次目标

以 `develop/0.1.6` 作为 Phase 0 权威收尾分支，关闭 `dev/review/2026-09-01-GoPulse项目Review报告.md` 中阻断进入 Phase 1 的 P1 项，并完成必要的仓库卫生修复。该批次不实现任何 Phase 1 业务能力。

## 2. 实施范围

1. 增加 `.gitattributes`，固定 Bash 与 Go 文件为 LF，消除 Windows `core.autocrlf=true` checkout 对语法与格式检查的破坏。
2. 建立 GitHub Actions 质量门禁，覆盖 Backend 测试/vet/race、Frontend 测试/构建、PowerShell/Bash 语法与 Compose 配置。
3. 增加分支治理脚本，校验分支名称、权威 Phase 版本分配、开发分支 `VERSION`，以及 `update` 分支的规划范围。
4. 调整自动 PR/merge 工作流，使建 PR 与自动合并必须依赖同一套质量门禁，不再先尝试无门禁立即合并。
5. 将 Compose 发布端口和 Backend 默认监听地址改为回环接口；远程访问只能通过显式配置选择。
6. 删除已被忽略但仍受 Git 跟踪的 `.DS_Store`。
7. 更新 README、Review 整改状态、Phase 0 总实施方案、实施记录与根 `VERSION`。

## 3. 不在本批范围

- Phase 1 的数据库迁移、认证、帖子、评论、点赞或缓存业务。
- Review 中建议纳入 Phase-01-01 的 HTTP Server 资源边界、readiness goroutine 生命周期、Vite 非默认端口契约与 `APP_ENV` 行为调整。
- Pester/Bats 全量脚本单元测试、`govulncheck`、ShellCheck、PSScriptAnalyzer 与 actionlint 的完整引入；本批先建立无需额外安装的语法和核心质量门禁。

## 4. 验收标准

- `git ls-files --eol scripts/*.sh backend/**/*.go` 显示目标文件工作区为 LF；`bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh` 通过；`gofmt -l backend` 无输出。
- `go test -count=1 ./...`、`go vet ./...`、`go test -race -count=1 ./...` 在 `backend/` 下通过。
- `npm test -- --run` 与 `npm run build` 在 `frontend/` 下通过。
- 三个 PowerShell 脚本 AST 解析无错误，Compose 使用 `.env.example` 解析通过。
- 治理脚本接受 `develop/0.1.6` 且能唯一映射到 Phase-00-06；非法分支、未分配版本、错误 `VERSION` 和 `update` 应用代码变更均有自动化负向测试。
- Compose 渲染结果的 MySQL、Redis、RabbitMQ 端口 Host IP 均为 `127.0.0.1`，Backend 无显式覆盖时默认监听 `127.0.0.1:8080`。
- `VERSION` 更新为 `0.1.6`，Phase-00-06 实施记录只记载实际执行结果，Phase 0 总实施方案标记本批完成。

## 5. 回归范围与完成条件

回归覆盖本批直接影响的配置加载、脚本语法、Compose 配置、仓库治理和现有 Backend/Frontend 自动化测试。由于 `.gitattributes`、CI 与默认网络绑定属于共享工程基础设施，额外执行 Backend race、Frontend production build 和实际本地启动/验证/停止闭环。

上述验收全部通过且无阻塞问题、实施记录和版本文件已更新并提交后，本批完成，Phase 0 最终关闭，Phase 1 可从权威计划分配的 `develop/0.2.1` 开始。
