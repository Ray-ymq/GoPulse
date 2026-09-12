# Phase-15-07 Review 整改开发记录

## 基线与结果

- 按用户要求使用权威分支 `develop/1.12.7`，开工执行 `git fetch origin`，延续基于 `5cf850b1aa82e2dc4a298c60e5614db53cdc9f49` 的 Review 分支，不重建或改号。
- 完成总方案登记的第七批，根 `VERSION` 从 `1.12.6` 更新至 `1.12.7`；双前端 package/lock 与 `.env.example` 的产品/镜像版本同步。
- 关闭报告 P2-01、P2-02、P2-03、P3-01。未跟踪文件 `~` 未读取、修改或暂存。

## 实际改动与文件

| 文件 | 已完成工作 |
| --- | --- |
| `backend/internal/alert/handler.go` | 列表 source 允许空值及 metrics/logs/events；保留未知来源拒绝和既有 cursor 签名合同 |
| `backend/internal/alert/list_sources_test.go` | 独立 MySQL 的真实 Handler/Repository 测试；三源各两条记录，rules/current/history 分页来源不串、无重复、无第三页；拒绝未知 source 和篡改 cursor |
| `backend/internal/adminoverview/sources.go`、`plugins_test.go` | observed=failed 优先于采样与新鲜度判定，保留生命周期与 up 证据，输出 degraded/process_failed；running/up=1 仍 healthy |
| `admin-frontend/src/services/management.ts`、`management.test.ts` | 精确 schema 推导类型并在运行时验证 catalog/rule/incident/user/role-change/audit；校验日期、ID、数值、枚举、字段集合及嵌套 selector/evaluation/审计详情白名单 |
| `admin-frontend/src/views/AlertsView.vue`、`UsersView.vue`、`AuditView.vue` | 所有读取和返回 DTO 的变更请求改用 validated helpers；DELETE 仍使用无响应体合同 |
| `admin-frontend/src/services/overview.ts` | 接受新增固定安全原因 process_failed，避免把正常失败状态隔离成 invalid_response |
| `VERSION`、`.env.example`、两前端的 `package.json`/`package-lock.json` | 同步 1.12.7 元数据 |
| Phase 15 总方案、本批拆分方案、本记录、第六批日志、原 Review 报告 | 登记第七批验收合同、追加主线事实及整改回执；不改写第六批历史验收结果 |

健康优先级：Monitor 不可达/未安装仍按原合同 unknown；Monitor 已确认 failed 强制 degraded/process_failed，不被新鲜 up=1 或旧成功时间戳覆盖；其余生命周期沿用现有采样判定。本批不扩展所有生命周期组合的含义。

## 实际验证

### 固定门禁

| 命令 | 结果 |
| --- | --- |
| `cd backend && go test ./internal/alert/... ./internal/adminoverview ./internal/user` | 通过；允许缓存。无 DSN 时两个受控 MySQL 测试跳过，其中本批列表测试另行真实执行如下；既有 `TestOwnedMySQLStateAndLease` 未重跑 |
| `cd admin-frontend && npm test` | 最终 8 files / 35 tests 全部通过；新增 11 个测试直接覆盖 DTO 合同与真实页面隔离 |
| `cd admin-frontend && npm run build` | typecheck 与 Vite release build 通过（55 modules） |
| `cd frontend && npm run build` | typecheck 与 Vite release build 通过（77 modules） |
| `git diff --check`、`git diff --cached --check` | 提交前通过；暂存范围仅本批文件 |

### 独立真实 MySQL 回归

执行以下命令，只新建自己的 MySQL，不连接或停止已有项目容器：

```bash
docker run -d --name gopulse-review-151207-mysql \
  -e MYSQL_ROOT_PASSWORD=review-local-only \
  -e MYSQL_DATABASE=gopulse_review_151207 \
  -p 127.0.0.1::3306 mysql:8.4.0
for i in $(seq 1 40); do
  docker exec gopulse-review-151207-mysql mysqladmin ping \
    -uroot -preview-local-only --silent >/dev/null 2>&1 && break
  sleep 1
done
cat backend/migrations/*.up.sql | docker exec -i gopulse-review-151207-mysql \
  mysql -uroot -preview-local-only gopulse_review_151207
port=$(docker port gopulse-review-151207-mysql 3306/tcp | cut -d: -f2)
(cd backend && REVIEW_ALERT_DSN="root:review-local-only@tcp(127.0.0.1:$port)/gopulse_review_151207?parseTime=true&loc=UTC" \
  go test ./internal/alert -run TestReviewMySQLSourceLists -v)
docker rm -fv gopulse-review-151207-mysql
```

最终 `TestReviewMySQLSourceLists` 与 rules/current/history 三个子测试全部通过，真实执行 9 组来源/列表的双页查询。容器与其匿名卷已删除。上述密码仅用于已销毁的隔离测试实例，不是部署凭据。

### 定向开发验证与实际失败修正

- `cd backend && go test ./internal/adminoverview`：failed/running 场景通过，使用真实 exporterplugin HTTP client 与新鲜 up=1 fixture。
- 首次 MySQL 测试失败为 fixture 未建立 bootstrap singleton/超级管理员，真实 Mutate 授权拒绝。补全持久引导身份后原命令通过；没有修改生产授权逻辑。
- 首次管理页测试使用了不存在的“禁用”按钮文案，改为实际“停用”，最终全套通过。
- 首次管理端 build 发现严格类型推导后 `includes(string)` 与 duration 字面量联合不兼容；改为等值 `some`，最终全套测试与 build 通过。
- 为核实 schema 与 Backend 完整目录一致，临时 `TestExportReviewCatalog` 经真实 `/alerts/catalog` 写出 `/tmp/gopulse-review-catalog.json`，执行 `go test ./internal/alert -run TestExportReviewCatalog` 通过；临时 `catalog-review-temp.test.ts` 调用 `isCatalog` 验证实际 JSON，`npm test -- src/services/catalog-review-temp.test.ts` 为 1/1 通过。两份临时测试均已删除，不作为永久项目文件；永久测试保留代表性合同 fixture。
- Go 改动执行 gofmt；未读取第三方依赖源码。修正后只重跑受影响检查，没有因记录整理重复成功门禁。

## 偏差、限制与停止条件

- 没有功能范围偏差；按报告建议使用直接相关回归，而非重跑完整 Compose、浏览器 E2E、三源故障注入、Docker 镜像构建、全仓测试、race 或跨平台矩阵。
- 本次 MySQL 检查针对列表来源与签名分页，不冒充三源评估状态机、旧卷升级或整个产品端到端验收；前六批日志仍为这些能力的历史证据。
- `scripts/ci/verify_phase15_closure.py` 的第六批验收 fixture 版本保持原历史合同，未把它当作本批产品版本来源。
- 必需门禁均已通过，无阻断失败；完成日志与版本后提交并推送，不增加无关审计、测试或重构。
