#!/usr/bin/env python3
"""Real Linux amd64 backup/recovery acceptance, using one authoritative source.

Every fixture is produced by real APIs/containers. Negative SQL input is derived
from that actual encrypted backup, never passed off as a successful source.
"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import signal
import socket
import subprocess
import time
from verify_plugin_metrics import Client, wait_until
from verify_alerts import rule
from release_artifacts import verify_bundle, platform_ref

ROOT = Path(__file__).resolve().parents[2]


def save(path, value):
    path.write_text(json.dumps(value, indent=2)+'\n');path.chmod(0o600)


def docker(*args, data=None, expected=0):
    result = subprocess.run(['docker', *args], input=data, capture_output=True, timeout=300)
    if result.returncode != expected:
        raise RuntimeError('scoped Docker command failed; raw diagnostics suppressed')
    return result.stdout.decode().strip()


class Recovery:
    def __init__(self, args):
        self.args = args
        self.bundle = args.manifest.resolve().parent
        self.manifest = verify_bundle(args.manifest.resolve())
        self.image = platform_ref(self.manifest['lifecycle'], 'linux/amd64')
        self.work = args.work.resolve();self.work.mkdir(parents=True, exist_ok=True, mode=0o700)
        if self.work.stat().st_mode & 0o077:raise RuntimeError('acceptance work directory must be private')
        self.receipt = self.work/'acceptance.json'
        self.identity = hashlib.sha256(args.manifest.read_bytes()).hexdigest()
        if self.receipt.exists():
            self.data = json.loads(self.receipt.read_text())
            if self.data['manifest_sha256'] != self.identity:raise RuntimeError('work directory belongs to another candidate')
        else:
            self.data={'schema':1,'manifest_sha256':self.identity,'completed':[]}
            self.record()
        self.key = self.work/'passphrase'
        if not self.key.exists():self.key.write_bytes(os.urandom(32));self.key.chmod(0o600)
        self.backup = self.work/'source'/'product.gpb'
        self.output=[]
        self.fixture = self.work/'backup-fixture'
        if not self.fixture.exists():
            subprocess.run(['go','build','-o',str(self.fixture),'./cmd/backup-fixture'],cwd=ROOT/'lifecycle',check=True)
        server=json.loads(docker('version','--format','{{json .Server}}'))
        if server['Os']+'/'+server['Arch'] != 'linux/amd64':raise RuntimeError('real Linux amd64 Docker required')
        docker('pull',self.image)

    def record(self):save(self.receipt,self.data)
    def mark(self,step):
        if step not in self.data['completed']:self.data['completed'].append(step)
        self.record();print('PASS: '+step,flush=True)
    def state(self,name):return json.loads((self.work/name/'state.json').read_text())
    def secrets(self,name):return json.loads((self.work/name/'secrets.json').read_text())
    def base(self):
        endpoint='/var/run/docker.sock'
        return ['docker','run','--rm','--network','host','--read-only','--cap-drop','ALL','--security-opt','no-new-privileges',
                '--user',str(os.getuid())+':'+str(os.getgid()),'--group-add',str(os.stat(endpoint).st_gid),'--tmpfs','/tmp',
                '-v',endpoint+':'+endpoint,'-v',str(self.bundle)+':/bundle:ro','-v',str(self.work)+':'+str(self.work),self.image]
    def argv(self,name,command,*args):
        if command == 'backup-inspect':return [*self.base(),command,*args]
        return [*self.base(),command,'--install',str(self.work/name),'--endpoint','unix:///var/run/docker.sock',*args]
    def call(self,name,command,*args,expected=0):
        result=subprocess.run(self.argv(name,command,*args),capture_output=True,text=True,timeout=1200)
        self.output.append(result.stdout+result.stderr)
        if result.returncode!=expected:raise RuntimeError(command+' returned '+str(result.returncode)+': '+result.stdout+result.stderr)
        text=result.stdout if expected==0 else result.stderr
        return json.loads(next(s for s in reversed(text.splitlines()) if s.startswith('{')))
    def init(self,name):
        path=self.work/name
        if (path/'state.json').exists():return
        path.mkdir(mode=0o700,exist_ok=True)
        with socket.socket() as sock:sock.bind(('127.0.0.1',0));port=sock.getsockname()[1]
        self.call(name,'init','--port',str(port))
    def cid(self,name,service):
        state=self.state(name)
        ids=docker('ps','-aq','--filter','label=com.docker.compose.project='+state['project'],'--filter','label=com.docker.compose.service='+service).split()
        if len(ids)!=1:raise RuntimeError('one owned service required: '+service)
        obj=json.loads(docker('inspect',ids[0]))[0];labels=obj['Config']['Labels']
        if labels.get('io.gopulse.lifecycle.installation')!=state['installation_token'] or labels.get('io.gopulse.lifecycle.manifest')!=state['manifest_digest']:raise RuntimeError('foreign service')
        return ids[0]
    def sql(self,name,query):
        return docker('exec','-i',self.cid(name,'mysql'),'sh','-c','MYSQL_PWD="$MYSQL_PASSWORD" exec mysql --binary-mode=1 --batch --skip-column-names -u "$MYSQL_USER" "$MYSQL_DATABASE"',data=query.encode())
    def client(self,name,username=None):
        base='http://127.0.0.1:'+str(self.state(name)['edge_port']);client=Client(base)
        if username:client.request('auth/login','POST',{'username':username,'password':self.data['password']})
        return client
    def empty(self,name):
        project=self.state(name)['project']
        for kind,cmd in [('container',['ps','-aq']),('volume',['volume','ls','-q']),('network',['network','ls','-q'])]:
            if docker(*cmd,'--filter','label=com.docker.compose.project='+project):raise RuntimeError('failed restore left owned '+kind)
        if self.state(name)['phase']=='ready':raise RuntimeError('failed restore published ready')
    def restore(self,name,archive=None,expected=0):
        self.init(name)
        return self.call(name,'restore','--archive',str(archive or self.backup),'--passphrase-file',str(self.key),expected=expected)
    def purge(self,name):
        if (self.work/name/'state.json').exists():self.call(name,'down','--purge','--confirm',self.state(name)['project'])
    def fixture_check(self,**opts):
        args=[str(self.fixture),'--archive',str(self.backup),'--passphrase-file',str(self.key)]
        for key,value in opts.items():args+=['--'+key.replace('_','-'),str(value)]
        result=subprocess.run(args,capture_output=True,text=True,timeout=60)
        if result.returncode:raise RuntimeError('actual backup fixture audit failed')
        if not opts:return json.loads(result.stdout)

    def seed(self):
        if 'source-seeded' in self.data['completed']:return
        self.init('source');self.call('source','up')
        new_accounts='admin' not in self.data
        if new_accounts:
            token=os.urandom(6).hex();self.data.update(password='Recovery-'+token+'-password',admin='admin_'+token,user='user_'+token);self.record()
        token=self.data['admin'].split('_',1)[1]
        admin,user=self.client('source'),self.client('source')
        for c,name in [(admin,self.data['admin']),(user,self.data['user'])]:
            c.request('auth/register' if new_accounts else 'auth/login','POST',{'username':name,'password':self.data['password']},201 if new_accounts else 200)
        uid=admin.request('users/me')['data']['id'];docker('exec',self.cid('source','backend'),'/usr/local/bin/admin-role','bootstrap','--user-id',str(uid))
        if 'post_id' not in self.data:
            post=user.request('posts','POST',{'title':'Recovery source '+token,'content':'Authoritative live product record '+token},201)['data']
            self.data['post_id']=post['id'];self.data['post_title']=post['title'];self.record()
        wait_until(lambda:admin.request('search/posts?q='+token)['data'],'source search indexing')
        secrets=self.secrets('source')
        configs={
            'mysql':{'config':{'host':'mysql','port':3306,'database':secrets['MYSQL_DATABASE'],'username':secrets['MYSQL_USER'],'connect_timeout':'1s','scrape_timeout':'2s'},'secrets':{'password':secrets['MYSQL_PASSWORD']}},
            'rabbitmq':{'config':{'host':'rabbitmq','management_port':15672,'vhost':'/','username':secrets['RABBITMQ_USER'],'connect_timeout':'1s','scrape_timeout':'2s'},'secrets':{'password':secrets['RABBITMQ_PASSWORD']}},
            'kafka':{'config':{'host':'kafka','port':19092,'topic':'gopulse-observability-v1','consumer_group':'gopulse-marshaller-metrics-v1','connect_timeout':'1s','scrape_timeout':'3s'},'secrets':{}},
            'elasticsearch':{'config':{'host':'elasticsearch','port':9200,'connect_timeout':'1s','scrape_timeout':'3s'},'secrets':{}},
            'victoriametrics':{'config':{'host':'victoriametrics','port':8428,'username':secrets['VICTORIAMETRICS_USERNAME'],'connect_timeout':'1s','scrape_timeout':'3s'},'secrets':{'password':secrets['VICTORIAMETRICS_PASSWORD']}},
        }
        installed={p['id'] for p in admin.request('exporter-plugins')['data']}
        for source,config in configs.items():
            if source+'-exporter' not in installed:admin.request('exporter-plugins/'+source+'-exporter/install','POST',config,201)
        self.check_plugins(admin)
        if 'rule_id' not in self.data:
            created=admin.request('alerts/rules','POST',rule('recovery-'+token,threshold=0),201)['data'];self.data['rule_id']=created['id'];self.record()
        wait_until(lambda:admin.request('alerts/history')['data'],'actual alert history',180)
        wait_until(lambda:admin.request('admin/audit-events')['data'],'actual administrative audit')
        self.mark('source-seeded')

    def check_plugins(self,client):
        for source in ['redis','mysql','rabbitmq','kafka','elasticsearch','victoriametrics']:
            path='exporter-plugins/'+source+'-exporter'
            wait_until(lambda p=path:(s if (s:=client.request(p)['data']).get('last_success_at') and s['observed_state']=='running' else None),'real '+source+' collection',180)
            wait_until(lambda s=source:client.request('observability/metrics?metric=gopulse_'+s+'_up&range=15m')['data']['series'],'transported '+source+' metrics',180)

    def same_arch(self):
        self.seed()
        if not self.backup.exists():
            writer=self.client('source',self.data['user'])
            proc=subprocess.Popen(self.argv('source','backup','--archive',str(self.backup),'--passphrase-file',str(self.key)),stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True)
            rejected=False
            deadline=time.monotonic()+300
            try:
                while time.monotonic()<deadline:
                    phase=self.state('source')['phase']
                    if phase.startswith('export-') or phase in ('maintenance-business-drain','maintenance-observability-drain','maintenance-quiesced'):
                        try:writer.request('posts','POST',{'title':'must not be accepted during maintenance','content':'blocked write'},201)
                        except Exception:rejected=True
                        break
                    if proc.poll() is not None:break
                    time.sleep(.05)
            finally:
                stdout,stderr=proc.communicate(timeout=1200);self.output.append(stdout+stderr)
            if proc.returncode:raise RuntimeError('backup failed: '+stdout+stderr)
            if not rejected:raise RuntimeError('maintenance write rejection was not established')
            self.mark('maintenance-rejects-new-business-writes')
        self.call('source','backup-inspect','--archive',str(self.backup),'--passphrase-file',str(self.key))
        facts=self.fixture_check();self.data['cutover_facts']=facts;self.record();self.mark('authenticated-encrypted-six-domain-backup')
        self.restore('target')
        self.call('target','verify')
        target=self.client('target',self.data['admin'])
        self.check_plugins(target)
        post=target.request('posts/'+str(self.data['post_id']))['data']
        if post['title']!=self.data['post_title']:raise RuntimeError('business fact changed')
        if not target.request('alerts/history')['data'] or not target.request('admin/audit-events')['data']:raise RuntimeError('history or audit missing')
        created=target.request('posts','POST',{'title':'Restored new write '+os.urandom(6).hex(),'content':'Real restored database write'},201)['data']
        self.data['restored_post_id']=created['id'];self.record()
        wait_until(lambda:target.request('search/posts?q='+created['title'].split()[-1])['data'],'restored search new write')
        if not self.args.acceptance_image:raise RuntimeError('--acceptance-image required for dual Frontend acceptance')
        # Reuse the existing focused real browser matrix, not screenshot mocks.
        from frontend_bundle_browser import run_browser
        browser=run_browser(self.work/'target',self.state('target'),self.call('target','status'),self.secrets('target'),self.args.acceptance_image,lambda *a:docker(*a),credentials=self.data)
        self.data['browser']=browser;self.record();self.mark('restored-facts-six-plugins-frontends-and-new-write')
        self.call('target','down') # preserve restored data; release RAM for failure matrix

    def failure_matrix(self):
        if 'restored-facts-six-plugins-frontends-and-new-write' not in self.data['completed']:raise RuntimeError('same-arch success is required first')
        source_state=(self.work/'source'/'state.json').read_bytes()
        before=self.sql('source','SELECT COUNT(*) FROM users; SELECT COUNT(*) FROM posts; SELECT COUNT(*) FROM bootstrap_super_admin;')
        self.init('negative')
        self.restore('source',expected=17)
        self.mark('nonempty-source-project-cannot-be-overwritten')
        doctor_dir=self.work/'doctor-diagnostics';doctor_dir.mkdir(mode=0o700,exist_ok=True)
        diagnosis=self.call('negative','doctor','--endpoint','unix:///missing-gopulse-daemon','--diagnostics-dir',str(doctor_dir),expected=11)
        if not Path(diagnosis.get('diagnostic','')).is_file():raise RuntimeError('doctor diagnostic was not written')
        self.mark('doctor-private-diagnostics')
        wrong=self.work/'wrong-passphrase';wrong.write_bytes(os.urandom(32));wrong.chmod(0o600)
        self.call('negative','restore','--archive',str(self.backup),'--passphrase-file',str(wrong),expected=21);self.empty('negative')
        tamper=self.work/'tampered.gpb';raw=bytearray(self.backup.read_bytes());raw[-1]^=1;tamper.write_bytes(raw);tamper.chmod(0o600)
        self.restore('negative',tamper,21);self.empty('negative');self.mark('wrong-passphrase-and-tamper-rejected-before-resources')
        # Real 1 MiB tmpfs: copy only the empty target identity, then run the
        # ordinary restore command. It must fail statfs before Docker mutation.
        base=self.base();base[-1:-1]=['--tmpfs',f'/tiny:rw,size=1048576,mode=0700,uid={os.getuid()},gid={os.getgid()}','--entrypoint','sh']
        seed=str(self.work/'negative')
        result=subprocess.run([*base,'-c','cp "$1/state.json" "$1/secrets.json" /tiny/; exec gopulse restore --install /tiny --bundle /bundle --endpoint unix:///var/run/docker.sock --archive "$2" --passphrase-file "$3"','capacity',seed,str(self.backup),str(self.key)],capture_output=True,text=True,timeout=120)
        self.output.append(result.stdout+result.stderr)
        if result.returncode!=13:raise RuntimeError('real low-space preflight did not reject: exit '+str(result.returncode))
        self.empty('negative');self.mark('real-low-space-preflight')
        invalid=self.work/'invalid-sql.gpb'
        if not invalid.exists():self.fixture_check(invalid_sql_output=invalid)
        self.restore('negative',invalid,18);self.empty('negative');self.mark('native-import-failure-cleans-only-new-target')
        # Cancel the actual lifecycle process after it enters the restore stage.
        name='gopulse-recovery-interrupt-'+os.urandom(6).hex()
        argv=self.argv('negative','restore','--archive',str(self.backup),'--passphrase-file',str(self.key));argv[3:3]=['--name',name]
        proc=subprocess.Popen(argv,stdout=subprocess.PIPE,stderr=subprocess.PIPE,text=True)
        try:
            deadline=time.monotonic()+120
            while time.monotonic()<deadline:
                state=self.state('negative')
                if state['phase']=='restore-infrastructure':break
                if proc.poll() is not None:raise RuntimeError('restore exited before interruption')
                time.sleep(.05)
            else:raise RuntimeError('restore never reached interruption stage')
            docker('kill','--signal','SIGTERM',name)
            stdout,stderr=proc.communicate(timeout=180);self.output.append(stdout+stderr)
            if proc.returncode!=20:raise RuntimeError('interruption exit is not stable')
        finally:
            if proc.poll() is None:
                docker('kill','--signal','SIGTERM',name);proc.communicate(timeout=180)
        self.empty('negative');self.restore('negative');self.call('negative','verify');self.purge('negative');self.mark('interruption-cleanup-and-retry-from-verified-backup')
        if (self.work/'source'/'state.json').read_bytes()!=source_state or self.sql('source','SELECT COUNT(*) FROM users; SELECT COUNT(*) FROM posts; SELECT COUNT(*) FROM bootstrap_super_admin;')!=before:raise RuntimeError('failure scenarios changed source facts')
        self.mark('source-unaffected-by-failure-matrix')
        for text in self.output:
            if self.state('source')['installation_token'] in text:raise RuntimeError('installation credential leaked')
            for value in self.secrets('source').values():
                if len(value)>20 and value in text:raise RuntimeError('credential in lifecycle diagnostic')
        for path in (self.work/'negative'/'diagnostics').glob('*.json'):
            data=path.read_bytes()
            for key,value in self.secrets('source').items():
                if any(w in key for w in ('PASSWORD','TOKEN','SECRET')) and value.encode() in data:raise RuntimeError('secret in diagnostic metadata')
        self.mark('secret-free-scoped-diagnostics')


def main():
    p=argparse.ArgumentParser(description=__doc__)
    p.add_argument('--platform',choices=['linux/amd64'],required=True)
    p.add_argument('--manifest',type=Path,default=ROOT/'dist/phase16-04-recovery-v1/release-manifest.json')
    p.add_argument('--work',type=Path,default=ROOT/'.run/phase16-04-recovery/product')
    p.add_argument('--acceptance-image', default=os.environ.get('GOPULSE_ACCEPTANCE_IMAGE'))
    p.add_argument('--current-product', action='store_true')
    modes=p.add_mutually_exclusive_group()
    modes.add_argument('--same-arch',action='store_true');modes.add_argument('--failure-matrix',action='store_true');modes.add_argument('--cleanup',action='store_true')
    a=p.parse_args()
    if not any([a.current_product,a.same_arch,a.failure_matrix,a.cleanup]):p.error('an acceptance mode is required')
    if a.current_product and a.same_arch:p.error('--current-product cannot be combined with --same-arch')
    if a.current_product:
        if a.work == ROOT/'.run/phase16-04-recovery/product':a.work=ROOT/'.run/phase16-05-recovery/product'
        from verify_current_recovery import CurrentRecovery
        r=CurrentRecovery(a)
        if a.cleanup:
            r.cleanup()
        elif a.failure_matrix:r.current_failures()
        else:r.current_product()
        return
    r=Recovery(a)
    if a.same_arch:r.same_arch()
    elif a.failure_matrix:r.failure_matrix()
    else:
        for name in ['negative','target','source']:r.purge(name)

if __name__=='__main__':main()
