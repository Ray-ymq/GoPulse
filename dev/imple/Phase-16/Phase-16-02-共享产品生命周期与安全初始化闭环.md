# Phase-16-02：共享产品生命周期与安全初始化闭环实施方案

> 目标版本：`1.13.2`  
> 开发分支：`develop/1.13.2`  
> 运行与验收平台：真实 Linux `amd64`

## 1. 批次目标

在 Phase-16-01 的不可变 Linux 制品之上实现产品生命周期控制面，让用户只依赖 Docker Engine、Docker Compose v2 和版本化 Bundle 完成：

```text
doctor → init → up → verify → status/logs → down → up
```

生命周期必须由同一个 Linux `amd64` 工具镜像实现；现有 Bash 入口只保留开发用途，冻结的 PowerShell 历史文件不修改。

## 2. 前置条件

- 从最新 `upstream/main` 创建 `develop/1.13.2`。
- Phase-16-01 release manifest、Bundle、image digest 和 plugin catalog 可读取。
- 真实 Linux `amd64` Docker server 可运行，并记录 CPU、内存、磁盘、Docker server 和 Compose 版本。
- 临时或正式 registry 能按 digest 拉取 Phase-16-01 候选制品。
- 当前产品 Compose 的服务、profile、volume、network、healthcheck 和初始化依赖已从公开配置核对。

## 3. 实施范围

### 3.1 生命周期命令与内部边界

- 在 `lifecycle/` 的单一 Go module 中实现 `version/doctor/init/up/down/status/logs/verify`。
- 所有命令提供稳定参数、结构化输出、退出码和可操作错误，不解析人类文本判断 Docker 状态。
- 工具容器通过显式 Docker endpoint 执行 project-scoped 编排；常驻产品容器不挂载 Docker socket。
- server OS/arch 不为 `linux/amd64` 时，在创建产品资源前拒绝执行。

### 3.2 安全初始化与安装状态

- `doctor` 检查 Docker server、Compose 版本、磁盘/内存、安装目录、端口、manifest、checksum 和 digest。
- `init` 只在空安装目录创建随机 installation token、应用 Secret、配置和 state。
- 文件在同一私有目录写入临时文件、flush/sync 后原子替换；失败清除本次临时文件。
- 不在参数、环境转储、stdout、stderr、Compose 默认值或日志中输出 Secret。
- 已初始化目录的重复 `init` 必须幂等或明确拒绝，不能覆盖现有 Secret。

### 3.3 启动与唯一 edge

- `up` 先拉取并校验 manifest 指定 digest，再运行基础设施和必要初始化 job，最后启动常驻服务。
- 只有 edge 对宿主发布端口；Backend、Frontend 和基础设施只通过 project network 通信。
- 初始化 job 可重复执行且不制造重复用户、角色、索引、队列或告警状态。
- 任一依赖失败时返回稳定阶段和诊断，不把半启动写成 ready。

### 3.4 Verify、status、logs 与 doctor

- `verify` 默认严格只读，不隐式 pull、build、recreate、restart、repair 或写业务数据。
- `status` 返回安装版本、manifest digest、服务健康、唯一 edge 地址和失败阶段。
- `logs` 支持服务 allowlist、时间范围和 tail；拒绝任意容器名，并执行 Secret 脱敏。
- `doctor` 对端口占用、daemon 不可用、digest 缺失、磁盘不足和权限失败给出稳定退出码。

### 3.5 Stop、锁与强归属

- 每个变更操作获取安装级互斥锁并生成 operation id。
- stop/down、失败清理和中断处理必须校验 project、installation token、service/resource label 与 digest。
- 正常 down、重复 down、startup failure、SIGINT 和 SIGTERM 都得到稳定终态。
- 默认 down 保留数据；破坏性清理需要显式 flag 与确认，并且不能影响无关 Docker 资源或用户文件。

### 3.6 开发路径兼容

- 现有 Bash 开发入口继续通过直接受影响的 self-test/smoke，但产品文档不再把源码脚本作为安装方式。
- `scripts/*.ps1` 内容和 hash 保持不变。
- 生命周期逻辑不得复制进新的宿主脚本。

## 4. 不在本批范围

- Frontend 视觉和交互改造、backup/restore、`1.9.4` upgrade。
- macOS、Windows、`linux/arm64` 支持与验收。
- 远程 Docker host、daemon 安装、宿主 service 注册、GUI 安装器和 Kubernetes。

## 5. 建议实施顺序

1. 锁定命令、配置、state、exit code、operation 和 ownership schema。
2. 实现 `version/doctor` 及 manifest/digest/server preflight。
3. 实现安全 `init` 和原子 state。
4. 实现 `up/down`、初始化 job、唯一 edge、锁和中断。
5. 实现只读 `verify/status/logs` 与脱敏诊断。
6. 在空 Linux 交付目录完成 clean-install 和失败注入。
7. 运行固定门禁，更新实施记录和 `VERSION`，提交后停止。

## 6. 预计直接影响文件

- `lifecycle/**`
- `compose*.yml` 或产品 Bundle 中的 Compose 模板
- `config/**`、`scripts/release/**`、`scripts/verify-*.sh`
- 产品安装与运维文档
- `dev/logs/Phase-16/Phase-16-02-共享产品生命周期与安全初始化闭环.md`
- `VERSION`

实际文件以实现为准；不得为匹配清单创建无用文件。

## 7. 批次验收标准

1. Linux `amd64` 生命周期镜像来自一个 module/revision，命令参数、JSON 字段和退出码稳定。
2. `doctor` 在资源创建前拒绝错误 server arch、无效 manifest/digest、端口冲突、磁盘不足和不可写目录。
3. 从空 Bundle 目录无需源码工具即可完成 `init/up/verify/status/logs/down/up`。
4. 初始化 Secret 随机、私有、原子写入且不泄露；重复 init 不覆盖。
5. 只有 edge 发布宿主端口，所有初始化 job 幂等，完整 Compose 达到健康状态。
6. `verify` 前后 Docker 和业务快照一致；`logs` 只能访问 allowlist 服务并脱敏。
7. 并发操作、中断、失败启动和重复 down 只影响当前强归属 project。
8. 现有 Bash 开发路径的直接回归通过，冻结 PowerShell 文件 hash 不变。
9. 根与受管版本更新为 `1.13.2`，同名实施记录完整且无阻断问题。

## 8. 固定验证命令与回归范围

```bash
go test ./lifecycle/...
scripts/test-lifecycle.sh
scripts/verify-release-artifacts.sh --manifest dist/release-manifest.json --platform linux/amd64 --runtime
scripts/verify-product-lifecycle.sh --platform linux/amd64 --clean-install
scripts/verify-product-lifecycle.sh --platform linux/amd64 --failure-matrix
scripts/verify-compose.sh
```

脚本名称可在实现时按仓库结构等价调整，并在实施记录中写明。最终门禁只运行一次；只有相关代码、配置、依赖或环境变化时才重跑成功项。

## 9. 实施记录与下一批交接

完成前创建 `dev/logs/Phase-16/Phase-16-02-共享产品生命周期与安全初始化闭环.md`，记录实际命令、配置/state schema、Secret 权限、Linux host/server、唯一 edge、锁、signal、清理、验证结果、失败轮次和偏差。

交给 Phase-16-03 的固定输入是可从 Bundle 调用的稳定生命周期、唯一 edge、安装 state、API origin 和只读验证合同。

