# Phase 14：插件体系与组件可观测闭环总实施方案

> 当前状态：Phase-14-01 至 Phase-14-05 已合入主线；Phase-14-06 本地实现和固定 Compose 验收已通过，开发分支完成版本为 `1.11.6`，待合入主线。Phase 14 主线阶段完成条件仍待第六批合入，不提前宣称里程碑完成。第六批实际证据见 `dev/logs/Phase-14/Phase-14-06-Compose集成验收与阶段收口.md`：完整平台/Phase 13 项目 `gopulse-accept-81d5cf3c48ce` 与跨批项目 `gopulse-p1401-652faf4f2dd5` 均通过且完成强归属清理。第五批已通过六自研组件真实指标链路、低基数与内部认证、端点/依赖/存储隔离和消费者替换回归，证据见同名开发记录。第四批已按修订后的删除行计数合同通过六插件并行、故障隔离、自观测恢复和浏览器验收，证据见同名开发记录。第三批已按 §10.2 的隔离临时 follower 合同通过真实验收并恢复单 broker 基线，结果与环境偏差见其同名开发记录。本文档于 2026-09-09 基于主远程 `upstream/main` 提交 `8caf8d4`、Phase 13 已完成产品版本 `1.10.6` 与 Compose 产品基线编写。Phase 14 使用 `1.11.x` 版本线，拆分为 6 个执行批次。本文档是 Phase 14 批次顺序、目标版本和开发分支的唯一权威来源；每批开工时仍须 fetch 主远程，从包含全部前置批次的最新 `upstream/main` 创建对应 `develop/x.x.x` 分支。

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
- 任一插件自身的目标连接失败、进程崩溃、配置失败或更新回滚，不中断其他插件；共享基础设施实际停机时按 §12 的真实依赖范围降级，插件改造不额外扩大业务影响。
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
| schema / runtime contract | 新安装只接受上述 `2/1/2` 组合（§7.4 的已登记 legacy-v1 状态恢复是显式兼容例外）；不支持的 Monitor API 或 metrics contract 在解包后、启动前拒绝 |
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

### 7.1.1 制品版本与信任决策表

- 每个 release catalog 条目必须声明 `current`、`retained` 或 `legacy-v1` 用途，并固定原始 archive/entrypoint/Schema digest；v1 的 Schema digest 明确为不适用，不以空值冒充 v2。旧卷中的已解包内容须与镜像内登记原包的 Manifest/入口逐项一致；原始 archive digest 从镜像包核对，不要求现场卷保存原 tar.gz，也不通过现场重新打包建立信任。同 ID/版本/平台只允许一组内容，禁止将 v1 改包为 v2 后沿用旧版本号。
- 每个镜像包含各已交付插件的一个 current v2 包，以及受支持升级/回滚所需的 retained 包；不要求保存所有历史版本。实际支持的精确版本、架构、构建提交、工具链和 digest 列表是 Phase-14-01 制品前置确认项，未记录前不得宣称支持旧卷升级。
- 上传接口只接受与该镜像已登记且已嵌入包逐字节一致的 archive，上传是选择受信版本，不是扩充 catalog。安全比较后从镜像可信目录取包执行，不执行上传临时路径。

| 输入/场景 | 唯一处理规则 |
| --- | --- |
| 空卷 install/bootstrap | 使用该 ID 的 current v2 包；没有 current 或 `available=false` 则安全拒绝 |
| 已安装且仍命中 catalog 的旧版本 | 恢复原版本和 desired state；bootstrap 不隐式升级，升级通过显式 update |
| 已安装版本高于镜像 current，但命中 retained/legacy-v1 | 允许恢复原版本，不降级；没有更高受信版本时 update 返回版本冲突 |
| 旧版本/内容未登记、digest 不同或平台不支持 | 保留旧卷并标记 `repair_required`，不执行、不自动收编、不降级；记录不支持的精确升级来源 |
| 已登记且严格更高版本的 update | 按 §7.5 事务试启动；禁止同版本换内容或手工降级 |
| 试启动失败的验收制品 | 只在隔离 acceptance 镜像的编译期 catalog 中登记确定性失败包，并同时嵌入旧包；与生产使用同一校验代码，无环境变量/请求开关绕过 trust check |

验收至少提供一个受信更高版本成功包与一个受信更高版本失败包；失败包的故障点必须在信任验证之后、事务提交之前。生产镜像不得包含失败包。前五批记录包版本/digest 与镜像用途，Phase-14-06 复用，不临时上传任意测试二进制。

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
- `password` 始终是 `type=secret,secret=true`，其他字段固定 `secret=false`。Elasticsearch 的 `username/password` 必须同时省略或同时提供；日常 Compose 因锁定目标关闭认证而同时省略。所有 `connect_timeout <= scrape_timeout`，非 Secret 字符串只允许 Unicode 字母/数字及 ASCII `._-`，另以 Schema 单值 enum 放行 RabbitMQ `/`；禁止空白、控制字符、URL 分隔符及其他标点。hostname、port、duration 使用各自专用解析，不应用此通用字符集。Secret 仅按有效 UTF-8、字节长度与无 NUL 校验，不裁剪或规范化。
- Kafka 本阶段使用 Compose 内部 PLAINTEXT 目标，不增加未使用的 SASL/TLS 字段；其 topic/group 和 RabbitMQ vhost 都是上述单值 enum，不接受任意业务对象名。
- 每种插件只接受结构化字段：主机、端口、必要的数据库/虚拟主机或固定 GoPulse topic/group 选择、用户名、超时与独立 Secret；不接受原始 DSN、完整 URL、额外 query 参数或任意环境变量。
- Backend 执行类型/大小/字符校验，Monitor 重新根据官方目录校验；Frontend Schema form 只改善 UX，不是授权或安全边界。
- host mode 只允许规划的回环原点；container mode 只允许该插件对应的固定 Compose service DNS 和受限端口。拒绝固定 IP、userinfo、`host.docker.internal`、控制字符和跨协议跳转。
- 连接测试使用 release catalog 中该 ID 当前官方 entrypoint 的固定 one-shot check mode；该模式由 Monitor 选择且与 `/metrics` 共用只读协议 adapter，不接受 Manifest hook、请求控制的命令/参数或任意脚本。进程有界运行，不持久化候选 Secret、不创建 Registry/release/process record，也不返回上游原始错误。
- install/config update 必须重新校验，不把早先的连接测试当作永久授权。首次配置必须提供本表所有必填 Secret；配置替换时 Secret key 省略表示保留，非空字符串表示替换，显式 `null`、空串或额外 key 均拒绝。

### 7.3 Secret 与持久布局

- Registry 只保存 Manifest、当前版本、desired state、非敏感配置修订引用和时间，不内联 host/username 等配置内容；不保存密码、token、完整连接串或进程参数。
- 每个 plugin ID 有独立配置与 Secret 文件，使用原子替换、安全父目录验证和最小文件权限；Secret 不与非敏感 Registry 串行化到同一公共 DTO。
- 启动子进程时由 Monitor 按固定白名单注入分离字段，不把 Secret 放入 command line、工作目录、事件或结构化日志。
- 公共状态只返回 `configured`、配置修订、固定 target 摘要、Secret 是否已设置和安全错误；绝不回显原始 Secret、内部路径、PID、命令或完整主机连接信息。
- 配置变更先写候选修订、验证和试启动，成功后再原子激活；失败恢复旧制品、旧配置/Secret 修订与原 desired state。

### 7.4 Redis v1 状态迁移

- Phase-14-01 必须识别现有单 Redis Registry/layout，先验证根边界、current symlink、制品 digest、进程归属和 desired state，再生成新通用记录。
- 现有 Redis 目标字段从受信 Monitor 配置导入唯一默认配置；Secret 进入独立存储，Registry/API 不读回。
- 状态迁移保留原 Manifest/包版本、desired state、installed/updated time 与可安全继承的采集时间；它不是二进制升级。running 使用命中 `legacy-v1` catalog 的原包经兼容 adapter 恢复并验证真实采集后提交；stopped 只迁移状态，不启动进程。
- 任一迁移步骤失败时保留旧持久状态或使 Redis 进入可解释的安全失败，不删卷、不静默重置为 running，不影响其他后续插件记录。
- 覆盖 stopped/running、已登记旧包相对 current 较新/较旧、中断迁移重试和未登记/损坏状态拒绝；不把人工清 volume 写成升级步骤。legacy-v1 adapter 只保留原受信 Redis 配置和运行能力，不伪造 v2 Manifest；其 configuration 写操作返回 `upgrade_required`，管理员显式升级到 v2 后再修改配置。历史状态兼容与新包能力必须分开验收。

### 7.5 配置/制品事务与中断恢复

每 ID 使用不可变 revision 目录，包含该修订的 Manifest 引用、非敏感配置及独立 `0600` Secret 文件；父目录 `0700`。这些文件不得合成可返回的 DTO。每个 ID 的单一原子 active 指针是提交事实，指向一个已完整写入并同步的修订；Registry/current 若为兼容投影，均从 active 重建，不是第二个提交点。不同 ID 不共享事务锁。

| 阶段 | 持久事实、进程与恢复规则 |
| --- | --- |
| prepare | 保存旧 active 与 desired state；候选文件完成写入和 fsync 后标记 prepared。旧 active 不变；此时中断丢弃未提交候选，不接纳候选 Secret |
| trial | running 先取消旧 collector，有界停止已验证归属的旧进程，再在同一固定端口启动候选；不要求同 ID 双进程零停机。独立持久 process record 标明候选归属，不能据任意 PID 清理 |
| commit | 候选健康且真实采集成功后原子替换 active 并同步父目录；随后重建投影。此点之前重启恢复旧 active，此点之后恢复新 active，均先核对/清理已证明归属的候选进程 |
| rollback | 提交前失败停止已证明归属的候选、保留旧 active；旧 desired=running 则恢复旧包/config/Secret，旧 desired=stopped 则保持停止。恢复也失败保留旧 active 与 desired，仅 observed 标记安全失败 |

- stopped 的 configure/update 允许受控临时试启动，但不启用周期 collector；成功采集一次后必须先停止候选，再提交新 active，最终仍为 stopped。迁移 stopped 是例外：不试启动、不暗中升级。
- install 旧 active 为空，成功后提交 running；失败不得产生已安装记录。start/stop 仅改变同 ID 的 desired state 和运行态；重复请求幂等，同 ID 同时发生的变更采用串行化，不允许并行事务。
- 连接测试没有 prepare/trial 持久阶段：Secret 仅在受信内存和固定进程环境中传递，不创建 registry/release/process record；父进程必须在超时/取消时回收它。
- 迁移另保留只读旧布局及单一迁移提交标记；提交前按旧布局恢复，提交后只按新 active 恢复，不扫描两个布局拼装状态。只有完整迁移成功后才允许回收备份，验收期间保留。
- Phase-14-01 固定验证 prepare 后和 active 替换后两个代表性中断点，以及 running 更新失败与 stopped 更新成功；不扩展成所有写盘步骤的全排列。

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

`config` 是只含非 Secret 字段的完整替换对象，`secrets` 是独立对象（Redis 示例：`{"config":{"host":"redis","port":6379,"database":0,"connect_timeout":"1s","scrape_timeout":"2s"},"secrets":{"password":"<candidate>"}}`）。install/connection-test 请求必须包含 `secrets` 对象，无 Secret 的 Kafka 使用 `{}`；configuration 可省略 `secrets` 或其 password key 表示保留。Elasticsearch 从无认证转为认证必须同时提供 username/password；本阶段已配置认证不能通过省略/null 清除，移除 username 却保留旧 password 必须拒绝。connection-test 不借用已安装 Secret，必须提供候选完整认证信息。

全部对外路由必须先经认证和数据库当前 `admin` 授权；Monitor 只验证 Backend 内部身份并重做输入校验，不直接查询用户角色。Backend 和 Monitor 双层拒绝未知 ID、多目标数组、未知字段、超限请求和不安全上游响应。已知错误映射为稳定公共 code/message，任何协议原始错误、主机、账号、路径或 Secret 都不进入响应。

### 8.3 Frontend 最小扩展

- 当前 Redis 专用页改为六类固定卡片/详情，清楚显示 installed/configured/desired/observed、版本、最近采集/成功和安全错误。
- 配置表单只根据 Backend 固定 catalog 生成允许字段；Secret 输入不回填、不写 local storage、不进 URL；候选认证字段在提交结束（成功/失败）或离开后清理，需要重试时重新输入。
- 连接测试、安装、启停、配置替换和更新均有独立提交中/成功/失败反馈，防止双击重复操作；停止、替换和更新保留明确确认。
- 从插件详情可进入指标查询并选中该 source 的服务端目录项。页面不接受用户输入 PromQL。
- 继续使用 Phase 11 管理壳层与 Phase 13 `AdminLayout` 隔离；不建新 Frontend 应用，不改造普通用户导航。

## 9. Metrics 消息、传输、存储与查询契约

### 9.1 多来源 envelope

- Metrics Envelope v2 顶层字段固定为 `schema_version=2`、`message_id`、`type=metrics`、`source`、`timestamp` 和 `payload`；`payload` 字段固定为 `producer_kind`、`producer_id`、`producer_version`、`target_id`、`scrape_status` 和 `samples`。每个 sample 继续只包含 `name,kind,labels,value`。
- `producer_kind` 只允许 `exporter_plugin|component`；前者 producer ID 是六个固定 plugin ID，后者是 `backend|business-worker|search-indexer|monitor|router|marshaller`。组件 target ID 固定为 `<producer_id>-local`；`producer_version` 为三段产品/插件 SemVer。
- `source` 必须与 producer catalog 唯一对应；插件使用六个基础设施 source，组件使用六个 component ID 作 source。`scrape_status` 只允许 `success|target_unavailable`。
- v2 不继续把自研组件伪装成 plugin ID，也不允许 sample 自带 `source`、`target_id`、`producer_kind` 或 `producer_id` 保留标签。Monitor 观测对象使用独立 `scraped_producer_kind`、`scraped_target_id` 标签，绝不复用这些 producer 来源身份键。Router/Marshaller 的被处理消息来源使用 `message_source`，不能用 `source` 覆盖组件自身来源。
- Router 在过渡期同时接受历史 Redis metrics v1 和新多来源 metrics 契约，仍只写固定 `gopulse-observability-v1` Topic，不解析 samples、不重组 body、不允许请求选 Topic。
- Marshaller 严格校验 source/producer/target/family/kind/label/value/count 组合，按服务端版本化目录转换；未知或超界样本作为确定性非法记录跳过，不污染存储且不阻塞后续合法记录。
- 非 Redis 存储添加受验证的 `source`、`target_id`、`producer_kind`、`producer_id`；Redis 两版都按 §9.3 的兼容例外只写旧来源标签。任何来源都不从 sample 接受保留标签。
- 最大 body、family、sample、label 数量、label 值和时间范围保持有界。传输继续是 at-least-once，幂等与 Kafka offset 语义不因多 source 改变。

### 9.2 Backend 指标目录

- `GET /api/v1/observability/metrics` 继续只允许服务端目录中的 metric/range，不提供任意 PromQL、label matcher 或上游 URL 透传。
- 每个目录项固定 metric name、kind、unit、source、target ID、producer 身份与允许的公共 labels。Backend 只构造该精确查询并严格校验 VictoriaMetrics matrix 响应；Redis 的 producer 由固定服务端 catalog 推导，不添加历史中不存在的 producer matcher，详见 §9.3。
- 目录可按 `source`/`producer_kind` 分组供 Frontend 选择，但不返回内部目标主机、凭据、PromQL 表达式或未管理 labels。
- 历史 Redis v1 series 保留原 `source=redis,target_id=redis-exporter-local`；新契约继续沿用这两个身份，使 Redis 查询时间线不因迁移断开。

### 9.3 Redis 历史 series 的唯一兼容策略

Phase 14 不重写历史存储、不双写两组 series、不在 Backend 合并两套标签。Redis v1 和 v2 都写原来的完整 label 形状：`source`、`target_id` 加该 family 原有业务 labels；producer 身份仍在 v2 envelope 中严格验证，但不新增为 Redis 存储标签。其他 source 才使用 §9.1 的四个来源标签。

| 位置 | 精确示例/规则 |
| --- | --- |
| 迁移前与迁移后存储 | `gopulse_redis_up{source="redis",target_id="redis-exporter-local"} 1`；CPU 仅额外 `mode`，DB 仅额外 `db` |
| Backend 查询 | `gopulse_redis_up{source="redis",target_id="redis-exporter-local"}`，CPU/DB 继续原固定查询合同，不按 producer_version 切分 |
| 返回校验 | `__name__`、两个固定来源标签和 family 允许 labels 必须精确匹配；Redis 响应额外携带 producer 标签也拒绝，而不是忽略 |
| 公共响应 | 保持 Phase 13 DTO，迁移前后时间范围返回同一 label 集的 series；不得添加迁移标记或合成历史样本 |

Phase-14-01 用迁移前写入的真实 v1 点和迁移后真实 v2 点查询跨越迁移时刻的同一时间范围；断言均可读、没有额外 producer series，并验证错误 producer 的 v2 输入被拒绝。Phase-14-06 复用这一策略验证最终版本，不重新选择兼容方案。

## 10. 六类官方插件权威指标目录

每个 Exporter 成功响应必须包含它的完整固定 family 集；目标级连接/认证/超时失败只返回 `503` 与对应 `gopulse_<source>_up 0`，不返回部分或上次成功值。上游字段映射可在实施时根据锁定版本调整，但对外 family 名、kind 与 label 形状以下表为权威合同：

| source | 固定 families（未特注均无 label） | kind / 标签 |
| --- | --- | --- |
| Redis | `gopulse_redis_up`、`uptime_seconds`、`connected_clients`、`used_memory_bytes`、`commands_processed_total`、`keyspace_hits_total`、`keyspace_misses_total`、`cpu_seconds_total`、`db_keys`、`db_expiring_keys`，后九项均以 `gopulse_redis_` 为前缀 | up/uptime/clients/memory/db 为 gauge，commands/hit/miss/CPU 为 counter；CPU `mode=user|system`，DB 两项仅单一数字 `db` |
| MySQL | `gopulse_mysql_up`、`uptime_seconds`、`connections`、`max_connections`、`threads_running`、`queries_total`、`slow_queries_total`、`transactions_total`、`buffer_pool_data_bytes`、`buffer_pool_dirty_bytes` | up/uptime/connections/threads/buffer 为 gauge，queries/slow/transactions 为 counter；transactions 固定 `result=commit|rollback` |
| RabbitMQ | `gopulse_rabbitmq_up`、`connections`、`channels`、`queues`、`consumers`、`messages`、`published_total`、`delivered_total`、`acked_total` | up/connections/channels/queues/consumers/messages 为 gauge，其他为 counter；messages 固定 `state=ready|unacked` |
| Kafka | `gopulse_kafka_up`、`brokers`、`controller_available`、`partitions`、`under_replicated_partitions`、`offline_partitions`、`consumer_group_lag` | 全部 gauge 且无 label；topic/group 是配置中的服务端固定值，不再写入 label |
| Elasticsearch | `gopulse_elasticsearch_up`、`cluster_health_status`、`nodes`、`data_nodes`、`active_primary_shards`、`active_shards`、`relocating_shards`、`initializing_shards`、`unassigned_shards`、`pending_tasks`、`documents`、`store_size_bytes` | 全部 gauge；health 固定三个 one-hot sample `status=green|yellow|red`，不带 node/index/shard 名 |
| VictoriaMetrics | `gopulse_victoriametrics_up`、`rows_inserted_total`、`query_requests_total`、`active_timeseries`、`storage_rows`、`storage_size_bytes`、`free_disk_space_bytes`、`active_merges`、`storage_rows_deleted_total` | up/active/storage/disk/merge 为 gauge，rows/query/storage_rows_deleted 为 counter；无 label，只映射锁定上游 families |

表中省略前缀的 family 均继承本行 source 的 `gopulse_<source>_` 前缀。Phase 14 只使用 counter/gauge，不增加 histogram。若锁定上游无法稳定提供某项，必须在对应批次开工前先修订总方案与未开工 split plan，不得在代码中静默缺省或替名。

### 10.1 聚合语义与上游映射确认

下表是产品语义决定，不表示已证明锁定上游具备对应接口。每个插件动手接入前，须在其同名实施记录列出每个 family 的上游 API/字段、聚合公式、单位、kind、缺失行为与精确 sample 数，并以锁定版本官方资料及一个最小只读真实快照确认；证据只保留脱敏字段与断言，不保存凭据或大响应。未确认项按 §16.1 处理，不允许自由替换含义。

| 来源 | 固定聚合/初始语义 |
| --- | --- |
| Redis | 沿用既有 10 families 和所配置单 DB 的含义；不能将配置切换为所有 DB，也不改 CPU mode 标签 |
| MySQL | server/global 范围；`connections` 为当前连接数、`max_connections` 为配置上限，不用累计连接量替代。`transactions_total` 固定为显式 COMMIT/ROLLBACK 命令计数，不声称覆盖 autocommit 或所有引擎事务；buffer pool 指标是字节，不把 pages 当 bytes |
| RabbitMQ | 只统计固定 `/` vhost；connections/channels/queues/consumers/messages 与三个消息累计量都必须使用一致 scope。published/delivered/acked 为上游累计量，不使用瞬时 rate 冒充 counter，不把 redelivery 自行加算一次 |
| Kafka | brokers/controller 为集群快照；partition/under-replicated/offline/lag 仅固定 topic。每分区 lag 为 `max(log_end_offset - committed_offset, 0)` 后求和；任一分区没有有效 committed offset、topic/group 不存在或结果不完整时整体安全 `target_unavailable`，不填零、不创建 topic、不初始化/提交 group offset |
| Elasticsearch | health/nodes/shards/task 为集群范围；documents/store 只取 primary 聚合，不重复计入 replica；不暴露各 index 明细。yellow/red 且完整快照可得时仍 up=1 |
| VictoriaMetrics | 只映射服务自身运行计数，query counter 的 API path allowlist 必须固定，active series 的上游时间窗和 storage rows/bytes 的统计范围必须记录；不存在同义稳定字段时必须修订目录，禁止把累计 created series 当当前 active series |

- success 必须满足完整目录；缺失字段不因“看起来是冷启动”而默认零。只有锁定接口有明确零省略语义且已写入映射表时才允许补零，认证/权限错误绝不能按零处理。
- 采集到的 counter 重置原样表达，不本地累计掩盖重启；gauge 不用历史成功值填补。动态上游值的验收比较同一次/有界时间窗的快照和单调关系，不要求两次采样瞬时绝对相等。
- Kafka 冷启动依赖真实 Marshaller 消费产生正式 committed offset；缺失时只让 Kafka 插件等待/安全失败，不把它加入 Router/Marshaller 业务启动依赖。验收先产生一条真实可观测消息并确认正式消费，再检查 Kafka 插件自动恢复或重试安装。

#### VictoriaMetrics 删除行计数合同修订（2026-09-11）

经用户确认，第九项正式改为 `gopulse_victoriametrics_storage_rows_deleted_total`（counter，单位 rows，无 label），取代未交付的 `gopulse_victoriametrics_retention_deletions_total`。这是一项产品语义调整，不是旧名称的等价重命名；不保留旧名 alias、不双写，也不把旧名放入 Backend/Frontend 指标目录。

- 锁定上游为 Compose 的 `victoriametrics/victoria-metrics:v1.151.0`，映射公式为 `vm_rows_deleted_total{type="storage/inmemory"}`、`{type="storage/small"}`、`{type="storage/big"}` 三个样本之和。仅接受这三个固定 type，不汇总 indexdb 或未来新增 type，不透传上游 labels。
- 含义仅为上述 storage 合并过程报告的已删除行数；显式删除后的合并也可能增加计数。它不是 retention 专属删除量，不保证覆盖所有过期数据清理路径，也不代表当前已删除 series 数、当前存储行数或释放的 bytes。
- 三个选定样本均必须存在且有效；冷启动真实零可以输出零，缺失、重复或不兼容字段必须整体安全失败，不补零、不沿用历史值。上游重启重置原样表达，不由 Exporter 本地累计。
- 修订依据为本批在独立临时实例的真实探测：默认 `1M` retention 下写入当前时间样本，执行显式 `delete_series`，再写入另一 series 并 flush/merge 后，三个 storage 分量由全零变为 `0 / 1 / 0`。因此原候选不能证明 retention 专属语义。探测仅操作临时数据，不是 Exporter 所需能力，不得为采集增加删除或强制合并权限。
- 该探测只解决删除计数的语义选择，不代表完整九项映射、Exporter、制品、六插件闭环或本批验收已经完成。其余映射继续按本节及 §16.1 实证锁定；Phase-14-04 的既有固定门禁不新增独立完整 Compose 运行。

### 10.2 Kafka 部分异常验收拓扑例外（2026-09-10 授权）

产品仍只支持单 broker、唯一 `kafka:19092` 目标及固定 topic/group。Phase-14-03 的真实探测
已否定“向未注册 broker 分配副本”的注入路径；用户批准仅在强归属、可清理的隔离验收环境
临时增加一个同版本 broker-only follower。该例外不改变 §7.2 配置/拨号 allowlist、生产
Compose 默认拓扑、发布制品或产品支持声明；不允许在生产集群扩容或添加多 broker 配置。

权威可执行合同见 `Phase-14-03-Kafka与Elasticsearch插件闭环.md` §2.2：先用真实业务链路
建立有效正式 committed offset，再由验收管理员将固定 topic 临时扩为两副本；保持 leader、
正式 group coordinator 及必需 offsets 分区 leader 在原 broker，仅停 follower，取得
under-replicated>0、up=1、HTTP 200 的完整 7 families / 7 samples 与 Backend 对照证据。
随后恢复 follower、验证同一 Exporter 进程的新快照，恢复原单副本并清理临时资源，最后在
原产品单 broker 基线上完成其余门禁。采集器仍不得写 topic/config/offset 或连接新 origin。

第二 broker 必须使用锁定 `apache/kafka:4.3.1`、临时 override、该次随机 project 专属资源，
不加入 controller quorum、不发布宿主端口、不接触既有业务资源。将验收管理员的副本调整
与采集器只读行为分别记录；成功、失败、中断均需强归属清理证据。正式 offset 只能来自
正常 Marshaller 消费；不以手工 offset、fixture 或历史指标替代真实状态。

该流程纳入既有 source-focused 固定验收入口；Phase-14-06 仅按既有证据复用规则引用实际
成功记录，不因这一例外另做拓扑矩阵。批准计划不等于流程已实证；步骤或客户端安全边界
不能满足时仍按 §16.1 处理，不静默放宽门禁。

## 11. 自研组件指标目录与基数预算

### 11.1 权威 family 与标签契约

| 组件 | 固定 families | kind / 允许 labels |
| --- | --- | --- |
| Backend | `gopulse_backend_http_requests_total`、`http_request_duration_seconds_total`、`outbox_pending`、`outbox_oldest_age_seconds`、`outbox_last_publish_success_timestamp_seconds`、`dependency_up` | 前两项 counter，outbox/dependency 为 gauge；HTTP 仅 `method,route,status_class`，dependency 仅 `mysql|redis|rabbitmq|elasticsearch` |
| Business Worker | `gopulse_business_worker_messages_total`、`message_processing_duration_seconds_total`、`messages_in_flight`、`prefetch_limit`、`last_success_timestamp_seconds`、`dependency_up` | 前两项 counter，其他 gauge；前两项仅 `event_type,result`，dependency 仅 `mysql|rabbitmq` |
| Search Indexer | `gopulse_search_indexer_messages_total`、`message_processing_duration_seconds_total`、`messages_in_flight`、`retrying`、`last_success_timestamp_seconds`、`dependency_up` | 前两项 counter，其他 gauge；前两项仅 `operation=create|update|delete,result`，dependency 仅 `mysql|rabbitmq|elasticsearch` |
| Monitor | `gopulse_monitor_scrapes_total`、`scrape_duration_seconds_total`、`last_scrape_success_timestamp_seconds`、`event_queue_length`、`event_queue_dropped_total`、`plugins_running`、`dependency_up` | scrape 两项 counter，dropped counter，其他 gauge；scrape 两个 counter 仅 `scraped_producer_kind,scraped_target_id,result`，last scrape success 仅 `scraped_producer_kind,scraped_target_id`，dependency 仅 `router` |
| Router | `gopulse_router_messages_total`、`produce_duration_seconds_total`、`buffered_records`、`buffered_bytes`、`last_kafka_ack_timestamp_seconds`、`dependency_up` | 前两项 counter，其他 gauge；前两项仅 `type,message_source,result`，dependency 仅 `kafka` |
| Marshaller | `gopulse_marshaller_records_total`、`record_processing_duration_seconds_total`、`records_in_flight`、`retrying`、`last_storage_success_timestamp_seconds`、`last_commit_success_timestamp_seconds`、`dependency_up` | 前两项 counter，其他 gauge；前两项仅 `type,message_source,stage,result`，last storage 仅 `storage=victoriametrics|elasticsearch`，dependency 仅 `kafka|victoriametrics|elasticsearch` |

表中同一行第一个 family 之后省略前缀的名称均继承该组件前缀。`event_type`、`type`、`message_source`、`stage`、`result` 的值必须取自当前严格消息契约的服务端 allowlist；不得使用未知原值作 fallback label。Backend `route` 取 Gin 匹配后的注册模板，无匹配路由统一使用固定 `_unmatched`，最大不超过实际注册路由数加一。

### 11.2 基数和信息安全

- 不得使用 user/post/comment/event/message/request ID、username、标题/正文、原始 URL/path/query、SQL、cache key、queue/topic/index 任意名称、错误原文或堆栈作为 label。
- route 只能是有界的服务端模板，status 使用 `2xx|4xx|5xx` 等有界类别；dependency、operation、stage、result 都使用代码固定枚举。
- 每个组件的 family 数、每 family label key/value 集和最大 sample 数写入 Monitor/Marshaller 双层目录，任意新 label 默认拒绝。
- 延迟固定使用表内 duration counter total 与对应 operation count，二者使用相同 label 组合和计数时刻；不改成 last-duration gauge；不为“更细”默认引入大量 histogram bucket。
- 组件 metrics 端点不包含日志、配置 dump、环境变量、build path、goroutine 栈或无界 runtime 指标全量暴露。

### 11.3 组件初始值、采样与内部 listener

- counter 和 in-flight/retrying/queue 在其事实为零时输出零；未发生成功时 last-success/last-ack/last-commit 时间戳固定为 `0`，不是当前时间，也不省略 family。时间戳单位是 Unix seconds，duration 单位是 seconds。
- `dependency_up` 固定 `-1=尚无观测`、`0=最近一次实际交互失败`、`1=最近一次实际交互成功`；它不是主动探测承诺。空闲时保留最后观测，不伪造成功。Monitor/Marshaller/Backend 对该 family 明确允许这三个值。
- label tuple 按固定目录惰性创建，端点可以没有尚未发生的 counter tuple；无 label 的 family、固定 dependency 和 last-success 必须始终存在。每个 tuple 首次出现时 count/duration 成对输出。未知输入只使用预定义 `unknown` 桶或不记录对应维度；是否存在该桶须在 §16.1 的 label 目录确定，禁止直接写未知原文。
- Backend outbox pending/oldest-age 使用有界后台聚合采样，不在业务请求或 metrics handler 中新增网络查询；固定 5s 周期、单次 1s 超时，不读 payload。首次尚无成功快照或采样失败时 endpoint 安全返回 503，不能把未知 backlog 写为零；成功后空 outbox 的两值为零。
- component endpoint 不可用时 Monitor 不合成组件业务零值；只更新自己的对应 scrape counter/status 和安全事件，不发送伪造成功快照。
- 六组件统一增加独立 internal listener：`GET /internal/v1/metrics`；Backend `19101`、Business Worker `19102`、Search Indexer `19103`、Monitor `19104`、Router `19105`、Marshaller `19106`。不复用 Backend 已发布 HTTP listener，也不经 Frontend/Backend 公共反代路由转发。
- host mode 绑定 `127.0.0.1`；container mode 绑定容器网络接口，Monitor 只访问固定 service DNS/上述端口。Compose 包括 Backend 在内均不得发布这些端口，受控验收客户端通过内部网络检查。新 listener 与主服务共用 root context/shutdown deadline，不引入另一套生命周期脚本。
- 每组件使用独立、至少 32 bytes token，Monitor 持有对应只读 token；不复用管理 API token，不把 token 写入 URL。无/错误/重复 Authorization 返回 401；认证后依次检查 path、method、query/body：未知 path 返回 404，已知 path 非 GET 返回 405，GET 含 query 或非空 body 返回 400；响应为固定安全错误。

## 12. 故障隔离、重启恢复与可解释性

故障验收先声明影响边界：单 Exporter 连接/认证/进程故障应隔离到该 ID；实际停止 Kafka/Elasticsearch/MySQL 等共享基础设施可能同时影响指标传输、查询或原业务依赖，不能要求这些依赖的消费者在停机期间仍正常。后一类验证既有局部 unavailable/降级、无插件引入的额外阻塞、进程存活和恢复；不把“所有查询一直成功”作为验收。不增加新业务容错能力来补足不可能的隔离承诺。

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
- Compose 向 Monitor 提供六类目标所需最小账号/密码和固定 service DNS。必须使用经验证的最小采集权限账号；不复用 root 账号，不把 Secret 写入 image layer、label 或启动日志。
- 六个 Exporter 仍由 Monitor 容器内子进程运行，不新增六个默认 Compose service；现有 Redis standalone profile 只作兼容/诊断，不与 Monitor 同时成为默认所有者。
- 如因只读根文件系等容器边界需要 runtime 目录，只能使用 Monitor 专属已归属 volume/tmpfs，不挂载 Docker socket 或宿主任意目录。
- `scripts/dev.sh`、`verify.sh`、`down.sh` 和 Phase 14 Compose 验收继续管理一个强归属 project，必须保留已有项目/容器/网络/volume 不变和有界信号退出。

### 13.1 采集账号的新卷与既有卷交付

Phase-14-02 必须交付一个有界、幂等的账号调和步骤，同时服务新卷初始化和 Phase 13 既有卷升级，不依赖“仅空数据目录执行”的初始化脚本，不删除卷。

1. 先固定 MySQL 只读状态查询和 RabbitMQ `/` vhost 所需 API，再在同名实施记录列出精确 SQL grants、RabbitMQ tags/permissions 与脱敏校验命令；当前文档未验证最小权限，不把 monitoring 标签本身当作权限足够的证据。
2. 调和步骤使用部署管理员提供的凭据，通过受控 Secret 文件读取，仅连接本项目固定目标；不得将管理员凭据交给 Exporter。权限不足安全失败，不自动回退 root 采集。
3. 使用独立专用账号，与业务账号分离；已存在同名但非本流程归属的账号返回冲突，不接管或更改其密码/权限。新建账号的归属标记与 Secret 引用保存在本项目受控持久状态中，输出不含凭据。
4. 对本流程已有账号幂等校验预期权限，不重复生成密码、不授予全库业务读写、不更改业务账号；需要修正或轮换时必须是显式部署操作。权限撤销/拒绝操作只在隔离验收资源内验证。
5. 先保存候选采集 Secret，再创建/核对账号与授权，connection-test 成功后原子激活 Monitor Secret 引用。中断保留可识别的待完成状态，同一候选 Secret 可重试；失败不回滚/删除业务账号，也不向 Monitor 发布半配置状态。
6. 新卷和有既有业务数据的旧卷各跑一次，旧卷重复调和一次证明幂等；分别验证允许的状态查询、禁止的业务写入/对象修改、原业务账号和数据不变。账号未就绪只阻断相应插件，不阻断业务服务 readiness。

具体 API/权限在 §16.1 确认前是该插件的明确阻断项，不允许在 implementation 中试授 `ALL` 后把验收通过当最小权限证明。

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
- 以真实 Kafka/Elasticsearch 验证部分状态、完全不可达、恢复和 Backend 查询，不绑定动态 broker/node 标签；Kafka 部分异常仅按 §10.2 使用临时 follower，随后恢复产品单 broker 基线。
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
7. 单 ID 连接故障时该插件安全降级，其他插件、历史 metrics 和社交业务可用；实际共享依赖停机按 §12 验证局部降级与恢复，不要求依赖它的查询仍成功。

### 15.2 迁移、Secret 与完整性

- 从 Phase 13 真实 Redis Manifest v1/Registry/volume 升级后，安装版本、desired state 和历史查询保留，新契约下 Redis 可继续采集；迁移重试不重复记录。
- 空 volume 可调和六个嵌入官方包；同 volume 替换 Monitor 容器后按六个 desired state 恢复，stopped 的类型不被静默启动。
- 篡改入口、Schema digest、未知 ID/source、不兼容契约、非安全 archive 和伪造 process record 均在执行/发信号前被拒绝。
- 使用故意包含特征串的密码、token、用户名和连接错误后，API 响应、结构化日志、Events、metrics labels、Registry 与诊断输出都查不到该特征串。Frontend 在候选表单提交并清理或离开页面后扫描 DOM，不把用户正在输入的候选字段当泄漏；私有 config 可保存 username，Secret 文件可保存密码，但二者均不进入公共 DTO/Registry/诊断导出。

### 15.3 自研组件指标

- Backend、Business Worker、Search Indexer、Monitor、Router 和 Marshaller 各有至少一条代表性处理结果、一条延迟信号、适用的队列/消费进度、最近成功和依赖降级可在 Backend 固定目录查询。
- 未认证访问内部 metrics 端点被拒绝；从浏览器/宿主不可直接访问包括 Backend 在内的六个独立内部 metrics listener；公共业务端口不提供该路由。
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
- 每批固定包含版本/分支治理检查；提交前对最终工作树运行 `git diff --check` 与 `git diff --cached --check`，提交后才用 `git diff --check upstream/main...HEAD` 核对提交范围，不能用尚未包含本次改动的 HEAD 比较代替工作树检查。任何因环境不可用未运行或失败的命令必须如实记录，不得将批次或阶段标为完成。

### 16.1 有限前置确认与禁止猜测清单

以下项目是尚待实证的依赖，不是已完成结论。开工时只读直接调用点、锁定版本官方资料并运行最小受控探测；遵守初始发现最多 10 分钟和已有执行效率规则。仍无法确认时记录具体缺口，先做不依赖它的本批契约实现；依赖路径不得以假值/扩大权限/跳过校验继续。证据显示产品合同不可实现时先在 `update` 修订受影响总/分方案，再实施，不静默降级验收。

| 责任批次 | 必须确认的有限输入 | 完成证据与未满足处理 |
| --- | --- | --- |
| Phase-14-01 | 受支持 Phase 13 旧包版本/工具链/digest、v1 兼容 adapter 字段能力、current/retained/acceptance 包清单 | 可复现构建与真实 v1 卷确认；无可信旧包证据不得宣称支持该来源，不导入现场任意 digest |
| Phase-14-02 | MySQL/RabbitMQ 每 family 上游字段、vhost scope、精确最小权限和账号调和入口 | 按 §10.1/§13.1 的表、一个真实快照和权限允许/拒绝证据；scope 或权限不成立先修订计划 |
| Phase-14-03 | Kafka offset 缺失、§10.2 临时 follower 部分异常注入与原单 broker 恢复、Elasticsearch primary 聚合字段 | 锁定 API；强归属验收管理员副本调整与采集器只读行为分离，记录真实快照/Backend/清理证据；不能用 fixture 冒充真实异常，不扩建生产集群或放宽客户端拨号边界 |
| Phase-14-04 | VictoriaMetrics 每个 family 的实际字段、query allowlist、active 时间窗和冷启动省略行为 | 完整映射与脱敏真实值；缺 family 先修订权威目录，不填零或近似替代 |
| Phase-14-05 | 各组件 label value 枚举、计数/耗时对应点、最大 family/sample/body 预算 | 同名记录写出数值与 allowlist，Monitor/Marshaller/Backend 同步注册；不允许使用“有界”“等价”代替实际预算 |
| Phase-14-06 | 前五批证据有效性、验收镜像/可信成功和失败包、旧卷与账号 fixture | 缺失即回到对应未满足项，不重新选择契约，不把最后一批变成另一次设计阶段 |

前置探测服务于本批验收，不扩展为上游源码审计、全量权限测试或指标覆盖率活动。确认结果记录在既有同名开发记录中，不额外创建默认独立评审报告。固定成功证据输入未变则沿用。

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
