# Phase-12-03：全栈 Compose 验收与阶段收口实施记录

## 1. 批次信息

- 日期：2026-09-06
- 分支：`develop/1.9.3`
- 开工基线：最新 `upstream/main` 提交 `1e63bdd`，产品版本 `1.9.2`
- 前批远程状态：Phase-12-01 已完成；Phase-12-02 由 Pull Request #106 于 2026-09-06 合入，权威远程运行 `34010783067` 成功
- 目标/本地完成版本：`1.9.3`
- 实施方案：`dev/imple/Phase-12/Phase-12-03-全栈Compose验收与阶段收口.md`
- 当前结论：分支已推送；本地实施、固定门禁与定向修复后的完整 Compose 矩阵通过。首次远程运行 `34013446831` 的 9 个非全栈 jobs 成功，Full-stack Compose job 失败且自动 PR 跳过；修复待远程复验，因此未标记 Phase 12 完成

## 2. 实际完成

### 2.1 唯一完整 Compose 入口

- 无参数 `scripts/verify-compose.sh` 现在默认执行 Phase 12 唯一权威全栈矩阵；显式 `--full` 等价。
- `--business` 保留为 Phase-12-01 聚焦业务诊断；`--observability` 为兼容别名并转入同一完整矩阵，不再形成第二套阶段门禁。
- 完整 runner 先解析当前环境中的 Docker、Git 与基础文本工具，再为每次验收创建只含这些精确工具软链接的临时 allow-list PATH，并确认宿主 Go/Node/npm/Python 与数据客户端均不可见；浏览器/API 客户端继续来自 one-shot acceptance image。初始化阶段另设 early cleanup trap，受限 PATH 建立失败也不会遗留临时目录。
- 随机 `gopulse-accept-<token>` project 使用临时 `0600` env、随机 Frontend/Backend loopback 端口和新命名卷，不读写日常 `.env`、`.run`、project 或卷。
- 开工快照扩展到 Git working tree、container、network、volume 与非本批精确版本镜像；正常、失败和 signal trap 均以 project/working-directory/config-file label 做强归属清理并复核快照。

### 2.2 镜像、拓扑、身份与输出边界

- 从当前版本与执行时 Git revision 构建 Frontend、Backend、Business Worker、Search Indexer、Router、Marshaller、Monitor、Redis Exporter 和 acceptance 镜像。
- 对八个产品镜像验证 tag 与运行容器 image ID、OCI version/revision/source、数字 UID:GID、entrypoint/cmd、StopSignal、daemon/OCI architecture 映射、非空 layers、最终运行内容，以及 image config/history 无运行凭据。
- 核对 Frontend bundle 无 sourcemap、内部 service 地址或身份变量；Backend 镜像包含 server/migrate/search-reindex/admin-role 且无 Go/Node/source tree；其余 Go 运行镜像无 toolchain/source tree。
- 对全部自研长运行容器验证非 privileged、无附加 capability、无 host network/PID/IPC、无 Docker socket；Router/Marshaller/Monitor rootfs 为只读，Monitor 仅挂载当前 project 的 `monitor_plugin_data` 命名卷。
- 核对 edge/business/observability 网络成员与默认发布端口；Frontend 不能解析 Monitor，Backend 内部验证 Monitor/Router/Marshaller 的无 token、错误 token 和 Cookie-only 请求均为 `401`，VictoriaMetrics 受保护查询的无/错误 Basic 身份为 `401`，正确服务身份为 `200`。
- Monitor bootstrap 同时核对 version、desired/observed running，并通过 `/proc/*/exe` 确认只有一个受管 Redis Exporter 进程。

### 2.3 同 project 跨批业务与可观测矩阵

- 冷启动六个官方基础设施、八个自研长运行服务和 migrate/search-init/kafka-init 三个作业；所有必需 health、running 与 exited/0 状态通过。
- 幂等重跑 migration、search initialization 与 Kafka Topic initialization。
- 同一真实浏览器 project 完成普通用户帖子、评论、点赞、通知、登录/退出、搜索；完成 Redis 停止时 MySQL 回退、暂停 Worker 后通知收敛、暂停 Indexer 后搜索收敛。
- 注册普通用户和待提升管理员，通过容器化 `admin-role` 提升；普通用户无管理导航、直达管理路由无内部请求、Metrics/Logs/Events/Exporter API 均为 `403`。
- 管理员完成社交写入、真实 Metrics/Logs/Events 查询和受管 Exporter stop/start；浏览器请求仅访问 Frontend origin。
- 分别停止 VictoriaMetrics、Monitor 与 Router；每个窗口内代表社交写入和 Backend readiness 继续成功，受影响能力准确局部降级，原服务恢复后继续使用，无需重建 Frontend/Backend。

### 2.4 替换、信号、持久化和空卷管理

- 替换 Backend、Business Worker、Search Indexer、Monitor、Marshaller，并复核事实、健康与 Monitor 单一受管进程。
- 替换 Redis、VictoriaMetrics、Kafka、Elasticsearch；Kafka Topic 作业重跑，业务、插件、Metrics/Logs/Events 和持久事实继续可读。
- 对 Frontend、Backend、Worker、Indexer、Router、Marshaller、Monitor 逐一执行有界 stop/start，原容器退出码均为 `0`，恢复后 production SPA/API smoke 成功。
- 整个 project 执行保留卷 down/up，确认 MySQL、Redis、RabbitMQ、Elasticsearch、Kafka、VictoriaMetrics、Monitor plugin 七个卷仍存在，重新运行三个幂等初始化，复核业务/通知/搜索、三类可观测查询和插件 desired state，并在重启后新增社交操作和可观测证据。
- 独立 Redis Exporter profile 通过真实 Redis、`up 0`、认证失败、同进程恢复与 SIGTERM；随后删除当前随机 project 的卷，以空 plugin state 冷启动并通过浏览器完成 install/stop/start/update、Metrics 与 Events。
- 最终随机 project `gopulse-accept-6d59b046cd8a` 的 container、network、volume 与临时 env 全部清理；开工 Git 与无关 Docker 资源快照保持。

### 2.5 日常生命周期与 CI 收敛

- 随机 project `gopulse-lifecycle-790ec47624` 完成 `dev.sh --no-build → verify.sh → down.sh（保留卷）→ dev.sh --no-build → verify.sh → down.sh --volumes`。
- 首次 `verify.sh` 前后 MySQL 用户/帖子计数均为 `0/0`，证明 smoke 未创建业务事实；第二次启动复用七个命名卷并再次通过只读验证，最后强确认删除该 project 卷。
- GitHub Actions 将原 `compose-observability` 和 `compose-business` 双真实矩阵收敛为一个 `compose-full-stack` job，运行无参数 `scripts/verify-compose.sh`。
- Router、Marshaller、Monitor、Redis Exporter 的历史真实 acceptance step 与独立 Logs/Events real jobs 不再在每次 CI 重复；其 format/unit/vet/race、Frontend、scripts/Compose、Backend integration 与唯一全栈矩阵仍保留。

## 3. 文件变更

- 版本/环境：`VERSION`、`.env.example`、`frontend/package.json`、`frontend/package-lock.json`
- 完整验收：`scripts/verify-compose.sh`、`scripts/verify-compose-observability.sh`、`frontend/e2e/compose-observability.spec.ts`
- CI/治理测试：`.github/workflows/quality-gates.yml`、`scripts/ci/test_auto_pr_workflow.py`、`scripts/ci/test_verify_business.py`
- 使用与组件文档：`README.md`、`backend/README.md`、`monitor/README.md`、`router/README.md`、`marshaller/README.md`、`exporters/redis/README.md`
- 方案状态：三份 Phase 12 拆分方案、`dev/imple/Phase-12/Phase-12-总实施方案.md`
- 本实施记录：`dev/logs/Phase-12/Phase-12-03-全栈Compose验收与阶段收口.md`
- 未修改、未暂存用户已有未跟踪文件 `使用指南.md`

## 4. 验证命令与结果

### 4.1 实施期定向检查

```bash
(cd frontend && npx tsc --noEmit --pretty false)                                      # PASS
python3 -m unittest scripts.ci.test_auto_pr_workflow scripts.ci.test_verify_business # PASS，12 tests
python3 -m unittest discover -s scripts/ci -p 'test_*.py'                             # PASS，27 tests
python3 scripts/ci/validate_versions.py                                               # PASS
python3 scripts/ci/validate_branch.py --branch develop/1.9.3 --base-ref upstream/main # PASS
bash -n scripts/verify-compose.sh scripts/verify-compose-observability.sh             # PASS
docker compose --env-file .env.example --file deploy/compose.yaml config --quiet      # PASS
scripts/verify-compose.sh --self-test                                                  # PASS，拒绝 6 个不安全 project name
git diff --check                                                                       # PASS
```

### 4.2 权威全栈矩阵

```bash
scripts/verify-compose.sh
```

最终结果：PASS。随机 project `gopulse-accept-6d59b046cd8a` 完成镜像重建、冷启动、三作业幂等、双使用态业务、管理员三链路/Exporter、内部身份、三类局部故障、应用/持久服务替换、七个应用信号关闭、保留卷 down/up、重启后新数据、独立 Exporter 与空卷管理流，并由 trap 清理全部强归属资源。

实施期三次前置运行暴露并修复的均为验收脚本假阴性，而非产品缺陷：

1. Docker daemon 报告 `x86_64`，OCI image 报告 `amd64`，增加规范化映射；
2. Monitor 的 project-scoped named volume 被初版断言误判为不允许的 bind，改为按 mount type/name/destination 精确允许唯一插件卷；
3. VictoriaMetrics `/health` 为公共 liveness，不能证明 Basic 身份，负测改用受保护 `/api/v1/query`。

每次失败均由 trap 删除对应随机 project；最终成功后未因会话或文档更新重复执行未受影响的完整真实矩阵。

首次推送触发远程运行 `34013446831`：Branch governance、Backend、Router、Marshaller、Monitor、Redis Exporter、Frontend、Scripts and Compose、Integration 共 9 个 jobs 成功；Full-stack Compose job 在 2026-09-06 05:13:38Z 于镜像构建前失败，自动 PR step 被跳过。根因为初版直接固定 `PATH=/usr/bin:/bin`，而 GitHub runner 的 Docker CLI 不保证位于该目录。修复改为在收敛 PATH 前解析当前环境中的 allow-list 工具绝对路径，并在临时目录创建软链接；仍显式拒绝 Go/Node/npm/Python/MySQL/Redis/RabbitMQ/Kafka/curl 客户端。修复后随机 project `gopulse-accept-64d812c5570b` 重新通过完整矩阵并清理全部资源。

### 4.3 日常生命周期

```bash
scripts/dev.sh --project-name gopulse-lifecycle-790ec47624 --env-file <temporary-env> --no-build
scripts/verify.sh --project-name gopulse-lifecycle-790ec47624 --env-file <temporary-env>
scripts/down.sh --project-name gopulse-lifecycle-790ec47624 --env-file <temporary-env>
scripts/dev.sh --project-name gopulse-lifecycle-790ec47624 --env-file <temporary-env> --no-build
scripts/verify.sh --project-name gopulse-lifecycle-790ec47624 --env-file <temporary-env>
scripts/down.sh --project-name gopulse-lifecycle-790ec47624 --env-file <temporary-env> --volumes --confirm-project gopulse-lifecycle-790ec47624
```

结果：PASS。首次 verify 前后用户/帖子计数保持 `0/0`；默认 down 保留七个卷，第二次启动和 verify 成功，最终显式清理卷。

### 4.4 固定完成门禁

实施方案第 9 节固定命令已在最终运行 diff 上全部通过：

- Backend format、全量 unit、vet 与指定 config/http/logship/exporterplugin/metricquery/worker/search race：PASS；
- Monitor format、全量 unit、vet 与指定 config/plugin/metrics/events race：PASS；
- Router format、全量 unit、vet：PASS；
- Marshaller format、全量 unit、vet 与指定 config/consumer/VictoriaMetrics/Elasticsearch race：PASS；
- Redis Exporter format、全量 unit、vet、全量 race：PASS；
- Frontend Vitest：`11` files / `58` tests PASS；typecheck 与 production build PASS；
- scripts CI unittest：`27` tests PASS；版本、分支、Bash syntax、Compose render、self-test 与 `git diff --check` PASS。

完整真实矩阵的成功结果沿用第 4.2 节，因为其后仅更新 README、方案状态和本实施记录，未改变脚本、Compose、镜像输入、应用实现或执行环境；按执行效率规则不重复已成功门禁。自动提交完成后按最终 Git HEAD 重新构建八个产品镜像并核对 OCI revision label，不把 dirty-tree revision 当作最终制品证据。

## 5. 与方案的偏差

- 没有新增 Dockerfile、service、端口、网络、配置模式、API、页面或持久类型，也没有修改产品 Go/Vue 实现。
- 为兼容既有调用，完整 runner 继续使用历史文件名 `verify-compose-observability.sh`；外部权威入口已统一为无参数 `verify-compose.sh`，`--observability` 只是兼容别名。
- 初版计划要求记录浏览器制品快照；acceptance image 不 bind mount Playwright output，运行后宿主无新增浏览器制品，因此以 Git working tree、容器挂载和请求 origin 证据证明隔离，没有保留宿主截图/trace。
- 未执行独立代码/架构 Review、容量测试、供应链审计、多架构构建或 Kubernetes 工作，符合非目标与停止条件。

## 6. 已知限制与后续事项

- 分支已推送，且首次远程运行已有部分事实证据；但 Full-stack Compose job 未通过、自动 PR 未创建，后续远程复验、Pull Request、合入和最终 Phase 12 完成状态仍必须在真实发生后单独更新，不能由本记录预判。
- 本地运行架构为 `amd64`；脚本接受 daemon `x86_64→amd64` 与 `aarch64→arm64` 的等价映射，但未把完整多架构发布作为门禁。
- Compose 仍是单节点开发/验收拓扑，不提供 TLS、SASL、高可用、容量或生产供应链保证。
- `--business` 与历史 component scripts 仅保留定向诊断价值；后续不应重新把它们并列为阶段完成门禁。

## 7. Phase 13 交接输入

- 已验证的 Frontend、Backend、Business Worker、Search Indexer、Router、Marshaller、Monitor、Redis Exporter image tag/OCI user/entrypoint/cmd/StopSignal/architecture 契约。
- 可映射为 Kubernetes Job/init 的 migrate、search-init、kafka-init 与 Monitor package bootstrap 幂等完成条件。
- edge/business/observability 网络成员、service DNS、仅 Frontend/Backend 用户面发布、Bearer/Basic 内部身份、七类持久卷和 liveness/readiness 语义。
- Monitor 在无 Docker socket/特权条件下持有唯一受管 Exporter 子进程并从 plugin volume 恢复 desired state 的运行边界。
- 同一身份系统下普通用户社交隔离、管理员社交与 Metrics/Logs/Events/Exporter 闭环，以及 VM/Monitor/Router 局部故障不新增 Backend readiness 依赖的事实。
- 应用/数据容器替换、整栈保留卷 down/up、重启后新业务/可观测数据、强归属清理矩阵，作为 Phase 13 行为等价验收基线。
