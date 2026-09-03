# Phase-04-03：Review 整改与阶段收口

## 1. 批次目标

在 `develop/1.1.3` 上关闭 `dev/review/2026-09-03-Phase-4实现Review报告.md` 的 P2-01～P2-03，不扩展到日志平台或业务重构：

1. 默认 `APP_ENV=development` 启动 Backend 时抑制 Gin framework 文本输出，保持应用 stdout 为 Schema v1 JSON Lines。
2. 区分 panic 发生时响应是否已经提交；已提交时不追加第二个错误 Envelope，并让访问完成日志明确记录 panic、实际 wire 状态和提交状态。
3. 收口 Phase 4 权威批次、版本、实施记录与必要验证，将完成版本更新为 `1.1.3`。

## 2. 前置条件与权威执行载体

- Phase-04-01 / `1.1.1` 已由 PR #55 于 2026-09-03 合入 `main`。
- Phase-04-02 / `1.1.2` 已由 PR #56 于 2026-09-03 合入 `main`。
- 本批目标版本为 `1.1.3`，开发分支为 `develop/1.1.3`，基线为上述两批均已合入后的 `origin/main`。
- 实施与验收使用 WSL2 Linux filesystem 和 Bash；不修改冻结的 PowerShell 文件。

## 3. 实施范围

### 3.1 development 模式 JSON Lines 边界

- 在 Gin mode 配置阶段关闭 Gin 默认 debug/error writers，避免 debug warning 和路由注册文本混入应用 stdout/stderr。
- 保留 `development`、`test`、`production` 三种 mode 选择及非法环境拒绝行为。
- 增加最低层回归测试，证明 development router 初始化不产生 Gin framework 文本。
- 让 `scripts/verify-business.sh --logging-live` 使用 `APP_ENV=development` 启动 Backend，并继续逐行解析其完整日志。

### 3.2 已提交响应后的 panic 语义

- Recovery 在捕获 panic 时记录有限的 `response_committed` 状态，不记录 panic 值、stack、header、query 或 body。
- 响应未提交时继续返回统一 `500 internal_error` Envelope。
- 响应已经提交时不再追加第二个 JSON Envelope；由于 HTTP 状态和既有字节不可逆，保留实际 wire status/body。
- Access middleware 读取 panic marker；无论实际 wire status 是否已经提交，完成记录都使用 error 等级，并带 `error_code=internal_error`、`panic_recovered=true` 和 `response_committed`。
- 增加一项代表性写出后 panic 回归，断言不存在混合 payload，完成日志不为 info，敏感 panic 值不泄漏。

### 3.3 治理、版本和记录

- 更新 Phase 4 总实施方案，真实记录前两批合入事实并唯一分配 Phase-04-03 / `1.1.3` / `develop/1.1.3`。
- 更新 README 的当前版本和 panic 已提交响应契约。
- 同步 `VERSION`、`frontend/package.json` 与 `frontend/package-lock.json` 到 `1.1.3`。
- 创建镜像实施记录，只记录实际完成工作、实际命令与结果。

## 4. 非目标

- 不通过全响应缓冲承诺流式或已提交响应仍可改写为 500。
- 不新增 API 路由、公共响应字段、数据库 Schema、AMQP Envelope、Frontend DTO 或持久事实变化。
- 不实现 LogMonitor、Kafka、日志文件、轮转、传输、存储、索引、查询、采样或动态日志级别。
- 不开展一般性代码审计、依赖审计、覆盖率扩张或与三项 Review 问题无关的重构。

## 5. 固定验证命令

按最小受影响检查到批次完成门禁的顺序执行；同一最终 diff 上成功后不重复：

```bash
(cd backend && go test -count=1 ./internal/http/middleware ./internal/http)

(cd backend && go test -count=1 ./...)
(cd backend && go vet ./...)
(cd backend && go test -race -count=1 ./...)
test -z "$(gofmt -l backend)"

bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-business.sh
scripts/verify-business.sh --self-test
python3 -m unittest discover -s scripts/ci -p 'test_*.py'
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/1.1.3 --base-ref origin/main
git diff --check origin/main...HEAD

scripts/verify-business.sh --logging-live
```

若最终变更未触及 Frontend 产品代码、Compose 拓扑、持久数据或异步业务语义，不重复 Phase-04-02 已通过的 Frontend build、完整 Phase 0～3 故障矩阵或 integration；本批只同步 Frontend 版本元数据，并由版本校验覆盖。

## 6. 验收标准

- development router 初始化不产生 `[GIN-debug]` 或其他 Gin framework 文本；focused logging 使用 development mode 后仍能把 Backend 每个非空日志行解析为 Schema v1 JSON。
- 写出前 panic 仍返回统一 500 Envelope，panic 值与 stack 不泄漏。
- 写出后 panic 保留不可逆的实际 wire status/body，但不追加内部错误 JSON；panic 与完成记录均为 error 语义，完成记录明确 `panic_recovered=true`、`response_committed=true`、实际 status 和 `internal_error`。
- Phase 4 总方案对三个批次只有一组权威分配，前两批合入事实和本批状态真实；branch/version governance 通过。
- 根与 Frontend 版本均为 `1.1.3`；镜像实施记录与实际 diff、验证和限制一致。
- 上述固定门禁全部通过且无阻断问题后，本批本地完成；远程门禁与合入状态只在实际发生后记录。

## 7. 完成条件

P2-01～P2-03 的关闭条件全部满足、固定验证通过、实施记录和版本已更新并提交后，停止本批实施。远程分支推送成功不等同于已合入 `main`；Phase 4 最终外部完成仍以该分支远程门禁成功并合入为准。
