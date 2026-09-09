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
