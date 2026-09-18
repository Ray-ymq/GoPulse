#!/usr/bin/env python3
"""Focused real MySQL migration checks, not the Phase-17-04 product receipt."""
import concurrent.futures
import json
import os
from pathlib import Path
import subprocess
import tempfile
import time
import uuid

ROOT = Path(__file__).resolve().parents[2]


def command(args, **kwargs):
    result = subprocess.run(args, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
                            timeout=180, **kwargs)
    if result.returncode:
        raise RuntimeError('migration verifier command failed: ' + args[0])
    return result.stdout.decode()


def main():
    token = uuid.uuid4().hex
    name = 'gopulse-migration-' + token[:12]
    password = 'migration-canary-' + token
    receipt = {'schema': 'gopulse.migration-state.v1', 'complete': False, 'checks': []}
    cid = None
    with tempfile.TemporaryDirectory(prefix=name+'-') as directory:
        work = Path(directory)
        executable = work/'migrate'
        command(['go', 'build', '-o', str(executable), './cmd/migrate'], cwd=ROOT/'backend')
        locker = work/'migration-lock'
        command(['go','build','-o',str(locker),str(ROOT/'scripts/ci/testdata/migration-lock.go')],cwd=ROOT/'backend')
        envfile = work/'mysql.env'
        envfile.write_text('MYSQL_ROOT_PASSWORD='+password+'\nMYSQL_DATABASE=gopulse\nMYSQL_USER=gopulse\nMYSQL_PASSWORD='+password+'\n')
        envfile.chmod(0o600)
        try:
            cid = command(['docker', 'run', '-d', '--name', name,
                           '--label', 'io.gopulse.migration-owner='+token,
                           '--env-file', str(envfile), '-p', '127.0.0.1::3306',
                           'mysql:8.4.0']).strip()
            info = json.loads(command(['docker', 'inspect', cid]))[0]
            port = info['NetworkSettings']['Ports']['3306/tcp'][0]['HostPort']
            env = dict(os.environ, GOPULSE_RUNTIME_MODE='host', MYSQL_HOST='127.0.0.1',
                       MYSQL_PORT=port, MYSQL_DATABASE='gopulse', MYSQL_USER='gopulse', MYSQL_PASSWORD=password)

            def sql(statement):
                return command(['docker', 'exec', '-i', cid, 'sh', '-c',
                                'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" exec mysql -uroot -N -B gopulse'],
                               input=statement.encode()).strip()

            def run(action, code=0):
                result = subprocess.run([str(executable), action], env=env, capture_output=True, timeout=180)
                assert password.encode() not in result.stdout+result.stderr, 'secret leaked'
                assert result.returncode == code, 'unexpected migration exit: '+str(result.returncode)
                return json.loads(result.stdout or result.stderr or b"{}")

            for _ in range(90):
                try:
                    sql('SELECT 1;')
                    break
                except RuntimeError:
                    time.sleep(1)
            else:
                raise RuntimeError('owned MySQL not ready')
            target = run('validate')['binary_target']
            assert run('status')['state'] == 'clean'
            # Both start on empty DB, including migration metadata initialization.
            with concurrent.futures.ThreadPoolExecutor(max_workers=2) as pool:
                results = list(pool.map(lambda _: run('up'), range(2)))
            assert sum(result['changed'] for result in results) == 1
            assert run('status')['state'] == 'current'
            assert not run('up')['changed']
            receipt['checks'].append('empty_concurrent_current_repeat')
            lock = subprocess.Popen([str(locker)],env=env,stdout=subprocess.PIPE,stderr=subprocess.PIPE)
            try:
                assert lock.stdout.readline() == b'locked\n'
                assert run('up',6)['reason'] == 'lock_timeout'
            finally:
                lock.wait(timeout=20)
            receipt['checks'].append('lock_timeout')
            baseline = sql('SHOW TABLES;')
            for version, code, state in [(11, 4, 'dirty'), (target+1, 5, 'ahead')]:
                sql(f'UPDATE schema_migrations SET version={version}, dirty={int(state=="dirty")};')
                assert run('status', code)['state'] == state
                assert run('up', code)['reason'] == state
                assert sql('SHOW TABLES;') == baseline
                assert sql('SELECT version FROM schema_migrations;') == str(version)
                receipt['checks'].append(state+'_rejected_unchanged')
            sql(f'UPDATE schema_migrations SET version={target}, dirty=0;')
            # Local-only reverse setup; never a product recovery mechanism.
            run('down')
            assert run('status')['state'] == 'behind'
            before_failed_apply = sql('SHOW TABLES;')
            # A real DDL permission error leaves the newly written dirty marker.
            sql("REVOKE CREATE ON gopulse.* FROM 'gopulse'@'%';")
            assert run('up', 8)['reason'] == 'apply_failure'
            assert run('status', 4)['state'] == 'dirty'
            assert sql('SHOW TABLES;') == before_failed_apply
            assert run('up', 4)['reason'] == 'dirty'
            receipt['checks'].append('apply_failure_keeps_dirty')
            sql("GRANT CREATE ON gopulse.* TO 'gopulse'@'%';")
            # Inject only the explicitly supported resume state for this test.
            sql('UPDATE schema_migrations SET version=12, dirty=1;')
            assert run('status', 4)['state'] == 'dirty'
            assert run('up')['state'] == 'current'
            assert sql('SHOW TABLES;') == baseline
            receipt['checks'].append('explicit_v12_resume_then_v13')
            receipt.update(binary_target=target, complete=True)
        finally:
            if cid:
                info = json.loads(command(['docker', 'inspect', cid]))[0]
                assert info['Config']['Labels'].get('io.gopulse.migration-owner') == token
                command(['docker', 'rm', '-fv', cid])
                receipt['cleanup'] = 'owned_container_and_anonymous_volumes_removed'
    print(json.dumps(receipt, indent=2))


if __name__ == '__main__':
    main()
