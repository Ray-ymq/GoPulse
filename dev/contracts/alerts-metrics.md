# Metrics 告警（Phase-15-02）

本批提供 Backend 告警 API 和持久评估，不包含告警页面或外部通知。必须先按超级管理员引导流程建立 `super_admin`；所有 `/api/v1/alerts/*` 路由均需要登录和数据库中的超级管理员角色。Logs/Events 暂不可创建。

## 目录与规则

先读取 `GET /api/v1/alerts/catalog`，从 `metrics` 的 `metric`、`label_keys`、`allowed_tuples` 和 `reducers` 选择**单条精确 series**。无标签 metric 必须传 `labels: {}`；有标签必须完整提供某个合法 tuple，不允许通配、正则、source/target/producer 覆盖或任意表达式。

`POST /api/v1/alerts/rules` 示例：

```json
{
  "name": "Redis connections high",
  "enabled": true,
  "severity": "warning",
  "source": "metrics",
  "selector": {"metric": "gopulse_redis_connected_clients", "labels": {}},
  "reducer": "last",
  "operator": "gt",
  "threshold": 100,
  "window": "5m",
  "for": "1m"
}
```

- 未删除规则最多 32 条、名称唯一且为 1..80 个可见 UTF-8 字节。
- window 仅 `1m|5m|15m`，for 仅 `0s|1m|5m` 且不得大于 window。
- gauge reducer 为 `last|max|min|avg`；counter 仅 `increase`，按相邻原始点正增量累计，reset 从新值开始。
- operator 为 `gt|gte|lt|lte|eq|neq`；threshold 必须为有限 JSON number，绝对值不超过 `1e15`。
- JSON 拒绝未知/重复字段、多个值、无效 UTF-8 和超限请求体。

## 路由与并发修改

| 操作 | 路由与请求 |
| --- | --- |
| 目录 | `GET /api/v1/alerts/catalog`，无 query/body |
| 列表 | `GET /api/v1/alerts/rules`，可选 `status`（rule state）、`source`、`limit` |
| 创建 | `POST /api/v1/alerts/rules`，完整规则并显式 enabled，不传 revision |
| 详情 | `GET /api/v1/alerts/rules/:ruleId` |
| 更新 | `PUT /api/v1/alerts/rules/:ruleId`，完整规则 + 当前 revision，**不传 enabled** |
| 启用/停用 | `POST /api/v1/alerts/rules/:ruleId/enable` 或 `/disable`，仅 `{"revision":1}` |
| 删除 | `DELETE /api/v1/alerts/rules/:ruleId`，仅当前 revision，成功 `204`，实际软删除 |
| 当前 | `GET /api/v1/alerts/current`，可选 `severity/source/limit`，只返回 firing |
| 历史 | `GET /api/v1/alerts/history`，可选 `status/severity/source/rule/start/end/limit` |

时间使用 UTC RFC3339；历史默认最近 90 天、单次范围不得超过 90 天；limit 为 1..100，默认 50。继续分页时只提交响应 `meta.next_cursor` 对应的 `cursor`，不与过滤条件混用。Cursor 绑定当前用户、列表类型和原有过滤条件，签名验证失败返回 400。规则按 ID 倒序；当前告警按 critical 优先、首次触发时间/ID 正序；历史按首次触发时间/ID 倒序。

每次成功修改 revision 递增；陈旧 revision 返回 `409 alert_revision_conflict`，重新读取后再决定是否重试。第 33 条规则返回 `409 alert_rule_limit`。不存在/已删除规则返回 `404 alert_not_found`，持久层不可用返回 `503 alerts_unavailable`。

## 评估和恢复

- Backend 约每 30 秒启动一个有界轮次（最多额外 255ms jitter），最多 4 条规则并发，单上游请求最多 2 秒。
- 使用截止点 `now−15s` 前窗口中的**原始采样点**；最新点相对截止点超过 90 秒、空 series/点、上游不可用，均为 unknown，不以 0 代替。Counter 至少需要两个原始点。
- `normal → pending → firing`；for=0 可直接 firing。pending 的 false/unknown 打断连续性；长于 65 秒的评估空白也不能计为连续异常。
- 同一持续异常只更新同一 incident，不重复写 trigger 审计；真实 false 才 recovered。firing 的 unknown 保持 firing 并标记 stale。
- PUT、disable、delete 关闭原 active incident，分别记为 `rule_updated|rule_disabled|rule_deleted`，不是 recovered。
- 状态、incident、lease 和审计保存在 MySQL；重启不清空历史，过期租约可重新领取。不要通过直接修改这些表来操作产品规则。

## 停止评估与验收

在 `.env` 中设置 `ALERT_EVALUATION_ENABLED=false` 并重建 Backend 容器，仅停止后台评估；规则与当前/历史 API、社交业务和 readiness 条件不变。这个开关不会自动关闭或恢复已触发告警。

```bash
bash scripts/verify-alerts.sh --self-test
bash scripts/verify-alerts.sh --sources metrics
```

真实验收创建独立强归属 Compose project，构建当前 checkout 的 Backend/迁移/运维二进制，复用 Phase 14 `1.11.5` 未修改运行镜像作为指标链路基线。需本机已有这些基线镜像、Go 工具链与 Docker/Compose。工具使用真实 Redis 客户端和既有 Monitor → Router → Kafka → Marshaller → VictoriaMetrics 链路，不直接导入样本来冒充告警闭环。证据保留在专属 `.run/gopulse-p1401-<随机标识>/` 下，退出时清理专属容器、网络、卷及临时凭据，不操作日常环境资源。
