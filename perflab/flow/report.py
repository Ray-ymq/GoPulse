"""仅读取归档数据，离线生成总结果和报告。"""
import json
import subprocess
from pathlib import Path

from .config import contract, ROOT
from .runtime import write_json


def report(directory, recompute=True):
    directory = Path(directory)
    config = json.loads((directory / 'effective-config.json').read_text())
    state = json.loads((directory / 'state.json').read_text())
    outcome_path = directory / 'outcome.json'
    outcome = json.loads(outcome_path.read_text()) if outcome_path.exists() else {'exit_code': state.get('exit_code', 3), 'reason': '未完成归档'}
    groups = [json.loads(path.read_text()) for path in sorted((directory / 'groups').glob('*/result.json'))]
    replay_errors = []
    if recompute:
        for group in groups:
            path = directory / 'groups' / group['group_id']
            if (path / 'load-result.json').exists():
                try:
                    raw = subprocess.check_output(['go', '-C', str(ROOT / 'perflab'), 'run', './cmd/pt-load', '--analyze', str(path / 'task.json')], text=True, stderr=subprocess.PIPE)
                    group['load'] = json.loads(raw)
                    group['business_passed'] = all(s['business_passed'] for s in group['load']['stages'])
                except (OSError, subprocess.CalledProcessError, ValueError) as error:
                    replay_errors.append({'group_id': group['group_id'], 'reason': type(error).__name__})
                    group['evidence']['passed'] = False
    expected = sorted((directory / 'groups').glob('*/task.json'))
    missing = [str(path.parent.relative_to(directory)) for path in expected if not (path.parent / 'result.json').exists()]
    scan = next((g for g in groups if g['group_id'] == 'scan'), {})
    measured = [s['target_rps'] for s in scan.get('load', {}).get('stages', []) if s['name'] != 'plateau-observation']
    # 跳过档位仅描述配置起点以前的标准档位，不能推断它们已通过。
    defaults = [10, 20, 40, 80, 160, 320, 640, 1280, 2560, 5120, 6400]
    execution_completed = bool(state.get('execution_completed') and outcome.get('pipeline_completed') and not missing and not replay_errors)
    value = contract('result', run_id=state['run_id'], mode='acceptance' if config.get('acceptance') else 'formal', start_rps=config['start_rps'], max_rps=config['max_rps'],
                     skipped_scan_rps=[r for r in defaults if r < config['start_rps']], measured_scan_rps=measured,
                     groups=groups, capacity=outcome.get('capacity', {}), skipped=outcome.get('skipped', []),
                     execution_completed=execution_completed, exit_code=outcome['exit_code'], reason=outcome.get('reason'),
                     missing=missing, replay_errors=replay_errors, highest_normal_is_limit=bool(outcome.get('capacity', {}).get('normal_rps') == config['max_rps']))
    write_json(directory / 'result.json', value)
    lines = [f'# PT 运行 {state["run_id"]}', '',
             f'执行完成：{execution_completed}；退出码：{value["exit_code"]}。',
             f'连续加压起点：{config["start_rps"]} RPS；扫描上限：{config["max_rps"]} RPS。',
             f'实际连续档位：{measured}。', f'起点前未测档位：{value["skipped_scan_rps"]}；这些档位未判为通过。',
             '达到扫描上限不代表找到系统极限。', '',
             f'容量区间及正常下界来源：`{json.dumps(value["capacity"], ensure_ascii=False)}`。', '',
             '| 组 | 执行完成 | 业务达标 | 停止原因 | 恢复 | 正确性 | 证据 | 资源保持 |',
             '| --- | --- | --- | --- | --- | --- | --- | --- |']
    for g in groups:
        lines.append('| ' + ' | '.join(str(v) for v in [g['group_id'], g['execution_completed'], g['business_passed'], g['stop_reason'],
                     g['recovery']['passed'], g['audit']['passed'], g['evidence']['passed'], g['resources']['passed']]) + ' |')
    lines += ['', '| 组与档位 | 目标 RPS | 实际 RPS | 成功 RPS | 失败率 | P95 ms | P99 ms | 丢弃迭代 |',
              '| --- | --- | --- | --- | --- | --- | --- | --- |']
    for group in groups:
        for stage in group.get('load', {}).get('stages', []):
            lines.append('| ' + ' | '.join(str(v) for v in [group['group_id'] + '/' + stage['name'], stage['target_rps'],
                         stage.get('actual_rps'), stage.get('success_rps'), stage.get('failure_rate'), stage.get('p95_ms'),
                         stage.get('p99_ms'), stage.get('dropped_iterations')]) + ' |')
    repeats = [s for g in groups if g['group_id'].startswith('repeat-') for s in g.get('load', {}).get('stages', [])]
    if repeats:
        rates = [s['success_rps'] for s in repeats if s.get('success_rps') is not None]
        lines += ['', f'异常重复验证已归档 {len(repeats)} 次；成功吞吐范围：{min(rates) if rates else None}～{max(rates) if rates else None} RPS。']
    if value['reason']:
        lines += ['', '结束原因：' + value['reason']]
    lines += ['', '未执行项目：`' + json.dumps(value['skipped'], ensure_ascii=False) + '`。',
              '缺失归档：`' + json.dumps(missing, ensure_ascii=False) + '`。',
              '重算失败：`' + json.dumps(replay_errors, ensure_ascii=False) + '`。', '',
              '资源定义见 effective-config.json；环境、覆盖及各组原始证据均在本运行目录。',
              'Redis 仅接入基础 INFO 统计；其余未接入指标见 coverage.json。',
              '异常容量档本身不等同于执行器故障。低负载、稳定性、资源与正确性分别验收。']
    (directory / 'report.md').write_text('\n'.join(lines) + '\n')
    return value
