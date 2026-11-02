"""通过固定 systemd 控制服务管理 Compose 自监控。"""
import argparse
import json
import subprocess
import urllib.request

import deployment

UNIT = 'gopulse-selfmonitor.service'


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', choices=['up', 'down', 'status', 'access', 'verify'])
    args = parser.parse_args()
    if args.action in ('up', 'down'):
        with deployment.lock():
            if args.action == 'up':
                deployment.check('compose', 'all', owner='gopulse')
            subprocess.run(['sudo', '-n', 'systemctl', 'start' if args.action == 'up' else 'stop', UNIT], check=True)
    elif args.action == 'status':
        subprocess.run(['systemctl', 'status', '--no-pager', UNIT], check=True)
    elif args.action == 'access':
        print('Grafana：http://127.0.0.1:23000/；Prometheus：http://127.0.0.1:29090/')
    else:
        subprocess.run(['systemctl', 'is-active', UNIT], check=True)
        with urllib.request.urlopen('http://127.0.0.1:29090/api/v1/targets?state=active', timeout=5) as response:
            value = json.load(response)
        targets = value['data']['activeTargets']
        if not targets or any(row['health'] != 'up' for row in targets):
            raise ValueError('自监控存在缺失或不可达目标')
        with urllib.request.urlopen('http://127.0.0.1:23000/api/health', timeout=5) as response:
            if json.load(response).get('database') != 'ok':
                raise ValueError('Grafana 数据库未就绪')
        print('自监控服务、当前目标和 Grafana 健康检查通过；完整预期目标与数据源状态以管理页检查为准。')


if __name__ == '__main__':
    main()
