# Phase-16-01：多架构制品与发布清单闭环开发记录

## 状态

**Phase-16-01 本批完成，产品版本 `1.13.1`。尚未对外发布；不代表 Phase 16 全阶段或 macOS/Windows 实机支持完成。**

最终有效候选来自源码 revision `c794b335922c`（完整值见下方制品表），在 `127.0.0.1:15001/gopulse-verified` 完成两平台制品、真实 amd64 runtime / 完整 Compose、六插件采集、arm64 metadata-only 及同 digest 晋升。以下早期状态和失败轮次保留为过程记录，最终结果以下方收口章节为准。

## 环境与分支

- 从最新 `origin/main` / `upstream/main` `3b32c9fc7431b592e781be5a732c0bee1d28a447` 开始，基线版本 `1.12.7`。
- 本地旧 `develop/1.13.1` 指针 `b0adbf07c117369a768db8e0dab78bb22c40d01f` 无独有提交，远程无同名分支；安全快进至主线后继续，未覆盖提交。
- Linux amd64 Docker Engine 29.7.2，Compose v5.5.0，Buildx v0.36.1 / BuildKit v0.32.2；工作区位于 Linux filesystem。
- 开始时内存约 11 GiB，swap 16 GiB，磁盘可用约 58 GiB；完整 inventory 位于忽略目录 `.run/phase16-01/inventory.txt`。
- 既有容器、卷和用户未跟踪文件 `~` 保留；验证应使用独立 checkout/worktree 和强归属 Compose project。
- macOS arm64 / Windows amd64 owner、窗口及访问方式待 Phase-16-06 排期；本批不宣称宿主支持。
- `gopulse-p1601-registry` 仅绑定 `127.0.0.1:15001`；registry 2 index `sha256:a3d8aaa63ed8681a604f1dea0aa03f100d5895b6a58ace528858a7b332415373`。
- 通过 binfmt 安装 arm64 构建模拟器，仅执行构建工具及基础镜像安装步骤，不作为真实 arm64 runtime 验收。

## 已完成实现

- 构建阶段固定 BUILDPLATFORM，目标 Go binary 使用 TARGETOS/TARGETARCH，保留原 numeric UID/GID、entrypoint 和 OCI 标签。
- 六第三方 tag 全部通过远端 index metadata 核验，均含 linux/amd64 与 linux/arm64；未升级版本。精确值保存 `deploy/release/third-party.lock.json`。
- 构建基础镜像 index 锁定到 `deploy/release/build-bases.lock.json` 及 Dockerfile。
- Monitor 跨构建 inspection 显式目标架构，runtime parser/catalog 仍绑定 runtime.GOARCH；current 包使用目标架构，历史包仅 amd64 catalog 登记。
- 固定历史源码 `102aa4fa9bb5256dfd0733f42454b0ec5deaf043` 重建 Redis 1.9.4 amd64 的构建入口；实际 binary/archive digest 待构建实测。
- Linux-only lifecycle Go module 实现 version、严格 manifest 读取、只读 server architecture preflight。
- 新增 closed manifest schema、验证器、Git archive 构建、双架构 metadata/plugin ELF 检查、OS 中立 Bundle 与不重建晋升入口。
- Compose 可注入 immutable refs；candidate 验证复用既有完整 Compose gate，运行容器引用必须为候选 platform digest。
- CI 候选流程仅 trusted branch 手动触发，无 PR trigger、无 package-write permission；正式 registry 留待 Phase-16-06。

## 早期已执行检查（过程记录）

- `cd monitor && go test ./internal/plugin ./cmd/plugin-release-catalog`：通过，包括新增跨架构 build inspection 成功 / runtime 拒绝用例。
- `cd lifecycle && gofmt -w . && go test ./... && go vet ./...`：通过。
- `python3 -m unittest discover -s scripts/ci -p 'test_release_*.py'`：通过。
- `bash -n scripts/verify-release-artifacts.sh scripts/verify-compose-observability.sh`：通过。

## 初始约定与当时未完成项（过程记录）

- 嵌入 manifest 的 bundle_sha256 为排序 payload 条目摘要；最终 tar.gz SHA256 保存在 detached checksum，避免 archive 包含自身摘要的循环依赖。完整 manifest 与 assets 另由包内 checksums 覆盖。
- supported_upgrade_sources 当前为空；Redis 1.9.4 只登记升级输入，不提前声称 Phase-16-05 的 upgrade 能力。
- 九产品及 lifecycle 的双架构构建、真实 amd64 runtime/full Compose、arm64 metadata-only、全固定门禁、同 digest 晋升尚待运行。

### 构建失败轮次 1

首次 Git archive stdin 构建失败：`-f` 使用宿主绝对路径，BuildKit 在归档上下文中找不到 Dockerfile；未生成完整 manifest。修复为归档内相对路径。

### 六插件固定验收补充

为本批“六类 current 插件各真实采集目标”条件，在既有完整 Compose gate 的强归属新 project 上新增 candidate-only 用例：使用独立最小权限 MySQL/RabbitMQ collector account，六类插件经 Backend 查询 up 指标，并验证单 collector 停止不影响兄弟采集及业务。没有扩展成 Phase 14 全面回归。新增 TypeScript 静态检查通过；实际运行尚待最终 candidate。

### 验收失败轮次 2（最终状态不采用本轮 receipt）

`ad562e0` 候选的九镜像、生命周期、六第三方、六插件采集、完整业务/可观测/持久恢复均通过，但 cleanup 把 `repository:<none>`（digest-only image 显示值）当作真实 tag，输出五条 snapshot 错误；该既有函数在 cleanup 条件调用中又被 Bash errexit 语义掩盖，返回 0，外层一度生成成功 receipt 并完成本地探测晋升。发现日志错误后，明确废弃该轮的最终完成资格，不将其写作批次通过。

直接修复：snapshot tag 表仅记录真实 tag（image ID 保留检查不变）；snapshot 任一失败显式 return 1；release gate 同时拒绝非零退出或明确的 `[gopulse-compose] ERROR:`。新增回归重现“先失败、后成功被掩盖”和 digest-only 情形。晋升另外拒绝覆盖不同内容的同版本目标。旧探测 namespace `127.0.0.1:15001/gopulse` 保留作无效轮次证据；最终候选使用新的 `gopulse-verified` namespace，不覆盖旧引用。

由于修改影响本批资源保留门禁，最终候选需从修复后的同一 revision 重建并重新执行完整 fixed runtime gate；这是具体已观察验收缺陷的必要重验，不是扩大业务回归范围。

## 最终验收收口

- 最终有效 runtime project：`gopulse-accept-60f740587f5a`；其容器、网络和卷已由强归属 cleanup 清理，pre-existing resource / image ID / 真实 tag / Git status 保留检查通过，无 ERROR 输出。
- 九产品镜像均使用 manifest 中 linux/amd64 platform digest 真实运行；migrate、search-init、admin-role 继续复用 Backend。arm64 只执行 metadata / archive / ELF 检查，没有运行 arm64 产品或插件。
- 六第三方原锁定版本均已在 Linux amd64 真实启动；完整 Compose 的初始化幂等、服务重建、持久 down/up、业务与可观测恢复通过。未发生第三方版本升级。
- 六类 current 插件在同一 Monitor 启动、采集真实目标并经 Backend 查询到各自 up 指标；新增 candidate-only 用例通过（49.1 秒），验证单 collector 停止后兄弟采集继续且业务可读，随后恢复。独立 Redis Exporter 的真实目标、认证失败、恢复及 SIGTERM 门禁也通过。
- 镜像 OCI version/revision/source/title、platform/config/layer、numeric UID:GID、read-only / privilege、entrypoint/signal、无源码/Go/Node/source map/开发 Secret 边界通过。
- 跨架构包仍复用严格 runtime 校验；新增 build inspection 可接受指定 arch、runtime 拒绝异 arch 用例通过；既有 catalog 缺失、archive / entrypoint / Schema digest 拒绝测试通过。
- 生命周期工具 version / manifest 读取 / Docker server 架构预检已在真实 amd64 engine 执行；错误 server 架构由 Go 单元用例确认提前拒绝。后续仅验收脚本和记录变更未改变该 Go module、依赖和执行环境，未重复已成功的无关检查。
- Bundle allowlist、只包含普通 0644 文件、POSIX 相对路径、LF、包内 checksums、payload checksum 与 detached tar.gz checksum 全部通过；没有宿主二进制、源码、私有配置或测试业务数据。
- 新 namespace 的十个候选 index 晋升到同名 `1.13.1` tag 后逐一核对 index digest 完全一致，platform digest 因同一 index 保持一致；未重建，Bundle checksum 不变。旧失败轮次不覆盖或冒充最终版本。

### 固定门禁与实际结果

| 命令 / 门禁 | 结果 |
| --- | --- |
| `(cd lifecycle && test -z "$(gofmt -l .)" && go test -count=1 ./... && go vet ./...)` | 通过；该 module 后续未改动，沿用已成功结果 |
| `scripts/verify-release-artifacts.sh --self-test` / 等价 `python3 -m unittest discover -s scripts/ci -p 'test_release_*.py'` | 最终 5 项通过，包含 closed manifest 与真实暴露的 cleanup 错误传播回归 |
| `scripts/verify-release-artifacts.sh --manifest dist/release-manifest.json --platform linux/amd64 --runtime` | 通过；在同 revision 的干净 worktree 执行，manifest 参数为生成目录绝对路径 |
| `scripts/verify-release-artifacts.sh --manifest dist/release-manifest.json --platform linux/arm64 --metadata-only` | 通过；明确 real arm64 runtime DEFERRED 到 Phase-16-06 |
| `scripts/verify-compose.sh` | 作为上述 runtime 门禁内部固定入口执行一次最终有效全栈验收，不再另跑重复门禁 |
| `python3 -m unittest discover -s scripts/ci -p 'test_*.py'` | 41 项通过 |
| `python3 scripts/ci/validate_versions.py` | 通过，根与两 Frontend package/lock、env metadata 同为 1.13.1 |
| `python3 scripts/ci/validate_branch.py --branch develop/1.13.1 --base-ref upstream/main` | 通过 |
| `git diff --check` / `git diff --cached --check` | 实现提交通过；收口记录提交前再次按实际新 diff 检查 |
| `git diff --check upstream/main...HEAD` | 实现提交通过；收口提交后按计划再执行 |
| `python3 scripts/ci/release_artifacts.py promote --manifest dist/release-manifest.json` | 最终 verified namespace 十 index 同 digest 晋升通过 |

### 最终制品清单

源码 revision：`c794b335922c950af740c1bd394658afc00e7c1e`。本开发记录收口提交只修改日志，不改变该已验证的制品源码或 digest。

| 镜像 | index digest | linux/amd64 | linux/arm64 |
| --- | --- | --- | --- |
| backend | `sha256:90d370259a213778845ae65dd8218428adb08dccc0406809809d44a6016a3ab5` | `sha256:24660a4523b1ce2ba2bf74138ed06918e06728afe56180928dce27eff0006310` | `sha256:f94c4b8905cd3b5a7e63efe74188ebe79091644996dcf055788dea8c4d32d5a2` |
| business-worker | `sha256:d85eb31d16b6b61a6545bc32a47e729db8b4b1c2ffe9605e2823136c853f5dcc` | `sha256:de098918b0835a9816a3e2ba345a41f690ab5fa9dfbfc0b95465b11126eccdb4` | `sha256:c796c651273d3ee74e12a566ec9369ecf020ed5eb2332891b97530a68ba795f2` |
| search-indexer | `sha256:3b6d79502aa21d754efbf474efb53b6f88888568aa1e9d1d75849e9066668290` | `sha256:3753f64c40770b11057b74999553fb0e0196b2297281c6943298eb556ee7e1f6` | `sha256:d177af771f3c2bf7fb32cb5792e1fa69eb2451b3a6c0524042d3d53076d7d29d` |
| frontend | `sha256:42da42480d070f938e9f1969c7d6fed94c7963e67858ec120c4d90c266dc6a23` | `sha256:7d1a35c6198b816caddb0ed3743aee37bf6443606504fc5f4a23d1bfb49fc06e` | `sha256:a7072550849dc50da6921be0a68cfd50f6eff90eada2085f9823aadf8e62feed` |
| admin-frontend | `sha256:939e8ad6f94541f0eccb7bff35424666bfd50b529a45ed8d593a00206f306190` | `sha256:b7b48d1b24f4cd63c7c0213f31be8b0151dc7aeb7fbf9ab3e158fa2da26e16ec` | `sha256:c99057aa6b0fd9656f1a9119f936ec90633998c91a411787a8cd033ccdc1cebc` |
| router | `sha256:53fed3b3dff02cb8bb0e21f648798276d00cfbd6cd7c24913e1f59c5e200cede` | `sha256:3730f3443cdf9c3482196dd13d69dcdcdffbd7443d3d11efb95259f465d9bd3c` | `sha256:1426cb93f6967192d542a837957c3ac0e9b46cb6a2bfb6cd56fbe610beeaa149` |
| marshaller | `sha256:9e1a0944d57f1592837631f612ea8323b7609321a80bfed5c40e9fc87e548d3b` | `sha256:1c7eda22ae7b4e5384ad580703297a805320594848a577897da9fff71d18dc37` | `sha256:17a523c7e29d5a4be7d4e4d1323d6fe8213e8aa758a7d04a1a21a25da37b67fb` |
| monitor | `sha256:e03ae440d918c54ba19a226167415243c0c0b7071f5f0d66c21eb803f03dd4f2` | `sha256:afe7d582eb23d4227b7dbfa10613fc4c12d54a94971e1325d2f63a91bad1efa5` | `sha256:015ad1cccb1c68092d19b53a5e6bb2a8b1a1f8fbe22bc115ac5bba6d91026c1d` |
| redis-exporter | `sha256:dbee444e641fa185c85f5b99bb0884d5f4f55433c08b81e33e5e8e879cbc6f83` | `sha256:bea4b3e72b990f18b98ce9937b2784453df84527a95262b5ef20524a2d702929` | `sha256:b6d2ebbfcd1a0b8b7693bb0c20472a9041b1c53066aad8a7bfcf10516a96ea9f` |
| lifecycle | `sha256:39c5b2ec289ba968d956f280f6ca7d6da963fb3b5ae7013aa828c64f33486e86` | `sha256:d62c77ca90cdff95cc4d74750bf80c75b03dc94deaa75174355e32e2ea222a09` | `sha256:04ee219cd2c3bdd1e0226179f38cff032520f48c2e536a3255353069298c54e1` |

### 插件精确内容

| 插件 | version / arch / purpose | archive | entrypoint | schema |
| --- | --- | --- | --- | --- |
| redis-exporter | 1.13.1 / amd64 / current | `sha256:614b9734c68982e718b50d8cdbd3063838325075eaed0fc92d8db332ea208418` | `sha256:ce4c4c965df569b292e7ac9d4c2ca4b6aa161755e0169aaf5fc5e6978a4cccf2` | `sha256:31f00ae09c7aa46b6a221e2ea310aa077cdda13482a2f3688ae067923e599c81` |
| mysql-exporter | 1.13.1 / amd64 / current | `sha256:fd9cdd5f95b48a5aed7c8799531b54ba7a76018968c0ea996c552c85eef0907e` | `sha256:2d3cb0b9bcd664898a727e0bb5dab02713c29137ae1f900967b7a9c303db2bba` | `sha256:9999fc8409f324043bdbe77387c62866f281a18617457c1e245120f0cc7e215c` |
| rabbitmq-exporter | 1.13.1 / amd64 / current | `sha256:73a3752cd3afbf30e896ab1d711c6dd1111954b09d095da0aa4de564389147d9` | `sha256:371970ec255493c03c1f73004d9e4ce17ba7333d4c8fd016709a57b9714bedce` | `sha256:a2a8bfb775a66ae6342e42676cc7a0fa3a0e88c93b7f94281b5744ee48e5062d` |
| kafka-exporter | 1.13.1 / amd64 / current | `sha256:d5c1286fdf8725101f96d4553dd02db30a2c84f7c53b02f612463d11eed8e743` | `sha256:c76dc4c37ff3929d40366a8c2de5838fbd0e184e042812ebd6f39597ad4b8227` | `sha256:b1ab7867198f3b111fcd48df80136eab11a9c81e13a91a302027fd30a7e040bb` |
| elasticsearch-exporter | 1.13.1 / amd64 / current | `sha256:36d9cc3dd315cc05ba2e5b27b55358a8fa980494a503e5ec0d6924f06a9f6eee` | `sha256:dcc1a7b23a306cd9b644cb10efd73c034bfcc6c27f5edb8d3398e2a8af42a831` | `sha256:2c710358ff0e1294a8fa63328cdb7e3c5f7facbd3081d2e963e00fedaf498a54` |
| victoriametrics-exporter | 1.13.1 / amd64 / current | `sha256:22b67c568a557596b6b19789c252ab4ee56f65ca86c850d2f9a9b3b22d20605a` | `sha256:91648f7f22e4d315fc05165726a1c9d8f778f428a1a393030f6e357f4bd92c4a` | `sha256:203f94d02f78076222b0a3ae21b7a7e8a3f890e202b34dfd583b97d0bfeed305` |
| redis-exporter | 1.9.4 / amd64 / upgrade-only | `sha256:d0cbd15b13cb7375ffb900992a0579ef204c5051b8dc465b2463b779f8d44a89` | `sha256:20978fc780e7531d6c130caf542d7e8ba6fe0e6842ba0610825d5e716941f2fa` | `` |
| redis-exporter | 1.13.1 / arm64 / current | `sha256:a3e19b744880e2b9315f317572b16af99ec62037d55c582192b48de7dc48c731` | `sha256:1af77cd81ae22c31ff6a600a9f29962e5195517f167eb8a652d60ec01d33e03d` | `sha256:31f00ae09c7aa46b6a221e2ea310aa077cdda13482a2f3688ae067923e599c81` |
| mysql-exporter | 1.13.1 / arm64 / current | `sha256:105517e8097c5eb97534bf5224d4c284198fd83c4434ea425443898d82c28868` | `sha256:727069eb6d256532cd8b0877046af9fe8d7019cf48281caf313da6adc76fe103` | `sha256:9999fc8409f324043bdbe77387c62866f281a18617457c1e245120f0cc7e215c` |
| rabbitmq-exporter | 1.13.1 / arm64 / current | `sha256:bbf6ed217d6b54a3f871d179728d3477a75545e72eabd98f5fd150868755d881` | `sha256:3c228fdb4f4f03cbd4c06c7dbd979429f105c9656b21ec1ab55bf0da66604f8d` | `sha256:a2a8bfb775a66ae6342e42676cc7a0fa3a0e88c93b7f94281b5744ee48e5062d` |
| kafka-exporter | 1.13.1 / arm64 / current | `sha256:0a3210cbc1d50c8a74835d81c49f8e76c2d2b8a0d104a152a5ebd373107f6922` | `sha256:f46e29390ce186920ac54a9ae9b07dc0baf6e95070a674e118ccce483247baa3` | `sha256:b1ab7867198f3b111fcd48df80136eab11a9c81e13a91a302027fd30a7e040bb` |
| elasticsearch-exporter | 1.13.1 / arm64 / current | `sha256:4c06017d7cf34db9cf42156b5d6a23005c5ddf7b5886abd3d3021d7bc3821182` | `sha256:df15928588a8c736ace42b00a7ee81c61d5d0e48ca76dd639b2a8c074be66cdb` | `sha256:2c710358ff0e1294a8fa63328cdb7e3c5f7facbd3081d2e963e00fedaf498a54` |
| victoriametrics-exporter | 1.13.1 / arm64 / current | `sha256:c7582085c3d82f27d4c6ccba57a5163ba38c96044046db81ccac40e726c257d2` | `sha256:42eb6599748ce8cda13f1531fbed7de00bec1ef17a7c37f57982c7c5dd49b89b` | `sha256:203f94d02f78076222b0a3ae21b7a7e8a3f890e202b34dfd583b97d0bfeed305` |

Bundle payload：`sha256:8bcc092601b3d80cc0254c9efd0e7c665ba276c22f42075f5de72b24f0bc3e1c`。
Manifest：`sha256:953dd89776564ff5c4b019576eb59fb62c69216e45790558b432c6923214cbf2`。
归档 detached checksum：`0ff92baf33a4a6ae4b3fe0152315d96c2aa3367b133cdccf25d93ae8b525ed54  gopulse-1.13.1-bundle.tar.gz`。

第三方六镜像 index/platform digest 见 `deploy/release/third-party.lock.json`；构建基础见 `deploy/release/build-bases.lock.json`。Redis 1.9.4 legacy 来源与锁定 Go 1.26.0 工具链见 `deploy/release/README.md`；仅存在 linux/amd64，不制造 arm64 legacy。binfmt 构建辅助镜像本次实际拉取 digest 为 `sha256:400a4873b838d1b89194d982c45e5fb3cda4593fbfd7e08a02e76b03b21166f0`，不作为产品支持证据。

### 实际文件范围

- `.env.example`
- `.github/workflows/release-candidate.yml`
- `.gitignore`
- `VERSION`
- `admin-frontend/package-lock.json`
- `admin-frontend/package.json`
- `deploy/compose.yaml`
- `deploy/docker/admin-frontend.Dockerfile`
- `deploy/docker/backend.Dockerfile`
- `deploy/docker/frontend.Dockerfile`
- `deploy/docker/lifecycle.Dockerfile`
- `deploy/docker/observability.Dockerfile`
- `deploy/plugins/redis-1.9.4-source.tar.gz`
- `deploy/release/BUNDLE-README.md`
- `deploy/release/README.md`
- `deploy/release/build-bases.lock.json`
- `deploy/release/release-manifest.schema.json`
- `deploy/release/third-party.lock.json`
- `dev/logs/Phase-16/Phase-16-01-多架构制品与发布清单闭环.md`
- `frontend/e2e/compose-release-plugins.spec.ts`
- `frontend/package-lock.json`
- `frontend/package.json`
- `lifecycle/cmd/gopulse/main.go`
- `lifecycle/go.mod`
- `lifecycle/internal/release/manifest.go`
- `lifecycle/internal/release/manifest_test.go`
- `monitor/cmd/plugin-release-catalog/main.go`
- `monitor/internal/plugin/archive.go`
- `monitor/internal/plugin/manifest.go`
- `monitor/internal/plugin/release.go`
- `monitor/internal/plugin/release_test.go`
- `scripts/ci/release_artifacts.py`
- `scripts/ci/release_candidate_env.py`
- `scripts/ci/release_manifest.py`
- `scripts/ci/test_release_manifest.py`
- `scripts/ci/test_release_snapshot.py`
- `scripts/ci/verify_release_artifacts.py`
- `scripts/verify-compose-observability.sh`
- `scripts/verify-release-artifacts.sh`

### 证据保存、限制与交接

- 最终 Bundle、manifest、build metadata、两份 verification receipt 和插件包保存在忽略目录 `dist/`；原始输出保存在 `.run/phase16-01/` 的 `build-verified.txt`、`arm64-verified.txt`、`runtime-verified.txt`、`promotion-verified.txt`，不把运行证据或私有配置提交。
- 本地 `gopulse-p1601-registry` 保留在 loopback `127.0.0.1:15001` 供 Phase-16-02 按 digest 消费；仅使用 `gopulse-verified` namespace 的最终 manifest。它不是正式 registry，尚未对外发布；后续明确不再需要时可只清理该专属 registry。
- macOS arm64、Windows amd64 实机矩阵及正式 registry/授权 identity 仍由 Phase-16-06 执行；当前 arm64 证据不能替代真实采集/浏览器/生命周期支持。
- 完整生命周期、唯一 edge 收窄、双 Frontend 体验、backup、upgrade 不属于本批；未进入 Phase-16-02。`supported_upgrade_sources=[]` 保持真实能力边界。
- 已知偏差：采用单一 Bundle payload checksum + detached archive checksum 避免自引用；使用 `scripts/ci/release_artifacts.py` 作为构建/晋升入口，固定 `verify-release-artifacts.sh` 门禁名称保持不变。
- 本地验收：完成。源码推送：在本记录提交后执行，最终以 Git push 输出和远端 HEAD 核对为准。PR/远程 checks/merge：本记录未观察到，未声称完成。
- 用户原有未跟踪文件 `~` 和既有运行项目保持不动。无其他阻断项；达到本批门禁后停止，不追加审计、覆盖率或无关重构。
