# 阶段 2–4 混合请求复测

本脚本使用 k6 `constant-arrival-rate` 执行四类只读请求。目标比例为时间线 40%、帖子详情 20%、帖子搜索 25%、用户搜索 15%。每次迭代调用一次 `http.get`。

1. 安装 k6。在测试实例中准备有效会话、帖子和搜索词。将数据保存为 `.run/capacity-seed.json`，包含 `base_url`、`search_term`、`posts` 和 `sessions`。`posts` 是帖子 ID 数组。`sessions` 是包含 `session` 字段的对象数组。会话值属于凭据，只保存在本地文件中。
2. 从仓库根目录运行以下命令。按测试档位指定 `ARRIVAL_RATE` 和 `DURATION`。`PRE_ALLOCATED_VUS` 默认为 `128`。`MAX_VUS` 默认为 `500`，最小值为 `500`。如果入口使用自签证书，可对该次运行显式添加 `--insecure-skip-tls-verify`。

   ```sh
   k6 run --env SEED_FILE="$PWD/.run/capacity-seed.json" \
     --env ARRIVAL_RATE=1400 --env DURATION=30m \
     --summary-export .run/capacity-summary.json \
     --out json=.run/capacity-samples.json tests/capacity/mixed.js
   ```

3. 检查全局和各接口的延迟、错误率、503 率与 `dropped_iterations`。按测试计划同步采集容器指标，并在达到停止条件时终止测试。 [测试计划](../test-plan.md)
