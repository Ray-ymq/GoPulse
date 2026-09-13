# Phase-16-04：一致备份恢复与诊断闭环实施方案

> 目标版本：`1.13.4`  
> 开发分支：`develop/1.13.4`  
> 运行与验收平台：真实 Linux `amd64`

## 1. 批次目标

在稳定生命周期、唯一 edge 和双 Frontend 产品合同上，实现 Linux `amd64` 的一致加密备份、同架构空 project 恢复、恢复后继续写入和脱敏诊断：

```text
preflight
  → maintenance / write quiesce
  → authoritative logical exports
  → manifest + checksums
  → encryption + atomic publish
  → empty-project restore
  → fact verification + resumed writes
```

备份必须表达可迁移的逻辑事实，不能以复制活动 volume 或打包平台二进制替代。

## 2. 前置条件

- 从 Phase-16-03 合入后的最新 `upstream/main` 创建 `develop/1.13.4`。
- Phase-16-02 的 operation lock、ownership、state、doctor 和 lifecycle 退出码稳定。
- Phase-16-03 的身份、角色、业务和双 Frontend 事实可通过公开 API/产品入口验证。
- MySQL、Elasticsearch、VictoriaMetrics 的受支持导出/恢复接口和当前版本已核对。
- RabbitMQ/Kafka 可排空且拓扑/offset 可逻辑保存；Monitor 提供插件状态逻辑 export/import 边界。
- Linux `amd64` 上有足够空间建立源 project、加密 backup 和目标空 project。

## 3. 实施范围

### 3.1 Backup preflight 与维护窗口

- 检查安装版本、manifest、健康、operation lock、目标路径、剩余空间和写权限。
- 进入维护窗口后拒绝新业务写入，排空 Worker、Indexer、告警和插件异步工作。
- 固定各数据域 cutover 时间、offset、逻辑计数和时间范围。
- preflight 失败不得创建完成标记或留下可误用的 backup。

### 3.2 权威数据导出

- MySQL 使用事务一致逻辑导出，并记录 schema/version 与关键表计数。
- Elasticsearch 使用受支持 snapshot/export，记录索引、alias、mapping、文档计数和时间范围。
- VictoriaMetrics 使用受支持 snapshot/export，记录关键 series/query 摘要。
- RabbitMQ/Kafka 只保存可重建拓扑、offset、排空状态和必要配置，不复制活动数据目录。

### 3.3 Monitor 插件逻辑状态

- 导出目标身份、配置、授权引用、采集状态、历史摘要、插件版本意图和 catalog digest。
- 不导出运行中 PID、宿主绝对路径、临时文件或插件可执行 archive。
- 恢复时从当前 Linux `amd64` 受信 catalog 重新物化插件，校验 entrypoint、Schema 和 digest。

### 3.4 配置、Secret 与加密

- 配置按可移植 schema 导出，剔除 installation token、project id、宿主路径、端口映射和运行时临时状态。
- Secret 默认不明文进入 payload；确需恢复的 Secret 使用独立加密条目和最小 metadata。
- 口令从无回显 stdin 或显式 secret source 读取，不进入命令行、环境转储、日志或实施记录。
- 加密临时文件位于安装私有目录，成功后原子发布；中断清除本次临时文件。

### 3.5 Backup format v1

Manifest 至少包含：

- format/schema version、source product version、release manifest digest；
- 文件 allowlist、大小、逐项 checksum、加密参数和完整性标记；
- 各数据域逻辑计数、时间范围、队列/offset、插件 catalog 摘要；
- operation id、开始/结束时间和完成标记。

解析器拒绝绝对路径、`..`、symlink/hardlink 逃逸、重复路径、超限大小、checksum 错误、未知必选字段和未完成 archive。

### 3.6 空 project 恢复与失败清理

- Restore 仅允许进入空且由当前 installation token 创建的目标 project。
- 创建资源前完成解密、format、版本、manifest、checksum、空间和目标状态校验。
- 按基础设施、配置/Secret、数据库、搜索/指标、消息拓扑、插件、应用和 edge 顺序恢复。
- 失败时不发布 ready state，只删除本 operation 新建且强归属的资源；源 project 和无关资源不变。
- 恢复后核对角色、会话失效策略、业务、搜索、指标、告警、审计和插件事实，并验证新写入可继续。

### 3.7 诊断闭环

- `doctor`、backup 和 restore 失败输出稳定阶段、退出码、恢复建议和脱敏诊断路径。
- 诊断包只包含 allowlist metadata、版本、digest、健康摘要和脱敏日志，不含 Secret 或完整业务数据。
- 覆盖错误口令、tamper、空间不足、导入失败和中断代表场景。

## 4. 不在本批范围

- `1.9.4` 升级、在线热备、增量备份、任意拓扑迁移和通用灾备编排。
- macOS、Windows、`linux/arm64` 或跨架构恢复。
- 自研加密算法；必须使用成熟库和经过认证的 AEAD/KDF 组合。

## 5. 建议实施顺序

1. 锁定 format v1、数据域责任表、限制和错误码。
2. 实现 preflight、维护窗口、排空与逻辑导出。
3. 实现插件/config/Secret 导出和加密原子发布。
4. 实现安全解析、空 project 恢复和强归属失败清理。
5. 实现事实核对、继续写入和脱敏诊断。
6. 运行同架构成功、tamper、wrong-passphrase、导入失败和中断门禁。
7. 更新实施记录和 `VERSION`，提交后停止。

## 6. 预计直接影响文件

- `lifecycle/**` 中 backup/restore/doctor 命令
- 各服务的逻辑 export/import 接口及最小直接测试
- backup schema、manifest、加密和安全解析实现
- Compose acceptance fixture 与验证脚本
- 备份恢复运维文档
- `dev/logs/Phase-16/Phase-16-04-一致备份恢复与诊断闭环.md`
- `VERSION`

## 7. 批次验收标准

1. 维护窗口阻止新写入并排空异步工作，所有数据域记录一致 cutover 证据。
2. Backup format v1 内容、checksum、限制、完整性和加密可独立检查。
3. 日志、参数、环境转储、archive 和诊断包不泄露 Secret。
4. Linux `amd64` backup 恢复到同架构空 project，关键逻辑计数和历史事实一致。
5. 插件从当前 Linux `amd64` 受信 catalog 重新物化，不执行 backup 中的二进制。
6. 恢复后的系统通过 lifecycle verify、双 Frontend 检查、六插件采集和新写入。
7. tamper、错误口令、空间不足、导入失败和中断不产生半恢复 ready state，也不影响无关资源。
8. 根与受管版本更新为 `1.13.4`，同名实施记录完整且无阻断问题。

## 8. 固定验证命令与回归范围

```bash
go test ./lifecycle/...
scripts/test-backup-format.sh
scripts/verify-backup-restore.sh --platform linux/amd64 --same-arch
scripts/verify-backup-restore.sh --platform linux/amd64 --failure-matrix
scripts/verify-product-lifecycle.sh --platform linux/amd64 --reuse-install
scripts/verify-compose.sh
```

只扩展直接影响的数据域、共享安全边界和失败恢复回归；不开展一般性数据平台审计。

## 9. 实施记录与下一批交接

完成前创建 `dev/logs/Phase-16/Phase-16-04-一致备份恢复与诊断闭环.md`，记录实际数据域接口、cutover、计数/digest、加密参数边界、恢复事实、继续写入、失败清理、诊断、命令结果和偏差。

交给 Phase-16-05 的固定输入是可验证的 format v1 加密 backup、同架构恢复、强归属清理和稳定生命周期诊断合同。

