# GoPulse Phase 1 实现 Review 报告

## 1. Review 信息

| 项目 | 内容 |
| --- | --- |
| Review 日期 | 2026-09-01 |
| Review 基线 | `592897b1080eb78483a2eeb49141671f16cfc8fe`（`origin/main`） |
| Phase 1 变更范围 | `94606d69f357ae12e8e77546d2889f79bc83c8ee..592897b1080eb78483a2eeb49141671f16cfc8fe` |
| Review 分支 | `update`，已快进同步到本次 Review 基线 |
| 当前完成版本 | `0.2.6` |
| 实施批次 | Phase-01-01 至 Phase-01-06 |
| 实际执行环境 | WSL2 Linux，Go 1.26.7，Node.js 24.20.0，npm 11.19.0，Docker context `default` |
| Review 范围 | Phase 1 总方案与六份拆分方案、六份实施记录、Backend、Frontend、迁移、Redis 缓存、Bash 生命周期与完整业务验收、GitHub Actions 配置、版本与分支治理 |
| 结论 | **有条件通过（Conditional Pass）** |

本次 Review 以 `origin/main` 的已合并实现为准，覆盖约 108 个文件、9998 行新增和 296 行删除。重点判断：

1. Phase 1 定义的注册、登录、发帖、查询、评论、点赞闭环是否真实可用。
2. MySQL 事实源、Redis 降级与最终一致边界是否符合总实施方案。
3. Phase 0 移交的 readiness、HTTP Server 和配置一致性问题是否已关闭。
4. Backend、Frontend、真实基础设施和浏览器验收是否具备可重复证据。
5. 当前仓库能否作为 Phase 2 异步化工作的稳定输入基线。

## 2. 总体结论

Phase 1 的核心实现质量较好，业务闭环、事实模型、缓存边界、安全基线和自动化验收均形成了完整链路。本次独立执行 Backend 单元测试、竞态测试、真实 MySQL/Redis integration 测试、Frontend 测试/类型检查/构建、Bash 安全自测以及隔离的 Playwright 完整业务验收，结果全部通过；验收过程中创建的容器、网络和数据卷也已清理。

本次未发现 P0 或 P1 级问题。主要优点如下：

- MySQL 始终是用户、帖子、评论和点赞的唯一事实来源，Redis 只保存可重建的帖子详情公共投影。
- `liked_by_me` 不进入公共缓存，而是按当前用户从 MySQL 单独读取，避免跨用户状态污染。
- 密码使用 bcrypt；JWT 只接受 HS256，并验证签名、`sub`、`iat`、`exp` 与有效期；认证通过 `HttpOnly`、`SameSite=Lax` Cookie 传输。
- 非本地环境强制 Secure Cookie；HTTP JSON 输入采用严格字段解析、单值限制和 64 KiB 请求体边界。
- readiness 对忽略 context 和 panic 的 checker 建立了有界隔离；HTTP Server 配置了 ReadHeader、Read、Write、Idle 超时和请求头大小限制。
- 帖子与评论使用严格解码的 keyset cursor；点赞幂等最终由数据库联合主键保证。
- integration 模式在缺失开关、非白名单数据库或 Redis DB 时会失败，而不是静默跳过。
- `verify-business.sh` 使用随机隔离资源、回环端口、归属校验和无条件清理，能够真实验证浏览器操作、Backend 重启、Redis 清空、故障与恢复。
- Phase-01-02 之后没有继续修改冻结在 `0.2.1` 能力基线的 PowerShell 脚本，符合 WSL2/Bash-only 平台规则。

有条件通过的原因不是核心业务不可用，而是仍有两项 P2 问题影响正式阶段收口或用户故障体验，另有一项 P3 元数据问题：

1. Phase 1 总实施方案的权威状态表仍把 Phase-01-02 至 Phase-01-06 标记为“待实施”，与合并历史、实施记录和 `VERSION=0.2.6` 冲突。
2. Frontend 首次认证恢复时把非 401 的临时网络或服务端错误也固化为匿名状态，导致有效 Cookie 用户被错误导航到登录页且本次 SPA 会话不再自动重试。
3. Frontend npm 包元数据仍为 `0.1.3`，与产品完成版本 `0.2.6` 不一致。

建议在 Phase 2 开始前关闭 P2-01；P2-02 可在 Phase 2 首个直接涉及 Frontend 的批次或独立近邻批次修复。P3-01 可与版本自动化或 Frontend 工程维护一并处理。

## 3. 风险分级

| 等级 | 定义 |
| --- | --- |
| P0 | 已造成数据丢失、严重安全事件或核心业务完全不可用，必须立即停止发布 |
| P1 | 阻断阶段验收、受支持平台或关键安全/事实一致性边界，应在进入下一阶段前修复 |
| P2 | 核心基线可运行，但阶段治理、故障体验、稳定性或维护风险明显，应安排近邻修复 |
| P3 | 低风险元数据、工程卫生或文档一致性问题，可随相邻批次处理 |

本次共记录：

- P0：0 项
- P1：0 项
- P2：2 项
- P3：1 项
- 已知且被方案接受的限制：1 项

## 4. Phase 1 完成定义核对

| 完成定义 | 结果 | Review 证据 |
| --- | --- | --- |
| Frontend 可完成注册、登录、发帖、查询、评论、点赞/取消点赞 | 通过 | `scripts/verify-business.sh` 从空隔离环境运行真实 Chromium，Playwright `business.spec.ts` 1/1 通过 |
| 核心业务事实保存在 MySQL，Backend 重启后仍存在 | 通过 | 完整业务验收在创建业务事实后重启 Backend，并重新验证用户、帖子、评论和点赞 |
| Redis 清空、故障和恢复不造成事实丢失 | 通过 | 完整业务验收覆盖验收 Redis DB 清空、Redis 停止、MySQL 降级、Redis 恢复和缓存重建 |
| 认证 Cookie、密码、密钥与底层错误满足 Phase 1 安全边界 | 通过 | 代码检查、认证单元/HTTP/integration 测试通过；Cookie 使用 HttpOnly/Lax，生产环境 Secure 强制开启 |
| 空库迁移、索引、约束和幂等语义经过验证 | 通过 | 隔离 MySQL 执行 `go run ./cmd/migrate up` 和 `go test -count=1 -tags=integration ./...` 通过；六份实施记录包含迁移/约束验收事实 |
| Phase 0 readiness、HTTP Server、配置一致性移交项关闭 | 通过 | 对应实现与自动测试存在；普通测试、race、非默认端口完整验收均通过 |
| Backend 单元测试、vet、race 通过 | 通过 | `go test ./...`、`go vet ./...`、`go test -count=1 -race ./...` 全部通过 |
| 隔离 MySQL/Redis integration 测试强制执行且不静默跳过 | 本地通过，远程待补证 | 本次使用独立 Compose project 执行带 `INTEGRATION_TESTS=1` 的 integration suite，全部通过并清理；当前 GitHub CLI 上下文未能读取 PR #27 的远程 checks |
| Frontend 测试、类型检查和生产构建通过 | 通过 | Vitest 7 个文件、33 个测试通过；`npm run typecheck` 与 `npm run build` 通过 |
| WSL2/Bash 生命周期和业务验收入口通过 | 通过 | Bash 语法检查、安全自测和完整业务验收通过；本次环境确认为 WSL2 |
| 完整验收只操作隔离且归属校验的资源 | 通过 | 随机项目 `gopulse-acceptance-7694b76f0087` 验收通过；完成后容器、网络和卷查询均为空 |
| `/health`、`/ready` 保持原契约且未引入后续阶段能力 | 通过 | Router 回归测试和完整验收通过；RabbitMQ 仍只参与 readiness，不作为 Phase 1 业务事实源 |
| 六份实施记录存在且与实际工作一致 | 基本通过 | `dev/logs/Phase-01/` 下六份镜像记录均存在；抽查的功能、文件、命令和限制与实现一致 |
| 根版本为 `0.2.6`，六批均按分配合并 | 通过 | `VERSION` 为 `0.2.6`；Phase-01-02 至 Phase-01-06 对应 PR #23 至 #27 已进入 `origin/main` |
| Phase 总方案反映阶段真实完成状态 | **未通过** | 权威状态表仍把 Phase-01-02 至 Phase-01-06 标记为“待实施”，见 P2-01 |

综合判断：产品实现和本地验收已满足 Phase 1 核心完成定义；正式阶段治理收口仍需更新权威状态表，并建议补存远程质量门禁结果。

## 5. Review Findings

### P2-01：Phase 1 权威批次状态表未随实现完成而收口

**位置**

- `dev/imple/Phase-01/Phase-01-总实施方案.md:35-44`
- `VERSION`
- `dev/logs/Phase-01/`

**证据**

总实施方案明确声明该表是 Phase 1 批次、版本和分支的“唯一权威分配”，但当前内容仍为：

```text
Phase-01-01  已完成
Phase-01-02  待实施
Phase-01-03  待实施
Phase-01-04  待实施
Phase-01-05  待实施
Phase-01-06  待实施
```

仓库实际状态则是：

- Phase-01-02 至 Phase-01-06 的实施记录全部存在。
- 五个对应功能提交/PR 已进入 `origin/main`：
  - `91d8d09 feat(auth): implement user authentication (#23)`
  - `cbdff55 feat(post): implement publishing and queries (#24)`
  - `d55b0fb feat(interactions): implement comments and likes (#25)`
  - `8cc84e1 feat(cache): add Redis post detail caching (#26)`
  - `592897b feat(frontend): complete Phase 1 business flow (#27)`
- 根 `VERSION` 已为 `0.2.6`。
- 本次独立完整验收通过。

**影响**

- 权威规划文档与代码、版本和实施记录互相矛盾，后续人员无法仅依赖总方案判断阶段是否已经完成。
- Phase 2 的前置条件、自动化计划解析或人工里程碑审计可能误判 Phase 1 尚未实施。
- 当前缺少一个明确的 Phase 级最终验收结论，六个批次虽然分别有记录，但阶段收口状态没有被权威文档确认。

**建议**

1. 在 `update` 上把 Phase-01-02 至 Phase-01-06 的状态更新为“已完成”。
2. 在总方案中补充 Phase 1 最终验收摘要，记录基线提交、完成版本、通过的验收入口和已接受限制。
3. 如远程 CI 结果可获取，补充 PR #27 或阶段最终提交的质量门禁链接/结论；不得把未查到的远程结果写成已验证。
4. 后续每批合并时同步更新总方案状态，避免只更新实施记录和 `VERSION`。

**完成条件**

- 总实施方案、六份实施记录、Git 合并历史和 `VERSION=0.2.6` 对 Phase 1 完成状态给出一致结论。
- Phase 2 可以从权威计划中明确读取“Phase 1 已完成并通过阶段验收”。

---

### P2-02：认证恢复把临时网络/服务端错误固化为匿名状态

**位置**

- `frontend/src/composables/useAuth.ts:19-34`
- `frontend/src/router/index.ts:25-36`
- `frontend/src/composables/useAuth.test.ts`
- `frontend/src/router/index.test.ts`

**证据**

`useAuth.initialize()` 在当前用户请求失败时会先执行 `clear()`，把状态设置为 `anonymous`；只有错误不是 401 时才继续向上抛出：

```ts
} catch (error) {
  clear()
  if (!(error instanceof ApiError && error.status === 401)) throw error
}
```

路由守卫随后捕获所有恢复错误，再次执行 `auth.clear()`，并把受保护页面导航到 `/login`：

```ts
try {
  await auth.initialize()
} catch {
  auth.clear()
}
if (to.meta.requiresAuth && auth.status.value !== 'authenticated') return '/login'
```

因此，页面刷新时只要 `/users/me` 遇到临时网络失败、502/503 或非法响应，即使浏览器仍持有有效 HttpOnly Cookie，也会发生：

1. 内存认证状态被设置为 `anonymous`。
2. 用户被导航到登录页，表现为“被退出登录”。
3. 因 `initialize()` 对任何非 `uninitialized` 状态直接返回，本次 SPA 会话不会在服务恢复后自动重试；通常需要整页刷新或重新登录。

当前测试只覆盖 401 → anonymous、成功恢复和并发请求合并，没有覆盖网络错误或 5xx 恢复失败。

**影响**

- 短暂 Backend 重启、反向代理错误或网络抖动会被错误呈现为认证失效。
- 用户无法区分“Cookie 无效”和“服务暂时不可用”，也看不到明确的重试状态。
- 该行为与 Frontend 对网络错误提供明确状态、避免刷新时误判认证的设计目标存在偏差。

**建议**

1. 仅在明确收到 401 `authentication_required` 时切换到 `anonymous`。
2. 对网络错误、5xx 和非法响应保留可重试状态，例如增加 `error` 状态，或恢复为 `uninitialized` 并显示“认证状态恢复失败，请重试”。
3. 路由守卫不要把所有异常等同于匿名；可导航到专门错误状态、保留目标路由并提供重试，或允许由顶层错误边界处理。
4. 增加以下测试：
   - `/users/me` 网络错误不会变成 anonymous。
   - 500/非法响应不会触发假退出。
   - 服务恢复后可在不重新登录的情况下重试成功。
   - 真实 401 仍会清除状态并进入登录页。

**完成条件**

- 只有明确认证失败才进入 anonymous。
- 临时依赖故障不会让有效会话表现为永久退出，并有自动或用户触发的恢复路径。

---

### P3-01：Frontend npm 包版本仍停留在 `0.1.3`

**位置**

- `frontend/package.json:4`
- `frontend/package-lock.json:3`
- `frontend/package-lock.json:9`
- `VERSION`

**证据**

- 根 `VERSION` 为 `0.2.6`，根据仓库规则它是当前完成产品版本的唯一来源。
- `frontend/package.json` 仍声明 `"version": "0.1.3"`。
- `frontend/package-lock.json` 的根包版本也仍为 `0.1.3`。
- 测试和构建输出因此显示 `gopulse-frontend@0.1.3`。

**影响**

- 不影响当前私有 Frontend 构建，也不改变根 `VERSION` 的权威性。
- 但 npm 输出、产物元数据、依赖扫描报告和未来发布自动化会显示一个已经过期的版本，增加排障和审计歧义。

**建议**

二选一并形成明确规则：

1. 把 Frontend package 版本视为产品版本，并由脚本在版本批次完成时从根 `VERSION` 同步；或
2. 明确说明该 private package 使用独立、非产品版本，不应被用于发布/运行时识别。

如果没有独立版本需求，优先选择从根 `VERSION` 自动同步，避免多个人工版本源。

**完成条件**

- npm 元数据与根版本一致，或仓库文档明确声明并验证两者的独立语义。

## 6. 已知且被方案接受的风险

### Cache-aside 并发旧回填可能在 TTL 内暴露陈旧公共计数

**相关位置**

- `backend/internal/post/service.go:72-94`
- `backend/internal/comment/service.go:45-59`
- `backend/internal/like/service.go:33-63`
- `backend/internal/platform/redis/post_detail.go:53-95`
- `README.md:214-218`
- `dev/imple/Phase-01/Phase-01-总实施方案.md:329-352`

帖子详情 miss 时会先从 MySQL 读取公共投影，再回填 Redis；评论、点赞和取消点赞在 MySQL 事实成功后执行 best-effort 缓存删除。以下交错仍可能发生：

```text
旧请求读取 MySQL 旧计数
→ 新评论/点赞事实提交
→ 删除缓存
→ 旧请求把旧计数重新写回缓存
```

另外，删除操作超时或失败也会让旧投影保留到 TTL 到期。结果是公共 `comment_count`/`like_count` 可能短期陈旧，但：

- MySQL 事实不会被缓存覆盖或回滚。
- 评论/点赞成功语义不依赖 Redis。
- `liked_by_me` 每次从 MySQL 单独查询，不受公共缓存陈旧影响。
- 清除缓存或 TTL 到期后会重新收敛。

这不是本次新发现的阶段阻断缺陷。Phase 1 总方案、README、实施记录和确定性测试均明确接受该边界，并明确不引入分布式锁、singleflight 或延迟双删。

**后续建议**

- Phase 2 及以后增加缓存命中、miss、解码失败、回填失败、失效失败和读取延迟指标。
- 当产品对计数实时性提出明确 SLA 时，再评估延迟双删、版本化投影、事件驱动失效或其他一致性方案。
- RabbitMQ 不得成为 Phase 1 查询事实源，也不得改变 MySQL 已提交写入的成功语义。

## 7. 实际验证命令与结果

### 7.1 Backend

```bash
cd backend && go test ./...
```

结果：通过。

```bash
cd backend && go vet ./...
```

结果：通过。

```bash
cd backend && go test -count=1 -race ./...
```

结果：通过，所有含测试的 Backend package 均返回 `ok`。

### 7.2 隔离 MySQL/Redis Integration

本次创建独立 Compose project `gopulse-review-integration-ffabb662b1ef`，使用随机回环端口、白名单数据库 `gopulse_integration`、用户 `gopulse_integration` 和 Redis DB 15，执行：

```bash
cd backend
go run ./cmd/migrate up
go test -count=1 -tags=integration ./...
```

结果：通过。认证、帖子、评论、点赞、HTTP、MySQL、Redis 缓存和迁移 integration package 全部返回 `ok`。命令结束后对应容器、网络和 volumes 查询均为空。

### 7.3 Frontend

```bash
cd frontend && npm test -- --run
```

结果：通过，7 个测试文件、33 个测试全部通过。

```bash
cd frontend && npm run typecheck && npm run build
```

结果：通过，TypeScript/Vue 类型检查和 Vite production build 完成。

### 7.4 治理、脚本和格式

```bash
python3 -m unittest discover -s scripts/ci -p 'test_*.py'
```

结果：通过，共 11 个测试。

```bash
bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-business.sh
```

结果：通过。

```bash
scripts/verify-business.sh --self-test
```

结果：通过；接受合法目标，并在不访问 Docker 的情况下拒绝 6 个不安全目标。

```bash
git diff --check
```

结果：通过。

Bash/Go 文件的 LF checkout 和 Bash 可执行位检查通过。

### 7.5 完整业务与浏览器验收

```bash
scripts/verify-business.sh
```

结果：通过。隔离项目 `gopulse-acceptance-7694b76f0087` 覆盖：

- 从空库迁移和应用启动。
- API 注册、认证恢复、发帖、分页、评论、点赞和取消点赞。
- 真实 Chromium 页面渲染与交互，Playwright 1/1 通过。
- Backend 重启后业务事实保持。
- 验收 Redis 清空后的缓存重建。
- Redis 停止时 readiness 降级、核心业务继续使用 MySQL。
- Redis 恢复后无需重启 Backend 即恢复 readiness 和缓存能力。
- 成功后只清理本次验收资源。

清理复核结果：该验收项目的容器、网络和 volumes 均为空。

### 7.6 远程 CI 证据边界

本次尝试从当前 GitHub CLI 上下文读取 PR #27 / 基线提交的远程 checks，但未获得可用结果。因此：

- 不将远程 GitHub-hosted runner 状态写成“已独立验证”。
- `.github/workflows/quality-gates.yml` 的本地等价命令和额外完整业务验收均已通过。
- 建议在阶段最终收口记录中补存可访问的远程质量门禁结果。

## 8. Phase 2 交接评估

Phase 1 已提供可用于 Phase 2 的稳定同步事实基础：

- `comment.created`、`post.liked`、`post.unliked` 均有明确的 MySQL 成功边界。
- 评论和点赞 API 已具备稳定错误码、认证上下文和幂等语义。
- Redis 只承担可降级投影缓存，不会与后续消息事实竞争。
- RabbitMQ 当前仅属于 readiness 依赖，尚未侵入 Phase 1 业务成功语义。
- 完整业务验收能够作为 Phase 2 引入 Producer/Consumer 后的必要回归基线。

Phase 2 实施时必须继续保持：

1. 先提交 MySQL 核心事实，再触发异步动作。
2. RabbitMQ 暂时不可用不能回滚已提交事实或把成功响应改成失败。
3. Consumer 结果不能替代 Phase 1 查询接口的事实来源。
4. 对消息重复、失败重试和幂等处理建立独立验收，不把这些问题转嫁给 Redis 缓存。
5. 扩展共享基础设施或持久化契约时，再扩大回归范围；否则按批次验收标准停止验证。

## 9. 最终结论与关闭条件

**最终结论：有条件通过（Conditional Pass）。**

Phase 1 核心产品实现可以视为可用，Backend、Frontend、MySQL、Redis、脚本和浏览器业务闭环均有通过证据，没有发现需要回滚或阻断使用的 P0/P1 缺陷。当前条件主要属于阶段治理完整性和故障体验改进。

建议按以下顺序关闭：

1. **Phase 2 开始前必须完成**：修正 Phase 1 总实施方案的批次状态，并记录 Phase 级最终验收结论。
2. **近邻 Frontend 批次完成**：区分认证失败与临时恢复故障，增加错误/重试状态及自动化测试。
3. **非阻断跟进**：统一或明确 Frontend package version 与根 `VERSION` 的关系。
4. **证据补全**：在可访问 GitHub checks 时补存 Phase 1 最终基线的远程质量门禁结果。
5. **持续接受但监控**：保留 cache-aside TTL 有界陈旧语义，并在后续阶段增加指标；除非产品需求变化，不把复杂强一致缓存治理提前纳入当前整改。

完成第 1 项后，Phase 1 可在仓库治理层面正式关闭；第 2、3、4 项可作为明确登记的后续工作，不需要否定本次已经通过的核心业务验收。
