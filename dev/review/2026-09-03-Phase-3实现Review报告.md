# GoPulse Phase 3 实现 Review 报告

## 1. Review 信息

| 项目 | 内容 |
| --- | --- |
| Review 日期 | 2026-09-03 |
| 用户指定权威 Review 分支 | `develop/0.4.4` |
| Review 基线 | `f54f1a2175c1f508c3ecac775077387e5af29682`（与已 fetch 的 `origin/main` 一致，PR #50 合并提交） |
| Phase 3 变更范围 | `6c86d6418613d48ef50a24c3d04adc63182d6604..f54f1a2175c1f508c3ecac775077387e5af29682` |
| 当前完成版本 | 根 `VERSION` 与 Frontend npm 元数据均为 `0.4.3`；本次只新增 Review 文档，不修改版本 |
| 实施批次 | Phase-03-01 至 Phase-03-03 |
| 实际执行环境 | WSL2 Linux，Go 1.26.7，Node.js 24.20.0，npm 11.19.0，Docker 29.7.2 / Compose v5.5.0，Docker context `default` |
| Review 范围 | Phase 3 总方案与三份拆分方案、三份实施记录、Elasticsearch 索引/重建/Search API、`post.created` Outbox、RabbitMQ 搜索拓扑、Search Indexer、Frontend 搜索闭环、Bash 生命周期和阶段验收、版本/分支治理、Git 与远程门禁证据 |
| 变更规模 | 88 个文件，4124 行新增、708 行删除 |
| 结论 | **有条件通过（Conditional Pass）** |

本次 Review 使用用户指定的本地 `develop/0.4.4` 作为权威工作分支。该分支当前指向 PR #50 合入后的 `origin/main`，工作开始前没有未提交改动。重点判断：

1. MySQL 是否仍是帖子事实来源，Elasticsearch 是否仅保存可删除、可重建的搜索投影。
2. 历史重建、增量索引、Backend 搜索、MySQL hydration 与 Frontend 是否形成完整闭环。
3. RabbitMQ、Elasticsearch、Indexer、Backend 重启或暂停时，是否保持核心事实并最终收敛。
4. `search_after`、索引 generation、alias 切换和并发增量是否满足分页与恢复契约。
5. Phase 3 实施记录、远程门禁、版本与分支治理是否足以支撑阶段收口及后续 `1.0.0` 发布。

## 2. 总体结论

Phase 3 的核心架构方向正确，端到端业务能力已经落地：

- 帖子创建在同一 MySQL 事务中提交 `posts` 与最小 `post.created` Outbox 事件。
- Outbox Publisher 根据事件类型把搜索事件发布到独立搜索 exchange/queue，不与通知消费者串流。
- Search Indexer 只加载 MySQL、RabbitMQ 和 Elasticsearch，消费事件后回读 MySQL，并以稳定帖子 ID 幂等写入搜索 alias。
- 搜索 API 仅从 Elasticsearch 获取有序帖子 ID，再从 MySQL 装配作者、评论数、点赞数和 `liked_by_me` 等当前事实。
- 重建命令采用新物理索引、受控 mapping、H1 扫描、计数门槛、原子 alias 切换、H2 尾部补偿和精确旧索引删除。
- Frontend 只访问 Backend 相对 API，具备 URL 查询恢复、空结果、错误、加载更多和帖子跳转。
- 独立执行的完整 `scripts/verify-business.sh` 通过了真实 MySQL、Redis、RabbitMQ、Elasticsearch、Backend、Business Worker、Search Indexer、Frontend 和 Chromium 组成的 Phase 0～3 隔离矩阵，退出后未遗留带验收标签的容器、网络或命名卷。

PR #50 已于 **2026-09-02 16:59:12 UTC** 合入 `main`，合并提交为 `f54f1a2`。其 head `e59b7d4` 上的 Backend、Frontend、Branch governance、Scripts and Compose、Integration 以及自动 PR/合并任务均为 `success`。

本次没有发现 P0 或 P1 问题，记录 3 项 P2 和 1 项 P3：

1. 搜索游标使用 `_score + search_after`，但物理索引仍被增量写入且没有 Point in Time；跨页期间相关性分数可能变化，无法保证不跳项或不重复。
2. PR #50 已合入且远程门禁成功，但 Phase 3 总方案和 README 仍保留“待合入/阶段未完成”的状态，阶段完成与 `1.0.0` 触发条件没有完成事后收口。
3. 用户指定的 `develop/0.4.4` 没有出现在 Phase 3 总实施方案的权威分配中，当前分支治理命令明确失败。
4. Frontend 的分页失败“重试”始终重新请求第一页，而不是重试失败的加载更多请求，会替换已经展示的累计结果。

因此，Phase 3 的单页搜索、静态数据分页、重建、增量收敛、故障恢复与跨阶段业务矩阵可以接受，但在发布 `1.0.0` 前建议先在正式分配的 Review 整改批次中关闭 P2-01～P2-03，并顺带修复 P3-01。若暂不修复 P2-01，至少必须把并发增量期间分页不具备快照一致性的限制写入公开契约，而不能继续将 generation cursor 描述为完整的稳定分页保证。

## 3. 风险分级

| 等级 | 定义 |
| --- | --- |
| P0 | 已造成数据丢失、严重安全事件或核心业务完全不可用，必须立即停止发布 |
| P1 | 阻断阶段验收、受支持平台或关键事实一致性边界，应在进入发布动作前修复 |
| P2 | 核心闭环可运行，但一致性、故障恢复、阶段治理或维护风险明显，应在近邻整改批次关闭 |
| P3 | 低风险交互、元数据或可维护性问题，可与相邻整改一起处理 |

本次共记录：

- P0：0 项
- P1：0 项
- P2：3 项
- P3：1 项
- 已知且被方案接受的限制：4 项

## 4. Phase 3 完成定义核对

| 完成定义 | 结果 | Review 证据 |
| --- | --- | --- |
| 历史帖子可从 MySQL 重建到新的搜索物理索引 | 通过 | `search.Reindexer` 使用 advisory lock、H1/H2、Bulk、refresh/count、alias 切换；完整隔离验收真实删除并恢复活动索引 |
| 标题和正文关键词可通过认证 Backend API 搜索 | 通过 | `multi_match best_fields` 查询 `title^2` 与 `content`；真实 API 和 Chromium 验收通过 |
| Elasticsearch 只返回命中 ID，最终 DTO 由 MySQL 装配 | 通过 | `Search` 不返回 `_source`；`post.MySQLRepository.FindMany` 有界集合查询并恢复命中顺序 |
| 新帖子提交后无需人工重建即可最终可搜索 | 通过 | 帖子与 Outbox 同事务；独立 Search Indexer 回读 MySQL 并向 alias 幂等 PUT；暂停/恢复场景通过 |
| 搜索与通知拓扑隔离 | 通过 | `BusinessTopology` 仅绑定评论/点赞，`SearchTopology` 仅绑定 `post.created`；隔离验收通过 |
| 删除索引、Broker/Indexer/Elasticsearch 故障后事实不丢且可恢复 | 通过 | MySQL 核心写入保持成功；Outbox、retry/dead、重建和恢复矩阵通过 |
| alias 重建与增量事件共同运行 | 通过 | H2 补偿与 alias 写入协作场景通过；并发发帖最终可搜索 |
| Search API cursor 跨重建失效 | 通过 | cursor 绑定 query digest 和物理 generation；重建后返回 `validation_failed` |
| Search API 分页在增量写入期间保持稳定 | **部分通过** | 静态数据分页通过；缺少 PIT，`_score` 在同一 generation 内仍可能因增量写入变化，见 P2-01 |
| Frontend 具备加载、空结果、错误、分页、重试和详情跳转 | **部分通过** | 主流程与浏览器验收通过；分页错误后的重试语义不正确，见 P3-01 |
| Phase 0～2 必要业务、通知和降级能力无回归 | 通过 | Backend 默认测试/vet/race、Frontend 门禁、完整 Phase 2 十项可靠性矩阵均通过 |
| 验收资源与日常资源隔离并安全清理 | 通过 | `--self-test` 通过；完整验收后未发现验收标签容器、网络或 volume |
| Phase 3 文档、Git、远程状态与版本一致 | **未完全通过** | 产品版本一致为 `0.4.3`，PR #50 门禁成功；权威状态和 Review 分支分配仍矛盾，见 P2-02、P2-03 |
| 满足后续 `1.0.0` release-only 动作的治理前提 | **待整改** | `develop/1.0.0` 有 release-only 分配，但应先关闭本报告 P2 项并完成 Phase 3 事后状态收口 |

## 5. 架构与实现 Review

### 5.1 事实边界与数据一致性

通过项：

- `backend/internal/post/repository.go:94-121` 在启用 Outbox 的生产构造中，以同一事务写帖子和 `post.created`。
- 事件只包含 event/actor/post 等最小定位字段；Indexer 在处理时重新读取 `posts`，避免把标题、正文或动态展示字段复制进消息。
- `backend/internal/search/contract.go:16-39` 的 strict mapping 只包含 `post_id`、`title`、`content`、`created_at`、`updated_at`。
- `backend/internal/search/service.go:129-158` 先取得 Elasticsearch 命中，再由 MySQL hydration 生成对外帖子列表。
- 评论数、点赞数和当前用户点赞状态没有进入索引，符合“MySQL 最终事实、Elasticsearch 可重建投影”的阶段目标。

### 5.2 重建与 alias 安全边界

通过项：

- advisory lock 防止两个应用重建命令同时运行。
- 新物理索引名只使用固定前缀、UTC 时间戳和随机后缀。
- 切换前验证 MySQL H1 范围与索引 count 一致；切换通过单次 aliases API 完成。
- 切换后捕获 H2 并补写 H1～H2 范围；后续新增帖子由写向新 alias 的增量事件覆盖。
- 删除只接受 `validPhysicalIndex` 校验通过的精确索引名，不使用 wildcard。
- 切换前失败会清理本次未发布索引且不改变旧 alias；切换后失败返回非零并保留当前状态，符合方案明确禁止不安全自动回滚的约束。

保留风险：

- 切换后尾部补偿失败可能暂时让新 alias 指向不完整索引；这是总方案已经接受的“非零退出、保留诊断、人工恢复”边界。当前恢复手段是再次完整重建，而不是自动回滚。
- 旧索引删除失败后，后续重建只枚举当前 alias 目标，不会自动发现已经脱离 alias 的历史孤儿索引；仍需操作人员按精确名称清理。

### 5.3 增量消息与 Worker 复用

通过项：

- Publisher 根据事件类型选择业务或搜索 exchange，并在建立 channel 时声明两套 durable topology。
- Search Worker profile 只允许 `post.created.v1`，通知 profile 只允许评论与点赞 routing key。
- retry/dead 二次发布使用 persistent、mandatory 和 publisher confirms；成功后才 ack 原消息。
- `PUT /{alias}/_doc/{post_id}?require_alias=true` 使用帖子 ID 幂等覆盖，并阻止 alias 缺失时意外创建同名索引。
- MySQL 暂时错误和 Elasticsearch 404/429/5xx 进入有限重试；缺失事实或确定性 mapping 4xx 进入 dead queue。
- Runtime 在 shutdown 或连接中断时取消并等待当前 handler，保持 Phase 2 的受控退出契约。

### 5.4 Search API 与公开错误边界

通过项：

- 只接受唯一的 `q`、`limit`、`cursor` 参数并拒绝未知参数。
- 查询词按 trim 后 Unicode code point 限制为 1～200；limit 默认 20、最大 50。
- cursor 使用 canonical Base64URL，绑定版本、query SHA-256、物理 generation 与 sort tuple。
- Elasticsearch 错误统一映射到脱敏 `503 search_unavailable`；对外不返回集群 URL、物理索引名或 DSL。
- Backend 直接查询物理 generation，避免 alias 在一次请求内部切换到另一个索引。

主要风险见 P2-01：物理 generation 在两次重建之间并非不可变快照，Search Indexer 会持续写入；仅绑定 generation 不能稳定 `_score`。

### 5.5 Frontend 搜索闭环

通过项：

- `/search` 是认证路由，导航入口明确。
- 搜索词保存在 URL query，前进/后退能够恢复搜索意图。
- API 返回的帖子结构经过字段级验证，浏览器不读取 Elasticsearch 配置或直连 9200。
- 页面覆盖初始提示、加载、空结果、不可用、参数/cursor 错误、加载更多与详情跳转。
- 请求序列号防止路由快速变化时旧响应覆盖新查询结果。

交互缺陷见 P3-01：所有错误的“重试”都调用 `load(true)`，分页错误没有保留原 cursor 请求语义。

### 5.6 生命周期、验收与资源安全

通过项：

- Compose 中 Elasticsearch 使用固定镜像、单节点、回环端口映射、健康检查和命名卷。
- `scripts/dev.sh` 在迁移后先执行 `search-reindex --if-missing`，再启动 Backend、Business Worker、Search Indexer 和 Frontend。
- Search Indexer 有独立 PID/process record；`down.sh` 与异常 cleanup 通过身份信息约束停止目标。
- 隔离验收使用随机 project、database、端口、路径和 volume，并通过安全 token、label、PID/路径校验限制清理范围。
- 本次完整验收结束后，Docker 中没有残留带 `com.gopulse.acceptance=true` 标签的容器、网络或 volume。

## 6. 详细问题

### P2-01：`_score + search_after` 未使用 PIT，增量写入期间分页边界会漂移

**位置**

- `backend/internal/search/elasticsearch.go:79-103`
- `backend/internal/search/service.go:118-156`
- `backend/internal/search/processor.go:90-113`

**问题**

Search API 以 `_score DESC`、`created_at DESC`、`post_id DESC` 排序，并把最后一个命中的 score/time/id 作为下一页 `search_after`。cursor 绑定物理 index generation，但没有打开 Elasticsearch Point in Time（PIT）或其他查询快照。

同一 generation 并不是不可变索引：Search Indexer 会持续通过 alias 把新帖子写入当前物理索引。新文档 refresh 后，BM25 的词频/逆文档频率统计可能改变，使第一页已经看到的旧文档在第二页请求时得到不同 `_score`。此时旧 cursor 中保存的 score 已不再是同一排序快照的边界，`search_after` 可能跳过尚未展示的命中，或重新返回已展示命中。

现有测试和完整验收只证明了静态数据集上的分页无重复，以及重建导致 generation 变化时 cursor 会失效；它们没有证明同一 generation 内发生增量 refresh 时的跨页一致性。

**影响**

- 活跃发帖期间，用户点击“加载更多”可能看到缺项或重复项。
- generation cursor 给出了“跨重建失效”保证，但容易被误解为“同一 generation 内稳定”；实际并不成立。
- 该问题不影响单页查询、MySQL 最终事实或索引可重建性，因此定为 P2，而不是 P1。

**建议**

1. 首选在第一页创建 PIT，后续请求使用同一 PIT + `search_after`；cursor 绑定 query digest、PIT 标识、sort tuple 和必要的过期信息。
2. 明确 PIT `keep_alive`、过期后的公共错误语义和重新搜索路径；不要把 PIT 标识写入日志或错误响应。
3. 如果不采用 PIT，则必须改为不会因索引统计变化而漂移的确定性排序，并明确接受相关性排序能力下降；仅增加 tie-breaker 不能解决 `_score` 本身变化。
4. 增加一个真实 Elasticsearch 场景：第一页后写入并 refresh 代表性文档，再请求下一页，证明同一搜索会话不重复、不跳项；另覆盖 PIT 过期后的安全重启语义。

**完成条件**

- 增量写入与 refresh 发生在两页请求之间时，搜索会话仍基于同一查询快照分页。
- cursor 不接受跨 query、跨 PIT/快照或篡改后的 sort tuple。
- 真实 Elasticsearch 验证通过，且重建后的旧搜索会话仍按定义失效或安全结束。

---

### P2-02：PR #50 已完成，但 Phase 3 权威状态和发布触发信息仍停留在合入前

**位置**

- `dev/imple/Phase-03/Phase-03-总实施方案.md:53-61`
- `README.md:383`
- `dev/logs/Phase-03/Phase-03-03-集成验收与里程碑收口.md:99-103`

**问题**

远程事实是：

- PR #50 已于 2026-09-02 16:59:12 UTC 合入。
- PR head 为 `e59b7d4d65ed431d6b44ecea121be68f5ba14f70`。
- 合并提交为 `f54f1a2175c1f508c3ecac775077387e5af29682`。
- Backend、Frontend、Branch governance、Scripts and Compose、Integration 及自动 PR/合并检查均成功。

但 Phase 3 总实施方案仍把 Phase-03-03 标为“本地完成，待 PR 与远程门禁”，README 仍写“Phase 3 is not marked complete until this branch is merged”，实施记录尾部也只有执行当时“尚未 push”的限制，没有追加事后状态。

实施记录保留执行当时的真实事实本身没有错误，但权威总方案和当前 README 没有追加合入后的最终状态，导致仓库同时表达“Phase 3 已关闭搜索里程碑”和“Phase 3 尚未完成”。`develop/1.0.0` 的 release-only 触发条件因此也缺少明确、可审计的当前结论。

**影响**

- 后续人员无法仅根据权威文档判断 Phase 3 是否完成、Review 整改属于哪个批次、何时允许创建 `develop/1.0.0`。
- 自动版本管理要求 Phase 3 完成并通过里程碑验收后发布 `1.0.0`；当前文档状态会造成提前发布或长期不发布两种相反风险。
- Phase 4 的基线和交接条件不清晰。

**建议**

1. 在 `update` 上更新 Phase 3 总实施方案的当前状态，记录 PR #50、head、merge commit、合入时间和实际远程检查结果。
2. 更新 README 的阶段状态，区分“Phase-03-03 已合入并通过门禁”“Phase 3 Review 整改尚未关闭”“`1.0.0` release-only 尚未执行”。
3. 对 Phase-03-03 实施记录只追加“后续状态”小节，不覆盖其执行当时尚未 push 的历史事实。
4. P2 整改完成后，再按总方案从已验证 `main` 创建 `develop/1.0.0`；不要在 Review 文档提交中提前修改 `VERSION`。

**完成条件**

- 总方案、README、Review 报告、Git/PR 事实对 Phase 3 当前状态表述一致。
- 仓库能明确回答 Phase 3 是否完成、Review 整改是否完成、`1.0.0` 是否已发布。
- `develop/1.0.0` 只从包含全部必要整改和成功门禁的主远程 `main` 创建。

---

### P2-03：`develop/0.4.4` 未被 Phase 3 总实施方案分配，分支治理无法通过

**位置**

- `dev/imple/Phase-03/Phase-03-总实施方案.md:53-77`
- `scripts/ci/validate_branch.py:97-119`
- 根 `VERSION`

**问题**

用户指定 `develop/0.4.4` 为本次权威 Review 分支，但 Phase 3 总实施方案只分配：

- `develop/0.4.1` → Phase-03-01
- `develop/0.4.2` → Phase-03-02
- `develop/0.4.3` → Phase-03-03
- `develop/1.0.0` → Milestone-01-Release

实际执行：

```text
python3 scripts/ci/validate_branch.py --branch develop/0.4.4 --base-ref origin/main
```

结果：

```text
ERROR: develop/0.4.4 must map to exactly one authoritative allocation; found 0
```

根 `VERSION` 当前为 `0.4.3`。这对“只新增 Review 文档、不改变产品版本”的当前任务是正确的，但说明 `develop/0.4.4` 还不能作为一个已经完成的开发批次通过质量门禁。

**影响**

- 如果直接推送当前 Review 分支，Branch governance 会失败，自动 PR/合并流程不能按既有规则完成。
- 若后续直接在该分支实现整改并把版本改为 `0.4.4`，仍会因为没有权威 batch allocation 而失败。
- 用户口头指定的“权威分支”与仓库规则中“总实施方案是唯一权威分配”的定义冲突。

**建议**

1. 在开始产品整改前，先在 `update` 上为 Phase 3 增加正式 Review 整改批次，例如 `Phase-03-04` → `0.4.4` → `develop/0.4.4`，并定义与本报告问题相称的验收标准。
2. 当前 Review 文档提交保持 `VERSION=0.4.3`；只有实际整改批次完成时才把根与 Frontend 版本一起更新为 `0.4.4`。
3. 因该分支尚未推送，不需要重写远程历史；完成权威分配后再按治理命令验证并推送。
4. 若团队决定 Review 只有文档、不建立整改批次，则 Review 文档应走允许规划/文档工作的 `update`，而不是把未分配的 `develop/0.4.4` 推向远程。当前用户已明确指定该分支，因此建议采用第 1 项。

**完成条件**

- `develop/0.4.4` 在且仅在一份 Phase 总实施方案中有唯一批次、版本和分支分配。
- 整改完成后的根 `VERSION` 与 Frontend npm 元数据均为 `0.4.4`。
- `validate_branch.py --branch develop/0.4.4 ...` 通过，且远程 Branch governance 成功。

---

### P3-01：分页失败后的“重试”重新加载第一页并替换累计结果

**位置**

- `frontend/src/views/SearchView.vue:32-51`
- `frontend/src/views/SearchView.vue:98-101`
- `frontend/src/views/SearchView.test.ts:34-82`

**问题**

`load(reset)` 使用 `reset=false` 时会携带 `nextCursor` 加载下一页。但页面上的错误重试按钮固定调用：

```vue
@click="load(true)"
```

因此，当“加载更多”因为临时 503、网络错误或无效响应失败后：

1. 已加载帖子仍显示，`nextCursor` 仍保留。
2. 用户点击“重试”。
3. 前端不携带 cursor，而是重新请求第一页。
4. 成功响应通过 `posts.value = page.data` 替换此前累计结果。

对于重建造成的 `validation_failed`，重新从第一页开始是合理的；但对于临时网络或服务不可用，按钮没有重试原失败请求。现有组件测试只覆盖第一页不可用和正常加载更多，没有覆盖加载更多失败后的按钮行为。

**影响**

- 用户可能在没有明确提示的情况下回到第一页，并丢失已加载的后续页面视图。
- “分页失败重试”与总方案定义的交互语义不一致。
- 不影响数据事实或 Backend 正确性，因此定为 P3。

**建议**

1. 保存最近一次失败请求是首次加载还是加载更多，以及当时使用的 cursor。
2. 临时网络、`search_unavailable` 或无效响应重试原请求；只有 cursor/generation 的 `validation_failed` 才明确清空并从第一页重新搜索。
3. 增加一个组件测试：第一页成功、加载更多失败、点击重试后仍携带原 cursor，并把成功结果追加而不是替换。

**完成条件**

- 分页临时失败后的重试继续使用失败时的 cursor，并保留已展示结果。
- cursor 已失效时，页面明确告知结果已更新，并执行一次受控的第一页重启。

## 7. 验证记录

### 7.1 本次实际通过的命令

```text
(cd backend && go test ./...)
(cd backend && go vet ./...)
(cd backend && go test -race ./...)
(cd frontend && npm ci)
(cd frontend && npm test -- --run)
(cd frontend && npm run typecheck)
(cd frontend && npm run build)
python3 -m unittest discover -s scripts/ci -p 'test_*.py'
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/0.4.3 --base-ref origin/main
bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-business.sh
docker compose --env-file .env.example --file deploy/compose.yaml config --quiet
scripts/verify-business.sh --self-test
scripts/verify-business.sh
git diff --check
```

结果摘要：

- Backend 默认测试通过。
- `go vet ./...` 通过。
- Backend race 测试通过。
- Frontend 9 个测试文件、44 项测试通过。
- Frontend typecheck 与 production build 通过。
- Python 治理测试 21 项通过。
- 根与 Frontend 版本一致为 `0.4.3`。
- `develop/0.4.3` 的既有 Phase-03-03 分配校验通过。
- Bash 语法、Compose 配置和验收安全自测通过。
- 完整 Phase 0～3 隔离业务验收通过，包含真实 Chromium、搜索重建、增量索引、故障恢复和 Phase 2 十项可靠性矩阵。
- 完整验收退出后未发现验收标签资源残留。

### 7.2 本次实际失败或未作为独立命令重复的项目

1. 第一次执行 Frontend 测试时，本地尚未安装依赖，失败为 `vitest: not found`；执行 `npm ci` 后，测试、typecheck 和 build 均通过。`node_modules` 为忽略内容，没有项目文件变更。
2. `python3 scripts/ci/validate_branch.py --branch develop/0.4.4 --base-ref origin/main` 失败，原因是权威方案中没有 `develop/0.4.4` 分配，详见 P2-03。
3. 本次没有再次独立运行 `(cd backend && go test -count=1 -tags=integration ./...)`。原因是 PR #50 的远程 Integration 门禁已成功，本次又独立执行并通过了覆盖真实基础设施和浏览器的完整 `scripts/verify-business.sh`；为遵守“不重复已成功验证”的范围控制，没有再建立第二套临时基础设施重复该门禁。实施记录中的 tagged integration 结果只作为历史证据引用，不冒充本次执行结果。

### 7.3 远程证据

通过 GitHub API 实际确认：

```text
PR 50: merged=true
merged_at: 2026-09-02T16:59:12Z
head: e59b7d4d65ed431d6b44ecea121be68f5ba14f70
merge commit: f54f1a2175c1f508c3ecac775077387e5af29682
Backend: success
Frontend: success
Branch governance: success
Scripts and Compose: success
Integration: success
Open PR and enable auto-merge: success
```

## 8. 已知且被方案接受的限制

以下内容不作为本次缺陷：

1. 当前产品只有帖子创建，没有帖子编辑或删除，因此增量索引只处理 `post.created`。
2. Search dead queue 没有自动 replay；恢复 alias 后需要显式重建或人工重放。
3. Elasticsearch 是本地单节点开发基础设施，不提供生产 HA、跨节点容灾或安全认证方案。
4. PowerShell 保持 `0.2.1` 历史能力基线；Phase 3 以后只维护 WSL2/Bash 生命周期和验收入口。

后续增强但不应扩大本次整改范围的项目：

- 中文/多语言 analyzer 质量调优、同义词、高亮、过滤、聚合和搜索建议。
- 自动 orphan index 清理与 dead queue 运维控制面。
- 搜索压力测试、容量模型和多节点 Elasticsearch 部署。
- 帖子编辑/删除事件及其索引更新语义。

## 9. 建议整改顺序

1. **先处理 P2-03**：在 `update` 上建立 `Phase-03-04 / 0.4.4 / develop/0.4.4` 权威分配，消除当前分支治理阻断。
2. **处理 P2-01**：确定 PIT 分页契约并实现最小真实 Elasticsearch 验证。
3. **处理 P2-02**：在整改事实明确后同步总方案、README 和 Phase-03-03 的后续状态，形成唯一阶段结论。
4. **顺带处理 P3-01**：修复 Frontend 分页重试并添加一个直接组件测试。
5. 在最终 diff 上运行整改批次固定门禁；只扩展到受 PIT、Frontend retry 和治理状态直接影响的回归。
6. `develop/0.4.4` 合入且远程门禁成功后，再从最新主远程 `main` 创建并执行独立 `develop/1.0.0` release-only 动作。

## 10. 最终结论

**Phase 3 实现 Review 结论：有条件通过。**

核心搜索闭环和真实故障矩阵已经通过，没有发现数据丢失、权限越界、索引替代 MySQL 事实源或阶段核心能力不可用的问题。当前最重要的实现风险是 `_score` 分页缺少 PIT，最明确的治理问题是 PR #50 合入后的阶段状态没有收口，以及用户指定的 `develop/0.4.4` 尚未取得总实施方案的权威分配。

关闭 3 项 P2、完成必要门禁并合入后，可将 Phase 3 Review 整改标记完成；随后按既定 release-only 流程发布 `1.0.0`。P3-01 建议同批关闭，但若因明确范围理由延期，应记录为发布后第一项搜索交互修复，而不是继续扩展本次 Review。

## 11. Phase-03-04 整改执行状态

本报告保留 Review 当时的证据、问题描述与“有条件通过”结论，不覆盖历史判断。后续由权威批次 `Phase-03-04 / 0.4.4 / develop/0.4.4` 执行整改：

- **P2-01 已在本地关闭**：Search API 第一页针对当前物理 generation 打开两分钟 Elasticsearch PIT，后续页使用同一快照、最新 PIT ID 与包含 `_shard_doc` 的完整 `search_after` tuple；游标使用服务端密钥派生 HMAC，绑定 query digest、generation、PIT、过期时间和排序边界。PIT 过期、generation 变化或游标篡改返回脱敏 `validation_failed`。
- **P2-02 已在仓库状态中关闭**：Phase 3 总方案和 README 已记录 PR #50 的合入提交与成功门禁；Phase-03-03 实施记录只追加后续远程状态，不改写执行时历史；`develop/1.0.0` 的前置条件调整为 Phase-03-04 合入并通过远程门禁。
- **P2-03 已在本地关闭**：Phase 3 总方案新增唯一 `Phase-03-04 / 0.4.4 / develop/0.4.4` 分配，并新增对应拆分方案和实施记录；根与 Frontend 版本同步为 `0.4.4`。
- **P3-01 已在本地关闭**：Frontend 保存失败请求的 reset/cursor 语义；临时加载更多失败重试原 cursor 并保留累计结果，失效快照游标明确清空旧结果并从第一页受控重启。

最终关闭条件仍以本批次固定门禁、远程 Branch governance/Backend/Frontend/Scripts and Compose/Integration 以及 PR 合入结果为准；未实际取得的远程结果不得由本附录预先写成成功。
