# Phase-16-02 实施记录

## 状态

目标 `1.13.2`，分支 `develop/1.13.2`。当前为候选实现快照，**尚未完成批次验收**；本文件将在真实运行后追加最终结果。提前提交是现有 release builder 的 committed-source 输入要求，不代表验收完成。

## 已实施

- 单一 lifecycle Go module 新增 doctor/init/up/down/status/logs/verify JSON schema 1 控制面。
- 显式 Unix Docker endpoint、Linux amd64 preflight、Docker >=24 / Compose >=2.24、资源检查、Bundle checksum / digest 检查。
- 0700 安装目录、0600 Secret/state、同目录 sync+rename、安装级 flock、随机 operation / installation token；重复 init 拒绝覆盖。
- 候选 Bundle 独立 edge、project-scoped volume/network、manifest/installation/service 标签；变更和日志操作校验归属，默认保留数据。
- 顺序基础设施/初始化 job/常驻服务；严格只读 verify、allowlist logs 与 Secret 脱敏；SIGINT/SIGTERM 终止整个 CLI process group 并保留诊断状态。
- 产品文档使用同一工具容器；未扩展 PowerShell；现有开发 Compose 保持不变。

## 已运行检查

- `(cd lifecycle && go test ./...)`：通过（实施中最小包检查）。
- `scripts/test-lifecycle.sh`：通过，包含 Go module 测试和 Compose Bash self-test。
- `scripts/verify-release-artifacts.sh --self-test`：5 项通过。
- `python3 -m py_compile scripts/ci/verify_product_lifecycle.py`：通过。

## 环境与边界

- Linux amd64，4 CPU，内存约 11 GiB，安装文件系统空闲约 53 GiB。
- Docker Engine 29.7.2、Compose v5.5.0。
- 原 Phase-16-01 loopback registry 已启动用于读取历史候选，不是正式发布。
- 用户未跟踪文件 `~` 和其他既有 Docker 项目不纳入提交/清理。
- 当前 release schema 保留 Phase-16-01 两架构元数据合同；本批产品运行验收只针对 Linux amd64，不宣称 arm64 产品支持。

## 待完成

同 revision 候选构建、真实 clean-install / failure matrix、release runtime / 直接 Compose 回归、PowerShell hash 对比和最终结果记录。

## 实施中的失败与调整

- 首轮候选 `c3b7f706fce1` 构建成功，输出 `dist/phase16-02/`；首次 clean-install 在 doctor 的 VictoriaMetrics digest 检查失败，尚未创建产品资源。
- 原因：`docker manifest inspect` 在工具客户端直接访问 Docker Hub，没有复用 daemon 的 registry mirror，实际返回 registry EOF。使用同一 endpoint 的 Engine distribution API 验证成功，因此改为 daemon-side 结构化 descriptor/platform 查询；不退回 tag、不跳过 digest、不传播 raw registry 诊断。
- 新增直接对应验收的错误 server arch、失败 pull 非 ready 状态测试；failure matrix 增加 Bundle 篡改和真实低容量 tmpfs。

- Engine distribution API 的实际单平台响应 `Platforms=null`，仅 index 提供平台列表；据此保留 index 平台校验并按 descriptor digest 验证 child，增加真实 Unix HTTP 边界回归。
- `ceff812` 候选已真实完成 init/up，全部常驻容器健康；验证 runner 的业务快照错误引用不存在的 `user_roles` 表，改为实际 `bootstrap_super_admin`。该轮未被记为 clean-install 通过；finally 的强归属清理完成。
- Bundle 新增根 `compose.yaml` 作为正式工具入口，纳入 payload / archive checksums；clean-install 改为直接通过交付 Bundle 的 Compose 调用，failure matrix 保留同镜像 Docker run 用于隔离故障注入。旧 Phase-16-01 Bundle 校验保留兼容。
