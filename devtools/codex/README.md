# Codex 写作 skill 与 hook

使用 [asd-ste100-zh](https://github.com/timwhitez/asd-ste100-zh-skill) 撰写或改写中文技术文字，包括回复、文档、注释、错误信息和提交说明。遵循 skill 自身的语言规则与适用边界。

## 安装

使用 Codex 自带的 skill-installer 安装仓库根目录，名称指定为 `asd-ste100-zh`：

```sh
python3 "${CODEX_HOME:-$HOME/.codex}/skills/.system/skill-installer/scripts/install-skill-from-github.py" \
  --repo timwhitez/asd-ste100-zh-skill --path . --name asd-ste100-zh
python3 devtools/codex/writing_skill_hook.py --install
```

skill 已存在时，跳过第一条命令。安装器不会覆盖已有 skill。

hook 安装到 `${CODEX_HOME:-$HOME/.codex}/hooks/writing_skill_hook.py`，并向同目录的 `hooks.json` 合并 `UserPromptSubmit` 配置。重复安装不会重复注册；已有其他 hook 保留，配置变更前保存到 `hooks.json.bak`。配置不依赖 GoPulse 的位置，对使用该 Codex 配置目录的项目生效。

安装后，在 Codex 的 `/hooks` 中审查并信任该 hook。新 skill 从下一轮开始可用；如客户端尚未重新发现 hook，重新打开会话。根据 [OpenAI 官方 hook 文档](https://learn.chatgpt.com/docs/hooks#review-and-trust-hooks)，未经信任的新 hook 会被跳过，定义变更后也需要重新审查。

## 触发方式

每轮请求提交时，hook 注入一条规则：写自然语言文字前，先读取并应用 `asd-ste100-zh/SKILL.md`。已在当前上下文完整读取时，直接应用。它不做关键词匹配，也不自行调用模型或阻止用户请求。具体写作仍由 Codex 按 skill 完成，代码逻辑不受写作规则约束。

hook 只依赖 Python 3 标准库。可直接检查输出协议：

```sh
printf '%s\n' '{"hook_event_name":"UserPromptSubmit","prompt":"解释这段代码"}' \
  | python3 devtools/codex/writing_skill_hook.py
```

如需停用，在 `/hooks` 中禁用对应条目。删除时，仅移除 `hooks.json` 中指向 `writing_skill_hook.py` 的 handler，再删除安装后的脚本；保留其他 hook。
