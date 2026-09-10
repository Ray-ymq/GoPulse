#!/usr/bin/env python3
"""Reconcile dedicated collectors on a single explicitly selected Compose project.

Admin credentials are read from a caller-owned 0600 JSON file, never arguments,
Exporter environment, logs, or persisted candidate state. Both new and existing
volumes use this same operation. Failures leave the candidate retryable.
"""
import argparse
import base64
import fcntl
import json
import os
from pathlib import Path
import re
import secrets
import stat
import subprocess
import sys
import urllib.request
import urllib.error

ROOT = Path(__file__).resolve().parents[2]

class SafeFailure(Exception):
    pass


class PermissionDenied(SafeFailure):
    pass


def command(args, data=None, sql=False):
    result = subprocess.run(args, input=data, stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=35)
    if result.returncode:
        if sql and any(('ERROR '+str(code)+' ').encode() in result.stderr for code in [1044,1142,1227]):
            raise PermissionDenied('collector operation not permitted')
        raise SafeFailure('deployment operation failed')
    return result.stdout


def private_read(path):
    info = path.lstat()
    if not stat.S_ISREG(info.st_mode) or stat.S_IMODE(info.st_mode) != 0o600 or info.st_uid != os.getuid():
        raise SafeFailure('credential/state file must be owned by the caller with mode 0600')
    if info.st_size > 16384:
        raise SafeFailure('credential/state file too large')
    return json.loads(path.read_bytes())


def save(path, value):
    temporary = path.with_name(path.name + '.tmp-' + secrets.token_hex(8))
    fd = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, 'w') as stream:
        json.dump(value, stream, separators=(',', ':'))
        stream.flush()
        os.fsync(stream.fileno())
    os.replace(temporary, path)
    directory = os.open(path.parent, os.O_RDONLY | os.O_DIRECTORY)
    try:
        os.fsync(directory)
    finally:
        os.close(directory)


class Reconciler:
    def __init__(self, project, admin_file, state_dir):
        if not re.fullmatch(r'[a-z0-9][a-z0-9_-]{0,62}', project):
            raise SafeFailure('invalid project')
        self.project = project
        self.admin = private_read(Path(admin_file))
        if not isinstance(self.admin, dict) or any(not isinstance(self.admin.get(key), str) or not self.admin[key] for key in ['mysql_root_password','mysql_database','rabbitmq_username','rabbitmq_password']):
            raise SafeFailure('invalid deployment credential document')
        self.directory = Path(state_dir).absolute() / project
        if self.directory.is_symlink():
            raise SafeFailure('invalid state directory')
        self.directory.mkdir(parents=True, exist_ok=True, mode=0o700)
        if stat.S_IMODE(self.directory.stat().st_mode) != 0o700:
            raise SafeFailure('state directory must have mode 0700')
        self.lock = open(self.directory / '.lock', 'a')
        os.chmod(self.directory / '.lock', 0o600)
        fcntl.flock(self.lock.fileno(), fcntl.LOCK_EX | fcntl.LOCK_NB)
        self.opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), NoRedirect())

    def container(self, service):
        ids = command(['docker', 'ps', '-q', '--filter', 'label=com.docker.compose.project='+self.project,
                       '--filter', 'label=com.docker.compose.service='+service]).decode().split()
        if len(ids) != 1:
            raise SafeFailure('exactly one running target service is required')
        info = json.loads(command(['docker', 'inspect', ids[0]]))[0]
        labels = info['Config']['Labels']
        if labels.get('com.docker.compose.project') != self.project or labels.get('com.docker.compose.service') != service:
            raise SafeFailure('target ownership mismatch')
        return ids[0], info

    def sql(self, username, password, query):
        cid, _ = self.container('mysql')
        # stdin carries credentials, not docker argv, persisted files or output.
        if '\n' in password or '\r' in password or '\x00' in password:
            raise SafeFailure('unsupported deployment credential encoding')
        script = 'IFS= read -r MYSQL_PWD; export MYSQL_PWD; exec mysql --protocol=socket -u "$1" -N -B'
        return command(['docker', 'exec', '-i', cid, 'sh', '-c', script, 'reconcile', username],
                       (password+'\n'+query+'\n').encode(), sql=True).decode().strip()

    def rabbit(self, path, method='GET', payload=None, credentials=None, expected=200):
        _, info = self.container('rabbitmq')
        network = self.project+'_business'
        ip = info['NetworkSettings']['Networks'].get(network, {}).get('IPAddress')
        if not ip:
            raise SafeFailure('fixed business network unavailable')
        user, password = credentials or (self.admin['rabbitmq_username'], self.admin['rabbitmq_password'])
        req = urllib.request.Request('http://'+ip+':15672/api/'+path, method=method,
            data=None if payload is None else json.dumps(payload).encode(), headers={
                'Content-Type': 'application/json',
                'Authorization': 'Basic '+base64.b64encode((user+':'+password).encode()).decode()})
        try:
            response = self.opener.open(req, timeout=10)
        except urllib.error.HTTPError as error:
            if error.code == expected:
                error.close()
                return None
            error.close()
            raise SafeFailure('management operation rejected') from None
        with response:
            raw = response.read((1 << 20)+1)
            if response.status != expected or len(raw) > 1 << 20:
                raise SafeFailure('management response rejected')
            return json.loads(raw) if raw else None

    def monitor(self, path, payload=None, method=None):
        cid, _ = self.container('monitor')
        # The deployed Monitor token remains in that container. JSON candidates
        # go through stdin into a private temporary file and are removed on exit.
        script = '''umask 077; file=$(mktemp); trap 'rm -f "$file"' EXIT
cat > "$file"
if [ "$1" = GET ]; then
 wget -qO- --header "Authorization: Bearer $MONITOR_API_TOKEN" "http://127.0.0.1:9090/internal/v1/exporter-plugins$2"
else
 wget -qO- --header "Authorization: Bearer $MONITOR_API_TOKEN" --header 'Content-Type: application/json' --post-file "$file" "http://127.0.0.1:9090/internal/v1/exporter-plugins$2"
fi'''
        raw = command(['docker', 'exec', '-i', cid, 'sh', '-c', script, 'reconcile', method or ('POST' if payload else 'GET'), path],
                      b'' if payload is None else json.dumps(payload).encode())
        return json.loads(raw)

    def reconcile(self, source):
        state_path = self.directory / (source+'.json')
        username = 'gopulse_metrics'
        if state_path.exists():
            state = private_read(state_path)
            if state.get('project') != self.project or state.get('source') != source or state.get('username') != username:
                raise SafeFailure('candidate ownership mismatch')
        else:
            # Never adopt an existing same-name account, even if its grants look
            # correct. Record absence before saving a retryable candidate.
            if source == 'mysql':
                exists = self.sql('root', self.admin['mysql_root_password'], "SELECT COUNT(*) FROM mysql.user WHERE User='gopulse_metrics';")
                if exists != '0':
                    raise SafeFailure('unowned account conflict')
            else:
                self.rabbit('users/'+username, expected=404)
            state = {'project': self.project, 'source': source, 'username': username,
                     'password': secrets.token_hex(32), 'active': False}
            save(state_path, state)
        password = state['password']
        if not re.fullmatch('[0-9a-f]{64}', password):
            raise SafeFailure('invalid candidate')
        if source == 'mysql':
            admin = self.admin['mysql_root_password']
            exists = self.sql('root', admin, "SELECT COUNT(*) FROM mysql.user WHERE User='gopulse_metrics' AND Host='%';")
            if exists == '0':
                self.sql('root', admin, "CREATE USER 'gopulse_metrics'@'%' IDENTIFIED BY '"+password+"';")
            grants = self.sql(username, password, 'SHOW GRANTS;')
            if grants != 'GRANT USAGE ON *.* TO `gopulse_metrics`@`%`':
                raise SafeFailure('collector privileges differ from contract')
            self.sql(username, password, "SHOW GLOBAL STATUS LIKE 'Uptime'; SELECT @@GLOBAL.max_connections;")
            config = {'host': 'mysql', 'port': 3306, 'database': self.admin['mysql_database'], 'username': username}
        else:
            users = self.rabbit('users')
            if not any(user.get('name') == username for user in users):
                self.rabbit('users/'+username, 'PUT', {'password': password, 'tags': 'monitoring'}, expected=201)
            self.rabbit('vhosts', credentials=(username, password))
            user = self.rabbit('users/'+username)
            if user.get('tags') != ['monitoring']:
                raise SafeFailure('collector tags differ from contract')
            permissions = self.rabbit('users/'+username+'/permissions')
            if not permissions and not state['active']:
                self.rabbit('permissions/%2F/'+username, 'PUT', {'configure': '^$', 'write': '^$', 'read': '^$'}, expected=201)
                permissions = self.rabbit('users/'+username+'/permissions')
            if len(permissions) != 1 or any(permissions[0].get(k) != v for k,v in {'vhost':'/','configure':'^$','write':'^$','read':'^$'}.items()):
                raise SafeFailure('collector permissions differ from contract')
            self.rabbit('vhosts', credentials=(username, password))
            config = {'host': 'rabbitmq', 'management_port': 15672, 'vhost': '/', 'username': username}
        config.update(connect_timeout='1s', scrape_timeout='2s')
        payload = {'config': config, 'secrets': {'password': password}}
        self.monitor('/'+source+'-exporter/connection-test', payload)
        catalog = self.monitor('/catalog')['data']
        item = next(item for item in catalog if item['id'] == source+'-exporter')
        if not item['configured']:
            self.monitor('/'+source+'-exporter/install', payload)
        else:
            # Recognize an interrupted successful install only by matching the
            # private candidate and config in the owned revision; never overwrite.
            revision = item['revision']
            if not re.fullmatch('[0-9a-f]{32}', revision):
                raise SafeFailure('invalid installed revision')
            cid, _ = self.container('monitor')
            root = '/var/lib/gopulse-monitor/plugins/'+source+'-exporter/revisions/'+revision+'/'
            installed = json.loads(command(['docker','exec',cid,'cat',root+'secret.json']))
            public = json.loads(command(['docker','exec',cid,'cat',root+'config.json']))
            if installed != payload['secrets'] or public != config:
                raise SafeFailure('configured plugin differs from candidate')
        state['active'] = True
        save(state_path, state)


class NoRedirect(urllib.request.HTTPRedirectHandler):
    def redirect_request(self, *args, **kwargs):
        return None


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--project-name', required=True)
    parser.add_argument('--admin-file', required=True)
    parser.add_argument('--state-dir', default=str(ROOT/'.run/plugin-accounts'))
    parser.add_argument('--sources', choices=['mysql', 'rabbitmq', 'mysql,rabbitmq'], default='mysql,rabbitmq')
    args = parser.parse_args()
    try:
        reconciler = Reconciler(args.project_name, args.admin_file, args.state_dir)
        failed = False
        for source in args.sources.split(','):
            try:
                reconciler.reconcile(source)
                print(source+': account verified; dedicated Secret activated')
            except (SafeFailure, OSError, ValueError, KeyError, subprocess.SubprocessError, urllib.error.URLError):
                failed = True
                print(source+': reconciliation failed; candidate retained; no administrator credential published', file=sys.stderr)
        return int(failed)
    except (SafeFailure, OSError, ValueError, KeyError):
        print('reconciliation setup rejected', file=sys.stderr)
        return 1

if __name__ == '__main__':
    raise SystemExit(main())
