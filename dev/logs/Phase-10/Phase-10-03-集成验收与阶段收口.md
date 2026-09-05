# Phase-10-03：集成验收与阶段收口实施记录

## 1. 实施信息

- 实施日期：2026-09-05。
- 目标版本：`1.7.3`。
- 开发分支：`develop/1.7.3`。
- 基线：`upstream/main` 提交 `3c6a194`，根与 Frontend 版本均为 `1.7.2`；该提交是 Phase-10-02 Pull Request #91 的主远程合入提交。
- 实施环境：WSL2 Linux filesystem `/home/ray/GoPulse-1.7.3`，使用独立 Git worktree；原 `/home/ray/GoPulse` 工作区中的用户未提交文件未被暂存、修改或清理。
- 状态：已完成。实施提交 `23399be` 通过 Pull Request #92 于 2026-09-05 合入主远程 `main`；远程 `Auto PR and Merge` run `33956183213` 成功，合入提交为 `d332630`，Phase 10 完成条件已满足。

## 2. 前置批次与远程基线核对

- Phase-10-01 Pull Request #89 已于 2026-09-05 07:28:17 UTC 合入，合入提交为 `50cb497`；其实施记录已包含远程门禁和合入证据。
- Phase-10-02 实现提交 `8f9623a` 已通过 Pull Request #91 于 2026-09-05 08:26:19 UTC 合入，合入提交为 `3c6a194`。
- Pull Request #91 的 `Auto PR and Merge` run `33954950505` 中 Backend、Monitor、Router、Marshaller、Frontend、Integration、Scripts and Compose、Branch governance 以及 Events/Logs 相关固定门禁全部成功。
- 从最新 `upstream/main` 创建 `develop/1.7.3`；开工时根 `VERSION` 与 `frontend/package.json` 均为 `1.7.2`，符合前置条件。

## 3. 实际完成内容

### 3.1 最终静态与组件门禁

- Backend、Monitor、Router、Marshaller 的全模块 gofmt、unit、vet 全部通过。
- Backend `eventquery`/HTTP、Monitor EventMonitor/Plugin Manager/Metrics collector、Marshaller Events/Elasticsearch/consumer 的直接受影响 package race 全部通过。
- 25 项脚本 CI unittest、版本一致性、Bash 语法、Compose 渲染和全部固定 self-test 通过。
- Phase-10-01/02 后应用代码、公共合同和依赖未在本批修改；本批未机械增加覆盖率测试或重复已由组件门禁覆盖的排列。

### 3.2 Phase 10 真实 Events 封闭矩阵

`scripts/verify-events.sh` 在随机项目 `gopulse-events-cee4b24013d7` 中最终通过，并输出：

```text
Failure, recovery, replay, offset, and mixed Events query closed end to end through index gopulse-events-v1-2026.09.05.
```

该真实矩阵覆盖并通过：

- 经 Backend 管理员插件 API 产生的 install/stop/start/update 生命周期事件；no-op、拒绝操作和 Monitor shutdown 不制造假生命周期事件。
- 真实终态 start failure、ownership 匹配的 unexpected exit、Router 故障导致的 collection failure/recovery，以及 Redis 停止/恢复导致的 target unavailable/recovered episode。
- 未登录 `401`、普通用户 `403` 与管理员 `source/plugin_id/limit=100` 受限查询。该批当时没有固定合法 cursor 翻页、API 空结果或 HTTP `503` 证据；这些缺口由 Phase-10-04 补齐。
- EventMonitor → Router → Kafka → 正式 Marshaller group → Elasticsearch → Backend 查询全链；同 ID 重放不增加文档，永久坏消息跳过后合法消息继续。
- Elasticsearch 停止期间正式 group offset 不推进，恢复并重新确认 template/mapping/alias 后推进。
- Metrics、Logs、Events 在同一 Topic/Marshaller 中并存并分别进入 VictoriaMetrics、Logs 索引和 Events 索引；Events、Logs 与帖子索引/alias 保持隔离。
- 随机 Compose project、端口、凭据、plugin root、临时目录和归属资源在退出后由脚本清理。

### 3.3 必要业务回归

`scripts/verify-business.sh` 在随机项目 `gopulse-acceptance-3c0aec214d38` 中最终通过：

- 首轮真实 Chromium 验收为 `2 passed / 2 skipped`，与脚本按矩阵选择的预期一致。
- targeted `search-rebuild` 与 `search-live` 各 `1 passed`。
- Phase 2 可靠性矩阵通过：consumer 停止/恢复、broker outage、Backend/Worker 重启、重复事件、临时错误、永久 poison 和 RabbitMQ container restart 均保持既有事实与幂等语义。
- Phase 4 进程日志合同通过：实际校验 Backend 284、Worker 27、Indexer 38、Reindex 8 条日志。
- 验收退出后只清理随机强归属资源，未发现关联 container/network/volume、进程或端口遗留。

### 3.4 日常生命周期与 verify 只读性

- 首次使用示例固定端口启动时，Docker 报告 `127.0.0.1:6379` 瞬时占用；当时未发现可归属监听者。立即执行 `scripts/down.sh`，只清理本次部分创建的 `gopulse` containers/network，并保留既有日常命名 volumes。
- 随后将工作树忽略的 `.env` 改为随机 loopback 端口，执行 `scripts/dev.sh → scripts/verify.sh → Ctrl+C → scripts/down.sh`。基础设施、Router、Marshaller、Monitor/Exporter、Backend、Worker、Indexer 与 Frontend 均按日常顺序启动，反向关闭成功。
- `scripts/verify.sh` 全部检查通过。前后快照证明 Git diff hash、插件文件 hash、用户角色、Events 文档数、index/alias/template 集合和 Elasticsearch open search contexts 未变化。
- 快照期间 Logs 文档数从 5 增至 6、Kafka offset 从 11 增至 15，这是运行中 Metrics scrape 与验证请求产生的正常 Logs/Metrics 流量；Events 文档数保持 1，验证脚本未执行插件安装/启停、未制造 Events，也未改变角色或打开遗留 PIT。
- `scripts/down.sh` 后随机端口、应用进程、`gopulse` containers/network 均不存在；既有日常命名 volumes 按脚本合同保留。`.run` 只保留仓库拥有的可重建二进制、package 与插件 registry，不含运行 PID 记录、日志或临时 token。

### 3.5 文档、状态与版本

- 根 `VERSION`、`frontend/package.json` 和 `frontend/package-lock.json` 同步为 `1.7.3`。
- README 当前版本更新为 `1.7.3`，不增加 Phase 11 Frontend 或其他未交付能力声明。
- Phase-10-02 拆分方案与实施记录补记 Pull Request #91、远程门禁和合入提交事实。
- Phase 10 总方案与 Phase-10-03 拆分方案更新为“本地收口完成、远程收口待完成”，未提前宣称 Phase 10 已满足远程完成条件。

## 4. 变更文件

- 版本：`VERSION`、`frontend/package.json`、`frontend/package-lock.json`。
- 项目说明：`README.md`。
- 状态与记录：
  - `dev/imple/Phase-10/Phase-10-总实施方案.md`；
  - `dev/imple/Phase-10/Phase-10-02-采集故障事件与可靠性闭环.md`；
  - `dev/imple/Phase-10/Phase-10-03-集成验收与阶段收口.md`；
  - `dev/logs/Phase-10/Phase-10-02-采集故障事件与可靠性闭环.md`；
  - `dev/logs/Phase-10/Phase-10-03-集成验收与阶段收口.md`。
- 固定矩阵未暴露产品阻断问题，因此未修改 Backend、Monitor、Router、Marshaller、Frontend 功能、Events 合同、脚本或 Compose 产品配置。

## 5. 实际验证与结果

### 5.1 Backend

```bash
(cd backend && test -z "$(gofmt -l .)")
(cd backend && go test -count=1 ./...)
(cd backend && go vet ./...)
(cd backend && go test -race -count=1 ./internal/eventquery ./internal/http/...)
```

结果：全部通过。

### 5.2 Monitor

```bash
(cd monitor && test -z "$(gofmt -l .)")
(cd monitor && go test -count=1 ./...)
(cd monitor && go vet ./...)
(cd monitor && go test -race -count=1 ./internal/events ./internal/plugin ./internal/metrics/collector)
```

结果：全部通过。

### 5.3 Router 与 Marshaller

```bash
(cd router && test -z "$(gofmt -l .)")
(cd router && go test -count=1 ./...)
(cd router && go vet ./...)
(cd marshaller && test -z "$(gofmt -l .)")
(cd marshaller && go test -count=1 ./...)
(cd marshaller && go vet ./...)
(cd marshaller && go test -race -count=1 ./internal/events ./internal/elasticsearch ./internal/consumer)
```

结果：全部通过。

### 5.4 仓库门禁与自检

```bash
python3 -m unittest discover -s scripts/ci -p 'test_*.py'
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/1.7.3 --base-ref upstream/main
bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-business.sh \
  scripts/verify-exporter.sh scripts/verify-monitor.sh scripts/verify-router.sh \
  scripts/verify-marshaller.sh scripts/verify-logs.sh scripts/verify-events.sh \
  scripts/package-redis-exporter.sh
docker compose --env-file .env.example --file deploy/compose.yaml config --quiet
scripts/verify-events.sh --self-test
scripts/verify-marshaller.sh --self-test
scripts/verify-router.sh --self-test
scripts/verify-monitor.sh --self-test
scripts/verify-logs.sh --self-test
scripts/verify-business.sh --self-test
git diff --check
```

结果：全部通过；Python CI 共 25 项，版本一致性、`develop/1.7.3` 分支治理和最终 diff 检查均通过。

### 5.5 真实集成与生命周期

```bash
scripts/verify-events.sh
scripts/verify-business.sh
scripts/dev.sh
scripts/verify.sh
scripts/down.sh
```

结果：除首次固定端口 `6379` 的瞬时环境冲突外，随机隔离重试和全部最终矩阵通过；失败运行未作为完成证据。该冲突未复现，也未要求产品代码或脚本修改。

## 6. 与计划的偏差

- 本批没有产品功能或验收脚本修改；当时执行了既有 `verify-events.sh`。后续实现 Review 识别出 PIT/cursor、API 空结果和 HTTP `503` 的固定证据缺口，已转由 Phase-10-04 整改。
- 日常生命周期首次固定端口启动遇到一次无法归属且未复现的 Docker bind 冲突。按计划保存诊断、清理本批部分资源并改用随机 loopback 端口重试；最终日常生命周期和只读验证通过。
- `verify.sh` 前后 offset 与 Logs 文档数会因系统正常采集和验证请求日志前进，不能以绝对不变作为只读判据；使用 Events 数量、Git/plugin hash、角色、索引合同和 open contexts 不变，并结合新增记录类型确认没有验证脚本写事件或改变业务配置。
- 未执行一般代码审查、依赖审计、覆盖率扩张、压力测试或 Phase 11 Frontend 工作。

## 7. 已知限制与后续事项

- EventMonitor 仍是有界内存 best-effort source queue；进程崩溃或容量耗尽时，尚未进入 Kafka 的远程 Events 副本可能丢失，这是既定产品边界。
- Pull Request #92、远程固定门禁和主远程 `main` 合入均已完成；后续工作应从 Phase 11 的独立开发批次开始，不继续使用 `develop/1.7.3`。
- 日常生命周期按合同保留 `gopulse_*` named volumes 和可重建 `.run` 工件；它们不属于随机验收资源，不应由验收清理误删。
- Phase 11 接收 Backend Events API、固定事件词汇、strict Events 索引/alias 与有界 best-effort/at-least-once/幂等语义；本批不实现管理员 Events Frontend。


## 8. 远程收口

- 本地实施提交：`23399be6efaa1f5026994aad6368571401bc9dcf`（`chore: close Phase 10 integration acceptance`）。
- 推送分支：`develop/1.7.3`；推送前版本、分支治理和工作树清洁检查通过。
- 远程门禁：`Auto PR and Merge` run `33956183213` 于 2026-09-05 完成并成功。
- Pull Request：#92，于 2026-09-05 08:53:26 UTC 由自动流程 squash merge。
- 主远程合入提交：`d33263023e085339aa878f751abf2572f0c76121`（`chore: close Phase 10 integration acceptance (#92)`）。
- `develop/1.7.3` 远程分支已按普通开发分支规则删除；本状态更新位于 `update` 规划分支，不改变产品版本 `1.7.3`。
