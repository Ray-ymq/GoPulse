"""从保存的 exporter 输出计算正式窗口统计，不查询数据库或操作系统。"""
from datetime import datetime
import json
from pathlib import Path
import sys

from common import atomic_json

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from diagnostics.metrics import parse_metrics, summarize_source


def epoch(value):
    return datetime.fromisoformat(value.replace('Z', '+00:00')).timestamp()


def summarize_metrics(directory, begin, end):
    path = directory / 'exporter-metrics.jsonl'
    if not path.exists():
        return {'status': 'missing'}
    samples, errors, finished = {}, [], set()
    expected = ('mysql', 'node', 'backend', 'worker', 'indexer')
    with path.open() as source:
        for line in source:
            try:
                row = json.loads(line)
                stamp = epoch(row['timestamp'])
                if begin <= stamp <= end:
                    errors.extend(row.get('errors', {}))
                for name, body in row.get('metrics', {}).items():
                    if name in finished:
                        continue
                    at = epoch(row['sampled_at'][name])
                    item = (at, parse_metrics(body))
                    if at <= begin:
                        samples[name] = [item]
                    else:
                        samples.setdefault(name, []).append(item)
                    if at >= end:
                        finished.add(name)
                if all(name in finished for name in expected):
                    break
            except (ValueError, KeyError, TypeError):
                errors.append('invalid_metrics_record')
    sources = {name: summarize_source(sorted(samples.get(name, []), key=lambda item: item[0]), begin, end)
               for name in expected}
    value = {'status': 'complete' if not errors and all(item['valid'] for item in sources.values()) else 'incomplete',
             'errors': sorted(set(errors)), 'sources': sources,
             'coverage': '分项成功和摘要覆盖范围须结合配置指标判断；聚合耗时不等同于 CPU 时间'}
    atomic_json(directory / 'metrics-summary.json', value)
    return {'status': value['status'], 'errors': value['errors'], 'sources':
            {name: {key: item[key] for key in ('valid', 'reasons')} for name, item in sources.items()},
            'file': 'metrics-summary.json'}
