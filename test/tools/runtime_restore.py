"""对已停止的隔离实例执行完整冷备、空目录还原与业务读取，不操作正式实例。"""

import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import tarfile
import time

from runtime_review import collect_and_stop, database_integrity, run, service_state


def archive_members(archive):
    members = archive.getmembers()
    for member in members:
        path = Path(member.name)
        assert not path.is_absolute() and '..' not in path.parts
        assert path.parts and path.parts[0] == 'instance'
        assert member.isdir() or member.isfile(), '备份包含链接或特殊文件'
    return members


def tree_digest(root):
    result = {}
    for path in sorted(root.rglob('*')):
        assert not path.is_symlink(), '隔离备份拒绝链接'
        if path.is_file():
            with path.open('rb') as stream:
                result[str(path.relative_to(root))] = hashlib.file_digest(stream, 'sha256').hexdigest()
    return result


def verify_backup_restore(source, evidence, password, proxy):
    assert os.geteuid() == 0 and run('hostname') == 'ikun2'
    source = source.resolve(strict=True)
    assert source.parent.parent == Path('/var/tmp')
    assert source.parent.name.startswith('smail-review-runtime-') and source.name == 'instance'
    assert run('systemctl', 'show', evidence['unit'], '--property=MainPID', '--value') == '0'
    database_integrity(source)
    before = service_state()
    source_digest = tree_digest(source)
    stamp = time.strftime('%Y%m%d-%H%M%S')
    target = Path('/var/tmp') / ('smail-review-restore-' + stamp)
    target.mkdir(mode=0o750)
    shutil.chown(target, user='smail', group='smail')
    archive_path = target / 'cold-backup.tar.gz'
    with tarfile.open(archive_path, 'x:gz') as archive:
        archive.add(source, arcname='instance')
    archive_path.chmod(0o600)
    with tarfile.open(archive_path, 'r:gz') as archive:
        members = archive_members(archive)
        assert not (target / 'instance').exists()
        archive.extractall(target, members=members, filter='data')
    root = target / 'instance'
    assert tree_digest(root) == source_digest
    assert (root / 'data/smail.db').stat().st_ino != (source / 'data/smail.db').stat().st_ino
    for path in [root, *root.rglob('*')]:
        shutil.chown(path, user='smail', group='smail')
    control = target / 'control'
    control.mkdir(mode=0o750)
    shutil.chown(control, user='smail', group='smail')
    probe_path = target / 'runtime_probe.py'
    shutil.copy2(Path(__file__).with_name('runtime_probe.py'), probe_path)
    probe_path.chmod(0o640)
    shutil.chown(probe_path, user='root', group='smail')
    unit = 'smail-runtime-review-' + stamp
    probe_unit = 'smail-runtime-probe-' + stamp
    result = {'source': str(source), 'target': str(target), 'file_count': len(source_digest),
              'archive_sha256': hashlib.sha256(archive_path.read_bytes()).hexdigest(),
              'empty_target': True, 'byte_identical': True, 'cold_backup': True}
    started = time.monotonic()
    try:
        subprocess.run([
            'systemd-run', '--unit=' + unit, '--property=Type=exec',
            '--property=User=smail', '--property=Group=smail',
            '--property=PrivateNetwork=yes', '--property=PrivateTmp=yes',
            '--property=ProtectSystem=strict', '--property=ProtectHome=yes',
            '--property=NoNewPrivileges=yes', '--property=CapabilityBoundingSet=CAP_NET_BIND_SERVICE',
            '--property=AmbientCapabilities=CAP_NET_BIND_SERVICE',
            '--property=MemoryMax=192M', '--property=MemorySwapMax=0', '--property=CPUQuota=25%',
            '--property=TasksMax=128', '--property=RuntimeMaxSec=180s', '--property=OOMPolicy=stop',
            '--property=KillMode=control-group', '--property=TimeoutStopSec=10s',
            '--property=BindPaths=' + str(root) + ':/opt/smail ' + str(control) + ':/run/smail',
            '--property=ReadWritePaths=/opt/smail /run/smail',
            '--property=WorkingDirectory=/opt/smail', '--setenv=SMAIL_ROOT=/opt/smail',
            '--setenv=GOMEMLIMIT=64MiB', '--setenv=GOMAXPROCS=1', '/opt/smail/smail',
        ], check=True)
        pid = run('systemctl', 'show', unit, '--property=MainPID', '--value')
        live_net = str(Path('/proc/self/ns/net').readlink())
        assert pid != '0' and str(Path('/proc/' + pid + '/ns/net').readlink()) != live_net
        assert Path('/proc/' + pid + '/root/opt/smail/smail').stat().st_ino == (root / 'smail').stat().st_ino
        probe = subprocess.run([
            'systemd-run', '--wait', '--pipe', '--unit=' + probe_unit,
            '--property=User=smail', '--property=Group=smail',
            '--property=NetworkNamespacePath=/proc/' + pid + '/ns/net',
            '--property=ProtectSystem=strict', '--property=ProtectHome=yes',
            '--property=NoNewPrivileges=yes', '--property=InaccessiblePaths=/opt/smail /run/smail',
            '--property=ReadWritePaths=' + str(target), '--property=MemoryMax=48M',
            '--property=MemorySwapMax=0', '--property=CPUQuota=25%', '--property=TasksMax=32',
            '--property=RuntimeMaxSec=150s', '--property=OOMPolicy=stop',
            '--property=KillMode=control-group', '--property=TimeoutStopSec=10s',
            'python3', str(probe_path), str(root), '--live-net', live_net, '--password-stdin',
            '--restore-message-id', evidence['probe']['smtp']['message_id'], *(['--proxy'] if proxy else []),
        ], input=password + '\n', text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=165)
        (target / 'probe.log').write_text(probe.stdout, encoding='utf-8')
        result['probe_code'] = probe.returncode
        assert probe.returncode == 0, '还原业务验收失败，请检查保留的日志'
        result['probe'] = json.loads((target / 'restore-probe-result.json').read_text())
        assert result['probe']['passed']
    finally:
        collect_and_stop(result, unit, probe_unit)
        result['live_unchanged'] = service_state() == before
        result['source_unchanged'] = tree_digest(source) == source_digest
        result['seconds'] = round(time.monotonic() - started, 2)
        result['integrity'] = database_integrity(root)
        (target / 'service.log').write_text(run('journalctl', '-u', unit, '--no-pager'), encoding='utf-8')
        (target / 'result.json').write_text(json.dumps(result, ensure_ascii=False, indent=2), encoding='utf-8')
    assert result['live_unchanged'] and result['source_unchanged']
    assert not result['cleanup_errors'] and not result['resource_errors']
    assert 'Result=success' in result['resources'] and 'MemorySwapPeak=0' in result['resources']
    return result
