# Phase 6-04：集成验收与阶段收口实施方案

> 执行序号：4 / 4
>
> 前置批次：Phase-06-01、Phase-06-02 与 Phase-06-03 已完成并通过验收
>
> 总方案来源：[Phase-06-总实施方案.md](Phase-06-总实施方案.md)

## 1. 批次目标

在同一最终构建上完成 Phase 6 的三条最小端到端闭环：

```text
普通用户注册/登录
  → 当前用户 role=user
  → 运维 CLI 显式提升
  → 同一账号和会话 role=admin
  → Backend 服务端授权

已登录管理员
  → Backend 插件 API
  → Monitor 安全导入/自动启动/停止/更新/回滚
  → Redis Exporter 实际状态变化

真实 Redis
  → Redis Exporter Prometheus 0.0.4
  → MetricsMonitor 立即与周期采集
  → GoPulse metrics Envelope v1
  → HTTP 捕获端读取完整消息
```

本批是固定集成验收和阶段收口批次，不增加新角色、插件格式、管理 API、采集目标、指标或消息字段。只允许对已复现、直接阻断总方案验收的问题实施最小修复；Phase 6 完成版本固定为 `1.3.4`。

## 2. 前置条件

- Phase-06-01、Phase-06-02 和 Phase-06-03 已分别从 `develop/1.3.1`、`develop/1.3.2` 和 `develop/1.3.3` 完成、合入主远程，根版本为 `1.3.3`。
- 三批实施记录、本地固定命令、Pull Request 和远程检查结果已如实记录，未遗留阻断本批的已知失败。
- 已 fetch 主远程，从包含三个前置批次的最新 `main` 创建 `develop/1.3.4`，没有沿用前一批分支。
- WSL2 Linux filesystem 具备隔离 MySQL、Redis、RabbitMQ、Elasticsearch、Backend、Worker、Search Indexer、Frontend、Monitor、Exporter 和 HTTP 捕获端所需资源，并使用一个明确 Docker daemon。
- 开始前保存日常 Compose project、container/network/volume、端口、`.run` 进程、插件根和 Git 快照；所有破坏性验收资源都必须有随机且可验证的归属证据。

## 3. 实施范围

### 3.1 封闭管理链路验收

- 在隔离数据库注册两个用户，通过真实 `admin-role promote` CLI 只提升其中一个；两者都使用相同登录端点和 Cookie 模型。
- 普通用户对 list/get/install/start/stop/update 全部获得 `403 permission_denied`，且 Monitor 不收到被拒绝的安装包或生命周期请求。
- 管理员通过 Backend 上传真实 Redis Exporter `tar.gz`，验证有界流式转发、Manifest/digest/平台检查、固定 release/current/Registry 布局、自动启动和 health 完成后的 `201/running`。
- 验证重复 install、相同版本、降级、超限、路径穿越、链接/device、错误 digest、未知 Manifest 字段和不匹配 plugin ID 均被安全拒绝且无残留。
- 验证 start/stop 幂等、真实 PID 变化、伪造/过期 PID 记录拒绝误杀，以及 stop 超时下只对匹配进程执行强制退出。
- 对 running 插件执行更高版本成功更新，确认 current/Registry/version/新进程一致；使用无法健康启动的更高版本触发回滚，确认旧版和 running desired state 完整恢复。
- 分别在 desired running/stopped 下重启 Monitor，确认只恢复 running 插件；单插件恢复失败可查询为 failed，Monitor 仍保持 ready。

### 3.2 封闭采集与消息链路验收

- 启动带密码的真实 Redis 7.2.x、通过安装包运行的 Redis Exporter、Monitor 和带随机 Bearer token 的 HTTP 捕获端。
- 向 Redis 写入代表性 key 并执行命中、未命中与普通命令，把同一时窗的 Redis `INFO`、Exporter `/metrics` 和 HTTP 捕获 Envelope samples 对值，确认数据非静态 fixture。
- 确认 install/start/update 后立即采集，之后按配置间隔持续产生消息；引入长请求时单 target 不形成重叠 scrape 或 goroutine 增长。
- 对 `success` Envelope 校验 schema version、安全唯一 message ID、type/source、Monitor UTC timestamp、plugin/target/version/status、稳定排序 samples、Bearer token、Idempotency-Key 和 `202` 成功判定。
- 停止 Redis 后，Exporter 进程与 health 保持不变，`/metrics` 返回严格 `503/up0`，Monitor 发布唯一 sample 的 `target_unavailable`；恢复 Redis 后同一 Exporter/Monitor 进程重新发布当前 `success`。
- 对超时、超限响应、畸形/重复/非有限 Prometheus 和其他 HTTP 状态验证不生成 Envelope、不重放陈旧 samples，安全错误可通过管理状态查询。
- 停止插件时取消在途 scrape，不再向捕获端发送旧 generation 消息；再次 start 后立即用新进程产生新消息。
- 关闭捕获端或返回非 `202`，确认 Publisher 失败有界、无持久队列/无自动重试，后续采集继续；重启捕获端后新周期自动恢复发布。

### 3.3 日常生命周期与 Phase 0～5 必要回归

- 执行日常 `scripts/dev.sh → scripts/verify.sh → scripts/down.sh`，确认 Backend、Frontend、Business Worker、Search Indexer、Monitor、Monitor 所有的 Exporter 与基础设施共同运行。
- `dev.sh` 不直接启动第二个 Exporter；`verify.sh` 保持只读；`down.sh` 先由 Monitor 有界停止所有子进程，再停止 Monitor 和其他应用。
- 执行 Phase 5 Exporter 固定验收，确认被安装包和 Plugin Manager 接管后仍保留被动拉取、无历史、目标故障存活和无需重启恢复语义。
- 执行 Phase 0～4 固定业务验收一次，重点确认角色迁移、Backend 配置/路由和共享 Bash 调整不破坏认证、发帖/评论/点赞、通知、Redis 缓存、搜索、异步处理和结构化日志。
- 在正常、失败和中断路径对比日常资源前后快照，确认隔离验收不误杀进程、不删除日常 plugin root/volume，不占用遗留端口。

### 3.4 验收失败的最小修复

- 只修复总方案第 15.3 节和本文第 3 节中已复现、会使 Phase 6 验收不成立的问题；修复前保留复现命令与有限诊断。
- 修复后只重跑受影响的 unit/integration/脚本场景，最终 diff 稳定后再执行第 8 节固定完成门禁一次。
- 新角色类型、新插件、新 Manifest 字段、新 API、安装包签名、历史队列、Kafka、指标派生或管理前端不属于“最小修复”。
- 如失败来自 Phase 5 已发布契约与总方案冲突，先更新总方案并记录兼容决策，不静默改写上游语义。

### 3.5 文档、版本与远程状态收口

- 更新根 README、Monitor README、Exporter README 和必要配置说明，使管理员提升、包形式、安装布局、API、状态、采集语义、Publisher 和限制与真实行为一致。
- 核对总方案、四份拆分方案、四份实施记录、Git 历史、版本和权威分支分配；不把计划命令写为已通过。
- 将根 `VERSION`、`frontend/package.json` 和 `frontend/package-lock.json` 更新为 `1.3.4`。
- 本地门禁通过只记录本地结果；只有 Pull Request 已合入且远程门禁实际成功后，才把 Phase 6 标记为完成。

## 4. 实施边界与非目标

- 不新增、删除或重命名 role、插件管理路由、Manifest 字段、状态值、target ID、Envelope 字段或 Phase 5 指标 family，除非已证明为阻断级契约错误。
- 不实现管理页面、通用 RBAC、用户管理、远程插件市场、签名或任意插件 hook。
- 不实现 MySQL Exporter、多 target、动态发现、指标计算/聚合、本地历史或 Publisher 持久重试。
- 不实现 Message Router、Kafka、Marshaller、VictoriaMetrics、LogMonitor、EventMonitor 或可观测 Frontend。
- 不进行长时压测、高并发安装、多 OS/arch 矩阵、网络故障全排列、一般代码审查或与验收无关的机会性重构。
- 不建立应用容器镜像，不修改冻结 PowerShell，不增加 Windows runner 或原生 Windows 验收。

## 5. 预计文件与交付物

```text
dev/imple/Phase-06/Phase-06-总实施方案.md
dev/logs/Phase-06/Phase-06-04-集成验收与阶段收口.md
README.md
monitor/README.md
exporters/README.md
.env.example
scripts/verify-monitor.sh（仅验收编排或阻断修复）
scripts/dev.sh（仅阻断修复）
scripts/down.sh（仅阻断修复）
scripts/verify.sh（仅阻断修复）
monitor/**（仅阻断修复）
backend/**（仅阻断修复）
frontend/**（仅阻断修复）
.github/workflows/**（仅门禁阻断修复）
scripts/ci/**（仅治理阻断修复）
VERSION
frontend/package.json
frontend/package-lock.json
```

预计文件是允许边界，不要求制造无意义修改。如固定验收未暴露产品问题，本批应仅以验收证据、文档、版本和实施记录收口。

## 6. 详细实施步骤

1. 核对 Phase-06-01/02/03 实施记录、合入提交、远程门禁、已知限制、当前版本和日常资源快照。
2. 在最终构建上执行 Monitor、Backend、Frontend、Redis Exporter 的格式、unit、vet、race/build 门禁，确认公共 Schema 没有漂移。
3. 执行 `verify-monitor.sh --self-test`，确认非法 token、路径、archive、PID、project、container、volume 和端口均会被拒绝。
4. 执行第 3.1 节管理链路，记录普通用户 `403`、管理员安装自启、幂等启停、成功更新、失败回滚、重启恢复和归属证据。
5. 执行第 3.2 节采集链路，对照 Redis `INFO`、Exporter `/metrics`、Envelope 与 HTTP 捕获内容，覆盖成功、故障、恢复、畸形和 Publisher 失败。
6. 执行日常 `dev.sh → verify.sh → down.sh`，确认 Monitor/Exporter 所有权、启停顺序、端口和 PID 记录没有残留或双重操作。
7. 执行 Phase 5 Exporter 和 Phase 0～4 必要业务验收一次，只对已观察失败进行有限诊断和最小修复。
8. 最终 diff 稳定后执行第 8 节固定完成门禁一次，保存真实输出摘要。
9. 更新 README、总方案阶段状态、本批实施记录和 `1.3.4` 版本元数据，核对文档与真实行为一致。
10. 提交并创建 Pull Request，查询并记录真实远程检查与合入状态；未合入或失败时保持 Phase 6 未完成。
11. 合入与远程门禁通过后立即停止 Phase 6 扩展，将 Envelope 和 HTTP Publisher 契约交给 Phase 7。

## 7. 风险与控制

- **只直调 Monitor 导致授权假通过**：管理主链必须从真实登录 Cookie 和 Backend 公共 API 开始，同时验证普通用户 `403`。
- **文件存在被误当作安装成功**：必须同时验证 Manifest/digest/current/Registry、真实 PID、health 和 API running 状态。
- **回滚只恢复链接未恢复运行**：回滚后对照 current、Registry version、desired/observed state、新 PID 和旧版 health。
- **静态指标或手工 JSON 假通过**：对真实 Redis 执行可观察变化，对照 `INFO`、Exporter 和捕获 Envelope 三层数值。
- **故障恢复依赖重启**：记录 Redis Exporter 和 Monitor PID，Redis 停止/恢复前后必须一致。
- **Publisher 故障遮蔽采集或形成堆积**：验证后续 scrape 时间、goroutine/队列有界性和捕获端恢复后的新消息，不要求补发旧消息。
- **隔离验收误伤日常栈**：所有删除/停止前校验随机 project label、container ID、port、plugin root 和 PID 归属，并对比前后快照。
- **收口批次扩张**：只修复固定矩阵的已复现阻断问题，其他改进记入后续事项。
- **虚构远程完成**：本地验收、PR、checks 和合入状态分别记录，未观察到的结果保持未完成。

## 8. 固定完成门禁

最终 diff 上每项执行一次：

```bash
(cd monitor && test -z "$(gofmt -l .)")
(cd monitor && go test -count=1 ./...)
(cd monitor && go vet ./...)
(cd monitor && go test -race -count=1 ./...)
(cd exporters/redis && test -z "$(gofmt -l .)")
(cd exporters/redis && go test -count=1 ./...)
(cd exporters/redis && go vet ./...)
(cd exporters/redis && go test -race -count=1 ./...)
(cd backend && test -z "$(gofmt -l .)")
(cd backend && go test -count=1 ./...)
(cd backend && go vet ./...)
(cd backend && go test -race -count=1 ./...)
(cd frontend && npm test -- --run)
(cd frontend && npm run build)
python3 -m unittest discover -s scripts/ci -p 'test_*.py'
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/1.3.4 --base-ref upstream/main
bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-business.sh scripts/verify-exporter.sh scripts/verify-monitor.sh scripts/package-redis-exporter.sh
docker compose --env-file .env.example --file deploy/compose.yaml config --quiet
scripts/verify-monitor.sh --self-test
scripts/verify-monitor.sh
scripts/verify-exporter.sh --self-test
scripts/verify-exporter.sh
scripts/verify-business.sh
git diff --check
```

`scripts/verify-monitor.sh` 是 Phase 6 两条闭环、文件/进程/资源安全和 HTTP Publisher 的固定验收入口；`verify-exporter.sh` 保护 Phase 5 Exporter 语义；`verify-business.sh` 是角色迁移、Backend 路由和共享生命周期的 Phase 0～4 必要回归。三者职责不得用重复测试相互替代。

完整验收只在 WSL2 Linux filesystem 和可确认归属的隔离资源执行。环境缺失时不得标记完成，也不得以 mock、fixture、源码阅读或手工 JSON 替代真实 Redis、安装进程、故障恢复和 HTTP 捕获证据。

远程 Pull Request 必须通过仓库实际配置的 Branch governance、Backend、Frontend、Redis Exporter、Monitor、Scripts and Compose、Integration 和自动 PR/合并相关门禁；只能记录实际观察到的检查名称和结果。

## 9. 验收标准

- 管理员使用与普通用户相同的登录/Cookie，但只有 admin 可查询、安装、启动、停止和更新插件；普通用户全部为 `403`。
- 真实 Redis Exporter 包经 Backend 上传、Monitor 安全解包、原子安装并自动运行；无效包不越界且不留残缺安装。
- start/stop 幂等，更新保持原 desired state，运行新版失败时旧版 current/Registry/进程完整恢复。
- Monitor 重启恢复 running、保留 stopped，且不因单插件失败丧失管理 API readiness。
- MetricsMonitor 在启动后立即并按间隔采集，单 target 无重叠，stop/update/shutdown 不发布延迟的旧版消息。
- 真实 Redis 数值可端到端对应到 `success` Envelope；Redis 停止产生严格 `target_unavailable/up0`，恢复后同一 Exporter/Monitor 进程产生当前成功值。
- 超时、超限、畸形/重复/非有限指标不生成 Envelope 或陈旧数据；日志和状态只表达安全有限错误。
- HTTP 捕获端读取完整 Envelope v1，Bearer token、Idempotency-Key 和 `202` 契约正确；Publisher 失败不导致无界重试或磁盘队列。
- Monitor 不导入 Kafka SDK，不实现 Router、Marshaller、存储、LogMonitor 或 EventMonitor。
- 日常与隔离生命周期不双重启动、不误杀、不误删、不遗留进程、端口、container、network、volume、plugin root 或临时文件。
- Phase 0～5 必要能力无回归，第 8 节固定门禁与远程检查通过，四份实施记录与真实提交一致，根与 Frontend 版本为 `1.3.4`。

## 10. Phase 6 完成与停止条件

只有第 9 节全部满足、Phase-06-04 已合入主远程 `main`、远程门禁成功且四份实施记录真实完整，Phase 6 才完成。任一管理员授权、双用户态隔离、安装回滚、重启恢复、真实 Redis 数值、故障消息、Publisher 捕获、资源归属、远程检查或实施记录缺失时，不得写成完成。

阶段验收通过后立即停止。通用 RBAC、管理前端、远程插件市场、数字签名、多 Exporter/target、持久消息可靠性、指标聚合、容器化和长期运维能力记为后续事项，不继续占用 Phase 6。

## 11. Phase 7 交接

- 可独立启动、健康检查和有界关闭的 Monitor，以及经 Backend/admin 代理的插件管理契约。
- 已安全安装、由 Monitor 唯一管理、可重启恢复和更新回滚的 Redis Exporter。
- 稳定的 target ID、立即/周期采集、Prometheus 校验、成功/目标故障语义和状态时间。
- GoPulse metrics Envelope v1 完整 Schema、安全 message ID、稳定 samples 排序、敏感信息边界和真实捕获证据。
- HTTP Publisher 固定交接：`POST /internal/v1/messages`、`application/json`、Bearer token、`Idempotency-Key=message_id` 与 `202 Accepted`。
- 明确保留给 Phase 7 的工作：正式 Message Router 服务、类型识别、Topic 选择、Kafka Producer、验证 Consumer、路由失败语义和消息传输闭环。
