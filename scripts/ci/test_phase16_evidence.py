"""Prove the new aggregation contract, not unchanged product/library behavior."""
import copy
import json
from pathlib import Path
import tempfile
import unittest
from phase16_evidence import atomic, now, SCENARIOS, sha, verify
from test_release_manifest import fixture


class EvidenceTest(unittest.TestCase):
    def test_complete_candidate_and_reject_incomplete_mixed_or_secret_evidence(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            m = fixture(); m['version'] = '1.13.6'
            for p in m['plugins']:
                if p['purpose'] == 'current': p['version'] = '1.13.6'
            atomic(root/'release-manifest.json',m); identity = sha(root/'release-manifest.json')
            runner = 'sha256:'+'f'*64
            stamp = now()
            document = {'schema':'gopulse.phase16.v1','complete':True,'status':'passed','redacted':True,
                'candidate':m,'manifest_sha256':identity,'bundle_archive_sha256':'b'*64,
                'inventory':dict(host_os='Linux',host_arch='x86_64',server_os='linux',server_arch='amd64',kernel='kernel',compose='compose',cpu_count=4,memory_bytes=100,disk_free_bytes=100),
                'runner':{'image':runner,'ref':'registry/acceptance@'+runner,'revision':m['revision']},
                'projects':{n:{'project_sha256':str(i)*64,'token_sha256':str(i+4)*64} for i,n in enumerate(('source','target','second','negative'),1)},
                'scenarios':{n:{'status':'passed','started_at':stamp,'finished_at':stamp,'manifest_sha256':identity,'revision':m['revision'],'bundle_archive_sha256':'b'*64,'facts':{'unrelated_resources_unchanged':True},'files':[]} for n in SCENARIOS}}
            attachments = {'artifact-runtime.json':dict(manifest_sha256='sha256:'+identity,revision=m['revision'],platform='linux/amd64',status='amd64-runtime-and-compose-passed')}
            for mode in ('clean-install','failure-matrix'):
                attachments[mode+'.json'] = dict(status='passed',manifest_sha256=identity,mode=mode,isolation_preserved=True,cleanup_passed=True,commands=['real fixture call'])
            recovery = dict(status='passed',manifest_sha256=identity,candidate=m,
                completed=['current-product-two-restores-passed','current-product-failure-matrix-passed','owned-cleanup-and-isolation-passed','wrong-passphrase-and-tamper-rejected-before-resources','source-three-source-facts','target-nonempty-restore-rejected','second-nonempty-restore-rejected'],
                browsers={n:dict(checks=['desktop','narrow','three-source-create'],timezone='Asia/Shanghai',runner_image=runner,product_revision=m['revision'],runner_revision=m['revision']) for n in ('source','target','second')},
                three_sources={'source':dict(metrics={},logs={},events={})},failures={'import':{},'interrupt':{}},backups={'source':{},'target':{}},restores={n:{'facts_verified':True} for n in ('target','second')})
            attachments['recovery.json'] = recovery
            for filename, content in attachments.items(): atomic(root/filename,content)
            document['scenarios'][SCENARIOS[0]]['files'] = [{'path':n,'sha256':sha(root/n)} for n in [*attachments,'release-manifest.json']]
            verify(document,root)
            changes = [lambda d:d.update(complete=False),lambda d:d['scenarios'].pop('owned-cleanup'),
                       lambda d:d['scenarios']['current-product'].update(manifest_sha256='c'*64),
                       lambda d:d['inventory'].update(server_arch='arm64'),
                       lambda d:d.update(secret='Recovery-123456789abc-password'),
                       lambda d:d['runner'].update(revision='c'*40)]
            for change in changes:
                value = copy.deepcopy(document);change(value)
                with self.subTest(change=change),self.assertRaises(ValueError):verify(value,root)
            (root/'recovery.json').write_text('{}')
            with self.assertRaisesRegex(ValueError,'checksum'):verify(document,root)

if __name__ == '__main__': unittest.main()
