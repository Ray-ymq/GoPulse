#!/usr/bin/env python3
"""Real Linux lifecycle acceptance; never print private installation contents."""
import argparse
import fcntl
import hashlib
import json
import os
from pathlib import Path
import signal
import shutil
import socket
import subprocess
import tempfile
import time
import urllib.request

ROOT = Path(__file__).resolve().parents[2]

def docker(*args):
    return subprocess.check_output(['docker', *args], stderr=subprocess.PIPE, text=True).strip()

def main():
    p = argparse.ArgumentParser()
    p.add_argument('--platform', choices=['linux/amd64'], required=True)
    p.add_argument('--manifest', type=Path, default=ROOT/'dist/release-manifest.json')
    p.add_argument('--acceptance-image', help='immutable local Linux amd64 browser image ID; clean install only')
    modes = p.add_mutually_exclusive_group(required=True)
    modes.add_argument('--clean-install', action='store_true')
    modes.add_argument('--failure-matrix', action='store_true')
    a = p.parse_args()
    if a.acceptance_image and not a.clean_install: p.error('--acceptance-image requires --clean-install')
    browser = None
    m = json.loads(a.manifest.read_text())
    image = m['lifecycle']['ref'].split('@')[0]+'@'+m['lifecycle']['platforms'][a.platform]
    server = json.loads(docker('version', '--format', '{{json .Server}}'))
    assert server['Os']+'/'+server['Arch'] == a.platform
    docker('pull', image)
    endpoint = '/var/run/docker.sock'
    with tempfile.TemporaryDirectory(prefix='gopulse lifecycle ') as tmp:
        install = Path(tmp)/'installation with spaces'
        install.mkdir(mode=0o700)
        sock = socket.socket(); sock.bind(('127.0.0.1', 0)); port = sock.getsockname()[1]; sock.close()
        base = ['docker', 'run', '--rm', '--network', 'host', '--read-only', '--cap-drop', 'ALL',
                '--security-opt', 'no-new-privileges', '--user', f'{os.getuid()}:{os.getgid()}',
                '--group-add', str(os.stat(endpoint).st_gid), '--tmpfs', '/tmp',
                '-v', endpoint+':'+endpoint, '-v', str(a.manifest.parent.resolve())+':/bundle:ro',
                '-v', str(install)+':'+str(install), image]
        common = ['--install', str(install), '--endpoint', 'unix://'+endpoint]
        tool_env = {**os.environ, 'GOPULSE_BUNDLE_DIR':str(a.manifest.parent.resolve()),
                    'GOPULSE_INSTALL_DIR':str(install), 'GOPULSE_TOOL_UID':str(os.getuid()),
                    'GOPULSE_TOOL_GID':str(os.getgid()), 'GOPULSE_SOCKET_GID':str(os.stat(endpoint).st_gid)}
        tool_base = ['docker','compose','-p','gopulse-lifecycle-test-'+os.urandom(4).hex(),
                     '-f',str((a.manifest.parent/'compose.yaml').resolve()),'run','--rm','-T','--no-deps','lifecycle']

        outputs = []
        def call(command, *args, expected=0):
            result = subprocess.run([*(tool_base if a.clean_install else base), command, *common, *args], env=tool_env, capture_output=True, text=True, timeout=900)
            outputs.append(result.stdout+result.stderr)
            assert result.returncode == expected, f'{command}: exit {result.returncode}, expected {expected}; '+result.stdout+result.stderr
            stream = result.stdout if result.returncode == 0 else result.stderr
            value = json.loads(next(line for line in reversed(stream.splitlines()) if line.startswith('{')))
            return value
        def snapshot():
            ids = docker('ps', '-aq', '--filter', 'label=com.docker.compose.project='+state['project']).split()
            objects = json.loads(docker('inspect', *ids))
            # Ignore independent healthcheck timestamps; compare lifecycle state.
            runtime = [(c['Id'], c['Image'], c['State']['Status'], c['State']['StartedAt'], c['RestartCount']) for c in objects]
            mysql = next(c['Id'] for c in objects if c['Config']['Labels']['com.docker.compose.service']=='mysql')
            business = docker('exec', mysql, 'sh', '-c', 'MYSQL_PWD="$MYSQL_PASSWORD" mysql -u "$MYSQL_USER" "$MYSQL_DATABASE" -N -e "SELECT COUNT(*) FROM users; SELECT COUNT(*) FROM posts; SELECT COUNT(*) FROM bootstrap_super_admin;"')
            return sorted(runtime), business, (install/'state.json').read_bytes(), (install/'secrets.json').read_bytes()
        state = None
        try:
            call('doctor', '--port', str(port))
            call('init', '--port', str(port))
            state = json.loads((install/'state.json').read_text())
            secrets = json.loads((install/'secrets.json').read_text())
            original = (install/'secrets.json').read_bytes()
            assert (install/'state.json').stat().st_mode & 0o777 == 0o600
            assert (install/'secrets.json').stat().st_mode & 0o777 == 0o600
            call('init', '--port', str(port), expected=14)
            assert (install/'secrets.json').read_bytes() == original
            call('verify', expected=19)
            if a.clean_install:
                call('up')
                before = snapshot(); call('verify'); assert snapshot() == before, 'verify modified Docker/business/files'
                status = call('status'); assert status['phase'] == 'ready'
                for path in ['/', '/admin/', '/health']:
                    with urllib.request.urlopen(status['edge']+path, timeout=10) as response: assert response.status == 200
                if a.acceptance_image:
                    from frontend_bundle_browser import run_browser
                    browser = run_browser(install, state, status, secrets, a.acceptance_image, docker)
                call('logs', '--service', 'backend', '--tail', '10')
                call('logs', '--service', 'arbitrary-container', expected=2)
                call('down'); call('down'); call('up'); call('verify')
            else:
                with open(install/'.lock', 'r+') as lock:
                    fcntl.flock(lock, fcntl.LOCK_EX|fcntl.LOCK_NB)
                    call('up', expected=16)
                call('doctor', '--endpoint', 'unix:///missing-daemon', expected=11)
                # Fault fixtures are kept outside the immutable candidate.
                invalid = Path(tmp)/'invalid bundle'
                for name in ['README.md','compose.yaml','release-manifest.json','checksums','deploy/product/compose.yaml']:
                    target = invalid/name; target.parent.mkdir(parents=True, exist_ok=True)
                    shutil.copyfile(a.manifest.parent/name, target)
                (invalid/'README.md').write_text('tampered')
                mount = next(i for i,v in enumerate(base) if v.endswith(':/bundle:ro'))
                original_mount = base[mount]; base[mount] = str(invalid)+':/bundle:ro'
                try: call('doctor', expected=10)
                finally: base[mount] = original_mount
                # A private 1 MiB tmpfs proves the real statfs low-disk exit.
                original_base = base[:]
                position = base.index(str(install)+':'+str(install))
                del base[position-1:position+1]
                position = base.index('--user')+1; base[position] = '0:0'
                base[-1:-1] = ['--mount', 'type=tmpfs,destination='+str(install)+',tmpfs-size=1048576,tmpfs-mode=0700']
                try: call('doctor', '--port', str(port), expected=13)
                finally: base[:] = original_base

                sock = socket.socket(); sock.bind(('127.0.0.1', port)); sock.listen()
                try: call('doctor', '--port', str(port), expected=15)
                finally: sock.close()
                os.chmod(install, 0o755)
                try: call('status', expected=14)
                finally: os.chmod(install, 0o700)
                call('down', '--purge', '--confirm', 'foreign', expected=2)
                # A foreign same-name volume must never be adopted or removed.
                foreign = state['project']+'_mysql_data'
                docker('volume', 'create', foreign)
                try:
                    call('up', expected=17); call('down', expected=17)
                    assert json.loads(docker('volume', 'inspect', foreign))[0]['Name'] == foreign
                finally: docker('volume', 'rm', foreign)
                for sig in [signal.SIGINT, signal.SIGTERM]:
                    name = 'gopulse-lifecycle-signal-'+os.urandom(4).hex()
                    argv = base[:3]+['--name', name]+base[3:]
                    proc = subprocess.Popen([*argv, 'up', *common], stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
                    deadline = time.monotonic()+60
                    while time.monotonic()<deadline:
                        current = json.loads((install/'state.json').read_text())
                        if current['phase']=='pull' and current['operation_id'] != state['operation_id']: break
                        if proc.poll() is not None: raise AssertionError('up exited before signal injection')
                        time.sleep(.1)
                    else: raise AssertionError('up did not reach pull')
                    docker('kill', '--signal', signal.Signals(sig).name, name)
                    stdout, stderr = proc.communicate(timeout=30); outputs.append(stdout+stderr)
                    assert proc.returncode == 20, 'signal did not produce stable interrupted exit'
                    state = json.loads((install/'state.json').read_text()); assert state['phase']=='interrupted'
                    call('down')
            for text in outputs:
                assert state['installation_token'] not in text
                for key, secret in secrets.items():
                    if any(word in key for word in ('PASSWORD','TOKEN','SECRET')): assert secret not in text, 'secret leaked'
            print(json.dumps({'schema':1, 'platform':a.platform, 'manifest_sha256':hashlib.sha256(a.manifest.read_bytes()).hexdigest(),
                              'mode':'clean-install' if a.clean_install else 'failure-matrix', 'status':'passed', 'browser':browser}))
        finally:
            if state is not None:
                call('down', '--purge', '--confirm', state['project'])

if __name__ == '__main__':
    main()
