"""在 47 服务器的独立命名空间恢复备份，不替换当前服务或删除文件。"""

import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import sqlite3
from contextlib import closing
import subprocess
import tarfile
import time

from database_inventory import sqlite_inventory


DATABASE_NAME = 'smail.db'


def run(*args):
    return subprocess.check_output(args, text=True).strip()


def database_path(root):
    data = root / 'data'
    files = sqlite_inventory(root, [data / 'smail.db'])
    expected = data / DATABASE_NAME
    assert files == [expected], '正式验收要求运行目录仅保留 data/smail.db'
    assert expected.is_file() and not expected.is_symlink(), '活动数据库不能是符号链接'
    return expected


def database_counts(root):
    counts = {}
    database_path_value = database_path(root)
    with closing(sqlite3.connect(database_path_value.as_uri() + '?mode=ro', uri=True)) as database:
        assert database.execute('PRAGMA integrity_check').fetchone()[0] == 'ok'
        tables = database.execute("SELECT name FROM sqlite_master WHERE type='table' AND name NOT LIKE 'sqlite_%'").fetchall()
        counts[DATABASE_NAME] = {
            table: database.execute('SELECT COUNT(*) FROM "' + table.replace('"', '""') + '"').fetchone()[0]
            for (table,) in tables
        }
    return counts


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('backup')
    parser.add_argument('--archive-sha', required=True)
    args = parser.parse_args()
    assert os.geteuid() == 0 and run('hostname') == 'ikun2'
    backup = Path(args.backup).resolve(strict=True)
    assert backup.parent == Path('/var/backups') and backup.name.startswith('smail-before-')
    archive = backup / 'smail.tar.gz'
    with archive.open('rb') as stream:
        assert hashlib.file_digest(stream, 'sha256').hexdigest() == args.archive_sha, '备份摘要不匹配'
    memory = dict(line.split(':', 1) for line in Path('/proc/meminfo').read_text().splitlines())
    assert int(memory['MemAvailable'].split()[0]) >= 256 * 1024, '可用内存不足，暂停恢复演练'
    stamp = time.strftime('%Y%m%d-%H%M%S')
    target = Path('/var/tmp') / ('smail-review-restore-' + stamp)
    target.mkdir(mode=0o700)
    with tarfile.open(archive) as stream:
        for member in stream.getmembers():
            path = Path(member.name)
            assert not path.is_absolute() and '..' not in path.parts and path.parts[0] == 'smail'
            assert member.isdir() or member.isfile(), '备份含特殊文件，停止自动恢复'
    subprocess.run(['tar', '-xzf', str(archive), '-C', str(target)], check=True)
    restored = target / 'smail'
    control = target / 'control'
    control.mkdir(mode=0o750)
    shutil.chown(control, user='smail', group='smail')
    before = database_counts(restored)
    live_pid = run('systemctl', 'show', 'smail.service', '--property=MainPID', '--value')
    unit = 'smail-restore-review-' + stamp
    result = {'backup': str(backup), 'restore': str(target), 'unit': unit, 'before': before}
    try:
        subprocess.run([
            'systemd-run', '--unit=' + unit, '--property=User=smail', '--property=Group=smail',
            '--property=PrivateNetwork=yes', '--property=PrivateTmp=yes',
            '--property=ProtectSystem=strict', '--property=ProtectHome=yes',
            '--property=NoNewPrivileges=yes', '--property=CapabilityBoundingSet=CAP_NET_BIND_SERVICE',
            '--property=AmbientCapabilities=CAP_NET_BIND_SERVICE',
            '--property=MemoryMax=128M', '--property=MemorySwapMax=0', '--property=CPUQuota=25%',
            '--property=TasksMax=128', '--property=RuntimeMaxSec=180s', '--property=OOMPolicy=stop',
            '--property=KillMode=control-group', '--property=TimeoutStopSec=10s',
            '--property=BindPaths=' + str(restored) + ':/opt/smail ' + str(control) + ':/run/smail',
            '--property=ReadWritePaths=/opt/smail /run/smail',
            '--property=WorkingDirectory=/opt/smail', '--setenv=SMAIL_ROOT=/opt/smail',
            '--setenv=GOMEMLIMIT=96MiB', '--setenv=GOMAXPROCS=1',
            '/opt/smail/smail',
        ], check=True)
        ready = False
        for _ in range(60):
            pid = run('systemctl', 'show', unit, '--property=MainPID', '--value')
            if pid != '0':
                probe = subprocess.run([
                    'nsenter', '--target', pid, '--net', 'curl', '--silent', '--fail', '--max-time', '2',
                    '--noproxy', '*', '--cacert', str(restored / 'ssl/custom.crt'),
                    '--resolve', 'web.solit.cn:443:127.0.0.1', 'https://web.solit.cn/api/ping',
                ], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
                if probe.returncode == 0:
                    ready = True
                    break
            time.sleep(1)
        assert ready, '恢复服务未就绪'
        assert os.readlink('/proc/' + pid + '/ns/net') != os.readlink('/proc/' + live_pid + '/ns/net')
        assert os.stat('/proc/' + pid + '/root/opt/smail/data/smail.db').st_ino == (restored / 'data/smail.db').stat().st_ino
        assert (restored / 'data/smail.db').stat().st_ino != Path('/opt/smail/data/smail.db').stat().st_ino
        probe = r'''
import smtplib,ssl,sys
from email.message import EmailMessage
message=EmailMessage()
message['From']='restore@review.invalid'
message['To']='remotecheck@solit.cn'
message['Subject']='restore-review'
message['Message-ID']='<restore-'+sys.argv[2]+'@review.invalid>'
message.set_content('restore database write check')
context=ssl.create_default_context(cafile=sys.argv[1])
with smtplib.SMTP('127.0.0.1',25,timeout=20) as client:
 client.ehlo('restore.invalid')
 client._host='mail.solit.cn'
 client.starttls(context=context)
 client.ehlo('restore.invalid')
 assert not client.send_message(message)
'''
        subprocess.run(['nsenter', '--target', pid, '--net', 'python3', '-c', probe,
                        str(restored / 'ssl/custom.crt'), stamp], check=True)
        with closing(sqlite3.connect(f'file:{restored / "data/smail.db"}?mode=ro', uri=True)) as database:
            assert database.execute('SELECT COUNT(*) FROM email WHERE msg_id=?', ('restore-' + stamp + '@review.invalid',)).fetchone()[0] == 1
        with closing(sqlite3.connect('file:/opt/smail/data/smail.db?mode=ro', uri=True)) as database:
            assert database.execute('SELECT COUNT(*) FROM email WHERE msg_id=?', ('restore-' + stamp + '@review.invalid',)).fetchone()[0] == 0
        result.update(ready=True, isolated=True, restored_smtp=True, live_pid_unchanged=live_pid == run(
            'systemctl', 'show', 'smail.service', '--property=MainPID', '--value'))
    finally:
        subprocess.run(['systemctl', 'stop', unit], check=True)
        result['after'] = database_counts(restored)
        result['service_log'] = run('journalctl', '-u', unit, '--no-pager', '-n', '80')
        (target / 'result.json').write_text(json.dumps(result, ensure_ascii=False, indent=2), encoding='utf-8')
    print(json.dumps({key: value for key, value in result.items() if key not in ('before', 'after', 'service_log')}, ensure_ascii=False, indent=2))


if __name__ == '__main__':
    main()
