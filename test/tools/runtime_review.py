"""在 47 服务器创建空数据副本并进行受限验收，不停止线上服务或删除文件。"""

import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import sqlite3
import subprocess
import time


def run(*args):
    return subprocess.check_output(args, text=True).strip()


def service_state():
    return {name: run('systemctl', 'show', name, '--property=MainPID,NRestarts,ActiveState')
            for name in ('smail.service', 'mysql.service', 'Scert.service')}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--main-sha', required=True)
    parser.add_argument('--binary', type=Path, default=Path('/opt/smail/smail'))
    args = parser.parse_args()
    assert os.geteuid() == 0 and run('hostname') == 'ikun2'
    os.umask(0o027)
    live = Path('/opt/smail')
    binary = args.binary.resolve(strict=True)
    assert binary == args.binary and binary.is_file()
    if binary != live / 'smail':
        assert binary.parent.parent == Path('/var/tmp') and binary.parent.name.startswith('smail-review-')
        assert binary.stat().st_uid == 0 and binary.stat().st_mode & 0o022 == 0
        assert binary.parent.stat().st_uid == 0 and binary.parent.stat().st_mode & 0o022 == 0
    with binary.open('rb') as stream:
        assert hashlib.file_digest(stream, 'sha256').hexdigest() == args.main_sha
        stream.seek(0)
        header = stream.read(20)
        assert header[:5] == b'\x7fELF\x02' and header[18:20] == b'\x3e\x00'
    with (live / 'smail').open('rb') as stream:
        live_sha = hashlib.file_digest(stream, 'sha256').hexdigest()
    memory = dict(line.split(':', 1) for line in Path('/proc/meminfo').read_text().splitlines())
    assert int(memory['MemAvailable'].split()[0]) >= 272 * 1024, '可用内存不足，暂停隔离验收'
    stamp = time.strftime('%Y%m%d-%H%M%S')
    target = Path('/var/tmp') / ('smail-review-runtime-' + stamp)
    target.mkdir(mode=0o750)
    shutil.chown(target, user='smail', group='smail')
    root = target / 'instance'
    control = target / 'control'
    for directory in (root, control, root / 'ssl'):
        directory.mkdir(mode=0o750)
        shutil.chown(directory, user='smail', group='smail')
    shutil.copy2(binary, root / 'smail')
    (root / 'smail').chmod(0o755)
    shutil.copy2(Path(__file__).with_name('runtime_probe.py'), target / 'runtime_probe.py')
    subprocess.run(['openssl', 'req', '-x509', '-newkey', 'rsa:2048', '-nodes', '-days', '2',
                    '-keyout', str(root / 'ssl/runtime.key'), '-out', str(root / 'ssl/runtime.crt'),
                    '-subj', '/CN=mail.runtime.invalid', '-addext',
                    'subjectAltName=DNS:runtime.invalid,DNS:*.runtime.invalid'],
                   check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    for name in ('runtime.key', 'runtime.crt'):
        path = root / 'ssl' / name
        path.chmod(0o600)
        shutil.chown(path, user='smail', group='smail')
    assert not (root / 'data').exists() and not (root / 'config.json').exists()
    before = service_state()
    unit = 'smail-runtime-review-' + stamp
    probe_unit = 'smail-runtime-probe-' + stamp
    result = {'target': str(target), 'unit': unit, 'main_sha256': args.main_sha,
              'source': str(binary), 'live_sha256': live_sha, 'before': before}
    started = time.strftime('%Y-%m-%dT%H:%M:%S')
    try:
        subprocess.run([
            'systemd-run', '--unit=' + unit, '--property=Type=exec',
            '--property=User=smail', '--property=Group=smail',
            '--property=PrivateNetwork=yes', '--property=PrivateTmp=yes',
            '--property=ProtectSystem=strict', '--property=ProtectHome=yes',
            '--property=NoNewPrivileges=yes', '--property=CapabilityBoundingSet=CAP_NET_BIND_SERVICE',
            '--property=AmbientCapabilities=CAP_NET_BIND_SERVICE',
            '--property=MemoryMax=128M', '--property=MemorySwapMax=0', '--property=CPUQuota=25%',
            '--property=TasksMax=128', '--property=RuntimeMaxSec=300s', '--property=OOMPolicy=stop',
            '--property=KillMode=control-group', '--property=TimeoutStopSec=10s',
            '--property=BindPaths=' + str(root) + ':/opt/smail ' + str(control) + ':/run/smail',
            '--property=ReadWritePaths=/opt/smail /run/smail',
            '--property=WorkingDirectory=/opt/smail', '--setenv=SMAIL_ROOT=/opt/smail',
            '--setenv=GOMEMLIMIT=64MiB', '--setenv=GOMAXPROCS=1', '/opt/smail/smail',
        ], check=True)
        pid = run('systemctl', 'show', unit, '--property=MainPID', '--value')
        assert pid != '0'
        live_net = str(Path('/proc/self/ns/net').readlink())
        assert str(Path('/proc/' + pid + '/ns/net').readlink()) != live_net
        assert Path('/proc/' + pid + '/root/opt/smail/smail').stat().st_ino == (root / 'smail').stat().st_ino
        assert (root / 'smail').stat().st_ino != (live / 'smail').stat().st_ino
        probe = subprocess.run([
            'systemd-run', '--wait', '--pipe', '--unit=' + probe_unit,
            '--property=User=smail', '--property=Group=smail',
            '--property=NetworkNamespacePath=/proc/' + pid + '/ns/net',
            '--property=PrivateTmp=no', '--property=ProtectSystem=strict', '--property=ProtectHome=yes',
            '--property=NoNewPrivileges=yes', '--property=InaccessiblePaths=/opt/smail /run/smail',
            '--property=ReadWritePaths=' + str(target), '--property=MemoryMax=48M',
            '--property=MemorySwapMax=0', '--property=CPUQuota=25%', '--property=TasksMax=32',
            '--property=RuntimeMaxSec=240s', '--property=OOMPolicy=stop',
            '--property=KillMode=control-group', '--property=TimeoutStopSec=10s',
            'python3', str(target / 'runtime_probe.py'), str(root), '--live-net', live_net,
        ], text=True, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=270)
        (target / 'probe.log').write_text(probe.stdout, encoding='utf-8')
        result['probe_code'] = probe.returncode
        if (target / 'probe-result.json').exists():
            result['probe'] = json.loads((target / 'probe-result.json').read_text())
        assert probe.returncode == 0, '隔离验收失败，请检查保留的 probe.log'
        assert result['probe']['passed']
        with sqlite3.connect(f'file:{live / "data/smail.db"}?mode=ro', uri=True) as database:
            message_id = result['probe']['smtp']['message_id']
            assert database.execute('SELECT COUNT(*) FROM email WHERE msg_id=?', (message_id,)).fetchone()[0] == 0
        result['live_message_absent'] = True
    finally:
        result['resources'] = run('systemctl', 'show', unit,
                                  '--property=MemoryPeak,MemoryCurrent,Result,ExecMainStatus,NRestarts')
        subprocess.run(['systemctl', 'stop', unit], check=True)
        subprocess.run(['systemctl', 'stop', probe_unit], check=False, stderr=subprocess.DEVNULL)
        result['after'] = service_state()
        with (live / 'smail').open('rb') as stream:
            result['live_unchanged'] = (result['after'] == before
                                        and hashlib.file_digest(stream, 'sha256').hexdigest() == live_sha)
        result['integrity'] = {}
        for name in ('smail.db', 'smail-state.db'):
            path = root / 'data' / name
            if path.exists():
                with sqlite3.connect(f'file:{path}?mode=ro', uri=True) as database:
                    result['integrity'][name] = database.execute('PRAGMA integrity_check').fetchall()
        (target / 'service.log').write_text(run('journalctl', '-u', unit, '--no-pager'), encoding='utf-8')
        kernel = run('journalctl', '-k', '--since', started, '--no-pager')
        result['oom_events'] = sum('oom-kill:constraint=' in line for line in kernel.splitlines())
        (target / 'result.json').write_text(json.dumps(result, ensure_ascii=False, indent=2), encoding='utf-8')
        print(json.dumps(result, ensure_ascii=False, indent=2))
    assert result['live_unchanged'] and result['oom_events'] == 0
    assert result['integrity'] == {'smail.db': [('ok',)], 'smail-state.db': [('ok',)]}


if __name__ == '__main__':
    main()
