# Phase-14-06：Compose 集成验收与阶段收口开发记录

## 当前状态

实施中，尚未通过最终门禁，不代表 Phase 14 完成。完成版本保持 `1.11.5`。

## 开工与范围

- 从远程 `origin/main` 创建 `develop/1.11.6`。本机 Git 代理端口不可用，使用仅对该命令有效的无代理环境成功 fetch，再快进到最新 `origin/main`；没有改动全局代理配置。
- origin 与 upstream 指向同一仓库，已分别 fetch；最新主线为 `c544eed`，包含前五批和对应实施记录，根版本为 `1.11.5`。
- 开工前未跟踪文件 `~` 属于既有状态，不修改、不提交。
- 仅补最终 Compose 验收接线、真实阻断修复和阶段记录。前五批 package 成功证据输入未变时直接沿用，不重复全量 package 验证。
- 最终待验证：真实旧卷迁移、空卷管理员流程、六插件和六组件查询、同卷恢复、代表性故障、Phase 13 回归、平台合同及强归属清理。

## 首轮接线与预检查

- 新增 `--phase14` 唯一入口，串行执行完整平台/Phase 13 门禁与新跨批迁移、冷启动、组件、故障验收；拒绝 `--keep`，不跳过清理。
- 新增当前六插件空卷浏览器场景和跨批 runner；沿用既有资源归属、账号调和、组件指标及 endpoint 故障实现。
- 已通过 Bash 语法、Python 编译、三个规定的 `--self-test`、版本元数据一致性和 `git diff --check`。
- 分支版本校验当前按预期失败：批次尚未验收，完成版本仍为 `1.11.5`，不得预先提升为 `1.11.6`。最终验收后必须再次通过该门禁。
- 已实际成功构建隔离 `gopulse/monitor-acceptance:p1406-probe`，当前 Docker 构建环境可用；不是完整门禁成功证据。
- 全栈验收要求干净构建源码。为保留用户既有未跟踪 `~`，先提交接线，再在同一提交的独立 detached worktree 执行固定入口；不移动或删除用户文件。主工作区 `.run/phase1406/` 保存开工资源、端口与 Git 快照。

## 接线修正（未声称完整通过）

- 首轮实际 `--phase14` 项目 `gopulse-accept-c04609ad2d9d` 已完成标准 Dockerfile 构建、Frontend tests/typecheck/build、7 项 Phase 13 浏览器测试及管理员场景；完整门禁仍在执行。
- 组合入口原本会在 Phase 13 与平台验收段各注册一次同名管理员。修正为组合运行只注册一次；独立原有模式不变。
- 现有 plugin update 明确要求更高版本，因此空卷浏览器不能上传同版本 current 包。为 acceptance-only Docker stage 增加 `ACCEPTANCE_UPDATE_VERSION` 构建参数，默认保留原 `1.11.5`，本批显式构建受信 `1.11.7` 成功包；失败包仍为受信 `1.11.90`。不改变生产 current/legacy 信任或运行时授权。
- 补充真实 legacy archive 固定 SHA-256 校验和当前包 digest 记录；`1.10.6/linux/amd64` 为唯一已证实的旧包来源。
- 延用前五批未变化证据：Phase-14-01 的 prepare/active 中断及 archive/Registry 拒绝、Phase-14-02 的精确账号权限、Phase-14-03 的真实 Kafka 临时 follower 拓扑与 offset 及 ES primary 聚合、Phase-14-04 的九项 VM 映射、Phase-14-05 的组件目录/预算及持久化边界 package 测试。最终只重新验证跨批 Compose 和接线变化。
