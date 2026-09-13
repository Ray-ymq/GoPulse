# 同源双 Frontend 使用与排障

- 从安装状态给出的 edge 地址进入；不要直接访问 Backend 或管理 Frontend 的内部容器端口。
- `/login` 是统一登录入口。普通用户进入 `/posts`；`super_admin` 默认进入 `/admin/`。访问受保护页面时登录后恢复允许的目标；外部地址、API 路径和未知页面不能作为登录回跳。
- 管理员在管理导航选择指标、日志、事件、告警、用户角色、审计和 Exporter/目标配置；社交页面保留帖子、搜索、收藏、通知等业务职责。普通用户不能使用管理 API。
- 管理页右上方“退出登录”和用户导航“退出”均使用 Backend 登出接口。不要在共享设备上以关闭标签页代替退出。
- 会话恢复失败时显示重试入口，不把服务故障当成未注册或要求重新注册。成功恢复后继续原有会话；过期会话回统一登录。
- 日志、事件和审计时间按浏览器时区显示；时间提示保留 API 原始 RFC3339。审计开始/结束输入为本地时间，提交时转换为 UTC。在 `Asia/Shanghai`，`2026-09-13 00:00` 对应 `2026-09-12T16:00:00Z`。
- 键盘 Tab 可遍历导航、表单和按钮；管理页“跳至主要内容”将焦点移到主内容。窄屏表格可横向滚动，不应依赖缩小文字操作。
- 查询失败而保留旧记录时，旧结果不是最新事实。使用刷新/重试；告警 `unknown` / `stale` 不代表恢复。
- HTTP loopback 安装使用 HttpOnly、Path=/、SameSite=Lax cookie，Secure=false；这不是公网 HTTPS 支持声明。非本地配置应按 Backend 安全约束使用 Secure cookie 和 HTTPS。浏览器不存储明文登录凭据。写请求必须同源：edge 保留含端口 Host，并覆盖转发协议；Backend 拒绝不匹配的 Origin 或跨源 Fetch Metadata。无浏览器来源头的自动化 API 客户端保持原有合同。

## 维护与验收

`frontend-shared/` 是两个生产构建共同的基础输入；不要只复制一端样式或日期函数。静态 runtime 镜像不含 Node、开发 server 或源码映射。

本批浏览器矩阵的真实入口是产品 Compose acceptance profile 中的 Playwright：

```bash
# 在验收 runner 已创建角色 fixture、Compose env 和隔离 project 的上下文中执行
# 不要使用真实用户密码作为 fixture。
docker compose --profile acceptance run --rm --no-deps -e GOPULSE_VIEWPORT=desktop acceptance e2e/frontend-product.spec.ts
docker compose --profile acceptance run --rm --no-deps -e GOPULSE_VIEWPORT=narrow acceptance e2e/frontend-product.spec.ts
```

`scripts/verify-compose.sh` 自动在隔离管理 fixture 上调用两组矩阵。浏览器时区固定 Asia/Shanghai。API 故障拦截仅用于重试状态的确定性检查，不代替真实 Backend/数据库与插件回归。

产品 Bundle 的直接验收使用现有 lifecycle 验证入口；它自动安装本批隔离产品、创建角色 fixture，再经唯一 edge 调用临时 acceptance profile，并在结束时按归属清理：

```bash
scripts/verify-product-lifecycle.sh \
  --manifest dist/phase16-03-v2/release-manifest.json \
  --platform linux/amd64 --clean-install \
  --acceptance-image sha256:<本机浏览器镜像ID>
```

浏览器镜像必须是 Linux amd64，且 OCI version 与被测 Backend 候选一致；receipt 分别记录测试源码 revision 与产品 revision，允许不改变产品制品的验收脚本修正；不接受可变 tag 作为该参数。实际候选目录和镜像 ID 以本批实施记录为准。此工具入口不要求宿主 Node.js，也不改变产品 Bundle 的启动合同。
