"""仅在 47 服务器备份并升级 Smail，保留单库和失败证据。"""

import argparse
import hashlib
import json
import os
from pathlib import Path
import shutil
import sqlite3
import subprocess
import time


ROOT = Path('/opt/smail')
STAGING = Path('/var/tmp/smail-review-20260917')
DATABASE_NAME = 'smail.db'


def run(*args):
    return subprocess.check_output(args, text=True).strip()


def digest(path):
    with path.open('rb') as stream:
        return hashlib.file_digest(stream, 'sha256').hexdigest()


def database_path(root):
    data = root / 'data'
    files = sorted(path for path in data.rglob('*.db')
                   if path.is_file() or path.is_symlink())
    expected = data / DATABASE_NAME
    assert files == [expected], '正式验收要求运行目录仅保留 data/smail.db'
    assert expected.is_file() and not expected.is_symlink(), '活动数据库不能是符号链接'
    return expected


def snapshot(source, target):
    with sqlite3.connect(f'file:{source}?mode=ro', uri=True) as original:
        with sqlite3.connect(target) as copied:
            original.backup(copied)
            assert copied.execute('PRAGMA integrity_check').fetchone()[0] == 'ok'
    target.chmod(0o600)


def wait_ready():
    for _ in range(60):
        result = subprocess.run([
            'curl', '--silent', '--fail', '--max-time', '2', '--noproxy', '*',
            '--cacert', str(ROOT / 'ssl/custom.crt'), '--resolve',
            'mail.solit.cn:443:127.0.0.1', 'https://mail.solit.cn/api/ping',
        ], stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
        if result.returncode == 0 and run('systemctl', 'is-active', 'smail.service') == 'active':
            return
        time.sleep(1)
    raise RuntimeError('升级后服务未就绪')


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--main-sha', required=True)
    parser.add_argument('--admin-sha', required=True)
    args = parser.parse_args()
    os.umask(0o077)
    assert os.geteuid() == 0 and ROOT.resolve() == ROOT and not ROOT.is_symlink()
    assert run('hostname') == 'ikun2'
    assert '172.31.134.42/20' in run('ip', '-4', 'address', 'show')
    assert run('systemctl', 'show', 'smail.service', '--property=User', '--value') == 'smail'
    assert run('systemctl', 'is-active', 'smail.service') == 'active'
    expected = {'smail': args.main_sha, 'smail-admin': args.admin_sha}
    for name, checksum in expected.items():
        path = STAGING / name
        with path.open('rb') as binary:
            header = binary.read(20)
        assert header[:5] == b'\x7fELF\x02' and header[18:20] == b'\x3e\x00'
        assert digest(path) == checksum
        assert not (ROOT / name).is_symlink()
    database = database_path(ROOT)

    backup = Path('/var/backups') / time.strftime('smail-before-outbox-%Y%m%d-%H%M%S')
    backup.mkdir(mode=0o700)
    installed = False
    subprocess.run(['systemctl', 'stop', 'smail.service'], check=True)
    try:
        # 停服归档包含配置、密钥和日志，仅保存在远程受保护目录。
        archive = backup / 'smail.tar.gz'
        subprocess.run(['tar', '-czf', str(archive), '-C', '/opt', 'smail'], check=True)
        subprocess.run(['tar', '-dzf', str(archive), '-C', '/opt'], check=True)
        archive.chmod(0o600)
        assert database.resolve() == database and not database.is_symlink()
        snapshot(database, backup / DATABASE_NAME)
        for name in expected:
            shutil.copy2(ROOT / name, backup / name)
        installed = True
        for name, checksum in expected.items():
            subprocess.run(['install', '-m', '0755', '-o', 'root', '-g', 'root',
                            str(STAGING / name), str(ROOT / name)], check=True)
            assert digest(ROOT / name) == checksum
        subprocess.run(['systemctl', 'start', 'smail.service'], check=True)
        wait_ready()
        database_path(ROOT)
        with sqlite3.connect(f'file:{database}?mode=ro', uri=True) as connection:
            assert connection.execute('PRAGMA integrity_check').fetchone()[0] == 'ok'
            version = connection.execute('SELECT MAX(version) FROM schema_migration').fetchone()[0]
            assert version == 23
            states = dict(connection.execute('SELECT state,COUNT(*) FROM outbound_delivery GROUP BY state'))
        result = {'backup': str(backup), 'archive_sha256': digest(archive),
                  'artifacts': expected, 'migration': version, 'queue_states': states,
                  'service': run('systemctl', 'is-active', 'smail.service'),
                  'admin_version': run(str(ROOT / 'smail-admin'), 'version'),
                  'database_integrity': 'ok', 'database_reset': False}
        (backup / 'result.json').write_text(json.dumps(result, ensure_ascii=False, indent=2), encoding='utf-8')
        print(json.dumps(result, ensure_ascii=False, indent=2))
    except BaseException:
        if installed:
            subprocess.run(['systemctl', 'stop', 'smail.service'], check=True)
            database = database_path(ROOT)
            snapshot(database, backup / ('failed-' + DATABASE_NAME))
            snapshot(backup / DATABASE_NAME, database)
            shutil.chown(database, user='smail', group='smail')
            for name in expected:
                shutil.copy2(backup / name, ROOT / name)
        subprocess.run(['systemctl', 'start', 'smail.service'], check=True)
        print('升级失败，备份及故障证据保留于：' + str(backup))
        raise


if __name__ == '__main__':
    main()
