# Phase 14：插件体系与组件可观测闭环总实施方案

> 当前状态：待实施。本文档于 2026-09-09 基于主远程 `upstream/main` 提交 `8caf8d4`、Phase 13 已完成产品版本 `1.10.6` 与 Compose 产品基线编写。Phase 14 使用 `1.11.x` 版本线，拆分为 6 个执行批次。本文档是 Phase 14 批次顺序、目标版本和开发分支的唯一权威来源；每批开工时仍须 fetch 主远程，从包含全部前置批次的最新 `upstream/main` 创建对应 `develop/x.x.x` 分支。

## 1. 阶段目标

在不引入第三方代码执行、同类多实例、告警或 Kubernetes 的前提下，把当前只接受 `redis-exporter` 的单点原型收敛为受限的六类官方插件体系，并让 GoPulse 六个自研长运行组件提供可统一查询的基础运行指标：

```text
管理员选择固定官方插件
  → 按受限 Schema 提交目标配置与 Secret
  → 连接测试
  → 安全安装并启动唯一实例
  → Monitor 周期采集
  → Router → Kafka → Marshaller → VictoriaMetrics
  → Backend 固定目录查询

Backend / Business Worker / Search Indexer / Monitor / Router / Marshaller
  → 受保护的内部指标端点
  → Monitor 固定目标采集
  → 同一传输、存储和查询链路
```

阶段完成必须同时证明：

- Redis、MySQL、RabbitMQ、Kafka、Elasticsearch 和 VictoriaMetrics 六类官方插件各自对一个真实目标产生可区分、可经 Backend 查询的指标。
- 一种插件最多一个安装记录、一个运行进程和一个采集目标；Backend、Monitor API、Registry 和运行时都不存在创建同类第二实例或第二目标的通道。
- Manifest、运行契约、目标配置 Schema、制品完整性、Secret 存储、连接测试、安装/启停/更新/回滚和重启恢复均有可执行的固定边界。
- 现有 Redis Manifest v1、Registry、desired state 和已安装版本可安全迁移，不要求人工删除 `monitor_plugin_data`。
- 任一插件目标不可达、进程崩溃、配置失败或更新回滚，不中断其他插件、历史指标和社交业务。
- 六个自研组件的请求/处理结果、延迟、队列或消费进度、最近成功与依赖降级信号按职责可观测，不使用用户、帖子、事件 ID、URL 原始路径等高基数标签。
- Phase 13 业务、用户端、权限、缓存/搜索一致性与删除防复活合同不回归，并继续在 Compose 中成立。

只写出六个 Exporter 二进制、只在各自 `/metrics` 看到数据、用 fixture 代替真实目标、把 Secret 放入 Manifest/Registry/日志，或在 VictoriaMetrics 中直接人工写入结果，均不构成 Phase 14 完成。

## 2. 产品范围与非目标

### 2.1 本阶段交付

- 官方插件固定目录：`redis-exporter`、`mysql-exporter`、`rabbitmq-exporter`、`kafka-exporter`、`elasticsearch-exporter`、`victoriametrics-exporter`。
- 受限 Manifest v2 与兼容性声明，入口 SHA-256、平台/架构、契约版本和配置 Schema 完整性校验。
- 每种插件的固定配置字段、Secret 字段、目标原点策略、连接测试和一次性配置修订。
- 六个独立官方 Exporter 模块或等价清晰源码边界，以及可复现的 `tar.gz` 插件包。
- 通用多类型单实例 Plugin Manager、每插件独立操作串行化、进程归属、采集器、状态与事件。
- 向后兼容的多来源 metrics envelope、Router 固定路由词汇、Marshaller 严格验证/转换与 Backend 服务端指标目录。
- 现有管理界面的最小扩展：六类插件列表、受限配置、连接测试、安装/启停/更新和安全状态；不做 Phase 15 的独立管理 Frontend 重构。
- Backend、Business Worker、Search Indexer、Monitor、Router 和 Marshaller 的低基数基础指标、内部端点和 Monitor 固定采集目标。
- 日常 Compose 六插件冷启动/重启恢复、空插件卷管理员操作、代表性故障隔离和强归属清理验收。

### 2.2 明确不做

- 同种插件多实例、一实例多目标、动态目标 CRUD、目标自动发现或批量启停。
- 未受信第三方插件、远程插件仓库、插件市场、在线下载、数字签名基础设施、通用脚本 hook 或代码沙箱。
- 对基础设施指标做自动告警、SLO、异常检测、容量预测或外部通知；它们属于 Phase 15 或以后。
- 独立管理 Frontend、`super_admin` 角色迁移、角色管理、管理大屏或管理信息架构重做。
- 让 MySQL、Kafka、Elasticsearch 等原始高基数指标无限透传，不采集 SQL、key、queue/topic 任意名称、index 名、用户 ID 或业务内容。
- 替换 VictoriaMetrics、Kafka 或日志/事件存储，增加新的指标数据库或通用 PromQL 代理。
- macOS/Windows 产品化、多架构发布验收、备份恢复产品化或 Kubernetes。
- Phase 13 范围外的媒体、私信、转发、推荐、治理和付费等社交扩展。

## 3. 当前基线与关键约束

- 根 `VERSION` 与 Frontend 版本均为 `1.10.6`；Phase 13 的用户端、关系、收藏、帖子编辑/删除、通知墓碑与搜索防复活边界已是主线基线。
- Monitor 当前把 `PluginID` 和 Registry/运行时/采集器固定为 `redis-exporter`，全局只有一把操作锁、一个回环采集端口和一个 metrics target。
- Manifest v1 严格固定 Redis、Linux、当前架构、入口文件及 `/health`/`/metrics`；Registry 不含目标配置 Schema 或独立 Secret 引用。
- metrics Envelope v1、Router allowlist、Marshaller 解码/变换和 Backend metric catalog 都固定 `source=redis`、`plugin_id=redis-exporter`、`target_id=redis-exporter-local` 及 10 个 Redis families。
- 六个自研长运行组件已有结构化日志和不同程度的 health/ready，但尚无统一的内部 metrics 端点与低基数目录。Business Worker 和 Search Indexer 需要增加轻量内部 HTTP 生命周期。
- Phase 12 Compose 中 Monitor 通过专用 volume 保存插件，在镜像中嵌入 Redis 包并在空卷启动时调和；Monitor 是插件进程的唯一所有者，不使用 Docker socket。
- 浏览器只访问 Frontend/Backend；插件管理、Metrics/Logs/Events 查询继续经 Backend 的数据库权威 `admin` 授权。Phase 14 不提前引入 Phase 15 的 `super_admin`。
- 实施和应用验收使用维护中的 Linux/Bash Compose 路径；Kubernetes 和原生 Windows/macOS 不是前置条件。

## 4. 权威批次、版本与分支分配

Phase 14 的 patch `0` 为阶段基线，不创建空批次。6 个执行批次按下表顺序实施：

| 批次 | 目标版本 | 开发分支 | 可独立验收的闭环 |
| --- | --- | --- | --- |
| Phase-14-01 | `1.11.1` | `develop/1.11.1` | 通用单实例插件契约、配置/Secret、多来源 metrics 链路与 Redis v1 无损迁移 |
| Phase-14-02 | `1.11.2` | `develop/1.11.2` | MySQL 与 RabbitMQ 官方插件的真实目标查询闭环 |
| Phase-14-03 | `1.11.3` | `develop/1.11.3` | Kafka 与 Elasticsearch 官方插件的真实集群/节点查询闭环 |
| Phase-14-04 | `1.11.4` | `develop/1.11.4` | VictoriaMetrics 官方插件、六插件并行运行与故障隔离闭环 |
| Phase-14-05 | `1.11.5` | `develop/1.11.5` | 六个 GoPulse 自研组件低基数运行指标的采集、存储和 Backend 查询闭环 |
| Phase-14-06 | `1.11.6` | `develop/1.11.6` | 完整 Compose 产品、迁移/重启/故障恢复、管理员浏览器与 Phase 13 回归的阶段收口 |

同一批次的全部实现提交共享该批目标版本。批次完成时同步根 `VERSION`、Frontend package 元数据与既有受管镜像/环境版本元数据；本次规划文档编写不修改 `VERSION`。

已创建或已推送的分支不得静默改名。若在实施前调整批次数量或顺序，必须先更新本表，再重算所有尚未创建的分支。

## 5. 跨批次顺序、依赖与拆分理由

```text
14-01 通用契约 + Redis 迁移 + 多来源链路
   ↓
14-02 MySQL + RabbitMQ（SQL / Management HTTP 与 Secret 差异）
   ↓
14-03 Kafka + Elasticsearch（集群协议 / REST 拓扑差异）
   ↓
14-04 VictoriaMetrics（自观测存储回路）+ 六插件隔离
   ↓
14-05 六自研组件指标 + 固定内部采集目标
   ↓
14-06 完整 Compose 与阶段收口
```

本阶段超过三个批次是因为存在五类必须独立收敛的风险，而不是按代码目录机械拆分：

1. Redis 现有持久状态、Manifest v1、公共 DTO 和 metrics v1 链路必须在放开新类型前一次安全迁移。
2. MySQL/RabbitMQ 分别使用 SQL 状态与 Management HTTP，账号权限、超时和聚合指标来源需要真实产品目标证据。
3. Kafka/Elasticsearch 具有集群拓扑和部分可用状态，需要与前一批的单端点协议故障分开验收。
4. VictoriaMetrics 既是采集目标又是本链路的存储，它宕机时不可能立即把自己的 `up=0` 写入自己；必须用实时安全状态/事件与恢复后查询共同证明。
5. 自研组件指标会修改六个长运行进程的公共运行边界，需要独立管理指标语义、标签基数、认证和失败对业务就绪的影响。

每个后续批次从前序已合入的最新 `upstream/main` 开始。不允许在一个插件批次中预做后续所有插件，也不把 Phase-14-06 变成独立架构 Review、依赖审计或覆盖率活动。

## 6. 目标架构与信任边界

### 6.1 运行拓扑

```text
Browser
  → Frontend
  → Backend（session + DB-authoritative admin authorization + strict DTO）
  → Monitor internal API
       ├─ Plugin Manager（6 个固定 plugin ID，各自 0..1 实例）
       │    └─ 6 个独立子进程（各自回环端口与一个目标）
       └─ Metrics Collector
            ├─ 采集运行中插件
            └─ 采集 6 个固定自研组件端点
                 → Router → Kafka → Marshaller → VictoriaMetrics
                                                     ↑
                                  Backend fixed catalog query
```

- Backend 只负责管理员授权、请求大小/形状、严格上游验证与安全错误映射；不解包、不保存 Secret、不启动进程。
- Monitor 是插件包、配置/Secret、desired state、运行进程和采集任务的唯一所有者。每个 plugin ID 使用独立操作锁，一个类型更新不阻塞其他类型采集。
- Exporter 是被动拉取、无历史状态的独立进程；`/health` 只表达进程存活，`/metrics` 表达当前真实目标快照。
- Exporter 只绑定 Monitor 运行边界内的回环地址，端口由六类固定目录分配，不对宿主或浏览器发布。
- 自研组件 metrics 端点使用内部 Bearer 身份且不发布宿主端口；在 host mode 仅允许回环，在 container mode 仅允许固定 Compose service DNS。
- 任一指标目标、Router、Kafka、Marshaller 或 VictoriaMetrics 故障只降级可观测链路，不加入 Backend/Worker/Indexer 业务就绪必要条件。

### 6.2 固定身份与单实例不变式

| source | plugin ID | target ID | Exporter 回环端口 | 最大实例/目标 |
| --- | --- | --- | --- | --- |
| `redis` | `redis-exporter` | `redis-exporter-local` | `9121` | `1 / 1` |
| `mysql` | `mysql-exporter` | `mysql-exporter-local` | `9122` | `1 / 1` |
| `rabbitmq` | `rabbitmq-exporter` | `rabbitmq-exporter-local` | `9123` | `1 / 1` |
| `kafka` | `kafka-exporter` | `kafka-exporter-local` | `9124` | `1 / 1` |
| `elasticsearch` | `elasticsearch-exporter` | `elasticsearch-exporter-local` | `9125` | `1 / 1` |
| `victoriametrics` | `victoriametrics-exporter` | `victoriametrics-exporter-local` | `9126` | `1 / 1` |

- `plugin_id` 是插件类型与 Registry key，不新增 `instance_id`、target 列表或用户自定义 ID。
- install 对已有同 ID 记录返回冲突；配置接口只替换该 ID 的唯一配置修订，不接受数组。
- Monitor 启动时若发现同 ID 多记录、多 current、多运行记录或非预期目标，该插件进入安全失败而不任选一个继续运行。
- `target_id` 只是服务端固定低基数来源身份，不包含主机、端口、用户名、数据库、queue/topic/index 名或 Secret 引用。
- 六个端口只绑定 Monitor 容器或 host mode 的回环地址，不进入用户配置、Manifest、公共 DTO 或宿主发布面；端口占用只使对应 ID 安全失败。

## 7. Manifest、配置、Secret 与持久状态契约

### 7.1 受限 Manifest v2

Manifest v2 继续使用严格 JSON，保留 v1 的基本字段，并固定增加兼容与 Schema 引用。字段集是：

`schema_version`、`id`、`name`、`version`、`kind`、`source`、`os`、`arch`、`entrypoint`、`entrypoint_sha256`、`health_path`、`metrics_path`、`runtime_contract_version`、`metrics_contract_version`、`config_schema_path` 和 `config_schema_sha256`。

其中 `schema_version=2`、`runtime_contract_version=1`、`metrics_contract_version=2`，`config_schema_path=config.schema.json`；所有字段必须恰好出现一次，未知、缺失或重复字段均拒绝。

| 语义 | 约束 |
| --- | --- |
| schema / runtime contract | 只接受上述 `2/1/2` 组合；不支持的 Monitor API 或 metrics contract 在解包后、启动前拒绝 |
| id / source / kind | 必须匹配六类服务端官方目录；`kind=metrics-exporter` |
| version | 三段 SemVer；update 必须严格高于当前版本，回滚只回到事务保留的前一已验证版本 |
| os / arch | Phase 14 固定 Linux 与 Monitor 当前架构；跨平台/多架构实际矩阵属于 Phase 16 |
| entrypoint / digest | 目录固定的相对入口与 64 位小写 SHA-256；不允许参数、绝对路径、hook 或额外 executable |
| health / metrics | 固定 `/health` 与 `/metrics`，无 query/userinfo/fragment |
| config schema | 指向包内唯一受限 Schema 普通文件并校验 digest；Monitor 还必须与内建官方目录逐字段核对 |

- 安装包仍只接受受限 `tar.gz`，继承现有压缩/解压大小、entry 数、路径、文件类型、重复、链接、权限和 staging 边界；为 Schema 文件扩展时不放开任意文件。
- 包内不得带配置实例、凭据、token、连接串、启动脚本或远程下载地址。
- 官方身份的信任根是随 Monitor 镜像发布的编译期 release catalog。catalog 对每个 `(plugin_id, version, linux, arch)` 固定完整 archive SHA-256、entrypoint SHA-256 和 config Schema SHA-256；包的 Manifest 自述 digest 只能做内部完整性校验，不能建立官方身份。
- install、bootstrap 和 update 只执行镜像内固定 packages 目录中、与 release catalog 三个 digest 完全一致的包。自洽但未登记的 archive、管理员上传的任意二进制和持久卷中凭空出现的 release 均在解包或执行前拒绝；Phase 14 不引入签名或远程仓库作为替代信任根。

### 7.2 配置 Schema 与连接测试

- `config.schema.json` 使用 GoPulse 受限 Schema v1，顶层字段固定为 `schema_version=1`、`plugin_id` 和 `fields`。每个 field 只允许 `name`、`type=hostname|port|integer|string|duration|secret`、`required`、`secret`、可选 `minimum`、`maximum`、`enum`；不支持 regex、嵌套 object/array、任意 UI 文本或执行指令。
- 六类 Schema 的字段顺序和约束固定如下；`R` 表示必填、`O` 表示可省略，字段集合之外的值一律拒绝：

| plugin ID | 精确字段与受限 Schema |
| --- | --- |
| `redis-exporter` | `host:hostname(R)`、`port:port(R)`、`database:integer(R,0..15)`、`connect_timeout:duration(R,100ms..10s)`、`scrape_timeout:duration(R,100ms..10s)`、`password:secret(R,1..256 bytes)` |
| `mysql-exporter` | `host:hostname(R)`、`port:port(R)`、`database:string(R,1..64 chars)`、`username:string(R,1..64 chars)`、`connect_timeout:duration(R,100ms..10s)`、`scrape_timeout:duration(R,100ms..10s)`、`password:secret(R,1..256 bytes)` |
| `rabbitmq-exporter` | `host:hostname(R)`、`management_port:port(R)`、`vhost:string(R,enum=/)`、`username:string(R,1..64 chars)`、`connect_timeout:duration(R,100ms..10s)`、`scrape_timeout:duration(R,100ms..10s)`、`password:secret(R,1..256 bytes)` |
| `kafka-exporter` | `host:hostname(R)`、`port:port(R)`、`topic:string(R,enum=gopulse-observability-v1)`、`consumer_group:string(R,enum=gopulse-marshaller-metrics-v1)`、`connect_timeout:duration(R,100ms..10s)`、`scrape_timeout:duration(R,100ms..10s)` |
| `elasticsearch-exporter` | `host:hostname(R)`、`port:port(R)`、`username:string(O,1..64 chars)`、`connect_timeout:duration(R,100ms..10s)`、`scrape_timeout:duration(R,100ms..10s)`、`password:secret(O,1..256 bytes)` |
| `victoriametrics-exporter` | `host:hostname(R)`、`port:port(R)`、`username:string(R,1..64 chars)`、`connect_timeout:duration(R,100ms..10s)`、`scrape_timeout:duration(R,100ms..10s)`、`password:secret(R,1..256 bytes)` |

- `hostname` 和端口还必须匹配运行模式目录：host mode 只接受 `127.0.0.1` 或 `::1`，端口依次固定为 Redis `6379`、MySQL `3306`、RabbitMQ Management `15672`、Kafka `9092`、Elasticsearch `9200`、VictoriaMetrics `8428`；container mode 的唯一 `(host,port)` 依次为 `redis:6379`、`mysql:3306`、`rabbitmq:15672`、`kafka:19092`、`elasticsearch:9200`、`victoriametrics:8428`。
- `password` 始终是 `type=secret,secret=true`，其他字段固定 `secret=false`。Elasticsearch 的 `username/password` 必须同时省略或同时提供；日常 Compose 因锁定目标关闭认证而同时省略。所有 `connect_timeout <= scrape_timeout`，字符串另经服务端固定字符集和 Unicode 长度校验，不因受限 Schema 不支持 regex 而放宽。
- Kafka 本阶段使用 Compose 内部 PLAINTEXT 目标，不增加未使用的 SASL/TLS 字段；其 topic/group 和 RabbitMQ vhost 都是上述单值 enum，不接受任意业务对象名。
- 每种插件只接受结构化字段：主机、端口、必要的数据库/虚拟主机或固定 GoPulse topic/group 选择、用户名、超时与独立 Secret；不接受原始 DSN、完整 URL、额外 query 参数或任意环境变量。
- Backend 执行类型/大小/字符校验，Monitor 重新根据官方目录校验；Frontend Schema form 只改善 UX，不是授权或安全边界。
- host mode 只允许规划的回环原点；container mode 只允许该插件对应的固定 Compose service DNS 和受限端口。拒绝固定 IP、userinfo、`host.docker.internal`、控制字符和跨协议跳转。
- 连接测试使用 release catalog 中该 ID 当前官方 entrypoint 的固定 one-shot check mode；该模式由 Monitor 选择且与 `/metrics` 共用只读协议 adapter，不接受 Manifest hook、请求控制的命令/参数或任意脚本。进程有界运行，不持久化候选 Secret、不创建 Registry/release/process record，也不返回上游原始错误。
- install/config update 必须重新校验，不把早先的连接测试当作永久授权。首次配置必须提供本表所有必填 Secret；配置替换时 Secret key 省略表示保留，非空字符串表示替换，显式 `null`、空串或额外 key 均拒绝。

### 7.3 Secret 与持久布局

- Registry 只保存 Manifest、当前版本、desired state、非敏感配置修订和时间；不保存密码、token、完整连接串或进程参数。
- 每个 plugin ID 有独立配置与 Secret 文件，使用原子替换、安全父目录验证和最小文件权限；Secret 不与非敏感 Registry 串行化到同一公共 DTO。
- 启动子进程时由 Monitor 按固定白名单注入分离字段，不把 Secret 放入 command line、工作目录、事件或结构化日志。
- 公共状态只返回 `configured`、配置修订、固定 target 摘要、Secret 是否已设置和安全错误；绝不回显原始 Secret、内部路径、PID、命令或完整主机连接信息。
- 配置变更先写候选修订、验证和试启动，成功后再原子激活；失败恢复旧制品、旧配置/Secret 修订与原 desired state。

### 7.4 Redis v1 状态迁移

- Phase-14-01 必须识别现有单 Redis Registry/layout，先验证根边界、current symlink、制品 digest、进程归属和 desired state，再生成新通用记录。
- 现有 Redis 目标字段从受信 Monitor 配置导入唯一默认配置；Secret 进入独立存储，Registry/API 不读回。
- desired state、installed/updated time 与可安全继承的采集时间尽量保留；新进程健康且真实 Redis 采集成功后才提交迁移。
- 任一迁移步骤失败时保留旧持久状态或使 Redis 进入可解释的安全失败，不删卷、不静默重置为 running，不影响其他后续插件记录。
- 覆盖 stopped/running、旧包较新/较旧、中断迁移重试和损坏状态拒绝；不把人工清 volume 写成升级步骤。

## 8. 生命周期、API 与管理界面契约

### 8.1 生命周期

- list/get/install/start/stop/update 从 Redis 常量改为六 ID 固定目录，全部操作只作用于 path 中的一种插件。
- connection-test 不改变持久状态；install 只在制品、Schema、配置、Secret、健康和首次真实采集均成功后提交 running。
- start/stop 幂等；stop 先取消对应采集，再有界停止已证明归属的进程，绝不根据未验证 PID 发信号。
- update 先验证新包与当前配置 Schema 兼容，保留前一版本和 desired state；新版本失败时原子回滚，回滚也失败则只将该 ID 标记为安全失败。
- Monitor 重启按 plugin ID 稳定顺序调和，恢复各自 desired state；一个包/配置损坏不阻止其他 ID 恢复。
- 进程意外退出和目标不可达是不同状态：前者表达运行时失败，后者保持进程 running 并通过安全采集错误/`up=0` 表达，目标恢复无需重启 Exporter。

### 8.2 Backend/Monitor API

保留 `/api/v1/exporter-plugins` 管理域，将单 Redis URL 改为显式 `:pluginId`，固定对外路由为：

| method / path | 请求 | 成功结果 |
| --- | --- | --- |
| `GET /api/v1/exporter-plugins/catalog` | 无 body/query | 六类固定 ID、显示名、source、available 和受限字段描述；不含 Secret 值或内部路径 |
| `GET /api/v1/exporter-plugins` | 无 body/query | 最多六个已安装状态，按 catalog ID 稳定顺序 |
| `GET /api/v1/exporter-plugins/:pluginId` | 无 body/query | 单个安全状态 |
| `POST /api/v1/exporter-plugins/:pluginId/connection-test` | 严格 JSON `config` + `secrets` | 固定 `reachable=true`；失败使用稳定公共错误，不持久化 |
| `POST /api/v1/exporter-plugins/:pluginId/install` | 严格 JSON `config` + `secrets`，使用镜像内嵌该 ID 官方包 | 安装并启动后的安全状态 |
| `PUT /api/v1/exporter-plugins/:pluginId/configuration` | 严格 JSON `config` + `secrets`，Secret 可以显式 preserve 语义省略 | 试启动并原子替换后的状态 |
| `POST /api/v1/exporter-plugins/:pluginId/start|stop` | 空 body | 幂等启停后的状态 |
| `POST /api/v1/exporter-plugins/:pluginId/update` | 恰好一个 `package` multipart 字段 | 兼容性验证与试启动后的状态，失败回滚 |

现有 `POST /api/v1/exporter-plugins/install` 在 Phase 14 保留为已配置 Redis 的兼容 alias，仍只接受受信 Redis 包并经新通用路径执行；新 Frontend 只使用显式 plugin ID 路由。Monitor 内部 API 使用相同语义和 `/internal/v1/exporter-plugins` 前缀，仅认证方式不同。

全部路由必须先经认证和数据库当前 `admin` 授权。Backend 和 Monitor 双层拒绝未知 ID、多目标数组、未知字段、超限请求和不安全上游响应。已知错误映射为稳定公共 code/message，任何协议原始错误、主机、账号、路径或 Secret 都不进入响应。

### 8.3 Frontend 最小扩展

- 当前 Redis 专用页改为六类固定卡片/详情，清楚显示 installed/configured/desired/observed、版本、最近采集/成功和安全错误。
- 配置表单只根据 Backend 固定 catalog 生成允许字段；Secret 输入不回填、不写 local storage、不进 URL，离开或成功后清理。
- 连接测试、安装、启停、配置替换和更新均有独立提交中/成功/失败反馈，防止双击重复操作；停止、替换和更新保留明确确认。
- 从插件详情可进入指标查询并选中该 source 的服务端目录项。页面不接受用户输入 PromQL。
- 继续使用 Phase 11 管理壳层与 Phase 13 `AdminLayout` 隔离；不建新 Frontend 应用，不改造普通用户导航。

## 9. Metrics 消息、传输、存储与查询契约

### 9.1 多来源 envelope

- Metrics Envelope v2 顶层字段固定为 `schema_version=2`、`message_id`、`type=metrics`、`source`、`timestamp` 和 `payload`；`payload` 字段固定为 `producer_kind`、`producer_id`、`producer_version`、`target_id`、`scrape_status` 和 `samples`。每个 sample 继续只包含 `name,kind,labels,value`。
- `producer_kind` 只允许 `exporter_plugin|component`；前者 producer ID 是六个固定 plugin ID，后者是 `backend|business-worker|search-indexer|monitor|router|marshaller`。组件 target ID 固定为 `<producer_id>-local`；`producer_version` 为三段产品/插件 SemVer。
- `source` 必须与 producer catalog 唯一对应；插件使用六个基础设施 source，组件使用六个 component ID 作 source。`scrape_status` 只允许 `success|target_unavailable`。
- v2 不继续把自研组件伪装成 plugin ID，也不允许 sample 自带 `source`、`target_id`、`producer_kind` 或 `producer_id` 保留标签。
- Router 在过渡期同时接受历史 Redis metrics v1 和新多来源 metrics 契约，仍只写固定 `gopulse-observability-v1` Topic，不解析 samples、不重组 body、不允许请求选 Topic。
- Marshaller 严格校验 source/producer/target/family/kind/label/value/count 组合，按服务端版本化目录转换；未知或超界样本作为确定性非法记录跳过，不污染存储且不阻塞后续合法记录。
- 存储统一添加受验证的 `source`、`target_id` 与 producer 类型/ID 标签；不从插件样本接受这些保留标签，防止伪造或覆盖。
- 最大 body、family、sample、label 数量、label 值和时间范围保持有界。传输继续是 at-least-once，幂等与 Kafka offset 语义不因多 source 改变。

### 9.2 Backend 指标目录

- `GET /api/v1/observability/metrics` 继续只允许服务端目录中的 metric/range，不提供任意 PromQL、label matcher 或上游 URL 透传。
- 每个目录项固定 metric name、kind、unit、source、target ID、producer 身份与允许的公共 labels。Backend 只构造该精确查询并严格校验 VictoriaMetrics matrix 响应。
- 目录可按 `source`/`producer_kind` 分组供 Frontend 选择，但不返回内部目标主机、凭据、PromQL 表达式或未管理 labels。
- 历史 Redis v1 series 保留原 `source=redis,target_id=redis-exporter-local`；新契约继续沿用这两个身份，使 Redis 查询时间线不因迁移断开。

## 10. 六类官方插件权威指标目录

每个 Exporter 成功响应必须包含它的完整固定 family 集；目标级连接/认证/超时失败只返回 `503` 与对应 `gopulse_<source>_up 0`，不返回部分或上次成功值。上游字段映射可在实施时根据锁定版本调整，但对外 family 名、kind 与 label 形状以下表为权威合同：

| source | 固定 families（未特注均无 label） | kind / 标签 |
| --- | --- | --- |
| Redis | `gopulse_redis_up`、`uptime_seconds`、`connected_clients`、`used_memory_bytes`、`commands_processed_total`、`keyspace_hits_total`、`keyspace_misses_total`、`cpu_seconds_total`、`db_keys`、`db_expiring_keys`，后九项均以 `gopulse_redis_` 为前缀 | up/uptime/clients/memory/db 为 gauge，commands/hit/miss/CPU 为 counter；CPU `mode=user|system`，DB 两项仅单一数字 `db` |
| MySQL | `gopulse_mysql_up`、`uptime_seconds`、`connections`、`max_connections`、`threads_running`、`queries_total`、`slow_queries_total`、`transactions_total`、`buffer_pool_data_bytes`、`buffer_pool_dirty_bytes` | up/uptime/connections/threads/buffer 为 gauge，queries/slow/transactions 为 counter；transactions 固定 `result=commit|rollback` |
| RabbitMQ | `gopulse_rabbitmq_up`、`connections`、`channels`、`queues`、`consumers`、`messages`、`published_total`、`delivered_total`、`acked_total` | up/connections/channels/queues/consumers/messages 为 gauge，其他为 counter；messages 固定 `state=ready|unacked` |
| Kafka | `gopulse_kafka_up`、`brokers`、`controller_available`、`partitions`、`under_replicated_partitions`、`offline_partitions`、`consumer_group_lag` | 全部 gauge 且无 label；topic/group 是配置中的服务端固定值，不再写入 label |
| Elasticsearch | `gopulse_elasticsearch_up`、`cluster_health_status`、`nodes`、`data_nodes`、`active_primary_shards`、`active_shards`、`relocating_shards`、`initializing_shards`、`unassigned_shards`、`pending_tasks`、`documents`、`store_size_bytes` | 全部 gauge；health 固定三个 one-hot sample `status=green|yellow|red`，不带 node/index/shard 名 |
| VictoriaMetrics | `gopulse_victoriametrics_up`、`rows_inserted_total`、`query_requests_total`、`active_timeseries`、`storage_rows`、`storage_size_bytes`、`free_disk_space_bytes`、`active_merges`、`retention_deletions_total` | up/active/storage/disk/merge 为 gauge，rows/query/retention 为 counter；无 label，只映射锁定上游 families |

表中省略前缀的 family 均继承本行 source 的 `gopulse_<source>_` 前缀。Phase 14 只使用 counter/gauge，不增加 histogram。若锁定上游无法稳定提供某项，必须在对应批次开工前先修订总方案与未开工 split plan，不得在代码中静默缺省或替名。

## 11. 自研组件指标目录与基数预算

### 11.1 权威 family 与标签契约

| 组件 | 固定 families | kind / 允许 labels |
| --- | --- | --- |
| Backend | `gopulse_backend_http_requests_total`、`http_request_duration_seconds_total`、`outbox_pending`、`outbox_oldest_age_seconds`、`outbox_last_publish_success_timestamp_seconds`、`dependency_up` | 前两项 counter，outbox/dependency 为 gauge；HTTP 仅 `method,route,status_class`，dependency 仅 `mysql|redis|rabbitmq|elasticsearch` |
| Business Worker | `gopulse_business_worker_messages_total`、`message_processing_duration_seconds_total`、`messages_in_flight`、`prefetch_limit`、`last_success_timestamp_seconds`、`dependency_up` | 前两项 counter，其他 gauge；前两项仅 `event_type,result`，dependency 仅 `mysql|rabbitmq` |
| Search Indexer | `gopulse_search_indexer_messages_total`、`message_processing_duration_seconds_total`、`messages_in_flight`、`retrying`、`last_success_timestamp_seconds`、`dependency_up` | 前两项 counter，其他 gauge；前两项仅 `operation=create|update|delete,result`，dependency 仅 `mysql|rabbitmq|elasticsearch` |
| Monitor | `gopulse_monitor_scrapes_total`、`scrape_duration_seconds_total`、`last_scrape_success_timestamp_seconds`、`event_queue_length`、`event_queue_dropped_total`、`plugins_running`、`dependency_up` | scrape 两项 counter，dropped counter，其他 gauge；scrape 项仅 `producer_kind,target_id,result`，dependency 仅 `router` |
| Router | `gopulse_router_messages_total`、`produce_duration_seconds_total`、`buffered_records`、`buffered_bytes`、`last_kafka_ack_timestamp_seconds`、`dependency_up` | 前两项 counter，其他 gauge；前两项仅 `type,source,result`，dependency 仅 `kafka` |
| Marshaller | `gopulse_marshaller_records_total`、`record_processing_duration_seconds_total`、`records_in_flight`、`retrying`、`last_storage_success_timestamp_seconds`、`last_commit_success_timestamp_seconds`、`dependency_up` | 前两项 counter，其他 gauge；前两项仅 `type,source,stage,result`，last storage 仅 `storage=victoriametrics|elasticsearch`，dependency 仅 `kafka|victoriametrics|elasticsearch` |

表中同一行第一个 family 之后省略前缀的名称均继承该组件前缀。`event_type`、`type`、`source`、`stage`、`result` 的值必须取自当前严格消息契约的服务端 allowlist；不得使用未知原值作 fallback label。Backend `route` 取 Gin 匹配后的注册模板，无匹配路由统一使用固定 `_unmatched`，最大不超过实际注册路由数加一。

### 11.2 基数和信息安全

- 不得使用 user/post/comment/event/message/request ID、username、标题/正文、原始 URL/path/query、SQL、cache key、queue/topic/index 任意名称、错误原文或堆栈作为 label。
- route 只能是有界的服务端模板，status 使用 `2xx|4xx|5xx` 等有界类别；dependency、operation、stage、result 都使用代码固定枚举。
- 每个组件的 family 数、每 family label key/value 集和最大 sample 数写入 Monitor/Marshaller 双层目录，任意新 label 默认拒绝。
- 延迟首选 counter total + operation count 或有界 last-duration gauge；不为“更细”默认引入大量 histogram bucket。
- 组件 metrics 端点不包含日志、配置 dump、环境变量、build path、goroutine 栈或无界 runtime 指标全量暴露。

## 12. 故障隔离、重启恢复与可解释性

- 一个插件目标不可达时，该 Exporter 保持存活、该状态显示安全 target error，其他插件与组件采集继续。目标恢复后同进程恢复 `up=1` 和完整快照。
- 一个插件进程崩溃时，Monitor 只更新该 ID 的 observed state/事件，不停止其他子进程、不删历史数据、不让 Monitor 整体不就绪。
- 某 ID 更新/配置失败仅回滚该 ID；并发操作其他 ID 不共享全局锁，但每 ID 自身仍串行化。
- Router/Kafka/Marshaller 短暂故障时不新建无界本地指标队列；各采集按已有丢弃/重试边界继续，恢复后从新快照继续。
- VictoriaMetrics 宕机时，Backend metrics API 按现有契约局部返回 unavailable；Monitor 插件状态与 Events 在可用链路中表达目标故障。恢复后新 `up=1` 与运行指标可查，不伪造宕机期已写入自身的 `up=0` 证据。
- Monitor 和 Compose 重启时保留六 ID 的制品、配置修订、Secret、desired state 与历史 VictoriaMetrics 数据；恢复不依赖手工上传或清 volume。
- 日志、Events、API 和 Frontend 只使用安全 reason code 与固定 message；故意使用包含 Secret/连接串的失败输入时，所有对外与可观测产物仍必须脱敏。

## 13. Compose 制品与产品生命周期

- Monitor 镜像使用固定包目录嵌入六个可复现官方包，不从网络下载。每个包的入口 digest 与实际嵌入二进制匹配。
- 日常完整 Compose 在空插件卷上按官方 catalog 独立调和六个包和固定内部目标配置；一个失败不回滚已成功的其他 ID。
- Compose 向 Monitor 提供六类目标所需最小账号/密码和固定 service DNS。尽量使用最小读权限账号；不复用 root 账号，不把 Secret 写入 image layer、label 或启动日志。
- 六个 Exporter 仍由 Monitor 容器内子进程运行，不新增六个默认 Compose service；现有 Redis standalone profile 只作兼容/诊断，不与 Monitor 同时成为默认所有者。
- 如因只读根文件系等容器边界需要 runtime 目录，只能使用 Monitor 专属已归属 volume/tmpfs，不挂载 Docker socket 或宿主任意目录。
- `scripts/dev.sh`、`verify.sh`、`down.sh` 和 Phase 14 Compose 验收继续管理一个强归属 project，必须保留已有项目/容器/网络/volume 不变和有界信号退出。

## 14. 各批次职责边界

### Phase-14-01：通用插件契约与 Redis 迁移闭环

- 交付六 ID 官方 catalog、Manifest v2、配置/Secret 存储、连接测试、按 ID 生命周期和公共 DTO/UI 通用化。
- 交付 metrics 新多来源契约、Router/Marshaller/Backend 兼容链路与 Redis v1 无损状态迁移。
- 必须以真实 Redis 完成管理员配置/连接测试/安装/查询，不提前实现其他五个 Exporter。

### Phase-14-02：MySQL 与 RabbitMQ 插件闭环

- 交付两个官方 Exporter、固定 Schema、最小读权限账号、确定性包与嵌入调和。
- 各自以真实 Compose 目标完成 success/unavailable/auth/recovery 和 Backend 查询，互不影响 Redis。
- 不扩展到任意 schema/table/queue/vhost 维度或数据库管理能力。

### Phase-14-03：Kafka 与 Elasticsearch 插件闭环

- 交付两个官方 Exporter、集群协议/REST 配置与聚合指标目录。
- 以真实 Kafka/Elasticsearch 验证部分状态、完全不可达、恢复和 Backend 查询，不绑定动态 broker/node 标签。
- 不修改 Kafka Topic 分工、Elasticsearch 业务/日志/事件 index 归属或搜索重建契约。

### Phase-14-04：VictoriaMetrics 与六插件隔离闭环

- 交付 VictoriaMetrics 官方 Exporter、锁定上游版本的白名单转换和 Backend 查询。
- 证明六个插件同时运行、独立操作/进程/采集状态、一类故障不影响其他五类与社交业务。
- 以实时安全状态、事件和恢复后新数据证明 VictoriaMetrics 自观测故障，不伪造停机期存储证据。

### Phase-14-05：自研组件基础运行指标闭环

- 为六个自研长运行组件增加受保护端点、有界指标目录和正确关闭；Worker/Indexer 的新 HTTP 生命周期不影响消费与退出。
- Monitor 将六个组件作为固定非插件目标采集，通过同一链路交付 Backend 查询。
- 用运行时受控词汇证明标签基数，不添加告警、通用 runtime dump 或无界 Prometheus 代理。

### Phase-14-06：Compose 验收与阶段收口

- 在完整 Compose 中验证旧 Redis 卷迁移、空卷六插件调和、重启恢复、管理员浏览器、六插件+六组件查询和代表性故障。
- 回归 Phase 13 用户主路、管理授权、Metrics/Logs/Events、版本/镜像/网络/端口/进程归属和清理。
- 只修复本阶段验收暴露的阻断问题，不开展默认独立 Review 或 Phase 15 功能。

## 15. 阶段级验收标准

### 15.1 六插件端到端

在强归属的真实 Compose 环境中，以一个普通用户和一个当前管理员完成：

1. 普通用户访问 catalog、连接测试、插件操作和 metrics 查询均被 Backend 拒绝；浏览器不能直连 Monitor 或 Exporter。
2. 管理员对六种固定类型查看 Schema/状态，对代表性未安装类型输入配置/Secret、完成连接测试、安装并启动，Secret 不被回显。
3. Redis、MySQL、RabbitMQ、Kafka、Elasticsearch 和 VictoriaMetrics 各自对应一个真实 Compose 目标，各自的 `up=1` 及至少一个实际运行数值经 Monitor、Router、Kafka、Marshaller、VictoriaMetrics 后由 Backend 查询返回。
4. 六种 source/target 互不混淆，Backend 不能用一个 metric 名查到错误 source/target；非目录 metric/label/range 被拒绝。
5. 对同 ID 重复 install 被拒绝，API 中无第二实例/第二目标创建语义，伪造 Registry 多记录在恢复时安全失败。
6. stop 只停止所选 ID；start 恢复同一进程契约和采集；更新失败回滚原版本/配置/desired state，其他五种不停止。
7. 任一目标不可达时该插件安全降级，其他插件、历史 metrics 和社交业务可用；目标恢复后无人工重装即重新查到完整数据。

### 15.2 迁移、Secret 与完整性

- 从 Phase 13 真实 Redis Manifest v1/Registry/volume 升级后，安装版本、desired state 和历史查询保留，新契约下 Redis 可继续采集；迁移重试不重复记录。
- 空 volume 可调和六个嵌入官方包；同 volume 替换 Monitor 容器后按六个 desired state 恢复，stopped 的类型不被静默启动。
- 篡改入口、Schema digest、未知 ID/source、不兼容契约、非安全 archive 和伪造 process record 均在执行/发信号前被拒绝。
- 使用故意包含特征串的密码、token、用户名和连接错误后，API、Frontend DOM、结构化日志、Events、metrics labels、Registry 与诊断输出都查不到该特征串。

### 15.3 自研组件指标

- Backend、Business Worker、Search Indexer、Monitor、Router 和 Marshaller 各有至少一条代表性处理结果、一条延迟信号、适用的队列/消费进度、最近成功和依赖降级可在 Backend 固定目录查询。
- 未认证访问内部 metrics 端点被拒绝；从浏览器/宿主不可直接访问 Worker、Indexer、Monitor、Router 或 Marshaller 的端点。
- 以包含多个用户、帖子、请求和消息的真实业务路径生成指标后，series 只按固定 route/operation/result/dependency 增长，不出现业务 ID 级 label 或内容。
- 关闭和替换 Worker/Indexer 时，新内部 HTTP 服务与现有消费者在时限内共同退出，不留端口或进程。

### 15.4 跨批集成与既有回归

- 普通用户完成注册/登录、发帖、评论、点赞、关注/Following、收藏、编辑、搜索、通知和删除代表性主路，插件/可观测故障不破坏 MySQL 事实与权限。
- 管理员 Metrics/Logs/Events 和六插件页可用，普通用户直接构造路由/API 仍被拒绝；UserAppShell 与 AdminLayout 没有互相污染。
- 现有 logs/events Envelope、Kafka Topic、Marshaller consumer group、VictoriaMetrics 持久化、Elasticsearch 业务搜索与 Redis 缓存不因 metrics 多 source 改造而回归。
- Compose image/version/network/port/read-only filesystem/numeric user/signal/volume 和强归属清理契约继续成立。

### 15.5 阶段里程碑条件

Phase 14 只在以上验收全部通过、6 份 split plan 均有同名真实实施记录、根完成版本为 `1.11.6`、全部执行批次已按顺序合入主线且无阻断问题时完成。该结果推进 Milestone 4，但 Milestone 4 仍要到 Phase 17 完整工程验收后才收口；Phase 14 不得提前宣称 Milestone 4 完成。

## 16. 验证策略与固定阶段门禁

- 实施中先运行直接受影响 Go package 与 Frontend Vitest；每个 split plan 的固定完成门禁只对该批最终 diff 运行一次。
- 每个插件批次必须有至少一条真实目标成功、一条代表性失败/恢复和从 Backend 固定 catalog 查询的证据；只测 Exporter handler 或使用 fixture 不足够。
- Phase-14-01 建立强归属的聚焦插件链路验收入口，后续批次按 source 扩展；Phase-14-05 建立自研组件聚焦验收；Phase-14-06 将它们纳入 `scripts/verify-compose.sh --phase14` 或等价固定入口。
- 新增测试只证明新/修改验收标准、观测到的缺陷或修改的安全/持久化/公共契约；一个状态转换优先一条成功和一条代表性失败。
- 已成功检查在代码、配置、依赖和环境没有相关变化时不重复；Phase-14-06 引用前五批实施记录中仍有效的 package 证据，只运行最终跨批 Compose 门禁。
- 只在观测到共享传输、Secret 边界、持久 Registry 或社交业务回归的具体风险时扩大检查，并在实施记录先写明理由。
- 每批固定包含版本/分支治理检查和 `git diff --check`。任何因环境不可用未运行或失败的命令必须如实记录，不得将批次或阶段标为完成。

## 17. 实施记录、版本与提交要求

每个批次完成前：

- 创建或更新与计划同名的 `dev/logs/Phase-14/Phase-14-XX-*.md`，记录实际制品/Schema/指标目录、实际文件、实际命令与结果、故障注入、偏差、限制和后续项。
- 将根 `VERSION`、Frontend 版本和既有受管版本元数据更新为分配表目标版本。
- 只提交当前批次文件，使用清晰的英文 Conventional Commit，并通过对应 `develop/x.x.x` 分支检查。
- 验收未通过时，不得将计划或记录写成已完成，不得仅因时间消耗提升版本。

## 18. 阶段完成、停止条件与 Phase 15 交接

Phase 14 的停止条件是第 15 节全部通过、无阻断失败、文档/代码/运行时与验收证据一致。达到条件后更新本文档实际状态与收口证据，提交并停止 Phase 14；不利用剩余时间扩展多实例、第三方插件、更多指标、告警或界面重构。

Phase 15 从合入后的 `1.11.6` 完成基线开始。交接时必须保留：

- 六个固定官方 plugin ID/source/target、单实例/单目标、安全配置/Secret 与可恢复生命周期。
- 六插件加六自研组件的服务端固定指标目录、低基数 labels 与多来源 metrics 链路。
- 依赖故障对社交业务的局部降级边界，以及 VictoriaMetrics 自观测停机期的可解释语义。
- Backend 数据库权威管理授权、安全公共 DTO 和浏览器不直连内部服务的边界；Phase 15 可迁移为 `super_admin` 但不得降级这些信任边界。
- Phase 13 完整业务与用户端契约、Phase 12/14 Compose 生命周期和强归属验收；Kubernetes 仍不是 Phase 15 前置条件。
