# 压测工具

压测统一使用仓库根目录的 `make pt`。准备、检查、恢复及执行条件见[固定流程操作说明](flow/README.md)。正式压测必须显式执行，不在工具修改或验证时自动启动。

## 目录结构

```text
perflab/
├── README.md
├── cmd/            # Go 工具入口
├── internal/       # Go 工具实现
├── flow/           # 固定流程、配置及回归测试
│   ├── README.md
│   └── docs/
│       └── VALIDATION.md
├── diagnostics/    # 公共诊断与指标采集
│   └── README.md
├── k6/             # 公共请求生成与语料序列
│   └── README.md
└── tools/          # 辅助脚本
```

- [指标来源与责任](diagnostics/README.md)
- [公共请求生成](k6/README.md)
- [工程验证记录](flow/docs/VALIDATION.md)

## 历史入口

历史方案与报告已移至本地主目录的 `~/performance/`。

PT3、PT4、PT5 和 PT6 的轮次执行链已停用，原 `pt3/`、`pt4/`、`pt5/` 和 `pt6/` 目录已移除。准备使用 `make pt-prepare`，检查使用 `make pt-check`，正式压测使用 `make pt`。这些命令均在仓库根目录执行；执行前须按固定流程操作说明准备专用环境。

历史报告中的轮次命令、运行路径及验证结果保留，作为当时的执行记录。旧命令不再适用于当前工具。公共流程不导入旧轮次的私有状态或凭据。
