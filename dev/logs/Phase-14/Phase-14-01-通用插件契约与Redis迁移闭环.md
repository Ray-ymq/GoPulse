# Phase-14-01：通用插件契约与 Redis 迁移闭环开发记录

## 状态

**部分实施，未完成批次验收，不可作为 `1.11.1` 发布。**

- 日期：2026-09-09。
- 已执行 `git fetch origin`，从 `origin/main` 创建 `develop/1.11.1`。`origin`、`upstream` 配置为同一个仓库。
- 批次目标仍为总实施方案分配的 `1.11.1`；根与 Frontend 完成版本保留 `1.10.6`，不以基础契约的完成代替整个批次完成。
- 未推送分支，未创建 PR，未操作现有 Redis 运行卷。

## 已完成实现

### 官方身份与配置 Schema

`monitor/internal/plugin/catalog.go` 提供返回独立值的六类固定目录及 ID lookup：

- Redis、MySQL、RabbitMQ、Kafka、Elasticsearch、VictoriaMetrics。
- 固定 `source`、`plugin ID`、`target ID`、入口、回环端口和运行模式端口。
- 仅 Redis `available=true`；该字段是本批制品交付范围，不表示已安装或正在运行。
- 这些内部结构包含运行原点/端口，不能直接作为公共 API DTO 返回。

`monitor/internal/plugin/schema.go` 定义受限 Schema v1，并按总方案字段顺序、类型、必填性、Secret 标记、边界和单值 enum 生成六类官方 Schema。duration 边界使用毫秒，密码长度使用字节，其他字符串长度边界表达 Unicode 字符数。

Schema 验证执行文件 SHA-256、完整 JSON/UTF-8、任意层级重复键检测、严格字段类型以及官方结构比对；自洽但修改了官方约束的 Schema 仍被拒绝。此处只交付 Schema，不代表其他五种 Exporter 或其配置 adapter 已实现。

### 独立 Manifest v2 与 archive 校验

- `types.go` 增加 `runtime_contract_version`、`metrics_contract_version`、`config_schema_path`、`config_schema_sha256`；v1 序列化保持省略这些字段。
- `manifest.go` 的 `ParseManifestV2` 固定 `2/1/2` 契约组合和 `config.schema.json`，验证官方 ID/source/entrypoint 组合、当前 Linux 架构、SemVer、digest、健康和采集路径；拒绝未知、缺失、重复字段。
- 现有 `ParseManifest` 仍只接受 v1。新 parser 没有使旧运行入口自动接受 v2。
- `archive.go` 增加独立 `extractPackageV2`，复用大小、entry 数、路径、链接拒绝与 staging 提取逻辑，并在 v2 路径限制为 `plugin.json`、`config.schema.json`、已交付 Redis 入口及可选 `bin` 目录，拒绝特殊权限位、额外文件和 Schema 内容/digest 不匹配。
- v2 archive 校验**仅证明内部完整性**，不证明官方发行身份；尚未建立镜像编译期 release catalog，也未把 v2 提取器接入 install/bootstrap/update。不能将通过该函数的制品直接执行。

### Redis 配置解析

`monitor/internal/plugin/redis_config.go` 提供 `ParseRedisConfiguration`：

- 输入是唯一结构化对象，字段为 `host`、`port`、`database`、`connect_timeout`、`scrape_timeout`、`password`。
- host mode 仅回环 `127.0.0.1` / `::1:6379`；container mode 仅 `redis:6379`。
- 数据库 `0..15`；`100ms <= connect_timeout <= scrape_timeout <= 10s`。
- 首次必须提供密码；替换时省略密码保留调用者提供的旧 Secret；拒绝 null、空串、数组、未知字段、重复字段和不安全原点。
- 返回独立 `RedisConfig` 与 `RedisSecret`；普通配置 JSON 不含密码，失败返回零值和固定安全错误。
- 此函数不写盘、不启动进程、不创建连接；持久化、API 和 Exporter 环境注入尚未接入。

## 实际文件

- `monitor/internal/plugin/catalog.go`
- `monitor/internal/plugin/schema.go`
- `monitor/internal/plugin/redis_config.go`
- `monitor/internal/plugin/types.go`
- `monitor/internal/plugin/manifest.go`
- `monitor/internal/plugin/archive.go`
- `monitor/internal/plugin/contract_v2_test.go`
- 本开发记录。

## 实际验证

| 命令 | 结果 |
| --- | --- |
| `docker info --format '{{.ServerVersion}}'` | 成功，ServerVersion `29.7.2`；只确认 Docker 可访问，未执行 Compose 验收 |
| `(cd monitor && go test ./internal/plugin)` | 两次实施中检查均通过；第二次之前新增了 Schema/config 代码 |
| `(cd monitor && go test ./internal/plugin -run 'TestOfficial\|TestManifestV2\|TestRedisConfiguration\|TestExtractV2' -count=1)` | 通过；覆盖目录、v2 拒绝边界、Schema 内容校验、配置分离/替换和受限 archive |
| `(cd monitor && go test ./...)` | 最终生产代码 diff 上通过，包括旧 v1 plugin、collector、HTTP、Events 和 Logs 单元测试 |
| `python3 scripts/ci/validate_versions.py` | 通过，现有版本元数据与 `1.10.6` 一致；不是目标批次版本验收通过 |
| `python3 scripts/ci/validate_branch.py --branch develop/1.11.1 --base-ref upstream/main` | **失败**：当前 `VERSION=1.10.6`，分支要求 `1.11.1`；批次未完成，不能为让此检查通过提前标记完成版本 |
| `git diff --check` | 通过 |

拒绝测试使用合成 archive 和 Secret 特征串，没有运行该 archive 中的测试字节，也没有使用真实凭据。配置 JSON 不包含特征串的断言已执行；API、日志、Events、Frontend DOM、真实 metrics 的特征串扫描尚未执行。

## 未完成验收与偏差

本次提交仅落地实施顺序第 1 项的契约基础，并保留现有 v1 行为；**没有完成整个实施方案**。以下均为当前批次必需工作，不是可跳过的非阻断优化：

1. 官方包可复现构建、镜像 release catalog 的完整 archive/entrypoint/schema 信任验证，以及新入口的实际接入。
2. 按 ID 独立操作锁、collector、运行时目录，以及通用 Registry/config/Secret 原子修订和回滚。
3. Redis Exporter one-shot check、无持久化 connection-test、首次真实采集门槛和 configure/update 失败恢复。
4. Phase 13 真实 running/stopped 卷 fixture、v1 可重入迁移、损坏状态和中断恢复测试。
5. metrics v2 producer 契约及 Router/Marshaller/Backend 的 v1/v2 查询兼容。
6. Backend 安全 DTO、授权、管理 API 与 Frontend 六卡片、配置/Secret 表单和操作闭环。
7. `scripts/verify-plugin-metrics.sh` 强归属 Compose 验收、真实 Redis 故障/恢复、Backend 查询、Logs/Events、社交/搜索回归。
8. 剩余各模块和 Frontend 固定门禁、根/Frontend 版本更新及批次完成提交。

未运行 exporters/redis、Router、Marshaller、Backend、Frontend 或完整 Compose 门禁；当前改动尚未接入它们，不能据 Monitor 单元测试推断整个链路通过。

## 后续继续位置

- 当前分支仍是同一未完成批次，后续应继续 `develop/1.11.1`，不要创建另一目标版本或提前打开完成 PR。
- 下一实现项是官方 release catalog/可复现 v2 制品与持久化事务设计和接入，随后运行状态迁移；不要直接把 `ParseManifest` 切换为 v2 造成旧卷无法恢复。
- Schema/catalog 的扩展点已提供，但 config adapter 目前只有 Redis 解析，metric family registration 和聚焦验收扩展点尚未交付；不能据此宣布 Phase-14-02 前置条件已满足。
- 继续工作时沿用已记录的成功验证；只在相关代码、配置或环境发生影响结果的变化后重跑。

## 2026-09-10 续接：请求边界、制品核对与单次连接检查

### 状态与分支

仍为**部分实施，未完成批次验收**。本节是对前次记录的增量，不撤销未受影响的成功证据。

- fetch 了 `origin` 和 `upstream`；两者 main 均为 `5562fd7`。
- 发现已有同批未完成分支，继续 `develop/1.11.1`，以 merge `72fbd93` 合入最新 main 的计划修订，没有覆盖既有实现或重新编号。
- 当前 root/Frontend 版本仍为完成版本 `1.10.6`。没有将候选制品版本冒充完成产品版本；没有推送、创建 PR 或操作日常运行卷。

### 实际实现与文件

- `monitor/internal/plugin/config_request.go`、`config_request_test.go`：新增公共请求形状解析边界，接受完整 `config` 和独立 `secrets`；拒绝在 config 放 password、在 secrets 放非 Secret 字段、重复/未知字段、null、数组和超过 16 KiB 的请求。只有传入旧 Secret 的 configuration replacement 才能省略 Secret；install/connection-test 调用必须传 nil。返回独立内部配置和 Secret，不持久化。**尚未接入 HTTP 路由**，不能据此宣称 API 已交付。
- `monitor/internal/plugin/release.go`、`release_test.go`：增加 image-root + 受信条目集合的验证基础，条目固定完整 Manifest、archive SHA-256、用途和单文件名；区分 current/retained/legacy-v1、拒绝重复 key/同 ID 多 current、未知版本、篡改和 symlink。上传只作同 digest 选择，实际提取的是 image-root 下包；提取后再次比对完整 Manifest（含入口/Schema digest）。**尚无编译期 catalog 生成/镜像注入或 Manager 调用**；测试传入的合成条目不能成为生产信任根。
- `exporters/redis/cmd/redis-exporter/main.go`、`check.go`、`check_test.go`：实际 CLI 增加 `--check`，不启动 HTTP，不写状态，使用 INFO collector 验证真实快照；仅返回固定 reachable/code JSON。候选上下文有 scrape timeout，支持 SIGTERM/SIGINT，禁用客户端原始协议日志。没有 Monitor connection-test 的候选进程管理实现。
- `exporters/redis/internal/config/config.go`、`config_test.go`：增加 `REDIS_EXPORTER_CONNECT_TIMEOUT`，100ms 到 scrape timeout，未提供时保留 v1 的同 scrape budget 默认；主 Exporter 与 check 均使用该 dial budget。
- `monitor/cmd/plugin-package-metadata/main.go`、`main_test.go`：从实际入口字节和内建 Redis Schema 生成 v2 Manifest/Schema，避免另抄一份 Python Schema。
- `scripts/package-redis-exporter.sh`：显式 `--contract-version 2` 生成 v2 三文件包；保留默认 v1，避免现有 Compose/Manager 被未迁移入口破坏；本机构建显式 `GOOS=linux`。归档没有配置实例或 Secret。
- `exporters/redis/README.md`：说明实际 CLI、timeout、v2 制包入口及尚未接入的边界。
- 本同名开发记录。

### 真实旧包来源证据

只创建了一个不启动、不挂载现有卷的临时容器，从本地 `gopulse/monitor:1.10.6` 镜像复制原始包后 `docker rm -v` 删除该临时容器。

| 项目 | 实测值 |
| --- | --- |
| 镜像 ID | `sha256:37530f786708d31eda7d6e3eacfdc83a2218cbe8a85398290406f65ba1c0b546` |
| 镜像 revision label | `f1276484944eb933d2ebb40dc7c960a6a414b5b7` |
| 原包版本/平台 | Manifest v1，`1.10.6`，`linux/amd64` |
| `go version -m` 工具链 | `go1.26.0` |
| 原始 archive SHA-256 | `b992b0dfa80a0983b9af63e4c2a4770216bfd7fcb718af2cd451281cf3306727` |
| 原始入口 SHA-256 | `20978fc780e7531d6c130caf542d7e8ba6fe0e6842ba0610825d5e716941f2fa` |
| Schema digest | 不适用（v1），未以空 Schema 伪造 v2 |

实际执行 `git archive f1276484944eb933d2ebb40dc7c960a6a414b5b7 exporters/redis` 保存独立源副本；在已有 `golang:1.26.0-alpine3.23` 中以 `--network none`、只读源/模块缓存、`CGO_ENABLED=0` 和 `go build -trimpath -buildvcs=false -ldflags='-s -w -buildid='` 重建，入口 `cmp` 与原包一致。再用默认 v1 制包脚本重新归档，完整 archive `cmp` 一致。

这些证据确定了一个可复现 legacy 候选，**不是已登记支持的旧卷清单**。尚未交付真实 running/stopped 卷 fixture、legacy adapter 或 image catalog；不宣称其他 v1 版本/架构可迁移。实际 legacy 配置只有原 Redis host/port/password/db/scrape timeout，不能把新增独立 connect timeout 当成旧包已有能力。

### v2 可复现产物

本机 `go1.26.7 linux/amd64` 连续执行两次：

```bash
bash scripts/package-redis-exporter.sh --version 1.11.1 --contract-version 2 --output .run/phase14-artifacts/v2-a.tar.gz
bash scripts/package-redis-exporter.sh --version 1.11.1 --contract-version 2 --output .run/phase14-artifacts/v2-b.tar.gz
cmp .run/phase14-artifacts/v2-a.tar.gz .run/phase14-artifacts/v2-b.tar.gz
```

`cmp` 通过。候选制品版本 `1.11.1`，契约 `2/1/2`：

- archive：`c04ae8fad8571335984d0ee76c4529509ce26bb1396774528add36512640e53d`。
- entrypoint：`108d1ee61b98f8a0c8e40f9bdd7bb9fd6cd173f2313f9ffa667d1e9ed531126e`。
- Schema：`31f00ae09c7aa46b6a221e2ea310aa077cdda13482a2f3688ae067923e599c81`。

此处只证明同一源和工具链的包可复现；没有声称不同 Go 工具链产物相同。产物均在被忽略的 `.run/phase14-artifacts/`，没有提交二进制、临时配置或运行 Secret。

### 实际验证与结果

| 命令/检查 | 结果 |
| --- | --- |
| `(cd monitor && go test ./internal/plugin)` | 通过；请求边界实现后的最小检查 |
| `(cd monitor && go test ./internal/plugin ./cmd/plugin-package-metadata)` | 通过；制品验证实现后的检查 |
| `(cd exporters/redis && go test ./...)` | 通过；最终 config test 补充默认 dial budget 断言后再次执行 |
| `(cd monitor && go test ./...)` | 通过；最终 Monitor diff（含 metadata 生成测试） |
| `bash -n scripts/package-redis-exporter.sh` | 通过 |
| 原始 v1 入口重建及整包 `cmp` | 均通过，见上节 |
| v2 两次独立制包 `cmp` | 通过 |
| 真实 Redis 一次性 check 探测 | 成功、错误密码失败、正确密码再次成功；无 Secret 特征串输出 |
| `python3 scripts/ci/validate_versions.py` | 通过，完成版本仍一致为 `1.10.6` |
| `python3 scripts/ci/validate_branch.py --branch develop/1.11.1 --base-ref upstream/main` | **未通过**：完成版本 `1.10.6` 不等于目标 `1.11.1`；不通过提前 bump 绕过未完成批次 |
| `git diff --check` | 通过 |

真实探测采用随机 `p1401-check-<uuid>` Docker network/Redis 容器、固定 ownership label；使用本地 `redis:7.2.5-alpine`，不发布宿主端口，不复用现有 volume。check 容器只读挂载本轮 v2 入口，`--read-only --cap-drop ALL --security-opt no-new-privileges`，`connect_timeout=500ms`、`scrape_timeout=1s`，只在一次性环境传测试 Secret。分别捕获 success/failure/recovery JSON，并扫描候选 Secret 和错误密码特征串；没有泄漏。退出清理后按本轮 label 查询 container/network 均为空。

**这个探测不是 `--sources redis --migration` 验收**：没有证明同 Exporter 常驻进程恢复、旧卷迁移、Router/Kafka/Marshaller/VM/Backend 链路或 Frontend DOM。未新增独立全量门禁，也未查看第三方依赖源码。

### 下一未满足项与停止状态

本轮推进了确定的请求契约、候选执行能力、归档验证基础和真实 legacy 制品来源，但并未完成本批必需路径。仍须完成：

1. 基于已核对制品的编译期 catalog 生成/镜像装配，生产 current/retained 与 acceptance 成功/失败包隔离；将验证接入 install/bootstrap/update/legacy 恢复，不能把当前测试构造器当成生产 catalog。
2. 单 active 提交点的不可变修订和独立 `0600` Secret、每 ID 操作锁与 process record 修订归属；尚未写入生命周期事务。实现时按总方案 §7.5，不再另造 Registry 提交事实。
3. 真实旧卷 fixture、迁移及两个固定中断点、running 回滚和 stopped 更新；保留旧状态，不清卷。
4. Monitor 候选进程的独立 timeout/reap、连接测试 HTTP、配置 API、Backend 授权/DTO 和 Frontend 六卡片闭环。
5. metrics v2 producer 契约与 v1 同 series 查询兼容、完整聚焦 Compose 脚本和其余固定门禁。
6. 全批通过后才更新 root/Frontend 到 `1.11.1` 并重跑版本/分支门禁。

Router、Marshaller、Backend、Frontend 及 `verify-plugin-metrics.sh` 固定门禁本轮未执行；上述模块没有本轮改动。上述未完成项都是**本批必需工作**，不是转移到 Phase-14-02 的非阻断优化。下一次继续同一分支，沿用本节未受影响的成功检查，从第一个未满足项继续。
