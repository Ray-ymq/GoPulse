"""将当前代理仅传给专用运行时，集群地址始终直连。"""
import json
import os
from pathlib import Path
import sys


def write(path):
    values = {key: os.environ.get(key, os.environ.get(key.lower(), '')) for key in ('HTTP_PROXY', 'HTTPS_PROXY', 'NO_PROXY')}
    values['NO_PROXY'] = ','.join(filter(None, [values['NO_PROXY'], 'localhost,127.0.0.1,::1,10.244.0.0/16,10.96.0.0/12,.svc,.cluster.local']))
    for value in values.values():
        if '\n' in value or '\r' in value:
            raise ValueError('代理配置不得包含换行')
    path.write_text(''.join(key + '=' + json.dumps(value) + '\n' for key, value in values.items()))
    path.chmod(0o600)


if __name__ == '__main__':
    write(Path(sys.argv[1]))
