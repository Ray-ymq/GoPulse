import datetime
import json
import tempfile
import unittest
from pathlib import Path
from phase17_evidence import REQUIRED, atomic, sha, verify

class Phase17EvidenceTest(unittest.TestCase):
    def test_complete_contract_and_rejections(self):
        with tempfile.TemporaryDirectory() as tmp:
            root=Path(tmp);(root/'attachments').mkdir();manifest=root/'attachments/release-manifest.json'
            manifest.write_text(json.dumps({'revision':'a'*40})+'\n')
            ref={'path':'attachments/release-manifest.json','sha256':sha(manifest),'redacted':True}
            stamp=datetime.datetime.now(datetime.timezone.utc).isoformat()
            scene={'status':'passed','reason_code':'accepted','manifest_sha256':sha(manifest),'revision':'a'*40,
                   'started_at':stamp,'finished_at':stamp,'facts':{'ok':True},'attachments':[ref]}
            doc={'schema':'gopulse.phase17.v1','status':'passed','complete':True,
                 'candidate':{'version':'1.14.5','revision':'a'*40,'manifest_sha256':'sha256:'+sha(manifest),
                    'bundle_sha256':'sha256:'+'b'*64,'runtime_contract_sha256':'sha256:'+'c'*64},
                 'inventory':{'host_os':'Linux','host_arch':'x86_64','server_platform':'linux/amd64'},
                 'scenarios':{name:dict(scene) for name in REQUIRED},'project_hashes':['d'*64],
                 'secret_scan':'passed','cleanup':'passed'}
            verify(doc,root)
            manifest.write_text('{"revision":"'+'a'*40+'","compose_value":"${AUTH_JWT_SECRET:?AUTH_JWT_SECRET is required}"}\n')
            ref['sha256']=sha(manifest)
            doc['candidate']['manifest_sha256']='sha256:'+sha(manifest)
            for value in doc['scenarios'].values(): value['manifest_sha256']=sha(manifest)
            verify(doc,root)
            bad=json.loads(json.dumps(doc));bad['scenarios'].pop(next(iter(REQUIRED)))
            with self.assertRaises(ValueError):verify(bad,root)
            manifest.write_text('{"password":"visible-secret-value"}\n')
            ref['sha256']=sha(manifest)
            doc['candidate']['manifest_sha256']='sha256:'+sha(manifest)
            for value in doc['scenarios'].values(): value['manifest_sha256']=sha(manifest)
            with self.assertRaisesRegex(ValueError, 'sensitive value'):verify(doc,root)

if __name__=='__main__':unittest.main()
