# GoPulse Phase 5 实现 Review 报告

## 1. Review 信息

| 项目 | 内容 |
| --- | --- |
| Review 日期 | 2026-09-03 |
| 用户指定权威 Review 分支 | `develop/1.2.3` |
| Review 分支创建方式 | 远端不存在 `develop/1.2.3`；fetch 后从最新 `origin/main` 创建本地分支，未推送 |
| Review 基线 | `e3915e30e402ff2e23146ac04d031c0136ccb386`（PR #61 合并提交，Review 开始时与 `origin/main` 一致） |
| Phase 5 实现起点 | `1d24246c49f6d978634f1950e76f0cb023e53c58`（Phase 4 Review 整改合并提交） |
| Phase 5 实现提交 | `59fe5ce731cba0630fdf59cd85db3176d221cd08`（Phase-05-01）与 `b85afa8e6feadf0f6113270f6c859d27c600c206`（Phase-05-02） |
| 当前完成版本 | 根 `VERSION` 与 Frontend npm 元数据均为 `1.2.2`；本次只新增 Review 文档，不修改版本 |
| 实施批次 | Phase-05-01、Phase-05-02 |
| 实际执行环境 | WSL2 Linux，Go 1.26.7，Node.js 24.20.0，npm 11.19.0，Docker 29.7.2 / Compose v5.5.0 |
| Review 范围 | Phase 5 总方案与拆分方案、实施记录、Redis Exporter 配置/采集/Prometheus/HTTP/日志/退出、Bash 生命周期与隔离验收、CI、版本与分支治理、远程合入证据 |
| Phase 5 实现规模 | 27 个唯一文件，1972 行新增、63 行删除；不把穿插合入的 Phase 6 规划文档计入实现规模 |
| 结论 | **有条件通过（Conditional Pass）** |

本次 Review 重点判断：

1. Redis Exporter 是否真正按请求读取真实 Redis `INFO`，并稳定形成 Prometheus 0.0.4 成功与失败契约。
2. Redis 停止、认证错误、超时和恢复是否与 Exporter 进程生命周期隔离，失败时是否没有部分或陈旧指标。
3. 默认回环监听、配置校验、结构化日志、PID/容器归属和失败清理是否满足公开安全边界。
4. 独立 Exporter 验收、Phase 0～4 回归、CI 与远程门禁是否提供可重复证据。
5. 用户指定的 `develop/1.2.3` 是否已经得到 Phase 5 权威版本与批次分配。

## 2. 总体结论

Phase 5 的主体能力已经形成完整、可运行的最小闭环：

- 新增独立 `exporters/redis` Go module；Exporter 启动不探测 Redis，只在 `GET /metrics` 时执行一次有界 `INFO`。
- `GET /health` 始终只表达 HTTP 进程存活；`GET /metrics` 成功返回当前真实指标，目标故障时返回 `503` 且正文只保留 `gopulse_redis_up 0`。
- 指标名称、类型和有限标签固定；Redis 停止、错误密码、超时以及无需重启的恢复均由独立真实 Redis 验收覆盖。
- Exporter 已纳入 `dev.sh → verify.sh → down.sh` 的构建、进程记录、只读检查和停止顺序，并新增独立 CI Redis Exporter job。
- 本次独立执行 Exporter unit、vet、race、self-test 和真实 Docker 验收均通过；Phase 0～4 完整业务验收也通过。
- PR #58 和 PR #60 已于 2026-09-03 合入；两个 head commit 的 Branch governance、Backend、Frontend、Redis Exporter、Scripts and Compose、Integration 与自动 PR/合并检查均为 success。

但是，本次发现 **1 项 P1、3 项 P2**：

1. `REDIS_EXPORTER_HTTP_HOST=[]` 会通过配置校验并被归一化为空 host，实际监听 `*:PORT`，把本应显式选择的远程暴露变成配置拼写错误即可触发的全接口监听。
2. 隔离验收的失败清理依赖 Redis 容器仍有活动端口映射；Redis 已停止时 Docker 清空该映射，随后任何断言失败都会使清理拒绝 `compose down`，遗留本次 container/network/volume。
3. 四个随机端口只检查了三个相邻组合，不保证两两不同，验收仍存在低概率端口冲突和非确定性失败。
4. Phase 5 权威表只分配到 `develop/1.2.2`；用户指定的 `develop/1.2.3` 当前无法通过仓库 Branch governance。

其中 P1-01 是明确的监听安全边界缺口，应在 Phase 6 开始复用 Exporter 之前优先关闭。P2-01 与 P2-02 不否定本次成功路径证据，但会使失败路径无法可靠自清理或产生偶发门禁失败；P2-03 必须在推送或为该分支创建 PR 前完成权威分配。因此结论为有条件通过，而不是无条件通过。

## 3. 风险分级

| 等级 | 定义 |
| --- | --- |
| P0 | 已造成数据丢失、严重安全事件或核心链路完全不可用，需要立即停止发布 |
| P1 | 会阻断受支持平台、破坏关键安全边界或形成明显网络暴露，应在下一阶段复用前修复 |
| P2 | 当前成功路径可运行，但会放大失败清理、稳定性、可维护性或仓库治理风险 |
| P3 | 低风险工程卫生或文档质量问题，可随近邻批次处理 |

本次未发现 P0 或 P3；记录 **1 项 P1、3 项 P2**。

## 4. Review Findings

### P1-01：空 IPv6 括号会被归一化为空 host，并使 Exporter 监听全部接口

**位置**

- `exporters/redis/internal/config/config.go:79-90`
- `scripts/dev.sh:224-250`
- `README.md:59`
- `dev/imple/Phase-05/Phase-05-总实施方案.md:65-71`

**实际证据**

`validateHost` 先判断原始 trim 后的值非空，最后却使用：

```go
return strings.Trim(value, "[]"), nil
```

`strings.Trim` 会删除两端任意数量的 `[`/`]`。因此 `[]` 在非空检查后被归一化成空字符串，`Config.Load()` 不返回错误。`Config.HTTPAddress()` 随后把空 host 拼成 `:PORT`，Go HTTP listener 将其解释为通配监听。

Review 实际构建当前 Exporter，并使用：

```text
REDIS_EXPORTER_HTTP_HOST=[]
REDIS_EXPORTER_HTTP_PORT=56730
```

进程成功启动，`ss -ltnp "sport = :56730"` 实际显示：

```text
LISTEN 0 4096 *:56730 *:* users:(("redis-exporter",pid=...,fd=4))
```

日志只记录正常的 `redis exporter listening`，没有配置错误。`scripts/dev.sh` 也只检查 host 去除空白后是否非空，因此同样会放行 `[]`。

同一实现还会放行 `bad/host` 之类明显不是合法 host 的值，只在 `net.Listen` 阶段记录 `listen_failed`，未满足“无效配置在监听前失败”的方案约束。

**影响**

- README 与 Phase 5 方案承诺默认回环监听，非回环地址应是开发者显式选择；当前一个括号拼写错误即可变成 IPv4/IPv6 通配监听。
- `/health` 与 `/metrics` 没有认证；当宿主机防火墙或网络策略允许时，局域网或其他可达网络中的客户端可以直接访问 Exporter。
- 暴露内容虽不含 Redis 密码或业务 key，但包含 Redis 存活、内存、连接、命令、CPU 与 keyspace 等运行信息，并扩大后续 Phase 6 插件复用的攻击面。
- 配置 loader、`dev.sh` 和实际 listener 对“有效 host”的理解不一致，现有测试没有覆盖归一化后为空或非法 hostname 字符的情况。

**建议整改**

1. 不要使用无条件 `strings.Trim(value, "[]")`：
   - 未加括号的值按 IPv4 或合法 hostname 校验；
   - 只有恰好一对外层方括号且内部为合法 IPv6 时才去括号；
   - 归一化后再次执行非空检查。
2. 明确拒绝 `[]`、`[`、`]`、多重括号、包含 `/` 的 host，以及其他不能形成合法监听/目标地址的输入。
3. 让 `scripts/dev.sh` 使用与 Go loader 等价的 host 规则，避免生命周期脚本先放行、进程后失败或意外公开监听。
4. 增加最低层配置测试，并增加一次真实 listener 回归：默认和非法 bracket 配置不得出现 `*:PORT`；只有用户显式配置 `0.0.0.0`、`::` 或其他非回环地址时才允许远程绑定。

**关闭条件**

- `REDIS_EXPORTER_HTTP_HOST=[]` 在调用 `net.Listen` 前以脱敏的 `invalid_configuration` 非零退出。
- 默认 `.env.example` 与 `scripts/dev.sh` 启动后仅监听 `127.0.0.1`。
- 合法 IPv4、hostname 和 IPv6 用例通过；空 bracket、畸形 bracket、slash host 用例被拒绝。
- Exporter unit/race、真实 Exporter 验收和 Bash 生命周期验证通过。

---

### P2-01：Exporter 验收在 Redis 已停止或 Compose 部分创建后不能保证清理本次资源

**位置**

- `scripts/verify-exporter.sh:116-123`
- `scripts/verify-exporter.sh:139-150`
- `scripts/verify-exporter.sh:244-260`
- `scripts/verify-exporter.sh:288-300`
- `dev/imple/Phase-05/Phase-05-总实施方案.md:255-256`

**实际证据**

`cleanup` 只有在 `CONTAINER_ID` 非空时才尝试清理，并且调用 `compose down` 前必须通过 `container_owned`。后者不仅验证 Compose project/service label，还要求 Redis 容器当前存在：

```text
127.0.0.1:$REDIS_PORT -> 6379/tcp
```

但主验收会在第 290 行执行 `compose stop redis`，之后还有 stopped-target 响应、无陈旧指标、health、PID identity、重启和恢复等多项可能失败的断言。

Review 使用同一 `redis:7.2.5-alpine` 镜像验证 Docker 行为：

```text
before={"6379/tcp":[{"HostIp":"127.0.0.1","HostPort":"54314"}]}
after={}
```

即容器停止后 `.NetworkSettings.Ports` 变为空对象。此时如果第 291～297 行任一检查失败并进入 `cleanup`，`container_owned` 必然失败，脚本会输出“Refusing to remove isolation resources”，并保留本次随机 project 的 container、network 和 volume。

另一个相同根因是：若 `compose up -d` 已创建 network/volume 或部分 container 后返回失败，下一行尚未来得及写入 `CONTAINER_ID`，`cleanup` 会完全跳过 Compose 清理。

**影响**

- 成功路径会清理，但失败路径可能遗留带随机名称的 container、network 和持久 volume，违反 Phase 5 封闭矩阵“成功、失败和中断均只清理本次资源”的验收条件。
- CI 或本地反复遇到真实故障时会累积资源，后续磁盘、名称、网络或端口问题会掩盖原始失败。
- 当前 `--self-test` 只覆盖错误 token/record 不误杀进程，没有覆盖“合法归属但容器已停止”和“Compose 部分创建”两条清理路径。

**建议整改**

1. 将“可删除本次 project”的证明与“容器当前运行且端口绑定正确”的运行断言拆开。
2. 在生成受限 project token 与私有 Compose 文件后记录 cleanup ownership；清理时用安全 project 正则、Compose 文件路径以及 container/network/volume labels 证明归属，不要求服务仍在运行。
3. 在 `compose up` 前设置可恢复的 Compose-created 状态，使部分创建失败也会尝试针对该随机 project 执行受限 `compose down --volumes --remove-orphans`。
4. 增加两个失败注入测试：Redis `stop` 后立即失败、Compose 创建部分资源后失败；两者结束后该 project 的 container/network/volume 必须全部不存在，日常 `gopulse` 栈快照必须不变。

**关闭条件**

- stopped-target 矩阵中任一断言失败时，本次随机 project 仍被完整清理。
- `compose up` 部分失败时不遗留 network、volume 或 container。
- 错误 label、错误 project 和非本次资源仍被拒绝删除。
- `scripts/verify-exporter.sh --self-test` 与真实验收通过。

---

### P2-02：四个随机端口没有执行完整的两两冲突检查

**位置**

- `scripts/verify-exporter.sh:27-33`
- `scripts/verify-exporter.sh:242-243`

**实际证据**

脚本分别获取 Redis、正常 Exporter、错误密码 Exporter 和超时 Exporter 四个端口，但只检查：

```text
REDIS_PORT != EXPORTER_PORT
EXPORTER_PORT != AUTH_EXPORTER_PORT
AUTH_EXPORTER_PORT != TIMEOUT_EXPORTER_PORT
```

缺少 `REDIS_PORT != AUTH_EXPORTER_PORT`、`REDIS_PORT != TIMEOUT_EXPORTER_PORT` 和 `EXPORTER_PORT != TIMEOUT_EXPORTER_PORT`。

Review 直接代入以下两组值，当前判断均输出 PASS：

```text
REDIS=10000 EXPORTER=10001 AUTH=10000 TIMEOUT=10002
REDIS=10000 EXPORTER=10001 AUTH=10002 TIMEOUT=10001
```

因此脚本声明“Random ports collided”之前并没有证明四个端口唯一。即使补全比较，`random_port` 释放 socket 后再使用仍有常规 TOCTOU 竞争，但当前遗漏比较是可以确定消除的额外缺口。

**影响**

- 低概率情况下，错误密码 Exporter 可能尝试占用 Redis 发布端口，或超时 Exporter 与正常 Exporter 使用同一端口，造成 `EADDRINUSE`、请求打到错误进程或非确定性 CI 失败。
- 失败消息可能指向 Exporter 启动/恢复问题，而真实原因只是验收脚本端口分配冲突，增加排查成本。

**建议整改**

1. 用集合验证四个端口数量为 4，而不是继续手写部分 pair 比较；发生冲突时重新分配全部端口。
2. 如需进一步消除 TOCTOU，可由一个受控分配器保留 listener 到消费者接管，或让可支持的服务绑定端口 `0` 后读取实际端口；Compose 发布端口至少应在启动失败时明确重试一次全新 project。
3. 在 self-test 中固定注入非相邻端口冲突，证明检查会拒绝。

**关闭条件**

- 四个端口任意一对相同都会被拒绝或重新分配。
- self-test 覆盖至少一个 `REDIS==AUTH` 和一个 `EXPORTER==TIMEOUT` 用例。
- 真实 Exporter 验收继续通过。

---

### P2-03：Phase 5 权威计划未分配 `develop/1.2.3`

**位置**

- `dev/imple/Phase-05/Phase-05-总实施方案.md:44-59`
- `dev/imple/Phase-05/Phase-05-总实施方案.md:296-300`
- `scripts/ci/validate_branch.py`

**实际证据**

截至 2026-09-03 fetch 后：

- 远端不存在 `origin/develop/1.2.3`；GitHub 已合入的 Phase 5 实现分支只有 `develop/1.2.1`（PR #58）与 `develop/1.2.2`（PR #60）。
- Phase 5 权威分配表也只包含 Phase-05-01 / `1.2.1` 和 Phase-05-02 / `1.2.2`。
- 本次按用户指定，从最新 `origin/main` 的 `e3915e3` 创建本地 `develop/1.2.3`，但执行：

```bash
python3 scripts/ci/validate_branch.py --branch develop/1.2.3 --base-ref origin/main
```

实际失败：

```text
ERROR: develop/1.2.3 must map to exactly one authoritative allocation; found 0
```

**影响**

- 当前 Review 文档可以本地提交，但该分支在现有治理规则下不能通过 Branch governance，也不应直接推送或自动创建 PR。
- 若后续使用同一分支整改本报告 findings，而未先更新权威分配，将重复 Phase 4 Review 中已经出现过的未分配 review/closeout 分支问题。
- 根版本仍为已完成产品版本 `1.2.2` 是正确的；问题是 review/整改批次没有权威版本与分支归属，而不是本次文档需要提前修改 `VERSION`。

**建议整改**

1. 若 `develop/1.2.3` 用于 Phase 5 Review 整改，在 Phase 5 总实施方案中显式新增 Review closeout 批次（例如 Phase-05-03），分配目标版本 `1.2.3` 与分支 `develop/1.2.3`，并定义只关闭本报告 findings 的验收边界。
2. 在开始产品代码整改前先把权威表更新合入；整改批次完成时再按版本规则更新根与 Frontend 元数据到 `1.2.3`。
3. 若不准备创建 Phase 5 整改批次，则 Review 文档应留在允许的规划分支范围内，不应把未分配的 `develop/1.2.3` 当作可推送开发分支。

**关闭条件**

- `develop/1.2.3` 在 Phase 5 总实施方案中恰好映射一个明确批次，或 Review 工作被迁移到符合规则的规划分支。
- `validate_branch.py --branch develop/1.2.3 --base-ref origin/main` 通过后才推送该开发分支。
- 未在整改完成前把根 `VERSION` 错误声明为 `1.2.3`。

## 5. 已验证的正向实现

### 5.1 配置、采集与 HTTP 契约

- Exporter 以独立 module 构建，配置错误不会记录密码或完整 Redis 地址。
- collector 每次 scrape 只执行一次分 section 的 `INFO`，严格解析固定字段；配置 DB 不存在时两个 DB gauge 返回零。
- `/health` 不触发 Redis 采集；错误 method、query 和 request body 被拒绝。
- 成功响应固定包含十个 metric family、正确 HELP/TYPE、`up 1`、CPU mode 与 DB label。
- 失败响应为 `503`，只包含 `gopulse_redis_up 0`，未混入上次成功值。

### 5.2 故障隔离与恢复

本次真实 `scripts/verify-exporter.sh` 使用随机隔离 project `gopulse-exporter-a18d6aac2031`，实际通过：

- 真实 Redis 7.2.5 的 INFO 数值核对和全部固定指标族校验。
- Redis stop 后有界 `503`、`up 0`、health 保持 `200`、Exporter PID 不变。
- Redis restart 后同一 Exporter 进程自动恢复 `200` / `up 1`。
- 错误密码分类为 `redis_auth_failed`，日志不含错误密码或目标地址。
- hanging TCP target 分类为 `redis_timeout`。
- SIGTERM 后进程和监听端口退出；正常成功路径删除本次 container、network、volume 与临时进程。

### 5.3 生命周期、回归与远程证据

- `scripts/dev.sh` 已加入 Exporter build/start/record/watch/stop 顺序；`verify.sh` 检查 PID identity、health 与 metrics；`down.sh` 验证记录后停止。
- 本次完整 `scripts/verify-business.sh` 通过，包含真实 Chromium、搜索 rebuild/live、Phase 2 十项可靠性矩阵以及 Phase 4 日志解析；最终日志验证为 Backend 279、Worker 27、Indexer 38、reindex 8。
- PR #58 head `31c0912bc8be9014f86fb0517913261944892365` 与 PR #60 head `0e3ec19c6bfc26db60e4886a494cd444174f0251` 的七项远程检查均为 success。
- Phase 5 两份实施记录与实际提交、版本 `1.2.1` / `1.2.2` 和合入 PR 基本一致。

## 6. 实际执行的验证

### 6.1 通过

```bash
(cd exporters/redis && go test -count=1 ./...)
(cd exporters/redis && go vet ./...)
(cd exporters/redis && go test -race -count=1 ./...)
test -z "$(gofmt -l exporters/redis)"

bash scripts/verify-exporter.sh --self-test
scripts/verify-exporter.sh
scripts/verify-business.sh

(cd backend && go test -count=1 ./...)
(cd backend && go vet ./...)
(cd frontend && npm test -- --run)
(cd frontend && npm run build)

python3 -m unittest discover -s scripts/ci -p 'test_*.py'
python3 scripts/ci/validate_versions.py
bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-business.sh scripts/verify-exporter.sh
docker compose --env-file .env.example --file deploy/compose.yaml config --quiet
git diff --check
```

结果摘要：

- Redis Exporter unit、vet、race、format 全部通过。
- Exporter self-test 与真实 Docker 成功/停止/认证/超时/恢复/退出矩阵通过。
- Backend 全 module 测试与 vet 通过。
- Frontend 9 个测试文件、46 项测试通过，typecheck 与 production build 通过。
- Python CI 24 项测试通过，版本元数据一致为 `1.2.2`。
- Bash syntax、Compose config 与 whitespace 检查通过。
- 完整业务验收通过并清理其隔离资源。

### 6.2 预期失败与 Review 复现

```bash
python3 scripts/ci/validate_branch.py \
  --branch develop/1.2.3 \
  --base-ref origin/main
```

结果：失败，原因是权威方案中没有 `develop/1.2.3` 分配，对应 P2-03。

此外执行了三个只用于复现 findings 的定向检查：

1. 以 `REDIS_EXPORTER_HTTP_HOST=[]` 启动当前二进制，`ss` 观察到 `*:PORT`，对应 P1-01；随后向该 Review 进程发送 SIGTERM，进程正常退出。
2. 对临时 Redis 容器比较 stop 前后 `.NetworkSettings.Ports`，从具体回环映射变为 `{}`，随后删除该临时容器，对应 P2-01。
3. 向当前端口判断表达式代入两个非相邻冲突组合，均被错误判定为 PASS，对应 P2-02。

## 7. 未执行、限制与后续

- 本次没有修改生产代码、测试、实施方案、实施记录或 `VERSION`，只新增本 Review 文档。
- 本次没有重新执行日常 `scripts/dev.sh → scripts/verify.sh → scripts/down.sh`，避免接管或改变可能属于用户的日常 Compose / `.run` 状态；Phase-05-02 实施记录已经记录该链路成功。本次分别独立执行了 Exporter 隔离验收与完整业务隔离验收。
- 没有增加 Redis 版本矩阵、并发压力、长时稳定性或网络故障排列；这些不属于 Phase 5 固定验收范围。
- 没有读取第三方依赖源码；findings 均由项目调用点、实际二进制、Docker 行为和仓库治理工具直接复现。
- 工作区原有未跟踪文件 `使用指南.md` 未被读取、修改、暂存或提交。

建议按以下顺序关闭：

1. 先为 `develop/1.2.3` 建立权威 Phase 5 Review closeout 分配，确保分支可合法推送和验收。
2. 修复 P1-01 的 host 归一化/校验与 wildcard 回归。
3. 修复 P2-01 的失败清理所有权模型，并加入 stop 后失败与 partial-up 失败注入。
4. 修复 P2-02 的端口唯一性检查。
5. 只重跑受影响的 Exporter unit/race、self-test、真实 Exporter 验收、脚本/治理门禁；因变更涉及共享日常生命周期与网络绑定，再补一次 `dev.sh → verify.sh → down.sh` 必要回归。

## 8. 最终结论

**Phase 5 实现 Review 结论：有条件通过（Conditional Pass）。**

真实 Redis 指标、被动拉取、目标故障隔离、同进程恢复、有界退出、独立 CI 与 Phase 0～4 回归均已得到有效证据，Phase 5 的主体产品能力成立。但 `[]` 可触发通配监听是必须优先关闭的明确网络安全边界问题；验收失败清理与端口分配也需要修复，且 `develop/1.2.3` 在推送前必须获得权威批次分配。上述条件关闭后，Phase 5 才适合作为 Phase 6 Plugin Manager 与 MetricsMonitor 的稳定输入基线。
