#!/usr/bin/env python3
"""Phase 17-01: focused real Compose/browser acceptance of admin presentation."""
import argparse
import json
import os
import subprocess
from verify_admin_frontend import AdminAcceptance
from verify_plugin_metrics import Client, ROOT, command, wait_until


class VisualAcceptance(AdminAcceptance):
    def browser(self, env, name, *specs):
        result = subprocess.run(
            ['npx', 'playwright', 'test', *specs], cwd=ROOT/'frontend', env=env,
            stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=420)
        (self.work/name).write_bytes(result.stdout.replace(self.auth_password.encode(), b'[REDACTED]'))
        assert result.returncode == 0, f'browser failed: {self.work/name}'

    def run(self, previous=None):
        server = json.loads(command(['docker', 'info', '--format', '{{json .}}']).stdout)
        assert server['OSType'] == 'linux' and server['Architecture'] == 'x86_64'
        services = ['backend', 'business-worker', 'search-indexer', 'frontend', 'admin-frontend', 'monitor', 'router', 'marshaller']
        self.images = ['gopulse/'+service+':'+self.tag for service in services]
        result = self.compose('build', *services, check=False, timeout=1800)
        (self.work/'build.log').write_bytes(result.stdout+result.stderr)
        assert result.returncode == 0, 'image build failed'
        self.started = True
        result = self.compose('up', '-d', '--wait', '--wait-timeout', '420', 'frontend', 'monitor', 'marshaller', 'business-worker', 'search-indexer', check=False, timeout=500)
        (self.work/'startup.log').write_bytes(result.stdout+result.stderr)
        assert result.returncode == 0, 'startup failed'
        base = 'http://'+self.compose('port', 'frontend', '8080').stdout.decode().strip()
        self.admin, self.user, demoted = Client(base), Client(base), Client(base)
        demoted_name = 'demote_'+self.token
        for client, name in [(self.admin, self.admin_name), (self.user, self.user_name), (demoted, demoted_name)]:
            client.request('auth/register', 'POST', {'username': name, 'password': self.auth_password}, 201)
        admin_id = self.admin.request('users/me')['data']['id']
        user_id = self.user.request('users/me')['data']['id']
        demoted_id = demoted.request('users/me')['data']['id']
        self.compose('exec', '-T', 'backend', '/usr/local/bin/admin-role', 'bootstrap', '--user-id', str(admin_id))
        self.admin.request(f'admin/users/{demoted_id}/role', 'PUT', {'role': 'super_admin'})
        wait_until(lambda: self.admin.request('exporter-plugins')['data'], 'plugin bootstrap')
        self.admin.request('exporter-plugins/redis-exporter/stop', 'POST')
        self.admin.request('exporter-plugins/redis-exporter/start', 'POST')
        # Sufficient actual HTTP records for the browser pagination acceptance.
        for _ in range(55):
            self.user.request('observability/logs', expected=403)
        def ready():
            data = self.admin.request('admin/overview?range=15m')['data']
            return data if all(row['value'] is not None for row in data['key_metrics']['items']) and data['logs']['status'] == 'healthy' and data['events']['status'] == 'healthy' else None
        overview = wait_until(ready, 'real overview data', 180)
        for metric in ['gopulse_redis_up', 'gopulse_redis_connected_clients']:
            wait_until(lambda: any(series['points'] for series in self.admin.request('observability/metrics?metric='+metric+'&range=15m')['data']['series']), 'real '+metric+' samples', 180)
        (self.work/'overview.json').write_text(json.dumps(overview, indent=2))
        env = dict(os.environ, GOPULSE_BASE_URL=base, GOPULSE_ADMIN_USERNAME=self.admin_name,
                   GOPULSE_USER_USERNAME=self.user_name, GOPULSE_DEMOTION_USERNAME=demoted_name,
                   GOPULSE_ACCEPTANCE_PASSWORD=self.auth_password, GOPULSE_USER_ID=str(user_id),
                   GOPULSE_DEMOTION_ID=str(demoted_id), GOPULSE_ADMIN_ID=str(admin_id),
                   GOPULSE_SCREENSHOT_DIR=str((previous or self.work)/'screenshots'))
        if previous:
            # Only reuse explicit successful receipts from this unchanged frontend candidate.
            assert '4 passed' in (previous/'admin-browser.log').read_text()
            assert '2 passed' in (previous/'dashboard-visual-browser.log').read_text()
            self.browser(env, 'visual-browser.log', 'e2e/admin-visual.spec.ts')
        else:
            self.browser(env, 'admin-browser.log', 'e2e/admin-frontend.spec.ts')
            # The preceding spec deliberately demotes this fixture; restore for its next independent scenario.
            self.admin.request(f'admin/users/{demoted_id}/role', 'PUT', {'role': 'super_admin'})
            self.browser(env, 'dashboard-visual-browser.log', 'e2e/dashboard.spec.ts', 'e2e/admin-visual.spec.ts')
        # A real unavailable metric source proves partial rendering, without expanding to a resilience campaign.
        self.owned_id('victoriametrics')
        self.compose('stop', 'victoriametrics')
        self.browser(dict(env, GOPULSE_PARTIAL_SECTIONS='key_metrics,components,plugins'), 'partial-browser.log', 'e2e/dashboard-partial.spec.ts')
        for service in ['frontend', 'admin-frontend']:
            self.compose('exec', '-T', service, 'sh', '-c',
                         "! grep -rE 'http://(backend|monitor|router|elasticsearch|victoriametrics):|MONITOR_API_TOKEN|gopulse-(logs|events)-v1-read|redis.default.svc.cluster.local' /usr/share/nginx/html")
            self.compose('exec', '-T', service, 'sh', '-c',
                         '! grep -rF "$1" /usr/share/nginx/html', 'scan', self.secret)
        (self.work/'result.json').write_text(json.dumps({
            'version': self.version, 'revision': command(['git', 'rev-parse', 'HEAD']).stdout.decode().strip(),
            'candidate': 'working-tree frontend changes at the recorded base revision',
            'docker_os': server['OSType'], 'docker_arch': server['Architecture'],
            'viewports': ['1440x1000', '820x1000', '390x844'], 'result': 'passed',
            'project': self.project, 'screenshots': env['GOPULSE_SCREENSHOT_DIR'],
            'reused_receipts': str(previous) if previous else None,
        }, indent=2))
        self.mark('real admin, ordinary user, demotion, overview/partial, four visual pages, responsive navigation and bundle scan')


if __name__ == '__main__':
    from pathlib import Path
    parser = argparse.ArgumentParser()
    parser.add_argument('--resume-presentation', type=Path, help='Prior evidence for the same unchanged frontend; rerun only the corrected visual spec and pending partial gate')
    args = parser.parse_args()
    acceptance = VisualAcceptance()
    print('Evidence: '+str(acceptance.work), flush=True)
    try:
        acceptance.run(args.resume_presentation.resolve() if args.resume_presentation else None)
    finally:
        acceptance.cleanup()
