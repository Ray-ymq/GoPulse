# Phase-16-01：多架构制品与发布清单闭环实施方案

> 执行序号：1 / 6。目标版本`1.13.1`、开发分支`develop/1.13.1`及后续批次顺序以[Phase-16-总实施方案.md](Phase-16-总实施方案.md)的权威分配表为准。

## 1. 批次目标

从Phase 15只能在Linux amd64本地构建/运行的镜像与插件基线出发，建立后续三个宿主实际消费的不可变交付事实：

```text
同一主线revision
  → 9个产品镜像
       ├─ linux/amd64 platform digest
       └─ linux/arm64 platform digest
  → 6类current插件包 × 2架构
  → 双架构Linux生命周期工具镜像
  → 单一OS中立Bundle + release manifest + checksums
  → candidate按digest验证，正式版本不重建晋升
```

本批只解决“交付什么、如何证明内容与平台”的问题，不提前实现完整生命周期、Frontend产品体验、backup或upgrade。

## 2. 前置条件

- Phase 15 Review整改已合入最新`upstream/main`，根完成版本是`1.12.7`；fetch后从该最新主线创建本批分支。
- 本地已有`develop/1.13.1`时，按总方案§3.3核对独有提交和远程；若远程不存在且本地无独有提交，记录旧指针后将分支安全快进/重建到最新`upstream/main`并继续，不将其作为开工阻断。
- 按总方案§3记录当前可访问Linux amd64 Docker server的CPU、内存、磁盘、Compose和工作区事实；macOS arm64、Windows amd64的owner/窗口/访问方式可保持待排期，不阻止本批开工或源码分支push。
- 只读核对当前9个逻辑产品镜像、3个复用Backend镜像的operation job、6个第三方基础镜像、六插件release catalog和Phase 15镜像验证清单。
- 本地未配置外部OCI registry认证时，在当前Docker engine上使用临时loopback OCI registry执行candidate push/pull-by-digest和同digest不重建晋升探测。正式registry命名空间与授权CI/release identity留给Phase-16-06前收口；不要求在聊天、日志或命令行提供密钥。
- 核对当前第三方镜像锁定版本是否同时提供`linux/amd64`与`linux/arm64`；缺失时先记录精确image/platform结果，再选择最小兼容版本。

## 3. 实施范围

### 3.1 双架构Docker构建

- 将Backend、Business Worker、Search Indexer、用户Frontend、管理Frontend、Router、Marshaller、Monitor、独立Redis Exporter的Dockerfile/build target统一接入BuildKit`TARGETOS/TARGETARCH`，清除隐含amd64路径和包名。
- 两个平台均从同一Git revision和锁定基础镜像构建，保留numeric UID:GID、只读根、固定entrypoint、OCI version/revision/source/title和无源码/toolchain runtime边界。
- 为每个逻辑镜像生成一个index digest及两个platform digest。本批在真实Linux amd64 engine拉取并运行对应platform，对arm64核对index、config、layer和OCI metadata；真实arm64 engine运行由Phase-16-06完成，不用本批metadata冒充平台支持。
- `migrate`、`search-init`、`admin-role`继续复用Backend镜像，不增加同内容operation镜像。
- acceptance image保持测试工具身份；仅在三宿主浏览器runner直接需要时补齐目标架构，不将其列为第10个产品镜像或加入日常Compose。

### 3.2 第三方镜像与Compose引用

- MySQL、Redis、RabbitMQ、Kafka、Elasticsearch、VictoriaMetrics按版本和platform digest锁定，release manifest保存每个平台实际digest。
- 如当前锁定tag缺少arm64，只允许升级到最小兼容版本；运行直接涉及的数据目录、healthcheck、协议、初始化job和代表性持久重启回归，并在实施记录说明原因。
- 产品Compose接受manifest解析出的immutable image ref；现有开发Compose仍可使用本地tag/build，但两种模式不得通过可变tag混用。
- product assets不发布Backend宿主端口的最终收窄由Phase-16-02完成；本批只保证Compose结构可由后续生命周期注入digest。

### 3.3 六插件双架构release catalog

- Redis、MySQL、RabbitMQ、Kafka、Elasticsearch、VictoriaMetrics的current v2 entrypoint分别构建`linux/amd64`和`linux/arm64`包，Manifest精确声明`os=linux`及实际arch。
- archive、entrypoint、config Schema digest进入对应Monitor platform catalog；同ID/version/arch只有一组内容，错arch、缺catalog或digest不同安全失败。
- Monitor amd64/arm64镜像只把当前arch可执行包用于运行；不得依靠QEMU执行另一架构插件，也不得从可写volume建立catalog信任。
- 从`102aa4f...`锁定源码重建`1.9.4/linux/amd64`Redis v1 archive，记录工具链和精确digest，登记为仅Phase-16-05升级使用的legacy输入；不制造`1.9.4/linux/arm64`。
- production Monitor不包含acceptance-only失败包；必要失败包只进入隔离target并使用同一校验实现。

### 3.4 Release manifest与交付包骨架

- 新增严格`release-manifest.json` schema，固定product version、source revision、Compose asset digest、生命周期工具镜像index/platform digest、Bundle checksum、逻辑镜像index/platform digest、第三方digest、插件catalog和supported-upgrade-source字段。
- manifest拒绝未知必需字段、重复identity、可变`latest`、无digest image、平台集合不完整、版本/revision不一致和同ID/version/arch多内容。
- 从同一`lifecycle/`Go module构建`linux/amd64`、`linux/arm64`生命周期工具镜像；本批只要求`version`/manifest读取和错误server架构早期拒绝，完整命令由下一批实现。
- 生成一个版本化、OS中立Bundle，包含相同Compose、manifest和文档；归档路径、行尾与权限在Linux/macOS/Windows可安全解压。不新增Darwin/Windows二进制、原生启动器或平台专用源码。
- bundle不包含源码、`.git`、Node modules、构建cache、开发固定Secret、registry credential、source map、私有绝对路径或测试业务数据。

### 3.5 候选发布与不重建晋升

- 受信主线/开发分支构建candidate image index和单一OS中立Bundle，记录完整digest/checksum；普通未受信pull request不获得package写权限。
- 本批Linux runtime smoke和后续批次只消费candidate digest，不以本地同名tag通过。临时registry只用于可重复的制品门禁，不宣称已完成对外发布。
- 候选version/tag在临时registry中引用经验证的同一index/platform digest和bundle checksum，不重新运行产生不同内容的build；正式registry将在Phase-16-06以同一规则重放并按digest验收。
- 发布失败或部分platform失败不生成完整manifest、不覆盖上一个版本、不把单arch tag标记为本批完成。

## 4. 不在本批范围

- `init/up/down/status/logs/verify/doctor`完整生命周期和资源ownership实现。
- 唯一edge端口收敛、双Frontend样式/状态/路由体验修改。
- backup format、恢复、`1.9.4`数据migration或升级事务。
- 原生Windows/macOS服务、宿主CLI/平台adapter、Kubernetes、镜像签名/SBOM体系、CVE审计或所有第三方镜像版本更新。
- 仅为提高缓存命中率、镜像体积或构建速度进行无验收依据的Dockerfile重构。

## 5. 建议实施顺序

1. 记录当前Linux amd64真实环境、两个待排期宿主占位、临时loopback registry probe、当前镜像/target/plugin清单和第三方platform metadata，冻结直接输入。
2. 建立release manifest schema及验证器，使缺平台、可变引用、metadata不一致先失败。
3. 按Backend、Frontend、observability三组修正Docker构建并产生双架构candidate index。
4. 构建六插件双架构包和`1.9.4/linux/amd64`Redis legacy包，核对catalog/digest/错架构拒绝。
5. 建立同源码双架构生命周期工具镜像和单一Bundle，扫描内容、路径、line ending、checksum和错误server架构行为。
6. 在Linux amd64 engine从临时registry按digest拉取并运行最小runtime smoke，对arm64执行OCI metadata/配置验证；运行Linux当前完整Compose必要回归。
7. 更新版本、manifest、本批实施记录与支持矩阵初始文档，在最终diff运行固定门禁并提交。

## 6. 预计直接影响文件

- `deploy/docker/*.Dockerfile`及直接构建helper
- `deploy/compose.yaml`与新增`deploy/product/`Compose/release资产
- `monitor/internal/plugin/**`、六Exporter package脚本/Manifest/catalog直接文件
- 新`lifecycle/`Go module中的version/manifest最小骨架及工具镜像构建
- 新`release/`manifest schema、bundle清单和构建配置
- `.github/workflows/`中的受信candidate/release构建工作流
- 新`scripts/verify-release-artifacts.sh`及`scripts/ci/`manifest/artifact验证器与直接测试
- `docs/platform-support.md`、必要发布说明、本批同名实施记录
- `VERSION`、两个Frontend package/lockfile和全部受管version metadata

预计文件是允许边界，不要求制造无意义修改；若registry或构建实现采用不同现有目录，实施记录需写明实际路径和原因。

## 7. 批次验收标准

### 7.1 产品镜像

- 9个逻辑产品镜像各有唯一index digest、amd64 digest和arm64 digest；真实Linux amd64 engine按digest运行后container arch与manifest匹配，arm64 index/config/layer/OCI metadata完整且与同一revision一致。
- version/revision/source/title、numeric user、只读根、entrypoint、health和无源码/toolchain/Secret扫描通过；两个平台来自同一提交。
- 6个第三方镜像的两个platform digest锁定，并在Linux amd64真实启动必要服务；若变更版本，直接数据/协议/health回归通过。arm64真实启动是Phase-16-06支持门禁。
- 开发本地tag不能替代candidate digest，临时registry中候选引用晋升前后digest完全一致；正式registry晋升结果由Phase-16-06记录。

### 7.2 六插件

- 每类current插件在amd64真实启动并采集一个目标，经Monitor到Backend查询链路至少对代表源闭环；arm64包通过archive/entrypoint/Schema digest、架构标识与catalog选择门禁，真实arm64采集由Phase-16-06完成。
- 错架构、缺失catalog、archive/entrypoint digest错各有代表性安全失败；一个插件失败不影响其他插件和业务。
- `1.9.4/linux/amd64`Redis包来源、工具链和digest可重复核对，只登记legacy升级用途；production arm64不会执行该包。

### 7.3 Bundle与发布

- 生命周期工具镜像的amd64/arm64变体来自同一module/revision，`version --json`与manifest/digest一致，错误server架构在创建产品资源前失败。
- 单一Bundle的Compose/manifest/checksum、归档路径、LF规则和内容allowlist通过，且包内无Darwin/Windows宿主二进制；Windows实机解压后的Compose可读性由Phase-16-06验收。
- manifest验证器拒绝缺platform、重复identity、mutable ref、错version/revision/checksum和未知required section。
- 临时loopback OCI registry中的candidate push/pull、按digest拉取和同digest晋升有真实记录；本地无外部registry credential不影响本批完成，但实施记录必须明确“尚未对外发布”。

### 7.4 完成条件

上述本批标准及固定门禁全部通过，无Linux制品阻断项；根与全部受管版本为`1.13.1`，分支为`develop/1.13.1`，同名实施记录只写真实结果与待Phase-16-06执行的arm64/正式registry项。达到条件后提交并停止，不宣称macOS/Windows支持，也不进入Phase-16-02生命周期实现。

## 8. 固定验证命令与回归范围

本批最终diff至少运行当时已落地的等价入口：

```bash
(cd lifecycle && test -z "$(gofmt -l .)" && go test -count=1 ./... && go vet ./...)
scripts/verify-release-artifacts.sh --self-test
scripts/verify-release-artifacts.sh --manifest dist/release-manifest.json --platform linux/amd64 --runtime
scripts/verify-release-artifacts.sh --manifest dist/release-manifest.json --platform linux/arm64 --metadata-only
scripts/verify-compose.sh
python3 -m unittest discover -s scripts/ci -p 'test_*.py'
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/1.13.1 --base-ref upstream/main
git diff --check
git diff --cached --check
```

- `linux/amd64 --runtime`在当前真实Linux amd64 Docker server运行；`linux/arm64 --metadata-only`只验证本批制品结构，必须在输出和实施记录中标为deferred，不得写成真实arm64 runtime通过。
- `scripts/verify-compose.sh`在Linux amd64最终diff运行一次，覆盖所有Dockerfile/第三方镜像变化的现有业务、可观测和强归属回归。
- 如只变manifest验证器，不重复无变化Frontend unit；如第三方版本变化，只扩展受影响service的持久/协议检查并记录原因。
- 提交后补充`git diff --check upstream/main...HEAD`，push/checks/merge状态分开记录。

## 9. 实施记录与下一批交接

完成前创建`dev/logs/Phase-16/Phase-16-01-多架构制品与发布清单闭环.md`，至少记录：

- Linux环境inventory、macOS/Windows待排期状态、临时registry probe、BuildKit/runner、第三方image platform结果。
- 9个image index/platform digest、6插件archive/entrypoint/Schema digest、生命周期工具镜像digest与Bundle checksum。
- 真实amd64 runtime smoke、arm64 metadata-only deferred记录与Linux Compose结果、所有失败轮次和最小修复。
- bundle内容扫描、临时registry候选晋升同digest证据、正式registry deferred项、偏差、限制和未完成项。

交给Phase-16-02的固定输入是可按digest消费的manifest/Compose、双架构生命周期工具镜像骨架、单一OS中立Bundle、双架构产品/插件制品及已确认的Docker/Compose共同能力；本批不宣称完整产品生命周期或三宿主支持已完成。
