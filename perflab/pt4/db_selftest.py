"""创建临时 MySQL 容器验证锁与 SQL 摘要；结束后删除该容器，不访问业务库。"""
import argparse
import json
import os
import secrets
import subprocess
import time
from common import ROOT, STATE, atomic_json


def main(perf=False):
    os.umask(0o077)
    directory = STATE / 'db-selftest' / str(time.time_ns())
    directory.mkdir(parents=True)
    password = secrets.token_hex(24)
    envfile = directory / 'mysql.env'
    envfile.write_text('MYSQL_ROOT_PASSWORD=' + password + '\nMYSQL_DATABASE=pt4_fixture\nMYSQL_ROOT_HOST=%\n')
    name = 'gopulse-pt4-fixture-' + directory.name
    passed = False
    try:
        subprocess.run(['docker', 'run', '--detach', '--name', name, '--label', 'gopulse.pt4.fixture=true',
                        '--memory', '768m', '--memory-swap', '768m', '--cpus', '1', '--restart', 'no',
                        '--tmpfs', '/var/lib/mysql:rw,size=512m', '--env-file', str(envfile),
                        '--publish', '127.0.0.1::3306', 'mysql:8.4.0'], check=True, stdout=subprocess.DEVNULL)
        for _ in range(120):
            result = subprocess.run(['docker', 'exec', name, 'mysqladmin', '--host=127.0.0.1', 'ping', '--silent'], capture_output=True)
            if result.returncode == 0:
                break
            time.sleep(.5)
        else:
            raise RuntimeError('独立 MySQL fixture 未就绪')
        port = subprocess.check_output(['docker', 'port', name, '3306/tcp'], text=True).strip().rsplit(':', 1)[1]
        dsn = directory / 'dsn'
        dsn.write_text('root:' + password + '@tcp(127.0.0.1:' + port + ')/pt4_fixture')
        # 入口只传私有文件路径，密码不进入测试输出。
        with (directory / 'test.log').open('wb') as log:
            result = subprocess.run(['go', '-C', 'perflab', 'test', '-race', '-count=1', '-v', './cmd/dbdiag'],
                                    cwd=ROOT, env=dict(os.environ, PT4_FIXTURE_DSN_FILE=str(dsn)), stdout=log, stderr=log)
        if result.returncode:
            raise RuntimeError('独立数据库验证失败，请检查 ' + str(directory / 'test.log'))
        if perf:
            from profiles import capture_mysql
            workload = subprocess.Popen(['docker', 'exec', name, 'sh', '-c',
                'MYSQL_PWD="$MYSQL_ROOT_PASSWORD" mysql -uroot pt4_fixture -e "SELECT BENCHMARK(30000000,SHA2(\'fixture\',256))"'],
                stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            try:
                evidence = capture_mysql(directory / 'perf', 3, name)
                atomic_json(directory / 'perf-result.json', evidence)
            finally:
                workload.terminate(); workload.wait(timeout=5)
        passed = True
    finally:
        removed = subprocess.run(['docker', 'rm', '--force', '--volumes', name], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, check=False)
        atomic_json(directory / 'result.json', {'passed': passed, 'log': str(directory / 'test.log'), 'container_removed': removed.returncode == 0})
    if removed.returncode:
        raise RuntimeError('独立验证容器清理失败：' + name)
    print(json.dumps({'passed': True, 'directory': str(directory)}, ensure_ascii=False))


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--perf', action='store_true', help='同时验证独立 MySQL 进程的 CPU 栈采集')
    main(parser.parse_args().perf)
