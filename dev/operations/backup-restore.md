# Linux amd64 一致备份与恢复

产品操作入口见 `deploy/release/BUNDLE-README.md`：使用候选 bundle 中固定 digest 的
lifecycle，不直接运行数据库工具，不把宿主机进程当作第二套实现。

- `backup` 使用完整维护窗口，先停止入口写入、排空 outbox/RabbitMQ/Kafka，再执行一致逻辑导出。
- format v1 归档认证加密，最大 256 MiB；Secret 条目独立加密。口令只从私有 0600 文件读取，
  为 16..4096 原始字节；不要放进参数值、环境变量、日志或 shell tracing。
- 保存归档、口令和**完全相同的源 bundle**，分别保管。仅相同版本还不够，manifest digest 必须一致。
- 恢复目标为独立 0700 安装目录：先 `init`（选未占用端口），再将归档和口令文件放进该目录。
  bundle 工具仅挂载选中的安装目录，不能直接读取另一个安装目录的备份。
- `restore` 只接受空 project；用户、角色、业务、告警和审计保留，JWT 会话失效，Redis 缓存重建。
- ES/VM 恢复已确认的日志和指标历史。Kafka 新空 topic 从 offset 0 继续，源真实 offset 保存在
  cutover 证据中；不伪造 broker 历史。RabbitMQ 恢复已排空后的拓扑，不携带 broker 用户密码散列。
- 插件由镜像内受信 catalog 重新物化，保留配置、desired state 与最后采集时间，不执行归档中的二进制。
- 失败不会发布 ready；清理仅限本次新建且强归属的目标资源。修复原因后从同一归档重试 restore。
  pending restore 禁止用普通 up/verify 绕过。不要手工删除 marker 使半恢复系统上线。
- `doctor --diagnostics-dir ABS` 需要已有私有目录；输出仅限脱敏元数据，不上传 state/secrets 或业务导出。

实际验收与已知限制见 `dev/logs/Phase-16/Phase-16-04-一致备份恢复与诊断闭环.md`。
本批不声明 1.9.4 升级、在线热备、跨架构、macOS 或 Windows 产品支持。
