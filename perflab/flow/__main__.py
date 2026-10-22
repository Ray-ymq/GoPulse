"""固定流程命令入口。"""
import argparse
import json
import os
from pathlib import Path
import subprocess
import sys

from .config import ROOT, STAGES, load
from .runtime import STATE, active, lock, stop


def plan(config):
    raw = subprocess.check_output(['go', '-C', str(ROOT / 'perflab'), 'run', './cmd/pt-load',
                                   '--start-rps', str(config['start_rps']), '--max-rps', str(config['max_rps']),
                                   '--warmup-seconds', str(config['warmup_seconds']), '--measurement-seconds', str(config['measurement_seconds'])], text=True)
    stages = json.loads(raw)
    return {'config': config, 'stages': STAGES, 'scan': stages,
            'duration': {'known_seconds': config['idle_seconds'] + config['warmup_seconds'] + config['measurement_seconds'] + sum(s['warmup_seconds'] + s['measurement_seconds'] for s in stages),
                         'additional': '另加最多 8 次边界、3 次异常复测、两组 30 分钟稳定性、22 项接口及各组最多 10 分钟恢复'}}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('command', choices=['prepare', 'plan', 'check', 'run', 'acceptance', 'status', 'stop', 'report'])
    parser.add_argument('--start-rps', default='10')
    parser.add_argument('--run')
    args = parser.parse_args()
    os.umask(0o077)
    try:
        if args.command == 'status':
            print((active() / 'state.json').read_text())
            return 0
        if args.command == 'stop':
            stop()
            print('已保存整次运行的停止请求。')
            return 0
        if args.command == 'report':
            from .report import report
            name = args.run or active().name
            if Path(name).name != name or name in ('.', '..'):
                raise ValueError('RUN 必须为运行编号')
            report(STATE / name)
            return 0
        config = load(args.start_rps)
        if args.command == 'plan':
            print(json.dumps(plan(config), ensure_ascii=False, indent=2))
            return 0
        if args.command == 'acceptance':
            config.update(acceptance=True, idle_seconds=5, warmup_seconds=1, measurement_seconds=10, soak_seconds=15, recovery_seconds=30)
        with lock():
            if args.command == 'prepare':
                from .prepare import prepare
                prepare(config)
            elif args.command == 'check':
                from .environment import preflight
                preflight(config)
            else:
                from .executor import execute
                return execute(config)
        return 0
    except (ValueError, FileNotFoundError, AssertionError) as error:
        print(str(error), file=sys.stderr)
        return 2
    except KeyboardInterrupt:
        return 130
    except Exception as error:
        print(f'{type(error).__name__}: {error}', file=sys.stderr)
        return 3


if __name__ == '__main__':
    sys.exit(main())
