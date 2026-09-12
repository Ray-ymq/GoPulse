# Phase-16-01：多架构制品与发布清单闭环开发记录

## 状态

实施中，尚未完成批次验收、尚未对外发布。源码候选提交用于固定构建 revision；最终完成状态以本记录后续实测更新为准。

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

## 当前已执行检查

- `cd monitor && go test ./internal/plugin ./cmd/plugin-release-catalog`：通过，包括新增跨架构 build inspection 成功 / runtime 拒绝用例。
- `cd lifecycle && gofmt -w . && go test ./... && go vet ./...`：通过。
- `python3 -m unittest discover -s scripts/ci -p 'test_release_*.py'`：通过。
- `bash -n scripts/verify-release-artifacts.sh scripts/verify-compose-observability.sh`：通过。

## 约定与未完成项

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
