"""补齐本轮删除语料；执行前必须先保存原始数据库快照。"""
import json
import shutil
from environment import ROOT, STATE, sql

if not (STATE/'snapshots/before-preparation.sql').exists():
    raise SystemExit('缺少改动前快照，停止准备数据')
if (STATE/'snapshots/baseline.sql').exists():
    raise SystemExit('正式快照已存在，拒绝再次准备数据')
progress=STATE/'seed-progress.json'
if progress.exists():
    corpus=json.loads(progress.read_text())
else:
    corpus=json.loads((ROOT/'.run/load/corpus.json').read_text())
    corpus['delete_post_ids']=[[] for _ in corpus['users']]
for start in range(0,len(corpus['users']),16):
    if len(corpus['delete_post_ids'][start])==256:
        continue
    values=[]
    for actor in range(start,min(start+16,len(corpus['users']))):
        user=int(corpus['users'][actor]['id'])
        values.extend(f"({user},'pt3 delete {actor} {item}','pt3 deterministic delete corpus {actor} {item}')" for item in range(256))
    first,count=map(int,sql('INSERT INTO posts(author_id,title,content) VALUES '+','.join(values)+'; SELECT LAST_INSERT_ID(),ROW_COUNT();').strip().split('\t'))
    if count!=len(values):raise RuntimeError('插入数量不符')
    for offset,actor in enumerate(range(start,min(start+16,len(corpus['users'])))):
        corpus['delete_post_ids'][actor]=list(range(first+offset*256,first+(offset+1)*256))
    progress.write_text(json.dumps(corpus));progress.chmod(0o600)
    print('prepared actors',start+16,flush=True)
path=STATE/'corpus.json';path.write_text(json.dumps(corpus));path.chmod(0o600)
shutil.copyfile(ROOT/'.run/load/credentials.json',STATE/'credentials.json')
(STATE/'credentials.json').chmod(0o600)
print('删除语料总数',sum(map(len,corpus['delete_post_ids'])))
