# Phase-14-06：Compose 集成验收与阶段收口开发记录

> 合入后状态（2026-09-12）：本批已通过 `d16ed4a03051658903b43e83e4f6b4cdd97ccf78` 合入主线，完成版本 `1.11.6`。Review 报告 `dev/review/2026-09-12-Phase-14实现Review报告.md` 发现的三项问题由 Phase-14-07 整改；已合入不等于复核无缺陷。下文“待合入”及交接结论均为第六批合入前历史记录；后续交接应包含第七批整改，结果见其开发记录。

## 合入前历史状态

**本批已完成实现与验收，完成版本 `1.11.6`，开发分支 `develop/1.11.6`，待合入主线。** 完整运行时门禁通过后，最终库存复核发现并修复迁移旧卷清理缺口；修复的真实清理与归属拒绝回归均通过。Phase 14 主线里程碑仍须本批合入后才成立。下文早期状态与失败保留为历史事实。

## 开工与范围

- 从远程 `origin/main` 创建 `develop/1.11.6`。本机 Git 代理端口不可用，使用仅对该命令有效的无代理环境成功 fetch，再快进到最新 `origin/main`；没有改动全局代理配置。
- origin 与 upstream 指向同一仓库，已分别 fetch；最新主线为 `c544eed`，包含前五批和对应实施记录，根版本为 `1.11.5`。
- 开工前未跟踪文件 `~` 属于既有状态，不修改、不提交。
- 仅补最终 Compose 验收接线、真实阻断修复和阶段记录。前五批 package 成功证据输入未变时直接沿用，不重复全量 package 验证。
- 最终待验证：真实旧卷迁移、空卷管理员流程、六插件和六组件查询、同卷恢复、代表性故障、Phase 13 回归、平台合同及强归属清理。

## 首轮接线与预检查

- 新增 `--phase14` 唯一入口，串行执行完整平台/Phase 13 门禁与新跨批迁移、冷启动、组件、故障验收；拒绝 `--keep`，不跳过清理。
- 新增当前六插件空卷浏览器场景和跨批 runner；沿用既有资源归属、账号调和、组件指标及 endpoint 故障实现。
- 已通过 Bash 语法、Python 编译、三个规定的 `--self-test`、版本元数据一致性和 `git diff --check`。
- 分支版本校验当前按预期失败：批次尚未验收，完成版本仍为 `1.11.5`，不得预先提升为 `1.11.6`。最终验收后必须再次通过该门禁。
- 已实际成功构建隔离 `gopulse/monitor-acceptance:p1406-probe`，当前 Docker 构建环境可用；不是完整门禁成功证据。
- 全栈验收要求干净构建源码。为保留用户既有未跟踪 `~`，先提交接线，再在同一提交的独立 detached worktree 执行固定入口；不移动或删除用户文件。主工作区 `.run/phase1406/` 保存开工资源、端口与 Git 快照。

## 接线修正（未声称完整通过）

- 首轮实际 `--phase14` 项目 `gopulse-accept-c04609ad2d9d` 已完成标准 Dockerfile 构建、Frontend tests/typecheck/build、7 项 Phase 13 浏览器测试及管理员场景；完整门禁仍在执行。
- 组合入口原本会在 Phase 13 与平台验收段各注册一次同名管理员。修正为组合运行只注册一次；独立原有模式不变。
- 现有 plugin update 明确要求更高版本，因此空卷浏览器不能上传同版本 current 包。为 acceptance-only Docker stage 增加 `ACCEPTANCE_UPDATE_VERSION` 构建参数，默认保留原 `1.11.5`，本批显式构建受信 `1.11.7` 成功包；失败包仍为受信 `1.11.90`。不改变生产 current/legacy 信任或运行时授权。
- 补充真实 legacy archive 固定 SHA-256 校验和当前包 digest 记录；`1.10.6/linux/amd64` 为唯一已证实的旧包来源。
- 延用前五批未变化证据：Phase-14-01 的 prepare/active 中断及 archive/Registry 拒绝、Phase-14-02 的精确账号权限、Phase-14-03 的真实 Kafka 临时 follower 拓扑与 offset 及 ES primary 聚合、Phase-14-04 的九项 VM 映射、Phase-14-05 的组件目录/预算及持久化边界 package 测试。最终只重新验证跨批 Compose 和接线变化。

- 首轮完整命令最终退出 1，未进入新的跨批 Python runner：Phase 13 套件中的 `compose-business.spec.ts` 已注册业务账号，随后原平台段重复运行同一注册场景失败。此为新组合入口的 fixture 重复，不是产品业务失败。改为组合模式只在原平台段运行该场景，独立 `--phase13` 行为保留；管理员场景同样仅执行一次。已通过部分不冒称完整成功。失败项目已清理。


## 最终门禁与真实清理修复

- `bash scripts/verify-compose.sh --phase14` 第二次运行退出 **0**，输出 `.run/phase1406/final-compose-2.log`。冻结源码为 `aaf7747`；在独立 detached worktree 运行，前后 `git status --porcelain` 均为空。
- 完整平台与 Phase 13 项目：`gopulse-accept-81d5cf3c48ce`。包含标准镜像构建、Frontend 17 文件 / 72 tests、typecheck/build、六项 Phase 13 浏览器测试、完整业务与管理员/普通用户场景、依赖故障、服务替换、SIGTERM、持久化、独立 Exporter 和空环境管理流程。
- Phase 14 跨批项目：`gopulse-p1401-652faf4f2dd5`。产品、Browser acceptance 与 Monitor acceptance 采用唯一标签 `p1406-652faf4f2dd5`，OCI version `1.11.6`；版本文件在运行门禁成功后才提升。所有结果见 `.run/phase1406/gopulse-p1401-652faf4f2dd5/closure-evidence.json`。
- 该次 runner 的基础 cleanup 曾打印清理成功，但随后额外清单复核发现 `monitor_plugin_data` 与 `p14_stopped` 两个旧迁移卷仍残留：它们已不在最终挂载图，当前 Compose `down --volumes` 没有删除它们。**不把首次 cleanup 输出作为最终清理证据。**
- 修复仅限 `verify_phase14_closure.py` 的 cleanup：列出本项目标签的残留卷，先对完整删除集合核对随机 project、名称前缀、Docker label 与开工前快照，全部通过后逐个删除；不使用 prune，不允许删除任何既有卷。镜像清理也不再忽略真实删除失败。
- 实际调用同一个 `cleanup_orphan_volumes` 函数清掉上述两个卷；新增的两个最小回归分别证明真实缺陷对应的允许删除路径、以及集合内任何卷属于既有快照时整个删除操作在第一笔删除前拒绝。
- 最终清单一致性通过：**35 个既有容器、10 个既有网络、220 个既有卷** 完整保留；两个 full-stack 项目与一个跨批项目均无残留容器/网络/卷；唯一测试镜像与 probe 镜像标签已移除。监听端口前后 `cmp` 一致，原未跟踪 `~` 保留。
- 清理修复不改变产品代码、Browser/API 场景、镜像构建参数或故障矩阵，故保留已成功的完整运行时证据，仅执行受影响的实际清理、两个归属回归和最终库存检查，没有重复整套业务/故障门禁。

## 实际交付与验收结果

| 合同 | 本次实证 |
| --- | --- |
| Redis v1 迁移 | 对锁定 `1.10.6/linux/amd64` 包校验 SHA-256；真实 running/stopped 卷迁移保留 version/installed_at/updated_at/desired_state；显式 v2 更新至 `1.11.6`，原业务帖子及迁移前后同范围历史点可读，重启仅一个记录 |
| stopped 更新 | 停止状态 v2 update/configuration 后仍 stopped，runtime process 文件不存在 |
| 空卷 Browser | 未 bootstrap；六类均可见，Redis Schema connection-test、install/start/stop、受信 `1.11.7` update、指标查询通过；提交后 Secret 输入和 DOM 清理 |
| 空卷其余五类 | MySQL/RabbitMQ 通过既有专用账号调和与重复幂等；Kafka 使用已由正式 Marshaller 消费建立的 offsets；Kafka/ES/VM 同契约安装；六个 up=1 及真实运行值可从 Backend 查询 |
| 同卷替换 | 六个 active revision、Secret 哈希与 release 恢复；ES 保持 stopped 且无进程，再显式 start；六种唯一可执行进程归属与新指标均成立 |
| 六组件 | Backend、Worker、Indexer、Monitor、Router、Marshaller 各一个处理结果与一个进度/最近成功 family 查询通过；另查 Monitor 自身 scraped identity 与 Router/Marshaller message_source/stage |
| 认证和低基数 | 六内部端点未认证/错误/重复 Authorization 拒绝，公共端口不提供 metrics，端口不发布；38 component families 查询预算、固定来源/producer、非法 metric/label/range、普通用户管理/catalog/query 拒绝通过 |
| 非存储目标故障 | 停 Redis 后 up=0，其他五种有新采样/查询点，业务缓存降级读取成功；恢复时六 Exporter process records 不变 |
| 单进程故障 | SIGKILL VM Exporter，安全 observed_state=failed，其余五进程记录不变且继续采集；受控 start 恢复唯一进程 |
| 可信失败包 | acceptance-only `1.11.90` VM 包经过原信任验证后 trial 返回 422，active revision 不变，随后六种重新有新值；无运行时 trust bypass |
| Router 故障 | Router 停机期间六 Exporters 与 Monitor 内部 endpoint 继续运行、业务事实可读；恢复后六 source 均有新采样和新查询点。未新增队列，沿用同步、有截止时间的 HTTP publisher 合同 |
| 单组件 endpoint 故障 | 仅 owned proxy 使 Backend metrics 返回 503；Backend ready/业务正常，另外五组件和六插件成功时间继续推进；恢复发布成功 |
| VM 存储故障 | Backend metrics 503、Marshaller dependency=0；Backend ready=200，故障期新帖搜索/评论通知继续；恢复后消费者故障期进度可查询且 dependency=1。不将不可查询期伪造为持久化 up=0 |
| 消费者退出 | Worker SIGTERM 0.40s、Indexer 0.29s，退出码 0、旧 PID=0、旧端点关闭；替换后端点及新消费事实恢复 |
| 业务/平台回归 | 资料/用户搜索、关注/Following/通知、收藏、帖子/评论/点赞、编辑/缓存/搜索、删除/墓碑/防复活，UserAppShell/AdminLayout、Metrics/Logs/Events、单 Topic/offset、镜像/网络/端口/read-only/numeric user/signal/volume 均由本次组合门禁及前批未变证据覆盖 |
| 脱敏 | 公共插件 DTO、catalog、Events、结构化日志、active/revision 文件和组件指标的候选凭据/内部 token 扫描通过；Browser Secret 清理通过 |

### 六插件实际代表值

| source | family（`gopulse_<source>_` 前缀） | 真实值 |
| --- | --- | ---: |
| redis | used_memory_bytes | 1069632 |
| mysql | uptime_seconds | 108 |
| rabbitmq | connections | 3 |
| kafka | brokers | 1 |
| elasticsearch | documents | 45 |
| victoriametrics | storage_rows | 5652 |

### 六组件查询与预算

| component | 处理结果 / 进度 family | 实际 endpoint / queried series | 上限 |
| --- | --- | --- | ---: |
| backend | http_requests_total / outbox_last_publish_success_timestamp_seconds | 61 / 61 | 547 |
| business-worker | messages_total / last_success_timestamp_seconds | 17 / 17 | 37 |
| search-indexer | messages_total / last_success_timestamp_seconds | 12 / 12 | 24 |
| monitor | scrapes_total / last_scrape_success_timestamp_seconds | 64 / 64 | 112 |
| router | messages_total / last_kafka_ack_timestamp_seconds | 68 / 68 | 112 |
| marshaller | records_total / last_storage_success_timestamp_seconds | 136 / 136 | 260 |

## 固定检查与补充检查

| 实际命令 | 最终结果 |
| --- | --- |
| `bash scripts/verify-compose.sh --self-test` | 通过 |
| `bash scripts/verify-plugin-metrics.sh --self-test` | 通过 |
| `bash scripts/verify-component-metrics.sh --self-test` | 通过 |
| `bash scripts/verify-compose.sh --phase14` | 第二次退出 0；完整运行时通过，额外发现的旧卷清理缺口已按上文修复并独立实证 |
| `bash scripts/verify-compose.sh --phase14 --keep` | 按合同拒绝，未触及 Docker |
| `python3 -m unittest discover -s scripts/ci -p test_phase14_cleanup.py` | 2 passed |
| 同一 cleanup 函数删除真实残留 + 全部既有资源库存比较 | 通过，见 `cleanup-verification.log` |
| `bash -n scripts/verify-compose.sh scripts/verify-compose-observability.sh` | 通过 |
| `python3 -m py_compile scripts/ci/verify_phase14_closure.py` | 通过 |
| `(cd frontend && npm run build)` | 版本提升后的 typecheck/release build 通过；未因仅版本文本变化重复业务测试 |
| `python3 scripts/ci/validate_versions.py` | 根、Frontend、lockfile、示例环境为 `1.11.6`，通过 |
| `python3 scripts/ci/validate_branch.py --branch develop/1.11.6 --base-ref upstream/main` | 通过 |
| `git diff --check` | 通过；最终暂存与提交范围检查在提交收口时执行 |

前五批未变 package 证据按其同名记录继承，不重跑全量 Go 测试、不阅读第三方实现、不产生独立 Review 报告。

## 文件范围、偏差与 Phase 15 交接

- 接线：`scripts/verify-compose.sh`、`scripts/verify-compose-observability.sh`、`scripts/ci/verify_phase14_closure.py`。
- 验收：`frontend/e2e/phase14-closure.spec.ts`、`scripts/ci/test_phase14_cleanup.py`、`deploy/docker/observability.Dockerfile` 的 acceptance-only 成功版本参数。
- 元数据：`VERSION`、`.env.example`、`frontend/package.json`、`frontend/package-lock.json`。
- 记录/状态：本记录、Phase 14 总方案与第六批方案的实际完成/待合入状态。
- 未修改前五批生产 Go 实现；未新增插件、指标 family、产品权限或业务能力。
- 偏差：采用独立 detached worktree 保持冻结构建 revision 和保护既有 `~`；最后的真实清理缺口通过最小补充回归关闭，而非重跑已成功的全部运行时。acceptance 成功包需高于当前版本，因此使用构建时登记的 `1.11.7`，不是关闭信任或任意现场包。
- 实际环境为 Linux amd64、Bash、Compose。没有声称 macOS/Windows 支持，没有运行 Kubernetes，没有把 Milestone 4 提前标为完成。
- Phase 15 可依赖六插件固定 `<source>-exporter` ID / `<source>-exporter-local` target、六组件 `<component>-local` target、Backend 固定 catalog 和低基数 labels；详细 family/labels/budgets 继续以服务端目录及 `docs/component-metrics.md` 为准。
- 采样成功/发布成功/存储可用性是不同事实：规则评估不得把 Router 停机的新值缺失或 VM 不可查询当成真实 target up=0。沿用安全实时状态、历史保留、恢复后新值与 Backend unavailable 语义。
- Secret/连接串/高基数业务标识不得进入公共 DTO、指标 labels 或浏览器内部连接；数据库实时管理授权与用户端隔离不降低。无遗留本批阻断；主线阶段完成仅待合入，Phase 15 从合入后的 `1.11.6` 开始。


## 实际制品标识

以下 current 包与镜像标识来自实际门禁证据。成功/失败 acceptance 包哈希在相同冻结源码 `aaf7747`、相同 Dockerfile/构建参数下从缓存镜像提取；成功包同时与 Browser 实际上传的存档逐字节 SHA-256 对照一致。此步骤仅补齐制品清单，没有重跑业务验收。

| 包 | 版本 | SHA-256 |
| --- | --- | --- |
| redis current | 1.11.6 | `39c26e4170905c30a602a805cffc783b7b6ad1de01ee1edda1e3238c888ecda1` |
| mysql current | 1.11.6 | `eb61dd94684ceafb7f239a67fe40a7dea1f1e795b68958194cd3ef706259acc7` |
| rabbitmq current | 1.11.6 | `100555ab7120d055ef342ae4b79c8f2d880c2ffedac211566ba0d73edefc942f` |
| kafka current | 1.11.6 | `cb904c31d7fa080166eec32b4ebe9a5590b50a901bbba3873cade4dca35e3fd0` |
| elasticsearch current | 1.11.6 | `c321f73a7ec38a659ccccc0270ce2230f4029ecf79998730bada213141468351` |
| victoriametrics current | 1.11.6 | `10155c625434a095af8ed9a1bf2e55d99cd9a1a89fe8ce8ccd79d0ab01fe86f9` |
| Redis legacy-v1 | 1.10.6 | `b992b0dfa80a0983b9af63e4c2a4770216bfd7fcb718af2cd451281cf3306727` |
| redis-update.tar.gz（acceptance only） | 1.11.7 | `b72b37b3f4925775c3732d0be38e411842055f8ec3e04843cfa8252bc134b295` |
| victoriametrics-failure.tar.gz（acceptance only） | 1.11.90 | `54032756b25903ce72cdda2218049d054b0c55138265e086b9140f392159ec92` |

| 实际验收镜像（唯一标签 `p1406-652faf4f2dd5`） | image ID |
| --- | --- |
| gopulse/backend | `sha256:01613c611a3c80c82122bbe6b23266edbb91718657f3d7f3e75838ccc9b9fe47` |
| gopulse/business-worker | `sha256:81b442f9115971178e6fc28ab822fb379d583d8899ee4478b5e6fc54191cec7c` |
| gopulse/search-indexer | `sha256:42d39f630b39e976139ca41aa6eb9317002e08be1ee83eebd77684eab83532c6` |
| gopulse/monitor | `sha256:8b502a78fd2f08b7285f2afa2bae47eb41a4dfda3a5f5e8ee0c5e04176aa3644` |
| gopulse/router | `sha256:74045120a614cb7d66d0fee278b4410a8b8d4fac240db2860f5680225256f085` |
| gopulse/marshaller | `sha256:7ff08173e99d6e64d1e8553b1af71a6d868fdbd2d645fc1e7061583f9f114006` |
| gopulse/frontend | `sha256:d0d2e0dd2143b736ead1ce6f075286ee7eea9f9bab8aeeb9d465f82842a91680` |
| gopulse/acceptance | `sha256:11b11d2f0f2b13daafda979ceca7c489b5b3b94ea881e7c0bf9266148bd76dcf` |
| gopulse/monitor-acceptance | `sha256:c0dc0d45c5864c1dba3cf9d53c0d854744cf5af937f627386d5e5914f4f14454` |

## 最终提交收口

- 最终 `git diff --check`、`git diff --cached --check` 通过；只暂存本批文件，既有未跟踪 `~` 不在提交中。
- 制品哈希提取用的临时容器/镜像亦已删除，再次核对 35 / 10 / 220 原资源库存一致；独立 worktree 验收后无源码变更，证据复制至主工作区 `.run/phase1406/` 后移除该临时 worktree。
- 版本/分支门禁与版本更新后的 Frontend release build 均退出 0。没有未执行的本批必需门禁，也没有遗留清理或产品阻断。
- 推送不等于合入。Phase 14 主线完成仍以远程 main 实际包含本批、VERSION 为 `1.11.6` 为条件。
