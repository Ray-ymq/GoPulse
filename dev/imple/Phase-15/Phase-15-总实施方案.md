# Phase 15：告警与管理端闭环总实施方案

> 当前状态：实施中；Phase-15-01 至 Phase-15-05 已完成并通过固定批次验收（当前 `1.12.5`），Phase-15-06 待实施。本文档于 2026-09-12 基于主远程 `upstream/main` 提交 `53615805aaa73190121be35e2e7b54b4af4d3b3a`、Phase 14 已完成产品版本 `1.11.7` 与完整 Compose 产品基线编写。Phase 15 使用 `1.12.x` 版本线，拆分为 6 个执行批次。本文档是 Phase 15 批次顺序、目标版本和开发分支的唯一权威来源；每批开工时仍须 fetch 主远程，从包含全部前置批次的最新 `upstream/main` 创建对应 `develop/x.x.x` 分支。

## 1. 阶段目标

在不引入外部通知、细粒度 RBAC、Kubernetes 或静态演示数据的前提下，把 Phase 14 已完成的业务、六类插件、六个自研组件和 Metrics / Logs / Events 查询能力收敛为可由超级管理员操作的内部告警与独立管理产品：

```text
旧 admin 数据与会话
  → user / super_admin 最终双角色
  → 唯一引导超级管理员受保护
  → 按用户 ID 查询与调整其他账号角色

Metrics / Logs / Events 服务端固定目录
  → 受限规则
  → 有界评估
  → pending / firing / recovered 或显式 closed
  → 当前告警、历史和审计

同一浏览器 origin
  ├─ 用户 Frontend
  └─ 独立管理 Frontend
       → 默认真实大屏
       → Metrics / Logs / Events / 六插件
       → 告警规则 / 当前 / 历史
       → 用户角色 / 管理审计
```

阶段完成必须同时证明：

- 数据库、Backend 和两个 Frontend 只接受 `user` 与 `super_admin`；旧 `admin` 可解释升级，旧会话因只以用户 ID 定位身份而无需强制失效。
- 系统有且只有一个被标记的引导超级管理员，产品路径不能删除或降级它；可存在多个其他超级管理员。
- Metrics、Logs 和 Events 各有至少一条基于真实数据的规则能触发、持续、恢复；同一持续异常只更新同一告警实例。
- 告警对象、阈值、标签、时间窗口与过滤条件均来自 Backend 固定目录，不接受任意 PromQL、Elasticsearch DSL、URL、index、SQL 或脚本。
- 管理 Frontend 是与用户 Frontend 分开构建、分开路由和分开部署单元的真实应用，但经现有 edge 对外暴露同一 origin 并共享 HttpOnly 会话 Cookie。
- 超级管理员登录或恢复会话后默认进入管理大屏；普通用户即使直接请求管理路由或 API 也不获得管理数据。
- 角色、告警规则和六插件的管理变更有脱敏审计记录；任何公共 DTO、日志、告警、审计、Frontend bundle 或 DOM 都不包含 Secret 或原始凭据。
- 关闭告警评估，或 VictoriaMetrics / Elasticsearch / Monitor / 某个 Exporter 故障，不改变社交业务的就绪条件、MySQL 事实或用户权限。

只添加数据表、只在单元测试中伪造触发、把现有管理页继续留在用户 bundle、用 fixture 或直接写库冒充三类真实告警，均不构成 Phase 15 完成。

## 2. 产品范围与非目标

### 2.1 本阶段交付

- `admin` 到 `super_admin` 的幂等数据迁移、运行时语义、旧会话兼容与 Frontend 验证器更新。
- 引导超级管理员的单例持久标识、外键删除保护、初始化/恢复命令与启动不变式检查。
- 按用户 ID 精确查询与 `user` / `super_admin` 调整 API；无用户名模糊搜索、批量授权或第三种角色。
- MySQL 权威的脱敏管理审计记录，覆盖角色变更、告警规则变更与插件管理命令。
- 受限告警目录、规则 CRUD/启停、Metrics/Logs/Events 评估器、持久状态、当前告警和历史查询。
- 评估租约、有界并发/超时、无数据或依赖失败的 `unknown` 语义、重复抑制、重启恢复与有界关闭。
- 新的 `admin-frontend` 独立应用与容器，从现有 `frontend` 迁出 Metrics、Logs、Events 和六插件页面。
- 服务端聚合的管理大屏，以分区部分降级方式展示组件、关键指标、Logs、Events、六插件、当前和近期告警。
- 管理端告警规则、当前/历史、用户角色和审计页面，全部使用真实 Backend 数据。
- 完整 Compose 下的角色升级、两应用同源会话、三源告警、故障隔离、浏览器与 Phase 13/14 回归。

### 2.2 明确不做

- 邮件、短信、Webhook、IM、PagerDuty 等外部通知，以及排班、升级、确认、静默窗口或自动修复。
- 任意告警表达式、用户自定义 PromQL/DSL/SQL、脚本执行、动态数据源、跨规则组合或异常检测。
- 多租户、组织、权限点、角色自定义、第三种角色或资源级 RBAC。
- 用户列表浏览、模糊搜索、批量角色操作、用户删除或账号封禁。
- 重写 Phase 14 的插件运行时、新增插件/指标，或让 Frontend 直连 Monitor、Exporter、VictoriaMetrics、Elasticsearch 或 Kafka。
- 稳定的外部域名/TLS 产品入口、多架构制品、macOS/Windows 真实宿主验收、升级/备份/恢复产品化；它们属于 Phase 16。
- Kubernetes、Ingress、集群对象规则或集群自动发现。
- 默认独立代码 Review、依赖审计、覆盖率运动或与验收无关的界面重构。

## 3. 真实基线与关键约束

- 根 `VERSION` 与 Frontend 版本为 `1.11.7`；Phase 14 六插件、六组件指标、Compose 验收与 Review 整改已合入主线。
- 当前 MySQL `users.role` 只允许 `user|admin`，Backend `RequireAdmin` 每次从 MySQL 重读角色，JWT 不携带授权事实；这是角色及时生效的必须保留边界。
- 当前 `admin-role promote --username` 可任意把用户改成 `admin`，没有引导账号持久身份、降级 API 或管理审计库。
- 当前只有一个 `frontend` Vue/Vite 应用；用户页与 `AdminLayout`/Observability 页共用同一 bundle和 Nginx 根目录。
- 当前 Frontend 是唯一宿主发布的 edge service，同源代理 `/api/v1`。Phase 15 保留这一入口，将新 `admin-frontend` 置于内部 edge network，不额外发布宿主端口。
- Backend 已持有 VictoriaMetrics 和 Elasticsearch 的受限查询客户端，Metrics 有固定 metric/source/target/producer/label 目录，Logs/Events 有固定 vocabulary 和 24h 最大查询范围。告警必须复用这些信任边界，不经公共 HTTP 回环。
- 现有 Events 只接受 Monitor 的固定插件/采集事件。Phase 15 管理审计以 MySQL 为权威来源，不为了展示审计而放开任意 Events envelope。
- Phase 15 继续在维护中的 Linux/Bash/Compose 路径实施和验收；原生 macOS/Windows 和 Kubernetes 都不是开工或完成条件。

## 4. 权威批次、版本与分支分配

Phase 15 的 patch `0` 是阶段分配基线，不表示当前产品已是 `1.12.0`，也不创建空提交。6 个执行批次按下表顺序实施：

| 批次 | 目标版本 | 开发分支 | 可独立验收的闭环 |
| --- | --- | --- | --- |
| Phase-15-01 | `1.12.1` | `develop/1.12.1` | `admin` 无损迁移、引导超级管理员保护、按 ID 角色变更与管理审计 |
| Phase-15-02 | `1.12.2` | `develop/1.12.2` | 受限告警规则、持久状态机与真实 Metrics 触发/持续/恢复 |
| Phase-15-03 | `1.12.3` | `develop/1.12.3` | Logs/Events 计数规则、三源重复抑制、重启恢复与依赖故障隔离 |
| Phase-15-04 | `1.12.4` | `develop/1.12.4` | 独立管理 Frontend、同源会话与现有 Metrics/Logs/Events/六插件管理能力迁移 |
| Phase-15-05 | `1.12.5` | `develop/1.12.5` | 真实管理大屏、告警页、用户角色页和审计页的浏览器闭环 |
| Phase-15-06 | `1.12.6` | `develop/1.12.6` | 完整 Compose 下的升级、双应用、三源告警、负向权限、故障隔离与阶段收口 |

同一批次的全部实现提交共享该批目标版本。批次完成时同步根 `VERSION`、用户 Frontend、管理 Frontend 与既有受管镜像/环境版本元数据；本次规划不修改 `VERSION`。

已创建或已推送的分支不得静默改名。若实施前调整批次数量或顺序，必须先更新本表，再重算所有尚未创建的分支。

## 5. 跨批次顺序、依赖与拆分理由

```text
15-01 双角色 + 引导账号 + 审计基础
   ↓
15-02 规则/状态机 + Metrics 评估
   ↓
15-03 Logs/Events 评估 + 三源可靠性
   ↓
15-04 独立管理应用 + 既有能力迁移
   ↓
15-05 大屏 + 告警/角色/审计产品闭环
   ↓
15-06 完整 Compose 与阶段收口
```

本阶段超过三个批次，是因为有五类必须分开收敛的持久数据与产品风险：

1. 角色 ENUM、旧管理员数据、现有会话和初始化命令必须先完成可重试升级，否则后续所有管理 API 都没有稳定授权根。
2. Metrics 是有值序列，Logs/Events 是有延迟的时间窗口计数；它们的无数据、恢复和依赖故障语义不同，不应在一批中混成一个未验证评估器。
3. 把管理页从已上线的用户 bundle 拆出会改变构建、Nginx、Compose、SPA fallback 和会话恢复，需要在新页面开发前独立证明既有管理能力不回归。
4. 大屏是跨 VictoriaMetrics、Elasticsearch、Monitor 和 MySQL 的部分可用聚合，而告警/用户/审计页是新产品闭环；它们需要在已稳定的独立应用上验收。
5. 最后一批只做跨批次组合、真实故障与回归，不应同时承担首次数据迁移、状态机设计或 Frontend 拆分。

每个后续批次从前序已合入的最新 `upstream/main` 开始。不允许在 Phase-15-01 预做全部告警或管理端，也不把 Phase-15-06 变成默认 Review 或覆盖率扩展批次。

## 6. 目标架构与信任边界

### 6.1 运行拓扑

```text
Browser 唯一 origin
  → frontend edge（唯一宿主发布端口）
       ├─ /, /posts, /login ... → user Frontend assets
       ├─ /admin/...            → admin-frontend internal service
       └─ /api/v1/...            → Backend

Backend
  ├─ authentication + DB-authoritative super_admin authorization
  ├─ user role / audit / alert rule / incident repositories → MySQL
  ├─ bounded alert evaluator
  │    ├─ metric catalog client → VictoriaMetrics
  │    └─ log/event count repositories → Elasticsearch read aliases
  ├─ dashboard partial aggregation
  └─ exporter plugin client → Monitor

Monitor / Router / Kafka / Marshaller / VictoriaMetrics / Elasticsearch
  → 保留 Phase 14 现有信任、传输、存储和故障边界
```

### 6.2 必须保留的边界

- Backend 是唯一公共管理 API，授权必须在访问上游、返回部分数据或进行持久变更前完成。
- 授权每次按会话中用户 ID 重读 MySQL 当前角色；Frontend 隐藏、路由守卫或旧 JWT 中任何值都不构成授权。
- 评估器调用 Backend 内部仓储/客户端，不使用管理员 Cookie 请求自己的公共 API，不把 Secret 或原始上游错误存入规则/告警。
- MySQL 是角色、引导账号、规则、评估状态、告警历史和审计的权威库；VictoriaMetrics/Elasticsearch 只是评估输入，不回写管理事实。
- 规则只可引用服务端 catalog 中的完整对象和精确标签 tuple；不接受自由 matcher、通配符、正则表达式或用户提供的上游地址。
- 两个 Frontend 都不保存认证 token；继续使用同一 `Path=/` 的 HttpOnly Cookie。候选 Secret 不回填、不进 URL/localStorage、提交结束或离开页面后清除。
- 告警评估不是 Backend `/ready` 必要条件；它的 panic、超时或依赖失败被限制在单次评估，不停止 HTTP server、Outbox 或社交业务。

## 7. 最终角色、迁移与引导账号

### 7.1 角色不变式

- 持久值、Go 常量、公共 `users/me` DTO 和两个 Frontend 联合类型只允许 `user|super_admin`；完成迁移后出现 `admin` 或其他值是安全失败，不降级为 `user`。
- 公开作者摘要、用户搜索、帖子、评论、通知、缓存与搜索投影继续不包含角色；只有当前用户和受权管理 DTO 可返回角色。
- `super_admin` 仍是可认证用户，Backend 现有通用社交 API 不因此默认禁止；产品导航和默认落点将其送往独立管理端。
- 任意账号的角色改变对后续请求立即生效：降级的现有管理会话下一个管理 API 得到 `403 permission_denied`，但会话仍可作为普通用户使用。
- 非引导 `super_admin` 允许降级自己；返回成功后其管理权限立即失效。引导账号使系统始终保留一个管理恢复根。

### 7.2 数据迁移和可重试性

Phase-15-01 使用当时下一个可用 migration（基于当前主线预计为 `000012`），语义顺序固定为：

1. 先把 `users.role` 临时扩大为 `user|admin|super_admin`，再把全部旧 `admin` 原位改为 `super_admin`，最后收窄为 `user|super_admin`。
2. 创建单例引导表，固定 singleton key、唯一 `user_id` 与指向 `users.id` 的 `ON DELETE RESTRICT` 外键。
3. 升级库已有一个或多个旧 `admin` 时，以最小稳定用户 ID 作为引导账号，其他旧管理员均保留为可降级 `super_admin`；不随机选择，不删除任何账号。
4. 空库没有旧管理员时，允许先注册一个普通用户，再通过只连 MySQL 的 operations 命令一次性声明它为引导账号；声明成功后不允许换成另一个用户。
5. 考虑 MySQL DDL 隐式提交，每一步必须可在中断后重跑；对已收窄 ENUM、已转换数据或已存在 singleton 不重复修改语义。
6. down migration 先兼容扩宽、将所有 `super_admin` 映射回 `admin`，再移除 singleton/新约束；它不删除用户。真实产品回滚是受控运维操作，不由 Frontend 发起。

### 7.3 引导命令与保护

- 保留 Backend 镜像中的 operations 二进制，新规范命令以精确用户 ID 声明引导账号，仅在 singleton 空缺时把该用户与标记原子写入。
- 旧 `admin-role promote --username` 仅作一个版本的安全兼容入口：singleton 空缺时可声明匹配账号，已存在且是同一账号时幂等成功，指向其他账号时安全冲突；不再用它创建第二个超级管理员。
- 引导账号删除由数据库外键拒绝，角色降级由服务事务在锁定用户与 singleton 后拒绝。当前产品不新增用户删除 API。
- Backend 启动检查 singleton 与对应角色是否一致。无引导账号的新装环境允许社交系统启动以完成首用户注册，但所有管理路径保持不可用并显示固定 setup 状态；Phase 15 验收和日常 Compose 必须完成声明。

### 7.4 角色 API 与并发

- `GET /api/v1/admin/users/:userId` 只接受标准正整数 ID，返回 `id,username,role,created_at,is_bootstrap_super_admin`；不提供列表或模糊搜索。
- `PUT /api/v1/admin/users/:userId/role` 严格接受 `{"role":"user|super_admin"}`，在一个 MySQL 事务中锁定 actor、target 与 singleton，进行保护校验、写角色和成功审计。
- 重复设置为当前角色是幂等成功，不重复写入角色变更审计。不存在用户返回 404，引导账号降级返回稳定 409 保护错误。
- 未登录返回 `401 authentication_required`，普通用户返回 `403 permission_denied`；验证和授权失败时不查询目标用户详情。

## 8. 管理审计合同

### 8.1 权威记录

MySQL `management_audit_events` 使用不可变追加记录，最小字段为：

`id`、`operation_id`、`occurred_at`、`actor_kind=user|system`、`actor_user_id`、`action`、`resource_type`、`resource_id`、`phase=requested|completed`、`outcome=succeeded|failed|unknown`、`request_id` 与受限 `details_json`。

- 角色和告警规则的成功数据变更与 `completed/succeeded` 审计同事务提交；事务失败时两者都不写成成功。
- 插件操作跨越 Backend 与 Monitor，Backend 在调用前先追加 `requested/unknown`，完成后再追加同 `operation_id` 的 `completed/succeeded|failed`。如完成记录不可写，请求记录保留 `unknown`，不伪造远程回滚。
- 覆盖的变更动作固定为：引导账号声明、用户角色变更、规则创建/更新/启用/停用/删除、插件 connection-test/安装/配置/启动/停止/更新，以及系统引发的告警触发/恢复/显式关闭。
- 读查询不默认进入审计表；被拒绝的未授权请求继续进安全结构化日志，不为每次扫描制造持久记录。

### 8.2 详情与脱敏

- `details_json` 由 action 对应的服务端结构产生，拒绝任意 map。可包含角色 before/after、rule ID/revision/severity/source、plugin ID/operation/version/reason code；不包含密码、token、Cookie、连接串、原始配置、主机、用户名候选值或上游错误。
- `actor_user_id` 和目标用户 ID 只在受 `super_admin` 授权的审计 API 返回，不加入 metrics label 或公开日志。
- `GET /api/v1/admin/audit-events` 使用签名 cursor、`limit=1..100` 与最大 90 天单次时间范围，过滤值只取 action/resource/outcome 固定词表。

## 9. 受限告警规则合同

### 9.1 公共规则形状

规则公共字段固定为：

`id`、`name`、`enabled`、`severity=warning|critical`、`source=metrics|logs|events`、`selector`、`reducer`、`operator`、`threshold`、`window`、`for`、`revision`、`created_by`、`updated_by`、`created_at`、`updated_at`。

- `name` 是 1..80 个 UTF-8 字节的可见文本，不允许控制字符；未删除规则中名称唯一。
- 全系统最多 32 条未删除规则，不因管理员增加而动态扩容。删除为软删除，历史告警保留快照。
- `window` 只允许 `1m|5m|15m`，`for` 只允许 `0s|1m|5m`；`for` 不得大于 `window`。
- `operator` 只允许 `gt|gte|lt|lte|eq|neq`，`threshold` 必须是有限 JSON number 且绝对值不超过 `1e15`。
- 对已有规则的 PUT、enable、disable 和 DELETE 必须提交当前 `revision` 作为乐观并发条件，不匹配返回 409；成功变更后 revision 递增。
- 任何影响评估的更新都显式关闭当前 firing 实例，原因为 `rule_updated`，再从 normal 使用新 revision 评估；disable/delete 分别使用 `rule_disabled|rule_deleted`，不伪称数据已恢复。
- 这里的“影响评估的更新”指任意成功 `PUT`（name、severity、selector、reducer、operator、threshold、window 或 for）；enabled 只能经独立 enable/disable endpoint 改变。新建规则必须显式提交 enabled，初始状态据此为 normal 或 disabled。

### 9.2 Metrics 规则

- `selector` 只包含 `metric` 和 `labels`。`metric` 必须命中 Backend 已有 metric catalog；source、target ID、producer 由服务端推导，不由请求覆盖。
- 规则必须精确提供该 metric 目录要求的全部非身份 label，且每个值命中 Phase 14 的固定 tuple；无标签 metric 使用空对象。额外、缺失、正则或通配值均拒绝，因此每条规则最多命中一个 series。
- gauge 允许 `last|max|min|avg`，counter 只允许 `increase`；`increase` 按相邻点正增量求和，观测到 counter reset 时从新值开始，不伪造负数。
- 规则只根据时间窗口内的真实点评估。无 series、无点、最新点超过新鲜度界限或 VictoriaMetrics 失败都是 `unknown`，绝不当作 0。

### 9.3 Logs 规则

- `selector` 只允许 `service,module,level,message,error_code` 中至少一项，组合必须命中现有 Log vocabulary；不接受 request/event/user/post/comment/notification/outbox ID、自由文本或原始 error。
- `reducer=count`，评估器在自己生成的 UTC 时间窗口中使用 Elasticsearch read alias 计数；成功查询且无匹配时值为 0。

### 9.4 Events 规则

- `selector` 只允许 `source,event_name,severity,plugin_id,operation,error_code` 中至少一项，必须命中现有 Monitor event vocabulary 的合法组合。
- `reducer=count`，窗口与 0 值语义同 Logs。Phase 15 不允许规则读取自己产生的管理审计或告警转换事件，从源头避免告警递归。

### 9.5 告警目录

`GET /api/v1/alerts/catalog` 返回上述固定 source、metric/reducer/label tuples、log/event selector vocabulary、operator、window、for 和 severity，不返回 PromQL、index/alias 名、上游 URL、认证信息或内部超时。管理 Frontend 只依赖此目录生成表单。

## 10. 评估、状态机与重复抑制

### 10.1 有界调度

- 全局评估 tick 固定为 30s，为每轮加入小幅固定上界 jitter，不由用户配置秒级频率。
- 评估截止点为 `now-15s` 以允许现有采集/传输延迟；Metrics 最新点必须不早于截止点前 90s，否则为 `unknown`。Logs/Events 对 `[cutoff-window, cutoff]` 计数。
- 最多 4 个规则并发评估，每个上游请求最长 2s，每条规则通过 MySQL claim/lease 防止多 Backend 重复评估；租约超时可重试，不以进程内锁作为持久事实。
- 评估以 rule ID 稳定顺序安排；一轮未完成不堆叠下一轮任务，不创建无界内存或本地磁盘队列。
- `ALERT_EVALUATION_ENABLED=false` 只停止新评估，规则/当前/历史查询仍可用，不改写已 firing 告警，不影响 Backend readiness。

### 10.2 最小状态机

```text
disabled ──enable──→ normal

normal ──condition=true, for>0──→ pending
normal ──condition=true, for=0──→ firing（创建一条 incident）
pending ──连续 true 达到 for──→ firing（创建一条 incident）
pending ──false 或 unknown──→ normal
firing ──true──→ firing（只更新原 incident）
firing ──false──→ recovered（结束原 incident）→ normal
firing ──unknown──→ firing + data_status=stale
normal ──unknown──→ normal + data_status=unknown
firing ──update──→ incident closed + state normal
firing ──disable/delete──→ incident closed + state disabled
```

- `alert_rule_states.state` 只允许 `disabled|normal|pending|firing`；`recovered|closed` 只属于 incident 终态，不能写入 rule state。
- `for` 的连续性只由成功 true 评估证明；unknown 中断 pending，不得靠墙钟穿过空白期触发。
- firing 期间的每次 true 只原位更新 `last_triggered_at`、`last_evaluated_at`、最新值和 `evaluation_count`，不新建 incident、history 或审计记录。
- false 才是数据恢复；依赖失败、无数据、评估关闭、规则停用/删除/更新都不伪称 recovered。
- 每条规则只有一行持久评估状态与最多一个 active incident；转移使用同一 MySQL 事务锁定规则、状态与 active incident，重复执行不产生第二个 firing 实例。
- 进程重启从 MySQL 状态继续；过期 lease 可重新 claim，已经提交的 incident 不因重启重建。

## 11. 持久模型与查询语义

### 11.1 持久实体

- `alert_rules`：受限标量字段与 canonical selector JSON、revision、enabled、soft-delete 时间、creator/updater。JSON 在写入前必须解析为 source-specific 结构并重新序列化，不保存未知键。
- `alert_rule_states`：每 rule 一行，保存 state、data status、pending since、active incident ID、last value/count、last evaluated/success timestamp、safe error code、lease owner/until。
- `alert_incidents`：保存 rule ID/revision/name/severity/source/object 快照、`status=firing|recovered|closed`、first/last trigger、recovered/closed time、resolution reason、last value/count 和 evaluation count。快照使软删除规则历史仍可解释。
- `management_audit_events`：按第 8 节追加。

表之间的外键不使用级联删除破坏历史。时间统一 UTC，持久值使用微秒或更精确的有序时间精度，绝不使用 Frontend 时钟。

### 11.2 当前与历史

- 当前告警只返回 `status=firing` 的 incident，按 severity 后 first-triggered 稳定排序；pending 在规则状态中展示，不冒充已触发告警。
- 历史返回 firing/recovered/closed 快照，固定包含阶段提纲要求的规则、严重程度、对象、首次/最近触发和恢复状态。
- list API 使用签名 keyset cursor、`limit=1..100`、最大 90 天单次时间范围；不接受 offset 无界扫描或任意 JSON 过滤。
- Phase 15 不自动删除历史告警或审计；重复抑制的有界性指同一连续 firing 不增加行。长期归档策略属于后续运维规划。

## 12. Backend API 合同

以下路由全部在认证后使用新 `RequireSuperAdmin` 并重做严格请求形状验证：

| method / path | 请求 | 成功结果 |
| --- | --- | --- |
| `GET /api/v1/admin/users/:userId` | 无 body/query | 精确用户与 bootstrap 标识 |
| `PUT /api/v1/admin/users/:userId/role` | 严格 role JSON | 当前角色与 `changed` |
| `GET /api/v1/admin/audit-events` | 固定过滤、limit/cursor | 脱敏审计页 |
| `GET /api/v1/admin/overview` | 最多固定 `range=15m` | 分区状态的大屏快照 |
| `GET /api/v1/alerts/catalog` | 无 body/query | 服务端可告警目录 |
| `GET /api/v1/alerts/rules` | 固定 status/source + cursor | 规则列表与评估状态 |
| `POST /api/v1/alerts/rules` | 严格完整规则 | revision 1 规则 |
| `GET /api/v1/alerts/rules/:ruleId` | 无 body/query | 规则与当前状态 |
| `PUT /api/v1/alerts/rules/:ruleId` | 完整规则 + 预期 revision | 新 revision 规则 |
| `POST /api/v1/alerts/rules/:ruleId/enable` | 仅预期 revision | 已启用规则 |
| `POST /api/v1/alerts/rules/:ruleId/disable` | 仅预期 revision | 已停用规则 |
| `DELETE /api/v1/alerts/rules/:ruleId` | 仅预期 revision | `204` 与软删除 |
| `GET /api/v1/alerts/current` | severity/source + cursor | firing incident 页 |
| `GET /api/v1/alerts/history` | 固定 status/severity/source/rule + 时间/cursor | 历史 incident 页 |

- 所有 JSON 拒绝未知/重复字段、多个 JSON 值、非 UTF-8、非有限数和超限 body。
- 稳定错误至少区分 validation、not found、permission denied、bootstrap protected、revision conflict、rule limit 和 alerts unavailable；不返回 SQL、PromQL、index、URL 或上游原始错误。
- 现有 `/api/v1/observability/*` 与 `/api/v1/exporter-plugins/*` 只将中间件语义从 admin 替换为 super_admin，不改变 Phase 14 公共 DTO、单实例和 Secret 合同。

## 13. 独立管理 Frontend 与同源会话

### 13.1 两个真实应用

- 新建独立 `admin-frontend/` Vue/Vite 工程，拥有自己的 package/lockfile、entrypoint、router、页面、单元测试、build 输出和 Nginx runtime image。
- 从 `frontend/` 移出 `AdminLayout`、Observability views/services/types 与对应路由/测试；用户应用的产物中不再包含管理业务页。两端可复用协议级小工具，但不共用业务页、布局或路由树。
- Compose 增加内部 `admin-frontend` service，不发布 host port；现有 `frontend` edge 在 `/admin/` 下反向代理它，`/api/v1` 继续代理 Backend。
- 管理 SPA base 固定 `/admin/`，内部路由为 `/admin/`、`/admin/metrics`、`/admin/logs`、`/admin/events`、`/admin/plugins`、`/admin/alerts`、`/admin/users`、`/admin/audit`。现有 `/admin/observability/*` 在本阶段保留明确重定向，不留下第三套页面。

### 13.2 登录、恢复与角色改变

- `/login` 继续由用户 Frontend 提供。登录或 `/users/me` 恢复得到 `super_admin` 时，无安全 redirect 目标则导航到 `/admin/`；`user` 默认到 `/posts`。
- redirect 只接受同 origin 绝对 path，并且目标必须与当前角色对应；拒绝 scheme/host、`//`、编码绕过或普通用户的 `/admin/` 目标。
- 管理应用启动先请求 `/api/v1/users/me`；401 转到 `/login?redirect=<safe-admin-path>`，角色不再是 `super_admin` 则转到 `/posts`，不在旧 DOM 中保留管理数据。
- 用户应用恢复到 `super_admin` 时导向管理应用。Frontend 分流只是产品体验，Backend 的数据库权威授权仍是安全根。

### 13.3 页面状态与真实数据

- 每个页面都有独立加载、空、部分失败、完全失败、过期权限和重试状态，不用假卡片、硬编数值或未请求的成功状态。
- 浏览器不组装 PromQL/ES DSL，不自行推导健康结论；大屏和告警对象均使用 Backend 已验证 DTO。
- 时间以 Backend UTC RFC3339 返回，Frontend 按浏览器 locale 显示并标明时区；不用浏览器时钟决定告警状态。

## 14. 管理大屏聚合合同

`GET /api/v1/admin/overview?range=15m` 是服务端有界 fan-out，返回 `generated_at`、全局 `status=healthy|degraded|unavailable`与以下独立 section：

| section | 权威输入 | 最小内容 |
| --- | --- | --- |
| `components` | Monitor 采集成功时间 + 六组件 dependency metrics | 六个固定 ID、healthy/degraded/unknown、最近样本时间与安全 reason |
| `key_metrics` | Backend metric catalog | outbox pending、worker in-flight、indexer retrying、Monitor queue、Router buffered records、Marshaller retrying 的最新值/时间 |
| `logs` | Elasticsearch logs read alias | 15m 内 info/warn/error 计数和查询状态 |
| `events` | Elasticsearch events read alias | 15m 内 info/warn/error 计数和查询状态 |
| `plugins` | Monitor safe plugin status + 六个 `gopulse_*_up` | 六 ID 的 installed/desired/observed/up/最近采集 |
| `alerts` | MySQL alert state/incidents | warning/critical firing 数、pending/unknown 数、近期恢复摘要、评估器 enabled/最近成功 |

- 每个 section 自带 `status`、`observed_at` 和固定 `reason_code`；一个上游失败不丢弃其他 section。至少一区可用时返回 200 与 partial DTO，只有认证/授权/基本 MySQL 请求无法建立时返回整体错误。
- fan-out 使用有界并发与子请求超时，总时间预算不超过 3s；请求取消时取消尚未完成的上游工作。
- 大屏是状态摘要，不替代各专项查询页。不将旧值标为当前 healthy，不用客户端缓存隐藏 unavailable。

## 15. 故障隔离、重启与安全

- VictoriaMetrics 不可用：Metrics 规则进入 unknown，已 firing 不自动恢复；Logs/Events 规则、MySQL 规则/历史、角色和社交业务继续，大屏只降级相关区。
- Elasticsearch 不可用：Logs/Events 规则进入 unknown，Metrics 规则和 MySQL 管理能力继续；不因 0 条伪恢复。
- Monitor 或单 Exporter 故障：沿用 Phase 14 状态/事件/指标语义，可由 Metrics 或 Events 规则观测；管理插件 section 可局部 unavailable，不阻塞用户 Frontend。
- Backend 重启：不在内存中重置 pending/firing，过期 lease 回收，不创建重复 incident；有界 shutdown 先停新 claim，再等待已开始的最多 2s 查询结束/取消。
- 角色变更与规则变更使用事务和行锁；多个管理员竞争时只有一个 revision/角色结果生效，其他请求得到可重新读取的冲突。
- 故意使用包含特征串的 Secret、插件配置和上游错误后，API、告警 object/value、审计 details、结构化日志、Frontend DOM/bundle 和验收输出都查不到该特征串。

## 16. 各批次职责边界

### Phase-15-01：超级管理员迁移与角色审计闭环

- 交付角色迁移、引导账号保护、安全兼容命令、`RequireSuperAdmin`、按 ID 查询/变更和审计基础。
- 将现有 Observability/插件 API 和当前 Frontend 临时语义更新为 `super_admin`，确保后续拆应用前不出现管理回归。
- 不实现告警表、评估器或独立管理 Frontend。

### Phase-15-02：告警规则与 Metrics 评估闭环

- 交付规则/状态/incident 迁移、受限 catalog、CRUD/启停 API、有界 scheduler/lease 和 Metrics 适配器。
- 用真实 Phase 14 metric 及真实异常完成 trigger/continue/recover，验证进程重启与单 active incident。
- 暂不允许创建 Logs/Events 规则；catalog 可显示后续来源为不可用，不伪造实现。

### Phase-15-03：Logs 与 Events 告警及故障隔离闭环

- 交付 Logs/Events 固定 count 查询、延迟窗口、0 值和 unknown 语义，封闭三源 catalog。
- 证明三条真实规则各自触发/持续/恢复，重复评估、重启、上游不可用和 evaluator 关闭不伪造记录或影响业务。
- 不增加外部通知、自由查询或告警自监控递归。

### Phase-15-04：独立管理 Frontend 与既有管理能力迁移

- 交付 `admin-frontend` 工程/镜像/内部 service，edge `/admin/` 路由、同源会话恢复和安全角色分流。
- 从用户应用迁移 Metrics、Logs、Events 和六插件页，使用真实 API 完成普通用户负向、超级管理员正向和降级后退出。
- 默认页可暂为有真实连接状态的最小管理首页；完整大屏与新告警/角色页属于下一批。

### Phase-15-05：管理大屏与告警权限操作闭环

- 交付 Backend 部分可用 overview 和管理 Frontend 默认大屏，展示六组件、关键 metrics、logs/events、六插件与告警。
- 交付规则表单/CRUD/启停、当前/历史告警、按 ID 用户角色和管理审计页。
- 全部页面使用真实 Backend DTO，验证 partial failure、revision 冲突、bootstrap 保护、自降级和 Secret DOM 清理。

### Phase-15-06：Compose 集成验收与阶段收口

- 在完整 Compose 中从 Phase 14 真实角色数据升级，验证新卷引导、多超级管理员、两应用同源会话和完整负向矩阵。
- 用真实 Metrics/Logs/Events 输入完成三源触发/持续/恢复，验证重启、重复抑制、分区降级、评估关闭和强归属清理。
- 回归 Phase 13 业务主路与 Phase 14 六插件/六组件、Metrics/Logs/Events 查询；只修复真实阻断问题。

## 17. 阶段级验收标准

### 17.1 身份、角色与审计

1. 从包含两个旧 `admin` 和活动会话的 Phase 14 数据升级：两者均成为 `super_admin`，最小 ID 被稳定标记为 bootstrap，会话继续可用，重跑 migration 不改变结果。
2. 空库注册首用户并执行引导命令后，重跑同账号幂等，试图指向另一用户被拒绝。
3. 超级管理员可按 ID 提升普通用户、降级非 bootstrap 超级管理员或自己；bootstrap 的降级/删除在产品/数据库保护边界被拒绝。
4. 未登录、普通用户、超级管理员，以及 bootstrap/非 bootstrap/不存在/非法 ID 形成固定负向矩阵，不由 Frontend 代替 Backend 拒绝。
5. 成功角色、规则和插件变更均有可查审计；跨 Monitor 操作可解释 requested/completed/unknown，审计不包含 Secret 或原始配置。

### 17.2 三源告警

1. 配置一条精确 Metrics 规则，通过真实组件/插件异常使数值越界，观察 pending（如有）、firing、多轮持续同 incident 和数值恢复后 recovered。
2. 用真实产生并经 Marshaller/Elasticsearch 存储的固定 Log 与 Monitor Event 分别触发 count 规则，窗口滑出后恢复；不直接写 index 或伪造 Backend DTO。
3. firing 期间连续至少三次 true 评估只存在一条 incident 和一次 trigger 审计，`last_triggered_at/evaluation_count` 递增；重启 Backend 仍不重复触发。
4. disable/update/delete 当前规则产生 closed 原因而非 recovered；unknown 不伪恢复 firing，也不让 pending 穿越无数据期。
5. 告警历史包含规则 revision/name、severity、safe object、首次/最近触发、状态、恢复/关闭时间与原因，不包含上游查询或凭据。

### 17.3 独立管理端与大屏

1. 两个 Frontend 分别安装依赖、测试、typecheck、build 并生成两个独立制品；用户 bundle 不包含管理页路由/组件，管理 bundle 不包含社交业务页。
2. 同一 origin 下，普通用户登录默认进入用户端，超级管理员默认进入 `/admin/`；直接深链接登录和页面刷新正确。
3. 大屏真实显示六组件、六插件、关键 metrics、logs/events 计数、当前/近期告警；任一上游不可用时只降级相关区。
4. Metrics、Logs、Events 和六插件页在新应用内完成现有代表性操作，旧 `/admin/observability/*` 重定向正确，用户应用中不再挂载旧管理页。
5. 告警、用户角色和审计页完成真实 CRUD/查询/冲突/保护流程；降级当前账号后管理 DOM 清除并转入用户端。

### 17.4 故障隔离与既有回归

- 分别停止 VictoriaMetrics 与 Elasticsearch，验证相关规则 unknown、firing 不伪恢复、大屏分区降级，且不相关规则、角色/历史查询和社交主路继续。
- 设置 `ALERT_EVALUATION_ENABLED=false` 并替换 Backend，验证已有告警不被恢复/删除，API 和业务可用；重新开启后从持久状态继续。
- 普通用户完成 Phase 13 的注册/登录、发帖、评论、点赞、关注/Following、收藏、编辑、搜索、通知和删除代表性主路。
- 超级管理员完成 Phase 14 六插件状态/代表性生命周期、六类插件与六组件 Metrics、Logs 和 Events 查询，普通用户始终被 Backend 拒绝。
- 镜像版本/revision、numeric user、read-only filesystem、内部端口/网络、SIGTERM 和强归属资源清理合同继续成立。

### 17.5 阶段里程碑条件

Phase 15 只在以上验收全部通过、6 份 split plan 均有同名真实实施记录、根完成版本为 `1.12.6`、全部执行批次已按顺序合入主线且无阻断问题时完成。该结果继续推进 Milestone 4，但 Milestone 4 仍要到 Phase 17 完整工程验收后才收口；Phase 15 不得提前宣称 Milestone 4 完成。

## 18. 验证策略与固定阶段门禁

- 实施中先运行直接受影响 Go package 与对应 Frontend Vitest；每个 split plan 的固定完成门禁只对该批最终 diff 运行一次。
- Phase-15-01 建立 `scripts/verify-role-management.sh`，Phase-15-02 建立 `scripts/verify-alerts.sh`，Phase-15-04 建立 `scripts/verify-admin-frontend.sh`；每个入口都先提供不触及 Docker 的 `--self-test`。
- Phase-15-03 扩展告警入口到 `--sources metrics,logs,events --fault-isolation`，Phase-15-05 扩展管理端入口到大屏/告警/角色/审计，Phase-15-06 将它们纳入 `scripts/verify-compose.sh --phase15` 或实施记录中的等价唯一固定入口。
- 三源验收必须产生真实 metric/log/event 并经已有链路读取；只测试 reducer/state machine 或直接 INSERT incident 不足够。
- 角色负向矩阵和告警故障隔离是固定完成门槛；管理页的前端路由拒绝不代替 API 拒绝。
- 新增测试只证明新/修改验收标准、观测到的缺陷或修改的安全/持久/公共合同；一个状态转移优先一条成功与一条代表性失败。
- 已成功检查在代码、配置、依赖和环境无相关变化时不重复；Phase-15-06 引用前五批仍有效的 package 证据，只运行最终跨批 Compose 门禁。
- 只在观测到角色 migration、告警状态、同源 Cookie、共享数据库或社交业务回归的具体风险时扩大检查，并在实施记录先写明理由。
- 每批固定包含版本/分支治理检查；提交前对最终工作树运行 `git diff --check` 与 `git diff --cached --check`，提交后才使用 `git diff --check upstream/main...HEAD` 核对提交范围。

## 19. 有限前置确认与禁止猜测清单

以下项目是尚待实证的直接依赖，不是已完成结论。开工时只读直接调用点、锁定版本公开契约并运行最小受控探测；遵守 10 分钟初始发现上限和既有执行效率规则。证据显示合同不可实现时，先在 `update` 修订受影响总/分方案，不在实现中静默换语义。

| 责任批次 | 必须确认的有限输入 | 完成证据与未满足处理 |
| --- | --- | --- |
| Phase-15-01 | MySQL 锁定版本对重复 `MODIFY ENUM`/DDL 中断重跑的实际行为，以及 Phase 14 可支持旧 admin 数据 fixture | 一次有界 MySQL 集成证据；无法安全重跑则修订迁移步骤，不靠手工改库验收 |
| Phase-15-02 | VictoriaMetrics 锁定 API 对单 series 时间点、空结果、counter reset 和 Phase 14 标签 tuple 的真实响应 | 固定目录查询 + 真实窗口数值；无法区分空/0 时先修订契约，不默认 0 |
| Phase-15-03 | Elasticsearch 锁定版本对 logs/events read alias 的 count、UTC 边界、索引未就绪与不可用的区分 | 各一条真实 log/event 与空窗口；不能区分 missing/unavailable 则阻断该源，不伪造恢复 |
| Phase-15-04 | Nginx 对 `/admin`/`/admin/`、SPA deep link、静态 asset base 和同源 Cookie 的真实容器行为 | 一次独立两 bundle 浏览器探测；不使用两个宿主 origin 冒充同源会话 |
| Phase-15-05 | overview 六个 key metric 的精确 label tuple、新鲜度和 Monitor/plugin 部分失败 DTO | 记录固定目录和真实快照；缺值显示 unknown/unavailable，不用硬编数字补齐大屏 |
| Phase-15-06 | 前五批证据有效性、Phase 14 升级 fixture、三源真实触发手段与强归属故障注入 | 缺失即回到对应未满足项，不在最后一批重新选择角色/状态机/路由合同 |

前置探测只服务于当批验收，不扩展为 MySQL/Elasticsearch/VictoriaMetrics 源码审计、全量边界组合或 Frontend 重设计。结果写入同名开发记录，不额外创建默认 Review 报告。

## 20. 实施记录、版本与提交要求

每个批次完成前：

- 创建或更新与计划同名的 `dev/logs/Phase-15/Phase-15-XX-*.md`，记录实际 migration/schema/API/route、实际文件、实际命令与结果、故障注入、偏差、限制和后续项。
- 将根 `VERSION`、用户 Frontend、管理 Frontend 与既有受管版本元数据更新为分配表目标版本。
- 只提交当前批次文件，使用清晰的英文 Conventional Commit，并通过对应 `develop/x.x.x` 分支检查。
- 验收未通过时，不得将计划或记录写成已完成，不得仅因时间消耗提升版本。

## 21. 阶段完成、停止条件与 Phase 16 交接

Phase 15 的停止条件是第 17 节全部通过、无阻断失败、文档/代码/运行时/验收证据一致。达到条件后更新本文档实际状态与收口证据，提交并停止 Phase 15；不利用剩余时间扩展外部通知、RBAC、新插件、界面美化或跨平台发布。

Phase 16 从合入后的 `1.12.6` 完成基线开始。交接时必须保留：

- `user|super_admin` 两角色、数据库权威授权、引导账号不可删除/降级与按 ID 角色调整合同。
- 受限三源告警 catalog、最小状态机、重复抑制、unknown/closed/recovered 语义、MySQL 规则/状态/历史/审计数据。
- 两个独立 Frontend 工程与镜像、同源 `/admin/` 运行路径、默认角色分流、真实大屏和既有管理能力。
- 六插件、六组件、Metrics/Logs/Events 与 Phase 13 业务的 Compose 契约和局部故障边界。
- 跨平台支持矩阵、多架构制品、统一产品生命周期、从 `1.9.4` 升级与备份/恢复仍属于 Phase 16，不得从 Phase 15 验收结果推导为已完成。
