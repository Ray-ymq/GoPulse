# Phase-14-01：通用插件契约与 Redis 迁移闭环开发记录

## 当前状态

**Phase-14-01 已完成：本批实现与固定验收通过，产品版本 `1.11.1`，开发分支 `develop/1.11.1`。**

最终冻结镜像的聚焦验收进程退出码为 0，项目 `gopulse-p1401-68e7d591d6c2` 的八组验收全部通过，全部自有资源已删除且既有资源保留。完整结论、实际命令/结果、制品清单与环境限制见文末“最终收口记录”。Phase 14 其余批次尚未完成。

## 历史状态（首次契约实现）

以下“部分实施”及旧版本说明保留当时事实，不代表当前完成状态。

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

## 后续续接进行中：运行时接入与固定验收

本次续接已开始接入 image catalog、按 ID active 修订与 v1 迁移、HTTP/Backend/Frontend 和 metrics v2。以下是进行中记录，不代表验收通过。

- 新生产构造器始终使用编译期 catalog；旧 v1 Manager 构造器改为私有，仅既有历史行为测试调用。没有 runtime 测试绕过环境变量。
- 制品前置沿用上节已核对 `1.10.6/linux/amd64` 原包；保留该提交的源归档，在固定 Go 1.26.0 工具链重建，Docker 构建对完整 legacy archive 的已知 digest 做硬校验。
- active.json 是每 ID 唯一提交事实；revision.json、config.json、secret.json 位于独立不可变修订目录。current 是可重建投影，不以其选择恢复版本。旧 registry 原文备份为只读文件。
- 本轮直接改变持久状态、进程归属和共享 metrics envelope，因此验证扩展到本批原定的 running/stopped 卷、两个中断点、失败回滚、完整 v1/v2 series 查询及管理授权，不扩展到其他插件或无关业务覆盖。
- 首次 Docker 构建被当前 daemon 的失效代理 `127.0.0.1:7890` 阻断了 Dockerfile frontend 镜像解析。未更改 daemon/代理配置；使用仅去掉 syntax 提示的临时 Dockerfile、内置 BuildKit frontend 和已有本地基础镜像继续；Monitor 镜像已构建成功。最终验收会记录这个环境差异。

### 实施中真实验收结果（非最终门禁）

首轮随机项目 `gopulse-p1401-9aa8dd4b3789` 已实证并捕获结果：running v1 卷迁移保留版本/时间/desired state、显式 v2 更新前后的同一 Backend 时间窗 series、已登记失败制品与错误 Secret 的 active 回滚、未登记上传拒绝、stopped v1 迁移和 v2 stopped 更新无进程。该项目已清理，既有资源完整保留。

随后空卷 connection-test 返回固定 422，验收按失败停止。最小复现读取 mount 信息确认 Docker tmpfs 默认带 `noexec`，候选入口执行返回 Permission denied。修复为 Monitor 专属、限额 160 MiB、`nosuid,nodev` 的显式 `exec` tmpfs；只有匹配编译期 catalog 的当前包能进入该路径，临时目录 0700，Secret 仍只在内存/候选环境，操作结束删除目录。此处执行权限是已观测失败所必需，不放宽上传信任或宿主目录权限。

另一个实施中单元测试发现立即退出的候选可能被固定端口的健康响应掩盖，已在 startup 对退出/进程归属做短稳定窗口确认；running 失败回滚测试随后通过。desired state 的提交也已改为先 prepare 修订，再操作进程并原子 commit，使 process record 的修订归属与启动候选一致；这些相关生产变更将纳入最终固定门禁。

### 浏览器指标目录失败及直接修复

完整浏览器闭环增加了“插件详情 → 指标页面 → 实际指标值”断言后，发现新指标目录 validator 使用了本模块不存在的 `isRecord`，导致页面捕获异常但未显示数据；已复用该模块现有 `record` 守卫，并加入直接证明新 catalog DTO 的成功/错误 producer 用例。原有 `npm run typecheck` 在 `files: []` 的 solution tsconfig 上执行 `vue-tsc --noEmit`，没有检查 app；这一实际运行时错误构成了修正固定门禁的具体依据。

因此把同一 `npm run typecheck` 命令改为显式检查 `tsconfig.app.json` 与 `tsconfig.node.json`。首次实际 app 检查报告了本批配置表单 Secret 对象与状态错误 narrowing 的类型问题，以及两个既有测试文件的断言 API 类型不兼容。只修正这些必要的类型表达/断言调用（不新增业务行为、不扩大回归用例）：`frontend/src/services/http.test.ts` 去除 matcher 的无效泛型参数，`frontend/src/views/NotificationsView.test.ts` 将存在性断言使用 find wrapper。此前仅命令退出成功不作为最终 TypeScript 验收依据，以修正后门禁结果为准。

浏览器失败诊断由聚焦脚本写入每轮证据目录的 `browser.log`，写入前替换候选 Secret 和登录密码；没有保留含 Secret 的 trace 或容器。为避免重复运行已知坏的前端镜像，已停止当前自有验收进程，由其清理逻辑删除本轮资源并确认既有资源保留；后续使用修复镜像重跑该固定门禁。

## 最终收口记录

本节覆盖上文的历史“部分实施”状态。最终冻结镜像的固定 Compose 门禁已通过，全部本批必需实现、验收和版本收口已完成；此前成功验证按受影响范围沿用，没有执行独立架构 Review、依赖审计或覆盖率活动。

### 最终交付的运行契约

1. **生产信任根**：`cmd/plugin-release-catalog` 在镜像构建时验证包并生成 Go 常量；生产 `NewManager` 始终使用它。源码直接构建的空 catalog 不授权运行任何包。上传只能选择与镜像中已登记 archive 完全一致的版本；执行的始终是镜像包经安全提取/再次 Manifest 对比后的入口，不执行上传临时文件。旧构造器 `newLegacyManager` 只保留给既有历史行为测试调用，没有生产配置项能切回旧信任路径。
2. **通用单实例状态**：六个官方 ID 都有独立 slot、操作串行化、active 修订和 observer 位置；只有 Redis 注册了实际配置 adapter/可运行制品。通用修订存储独立 `json.RawMessage`，不把 Redis 字段硬编码成持久化存储结构；`configurationAdapter` 负责 source 字段验证及环境注入，未来不需要复制事务引擎。
3. **API**：管理员 `GET /api/v1/exporter-plugins/catalog` 返回六个固定 ID、source、available、受限 Schema、configured/secret_configured、opaque revision 和固定 summary。安装/连接测试/配置替换只接受严格的 `{"config":{...},"secrets":{...}}`；configuration 省略 password 保留，connection-test 不借用已安装 Secret。保留 Redis multipart install alias，新页面使用 `/:pluginId/install`。Monitor 使用相同内部前缀语义并重新验证；Backend 先 session + DB 当前 admin 授权，并独立验证请求/上游响应。新增 `upgrade_required` 明确阻止 v1 configuration 写入。
4. **Secret 与持久化**：每 ID 的 `<root>/<id>/active.json` 是唯一提交事实，内容为 `{"revision":"<32 hex>"}`。`revisions/<revision>/revision.json` 保存 Manifest/desired/timestamps；`config.json` 保存非敏感对象；`secret.json` 单独 `0600`。安全父目录 `0700`；三个文件及目录同步后才允许 active 原子提交。Registry/current 是投影，不从它们选取已提交版本；旧 Registry 保留只读备份。未提交修订恢复时清理，未知 Registry ID 拒绝且不改写原输入，损坏旧 Registry 不取代已提交 active。
5. **生命周期**：prepare → 停止已证明归属旧进程 → 候选健康及真实 snapshot 验证 → commit。running 失败恢复旧包/config/Secret/desired；stopped 只临时试启动、不挂周期 collector，停止候选后提交。process record 记录 plugin ID、revision、PID/start ticks、实际 executable/cwd/command marker；不能只凭 PID 发信号。start/stop 重复调用幂等。
6. **迁移**：只认已核对的 `1.10.6/linux/amd64` 原始 v1 包，原 Manifest 不改成 v2。running 按原包恢复并真实采集后提交，stopped 不启动；保留版本、desired、installed/updated time。原 Phase 13 Registry 未持久化的 scrape 时间不伪造，恢复后的时间来自实际新采集。bootstrap 不隐式升级或降级已登记安装；显式 update 后才允许 v2 configuration。单元测试还验证了高于 image current 的 retained `1.11.3` stopped 状态重启/Bootstrap 保持不变。
7. **连接测试**：在 Monitor 专属受限 tmpfs 的 `0700` 临时目录提取已登记 current 包，执行 `--check`，单独 timeout/取消与进程组回收；不创建插件 ID 目录、release、process record 或候选 Secret 文件。输出/原始协议错误被丢弃，API 只返回固定 reachable 或安全错误。
8. **指标**：v1 仍保留 plugin_id/plugin_version；v2 payload 恰好为 producer_kind/producer_id/producer_version/target_id/scrape_status/samples。Router 原样转发两版 Redis 到原 Kafka Topic；Marshaller 拒绝错 producer/target/保留 labels，Redis 两版都写相同旧 source/target_id label 形状，不添加 producer labels。Backend 新 `GET /api/v1/observability/metrics/catalog` 显式返回 source/target/producer 展示目录；旧十个 families、query 参数和 Result/labels DTO 不改。其他 Exporter/组件只保留身份扩展边界，本批没有伪造它们的指标。
9. **Frontend**：六类 ID 类型与最多六项去重状态校验、六张可用性卡片、Redis Schema 表单、连接测试、安装、启停、配置替换、更新和指标目录跳转。其他五类明确未交付且不显示 running。Secret 不回填，成功/失败、切换插件和卸载时清空，不入 URL/local storage；保留 AdminLayout/普通用户隔离。

### 最终制品清单与构建环境

最终 current/acceptance Exporter 使用 Docker 构建中的 Go `1.26.0`、Linux amd64。源码是本记录所在提交的开发工作树；验收镜像在源码提交前构建，因此不把镜像当时的 base revision label 冒充最终 Git 提交 ID。legacy 源提交及重建证据见前文。

| 用途 | 版本 | 包 | archive SHA-256 |
| --- | --- | --- | --- |
| production current | `1.11.1` | `gopulse-redis-exporter.tar.gz` | `adfe8037413665a2daaecd9c574a2fe68e9e1e6b027def85cc1b8fd18205f0ef` |
| production legacy-v1 | `1.10.6` | `redis-1.10.6.tar.gz` | `b992b0dfa80a0983b9af63e4c2a4770216bfd7fcb718af2cd451281cf3306727` |
| acceptance retained failure | `1.11.2` | `redis-failure.tar.gz` | `9ca6e5ba8ac5115dc1052ce79e40b8efccfaeee48ce7c011b20bd9a54f800be1` |
| acceptance retained success | `1.11.3` | `redis-update.tar.gz` | `5729de0f6bcda9360555abd1dae468e8e6a2b060cb6e3e2f2137dfe3e6c3f52f` |

清单由 `.run/phase14-artifacts/final-packages/roster.json` 的实际提取结果核对。

- current 和成功 update 入口 digest：`ce4c4c965df569b292e7ac9d4c2ca4b6aa161755e0169aaf5fc5e6978a4cccf2`。
- failure 入口 digest：`3cb2115d921ebb3f2c984aeed925ea556c3be5627c44a8be0dca7e794202c4ab`。
- 三个 v2 Schema digest：`31f00ae09c7aa46b6a221e2ea310aa077cdda13482a2f3688ae067923e599c81`；legacy 为不适用。
- 已实际检查 production `monitor` 镜像只包含 current/legacy 两包，不含 failure/update acceptance 包。没有 runtime bypass 开关。
- 本机 Go `1.26.7 linux/amd64` 运行 Go 门禁；Node `v24.20.0`、npm `11.19.0` 运行 Frontend 门禁。有效的 v2 同工具链可复现证据沿用前文两次显式 `--contract-version 2` 的字节比对（相关 Exporter/Schema 内容未变）；当前默认制包已切至 v2，legacy 重建显式选择 v1。

### 验证结果与层次

| 固定命令 | 实际结果/证据 |
| --- | --- |
| `(cd exporters/redis && go test ./...)` | 通过，`.run/phase14-artifacts/gates/exporters-redis.log` |
| `(cd monitor && go test ./...)` | 通过，最终修订的 `.run/phase14-artifacts/gates/monitor.log`；包含事务回滚、stopped、prepare/active 两个中断点、retained 高版本恢复、未登记 legacy 不停止无归属进程、未知 Registry ID 拒绝 |
| `(cd router && go test ./...)` | 通过，`gates/router.log`；v2 bytes 原样保留、v2 不放开 Logs/Events |
| `(cd marshaller && go test ./...)` | 通过，`gates/marshaller.log`；两版 Redis 同 label、错 producer/保留 labels 拒绝且后续合法 decode 成功；保留现有 consumer 回归 |
| `(cd backend && go test ./internal/exporterplugin ./internal/metricquery ./internal/eventquery ./internal/http)` | 通过，`gates/backend.log`；严格 config/secrets、Redis 历史 labels 与额外 producer label 拒绝、管理路由回归 |
| `(cd frontend && npm test)` | 通过，17 个文件、70 项测试；`frontend-completion-gates.log` 的测试阶段。随后仅修正已被执行门禁指出的 matcher 类型参数，不改变测试运行语义 |
| `(cd frontend && npm run typecheck)` | 最终通过，显式 app/node 配置检查；`frontend-type-build-final.log`，不是此前空 solution 配置的退出成功 |
| `(cd frontend && npm run build)` | 通过，同上，90 modules；使用通过该门禁的 dist 进行真实浏览器验证 |
| `bash scripts/verify-plugin-metrics.sh --self-test` | 通过；随机 project/source 参数边界自测，不访问 Docker |
| `bash scripts/verify-plugin-metrics.sh --sources redis --migration` | 通过，退出码 0；`.run/phase14-artifacts/acceptance-frozen.log` 与 `.run/gopulse-p1401-68e7d591d6c2/results.json`、`browser.log`；包含真实指标目录跳转及值展示 |
| `python3 scripts/ci/validate_versions.py` | 通过；root、Frontend 与 `.env.example` 对齐 `1.11.1` |
| `python3 scripts/ci/validate_branch.py --branch develop/1.11.1 --base-ref upstream/main` | 通过 |
| `git diff --check` / staged diff check / `git diff --check upstream/main...HEAD` | 工作树、暂存区与 `upstream/main...HEAD` 检查均通过；提交后再次确认最终 HEAD |

实际 Compose 验收使用当前 Backend/Frontend/Router/Marshaller/Monitor acceptance，以及原 Phase 13 business-worker/search-indexer/Playwright 镜像；没有运行全量 Phase 13 浏览器套件，也没有将单元级故障注入冒称为真实 Docker 崩溃实验。真实层验证旧 running/stopped 卷、空卷、管理员浏览器、同时间窗 v1/v2、失败 update/config 回滚、未登记上传、十个 families、同 PID 目标失败/恢复、普通用户所有新增管理/catalog/query 拒绝、一条帖子/搜索、一条 Logs/Events 与多处 Secret 扫描；两个固定提交中断点由最低有效的 plugin 持久化测试实证。

### 偏差、限制与下一批边界

- 实际 Monitor Dockerfile 是现有 `deploy/docker/observability.Dockerfile`，不是计划预计路径中的独立 `monitor.Dockerfile`。复用已有 Compose/lifecycle，不新增一套编排逻辑。
- daemon 的镜像代理端点不可达，标准 Dockerfile frontend/部分基础镜像元数据解析失败；未修改 daemon、代理或用户 `.env`。Monitor/Backend/Router/Marshaller 用去掉 syntax 提示的临时 Dockerfile 和本地基础镜像构建；Frontend 使用相同 Node 主工具链构建的当前 dist，复制进已验证 `gopulse/frontend:1.10.6` 的 Nginx runtime 后接受真实浏览器验证。标准联网镜像拉取仍取决于用户环境恢复，不把这次替代构建说成远端拉取成功。
- Phase-14-01 支持的迁移来源精确限定为已登记 `1.10.6/linux/amd64` 制品；任意自制同版本包、其他历史版本/架构不自动收编。Phase 16 支持矩阵未执行，不声称 macOS/Windows 支持。
- 根 Registry/current 是兼容投影；已有 v1 只读 Registry 备份保留，未把删卷作为升级步骤。测试与验收未操作任何日常 Redis/plugin 卷。
- 两个既有 Frontend 测试文件的机械类型兼容修正仅用于恢复真实 typecheck 门禁，不含通知或 HTTP 业务功能变更。
- 上游首次自动 PR 运行（`34378209399`，提交 `b04765c`）在批次尚未完成时由 governance 单元测试失败而停止；最终完成提交 `89d141b` 再次触发后，governance 已通过，但 Monitor 的格式门禁发现 acceptance-only `failing-exporter.go` 未经 `gofmt`。已格式化该固定故障注入程序，并在后续提交重新运行 Monitor 完整门禁与上游自动 PR。
- Phase-14-02 继续扩展 `configurationAdapter`、官方 Schema/目录、构建时 release catalog、各来源 metrics family/producer 注册、collector 实例及聚焦脚本 source 分支；不得把另外五张卡片当成已交付 Exporter。自研组件指标与其他插件均留在各自计划批次。

### 本次续接实际文件清单

以下包括实现、验收、版本与同名记录；早先已提交契约文件的未改动部分不重复列为本次新增。
- `.env.example`
- `VERSION`
- `backend/internal/apperror/error.go`
- `backend/internal/exporterplugin/catalog.go`
- `backend/internal/exporterplugin/client.go`
- `backend/internal/exporterplugin/config_request.go`
- `backend/internal/exporterplugin/config_request_test.go`
- `backend/internal/exporterplugin/configuration.go`
- `backend/internal/exporterplugin/handler.go`
- `backend/internal/exporterplugin/redis_config.go`
- `backend/internal/exporterplugin/schema.go`
- `backend/internal/http/api.go`
- `backend/internal/http/response/response.go`
- `backend/internal/metricquery/handler.go`
- `backend/internal/metricquery/metricquery.go`
- `backend/internal/metricquery/metricquery_test.go`
- `deploy/compose.yaml`
- `deploy/docker/acceptance.Dockerfile`
- `deploy/docker/observability.Dockerfile`
- `deploy/plugins/README.md`
- `deploy/plugins/redis-1.10.6-source.tar.gz`
- `dev/logs/Phase-14/Phase-14-01-通用插件契约与Redis迁移闭环.md`
- `exporters/redis/README.md`
- `frontend/e2e/phase14-plugin.spec.ts`
- `frontend/package-lock.json`
- `frontend/package.json`
- `frontend/src/services/exporters.test.ts`
- `frontend/src/services/exporters.ts`
- `frontend/src/services/http.test.ts`
- `frontend/src/services/observability.test.ts`
- `frontend/src/services/observability.ts`
- `frontend/src/types/exporter.ts`
- `frontend/src/views/NotificationsView.test.ts`
- `frontend/src/views/ObservabilityExportersView.vue`
- `frontend/src/views/ObservabilityMetricsView.vue`
- `marshaller/internal/envelope/envelope.go`
- `marshaller/internal/envelope/envelope_test.go`
- `marshaller/internal/metrics/transform_test.go`
- `monitor/cmd/monitor/main.go`
- `monitor/cmd/plugin-release-catalog/main.go`
- `monitor/internal/httpserver/plugin_v2.go`
- `monitor/internal/httpserver/server.go`
- `monitor/internal/metrics/collector/collector.go`
- `monitor/internal/metrics/envelope/envelope.go`
- `monitor/internal/plugin/adapter.go`
- `monitor/internal/plugin/manager.go`
- `monitor/internal/plugin/manager_test.go`
- `monitor/internal/plugin/process.go`
- `monitor/internal/plugin/public_catalog.go`
- `monitor/internal/plugin/release.go`
- `monitor/internal/plugin/release_catalog_generated.go`
- `monitor/internal/plugin/runtime.go`
- `monitor/internal/plugin/runtime_test.go`
- `monitor/internal/plugin/storage.go`
- `monitor/internal/plugin/testdata/failing-exporter.go`
- `monitor/internal/plugin/types.go`
- `router/internal/envelope/envelope.go`
- `router/internal/envelope/envelope_test.go`
- `scripts/ci/verify_plugin_metrics.py`
- `scripts/package-redis-exporter.sh`
- `scripts/verify-plugin-metrics.sh`

- `dev/imple/Phase-14/Phase-14-01-通用插件契约与Redis迁移闭环.md`（完成状态）
- `dev/imple/Phase-14/Phase-14-总实施方案.md`（只更新第一批状态，不标记整个 Phase 完成）

### 最终结论

本批验收合同全部通过，无剩余阻断项。根/Frontend/示例版本为 `1.11.1`。最终冻结代码的真实验收中八组断言全部通过，包含有效 browser 指标目录、migration/config/update/target failure 的实际结果；故障输入与提交中断的最低有效测试均通过。最后补充的 retained 高版本恢复断言只改变测试文件，最终 Monitor 完整门禁通过，不改变已验证运行镜像。

自动提交只包含本批列出的文件；不包含 `.run`、运行 Secret、任何既有卷或用户 `.env`，本轮不自动推送。批次完成不等于 Phase 14 全阶段完成，下一批是 Phase-14-02。
