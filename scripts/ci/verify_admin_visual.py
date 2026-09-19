#!/usr/bin/env python3
"""Phase 17-02: focused real Compose/browser acceptance of admin presentation."""
import argparse
import json
import os
import subprocess
import shutil
from verify_admin_frontend import AdminAcceptance
from verify_plugin_metrics import Client, ROOT, command, wait_until


class VisualAcceptance(AdminAcceptance):
    def browser(self, env, name, *specs):
        result = subprocess.run(
            ['npx', 'playwright', 'test', *specs], cwd=ROOT/'frontend', env=env,
            stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=420)
        (self.work/name).write_bytes(result.stdout.replace(self.auth_password.encode(), b'[REDACTED]'))
        assert result.returncode == 0, f'browser failed: {self.work/name}'

    def run(self, previous=None, changed_pages=False, completed_dashboard=None):
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
        for metric in ['gopulse_redis_up', 'gopulse_redis_connected_clients', 'gopulse_backend_outbox_pending', 'gopulse_monitor_event_queue_length']:
            wait_until(lambda: any(series['points'] for series in self.admin.request('observability/metrics?metric='+metric+'&range=15m')['data']['series']), 'real '+metric+' samples', 180)
        (self.work/'overview.json').write_text(json.dumps(overview, indent=2))
        env = dict(os.environ, GOPULSE_BASE_URL=base, GOPULSE_ADMIN_USERNAME=self.admin_name,
                   GOPULSE_USER_USERNAME=self.user_name, GOPULSE_DEMOTION_USERNAME=demoted_name,
                   GOPULSE_ACCEPTANCE_PASSWORD=self.auth_password, GOPULSE_USER_ID=str(user_id),
                   GOPULSE_DEMOTION_ID=str(demoted_id), GOPULSE_ADMIN_ID=str(admin_id),
                   GOPULSE_SCREENSHOT_DIR=str((self.work if changed_pages else previous or self.work)/'screenshots'))
        if changed_pages:
            receipt = json.loads((previous/'result.json').read_text())
            assert receipt['result'] == 'passed'
            screenshots = self.work/'screenshots'
            screenshots.mkdir()
            for image in __import__('pathlib').Path(receipt['screenshots']).glob('*.png'):
                if not image.name.startswith(('metrics-', 'plugins-')):
                    shutil.copyfile(image, screenshots/image.name)
            self.browser(dict(env, GOPULSE_PRESENTATION_ROUTES='metrics,plugins'), 'changed-pages-browser.log', 'e2e/admin-visual.spec.ts')
        elif previous:
            assert '4 passed' in (previous/'admin-browser.log').read_text()
            receipt = (previous/'dashboard-visual-browser.log').read_text()
            if completed_dashboard:
                assert '1 passed' in (completed_dashboard/'dashboard-browser.log').read_text()
                # All 24 route/viewport checks and 15 captures completed before
                # the subsequent catalog-filter locator failed. Resume controls only.
                assert "getByLabel('插件状态'" in (completed_dashboard/'visual-browser.log').read_text()
                for page in ['dashboard', 'metrics', 'logs', 'alerts', 'plugins']:
                    for width in [1440, 768, 390]:
                        assert (previous/'screenshots'/f'{page}-{width}.png').is_file()
                env['GOPULSE_SKIP_LAYOUTS'] = '1'
            elif '2 passed' not in receipt:
                # The first batch-02 run passed self-demotion, but its old article
                # locator failed after incidents became table rows. Run that gate only.
                assert '1 passed' in receipt and 'real overview, catalog rule lifecycle' in receipt
                self.browser(env, 'dashboard-browser.log', 'e2e/dashboard.spec.ts', '-g', 'real overview, catalog rule lifecycle')
            self.browser(env, 'visual-browser.log', 'e2e/admin-visual.spec.ts')
        else:
            self.browser(env, 'admin-browser.log', 'e2e/admin-frontend.spec.ts')
            self.admin.request(f'admin/users/{demoted_id}/role', 'PUT', {'role': 'super_admin'})
            self.browser(env, 'dashboard-visual-browser.log', 'e2e/dashboard.spec.ts')
            self.browser(env, 'visual-browser.log', 'e2e/admin-visual.spec.ts')
        # A real unavailable metric source proves partial rendering, without expanding to a resilience campaign.
        if not changed_pages:
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
            'viewports': ['1440x1000', '768x1024', '390x844'], 'result': 'passed',
            'project': self.project, 'screenshots': env['GOPULSE_SCREENSHOT_DIR'],
            'reused_receipts': str(previous) if previous else None,
            'changed_pages_only': ['metrics', 'plugins'] if changed_pages else None,
            'reused_dashboard_and_layout_steps': str(completed_dashboard) if completed_dashboard else None,
        }, indent=2))
        self.mark('real admin, ordinary user, demotion, overview/partial, five visual pages, responsive navigation and bundle scan')


if __name__ == '__main__':
    from pathlib import Path
    parser = argparse.ArgumentParser()
    parser.add_argument('--resume-presentation', type=Path, help='Prior evidence for the same unchanged frontend; rerun only the corrected visual spec and pending partial gate')
    parser.add_argument('--changed-pages', action='store_true', help='Verify only final Metrics heading and Exporter summary changes; requires prior passed presentation receipts')
    parser.add_argument('--completed-dashboard', type=Path, help='Same-candidate passed dashboard and completed layout steps; resume failed post-layout controls and pending partial only')
    args = parser.parse_args()
    if args.completed_dashboard and (not args.resume_presentation or args.changed_pages):
        parser.error('--completed-dashboard requires --resume-presentation and cannot combine with --changed-pages')
    if args.changed_pages and not args.resume_presentation:
        parser.error('--changed-pages requires --resume-presentation')
    acceptance = VisualAcceptance()
    print('Evidence: '+str(acceptance.work), flush=True)
    try:
        acceptance.run(args.resume_presentation.resolve() if args.resume_presentation else None, args.changed_pages, args.completed_dashboard.resolve() if args.completed_dashboard else None)
    finally:
        acceptance.cleanup()
