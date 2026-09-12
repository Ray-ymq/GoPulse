#!/usr/bin/env python3
"""Build, inspect and promote immutable candidates. Never publishes partial manifests."""
import argparse
import gzip
import io
import json
import os
from pathlib import Path
import re
import subprocess
import tarfile
import tempfile
import urllib.request

from release_manifest import ROOT, PRODUCTS, SOURCES, PLATFORMS, load, sha, validate, payload_digest


def run(*args, capture=True, **kwargs):
    return subprocess.run(args, check=True, text=True, stdout=subprocess.PIPE if capture else None, **kwargs).stdout


def raw(ref):
    return run('docker', 'buildx', 'imagetools', 'inspect', '--raw', ref).encode()


def image_record(ref):
    data = raw(ref)
    doc = json.loads(data)
    platforms = {}
    for item in doc['manifests']:
        p = item.get('platform', {})
        platform = p.get('os', '') + '/' + p.get('architecture', '')
        if platform in PLATFORMS:
            if platform in platforms:
                raise ValueError('duplicate image platform')
            platforms[platform] = item['digest']
    if set(platforms) != set(PLATFORMS):
        raise ValueError('missing image platform')
    return {'ref': ref.split('@')[0]+'@'+sha(data), 'platforms': platforms}


def platform_ref(image, platform):
    return image['ref'].split('@')[0]+'@'+image['platforms'][platform]


def inspect_image(image, platform, version, revision, product=True):
    ref = platform_ref(image, platform)
    index = raw(image['ref'])
    if sha(index) != image['ref'].split('@')[1]:
        raise ValueError('index digest mismatch')
    selected = image_record(image['ref'])
    if selected['platforms'] != image['platforms']:
        raise ValueError('index platform digest mismatch')
    data = raw(ref)
    if sha(data) != image['platforms'][platform]:
        raise ValueError('platform manifest digest mismatch')
    doc = json.loads(data)
    if not doc.get('layers') or not re.fullmatch(r'sha256:[0-9a-f]{64}', doc['config']['digest']):
        raise ValueError('missing config/layers')
    for layer in doc['layers']:
        if layer['size'] <= 0 or not re.fullmatch(r'sha256:[0-9a-f]{64}', layer['digest']):
            raise ValueError('invalid layer metadata')
    cfg = json.loads(run('docker', 'buildx', 'imagetools', 'inspect', '--format', '{{json .Image}}', ref))
    if cfg['os']+'/'+cfg['architecture'] != platform or len(cfg['rootfs']['diff_ids']) != len(doc['layers']):
        raise ValueError('config platform/layers mismatch')
    if product:
        c = cfg['config']
        labels = c.get('Labels', {})
        expected = {'version': version, 'revision': revision, 'source': 'https://github.com/Ray-ymq/GoPulse'}
        for key, value in expected.items():
            if labels.get('org.opencontainers.image.'+key) != value:
                raise ValueError(f'OCI {key} mismatch')
        if not labels.get('org.opencontainers.image.title') or not re.fullmatch(r'[0-9]+:[0-9]+', c.get('User', '')) or not c.get('Entrypoint'):
            raise ValueError('missing numeric user/title/entrypoint')
    return cfg


def plugin_records(image, platform, output):
    """Pull/copy only: never execute foreign-architecture plugin binaries."""
    ref = platform_ref(image, platform)
    run('docker', 'pull', '--platform', platform, ref, capture=False)
    container = run('docker', 'create', '--platform', platform, ref).strip()
    try:
        output.mkdir(parents=True, exist_ok=True)
        run('docker', 'cp', container+':/opt/gopulse/packages/.', str(output), capture=False)
    finally:
        run('docker', 'rm', '-v', container)
    records = []
    files = ['gopulse-'+s+'-exporter.tar.gz' for s in SOURCES]
    if platform == 'linux/amd64':
        files.append('redis-1.9.4.tar.gz')
    for name in files:
        data = (output/name).read_bytes()
        with tarfile.open(fileobj=io.BytesIO(data), mode='r:gz') as archive:
            m = json.load(archive.extractfile('plugin.json'))
            binary = archive.extractfile(m['entrypoint']).read()
            if sha(binary)[7:] != m['entrypoint_sha256']:
                raise ValueError('plugin entrypoint checksum mismatch')
            arch = platform.split('/')[1]
            machine = int.from_bytes(binary[18:20], 'little')
            if binary[:4] != b'\x7fELF' or machine != {'amd64':62, 'arm64':183}[arch] or m['arch'] != arch or m['os'] != 'linux':
                raise ValueError('plugin ELF/manifest architecture mismatch')
            schema = ''
            if m['schema_version'] == 2:
                schema = sha(archive.extractfile('config.schema.json').read())
                if schema[7:] != m['config_schema_sha256']:
                    raise ValueError('plugin schema digest mismatch')
            records.append(dict(id=m['id'], version=m['version'], os=m['os'], arch=m['arch'],
                                purpose='current' if schema else 'upgrade-only', archive_sha256=sha(data),
                                entrypoint_sha256=sha(binary), schema_sha256=schema))
    return records


def product_compose(m):
    doc = json.loads(run('docker', 'compose', '--env-file', str(ROOT/'.env.example'), '-f', str(ROOT/'deploy/compose.yaml'),
                         'config', '--no-interpolate', '--no-path-resolution', '--format', 'json'))
    doc.pop('name', None)
    doc['services'].pop('acceptance')
    aliases = {'migrate':'backend', 'search-init':'backend', 'admin-role':'backend', 'kafka-init':'kafka'}
    for name, service in doc['services'].items():
        service.pop('build', None)
        logical = aliases.get(name, name)
        image = m['images'].get(logical) or m['third_party'].get(logical)
        if image is None:
            raise ValueError('unmapped product service: '+name)
        service['image'] = image['ref']
        service['pull_policy'] = 'always'
    doc['services']['lifecycle'] = {'image':m['lifecycle']['ref'], 'profiles':['tools'], 'read_only':True,
                                   'network_mode':'none', 'user':'10001:10001', 'cap_drop':['ALL'],
                                   'security_opt':['no-new-privileges:true'], 'volumes':['../../:/bundle:ro'],
                                   'command':['version','--json','--manifest','/bundle/release-manifest.json']}
    return (json.dumps(doc, indent=2)+'\n').encode()


def write_bundle(m, out):
    files = {'deploy/product/compose.yaml': product_compose(m),
             'README.md': (ROOT/'deploy/release/BUNDLE-README.md').read_bytes()}
    m['compose'] = {'path':'deploy/product/compose.yaml', 'sha256':sha(files['deploy/product/compose.yaml'])}
    m['bundle_sha256'] = payload_digest(files)
    validate(m)
    files['release-manifest.json'] = (json.dumps(m, indent=2)+'\n').encode()
    files['checksums'] = ''.join(f'{sha(data)[7:]}  {name}\n' for name, data in sorted(files.items())).encode()
    for name, data in files.items():
        p=out/name;p.parent.mkdir(parents=True,exist_ok=True);p.write_bytes(data)
    name = 'gopulse-'+m['version']+'-bundle.tar.gz'
    with open(out/name, 'wb') as stream:
        with gzip.GzipFile(filename='', mode='wb', fileobj=stream, mtime=0) as gz:
            with tarfile.open(fileobj=gz, mode='w', format=tarfile.USTAR_FORMAT) as archive:
                for path, data in sorted(files.items()):
                    info=tarfile.TarInfo(path);info.size=len(data);info.mode=0o644;info.mtime=0
                    archive.addfile(info,io.BytesIO(data))
    (out/(name+'.sha256')).write_text(sha((out/name).read_bytes())[7:]+'  '+name+'\n')
    verify_bundle(out/'release-manifest.json')


def verify_bundle(path):
    m=validate(load(path));root=path.parent
    files={name:(root/name).read_bytes() for name in ('deploy/product/compose.yaml','README.md')}
    if sha(files[m['compose']['path']])!=m['compose']['sha256'] or payload_digest(files)!=m['bundle_sha256']:
        raise ValueError('bundle asset checksum mismatch')
    allowed=set(files)|{'release-manifest.json','checksums'}
    archive=root/('gopulse-'+m['version']+'-bundle.tar.gz')
    detached=(root/(archive.name+'.sha256')).read_text()
    if detached!=sha(archive.read_bytes())[7:]+'  '+archive.name+'\n':raise ValueError('archive checksum mismatch')
    with tarfile.open(archive,'r:gz') as tar:
        members=tar.getmembers()
        if len(members)!=len(allowed) or {p.name for p in members}!=allowed:raise ValueError('bundle allowlist mismatch')
        for member in members:
            if not member.isfile() or member.mode!=0o644 or member.uid!=0 or member.gid!=0:raise ValueError('unsafe bundle entry')
            data=tar.extractfile(member).read()
            if data!=(root/member.name).read_bytes() or b'\r' in data or b'\x00' in data:raise ValueError('bundle bytes or LF mismatch')
    checks=''.join(f'{sha((root/name).read_bytes())[7:]}  {name}\n' for name in sorted(allowed-{'checksums'}))
    if (root/'checksums').read_text()!=checks:raise ValueError('bundle checksums mismatch')
    return m


def build(args):
    if run('git','-C',str(ROOT),'status','--porcelain','--untracked-files=no').strip():
        raise ValueError('candidate builds require a committed source tree')
    revision=run('git','-C',str(ROOT),'rev-parse','HEAD').strip()
    version=(ROOT/'VERSION').read_text().strip()
    if not re.fullmatch(r'[a-zA-Z0-9][a-zA-Z0-9.:-]*(/[a-z0-9._-]+)*',args.registry):raise ValueError('invalid registry namespace')
    out=Path(args.output).resolve();out.mkdir(parents=True,exist_ok=True)
    if (out/'release-manifest.json').exists():raise ValueError('refusing to overwrite complete candidate')
    images={}
    for name in (*PRODUCTS,'lifecycle'):
        if name in ('backend','business-worker','search-indexer'):dockerfile='backend';target=name
        elif name in ('router','marshaller','monitor','redis-exporter'):dockerfile='observability';target=name
        else:dockerfile=name;target=None
        tag=f'{args.registry}/{name}:{version}-candidate-{revision[:12]}'
        cmd=['docker','buildx','build','--platform',','.join(PLATFORMS),'--provenance=false',
             '--build-arg','VERSION='+version,'--build-arg','REVISION='+revision,
             '--metadata-file',str(out/(name+'-build.json')),'-f',f'deploy/docker/{dockerfile}.Dockerfile','-t',tag,'--push']
        if target:cmd+=['--target',target]
        # A git archive is the build context: untracked local state cannot leak.
        context=subprocess.Popen(['git','-C',str(ROOT),'archive','--format=tar',revision],stdout=subprocess.PIPE)
        try:subprocess.run(cmd+['-'],stdin=context.stdout,check=True)
        finally:context.stdout.close()
        if context.wait()!=0:raise ValueError('git archive failed')
        images[name]=image_record(tag)
    third=load(ROOT/'deploy/release/third-party.lock.json')
    # Normalized logical name differs from the VictoriaMetrics repository name.
    third['victoriametrics']=third.pop('victoria-metrics',third.get('victoriametrics'))
    m=dict(schema_version=1,version=version,revision=revision,images={n:images[n] for n in PRODUCTS},
           lifecycle=images['lifecycle'],third_party=third,plugins=[],supported_upgrade_sources=[])
    for platform in PLATFORMS:
        m['plugins']+=plugin_records(images['monitor'],platform,out/'plugins'/platform.split('/')[1])
        for image in images.values():inspect_image(image,platform,version,revision)
    # Publish the manifest last: a failed bundle assembly is never a complete release.
    with tempfile.TemporaryDirectory(prefix='.bundle-', dir=out) as stage:
        staging=Path(stage)
        write_bundle(m,staging)
        for path in sorted(staging.rglob('*')):
            if path.is_file() and path.name!='release-manifest.json':
                dest=out/path.relative_to(staging);dest.parent.mkdir(parents=True,exist_ok=True)
                os.replace(path,dest)
        os.replace(staging/'release-manifest.json',out/'release-manifest.json')
    print('Candidate complete; not externally published:',out/'release-manifest.json')


def promote(path):
    m=verify_bundle(path)
    for arch in ('amd64','arm64'):
        receipt=load(path.parent/('verification-'+arch+'.json'))
        expected='amd64-runtime-and-compose-passed' if arch=='amd64' else 'metadata-only; real arm64 runtime DEFERRED to Phase-16-06'
        if receipt != {'manifest_sha256':sha(path.read_bytes()),'revision':m['revision'],'platform':'linux/'+arch,'status':expected}:
            raise ValueError('candidate lacks matching successful verification receipts')
    for image in [*m['images'].values(),m['lifecycle']]:
        ref=image['ref'];repo=ref.split('@')[0].rsplit(':',1)[0]
        target=repo+':'+m['version']
        existing=subprocess.run(['docker','buildx','imagetools','inspect','--raw',target],text=True,capture_output=True)
        if existing.returncode==0:
            if sha(existing.stdout.encode())!=ref.split('@')[1]:
                raise ValueError('refusing to replace an existing version with different content')
        elif 'not found' not in existing.stderr.lower():
            raise ValueError('cannot establish whether promotion target exists')
        # Copy the exact index, not a rebuilt or reserialized single-arch image.
        run('docker','buildx','imagetools','create','--prefer-index=true','-t',target,ref,capture=False)
        if sha(raw(target))!=ref.split('@')[1]:raise ValueError('promotion changed index digest')
    print('Same-digest promotion verified; bundle checksum unchanged.')


def main():
    parser=argparse.ArgumentParser();sub=parser.add_subparsers(dest='cmd',required=True)
    b=sub.add_parser('build');b.add_argument('--registry',required=True);b.add_argument('--output',default='dist')
    p=sub.add_parser('promote');p.add_argument('--manifest',type=Path,required=True)
    args=parser.parse_args()
    if args.cmd=='build':build(args)
    else:promote(args.manifest)

if __name__=='__main__':main()
