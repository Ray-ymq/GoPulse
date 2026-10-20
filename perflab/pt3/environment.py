"""测试三本地环境操作；凭据只从被忽略的私有文件读取。"""
import hashlib
import json
import os
from pathlib import Path
import shlex
import signal
import subprocess
import time
import urllib.request

ROOT = Path(__file__).resolve().parents[2]
STATE = ROOT / '.run/pt3'
MYSQL = 'gopulse-loadtest-mysql-1'
CONTAINERS = [MYSQL, 'gopulse-loadtest-rabbitmq-1', 'gopulse-loadtest-elasticsearch-1']

def environment():
    values = dict(os.environ)
    for line in (STATE / 'test.env').read_text().splitlines():
        if line and not line.startswith('#') and '=' in line:
            key, value = line.split('=', 1)
            values[key] = shlex.split(value)[0] if value else ''
    return values

def sql(query, root=False, timeout=60):
    env = environment()
    user = 'root' if root else env['MYSQL_USER']
    password = env['MYSQL_ROOT_PASSWORD' if root else 'MYSQL_PASSWORD']
    command = ['docker', 'exec', '-i', '-e', 'MYSQL_PWD=' + password, MYSQL,
               'mysql', '-u' + user, '-N', '-B', env['MYSQL_DATABASE']]
    result = subprocess.run(command, input=query, capture_output=True, text=True, timeout=timeout)
    if result.returncode:
        raise RuntimeError(result.stderr.replace(password, '[redacted]'))
    return result.stdout

def http(path, method='GET', data=None):
    env = environment()
    url = env['ELASTICSEARCH_URL'].rstrip('/') + path
    request = urllib.request.Request(url, data=None if data is None else json.dumps(data).encode(), method=method)
    request.add_header('Content-Type', 'application/json')
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    with opener.open(request, timeout=120) as response:
        return json.load(response)

def stop_services():
    for name in ['server', 'worker', 'indexer']:
        path = STATE / 'logs' / (name + '.pid')
        if not path.exists():
            continue
        pid = int(path.read_text())
        try:
            expected = str(STATE / 'bin' / name).encode()
            if expected not in Path(f'/proc/{pid}/cmdline').read_bytes():
                raise RuntimeError('PID 不属于本轮测试服务')
            os.kill(pid, signal.SIGTERM)
            for _ in range(150):
                if not Path(f'/proc/{pid}').exists() or Path(f'/proc/{pid}/stat').read_text().split(') ',1)[1].startswith('Z '):
                    break
                time.sleep(.1)
            else:
                raise RuntimeError('服务未按时退出；未强制终止')
        except ProcessLookupError:
            pass
        except FileNotFoundError:
            pass
        path.unlink(missing_ok=True)

def start_services():
    env = environment()
    for name, instance in [('server','pt3-backend'),('worker','pt3-worker'),('indexer','pt3-indexer')]:
        process_env = dict(env, GOPULSE_INSTANCE_ID=instance)
        path = STATE / 'logs' / (name + '.pid')
        if path.exists() and Path('/proc/' + path.read_text().strip()).exists():
            raise RuntimeError('服务已经运行，拒绝重复启动')
        with (STATE / 'logs' / (name + '.log')).open('ab') as log:
            process = subprocess.Popen([str(STATE / 'bin' / name)], cwd=ROOT, env=process_env,
                                       stdout=log, stderr=log, stdin=subprocess.DEVNULL, start_new_session=True)
        path.write_text(str(process.pid))
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    for _ in range(100):
        try:
            with opener.open('http://127.0.0.1:18080/ready', timeout=1) as response:
                if response.status == 200:
                    return
        except OSError:
            pass
        time.sleep(.2)
    raise RuntimeError('业务服务尚未 ready；请检查私有日志')

def queues():
    output = subprocess.check_output(['docker','exec','gopulse-loadtest-rabbitmq-1','rabbitmqctl','-q','list_queues',
                                      'name','messages_ready','messages_unacknowledged','consumers'],text=True)
    return [line.split('\t') for line in output.splitlines() if line.startswith('gopulse.')]

def assert_drained():
    if any(int(row[1]) or int(row[2]) for row in queues()):
        raise RuntimeError('队列未清空，不能建立一致快照')
    if int(sql('SELECT COUNT(*) FROM business_event_requirements WHERE closed_at IS NULL;').strip()):
        raise RuntimeError('异步 requirements 未关闭')

def reindex():
    with (STATE / 'logs/reindex.log').open('ab') as log:
        subprocess.run([str(STATE/'bin/reindex')],cwd=ROOT,env=dict(environment(),SEARCH_REINDEX_BATCH='5000'),stdout=log,stderr=log,check=True,timeout=600)

def backup(name):
    env = environment()
    path = STATE/'snapshots'/(name+'.sql')
    if path.exists():
        raise RuntimeError('快照已存在，拒绝覆盖')
    with path.open('wb') as dump:
        result = subprocess.run(['docker','exec','-e','MYSQL_PWD='+env['MYSQL_PASSWORD'],MYSQL,'mysqldump',
                                 '-u'+env['MYSQL_USER'],'--single-transaction','--no-tablespaces','--set-gtid-purged=OFF',
                                 '--skip-dump-date',env['MYSQL_DATABASE']],stdout=dump,stderr=subprocess.PIPE,timeout=180)
    path.chmod(0o600)
    if result.returncode:
        path.unlink()
        raise RuntimeError('数据库备份失败')
    digest = hashlib.sha256(path.read_bytes()).hexdigest()
    (path.with_suffix('.sha256')).write_text(digest+'\n')
    if name == 'baseline':
        backup_index(name)
    return digest

def restore(name):
    stop_services()
    env = environment()
    path = STATE/'snapshots'/(name+'.sql')
    if hashlib.sha256(path.read_bytes()).hexdigest() != path.with_suffix('.sha256').read_text().strip():
        raise RuntimeError('快照校验失败')
    for row in queues():
        if int(row[2]):
            raise RuntimeError('停服务后仍有未确认消息，拒绝恢复')
        subprocess.run(['docker','exec','gopulse-loadtest-rabbitmq-1','rabbitmqctl','purge_queue',row[0]],check=True,stdout=subprocess.DEVNULL)
    with path.open('rb') as dump:
        result = subprocess.run(['docker','exec','-i','-e','MYSQL_PWD='+env['MYSQL_PASSWORD'],MYSQL,
                                 'mysql','-u'+env['MYSQL_USER'],env['MYSQL_DATABASE']],stdin=dump,stderr=subprocess.PIPE,timeout=300)
    if result.returncode:
        raise RuntimeError('数据库恢复失败')
    if (STATE/'snapshots'/(name+'-index.json')).exists():
        restore_index(name)
    else:
        reindex()
    assert_drained()

def fingerprint():
    # 同时检查所有业务表的内容校验和、行数和搜索文档数。
    tables = sql('SHOW TABLES;').splitlines()
    counts = sql(' UNION ALL '.join(f'SELECT "{t}",COUNT(*) FROM `{t}`' for t in tables)+';')
    checksums = sql('CHECKSUM TABLE '+','.join('`'+t+'`' for t in tables)+' EXTENDED;')
    if any(line.endswith('\tNULL') for line in checksums.splitlines()):
        raise RuntimeError('数据表校验和不可用')
    return {'counts': counts, 'checksums': checksums, 'search': index_digest()}


def clone_index(source, target):
    http('/'+source+'/_block/write','PUT')
    http('/'+source+'/_flush','POST')
    response=http('/'+source+'/_clone/'+target+'?wait_for_active_shards=1','POST',{'settings':{'index.number_of_replicas':0}})
    if not response.get('acknowledged'):raise RuntimeError('搜索索引克隆未获确认')
    health=http('/_cluster/health/'+target+'?wait_for_status=green&timeout=60s')
    if health.get('timed_out'):raise RuntimeError('克隆索引未就绪')

def backup_index(name):
    alias='gopulse-post-search-v1'
    current=next(iter(http('/_alias/'+alias)))
    snapshot=alias+'-pt3-snapshot-'+name
    clone_index(current,snapshot)
    http('/'+current+'/_settings','PUT',{'index.blocks.write':False})
    # 克隆索引只读保留，不绑定业务 alias。
    http('/'+snapshot+'/_settings','PUT',{'index.blocks.write':True})
    manifest={'index':snapshot,'digest':index_digest(snapshot)}
    (STATE/'snapshots'/(name+'-index.json')).write_text(json.dumps(manifest,indent=2)+'\n')

def restore_index(name):
    alias='gopulse-post-search-v1'
    manifest=json.loads((STATE/'snapshots'/(name+'-index.json')).read_text())
    if index_digest(manifest['index'])!=manifest['digest']:raise RuntimeError('搜索基线索引校验失败')
    target=alias+'-pt3-restored-'+str(time.time_ns())
    clone_index(manifest['index'],target)
    http('/'+target+'/_settings','PUT',{'index.blocks.write':False})
    old=list(http('/_alias/'+alias))
    actions=[{'remove':{'index':index,'alias':alias}} for index in old]
    actions.append({'add':{'index':target,'alias':alias}})
    http('/_aliases','POST',{'actions':actions})
    if index_digest(target)!=manifest['digest']:raise RuntimeError('恢复的搜索索引校验失败')
    for index in old:
        if index!=manifest['index']:http('/'+index,'DELETE')

def index_digest(index='gopulse-post-search-v1'):
    response=http('/'+index+'/_search?scroll=2m','POST',{'size':5000,'sort':['_doc'],'version':True,'query':{'match_all':{}}})
    count=0;digest=0;scroll=None
    try:
        while True:
            scroll=response.get('_scroll_id');hits=response['hits']['hits']
            if not hits:break
            for hit in hits:
                content=json.dumps([hit['_id'],hit['_version'],hit['_source']],sort_keys=True,separators=(',',':')).encode()
                digest^=int.from_bytes(hashlib.sha256(content).digest(),'big');count+=1
            response=http('/_search/scroll','POST',{'scroll':'2m','scroll_id':scroll})
    finally:
        if scroll:http('/_search/scroll','DELETE',{'scroll_id':[scroll]})
    mappings=list(http('/'+index+'/_mapping').values())[0]['mappings']
    return {'count':count,'sha256_xor':f'{digest:064x}','mappings_sha256':hashlib.sha256(json.dumps(mappings,sort_keys=True).encode()).hexdigest()}

if __name__ == '__main__':
    import argparse
    parser = argparse.ArgumentParser(description='测试三环境操作，不启动负载')
    parser.add_argument('action',choices=['start','stop','backup','restore','fingerprint'])
    parser.add_argument('--name',default='baseline')
    args=parser.parse_args()
    if args.action=='start':start_services()
    elif args.action=='stop':stop_services()
    elif args.action=='backup':print(backup(args.name))
    elif args.action=='restore':restore(args.name)
    else:print(json.dumps(fingerprint(),ensure_ascii=False,indent=2))
