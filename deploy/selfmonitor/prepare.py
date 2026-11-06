#!/usr/bin/env python3
"""Prepare private collection credentials from the local product environment."""
import argparse
import hashlib
import base64
import json
import os
from pathlib import Path
import secrets
import re
import subprocess


def environment(path):
    values = {}
    for number, raw in enumerate(path.read_text().splitlines(), 1):
        line = raw.strip()
        if not line or line.startswith('#'):
            continue
        if line.startswith('export '):
            line = line[7:].strip()
        key, separator, value = line.partition('=')
        if not separator or not key.strip().replace('_', '').isalnum():
            raise ValueError(f'Invalid environment entry at line {number}')
        value = value.strip()
        if len(value) >= 2 and value[0] == value[-1] and value[0] in "\"'":
            value = value[1:-1]
        values[key.strip()] = value
    return values


def prepare(root, product_env):
    values = environment(product_env)
    tokens = {}
    for component in ('backend', 'business-worker', 'search-indexer', 'monitor', 'marshaller'):
        key = component.upper().replace('-', '_') + '_METRICS_TOKEN'
        token = values.get(key, '')
        if len(token) < 32 or any(c.isspace() for c in token):
            raise ValueError(f'{key} must contain at least 32 non-whitespace characters')
        tokens[component + '_token'] = token
    username = values.get('VICTORIAMETRICS_USERNAME', '')
    password = values.get('VICTORIAMETRICS_PASSWORD', '')
    if not username or not password or ':' in username:
        raise ValueError('Valid VictoriaMetrics username/password are required')

    if not values.get('REDIS_PASSWORD'):
        raise ValueError('REDIS_PASSWORD is required')
    folder = root / 'secrets'
    folder.mkdir(mode=0o700, exist_ok=True)
    folder.chmod(0o700)

    def save(name, value, replace=False):
        path = folder / name
        if path.exists() and not replace:
            return path.read_text()
        # Files are readable by container UIDs; the host directory is private.
        temporary = folder / (name + '.tmp')
        with temporary.open('w') as output:
            output.write(value)
        temporary.chmod(0o444)
        os.replace(temporary, path)
        return value

    for name, token in tokens.items():
        save(name, token, replace=True)
    save('grafana_password', secrets.token_urlsafe(32))
    mysql_password = save('mysql_password', secrets.token_hex(24))
    save('mysql.cnf', '[client]\nhost=mysql\nport=3306\nuser=selfmonitor\npassword=' + mysql_password + '\n')
    redis_password = save('redis_password', secrets.token_hex(24))
    default_hash = hashlib.sha256(values['REDIS_PASSWORD'].encode()).hexdigest()
    readonly_hash = hashlib.sha256(redis_password.encode()).hexdigest()
    save('redis.acl', 'user default on #' + default_hash + ' ~* &* +@all\n'
         'user selfmonitor on #' + readonly_hash + ' +ping +info +config|get +client|list +client|setname '
         '+slowlog|get +slowlog|len +latency|latest +latency|histogram +memory|stats\n', replace=True)
    save('redis_passwords.json', json.dumps({'redis://selfmonitor@redis:6379': redis_password}), replace=True)
    vm_token = save('victoriametrics_token', secrets.token_urlsafe(32))
    basic = base64.b64encode((username + ':' + password).encode()).decode()
    config = {'users': [{'bearer_token': vm_token, 'url_map': [{
        'src_paths': ['/proxy/metrics'], 'drop_src_path_prefix_parts': 1,
        'url_prefix': 'http://victoriametrics:8428',
        'headers': ['Authorization: Basic ' + basic],
    }]}]}
    # JSON is a YAML subset; encoding prevents credentials changing structure.
    save('vmauth.yaml', json.dumps(config, indent=2) + '\n', replace=True)


def provision(root):
    """Create bounded read-only accounts in this launcher's MySQL and Redis."""
    def container(service):
        identity = subprocess.check_output([str(root / 'product.sh'), 'ps', '-q', service], text=True).strip()
        if not identity or "\n" in identity:
            raise ValueError(f'Exactly one running {service} container is required')
        return identity

    folder = root / 'secrets'
    password = (folder / 'mysql_password').read_text()
    if not re.fullmatch(r'[a-f0-9]{48}', password):
        raise ValueError('Generated MySQL collection password is invalid')
    sql = ("CREATE USER IF NOT EXISTS 'selfmonitor'@'%' IDENTIFIED BY '" + password + "';\n"
           "ALTER USER 'selfmonitor'@'%' IDENTIFIED BY '" + password + "' WITH MAX_USER_CONNECTIONS 3;\n"
           "GRANT PROCESS, REPLICATION CLIENT, SELECT ON *.* TO 'selfmonitor'@'%';\n")
    subprocess.run(['docker', 'exec', '-i', container('mysql'), 'sh', '-c',
                    'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" exec mysql --user=root'],
                   input=sql.encode(), check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    commands = ['ACL', 'SETUSER', 'selfmonitor', 'reset', 'on',
                '>' + (folder / 'redis_password').read_text(),
                '+ping', '+info', '+config|get', '+client|list', '+client|setname', '+slowlog|get',
                '+slowlog|len', '+latency|latest', '+latency|histogram', '+memory|stats']
    encoded = [value.encode() for value in commands]
    resp = b'*' + str(len(encoded)).encode() + b'\r\n'
    for value in encoded:
        resp += b'$' + str(len(value)).encode() + b'\r\n' + value + b'\r\n'
    subprocess.run(['docker', 'exec', '-i', container('redis'), 'sh', '-c',
                    'REDISCLI_AUTH="$REDIS_PASSWORD" exec redis-cli --no-auth-warning --pipe'],
                   input=resp, check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--product-env', type=Path, default=Path(__file__).resolve().parents[2] / '.env')
    parser.add_argument('--provision', action='store_true', help='Create read-only accounts after the product MySQL/Redis are ready')
    args = parser.parse_args()
    try:
        root = Path(__file__).resolve().parent
        prepare(root, args.product_env)
        if args.provision:
            provision(root)
    except (OSError, ValueError, subprocess.SubprocessError) as error:
        parser.exit(1, f'Cannot prepare self-monitoring credentials: {type(error).__name__}; check configuration, file permissions and service readiness.\n')
    print('Private credentials prepared in deploy/selfmonitor/secrets; no credentials were printed.')
