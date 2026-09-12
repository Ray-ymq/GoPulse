#!/usr/bin/env python3
"""Two real runtime images and one published browser origin; owned fixtures only."""
import json
import os
import re
import subprocess
import sys
from verify_plugin_metrics import Acceptance, Client, ROOT, PATTERN, command, wait_until


def self_test():
    assert PATTERN.fullmatch('gopulse-p1401-012345abcdef')
    assert not PATTERN.fullmatch('gopulse')
    user = (ROOT/'frontend/src/router/index.ts').read_text()
    assert 'AdminLayout' not in user and 'Observability' not in user
    assert "createWebHistory('/admin/')" in (ROOT/'admin-frontend/src/router/index.ts').read_text()
    edge = (ROOT/'deploy/docker/frontend/nginx.conf').read_text()
    assert 'location = /admin { return 308 /admin/; }' in edge
    assert 'http://admin-frontend:8080' in edge
    assert edge.count('client_max_body_size 65m;') == 7
    assert 'requestData' in (ROOT/'admin-frontend/src/App.test.ts').read_text()
    print('Admin Frontend self-test passed (no Docker access).')


class AdminAcceptance(Acceptance):
    def __init__(self):
        super().__init__()
        self.version = (ROOT/'VERSION').read_text().strip()
        self.tag = 'admin-acceptance-'+self.token
        self.images = []
        values = dict(line.split('=', 1) for line in self.env_file.read_text().splitlines())
        values.update(GOPULSE_VERSION=self.version, GOPULSE_IMAGE_TAG=self.tag)
        self.env_file.write_text(''.join(f'{k}={v}\n' for k,v in values.items()))
        self.override = {'services': {}}
        self.override_file.write_text(json.dumps(self.override))

    def run(self):
        services = ['backend', 'business-worker', 'search-indexer', 'frontend', 'admin-frontend', 'monitor', 'router', 'marshaller']
        self.images = ['gopulse/'+s+':'+self.tag for s in services]
        result = self.compose('build', *services, check=False, timeout=1800)
        (self.work/'build.log').write_bytes(result.stdout+result.stderr)
        assert result.returncode == 0, 'image build failed; see '+str(self.work/'build.log')
        self.started = True
        result = self.compose('up', '-d', '--wait', '--wait-timeout', '420', 'frontend', 'monitor', 'marshaller', 'business-worker', 'search-indexer', check=False, timeout=500)
        (self.work/'startup.log').write_bytes(result.stdout+result.stderr)
        assert result.returncode == 0, 'startup failed; see '+str(self.work/'startup.log')
        base = 'http://'+self.compose('port', 'frontend', '8080').stdout.decode().strip()
        self.admin, self.user, demoted = Client(base), Client(base), Client(base)
        demoted_name = 'demote_'+self.token
        for client,name in [(self.admin,self.admin_name),(self.user,self.user_name),(demoted,demoted_name)]:
            client.request('auth/register','POST',{'username':name,'password':self.auth_password},201)
        admin_id = self.admin.request('users/me')['data']['id']
        self.compose('exec','-T','backend','/usr/local/bin/admin-role','bootstrap','--user-id',str(admin_id))
        demoted_id = demoted.request('users/me')['data']['id']
        self.admin.request(f'admin/users/{demoted_id}/role','PUT',{'role':'super_admin'})
        wait_until(lambda: len(self.admin.request('exporter-plugins/catalog')['data']) == 6, 'six plugin catalog')
        # HTTP-generated logs and plugin lifecycle events are the existing real sources.
        for _ in range(3):
            self.user.request('observability/logs', expected=403)
        env = dict(os.environ, GOPULSE_BASE_URL=base, GOPULSE_ADMIN_USERNAME=self.admin_name,
                   GOPULSE_USER_USERNAME=self.user_name, GOPULSE_DEMOTION_USERNAME=demoted_name,
                   GOPULSE_ACCEPTANCE_PASSWORD=self.auth_password)
        result = subprocess.run(['npm','run','test:e2e','--','admin-frontend.spec.ts'],cwd=ROOT/'frontend',env=env,
                                stdout=subprocess.PIPE,stderr=subprocess.STDOUT,timeout=300)
        (self.work/'browser.log').write_bytes(result.stdout.replace(self.auth_password.encode(),b'[REDACTED]'))
        assert result.returncode == 0, 'browser failed; see '+str(self.work/'browser.log')
        revision = command(['git','rev-parse','HEAD']).stdout.decode().strip()
        for service in ['frontend','admin-frontend']:
            info = json.loads(command(['docker','inspect',self.owned_id(service)]).stdout)[0]
            assert info['Config']['User'] == '101:101' and info['HostConfig']['ReadonlyRootfs']
            assert info['Config']['Labels']['org.opencontainers.image.version'] == self.version
            assert info['Config']['Labels']['org.opencontainers.image.revision'] == revision
            assert info['State']['Health']['Status'] == 'healthy'
            if service == 'admin-frontend':
                assert not info['HostConfig']['PortBindings']
                assert list(info['NetworkSettings']['Networks']) == [self.project+'_edge']
            scan = self.compose('exec','-T',service,'sh','-c',
                "! command -v node && ! command -v npm && test -z \"$(find /usr/share/nginx/html -name '*.map')\" && "
                "! grep -rE 'http://(backend|monitor|router|elasticsearch|victoriametrics):|MONITOR_API_TOKEN|gopulse-(logs|events)-v1-read|gopulse-observability-v1|gopulse-marshaller-metrics-v1' /usr/share/nginx/html")
            assert scan.returncode == 0
            self.compose('kill','--signal','SIGTERM',service)
            wait_until(lambda: json.loads(command(['docker','inspect',info['Id']]).stdout)[0]['State']['Status'] == 'exited', service+' SIGTERM',15)
            assert json.loads(command(['docker','inspect',info['Id']]).stdout)[0]['State']['ExitCode'] == 0
        self.mark('two independently built images; same-origin browser/auth/management; labels, isolation, read-only and SIGTERM')

    def cleanup(self):
        super().cleanup()
        for image in self.images:
            command(['docker','image','rm',image],check=False)


if __name__ == '__main__':
    if sys.argv[1:] == ['--self-test']:
        self_test()
    elif sys.argv[1:] == ['--existing-management']:
        acceptance = AdminAcceptance()
        print('Evidence: '+str(acceptance.work),flush=True)
        try:
            acceptance.run()
        finally:
            acceptance.cleanup()
    else:
        raise SystemExit('expected --self-test or --existing-management')
