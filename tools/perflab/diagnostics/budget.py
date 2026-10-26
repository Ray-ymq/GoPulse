"""读取应用发布的全 epoch 业务预算合计，缺失或过期时拒绝判定。"""
import time
from .metrics import parse_metrics


def parse_budget(body, now=None, not_before=None):
    now = time.time() if now is None else now
    values = parse_metrics(body)

    def gauge(suffix):
        item = values.get(('gopulse_backend_async_budget_' + suffix, ()))
        if item is None or item['kind'] != 'gauge' or item['value'] < 0:
            raise ValueError('应用预算指标缺失或无效：' + suffix)
        return item['value']

    stamp = gauge('last_success_timestamp_seconds')
    if gauge('sample_success') != 1 or not -1 <= now - stamp <= 15:
        raise ValueError('应用预算采样失败或过期')
    if not_before is not None and stamp < not_before:
        raise ValueError('应用预算尚未在本次检查期间采样')
    result = {'source': 'backend', 'scope': 'all_epochs', 'sampled_at': stamp}
    for name in ('open_requirements', 'parked_messages', 'retained_groups', 'retained_bytes'):
        value = gauge(name)
        if not value.is_integer():
            raise ValueError('应用预算不是整数：' + name)
        result[name] = int(value)
    return result
