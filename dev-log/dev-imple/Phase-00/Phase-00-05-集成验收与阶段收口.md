# Phase 0-05：集成验收与阶段收口实施方案

> 执行序号：5 / 5
> 前置批次：Phase 0-01 至 Phase 0-04 已完成并通过验收
> 总方案来源：[Phase-00-总实施方案.md](../Phase-00-总实施方案.md)

## 1. 批次目标

建立跨平台验收脚本，执行 Phase 0 的完整冷启动、健康检查、故障、恢复和停止验证，补齐开发入口文档与阶段记录，形成可交付给 Phase 1 的工程骨架。

本批不再增加新的基础能力；发现缺陷时应回到对应批次修复，然后重新执行完整验收。

## 2. 前置条件

- Phase 0-01 的 Compose 基础设施已经稳定。
- Phase 0-02 的 Backend 测试和接口联调通过。
- Phase 0-03 的 Frontend 测试、类型检查和构建通过。
- Phase 0-04 的 PowerShell、Bash 开发脚本已验证。
- 当前环境能够运行至少一套完整端到端流程，并可对另一平台脚本执行语法检查或实际验证。
- 开始前记录 Git 状态，避免提交用户已有改动。

## 3. 实施范围

### 3.1 验收脚本

新增：

```text
scripts/verify.ps1
scripts/verify.sh
```

两套脚本语义一致，并根据脚本自身路径解析仓库根目录。

`verify` 至少检查：

1. MySQL、Redis、RabbitMQ 三个 Compose 服务存在且为 healthy。
2. `GET /health` 返回 HTTP 200，响应包含预期 `status` 和 `service`。
3. `GET /ready` 返回 HTTP 200，且三个 checks 均为 `up`。
4. Frontend 首页能够通过 HTTP 访问。
5. 任一检查失败时以非零状态退出，并明确指出失败项。

脚本不得只通过字符串包含关系判断关键 JSON；应解析响应结构并验证字段和值。

### 3.2 完整集成验收

必须执行以下主流程：

1. 从应用和容器均未启动的状态执行一条 `dev` 命令。
2. 三项 Compose 服务进入 healthy。
3. Backend `/health` 返回 200。
4. Backend `/ready` 返回 200，三个 checks 均为 `up`。
5. Frontend 页面可访问并展示四项正常状态。
6. 停止任意一项基础设施后，`/health` 仍返回 200。
7. 同一情况下 `/ready` 返回 503，并准确标记失败项。
8. 恢复基础设施后，`/ready` 无需重启 Backend 即恢复为 200。
9. 执行 `down` 后应用和容器停止，具名卷仍保留。

### 3.3 扩展可靠性场景

除总方案主流程外，补充：

- 依次停止 MySQL、Redis、RabbitMQ，验证每一项故障映射。
- 同时停止两项依赖，验证所有失败项均被报告。
- 在故障期间重复调用 `/ready`，确保请求在超时范围内结束。
- 连续执行两次 `dev`，不产生重复容器或孤儿应用进程。
- 连续执行两次 `down`，第二次保持幂等。
- 制造陈旧 PID 文件，确认不会误杀无关进程。
- 占用 5173 或 8080，确认 `dev` 提前给出可定位错误。
- 从非仓库根目录调用脚本，确认路径解析正确。
- 停止 Backend 后确认 Frontend 显示不可达状态。

### 3.4 文档收口

完善根 README，至少包含：

- 项目当前阶段和 Phase 0 能力说明。
- Go、Node.js、npm、Docker、Compose 版本基准。
- `.env` 初始化和本地凭据说明。
- Windows 与 Unix 的 `dev`、`down`、`verify` 命令。
- Frontend、Backend、健康接口和 RabbitMQ 管理台地址。
- 常见端口冲突、容器不健康和陈旧 PID 的排查方式。
- 普通停止保留数据卷的说明。
- 明确 Phase 0 尚未实现的业务能力。

阶段记录至少写明：

- 实际完成内容。
- 执行过的测试和验收命令。
- 验收结果。
- 已知限制或平台差异。
- 进入 Phase 1 的可复用基线。

## 4. 明确不做的内容

- 不新增业务 API、数据库 Schema、迁移或种子数据。
- 不声明 RabbitMQ 业务拓扑或消费者。
- 不增加 CI、代码生成、完整日志/指标/追踪体系。
- 不创建应用 Dockerfile，不进行全面容器化。
- 不引入 Kafka、Elasticsearch、VictoriaMetrics 或 Kubernetes。
- 不通过跳过测试、延长到不合理超时或放宽断言来绕过缺陷。
- 不提前实施 Phase 1 的 User、Post、Comment、Like 业务。

## 5. 目标文件和目录

本批预计涉及：

```text
scripts/verify.ps1
scripts/verify.sh
README.md
dev-log/dev-logs/            # Phase 0 实施记录
```

若前四批验收暴露缺陷，可以修改对应文件，但必须记录修复归属并重新执行受影响批次及本批完整验收。

## 6. 详细实施步骤

1. 定义 PowerShell 与 Bash `verify` 的共同检查项、超时和退出码。
2. 实现 Compose 服务状态读取和 healthy 校验。
3. 实现 `/health`、`/ready` HTTP 调用及 JSON 字段验证。
4. 实现 Frontend HTTP 可访问性检查。
5. 为每项失败输出明确名称、实际结果和建议诊断命令，同时避免泄露敏感配置。
6. 执行 Backend 全部测试和静态检查。
7. 执行 Frontend 测试、类型检查和生产构建。
8. 从冷状态执行完整主流程。
9. 执行单项、多项故障、恢复、重复启动、幂等停止、端口冲突和陈旧 PID 场景。
10. 在可用的平台分别验证 PowerShell、Bash；无法实际运行的平台必须至少完成语法检查并记录限制。
11. 完善 README 和 Phase 0 实施记录。
12. 对照总方案逐项核验范围和阶段边界。
13. 检查 Git 暂存范围，只包含 Phase 0 本次实际完成文件。

## 7. 测试与验收标准

### 7.1 Backend

- `go test ./...` 通过。
- `go vet ./...` 通过。
- `/health`、`/ready` 单元测试覆盖正常、失败、超时和敏感信息保护。

### 7.2 Frontend

- npm 测试通过。
- TypeScript 类型检查通过。
- Vite 生产构建通过。
- mock 场景覆盖全部正常、部分异常、Backend 不可达和加载中。

### 7.3 脚本

- PowerShell 脚本语法和核心流程通过。
- Bash 脚本通过 `bash -n` 和核心流程验证。
- `verify` 成功时退出码为 0。
- 任一服务、接口或页面失败时退出码非 0，并指出具体失败项。
- `dev`、`down`、`verify` 可从非仓库根目录调用。

### 7.4 端到端验收矩阵

| 场景 | `/health` | `/ready` | Frontend | 预期结果 |
| --- | --- | --- | --- | --- |
| 全部正常 | 200 | 200 | 四项正常 | 验收通过 |
| MySQL 停止 | 200 | 503 | MySQL 异常 | 可自动恢复 |
| Redis 停止 | 200 | 503 | Redis 异常 | 可自动恢复 |
| RabbitMQ 停止 | 200 | 503 | RabbitMQ 异常 | 可自动恢复 |
| 多项依赖停止 | 200 | 503 | 多项异常 | 全部准确标记 |
| Backend 停止 | 不可达 | 不可达 | Backend 不可达 | 无未处理异常 |
| `down` 后 | 不可达 | 不可达 | 不可达 | 容器停止、卷保留 |

### 7.5 文档与范围

- README 中所有命令与实际文件、端口一致。
- 阶段记录包含真实执行结果，不把未运行检查写成已通过。
- 没有引入总方案明确排除的内容。
- Phase 1 可直接复用 Backend module、MySQL、Redis 和配置入口。

## 8. 完成定义

仅当以下条件全部满足时，Phase 0 才算完成：

- 一条 `dev` 命令能够建立完整本地开发链路。
- `/health`、`/ready` 和 Frontend 状态页面符合契约。
- 三项基础设施的故障和恢复无需重启 Backend。
- `down` 可安全、幂等停止应用和容器，并保留具名卷。
- Go、Frontend、脚本和端到端验收全部通过，或明确记录无法执行的平台限制。
- README 与实施记录完整且与实际行为一致。
- Git 提交不包含与 Phase 0 无关的用户改动。

## 9. 下一批次交接条件

本批为 Phase 0 最终批次。交付 Phase 1 时应提供：

- 可重复使用的开发环境启动、停止和验收命令。
- 稳定的 Backend module、配置入口和基础设施客户端。
- MySQL 作为后续业务事实来源的可连接实例。
- Redis 可连接实例，但尚无缓存键和策略。
- RabbitMQ 可连接实例，但尚无业务拓扑和消费者。
- Phase 0 最终测试记录、已知限制和未完成项。

Phase 1 应在现有 Backend module 中按业务边界增加模块，不复制或重新建立 Phase 0 基础设施入口。
