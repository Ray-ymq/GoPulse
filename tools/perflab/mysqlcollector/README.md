# 压测 MySQL 采样

本工具由 `tools/perflab/flow/diagnose.py` 在独立压测环境构建并启动，用于读取 MySQL 性能指标。它不提供产品插件安装或管理入口。

Go module 名称保持为 `github.com/Ray-ymq/GoPulse/exporters/mysql`。在根目录执行 `make test MODULE=tools/perflab/mysqlcollector` 验证。
