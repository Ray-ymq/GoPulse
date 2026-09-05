# Phase-11-03：集成验收与 Milestone 3 收口开发记录

## 1. 执行信息

- 执行日期：2026-09-05
- 开发分支：`develop/1.8.3`
- 开工基线：`upstream/main` / `b6697c6`
- 目标版本：`1.8.3`
- 当前结论：本地实现和固定验收已通过；Pull Request、权威远程固定门禁与主远程合入尚未发生，因此本记录暂不宣称 Phase 11 或 Milestone 3 最终完成。

## 2. 实际完成工作

### 2.1 双使用态、登录回跳与运行中降权

- 普通用户浏览器逐一直达总览、Metrics、Logs、Events、Exporter 路由，均在管理组件请求前进入无权限页；Metrics/Logs/Events 与 Exporter list/get/install/start/stop/update 代表 API 均返回 `403 permission_denied`。
- 受保护 URL 未登录访问现在把原始站内目标写入登录页 `redirect` query；管理员登录后返回原管理页，普通用户登录后仍由实时角色检查送入 `/forbidden`。
- 新增第二个隔离管理员账号，仅用隔离 MySQL 中的受控 SQL 将其降为普通用户；下一次 Exporter API `403` 触发管理页卸载与无权限导航，同时同一 Cookie 的社交会话继续有效。
- Exporter 上传文件在成功、失败和离页路径均清除引用；失败更新继续保留最后一个已验证 DTO，不把失败响应改写为当前事实。

### 2.2 Metrics、Logs、Events 与 Exporter 浏览器主闭环

- 隔离生命周期可选择以空 plugin root 启动 Monitor；Chromium 从“未安装”状态上传合法 `1.8.2` 包完成 install，随后执行 stop/start、升级到 `1.8.3`，并验证低版本更新失败不会覆盖现有状态。
- Metrics 页面先证明空结果，再等待真实 Redis → Exporter → Monitor → Router → Kafka → Marshaller → VictoriaMetrics 样本，查询无标签 `gopulse_redis_up` 和带 `mode` 标签的 `gopulse_redis_cpu_seconds_total`；`1h` 请求只含固定 `metric`/`range`，页面显示服务器固定 `60s` step。
- 验收从 Backend 响应头取得真实生成的 32 位 request ID，通过 Logs 页面精确筛选；生成超过一页的真实 HTTP 日志，并验证 50 条首页、加载更多、新筛选重置和损坏 cursor 的安全恢复。
- 通过真实 Backend Exporter stop/start 操作产生超过一页的生命周期 Events，验证分页、`exporter_plugin_updated` 精确筛选、cursor 恢复和 DTO metadata 白名单。
- 定向畸形成功响应只用于 trust-boundary 异常验证：Frontend runtime validator 将未知字段转为通用安全错误，HTML/script 哨兵未渲染、未执行；真实健康主闭环未使用 mock/fake 数据替代。

### 2.3 局部故障、业务回归与体验

- 在验证 Compose project label/container ID 和 Monitor PID/start ticks/executable/cwd/command marker 后，依次停止恢复 VictoriaMetrics、暂停恢复 Monitor、停止恢复 Elasticsearch。
- VictoriaMetrics 与 Monitor 故障不改变 Backend `/health` 或 `/ready`；对应 Metrics/Exporter 区域局部失败，Logs、Events 和其他总览区域保留成功结果并可独立恢复。
- Elasticsearch 故障使 Logs/Events 局部不可用，并按既有合同令 Backend `/ready` 返回 `503`/`elasticsearch=down`；`/health`、帖子发布、评论和点赞仍成功。恢复原 Elasticsearch 后 readiness 与两个区域均恢复。
- 五个管理页面在 375px 窄屏无不可用横向溢出；关键表单保持 label，管理区域暴露 busy/status 语义，键盘 Tab 可进入交互控件，状态文字不只依赖颜色。
- 浏览器网络只访问随机 Frontend origin 下的同源 Backend；生产 bundle 扫描未发现 VM/ES/Monitor 地址、内部 alias/Topic、临时 token 或服务器绝对路径。

### 2.4 生命周期、治理与远程门禁

- `dev.sh` 新增严格受限的 `GOPULSE_MONITOR_PLUGIN_BOOTSTRAP=skip`，只允许隔离空 plugin root 用于浏览器 install 验收；默认 `ensure` 行为不变。
- `verify.sh`、`down.sh` 支持与 `dev.sh` 相同的受限随机 project、`/tmp` env/run override，并在任何 Docker/进程操作前拒绝不安全 project/path；`verify.sh` 同时使用配置的随机 Frontend 端口。
- 完整主入口执行 `dev.sh → verify.sh → down.sh`，随后删除自有随机 containers/network/volumes/temp root，并确认没有自有进程残留；预存的日常/历史资源未被修改或删除。
- 可复用质量门禁新增 `Observability browser acceptance` job，在远程安装 Chromium 并运行同一隔离主入口；实际远程结果待本批 push 后记录。
- 根 `VERSION`、Frontend package metadata 同步为 `1.8.3`。

## 3. 主要变更文件

- Frontend 产品修复：`frontend/src/components/AuthForm.vue`、`frontend/src/router/index.ts`、`frontend/src/router/index.test.ts`、`frontend/src/views/ObservabilityExportersView.vue`。
- 浏览器与生命周期验收：`frontend/e2e/observability.spec.ts`、`scripts/verify-observability-ui.sh`、`scripts/dev.sh`、`scripts/verify.sh`、`scripts/down.sh`。
- 远程门禁：`.github/workflows/quality-gates.yml`。
- 文档、计划与版本：`README.md`、`backend/README.md`、Phase 11 总方案与本批方案、`VERSION`、`frontend/package.json`、`frontend/package-lock.json`。

## 4. 实际验证

### 4.1 已完成的主闭环

- `scripts/verify-observability-ui.sh`：最终实现上成功；7 个真实 Chromium tests 在约 1.3 分钟内全部通过，随后 production bundle 扫描、隔离 `verify.sh`、`down.sh` 和强归属资源清理全部通过。
- 主闭环覆盖普通用户五条管理路由零数据请求和全部代表 API `403`、未登录回跳、浏览器 install/stop/start/update/失败保留、固定 Metrics、Logs/Events 筛选与分页、VM/Monitor/ES 局部故障恢复、ES 故障窗口社交写入、窄屏/键盘/畸形 DTO、运行中降权和会话保留。

### 4.2 最终固定门禁

以下命令在 2026-09-05 的最终 diff 上实际执行并成功：

- `(cd backend && test -z "$(gofmt -l .)")`
- `(cd backend && go test -count=1 ./...)`
- `(cd backend && go vet ./...)`
- `(cd backend && go test -race -count=1 ./internal/metricquery ./internal/exporterplugin ./internal/http/...)`
- `(cd frontend && npm test -- --run)`：11 个 test files、56 个 tests 通过。
- `(cd frontend && npm run build)`：`vue-tsc --noEmit` 与 Vite production build 通过。
- `python3 -m unittest discover -s scripts/ci -p 'test_*.py'`：25 个 tests 通过。
- `python3 scripts/ci/validate_versions.py`
- `python3 scripts/ci/validate_branch.py --branch develop/1.8.3 --base-ref upstream/main`
- `bash -n`（实施方案第 9 节列出的全部 Bash 脚本）
- LF checkout 检查（受维护的 Bash/Go 文件均为 working-tree LF）
- `docker compose --env-file .env.example --file deploy/compose.yaml config --quiet`
- Compose 固定 loopback、Kafka、VictoriaMetrics Basic Auth、dedup 与 volume 渲染断言
- `scripts/verify-observability-ui.sh --self-test`
- `scripts/verify-observability-ui.sh`：7 个真实 Chromium tests、bundle 扫描、隔离 `verify.sh`、`down.sh` 与资源清理通过。
- `scripts/verify-events.sh --self-test`
- `scripts/verify-logs.sh --self-test`
- `scripts/verify-marshaller.sh --self-test`
- `scripts/verify-monitor.sh --self-test`
- `scripts/verify-business.sh --self-test`
- `.github/workflows/quality-gates.yml` YAML 解析
- `git diff --check`

Backend 日志保存于 `/tmp/gopulse-phase-11-03-final-backend.log`，Frontend 日志保存于 `/tmp/gopulse-phase-11-03-final-frontend.log`，静态/治理日志保存于 `/tmp/gopulse-phase-11-03-final-static.log`，最终浏览器主闭环日志保存于 `/tmp/gopulse-phase-11-03-observability-ui-expanded-5.log`。这些是本机临时证据，不纳入提交。

远程门禁、PR 和 merge 证据仅在真实观察后补写。

## 5. 计划偏差与处理

- 基线 `dev.sh` 总会预安装当前插件，无法证明浏览器 install。按方案“真实阻断只做最小修复”增加默认关闭的隔离 bootstrap skip，并要求空 plugin root，否则立即拒绝。
- `verify.sh` 和 `down.sh` 原先硬编码日常 `.env`、`.run`、`gopulse` 与 Frontend 5173，无法执行隔离 `dev → verify → down`。本批统一为受限 override，并复制 `dev.sh` 的 project/path 安全边界。
- 初次 request-ID 验收尝试传入 `X-Request-ID`，实际 Backend 合同会忽略客户端值并生成新 ID；最终从真实响应头读取生成值后进行精确日志筛选，没有修改产品 request-ID 合同。
- Metrics 数据为异步链路，安装/更新 DTO 的 `last_success_at` 不等于 VictoriaMetrics 已完成写入；最终浏览器用有界刷新等待无标签和带标签 family，不把两者误作强一致事务。
- Events 分页需要超过 50 条真实数据；使用已授权浏览器会话调用现有 stop/start 产品 API 生成生命周期事件，没有直接写 Elasticsearch 或修改 alias/PIT。
- MySQL CLI 会输出“命令行密码可能不安全”的标准警告；凭据仅存在随机 `/tmp` 环境和本地验收进程，未进入 Frontend、日志记录或提交内容。

## 6. 已知限制与后续项

- 页面仍不自动轮询，不提供任意 MetricsQL/PromQL/Elasticsearch DSL、全文检索、告警、复杂图表、跨数据关联或多 Exporter。
- Phase 8 单节点 VictoriaMetrics 继续复用 Marshaller Basic Auth 身份；这是已记录的本地 MVP 最小权限边界。
- Elasticsearch 仍是帖子 search/readiness 与 Logs/Events 的共享既有依赖；本批证明非搜索社交 API 不新增依赖，但未改变该架构。
- 浏览器主入口会执行真实 Docker、Go build、npm build 和 Chromium；远程 job 的首次实际耗时与 runner 资源结果需在 push 后据实补写。
