# CI 缓存与快速失败：Linux 交接

任务：CI-2026-10-06。用户授权本次在 `update` 修改并推送远程。
范围、预算和实际检查记录见 [维护记录](2026-10-06-ci-cache-fast-fail.md)。

## 已实现的行为

1. `quality-gates.yml` 的九组产品作业依赖 governance。治理通过后仍并行执行；治理失败
   时跳过。`update` 仍只运行治理，开发分支仍运行完整产品检查。
2. Compose 构建使用每镜像独立的 GHA 缓存，以及独立持久化的 Go/npm cache mount。
   缓存限定 Linux/amd64。Mac 无需准备或迁移 Linux 缓存；普通 Linux 主机没有 Actions
   runtime 时自动冷构建，不需要复制 GitHub token。
3. `cache-warm.yml` 在 main 更新后预热 Compose 与五个 Go 模块、两个前端的缓存。
   Go 只编译测试二进制，npm 只安装依赖。后台预热增加计算量，实际收益待观察。

缓存动作和权威验收共用 Buildx builder。缓存动作先构建，原验收入口随后仍调用 Compose
build，复用同一 builder 的层并生成自己的验收 tag。不能据此声称取消了第二次调用，
也不能将缓存当作旧版本镜像、验收证据或跳过验收的依据。

涉及文件：

- `.github/workflows/quality-gates.yml`
- `.github/workflows/cache-warm.yml`
- `.github/actions/compose-build-cache/action.yml`
- `scripts/ci/compose_build_cache.py`
- `scripts/ci/test_compose_build_cache.py`
- 两份本次维护/交接文档

产品 VERSION、Dockerfile、原验收脚本、自动合并入口未改。
Mac 工作区原有文档修改和删除不属于本次提交，也不通过这次推送迁移。

## Linux 接续步骤

在无未提交修改的 Linux checkout 中确认 primary remote。以下使用本仓库配置 `upstream`；
新 clone 若使用 `origin`，替换 remote 名。命令按仓库 RTK 规则执行。

```bash
rtk proxy git fetch upstream
rtk proxy git switch update
rtk proxy git pull --ff-only upstream update
rtk proxy git status --short
rtk proxy git log -1 --oneline
rtk proxy python3 scripts/ci/validate_versions.py
rtk proxy python3 scripts/ci/validate_branch.py --branch update --base-ref upstream/main
```

如果本次提交已自动合并到 main，三点比较可能没有独有文件，此时范围验证器会报告
“requires --base-ref or at least one --changed-file”。先确认合并状态；这是空差异提示，
不要为使检查通过制造额外修改。合并后的工作基线使用最新 main。

推送 update 会触发现有 Auto PR and Merge：治理通过后自动创建 PR 并使用 merge commit
合并到 main，保留 update；update 本身不会运行全套产品验收。main 合并后检查预热：

```bash
rtk proxy gh run list --repo Ray-ymq/GoPulse --workflow cache-warm.yml --limit 5
rtk proxy gh run view RUN_ID --repo Ray-ymq/GoPulse --log
```

优先查看实际日志，确认镜像层缓存导出、Go/npm cache mount 提取、依赖缓存保存成功。
只有自然触发失败或没有 main 预热记录时才手动补跑，避免重复成本：

```bash
rtk proxy gh workflow run cache-warm.yml --repo Ray-ymq/GoPulse --ref main
```

用户明确要求本次不再做 Linux 脚本回归，不需要为本次交接补跑脚本测试或搭建验收栈。
随后在下一个按 Phase 计划分配的开发批次观察完整 CI 的命中和时长：构建日志应出现
`CACHED`，制品 revision 对应该次提交，完整验收仍通过。不为触发 CI 新建未分配的开发
分支、不改 VERSION。本次不额外启动完整验收；该验收在后续正常开发分支 CI 中运行。

## Linux 接续结果（2026-10-06 19:58 CST）

- 在现有 `/var/tmp/gopulse-phase20-plan-fix-5Jssvw` `update` worktree 执行
  `rtk proxy git fetch upstream`，`upstream/update` 从 `e47a22c` 更新到 `6f0ccad`；随后
  `rtk proxy git pull --ff-only upstream update` 快进成功，worktree 无未提交修改。
- `python3 scripts/ci/validate_versions.py` 通过。
- `python3 scripts/ci/validate_branch.py --branch update --base-ref upstream/main` 按预期报告
  没有独有文件；`upstream/update` 已是 `upstream/main` 的祖先，空差异属于合入后的正常状态。
- `cache-warm.yml` 的 GitHub Actions Run
  [37436975349](https://github.com/Ray-ymq/GoPulse/actions/runs/37436975349) 以
  `main@7ac3f1716e25bea5a7da3044f548d91556355163` 运行并成功完成。Compose、五个 Go 模块和
  两个前端共七个 job 全部成功；日志确认 Compose 镜像层已导出到 GitHub Actions Cache，Go/npm
  cache mount 已提取并保存，`buildkit-mounts` 主缓存也保存成功。首次预热的 Go/npm 主机缓存
  显示 miss，随后保存成功，符合首次预热预期。

本次未运行未授权的新开发分支完整产品验收，也没有验证下一次开发分支的缓存命中率、耗时收益
或制品 revision 绑定；这些仍由后续按 Phase 分配的开发批次 CI 验证。

## 未验证项与诊断边界

- GitHub Actions 实际缓存恢复/导出、首次 main 预热，以及下一开发分支的耗时收益。
- 完整业务、故障恢复、重启和浏览器验收。
- 保留当前缓存配置和原验收门禁。先定位实际失败步骤，再复现最小边界；缓存服务错误
  允许适配器去掉远程缓存重试一次，编译失败直接返回，不做无界重试。
- 配置维护在本地固定门禁通过并提交后完成。后续 Linux 验证需记录实际开始时间、
  有界预算与结果；不要把 Mac 的静态检查记作 GitHub 缓存或完整验收通过。
