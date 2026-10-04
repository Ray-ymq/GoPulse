# Backup format v1：格式层实施说明

> **状态：Phase-16-04 部分实现，尚非可交付备份恢复能力。**
> 当前实现只处理受限大小的加密逻辑载荷及离线检查。
> 维护窗口、各数据域导出/导入、目标归属清理和真实产品恢复尚未接入。
> 不可把离线 `passed` 当作一致 cutover、恢复成功或产品 ready 的证据。

## 已实现的边界

实现位于 `lifecycle/internal/backup/`，仅使用 Go 标准库密码学实现。

- 最外层：8 字节 `GPBACK01`、32 字节随机 salt、GCM 随机 nonce 与密文/tag。
- KDF 固定为 PBKDF2-HMAC-SHA256、600000 次迭代、32 字节密钥。
- AEAD 为 AES-256-GCM；`cipher.NewGCMWithRandomNonce` 生成 nonce。
- 整个固定头部作为 AAD 认证。格式 v1 不接受来自文件的可变 KDF 工作量。
- 每次加密使用新的随机 salt；`secrets.enc` 是独立加密条目，以 `GPSECR01`
  作为类型标记/AAD，不能与最外层 envelope 互换。
- 密码错误和认证篡改返回相同安全错误，不输出原始数据或底层错误。

这定义的是标准算法之上的文件封装，不是自研加密算法。

### 大小与内存限制

当前为有界、内存型实现：解密后的完整 USTAR archive 最大 256 MiB，manifest
最大 1 MiB，逻辑条目总量预留 manifest 和 tar 头部空间。加密文件还包含固定头部、
nonce 和 tag。读取时先检查文件类型、权限、大小，并再次限制实际读取长度。

该限制是当前实现的硬边界，不是大规模生产数据量支持声明。导出层必须在维护
窗口之前做空间/数据量评估；不得截断导出后继续打完成标记。后续如需流式分块格式，
必须另行明确协议和兼容性，不能静默改变 v1 的加密含义。

## 明文载荷结构

解密后的 archive 仅接受规范 USTAR 常规文件，顺序如下：

```text
manifest.json
config.json
elasticsearch.json
kafka.json
mysql.sql
plugins.json
rabbitmq.json
secrets.enc
victoriametrics.native
```

必须包含全部条目，无目录、可执行插件 archive、额外文件、符号链接、硬链接、
PAX/GNU 扩展。必须有完整的双零块结束标记，不接受截断、拼接或尾随数据。
文件名使用精确 allowlist，不能包含绝对路径或 `..`。

Manifest 定义见 Go 类型 `backup.Manifest`：

- format/schema 均为 1，平台 `linux/amd64`；
- source product version、release manifest SHA256、operation id；
- 开始/结束时间、显式 complete、固定 AEAD/KDF 参数；
- 逐文件路径、大小、SHA256；
- MySQL、Elasticsearch、VictoriaMetrics、RabbitMQ、Kafka、plugins 六个域；
- 每域逻辑计数、数据时间范围、相同 cutover、offset、排空和 catalog 摘要。

解析器拒绝未知 JSON 字段、重复键、超深 JSON、重复文件、超限声明、checksum
错误及 incomplete。RabbitMQ/Kafka 必须声明 drained；Kafka 必须包含 offset map；
插件域必须包含 catalog digest。空数据域可用零计数和 cutover 时间表达空时间范围。

**这些是结构与声明检查，不是事实证明。** 当前未解析各业务条目的内部 schema，
未验证计数与实际业务数据相等，也未执行 SQL、搜索或指标导入。后续导出适配器必须
负责可移植 schema、installation token/project/宿主路径剔除、业务数据一致性和 Secret
分类；恢复适配器必须先验证版本兼容性、当前 manifest/catalog、目标状态和空间。

## 口令来源与离线检查

新增命令不访问 Docker，也不要求安装目录或 release Bundle：

```bash
cd lifecycle
go run ./cmd/gopulse backup-inspect \
  --archive /private/install/snapshot.gpb \
  --passphrase-file /private/backup-passphrase
```

口令只从显式选择的私有常规文件读取（例如权限 0600），长度 16–4096 字节。
不支持将口令作为命令行值或环境变量；**按原始字节读取，不去除末尾换行**。
由操作员通过可信 Secret 工具创建文件，不在命令参数、日志或实施记录中写真实口令。
命令也要求 archive 为私有常规文件，拒绝最终路径为 symlink 的源文件。

成功输出固定 allowlist JSON：格式、平台、版本、release digest、operation、时间、
文件/域数量和算法，不输出配置、Secret、原始业务计数键、文件路径或载荷。
输出 `scope` 明确限定为格式认证，不证明恢复可用。

| 退出码 | 阶段 | 含义 |
| --- | --- | --- |
| 2 | `backup-arguments` | 参数不符合离线检查合同 |
| 14 | `backup-secret-source` | 私有 Secret source 不可安全读取 |
| 21 | `backup-read` | archive 权限、类型、大小或读取错误 |
| 21 | `backup-authenticate` | 口令、认证或格式检查失败 |
| 21 | `backup-secrets` | 独立 Secret envelope 不合法 |

本命令是源码工具入口；当前没有构建或验证包含它的新产品 Bundle。
`backup`/`restore` 命令及 doctor 诊断包仍未实现，不提供占位成功响应。

## 发布 API

`Publish(ctx, installationDir, basename, ciphertext)`：

1. 要求私有真实目录，使用 `os.Root` 锚定已检查的目录 inode。
2. 不接受路径逃逸，检查目标不存在和可用空间（密文大小加 16 MiB 保留空间）。
3. 在同一私有目录创建随机 `.backup-pending-*` 文件，权限 0600。
4. 分块写入并检查 context，文件 fsync 后以不覆盖已有目标的原子硬链接发布，
   删除临时名字并同步目录。这里的本地密文发布链接不属于 archive 条目。
5. 可处理的写入失败或 context 取消清除此调用的临时文件。SIGKILL/断电无法运行
   defer，可能遗留私有临时密文；尚未实现 lifecycle 启动后的强归属残留回收。

调用者必须持有 operation lock，并传入 `Seal` 成功返回的完整密文。
当前 API 尚未被产品 backup 调用，也未实现备份期间源服务恢复流程。

## 验证与未完成范围

```bash
scripts/test-backup-format.sh
(cd lifecycle && go test ./...)
```

测试直接覆盖认证 round trip、Secret 独立加密、错误口令、tamper、危险 archive、
计数/cutover 结构约束、私有文件、原子不覆盖、已取消 context 清理和离线输出脱敏。
这是格式层测试，不是计划要求的真实 Linux Compose 成功/失败矩阵。

整批仍须实现和验证：权威数据导出、写入静默及排空证据、插件受信 catalog 重新物化、
空 project 恢复顺序、按 operation 强归属清理、角色/会话/审计/历史事实和新写入、
双 Frontend/六插件回归、空间不足/导入失败/运行中中断与诊断包。
