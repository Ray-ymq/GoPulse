"""执行测试三的一个独立组；只在用户另行要求启动压测后使用。"""
import argparse
from datetime import datetime, timezone
import hashlib
import json
from pathlib import Path
import signal
import subprocess
import sys
import time
from environment import ROOT,STATE,restore,start_services
from smoke import Client,business_probe

def atomic_json(path,value):
    temp=path.with_suffix('.tmp');temp.write_text(json.dumps(value,indent=2)+'\n');temp.replace(path)

def main():
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--rps',type=float,help='独立复测档位；省略时从 10 RPS 连续翻倍')
    parser.add_argument('--measurement-seconds',type=int,default=300)
    args=parser.parse_args()
    if (STATE/'STOP').exists():raise SystemExit('存在人工停压标记，须先核对原因并移除 .run/pt3/STOP')
    prepared=json.loads((STATE/'prepared.json').read_text())
    if not prepared['ready']:raise SystemExit('压测准备未完成')
    for file,digest in {**prepared['source_sha256'],**prepared['artifact_sha256']}.items():
        if hashlib.sha256((ROOT/file).read_bytes()).hexdigest()!=digest:raise SystemExit('准备后源码或配置发生变化，必须重新验证')
    if args.rps is None and args.measurement_seconds != 300:raise SystemExit('连续组只使用 300 秒测量；1800 秒持续测试必须指定单个档位')
    if args.measurement_seconds not in [300,1800]:raise SystemExit('正式测量时长必须为 300 或 1800 秒')
    if args.rps is not None and not 0<args.rps<=2560:raise SystemExit('请求率不在本轮已准备的资源范围内')
    observer_pid=STATE/'logs/observer.pid'
    if observer_pid.exists():
        try:
            pid=int(observer_pid.read_text())
            if b'perflab/pt3/observe.py' in Path(f'/proc/{pid}/cmdline').read_bytes():
                import os
                os.kill(pid,signal.SIGTERM)
                for _ in range(50):
                    if not Path(f'/proc/{pid}/stat').exists() or Path(f'/proc/{pid}/stat').read_text().split(') ',1)[1].startswith('Z '):break
                    time.sleep(.1)
                else:raise RuntimeError('旧观测进程未退出')
        except (ProcessLookupError,FileNotFoundError):pass
    restore('baseline');start_services()
    config=json.loads((ROOT/'perflab/k6/test3.json').read_text())
    if args.rps is not None:config['boundary']['start_rps']=args.rps;config['boundary']['max_rps']=args.rps
    config['boundary']['measurement_seconds']=args.measurement_seconds
    config_path=STATE/'active-config.json';atomic_json(config_path,config)
    load=str(STATE/'bin/load')
    plan=json.loads(subprocess.check_output([load,'--mode','boundary','--config',str(config_path),'--dry-run'],cwd=ROOT,text=True))
    atomic_json(STATE/'active-run.json',plan)
    directory=STATE/'observations'/datetime.now().strftime('%Y%m%dT%H%M%S')
    log=(STATE/'logs/observer.log').open('ab')
    observer=subprocess.Popen([sys.executable,str(ROOT/'perflab/pt3/observe.py'),'--directory',str(directory)],cwd=ROOT,stdout=log,stderr=log,start_new_session=True)
    observer_pid.write_text(str(observer.pid))
    for _ in range(100):
        if observer.poll() is not None:raise RuntimeError('观测进程退出')
        path=STATE/'guard.json'
        if path.exists():
            guard=json.loads(path.read_text())
            if guard.get('directory')==str(directory) and guard['status']=='ready':break
            if guard.get('directory')==str(directory) and guard['status']=='stop':raise RuntimeError('观测拒绝压测：'+str(guard['reason']))
        time.sleep(.1)
    else:raise RuntimeError('观测链路未准备好')
    with (directory/'load.log').open('wb') as output:
        process=subprocess.Popen([load,'--mode','boundary','--config',str(config_path)],cwd=ROOT,stdout=output,stderr=output)
        def stop_load(*_):
            if process.poll() is None:process.send_signal(signal.SIGINT)
        signal.signal(signal.SIGTERM,stop_load);signal.signal(signal.SIGINT,stop_load)
        result=process.wait()
    stopped=datetime.now(timezone.utc).isoformat()
    client=Client()
    try:login=client.login()
    except Exception as error:login=str(error)
    # 无论正常结束还是故障停压，都保留十分钟自然恢复窗口。
    started=time.monotonic()
    with (directory/'recovery.jsonl').open('w') as out:
        while time.monotonic()-started<600:
            if login!=200:
                try:login=client.login()
                except Exception as error:login=str(error)
            sample={'timestamp':datetime.now(timezone.utc).isoformat(),'elapsed_seconds':time.monotonic()-started,'login':login,'business':business_probe(client)}
            for filename,key in [('database-probe.jsonl','database'),('database-state.jsonl','database_state')]:
                path=directory/filename
                try:sample[key]=json.loads(path.read_text().splitlines()[-1])
                except (OSError,ValueError,IndexError):sample[key]={'evidence':'unavailable'}
            sample['observation']=json.loads((STATE/'guard.json').read_text())
            out.write(json.dumps(sample)+'\n');out.flush()
            time.sleep(min(10,max(0,600-(time.monotonic()-started))))
    atomic_json(directory/'group.json',{'stopped_at':stopped,'load_exit_code':result,'natural_recovery_seconds':time.monotonic()-started,'recovery_log':'recovery.jsonl'})
    print('本组已结束，证据目录：'+str(directory))
    return result

if __name__=='__main__':sys.exit(main())
