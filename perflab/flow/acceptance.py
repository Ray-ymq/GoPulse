"""正式执行前核验真实专用环境的缩短全流程验收。"""
import json
from pathlib import Path

from .config import fingerprint
from .environment import source_hashes
from .runtime import STATE, write_json


def check(config, root=STATE):
    try:
        receipt = json.loads((Path(root) / 'accepted.json').read_text())
        result = json.loads((Path(root) / receipt['run_id'] / 'result.json').read_text())
    except (OSError, ValueError, KeyError) as error:
        raise ValueError('真实隔离全流程尚未验收；先执行 make pt-acceptance，正式压测不会启动') from error
    if (receipt.get('source_sha256') != source_hashes() or receipt.get('resources_sha256') != fingerprint(config['resources'])
            or receipt.get('result_sha256') != fingerprint(result) or not result.get('execution_completed') or result.get('exit_code') != 0 or result.get('mode') != 'acceptance'):
        raise ValueError('隔离验收记录已失效或未通过，拒绝正式压测')


def record(run, config, result):
    if result['exit_code'] == 0 and result['execution_completed'] and len([g for g in result['groups'] if g['group_id'].startswith('route-')]) == 22:
        write_json(run.root / 'accepted.json', {'schema_version':'gopulse.pt.acceptance.v1', 'run_id':run.id,
                                              'source_sha256':source_hashes(), 'resources_sha256':fingerprint(config['resources']), 'result_sha256':fingerprint(result)})
