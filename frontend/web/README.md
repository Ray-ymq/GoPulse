# GoPulse 用户前端

本模块使用 TypeScript、Vue 3、Vue Router 和 Vite。设计基准为 [纸墨风格原型](../../docs/design/gopulse-uiux.html)。页面使用真实业务 API，不包含演示账号、演示帖子或自动降级到模拟数据的逻辑。

## 本地开发

要求 Node.js 24.20.0 或更新版本。先自行准备后端依赖并启动业务进程，然后另开终端执行：

```sh
make -C frontend/web install
make -C frontend/web dev
```

前端默认地址为 `http://127.0.0.1:5173`。Vite 读取根目录 `.env` 的 `FRONTEND_PORT`、`HTTP_PORT` 和 `GOPULSE_API_TARGET`。未指定 `GOPULSE_API_TARGET` 时，`/api` 代理到 `http://127.0.0.1:${HTTP_PORT:-8080}`。

代理保留浏览器请求的 Host，使后端的同源写入校验可以核对 Origin。生产环境需配置 `/api` 的同源代理，无需在前端保存令牌或配置跨域 API 地址。登录与注册使用后端下发的 HttpOnly 会话 Cookie。

本地开发编排已移除；前端使用上面的独立终端启动，并通过 Ctrl+C 停止。

## 页面与交互

- 发现广场、关注动态、收藏列表、搜索帖子和搜索用户。
- 登录、注册、会话失效处理和退出登录。
- 作者主页、资料编辑、关注与粉丝列表。
- 发帖、帖子详情、作者编辑与删除、点赞、收藏和评论。
- 通知列表、单条已读和当前已加载消息的批量已读。
- 超级管理员的角色管理：按完整用户名查找账号、授予或撤销超级管理员权限。

所有列表使用后端游标分页。时间线按发布时间排序。接口未提供话题分类、热门榜或推荐作者，因此兴趣入口执行关键词搜索；侧栏话题提取自最近加载帖子的 `#话题`，作者入口来自这些帖子的真实作者。没有完整未读总数接口，未读提示只统计最近加载的通知，不代表全站未读总数。

纸墨主题覆盖导航、头像、帖子、表单、弹窗和手机底部导航。字体优先采用系统楷体与宋体；未安装相应字体时使用后续字体回退。页面不依赖外部字体或图片服务。

## 发布、草稿与重试

发帖先读取数据 epoch，再使用成对的幂等请求头提交。网络中断后保留原内容、幂等键和 epoch；用户可以查证结果或重试原发布，前端不会自动换键重新发布。

草稿与尚未确认的发布记录保存在当前浏览器的 localStorage，并按用户区分；不保存密码或会话令牌。浏览器存储不可用时，仅在当前页面保留这些记录。退出登录不会自动清除草稿。共用设备上请使用浏览器的站点数据管理功能清理本地内容。

评论接口没有幂等键。发送时若网络中断，界面提示先刷新评论、核对结果，再决定是否重试。

## 验证与构建

```sh
make check MODULE=frontend/web
make test MODULE=frontend/web
make build MODULE=frontend/web
```

构建产物在 `frontend/web/dist/`。`check` 和 `lint` 执行 Vue 与 TypeScript 类型检查；单元测试覆盖会话请求、错误处理、发帖幂等重试和输入边界。

浏览器测试使用隔离的 API fixture，不写入真实业务环境：

```sh
cd frontend/web
npx playwright install chromium
npm run test:e2e
```

该测试会自行启动本地 Vite 服务并在结束时停止。Linux 缺少浏览器运行库时，可执行 `npx playwright install --with-deps chromium`。真实数据库、搜索和异步通知的联调仍需使用已启动的业务环境验证。

容器构建使用本目录的 `Dockerfile`，由 Nginx 托管静态产物并提供 API 同源代理和前端路由回退。完整部署步骤见 [容器部署说明](../../deploy/README.md)。前端 CI 保留在 `.github/workflows/frontend.yml`。

## 目录

```text
src/
  components/    纸墨组件、帖子列表与交互控件
  composables/   会话和跨页面社区状态
  services/      HTTP、业务 API 与幂等发帖
  types/         业务数据类型
  utils/         文本、时间与跳转校验
  views/         路由页面
  router.ts      路由与登录检查
  styles.css     原型样式与 Vue 页面状态
```

## 管理员角色

超级管理员登录后，导航显示“角色管理”，页面地址为 `/admin/roles`。普通用户不能进入该页面，后端也会独立检查每次管理请求的权限。角色变更需确认；非初始管理员可以撤销自己的权限，完成后返回首页。

系统维护者需先在已配置 MySQL 连接且已完成数据库迁移的环境中，设置一个已注册账号为初始超级管理员。在 `backend/business/` 下执行：

```sh
go run ./cmd/admin-role bootstrap --user-id <已注册用户的ID>
```

初始超级管理员不能被降级，也不能通过该命令替换成其他账号。日常新增或撤销超级管理员权限使用网页操作。`admin-role promote --username <用户名>` 同样用于初始管理员设置，不能替代网页中的日常角色管理。

角色查询使用 `GET /api/v1/admin/users/by-username/{username}`，角色变更使用 `PATCH /api/v1/admin/users/{userId}/role`。后端在同一事务内完成权限复核、角色变更和成功审计记录。网络错误后，页面要求重新查询当前角色，再决定是否提交。
