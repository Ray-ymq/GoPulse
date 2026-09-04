# Phase-09-03：集成验收与阶段收口实施记录

- 实施日期：2026-09-04
- 目标版本：`1.6.3`
- 开发分支：`develop/1.6.3`
- 基线：`origin/main` / `upstream/main` at `58c0d167168f88e4efaefa2f5be1a49595f7f0c2`，基线版本 `1.6.2`
- 完成状态：本地固定门禁与真实隔离验收通过；远程 quality gates 全部成功，Pull Request #85 已于 2026-09-04 squash 合入 `main`

## 1. 实际完成内容

### 1.1 阶段级真实日志闭环

- 在 WSL2 Linux filesystem 和唯一 Docker daemon 上，以随机项目、随机 loopback 端口、随机凭据和临时目录重新执行完整 `scripts/verify-logs.sh`。
- 真实注册请求 `19b4846660d21504ee5683b265c0216f` 同时查询到 `user registered` 与 `http request completed`；真实参数错误请求 `f4c96d2bef5297825928baab2cc63ada` 返回安全 `400 validation_failed`，并通过 `from/to`、service、module、level、message、request ID、error code 和 limit 的组合查询命中对应日志。
- 真实帖子事件 `d01fc6cc-7672-4631-802b-2b6cdd3bc1b3` 同时查询到 Backend Outbox 与 Search Indexer 日志；真实评论事件 `0f373a5c-f19f-49ea-b518-5d2bee5f3c20` 同时查询到 Backend Outbox 与 Business Worker 日志；search-reindex 的 started 与 completed/skipped 生命周期日志均可由 admin 查询。
- 无 Cookie 与普通用户 Cookie 分别返回 `401`、`403`；管理员在实时数据库角色授权后成功查询。既有定向 HTTP 测试继续证明前两类拒绝不会调用日志 application/repository。

### 1.2 查询、分页与安全返回矩阵

- 扩展 `scripts/verify-logs.sh`，在真实 Elasticsearch PIT 上生成并遍历 8 页、16 条固定过滤结果；验证按时间降序、无重复、游标请求只携带 cursor，第一页的实际时间窗、filters 与 limit 在签名游标内保持固定。
- 真实拒绝篡改 cursor、cursor 与其他参数混用、未知参数、空参数、通配符、重复参数和超过 24 小时时间窗，均返回安全 `400 validation_failed`；合法但不存在的 request ID 返回空 `data` 与 `next_cursor=null`。
- 查询 DTO 只允许 canonical 字段；真实 admin 响应不包含 `_index`、`_id`、`_score`、PIT、Envelope、Kafka metadata、token、路径或未知字段。
- Elasticsearch 停止期间日志查询返回精确公共错误 `503 logs_unavailable`，正文不包含 URL、index、PIT、DSL、响应 body 或底层错误。

### 1.3 索引、重放、故障和业务隔离

- 验证 template 只匹配 `gopulse-logs-v1-*`，mapping 为 `dynamic: strict`，并自动附加 `gopulse-logs-v1-read`；实际日志索引为 `gopulse-logs-v1-2026.09.04`，与 `gopulse-post-search-v1` 完全分离。
- 扫描 Elasticsearch 命中，确认日志 `_id` 均为 32 位 message ID，`_source` 只含 canonical 字段；直接写入未知字段被 strict mapping 以 `400` 拒绝。
- 永久无效 Kafka 日志未写入存储且合法后继继续；Elasticsearch 停止时已由 LogMonitor 接受的 `33333333333333333333333333333333` 在同一 Marshaller 进程内恢复写入；`abcdef0123456789abcdef0123456789` 原样重放两次后仍只有一份文档。
- Elasticsearch 日志故障窗口内，真实评论与通知事件 `a956d71e-335b-4cc3-b38e-1072e6e97d99` 仍完成业务事实和 RabbitMQ 消费；恢复后对应 Business Worker 日志可查询，证明可观测故障未改变非搜索业务控制流。
- Router token 被 LogMonitor 拒绝，LogMonitor ingest token 被 Router 拒绝；随机敏感哨兵未出现在四进程 stdout、Monitor/Router/Marshaller 日志、日志查询响应或 Elasticsearch 日志文档中。

### 1.4 Metrics 共存、必要业务回归与资源安全

- `scripts/verify-marshaller.sh` 通过真实 10-family/11-sample Metrics 矩阵、三类永久异常继续、双成员 rebalance、Kafka/VictoriaMetrics 故障恢复、正式 group offset 与 captured-real replay。捕获记录为 message ID `8765d6f3ee0cc1c063bf682b3f27599b`、partition `0`、offset `30`；永久异常使 committed offset 依次从 `33` 推进到 `36`。
- `scripts/verify-business.sh` 通过完整浏览器、通知、搜索、Outbox、Worker/Indexer、retry/dead、broker/Elasticsearch/reindex 和 Phase 4 四进程结构化日志矩阵；最终统计为 backend `279`、worker `27`、indexer `38`、reindex `8` 条结构化日志。
- 验收前无运行中的 Compose 项目、相关监听端口或 GoPulse 验收进程。每次真实脚本结束后再次检查，随机 `gopulse-logs-*`、`gopulse-marshaller-*`、`gopulse-acceptance-*` container、network、volume、进程和端口均无残留；预先存在的日常/历史 volume 与 network 未被删除。

## 2. 实际变更文件

- `scripts/verify-logs.sh`：补齐真实精确过滤、PIT 分页、非法查询、空结果、`503`、内部身份互斥、strict mapping、索引隔离、敏感哨兵、业务故障隔离和非敏感证据输出。
- `README.md`：记录 Phase 9 在 `1.6.3` 的阶段收口范围和已验证边界。
- `dev/imple/Phase-09/Phase-09-总实施方案.md`：同步三个批次的真实完成状态。
- `dev/imple/Phase-09/Phase-09-03-集成验收与阶段收口.md`：同步本批本地验收状态。
- `dev/logs/Phase-09/Phase-09-03-集成验收与阶段收口.md`：新增本实施记录。
- `VERSION`、`frontend/package.json`、`frontend/package-lock.json`：统一更新到 `1.6.3`。

未修改 Backend、Monitor、Router、Marshaller 产品代码、公共 API、Envelope、mapping、Topic/group 或投递语义。

## 3. 实际验证

### 3.1 Go 与 Frontend 固定门禁

以下命令均通过：

```bash
(cd backend && test -z "$(gofmt -l .)")
(cd backend && go test -count=1 ./...)
(cd backend && go vet ./...)
(cd backend && go test -race -count=1 ./...)
(cd monitor && test -z "$(gofmt -l .)")
(cd monitor && go test -count=1 ./...)
(cd monitor && go vet ./...)
(cd monitor && go test -race -count=1 ./...)
(cd router && test -z "$(gofmt -l .)")
(cd router && go test -count=1 ./...)
(cd router && go vet ./...)
(cd router && go test -race -count=1 ./...)
(cd marshaller && test -z "$(gofmt -l .)")
(cd marshaller && go test -count=1 ./...)
(cd marshaller && go vet ./...)
(cd marshaller && go test -race -count=1 ./...)
(cd frontend && npm test -- --run)
(cd frontend && npm run build)
```

Frontend 单元测试结果为 9 个文件、48 个测试通过；构建完成。Go 四模块 unit/vet/race 全部通过。

### 3.2 脚本、配置与 self-test

以下命令均通过：

```bash
python3 -m unittest discover -s scripts/ci -p 'test_*.py'
python3 scripts/ci/validate_versions.py
bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-business.sh \
  scripts/verify-exporter.sh scripts/verify-monitor.sh scripts/verify-router.sh \
  scripts/verify-marshaller.sh scripts/verify-logs.sh scripts/package-redis-exporter.sh
docker compose --env-file .env.example --file deploy/compose.yaml config --quiet
scripts/verify-logs.sh --self-test
scripts/verify-marshaller.sh --self-test
scripts/verify-router.sh --self-test
scripts/verify-monitor.sh --self-test
scripts/verify-exporter.sh --self-test
scripts/verify-business.sh --self-test
```

CI Python 测试结果为 25 个测试通过；`validate_versions.py`、`validate_branch.py --branch develop/1.6.3 --base-ref upstream/main` 与 `git diff --check` 均在最终 diff 上通过。

### 3.3 真实隔离验收

```bash
scripts/verify-logs.sh
scripts/verify-marshaller.sh
scripts/verify-business.sh
```

三项均在真实 Kafka、Elasticsearch、VictoriaMetrics、MySQL、Redis、RabbitMQ 和随机强归属资源上通过。`verify-logs.sh` 扩展前的原阶段矩阵先通过；扩展后的最终矩阵再次通过。`verify-marshaller.sh` 与 `verify-business.sh` 在相关产品代码和依赖未变化后未重复执行。

## 4. 与方案的差异和实施中问题

- 固定验收首次通过后发现原 `verify-logs.sh` 的证据输出与阶段查询矩阵不足以单独证明真实 PIT 分页、组合 filters、非法参数、严格 mapping、身份互斥、敏感哨兵和业务故障隔离，因此只扩展验收编排，没有扩大产品能力。
- 首次在仓库根运行 `npm version` 因根目录没有 `package.json` 返回 `ENOENT`；随后在 `frontend/` 正确更新 package metadata。扩展脚本后的首轮辅助 `py_compile /dev/stdin` 又因 `/dev/__pycache__` 权限返回错误；该命令不属于项目门禁。两项均未反映产品或正式脚本失败，最终固定门禁与完整验收通过。
- 未增加独立 Review、Frontend 页面、Events、告警、全文分析、ILM、spool、Topic 拆分或容量优化。

## 5. 已知限制与后续项

- stdout 先于远程 `202`；LogMonitor 返回 `202` 前仍是 best-effort 内存边界，进程崩溃、队列满、永久输入错误或 drain 超时可丢失远程副本，stdout 不受影响。
- Kafka 接受后保持 at-least-once；相同 message ID 通过 Elasticsearch `_id` 幂等收敛，但不宣称端到端 exactly-once。
- 单 Topic/单 partition 使 Elasticsearch 暂时故障按顺序阻塞后续 logs 与 metrics；独立 Topic/group、磁盘 spool 和生产 SLA 不在 Phase 9 范围。
- 日志索引仍未启用 ILM 或自动删除；Frontend 日志页、Events、聚合、全文检索和告警分别留给后续阶段。
- Phase 9 只完成 Milestone 3 的日志部分；完整可观测 MVP 仍需 Phase 10 Events 与 Phase 11 统一管理员前端通过验收。

## 6. 远程门禁与合入

- 实现与验收提交：`ad9a1acc5e2f87d890eb0e4ca541e56e0339df8c`（`test: close Phase 9 integration acceptance`），已推送到 `develop/1.6.3`。
- GitHub Actions `Auto PR and Merge` run `33899802306` 于 2026-09-04 17:17:46 UTC 启动、17:24:01 UTC 完成，结论为 `success`。
- Branch governance、Backend、Message Router、Monitor、Redis Exporter、Backend log pipeline、Marshaller、Frontend、Scripts and Compose、Integration 十个远程质量门禁全部通过；`Open PR and enable auto-merge` job 同样成功。
- 自动创建 Pull Request #85，并按开发分支规则使用 squash merge；PR 于 2026-09-04 17:23:56 UTC 合入，`main` 提交为 `cff5098d372198bccd8a78af6a77172e2c4bcfd0`（`test: close Phase 9 integration acceptance (#85)`）。远程 `develop/1.6.3` 已由自动流程删除。
- 合入后的根 `VERSION`、Frontend `package.json` 与 `package-lock.json` 均为 `1.6.3`。三份 Phase 9 实施记录与真实提交一致，Phase 9 完成；Milestone 3 仍等待 Phase 10 Events 与 Phase 11 统一管理员前端。
