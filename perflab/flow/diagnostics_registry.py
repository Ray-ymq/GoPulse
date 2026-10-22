"""登记已接入的观测；不把未接入指标写成已实现。"""


def registry():
    rows = [
        ('mysql', '标准 MySQL exporter + dbdiag', 'counter/gauge', '指标声明单位', '边界增量/瞬时值；诊断去重', True, 'implemented'),
        ('application', '环境发现的 7 个应用 metrics', 'counter/gauge', '指标声明单位', '按实例分别取边界，禁止混合标签', True, 'implemented'),
        ('host', '官方 node_exporter', 'counter/gauge', '指标声明单位', '边界增量；缺样不补零', True, 'implemented'),
        ('container', '标准 container-exporter', 'counter/gauge', 'usec/byte/count', '5 秒差分及配额核验', True, 'implemented'),
        ('rabbitmq', 'rabbitmq_prometheus', 'gauge', 'message', '队列 ready/unacked 总和', True, 'implemented'),
        ('redis', 'Redis INFO stats', 'counter', 'command/count', '保留原始值；仅基础状态', True, 'partial'),
        ('database_probe', '独立只读诊断账号', 'gauge', 'ms/bool', '恢复期单独标记，最多 1 连接', True, 'implemented'),
        ('redis_full', '标准 Redis exporter 全量指标', 'mixed', '指标声明单位', '尚未接入全量窗口', False, 'missing'),
    ]
    return {'schema_version': 'gopulse.pt.coverage.v1', 'observer_budget': '系统余量',
            'sources': [dict(zip(('name', 'source', 'type', 'unit', 'window', 'required', 'implementation'), row),
                             missing='必需源失效立即停压；可选源明确列缺失') for row in rows]}
