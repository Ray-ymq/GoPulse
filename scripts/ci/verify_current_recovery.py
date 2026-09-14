"""Current-candidate A/B/C product acceptance; uses the public recovery engine."""
import hashlib
import json
import os
import subprocess
import time
from urllib.parse import quote

from verify_backup_restore import ROOT, Recovery, docker, save
from verify_plugin_metrics import wait_until
from verify_alerts import rule


class CurrentRecovery(Recovery):
    def __init__(self, args):
        super().__init__(args)
        if self.manifest['version'] != (ROOT/'VERSION').read_text().strip():
            raise RuntimeError('current candidate must match the checkout version')
        self.data.setdefault('candidate', {'version': self.manifest['version'],
            'revision': self.manifest['revision'], 'images': self.manifest['images'],
            'plugins': self.manifest['plugins']})
        self.record()

    def facts(self, name):
        # Read-only canonical rows: identity, business content/history and audit.
        # Password hashes remain private and are represented only by a digest.
        queries = {
            'identity': 'SELECT id,username,role,SHA2(password_hash,256) FROM users ORDER BY id',
            'posts': 'SELECT * FROM posts ORDER BY id',
            'comments': 'SELECT * FROM comments ORDER BY id',
            'likes': 'SELECT * FROM post_likes ORDER BY post_id,user_id',
            'audit': 'SELECT * FROM management_audit_events ORDER BY id',
            'incidents': 'SELECT id,rule_id,revision,name,severity,source,object,first_triggered_at FROM alert_incidents ORDER BY id',
            'bootstrap': 'SELECT * FROM bootstrap_super_admin ORDER BY singleton',
            'rules': 'SELECT * FROM alert_rules ORDER BY id',
        }
        return {key: self.sql(name, query+';').splitlines() for key, query in queries.items()}

    def compare(self, name, expected):
        actual = self.facts(name)
        for domain, rows in expected.items():
            if not set(rows).issubset(actual[domain]):
                raise RuntimeError('restored content mismatch: '+domain)
        self.data.setdefault('comparisons', {})[name] = {
            domain: {'rows': len(rows), 'sha256': hashlib.sha256('\n'.join(rows).encode()).hexdigest()}
            for domain, rows in expected.items()}
        self.record()

    def browser(self, name):
        if not self.args.acceptance_image:
            raise RuntimeError('--acceptance-image required for dual Frontend acceptance')
        from frontend_bundle_browser import run_browser
        self.data.setdefault('browsers', {})[name] = run_browser(
            self.work/name, self.state(name), self.call(name, 'status'), self.secrets(name),
            self.args.acceptance_image, lambda *a: docker(*a), credentials=self.data)
        self.record()

    def write(self, name):
        key = name+'-new-post'
        client = self.client(name, self.data['user'])
        if key not in self.data:
            self.data[key] = client.request('posts', 'POST', {
                'title': 'Recovery '+name+' '+os.urandom(8).hex(),
                'content': 'Current candidate real continuing write'}, 201)['data']
            self.record()
        post = self.data[key]
        wait_until(lambda: any(p['id'] == post['id'] for p in client.request(
            'search/posts?q='+quote(post['title']))['data']), name+' new searchable write')
        return post

    def continuing(self, name):
        admin = self.client(name, self.data['admin'])
        user = self.client(name, self.data['user'])
        user.request('admin/audit-events', expected=403)
        start = {p['id']: p.get('last_success_at') for p in admin.request('exporter-plugins')['data']}
        self.check_plugins(admin)
        for plugin, timestamp in start.items():
            wait_until(lambda p=plugin,t=timestamp: admin.request('exporter-plugins/'+p)['data'].get('last_success_at') != t, name+' fresh '+plugin+' collection', 180)
        key = name+'-rule'
        if key not in self.data:
            self.data[key] = admin.request('alerts/rules', 'POST',
                rule('current-recovery-'+name+'-'+os.urandom(6).hex(), threshold=0), 201)['data']['id']
            self.record()
        wait_until(lambda: any(item['rule_id'] == self.data[key] for item in
            admin.request('alerts/history')['data']), name+' new real alert', 180)
        self.write(name)
        self.browser(name)

    def backup_project(self, name):
        path = self.work/name/'product.gpb'
        checkpoint = name+'-backup-inspected'
        if checkpoint not in self.data['completed']:
            fact_path = self.work/(name+'-facts.json')
            if not path.exists():
                save(fact_path, self.facts(name))
                self.call(name, 'backup', '--archive', str(path), '--passphrase-file', str(self.key))
            inspection = self.call(name, 'backup-inspect', '--archive', str(path), '--passphrase-file', str(self.key))
            old = self.backup
            self.backup = path
            try:
                audit = self.fixture_check()
            finally:
                self.backup = old
            if not fact_path.exists():
                raise RuntimeError('archive has no source baseline; use a new private work directory')
            self.compare(name, json.loads(fact_path.read_text()))
            self.data.setdefault('backups', {})[name] = {
                'sha256': hashlib.sha256(path.read_bytes()).hexdigest(),
                'inspection': inspection, 'audit': audit}
            self.mark(checkpoint)
        return path, json.loads((self.work/(name+'-facts.json')).read_text())

    def current_product(self):
        if 'current-product-two-restores-passed' in self.data['completed']:
            return
        self.seed()
        if 'current-source-ready' not in self.data['completed']:
            self.continuing('source')
            user = self.client('source', self.data['user'])
            post_id = str(self.data['post_id'])
            user.request('posts/'+post_id, 'PATCH', {'title': self.data['post_title'], 'content': 'Current candidate edited business history'})
            user.request('posts/'+post_id+'/comments', 'POST', {'content': 'Current candidate real comment'}, 201)
            self.mark('current-source-ready')
        archive, facts = self.backup_project('source')
        if self.state('source')['phase'] != 'stopped':self.call('source', 'down')
        for name in ['target', 'second']:
            if name+'-continuity-passed' not in self.data['completed']:
                if not (self.work/name/'state.json').exists() or self.state(name)['phase'] != 'ready':
                    self.restore(name, archive)
                self.call(name, 'verify')
                receipt = json.loads((self.work/name/'restore-result.json').read_text())
                if not receipt.get('facts_verified') or receipt['source_manifest'] != self.state(name)['manifest_digest']:
                    raise RuntimeError('missing cutover-domain verification')
                self.data.setdefault('restores', {})[name] = receipt
                self.record()
                self.compare(name, facts)
                self.continuing(name)
                before = self.facts(name)
                self.restore(name, archive, 17)
                self.compare(name, before)
                self.mark(name+'-nonempty-restore-rejected')
                self.mark(name+'-continuity-passed')
            if name == 'target':
                archive, facts = self.backup_project(name)
            self.call(name, 'down')
        self.mark('current-product-two-restores-passed')

    def failure_evidence(self, scenario):
        state = self.state('negative')
        if not state.get('operation_id') or state['phase'] not in ('restore-failed', 'interrupted'):
            raise RuntimeError('missing stable failure state')
        diagnostics = list((self.work/'negative'/'diagnostics').glob('*.json'))
        matching = [json.loads(path.read_text()) for path in diagnostics if json.loads(path.read_text()).get('operation_id') == state['operation_id']]
        if not matching or not matching[-1].get('recovery'):
            raise RuntimeError('missing failure diagnosis/recovery guidance')
        self.data.setdefault('failures', {})[scenario] = matching[-1]
        self.record()

    def current_failures(self):
        if 'current-product-two-restores-passed' not in self.data['completed']:
            raise RuntimeError('current-product success required first')
        original = hashlib.sha256(self.backup.read_bytes()).hexdigest()
        self.init('negative')
        if 'current-import-failure-retry' not in self.data['completed']:
            invalid = self.work/'invalid-sql.gpb'
            if not invalid.exists():
                self.fixture_check(invalid_sql_output=invalid)
            self.restore('negative', invalid, 18)
            self.empty('negative')
            self.failure_evidence('import' if 'current-import-failure-retry' not in self.data['completed'] else 'interrupt')
            self.restore('negative')
            self.call('negative', 'verify')
            self.compare('negative', json.loads((self.work/'source-facts.json').read_text()))
            self.purge('negative')
            self.mark('current-import-failure-retry')
        if 'current-interrupt-retry' not in self.data['completed']:
            name = 'gopulse-current-interrupt-'+os.urandom(6).hex()
            argv = self.argv('negative', 'restore', '--archive', str(self.backup), '--passphrase-file', str(self.key))
            argv[3:3] = ['--name', name]
            proc = subprocess.Popen(argv, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)
            try:
                wait_until(lambda: self.state('negative')['phase'] == 'restore-infrastructure', 'restore interruption stage')
                docker('kill', '--signal', 'SIGTERM', name)
                stdout, stderr = proc.communicate(timeout=180)
                self.output.append(stdout+stderr)
                if proc.returncode != 20:
                    raise RuntimeError('restore interruption did not return stable exit 20')
            finally:
                if proc.poll() is None:
                    docker('kill', '--signal', 'SIGTERM', name)
                    proc.communicate(timeout=180)
            self.empty('negative')
            self.failure_evidence('import' if 'current-import-failure-retry' not in self.data['completed'] else 'interrupt')
            self.restore('negative')
            self.call('negative', 'verify')
            self.compare('negative', json.loads((self.work/'source-facts.json').read_text()))
            self.purge('negative')
            self.mark('current-interrupt-retry')
        if hashlib.sha256(self.backup.read_bytes()).hexdigest() != original:
            raise RuntimeError('authoritative archive changed')
        self.mark('current-product-failure-matrix-passed')
