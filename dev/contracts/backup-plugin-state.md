# Monitor 离线插件状态传输（Phase-16-04 子项）

> 这是产品备份恢复的内部组件，**不是可独立使用的 backup/restore 命令**。
> 该接口已接入 lifecycle 维护窗口、加密归档和空项目恢复；完整产品验收结果以同名实施记录为准。
> 本接口不声明 `1.9.4` 升级支持。

## 容器内接口

Monitor 产品镜像中的同一可执行文件提供：

```text
monitor plugin-state export --root /var/lib/gopulse-monitor/plugins
monitor plugin-state import --root /var/lib/gopulse-monitor/plugins
```

- 仅用于受维护的 Linux amd64 Compose 产品。导出前必须停止 Monitor；恢复进入新的空插件 volume。
- 导出的 stdout / 导入的 stdin 必须是 pipe，不能是终端或普通文件。不得使用 TTY、后台容器日志、`tee`、shell tracing 或打印 transport。
- stdout 是含 Secret 的**内部传输通道**，不是诊断输出。调用方必须捕获它，在内存中拆分 public/private，将 private 用格式层的独立 Secret 加密条目封装，之后才能发布备份。生命周期已接入此步骤；不可直接保存这个 transport 当作备份。
- JSON transport 仅包含 `public` 和 `private`，总量限制为 1 MiB。失败 stderr 为固定脱敏信息；失败退出码为 1，非 pipe 使用为 2。
- 不新增 HTTP 管理端点，避免把批量凭据导出暴露给现有管理 API。

## 数据合同

`public` 的 schema 为 1，包含受信 catalog 的 SHA-256 摘要，以及每个已提交插件的：

- ID、版本意图、desired state；
- 安装和更新时间；
- 由原有插件适配器验证并规范化的公开配置；
- 指向 `private.plugins` 的 Secret 引用。

`private` 的 schema 为 1，仅包含适配器验证的插件 Secret；它必须独立加密。
不导出 PID、进程身份、可执行路径、revision 路径、archive 或 entrypoint。
本接口保存最后采集/成功时间；VM/ES 历史由 lifecycle 各自的逻辑导出接口保存，不混入插件 transport。

## 并发、信任和失败边界

- Monitor runtime 在存储根持有 `.operation.lock` 的排他 flock，离线传输必须取得同一 lease。拒绝 symlink、非普通文件、额外 hardlink 及宽松权限的 lock 文件。
- 这个插件存储 lease **不能代替 lifecycle 的 installation `.lock`**；完整备份仍需要外层维护窗口、排空与归属检查。
- 导出只读取 active pointer 选出的已提交 revision，拒绝遗留/不完整状态被当作空插件集合。
- 导入在写盘前严格验证 schema、大小、重复键、配置、Secret 引用、版本和 image-owned package digest。
- 当前只接受**相同 catalog**，不隐式升级版本。catalog 不从备份加载；包从镜像中重新校验、解压、检查 entrypoint 与 Schema。
- 导入不会启动插件进程。完整提交后，正常 Monitor 启动才恢复 desired state。
- `.restore-in-progress` 在写入前落盘；中断残留会阻止正常启动和再次导出。普通失败只清理本次空目标中新建的固定插件目录，清理失败时保留 marker。外层生命周期仍需要负责 operation 资源归属、失败清理和恢复诊断。

## 已有验证入口

```bash
(cd monitor && go test ./... && go vet ./...)
(cd monitor && go test -race ./internal/plugin)
scripts/verify-plugin-state.sh \
  --monitor-image sha256:<本次构建的本地不可变镜像ID> \
  --evidence .run/phase16-04-portable/evidence.json
```

容器验证使用真实 Redis 和 Monitor，从实际运行产生的 revision 导出，导入新 volume 后验证相同逻辑身份和新的采集成功时间，并拒绝 live export / nonempty import。
它使用 Monitor 内置 discard publisher，**不验证 Router/Kafka/ES/VM、六插件产品矩阵、跨数据域恢复或加密备份**。测试只删除携带本次随机 ownership label 的资源。
