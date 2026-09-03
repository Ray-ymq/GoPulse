# Phase-05-03：实现 Review 整改实施记录

## 1. 执行信息

- 执行日期：2026-09-03
- 权威分支：`develop/1.2.3`
- 代码基线：`origin/main` 的 `e3915e30e402ff2e23146ac04d031c0136ccb386`
- Review 输入：`dev/review/2026-09-03-Phase-5实现Review报告.md`
- 目标版本：`1.2.3`
- 执行环境：WSL2 Linux filesystem，Bash，Go 1.26.7，Docker/Compose

开始前已执行 `git fetch --prune origin`，确认远程不存在同名开发分支且 `origin/main` 仍为上述基线。本批继续使用用户指定并已承载 Review 文档的本地 `develop/1.2.3`。工作区既有未跟踪文件 `使用指南.md` 未被读取、修改、暂存或提交。

## 2. 实际完成工作

### 2.1 P1-01：关闭空 IPv6 bracket 触发 wildcard listener

- Go 配置 loader 不再使用无条件 `strings.Trim(value, "[]")`。
- 完整的一对方括号只接受合法 IPv6，归一化后返回内部地址；方括号 IPv4、空/残缺/嵌套括号均被拒绝。
- 未加括号的合法 IPv4、IPv6 与 hostname 保持可用；hostname 标签只允许 ASCII 字母、数字与非首尾连字符，拒绝 slash、反斜线、下划线、空标签、非法首尾连字符和 host:port。
- 保留显式 `0.0.0.0` 与 `::` wildcard 配置；默认仍为 `127.0.0.1`。
- `scripts/dev.sh` 使用 Python `ipaddress` 与等价 hostname 规则，在启动 Compose 前拒绝非法 Redis/Exporter host。
- 增加合法/非法地址表和默认真实 loopback listener 测试；真实 Exporter 验收会以 `REDIS_EXPORTER_HTTP_HOST=[]` 启动最终二进制，确认其以 `invalid_configuration` 非零退出且没有打开端口。

### 2.2 P2-01：失败路径按稳定 Compose 归属清理

- 将运行期 container/service label 与端口绑定检查拆开：端口绑定只用于“正在运行且绑定正确”的断言，不再作为失败清理前提。
- 在生成受限随机 project 和私有 Compose 文件后、执行 `compose up` 前，记录 Compose 文件路径和 SHA-256，并启用 cleanup ownership。
- cleanup 使用 project 格式、Compose 文件摘要及现存 container/network/volume 的 Compose project label 证明归属；Redis 停止、端口映射消失或尚未获得 container ID 时仍可清理。
- self-test 证明非法 project、被篡改 Compose 文件和错误 project label 不会获得删除授权。
- 默认真实验收新增两项子进程失败注入：Redis 停止后故意返回失败；本机 listener 占用发布端口，使 Compose 在已创建部分资源后启动失败。两项均返回预期失败，随后对应随机 project 的 container、network、volume 全部不存在，日常 `gopulse` 快照不变。

### 2.3 P2-02：四个验收端口保证两两唯一

- 新增集合唯一性检查；每轮一次分配 Redis、正常 Exporter、错误密码 Exporter和超时 Exporter四个端口，任意重复时重新分配全部端口，最多尝试 20 轮。
- self-test 固定覆盖 `REDIS==AUTH` 与 `EXPORTER==TIMEOUT` 两个原先遗漏的非相邻冲突组合，均被拒绝。
- 常规 `random_port` 仍存在释放临时 socket 后由消费者接管的常规 TOCTOU 窗口；真实 Compose/进程启动失败会明确失败并执行本次 project 清理，不会遗留资源。

### 2.4 P2-03：权威批次、版本与记录

- Phase 5 总实施方案新增唯一 Phase-05-03 / `1.2.3` / `develop/1.2.3` 分配，并将 Review 后最终完成条件更新为三批全部完成、门禁成功和记录齐全。
- 新增 `Phase-05-03-实现Review整改.md` 拆分方案和本镜像实施记录。
- 根 `VERSION`、`frontend/package.json`、`frontend/package-lock.json` 与 README 当前版本同步为 `1.2.3`。
- `validate_branch.py` 和 `validate_versions.py` 均通过。

## 3. 实际变更文件

- `dev/review/2026-09-03-Phase-5实现Review报告.md`（本分支先行提交的 Review 输入）
- `dev/imple/Phase-05/Phase-05-总实施方案.md`
- `dev/imple/Phase-05/Phase-05-03-实现Review整改.md`
- `dev/logs/Phase-05/Phase-05-03-实现Review整改.md`
- `exporters/redis/internal/config/config.go`
- `exporters/redis/internal/config/config_test.go`
- `scripts/dev.sh`
- `scripts/verify-exporter.sh`
- `README.md`
- `VERSION`
- `frontend/package.json`
- `frontend/package-lock.json`

## 4. 实际验证与结果

### 4.1 Redis Exporter 代码门禁

```bash
(cd exporters/redis && test -z "$(gofmt -l .)")
(cd exporters/redis && go test -count=1 ./...)
(cd exporters/redis && go vet ./...)
(cd exporters/redis && go test -race -count=1 ./...)
```

结果：全部通过。配置、collector、HTTP API 和 logging module 编译/测试通过；race detector 未报告数据竞争；gofmt 无未格式化文件。

### 4.2 Exporter safety 与真实 Docker 验收

```bash
bash scripts/verify-exporter.sh --self-test
scripts/verify-exporter.sh
```

结果：全部通过。

- self-test 拒绝错误进程 marker、非法 project、被篡改 Compose 文件、错误 project label、`REDIS==AUTH` 和 `EXPORTER==TIMEOUT` 冲突，不停止无关进程。
- 两项真实 cleanup failure injection 均通过：stopped-target 与 partial-up 结束后无对应 container、network 或 volume，日常资源快照不变。
- 最终主验收 project 为 `gopulse-exporter-49f018ce4f4f`。
- `[]` 在监听前以 `invalid_configuration` 退出且未打开端口。
- 实时 Redis `INFO` 对值、固定指标、停止目标、认证失败、超时、同进程恢复、SIGTERM、端口释放与最终资源清理全部通过。

### 4.3 脚本、治理与版本

```bash
bash -n scripts/dev.sh scripts/down.sh scripts/verify.sh scripts/verify-exporter.sh
docker compose --env-file .env.example --file deploy/compose.yaml config --quiet
python3 -m unittest discover -s scripts/ci -p 'test_*.py'
python3 scripts/ci/validate_versions.py
python3 scripts/ci/validate_branch.py --branch develop/1.2.3 --base-ref origin/main
git diff --check origin/main...HEAD
git diff --check
```

结果：全部通过。

- Python CI 共 24 项测试通过。
- 根与 Frontend 版本一致为 `1.2.3`。
- `develop/1.2.3` 唯一映射到 Phase-05-03。
- Bash syntax、Compose config 与 whitespace 检查通过。

此外，在不执行 `main`、不启动或停止资源的条件下定向加载 `scripts/dev.sh` 配置函数：`REDIS_EXPORTER_HTTP_HOST=[]` 在启动前被拒绝，`[::1]` 被接受。

## 5. 与方案的偏差

- 未执行完整 `scripts/dev.sh → scripts/verify.sh → scripts/down.sh`。执行前资源检查发现日常 project 已有健康的 `gopulse-redis-1`、活动的 `.run/redis-exporter.json` / 日志和 `127.0.0.1:9121` listener。为避免接管、停止或改变用户现有资源，按仓库资源保护规则安全跳过该链路。
- 该限制不影响独立 Exporter 真实矩阵：修改后的 `scripts/verify-exporter.sh` 已在随机 project 中完成 listener、进程、Docker 成功/失败路径和清理验证；共享 `dev.sh` 的新增 host 判断另由不启动资源的定向解析验证覆盖。
- 未重复 Phase 0～4 完整业务验收、Backend 或 Frontend 产品测试。原因是本批未修改业务代码、持久数据、消息、搜索、HTTP 业务接口或 Frontend 产品实现；Frontend 只同步版本元数据并由版本门禁验证。

## 6. 已知限制与后续项

- `random_port` 在探测 socket 关闭后仍有常规 TOCTOU 竞争；本批消除了已确认的两两比较缺口，并保证启动失败时清理本次隔离 project，没有引入长期端口保留器。
- 本批本地门禁已完成，但完整日常生命周期未在当前用户资源占用条件下重复执行；远程 Integration 门禁可在干净 runner 上补充共享生命周期证据。
- 本记录不把尚未实际发生的远程 workflow、Pull Request 或合入 `main` 写成完成事实。

## 7. 完成结论

Review 报告的 P1-01、P2-01、P2-02 与 P2-03 已由生产配置、脚本安全逻辑、定向测试、真实 Redis/Docker 失败矩阵、权威分配和 `1.2.3` 版本同步关闭。除因用户现有日常栈而安全跳过的共享生命周期重跑外，固定本地门禁均通过；本批达到提交并推送 `develop/1.2.3` 的条件，最终外部完成仍以远程门禁成功并合入 `main` 为准。
