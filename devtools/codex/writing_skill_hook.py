#!/usr/bin/env python3
"""Load the Chinese technical-writing skill for each Codex user turn."""

import json
import os
from pathlib import Path
import shlex
import shutil
import sys


def codex_home():
    return Path(os.environ.get("CODEX_HOME") or Path.home() / ".codex").resolve()


def install():
    root = codex_home()
    skill = root / "skills/asd-ste100-zh/SKILL.md"
    if not skill.is_file():
        raise ValueError(f"请先安装 asd-ste100-zh skill：{skill}")

    config = root / "hooks.json"
    data = json.loads(config.read_text(encoding="utf-8")) if config.exists() else {}
    groups = data.setdefault("hooks", {}).setdefault("UserPromptSubmit", [])
    target = root / "hooks/writing_skill_hook.py"
    command = f"python3 {shlex.quote(str(target))}"
    if not any(
        handler.get("command") == command
        for group in groups
        for handler in group.get("hooks", [])
    ):
        groups.append({"hooks": [{
            "type": "command",
            "command": command,
            "timeout": 5,
            "statusMessage": "加载中文技术写作规则",
        }]})

    target.parent.mkdir(parents=True, exist_ok=True)
    if Path(__file__).resolve() != target:
        shutil.copyfile(__file__, target)
    updated = json.dumps(data, ensure_ascii=False, indent=2) + "\n"
    if not config.exists() or config.read_text(encoding="utf-8") != updated:
        if config.exists():
            shutil.copyfile(config, config.with_suffix(".json.bak"))
        config.write_text(updated, encoding="utf-8")
    print(f"已安装：{target}\n已配置：{config}\n请在 Codex 的 /hooks 中审查并信任此 hook。")


def run():
    event = json.load(sys.stdin)
    if event.get("hook_event_name") != "UserPromptSubmit":
        return
    skill = codex_home() / "skills/asd-ste100-zh/SKILL.md"
    if not skill.is_file():
        print(json.dumps({"systemMessage": f"写作 skill 缺失：{skill}"}, ensure_ascii=False))
        return

    context = (
        "本轮凡需撰写或改写自然语言文字，先使用 $asd-ste100-zh。"
        f"请在动笔前读取完整入口：{skill}；已在当前上下文完整读取时可直接应用。"
        "适用内容包括回复与技术解释、文档、操作步骤、报告、代码注释、错误信息、"
        "工具描述、commit 和 PR 说明，不以用户是否提到‘写作’为触发条件。"
        "按入口指引读取必要的相对引用。遵循 skill 的语言选择和适用边界，"
        "创意或营销任务按其边界处理。保留用户指定的语言、格式、事实、条件、"
        "风险、置信度及必须原样保留的引用、代码、命令、路径和标识符。"
        "该规则只约束自然语言表达，不改变程序逻辑；仍须遵循更高优先级指令。"
    )
    print(json.dumps({"hookSpecificOutput": {
        "hookEventName": "UserPromptSubmit",
        "additionalContext": context,
    }}, ensure_ascii=False))


if __name__ == "__main__":
    try:
        if sys.argv[1:] == ["--install"]:
            install()
        elif not sys.argv[1:]:
            run()
        else:
            raise ValueError("用法：writing_skill_hook.py [--install]")
    except (OSError, ValueError, TypeError, AttributeError) as error:
        print(f"writing-skill-hook: {error}", file=sys.stderr)
        sys.exit(1)
