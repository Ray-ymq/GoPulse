# Codex 全局写作规则

使用 [asd-ste100-zh](https://github.com/timwhitez/asd-ste100-zh-skill) 撰写或改写中文技术文字，包括回复、文档、注释、错误信息和提交说明。遵循 skill 自身的语言规则与适用边界。

## 安装

使用 Codex 自带的 skill-installer 安装仓库根目录，名称指定为 `asd-ste100-zh`：

```sh
python3 "${CODEX_HOME:-$HOME/.codex}/skills/.system/skill-installer/scripts/install-skill-from-github.py" \
  --repo timwhitez/asd-ste100-zh-skill --path . --name asd-ste100-zh
```

skill 已存在时，跳过第一条命令。安装器不会覆盖已有 skill。

将 [global-AGENTS.md](global-AGENTS.md) 的内容合并到 `${CODEX_HOME:-$HOME/.codex}/AGENTS.md`。文件不存在时可直接创建；已有内容时保留原有规则，避免重复添加。仓库中的模板用于维护规则，实际生效位置是 Codex 配置目录中的 `AGENTS.md`。

根据 [OpenAI 官方 AGENTS.md 文档](https://learn.chatgpt.com/docs/agent-configuration/agents-md)，Codex 启动会话时读取全局规则。安装后开启新会话，使客户端加载新配置。如果同目录存在 `AGENTS.override.md`，Codex 优先读取该文件，需要将规则合并到实际加载的文件中。

## 使用方式

全局规则要求写作前读取并应用 `asd-ste100-zh/SKILL.md`，并直接列出中文核心要求。文档修改后必须回读实际文件，检查表达和事实逻辑，再交付结果。该配置属于模型指令，具体遵守情况仍需要检查实际输出。

在支持该调试命令的 Codex CLI 中，可以查看新会话将加载的上下文：

```sh
codex debug prompt-input
```

## 从旧 hook 迁移

先写入全局规则，再从 `hooks.json` 的 `UserPromptSubmit` 中移除指向 `hooks/writing_skill_hook.py` 的 handler，并删除该脚本。保留其他 handler、事件和配置；无需关闭全局 hooks 功能。仓库已移除旧 hook 的安装入口。
