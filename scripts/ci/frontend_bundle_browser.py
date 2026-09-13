"""Focused browser matrix against an owned, real lifecycle Bundle installation."""
import json
import datetime
import urllib.parse
import os
import re
import subprocess
from pathlib import Path
from verify_plugin_metrics import Client, wait_until


def run_browser(install, state, status, secrets, image, docker):
    # Resolve a local immutable image ID, never execute an arbitrary mutable tag.
    info = json.loads(docker('image', 'inspect', image))[0]
    assert image == info['Id'], 'acceptance image must be an immutable local image ID'
    assert info['Os'] == 'linux' and info['Architecture'] == 'amd64'
    project = state['project']
    backend = docker('ps', '-q', '--filter', 'label=com.docker.compose.project='+project,
                     '--filter', 'label=com.docker.compose.service=backend').split()
    assert len(backend) == 1
    labels = json.loads(docker('inspect', backend[0]))[0]['Config']['Labels']
    runner_labels = info['Config'].get('Labels', {})
    assert runner_labels.get('org.opencontainers.image.version') == labels['org.opencontainers.image.version'], 'browser/Backend version mismatch'
    runner_revision = runner_labels.get('org.opencontainers.image.revision', '')
    assert re.fullmatch(r'[0-9a-f]{40}', runner_revision), 'browser source revision required'
    origin = status['edge']
    admin, user = Client(origin), Client(origin)
    token = os.urandom(6).hex()
    password = 'Acceptance-'+token+'-password'
    names = ['admin_'+token, 'user_'+token]
    for client, name in zip([admin, user], names):
        client.request('auth/register', 'POST', {'username': name, 'password': password}, 201)
    uid = admin.request('users/me')['data']['id']
    docker('exec', backend[0], '/usr/local/bin/admin-role', 'bootstrap', '--user-id', str(uid))
    wait_until(lambda: len(admin.request('exporter-plugins/catalog')['data']) == 6, 'six plugin catalog')
    # The maintained Bundle bootstraps its real Redis plugin. Require that
    # source rather than replacing installation state with synthetic records.
    wait_until(lambda: admin.request('exporter-plugins/redis-exporter')['data']['observed_state'] == 'running', 'Bundle Redis plugin running', 120)
    # Startup events are best-effort and may precede transport readiness.
    # Produce a real, auditable operation after the complete Bundle is ready.
    admin.request('exporter-plugins/redis-exporter/stop', 'POST')
    admin.request('exporter-plugins/redis-exporter/start', 'POST')
    def events():
        now = datetime.datetime.now(datetime.timezone.utc)
        query = urllib.parse.urlencode({'from':(now-datetime.timedelta(minutes=15)).isoformat().replace('+00:00', 'Z'), 'to':now.isoformat().replace('+00:00', 'Z'), 'limit':'50'})
        return admin.request('observability/events?'+query)['data']
    if not events():
        wait_until(events, 'real transported event', 120)
    env = {'GOPULSE_BASE_URL':origin, 'GOPULSE_OBSERVABILITY_ADMIN_USERNAME':names[0],
           'GOPULSE_OBSERVABILITY_USER_USERNAME':names[1], 'GOPULSE_OBSERVABILITY_PASSWORD':password,
           'GOPULSE_ADMIN_USERNAME':names[0], 'GOPULSE_ACCEPTANCE_PASSWORD':password}
    file = Path(install)/'browser-compose.json'
    file.write_text(json.dumps({'services': {'acceptance': {'profiles':['acceptance'], 'image':image,
        'pull_policy':'never', 'network_mode':'host', 'environment':env}}}))
    file.chmod(0o600)
    base = ['docker','compose','-p',project+'-browser','-f',str(file),'--profile','acceptance','run','--rm','--no-deps']
    checks = []
    try:
        for viewport in ['desktop','narrow']:
            result = subprocess.run([*base,'-e','GOPULSE_VIEWPORT='+viewport,'acceptance','e2e/frontend-product.spec.ts'], capture_output=True, text=True, timeout=300)
            assert result.returncode == 0, 'Bundle '+viewport+' browser failed: '+(result.stdout+result.stderr).replace(password,'[REDACTED]')
            checks.append(viewport)
        result = subprocess.run([*base,'acceptance','e2e/phase15-closure.spec.ts','--grep','create exact three-source'], capture_output=True, text=True, timeout=180)
        assert result.returncode == 0, 'three-source browser failed: '+(result.stdout+result.stderr).replace(password,'[REDACTED]')
        checks.append('three-source-create')
    finally:
        try:
            subprocess.run(['docker','compose','-p',project+'-browser','-f',str(file),
                            '--profile','acceptance','down','--remove-orphans'],
                           capture_output=True, text=True, timeout=60, check=True)
        finally:
            file.unlink(missing_ok=True)
    return {'runner_image':image, 'runner_revision':runner_revision, 'product_revision':labels['org.opencontainers.image.revision'], 'timezone':'Asia/Shanghai', 'checks':checks}
