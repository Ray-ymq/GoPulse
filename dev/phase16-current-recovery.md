# 当前候选产品恢复验收

仅支持真实 Linux amd64、同一 manifest 的空 project 恢复。不是跨版本升级。

从已提交源码构建当前候选，再用同版本 acceptance 镜像的不可变本地 image ID：

```bash
python3 scripts/ci/release_artifacts.py build --registry 127.0.0.1:15002/gopulse --output dist/phase16-05-current
scripts/verify-backup-restore.sh --manifest dist/phase16-05-current/release-manifest.json --platform linux/amd64 --current-product --acceptance-image "$ACCEPTANCE_IMAGE"
scripts/verify-backup-restore.sh --manifest dist/phase16-05-current/release-manifest.json --platform linux/amd64 --current-product --failure-matrix
scripts/verify-compose.sh
```

默认工作目录 `.run/phase16-05-recovery/product` 必须为 0700；`--work` 可选择另一个独立私有目录。凭据、源事实和加密备份为 0600。完成状态仅在对应检查实际成功后写入 `acceptance.json`，同一 manifest 可恢复已成功进度。失败必须保留诊断，不把部分输出视为完成证据。

## 数据配方与比较合同

A (`source`) 通过 register、正式 admin-role bootstrap 创建普通用户/超级管理员；发布、编辑文章和评论；六类 current 插件真实采集，真实阈值告警及管理操作生成审计。Browser 使用正式产品路由，覆盖双 Frontend。

备份为既有 format v1。引擎在停写/排空切点记录并恢复后精确核对 SQL dump、搜索文档、历史指标摘要、插件配置/意图，只有这些校验与启动全部成功才发布 `restore-result.json`。独立 inspect 和 fixture 审计提供各域计数、切点和 payload checksum。

产品层比较身份/角色/口令哈希、bootstrap 关系、文章内容/版本、评论、点赞、规则、告警初始身份及审计的完整代表行，而非仅比较数量。运行中的告警评估时间/计数和采集时间允许前进；新增行允许存在。API 会话按原合同失效，恢复后重新登录，普通用户访问管理员审计必须拒绝。搜索索引结果需包含新写入的确切文章 ID。

B (`target`) 恢复 A 后验证既有事实，等待六插件新的采集时间，创建新规则并等待该规则的真实 incident，写入新文章并查询。备份 B 后在 C (`second`) 再次恢复及继续使用。两个成功目标都必须拒绝非空恢复且既有内容不变。

故障入口只使用已通过 A/B/C 的同候选备份，执行真实非法 SQL 导入失败和 SIGTERM 中断，要求失败非 ready、归属资源清理、operation id/阶段/安全恢复指引及相同备份的正式重试成功。不重复上一批完整错误口令、tamper、容量与归属矩阵。

验收结束用相同 manifest 和 work 调用 `--current-product --cleanup` 强归属清理四个项目；registry/加密备份可保留用于复核。不得使用全局 Docker prune。
