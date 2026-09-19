"""Formal source backup/restore, followed by an immutable candidate migration.

The source lifecycle owns its installation throughout. Candidate one-shots and
application processes have a separate owner label and never edit lifecycle state.
This is a single-hop acceptance harness, not a new product upgrade command.
"""
import hashlib
import json
import os
from pathlib import Path
import subprocess
from types import SimpleNamespace
import urllib.request
import uuid

from release_artifacts import platform_ref
from verify_backup_restore import Recovery, docker, save
from verify_current_recovery import CurrentRecovery
from verify_plugin_metrics import Client, wait_until


class MigrationAcceptance(Recovery):
    facts = CurrentRecovery.facts
    compare = CurrentRecovery.compare
    backup_project = CurrentRecovery.backup_project
    write = CurrentRecovery.write

    def __init__(self, source_path, target, work):
        self.target = target
        self.owner = uuid.uuid4().hex
        self.candidates = []
        self.results = []
        super().__init__(SimpleNamespace(manifest=source_path, work=work,
                                         acceptance_image=None))
        self.data.setdefault('target_revision', target['revision'])
        if self.data['target_revision'] != target['revision']:
            raise ValueError('migration workspace belongs to another candidate')
        self.record()

    def record(self):
        super().record()

    def note(self, step, **facts):
        self.results.append({'step': step, **facts})
        save(self.work/'migration-evidence.json', {
            'schema': 'gopulse.phase17-migration.v1',
            'complete': False, 'results': self.results})
        print('PASS: '+step, flush=True)

    def owned_candidate(self, cid):
        obj = json.loads(docker('inspect', cid))[0]
        if obj['Config']['Labels'].get('io.gopulse.state-owner') != self.owner:
            raise RuntimeError('candidate ownership mismatch')
        return obj

    def remove_candidates(self):
        for cid in reversed(self.candidates):
            obj = self.owned_candidate(cid)
            logs = subprocess.run(['docker','logs',cid],capture_output=True,timeout=30)
            diagnostic = self.work/('candidate-'+cid[:12]+'.log')
            diagnostic.write_bytes(logs.stdout+logs.stderr)
            diagnostic.chmod(0o600)
            self.output.append((logs.stdout+logs.stderr).decode(errors='replace'))
            docker('rm', '-f', cid)
        self.candidates.clear()

    def network(self, name, role):
        state = self.state(name)
        network = state['project']+'_'+role
        obj = json.loads(docker('network', 'inspect', network))[0]
        labels = obj['Labels']
        if (labels.get('io.gopulse.lifecycle.installation') != state['installation_token']
                or labels.get('io.gopulse.lifecycle.manifest') != state['manifest_digest']):
            raise RuntimeError('foreign restored network')
        return network

    def environment(self, name):
        # Read the lifecycle-created, verified backend's actual runtime config;
        # do not synthesize source business state or expose values in argv.
        obj = json.loads(docker('inspect', self.cid(name, 'backend')))[0]
        env = dict(item.split('=', 1) for item in obj['Config']['Env'])
        env.update(GOPULSE_VERSION=self.target['version'],
                   GOPULSE_REVISION=self.target['revision'],
                   GOPULSE_RUNTIME_MODE='container',
                   BACKEND_METRICS_TOKEN='state-metrics-'+self.owner)
        path = self.work/(name+'-candidate.env')
        path.write_text(''.join(k+'='+v+'\n' for k,v in env.items()))
        path.chmod(0o600)
        return path

    def create(self, name, entrypoint, args=(), server=False):
        ref = platform_ref(self.target['images']['backend'], 'linux/amd64')
        env = self.environment(name)
        command = ['create', '--label', 'io.gopulse.state-owner='+self.owner,
                   '--read-only', '--cap-drop', 'ALL', '--security-opt',
                   'no-new-privileges', '--tmpfs', '/tmp', '--env-file', str(env),
                   '--network', self.network(name, 'business'),
                   '--entrypoint', entrypoint]
        if server:
            command += ['-p', '127.0.0.1::8080']
        cid = docker(*command, ref, *args).strip()
        self.candidates.append(cid)
        self.owned_candidate(cid)
        if server:
            docker('network', 'connect', self.network(name, 'observability'), cid)
            docker('network', 'connect', self.network(name, 'edge'), cid)
        return cid

    def migrate(self, name, action, expected=0):
        cid = self.create(name, '/usr/local/bin/migrate', [action])
        result = subprocess.run(['docker','start','-a',cid], capture_output=True, timeout=120)
        self.output.append((result.stdout+result.stderr).decode(errors='replace'))
        info = self.owned_candidate(cid)
        if info['State']['ExitCode'] != expected:
            raise RuntimeError('candidate migrate '+action+' unexpected exit')
        raw = result.stdout or result.stderr
        try:
            value = json.loads(raw)
        except ValueError:
            raise RuntimeError('candidate migration output is not safe JSON') from None
        if not set(value).issubset({'binary_target','database_version','state','changed','reason','exit_code'}):
            raise RuntimeError('unexpected migration output fields')
        self.owned_candidate(cid)
        docker('rm',cid)
        self.candidates.remove(cid)
        return value

    def candidate_backend(self, name, ready):
        old = self.cid(name, 'backend')
        docker('stop', old)
        cid = self.create(name, '/usr/local/bin/server', server=True)
        docker('start', cid)
        obj = self.owned_candidate(cid)
        ports = obj['NetworkSettings']['Ports'].get('8080/tcp')
        if not ports:
            raise RuntimeError('candidate backend exited before publishing its private probe port')
        port = ports[0]['HostPort']
        base = 'http://127.0.0.1:'+port
        def status():
            try:
                with urllib.request.urlopen(base+'/ready',timeout=3) as response:
                    return response.status
            except urllib.error.HTTPError as error:
                return error.code
            except OSError:
                # A published port can reset until the server has bound it.
                return 0
        if ready:
            wait_until(lambda: status()==200, 'candidate schema readiness', 90)
        else:
            wait_until(lambda: status()==503, 'candidate refuses dirty schema readiness', 30)
        return Client(base)

    def run(self):
        docker('pull', platform_ref(self.target['images']['backend'], 'linux/amd64'))
        self.seed()
        # Add real comments/likes to the source recipe, through public APIs.
        if 'source-social-history' not in self.data['completed']:
            user = self.client('source', self.data['user'])
            post = str(self.data['post_id'])
            user.request('posts/'+post+'/comments','POST',{'content':'Phase17 source comment'},201)
            user.request('posts/'+post+'/like','PUT',expected=204)
            self.mark('source-social-history')
        archive, facts = self.backup_project('source')
        self.note('formal_source_backup_inspected', source_version=self.manifest['version'],
                  backup_sha256=hashlib.sha256(archive.read_bytes()).hexdigest(),
                  domains={k:len(v) for k,v in facts.items()})
        self.call('source','down')
        self.restore('target',archive)
        self.call('target','verify')
        self.compare('target',facts)
        self.note('formal_source_restore_facts', comparisons=self.data['comparisons']['target'])
        before = self.sql('target','SELECT version,dirty FROM schema_migrations;')
        validate = self.migrate('target','validate')
        status = self.migrate('target','status')
        up = self.migrate('target','up')
        repeated = self.migrate('target','up')
        if repeated.get('changed') or status.get('binary_target') != validate.get('binary_target'):
            raise RuntimeError('candidate repeat or target contract failed')
        self.compare('target',facts)
        self.note('candidate_migration_current_repeat', before=before,
                  validate=validate,status=status,up=up,repeated=repeated)
        client = self.candidate_backend('target',ready=True)
        client.request('auth/login','POST',{'username':self.data['user'],'password':self.data['password']})
        client.request('admin/audit-events',expected=403)
        created=client.request('posts','POST',{'title':'Candidate migrated write '+self.owner[:12],
                              'content':'Written by immutable candidate after formal restore'},201)['data']
        from urllib.parse import quote
        wait_until(lambda: any(p['id']==created['id'] for p in client.request('search/posts?q='+quote(created['title']))['data']),
                   'candidate new write search convergence')
        self.note('candidate_ready_role_and_new_write', post_id=created['id'])
        self.remove_candidates()
        # A fixed non-12 dirty marker is a rejection fixture, never a repair.
        # Do not clear/force it. Restore the formal archive into a new project.
        self.sql('target','UPDATE schema_migrations SET version=11, dirty=1;')
        stable = self.facts('target')
        refused = self.migrate('target','up',4)
        if refused.get('reason') != 'dirty' or self.facts('target') != stable:
            raise RuntimeError('dirty rejection changed persistent facts')
        self.candidate_backend('target',ready=False)
        self.note('dirty_rejected_without_fact_changes', result=refused, ready_status=503)
        self.remove_candidates()
        self.purge('target')
        self.restore('rollback',archive)
        self.call('rollback','verify')
        self.compare('rollback',facts)
        self.note('formal_restore_after_failure',comparisons=self.data['comparisons']['rollback'])
        return self.results

    def cleanup(self):
        self.remove_candidates()
        for name in ('rollback','target','source'):
            self.purge(name)
        # Both captured lifecycle output and candidate logs are private. Scan
        # known credential values; publish only allowlisted aggregate results.
        secrets=set()
        for name in ('source','target','rollback'):
            if (self.work/name/'secrets.json').exists():
                secrets.update(v for k,v in self.secrets(name).items()
                               if any(s in k for s in ('PASSWORD','SECRET','TOKEN')) and len(v)>8)
        if any(secret in output for secret in secrets for output in self.output):
            raise RuntimeError('secret found in lifecycle or migration output')
