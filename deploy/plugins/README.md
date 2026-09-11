# Redis release inputs

`redis-1.10.6-source.tar.gz` is a source-only `git archive` of
`exporters/redis` at `f1276484944eb933d2ebb40dc7c960a6a414b5b7`.
It contains no binary, runtime volume, configuration instance or credential.
The Monitor Dockerfile reproduces the historical Linux amd64 binary with
Go 1.26.0, `CGO_ENABLED=0`, `-trimpath -buildvcs=false -ldflags='-s -w -buildid='`,
and explicitly packages Manifest v1. The build rejects any mismatch against:

- archive SHA-256: `b992b0dfa80a0983b9af63e4c2a4770216bfd7fcb718af2cd451281cf3306727`
- executable SHA-256: `20978fc780e7531d6c130caf542d7e8ba6fe0e6842ba0610825d5e716941f2fa`
- schema digest: not applicable (v1).

The production `monitor` target compiles one current v2 package and this exact
legacy package into its release catalog. It does not silently upgrade migrated
state; an explicit administrator update selects the registered current package.
Unknown historical versions/content fail closed and retain their volume.
This batch establishes Linux amd64 support only, not the Phase 16 platform matrix.

The separate `monitor-acceptance` target also registers a deterministic failing
v2 `1.11.2` package and successful v2 `1.11.3` package. It uses the same verifier
and transaction code. The production target contains neither acceptance package,
and there is no runtime trust-bypass option.

Build the impacted images with version `1.11.1` before running
`scripts/verify-plugin-metrics.sh --sources redis --migration`. The focused runner
uses the proven `gopulse/monitor:1.10.6` and business/Playwright acceptance baseline
images plus the current `backend`, `frontend`, `router`, `marshaller` and
`monitor-acceptance` images. It always creates and removes its own random Compose
project/volumes and never mounts the daily product volume.

## Phase-14-02：MySQL / RabbitMQ 专用账号与 Secret

两种 Manifest v2 包随 Monitor 镜像固定嵌入；空插件卷由管理员安装，不借用业务/root 账号采集。
账号部署与应用启动解耦：账号未就绪只阻断对应插件，不进入 Backend/业务 readiness。
新卷与 Phase 13 既有业务卷均调用同一个幂等入口，不依赖 MySQL 空卷初始化目录：

```bash
# 先按既有 scripts/dev.sh 启动目标 Compose 项目。
# 在私有目录创建 0600 的 admin.json，内容为：
# {"mysql_root_password":"<deployment-secret>","mysql_database":"gopulse",
#  "rabbitmq_username":"<deployment-admin>","rabbitmq_password":"<deployment-secret>"}
bash scripts/reconcile-plugin-accounts.sh \
  --project-name gopulse --admin-file /private/path/admin.json
# 可独立重试：--sources mysql 或 --sources rabbitmq
```

此显式账号部署步骤在维护的 WSL2/Linux 环境需要 Python 3 标准库和 Docker CLI；
不改变日常 Bash/Compose 启停对 Go/Node/Python 的依赖，不扩展历史 PowerShell 实现。
管理员文件由操作者提供，只读入部署进程内存；MYSQL root/RabbitMQ 管理员凭据从不发布给 Monitor。
Monitor 的既有 API token 留在容器内。`docker exec` stdin 传递候选数据，不在命令行打印凭据。

- 固定专用用户名 `gopulse_metrics`；MySQL 仅 USAGE，RabbitMQ 仅 monitoring 和 `/` 三项 `^$`。
- 默认 `.run/plugin-accounts/<project>/` 为 0700；每类型状态文件为 0600，包含归属和候选 Secret。
  请按 Secret 备份、限制访问；不可提交 Git。可通过 `--state-dir` 指定受控持久路径。
- 同名非归属账号冲突安全失败，不接管、不增权、不换业务密码、不清卷。
- 先保存候选，再建立/校验账号，通过完整 connection-test 后才调用 Monitor install；
  成功后原子标记激活。中断时复用同一候选；若安装已成功但标记未写入，须核对已安装的私有配置/Secret。
- 旧卷重复运行不改密码或 grants，也不启动管理员已停止的插件。
- 一个类型失败仍尝试另一个类型，最后非零退出；只输出安全结果。
- 轮换、超出合同的权限修复或非归属账号迁移不自动执行，应使用显式部署流程。

### 传输及存储身份

MySQL/RabbitMQ 使用 envelope v2。Monitor 与 Marshaller 分别验证完整 family/kind/label/sample count，
Router 只放行固定 source。存储 label 为 `source`、`target_id=<source>-exporter-local`、
`producer_kind=exporter_plugin`、`producer_id=<source>-exporter` 加固定业务枚举 label。
不存 producer version label。Redis 历史存储身份保持不变。

```bash
bash scripts/package-redis-exporter.sh --source mysql --version 1.11.2
bash scripts/package-redis-exporter.sh --source rabbitmq --version 1.11.2
bash scripts/verify-plugin-metrics.sh --sources mysql,rabbitmq
```

完整逐字段目录见 `exporters/mysql/README.md`、`exporters/rabbitmq/README.md`。

## Kafka / Elasticsearch packages

The same packager accepts `--source kafka` and `--source elasticsearch`; both use
Manifest v2 only and the shared compile-time release catalog. The production
Monitor embeds both current packages. `monitor-acceptance` alone embeds trusted
`1.11.4` success and `1.11.90` failure packages for their transactional update gate.
No acceptance package is a new completed product version.

Kafka uses the fixed PLAINTEXT `kafka:19092` origin without Secret fields.
Elasticsearch uses `elasticsearch:9200` with optional paired username/password;
the standard Compose target does not require account reconciliation. The
security-enabled acceptance fixture uses only monitor privileges, never a
business/admin credential in the Exporter. Per-source details are in each
Exporter README and the Phase-14-03 record.

### VictoriaMetrics / six-source artifacts

`--source victoriametrics` builds the sixth reproducible Manifest v2 package.
The production Monitor embeds current six-source packages and no failure fixtures.
The acceptance target alone trusts VictoriaMetrics `1.11.90` (failure) and
`1.11.5` (successful upgrade) packages; an unregistered self-consistent archive is
still rejected before execution. Packaging does not grant arbitrary source,
origin, metrics-family or credential permissions. The exact nine-family mapping
is in `exporters/victoriametrics/README.md`.
