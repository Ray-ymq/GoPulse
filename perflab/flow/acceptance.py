"""记录固定 PT 的独立缩短流程验收。"""

from .config import fingerprint
from .environment import source_hashes
from .runtime import write_json


def record(run, config, result):
    if result['exit_code'] == 0 and result['execution_completed'] and len([g for g in result['groups'] if g['group_id'].startswith('route-')]) == 22:
        write_json(run.root / 'accepted.json', {'schema_version':'gopulse.pt.acceptance.v1', 'run_id':run.id,
                                              'source_sha256':source_hashes(), 'resources_sha256':fingerprint(config['resources']), 'result_sha256':fingerprint(result)})
