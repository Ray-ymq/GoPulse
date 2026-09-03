# Phase-05-03：实现 Review 整改

## 1. 目标

关闭 `dev/review/2026-09-03-Phase-5实现Review报告.md` 中的 P1-01、P2-01、P2-02 与 P2-03，使 Redis Exporter 的监听配置、隔离验收失败清理、随机端口分配和分支治理重新满足 Phase 5 对外边界，并将完成版本推进到 `1.2.3`。

## 2. 前置条件与权威执行载体

- Phase-05-01 / `1.2.1` 已由 PR #58 合入 `main`。
- Phase-05-02 / `1.2.2` 已由 PR #60 合入 `main`。
- 本批目标版本为 `1.2.3`，权威开发分支为 `develop/1.2.3`，基线为上述两批与 Phase 5 状态文档均已合入后的 `origin/main` 提交 `e3915e3`。
- 实施与验收使用 WSL2 Linux filesystem、Bash 和单一 Docker daemon；不修改冻结的 PowerShell 文件。
- 工作区既有未跟踪或用户自有文件不得读取、修改、暂存或提交。

## 3. 实施范围

### 3.1 Exporter host 安全边界

- 将 Go 配置加载器的 host 规则收紧为合法 IPv4、IPv6 或 hostname；只有完整的一对 IPv6 方括号可以被去除。
- 拒绝空括号、残缺或嵌套括号、方括号包裹的 IPv4、slash、非法 hostname 标签和 host:port 输入。
- 保留用户显式选择 `0.0.0.0`、`::` 或其他非回环地址的能力；默认仍为 `127.0.0.1`。
- `scripts/dev.sh` 使用等价规则在启动 Compose 和应用前拒绝非法 Redis/Exporter host。
- 增加最低层配置测试与真实进程回归，证明 `[]` 在 `net.Listen` 前以 `invalid_configuration` 退出且不创建监听端口。

### 3.2 隔离验收失败清理

- 将“容器仍在运行且端口绑定正确”的运行断言与“资源属于本次随机 Compose project”的清理断言分离。
- 生成受限 project token 和私有 Compose 文件后立即记录清理所有权；使用 project 格式、Compose 文件路径/摘要和 container/network/volume project label 限制删除范围。
- 在 `compose up` 前启用清理，使部分创建失败也会尝试仅对本次 project 执行 `down --volumes --remove-orphans`。
- 增加 Redis 已停止后故意失败和端口占用导致 Compose 部分创建后失败两项真实注入；两者必须无遗留资源且日常 `gopulse` 快照不变。
- self-test 必须拒绝非法 project、被篡改的 Compose 文件和错误 project label，不停止或删除无关资源。

### 3.3 端口、治理、版本和记录

- 用集合唯一性检查四个随机端口，冲突时重新分配全部端口；self-test 固定覆盖 `REDIS==AUTH` 与 `EXPORTER==TIMEOUT`。
- 在 Phase 5 总实施方案中唯一分配 Phase-05-03 / `1.2.3` / `develop/1.2.3`。
- 同步根 `VERSION`、`frontend/package.json`、`frontend/package-lock.json` 和 README 当前版本到 `1.2.3`。
- 创建同名实施记录，只记录实际完成内容、实际命令、结果、偏差和限制。

## 4. 非目标

- 不新增指标 family、HTTP 路由、Redis 数据源、多目标、TLS、HTTP 鉴权、主动采集、缓存或历史数据。
- 不改变 Backend、Frontend 业务、数据库 Schema、消息契约、搜索、通知或 Phase 6 Plugin Manager/MetricsMonitor 设计。
- 不更新冻结的原生 PowerShell 生命周期脚本。
- 不开展一般性代码审计、依赖审计、覆盖率扩张或与四项 Review finding 无关的重构。

## 5. 固定验证命令

按最小受影响检查和最终完成门禁执行；同一最终 diff 成功后不重复：

```bash
(cd exporters/redis && test -z "$(gofmt -l .)")
(cd exporters/redis && go test -count=1 ./...)
(cd exporters/redis && go vet ./...)
(cd exporters/redis && go test -race -count=1 ./...)

bash scripts/verify-exporter.sh --self-test
scripts/verify-exporter.sh
bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-exporter.sh
python3 -m unittest discover -s scripts/ci -p 'test_*.py'
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/1.2.3 --base-ref origin/main
docker compose --env-file .env.example --file deploy/compose.yaml config --quiet
git diff --check origin/main...HEAD

scripts/dev.sh
scripts/verify.sh
scripts/down.sh
```

日常生命周期仅在确认不会接管用户进程或资源后执行。若启动链路失败，先用 `scripts/down.sh` 清理本次已确认归属的状态，再记录真实结果。由于本批不改变业务、持久数据、消息或搜索语义，不重复 Phase-05-02 已通过的完整 Phase 0～4 业务矩阵。

## 6. 验收标准

- `REDIS_EXPORTER_HTTP_HOST=[]`、畸形 bracket、slash host 和非法 hostname 在监听前被拒绝；日志只包含脱敏的 `invalid_configuration` 分类。
- 合法 IPv4、hostname、带或不带方括号的 IPv6通过；默认真实 listener 为 loopback，只有显式配置 wildcard/non-loopback 才允许远程绑定。
- `scripts/dev.sh` 与 Go loader 对受支持 host 类型和拒绝边界保持一致。
- Redis 已停止后的断言失败与 Compose 部分创建失败均会清理本次 container、network、volume；日常 `gopulse` 资源前后不变。
- 非法 project、被篡改 Compose 文件、错误 label 和非本次资源均不能获得清理授权。
- 四个主验收端口任意两者都不重复；指定的两个非相邻冲突 self-test 均被拒绝。
- Exporter unit/vet/race、self-test、真实 Redis 验收、Bash 生命周期、治理和版本门禁全部通过。
- Phase 5 总方案只有一条 `develop/1.2.3` 权威分配，根与 Frontend 版本为 `1.2.3`，实施记录与实际结果一致。

## 7. 完成条件

四项 Review finding 的关闭条件、上述固定验证、版本同步和实施记录全部完成后，本批可以提交并推送 `develop/1.2.3`。远程分支存在不等同于已合入 `main`；远程门禁与合入事实只能在实际发生后补记。
