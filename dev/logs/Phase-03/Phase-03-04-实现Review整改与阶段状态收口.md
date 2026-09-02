# Phase-03-04：实现 Review 整改与阶段状态收口开发记录

## 1. 完成内容

- 在 Phase 3 总实施方案中增加唯一 `Phase-03-04 / 0.4.4 / develop/0.4.4` 分配，并把 `develop/1.0.0` 的前置条件调整为本批次合入且远程门禁成功。
- Search API 改为在第一页针对当前物理 generation 打开 Elasticsearch PIT，后续页通过 PIT 与完整 `_score / created_at / post_id / _shard_doc` 排序 tuple 执行 `search_after`。
- PIT `keep_alive` 固定为两分钟；搜索响应携带的最新 PIT ID 被放入下一页 opaque cursor，没有下一页或请求失败时尽力关闭 PIT。
- 搜索 cursor 升级为版本 2，绑定 query digest、generation、PIT、过期时间和完整排序边界，并使用从 `AUTH_JWT_SECRET` 派生的独立 HMAC-SHA256 key 校验完整性。
- generation 变化、客户端过期、签名无效和 Elasticsearch 中 PIT 上下文缺失均返回脱敏 `validation_failed`；普通依赖失败仍返回 `search_unavailable`。
- Frontend 保存最近一次失败请求的 reset/cursor。临时加载更多失败重试原 cursor 并保留累计结果；分页 cursor 失效时清空旧结果、显示结果已更新提示并从第一页受控重启。
- README、Phase-03-03 后续状态和 Review 报告附录已同步 PR #50 事实、整改批次状态与 release-only 前置条件。
- 根 `VERSION`、`frontend/package.json` 和 `frontend/package-lock.json` 已更新为 `0.4.4`。

## 2. 变更文件

- Backend：`backend/internal/search/elasticsearch.go`、`backend/internal/search/service.go`、`backend/internal/search/service_test.go`、`backend/internal/search/pit_integration_test.go`、`backend/cmd/server/main.go`
- Frontend：`frontend/src/views/SearchView.vue`、`frontend/src/views/SearchView.test.ts`
- 版本：`VERSION`、`frontend/package.json`、`frontend/package-lock.json`
- 文档：`README.md`、Phase 3 总方案与 Phase-03-04 拆分方案、Phase-03-03 后续状态、本记录、实现 Review 报告附录

## 3. 实际验证

通过：

```text
(cd backend && go test ./...)
(cd backend && go vet ./...)
(cd backend && go test -race ./...)
(cd frontend && npm test -- --run)
(cd frontend && npm run typecheck)
(cd frontend && npm run build)
python3 -m unittest discover -s scripts/ci -p 'test_*.py'
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/0.4.4 --base-ref origin/main
bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-business.sh
docker compose --env-file .env.example --file deploy/compose.yaml config --quiet
scripts/verify-business.sh --self-test
```

结果摘要：

- Backend 默认测试、vet 和 race 测试全部通过。
- Frontend 9 个测试文件、46 项测试通过；typecheck 与 production build 通过。
- Python 治理测试 21 项通过；根与 Frontend 版本一致为 `0.4.4`；`develop/0.4.4` 唯一权威分配校验通过。
- Bash 语法、Compose 配置和验收安全自测通过。
- 使用独立 Compose project 和 Elasticsearch 9.5.2 运行 `go test -count=1 -tags=integration ./internal/search` 通过。场景在第一页后向实时物理索引写入并 refresh 20 个高相关文档，后续 PIT 页面仍与初始快照 ID 顺序完全一致；显式关闭 PIT 后再次搜索返回安全的 PIT-missing sentinel。临时容器、网络和命名卷已清理。
- `git diff --check` 通过。

## 4. 失败、调整与偏差

- 第一次真实 Elasticsearch 测试尝试用 `keep_alive=1ms` 加固定 250ms 等待来触发自然过期，但 Elasticsearch 在该时点仍接受了 PIT，请求成功，因此该检查失败。此行为不适合作为确定性门禁。
- 随后把真实基础设施场景改为显式关闭 PIT 后验证相同的缺失搜索上下文错误映射；客户端绝对过期时间与安全重启语义由 Backend 单元测试确定性覆盖。调整后定向 integration 通过。
- 未重复完整 `scripts/verify-business.sh`。Phase-03-03 与 Review 已通过该封闭矩阵；本批次没有修改生命周期、Outbox、RabbitMQ 拓扑、重建流程或浏览器认证主闭环，按方案只运行 PIT、Frontend retry、版本和治理直接影响的门禁。

## 5. 已知限制与后续

- PIT `keep_alive` 固定为两分钟，不是无限滚动会话；用户超过该窗口继续分页会得到 `validation_failed` 并从第一页重新搜索。
- cursor 对客户端保持 opaque，但其中承载的 PIT ID 不是加密数据；Backend 不记录该 ID，也不在错误消息中输出。
- Phase 3 Review 只有在 `develop/0.4.4` 推送后远程 Branch governance、Backend、Frontend、Scripts and Compose、Integration 和自动 PR/合并均成功时才最终关闭。随后才可从最新 `origin/main` 创建独立 `develop/1.0.0`。


## 6. 后续远程状态与 PR 编排修复

- `develop/0.4.4` 已推送；Auto PR and Merge push run `33665066803` 的 Branch governance、Backend、Frontend、Scripts and Compose、Integration 和 Open PR and enable auto-merge 全部成功。
- 自动创建的 PR #51 已于 2026-09-02 18:10:52 UTC 合入 `main`，合并提交为 `3d07f1b831499f4e5bd449b53e6cc0561dad51c8`。普通 development 分支随后按规则从远程删除。
- PR 触发的 CI run `33665595978` 在合入后一秒以 failure 结束且没有创建任何 job。结合 PR 已先完成合入并删除 head 分支的时间顺序，该失败属于自动合并早于 pull-request CI 初始化的编排竞态，而不是 Backend、Frontend、治理、Compose 或 Integration job 失败。
- 后续在长期 `update` 分支修复 `.github/workflows/auto-pr-merge.yml`：创建 PR 后先定位相同 head SHA 的 `ci.yml` / `pull_request` run，并用 `gh run watch --exit-status` 等待成功；只有随后才允许启用 auto-merge。若 PR CI 失败，PR 和 head 分支将保留用于整改，不再先删除分支制造无 job 的失败 run。该仓库规则修复不修改 `VERSION`。

- `update` 首次推送编排修复后，push run `33666308567` 的五类质量门禁全部成功，但 Create or find pull request 步骤因 PR body 的单引号进入 Bash 单引号字面量而报 `unexpected EOF while looking for matching quote`。已移除该不安全标点并新增后续提交重跑；这次失败未创建 PR，也未改变 `main`。
