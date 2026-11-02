"""检查本次验收请求的新 span；原始追踪数据保留在 PVC 中。"""
from datetime import datetime
import json
import uuid

import k8s


def summarize(raw, since):
    services, traces = {}, {}
    for line in raw.splitlines(keepends=True):
        if not line.endswith('\n'):
            continue
        for resource in json.loads(line).get('resourceSpans', []):
            service = next((row['value'].get('stringValue') for row in resource.get('resource', {}).get('attributes', [])
                            if row['key'] == 'service.name'), None)
            for scope in resource.get('scopeSpans', []):
                for span in scope.get('spans', []):
                    if service and int(span['startTimeUnixNano']) >= since:
                        services[service] = services.get(service, 0) + 1
                        traces.setdefault(span['traceId'], set()).add(service)
    return {'services': services, 'trace_count': len(traces),
            'cross_service_traces': sum(len(names) > 1 for names in traces.values())}


def verify(mode, started_at):
    from acceptance import now, wait
    if not k8s.owned_namespace(mode):
        raise ValueError('当前模式未部署')
    ns = k8s.namespace(mode)
    name = 'trace-check-' + uuid.uuid4().hex[:10]
    job = {'apiVersion': 'batch/v1', 'kind': 'Job', 'metadata': {'name': name, 'namespace': ns,
           'labels': {'app.kubernetes.io/part-of': 'gopulse'}}, 'spec': {'backoffLimit': 0, 'activeDeadlineSeconds': 180,
           'template': {'metadata': {'labels': {'app.kubernetes.io/part-of': 'gopulse'}}, 'spec': {
           'restartPolicy': 'Never', 'automountServiceAccountToken': False,
           'containers': [{'name': 'read', 'image': 'busybox:1.37.0', 'command': ['sleep', '180'],
               'resources': {'requests': {'cpu': '10m', 'memory': '16Mi'}, 'limits': {'cpu': '250m', 'memory': '128Mi'}},
               'volumeMounts': [{'name': 'trace', 'mountPath': '/trace', 'readOnly': True}]}],
           'volumes': [{'name': 'trace', 'persistentVolumeClaim': {'claimName': 'phase20-trace-data', 'readOnly': True}}]}}}}
    k8s.resource_check([job], mode)
    since = int(datetime.fromisoformat(started_at).timestamp() * 1_000_000_000)
    expected = {'backend'} if mode == 'observability' else {'backend', 'business-worker', 'search-indexer'}
    try:
        k8s.apply([job])
        k8s.kube('-n', ns, 'wait', 'pod', '-l', 'job-name=' + name, '--for=condition=Ready', '--timeout=90s')
        pod = json.loads(k8s.kube('-n', ns, 'get', 'pod', '-l', 'job-name=' + name, '-o', 'json'))['items'][0]['metadata']['name']
        def collected():
            raw = k8s.kube('-n', ns, 'exec', pod, '--', 'sh', '-ec', 'test ! -f /trace/spans.jsonl || cat /trace/spans.jsonl')
            result = summarize(raw, since)
            if expected <= result['services'].keys() and (mode == 'observability' or result['cross_service_traces'] > 0):
                return result
            return False
        result = wait('本次请求的追踪数据', collected, seconds=60)
        result.update(checked_at=now(), started_at=started_at, pvc='phase20-trace-data')
        (k8s.PRIVATE / mode / 'trace-acceptance.json').write_text(json.dumps(result, indent=2) + '\n')
        print('本次请求的新 span 和追踪 PVC 读取检查通过。')
        return result
    finally:
        k8s.kube('-n', ns, 'delete', 'job', name, '--ignore-not-found', '--wait=true')
