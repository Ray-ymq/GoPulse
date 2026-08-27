# Phase 0-03：Frontend 连通性页面实施方案

> 执行序号：3 / 5
> 前置批次：Phase 0-01、Phase 0-02 已完成并通过验收
> 总方案来源：[Phase-00-总实施方案.md](../../dev-phases/Phase-00/Phase-00-总实施方案.md)

## 1. 批次目标

建立 Vue 3 + TypeScript + Vite Frontend，通过稳定的 `/health` 和 `/ready` 契约展示 Backend 与三项基础设施的实时连通状态。

本批完成后，Frontend 应既能通过模拟 HTTP 响应独立测试，也能通过 Vite 开发代理与第二批 Backend 联调。

## 2. 前置条件

- Phase 0-01 的基础设施可以正常启动。
- Phase 0-02 已固定 `/health`、`/ready` 路径、状态码和 JSON 响应。
- 本机具备 Node.js 24 和 npm 11 开发环境。
- Backend 默认监听 `localhost:8080`，Frontend 计划监听 `localhost:5173`。
- 开始前记录 Git 状态，不覆盖或提交无关改动。

## 3. 实施范围

### 3.1 Frontend 工程

使用 Vue 3、TypeScript、Vite 和 npm 创建最小工程：

```text
frontend/
├── src/
│   ├── components/
│   ├── services/
│   ├── types/
│   ├── App.vue
│   └── main.ts
├── package.json
├── package-lock.json
├── tsconfig*.json
└── vite.config.ts
```

要求：

- 所有依赖写入 `package.json`，提交 `package-lock.json`。
- 不依赖未固定的全局脚手架。
- 提供开发、测试、类型检查和生产构建命令。
- 使用浏览器原生请求能力或轻量封装，不为两个简单接口引入不必要的数据层。

### 3.2 页面内容

首页仅展示工程连通性：

- GoPulse 项目标题。
- Backend 存活状态。
- MySQL 就绪状态。
- Redis 就绪状态。
- RabbitMQ 就绪状态。
- 手动刷新按钮。

状态至少包括：

- `loading`：请求正在进行。
- `up`：对应服务可用。
- `down`：Backend 可访问，但对应基础设施不可用。
- `unreachable`：Backend 无法访问或返回无法解析的响应。

页面首次挂载时加载状态；用户点击刷新按钮后重新请求。刷新期间应防止重复提交或明确显示正在刷新。

### 3.3 请求与状态映射

页面分别调用：

```text
GET /health
GET /ready
```

建议处理顺序：

1. 同时发起两个请求或在统一刷新函数中管理两者生命周期。
2. `/health` 成功则 Backend 为正常。
3. `/ready` 的 200 和 503 都应解析响应体；503 是合法业务状态，不等同于网络错误。
4. 根据 `checks.mysql`、`checks.redis`、`checks.rabbitmq` 映射三项状态。
5. 网络失败、超时或无效 JSON 映射为 Backend 不可达，并避免保留误导性的旧状态。
6. 错误信息使用面向开发者的简短描述，不显示敏感连接信息。

### 3.4 Vite 开发代理

配置：

```text
/health → http://localhost:8080/health
/ready  → http://localhost:8080/ready
```

Frontend 必须使用同源相对路径请求接口。本阶段不在 Backend 添加开发 CORS 配置。

Frontend 默认监听端口：

```text
5173
```

## 4. 明确不做的内容

- 不实现 Vue Router、Pinia 或其他全局状态管理。
- 不实现登录、认证、业务页面或业务 API。
- 不展示数据库记录、缓存内容或 RabbitMQ 队列详情。
- 不引入完整设计系统或复杂动画。
- 不创建 Frontend Dockerfile。
- 不修改 Backend HTTP 契约；发现契约问题时回到第二批修正并重新验收。
- 不实现统一 `dev`、`down`、`verify` 脚本。

## 5. 目标文件和目录

本批预计新增：

```text
frontend/package.json
frontend/package-lock.json
frontend/vite.config.ts
frontend/tsconfig*.json
frontend/src/main.ts
frontend/src/App.vue
frontend/src/components/
frontend/src/services/
frontend/src/types/
frontend/src/**/*.test.ts
```

实际拆分以保持简单、可测试为原则，避免为单页状态展示建立过度抽象。

## 6. 详细实施步骤

1. 使用明确版本的 Vue、TypeScript、Vite 初始化 Frontend，并生成 lockfile。
2. 配置 npm scripts：开发、测试、类型检查和生产构建。
3. 定义与 Backend 响应一致的 TypeScript 类型。
4. 实现 `/health`、`/ready` 请求函数，正确处理 503 响应体和网络异常。
5. 建立页面状态模型，区分加载、正常、依赖异常和 Backend 不可达。
6. 实现最小状态页面和手动刷新按钮。
7. 配置 Vite 代理和默认开发端口。
8. 使用 mock HTTP 响应编写组件或页面测试。
9. 运行类型检查、测试和生产构建。
10. 启动第二批 Backend，通过 Vite 代理执行真实联调。

## 7. 测试与验收标准

### 7.1 自动化测试

覆盖以下场景：

- 页面初始加载期间显示 loading。
- `/health` 和 `/ready` 均正常时显示四项正常。
- `/ready` 返回 503 时仍解析 JSON，并准确显示失败基础设施。
- 多项基础设施失败时全部正确显示。
- Backend 无法连接时显示 Backend 不可达，不把依赖误标为正常。
- 响应 JSON 缺失必要字段或格式错误时进入安全的错误状态。
- 点击刷新后重新请求并更新状态。
- 刷新过程中按钮行为明确，不产生不可控的并发请求。

### 7.2 工程检查

- TypeScript 类型检查通过。
- Frontend 测试通过。
- Vite 生产构建通过。
- `package-lock.json` 已生成且与 `package.json` 一致。
- 请求代码只使用 `/health` 和 `/ready` 相对路径。

### 7.3 联调验收

1. 启动 Backend 和 Frontend。
2. 浏览器访问 `http://localhost:5173`。
3. 三项基础设施正常时页面显示 Backend、MySQL、Redis、RabbitMQ 正常。
4. 停止 Redis 后，Backend 仍显示可用，Redis 显示异常，其余依赖状态正确。
5. 恢复 Redis 并刷新，页面恢复正常。
6. 停止 Backend 后刷新，页面显示 Backend 不可达。
7. 浏览器请求通过 Vite 代理完成，无需 Backend CORS 配置。

## 8. 完成定义

- Vue 3 + TypeScript + Vite 最小工程和 lockfile 已建立。
- 页面完整覆盖四种核心状态。
- 503 readiness 响应被视为可解析状态而非普通网络失败。
- 自动化测试、类型检查和生产构建通过。
- 与真实 Backend 的正常、异常和恢复联调通过。
- 未引入路由、全局状态、业务页面或容器化。

## 9. 下一批次交接条件

交付给 Phase 0-04 前必须提供：

- Backend 的确定启动命令和工作目录。
- Frontend 的 npm 安装、开发启动和构建命令。
- Frontend、Backend 的默认端口。
- 两个应用进程的正常退出方式和退出码行为。
- 从根目录分别手动启动二者并成功联调的记录。

Phase 0-04 负责统一编排现有应用，不应修改第二、三批的核心功能来规避脚本问题。
