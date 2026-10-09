"""Lifecycle regression on the actual restored product; no new fixture/project."""
import json
import os
from pathlib import Path
import subprocess
import urllib.request


def verify_reused(install, manifest, image):
    if not install.is_dir() or install.stat().st_mode & 0o077:
        raise RuntimeError('private restored installation required')
    before=json.loads((install/'state.json').read_text())
    receipt=json.loads((install/'restore-result.json').read_text())
    if not receipt['facts_verified'] or (install/'restore-pending.json').exists():
        raise RuntimeError('successful restore fact receipt required')
    socket='/var/run/docker.sock'
    base=['docker','run','--rm','--network','host','--read-only','--cap-drop','ALL','--security-opt','no-new-privileges',
          '--user',str(os.getuid())+':'+str(os.getgid()),'--group-add',str(os.stat(socket).st_gid),'--tmpfs','/tmp',
          '-v',socket+':'+socket,'-v',str(manifest.parent)+':/bundle:ro','-v',str(install)+':'+str(install),image]
    def call(command):
        result=subprocess.run([*base,command,'--install',str(install),'--endpoint','unix://'+socket],capture_output=True,text=True,timeout=900)
        if result.returncode:raise RuntimeError('restored lifecycle '+command+' failed; raw output suppressed')
        return json.loads(result.stdout)
    call('up')
    state_before=(install/'state.json').read_bytes()
    secrets_before=(install/'secrets.json').read_bytes()
    result=call('verify');call('status')
    if state_before!=(install/'state.json').read_bytes() or secrets_before!=(install/'secrets.json').read_bytes():
        raise RuntimeError('read-only verification mutated restored state')
    after=json.loads(state_before)
    for key in ('project','installation_token','manifest_digest','version','edge_port'):
        if after[key]!=before[key]:raise RuntimeError('restored identity changed on lifecycle reuse')
    for path in ['/login','/admin/']:
        with urllib.request.urlopen(result['edge']+path,timeout=10) as response:
            if response.status!=200 or b'<html' not in response.read().lower():raise RuntimeError('restored frontend route unavailable')
    print(json.dumps({'schema':1,'mode':'reuse-install','platform':'linux/amd64','status':'passed','manifest_digest':after['manifest_digest'],'read_only_verify':True,'dual_frontend_routes':True}))
