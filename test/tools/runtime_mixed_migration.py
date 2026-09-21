"""SQLite 与服务器数据库之间的隔离应用级验收，不操作正式库。"""

import argparse
import base64
from contextlib import contextmanager
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import sys
import time

from runtime_migration import database_fingerprint, read_database, run_probe, service_state
from runtime_sql import SQLDatabase
from runtime_sql_migration import connection_count, fingerprint, history_rows, prepare, target_payload, verify_mail


class SQLiteDatabase:
    def __init__(self, root, settings):
        assert set(settings) == {'kind', 'name'} and settings['kind'] == 'sqlite'
        assert settings['name'] in ('smail.db', 'migrated.db')
        self.root = root
        self.settings = settings.copy()
        self.kind = 'sqlite'

    @property
    def path(self):
        path = self.root / 'data' / self.settings['name']
        assert self.root.is_absolute() and self.root.resolve(strict=True) == self.root
        assert not self.root.is_symlink() and not path.parent.is_symlink() and not path.is_symlink()
        assert path.resolve() == path
        return path

    def query(self, sql):
        with read_database(self.path) as database:
            rows = database.execute(sql).fetchall()
        assert all(len(row) == 1 and row[0] is not None for row in rows)
        return '\n'.join(str(row[0]) for row in rows)

    def verify_message(self, message_id, account, body, attachment):
        with read_database(self.path) as database:
            rows = database.execute('SELECT id,text,attachments FROM email WHERE msg_id=?', (message_id,)).fetchall()
            assert len(rows) == 1
            key, text, attachments = rows[0]
            assert body in text
            values = json.loads(attachments)
            assert len(values) == 1 and values[0]['Filename'] == '验收附件.txt'
            assert base64.b64decode(values[0]['Content'], validate=True) == attachment
            assert database.execute('SELECT u.account FROM user_email e JOIN "user" u ON u.id=e.user_id '
                                    'WHERE e.email_id=?', (key,)).fetchall() == [(account,)]


def assert_files(root, source, target):
    expected = {root / 'data' / database.settings['name'] for database in (source, target)
                if database.kind == 'sqlite'}
    seen = set()
    # 不只按扩展名找库：同时识别 SQLite 文件头，拒绝嵌套、改名和符号链接。
    for path in root.rglob('*'):
        assert not path.is_symlink(), '隔离实例不能通过符号链接隐藏第二个库'
        if not path.is_file():
            continue
        with path.open('rb') as stream:
            header = stream.read(16)
        if header == b'SQLite format 3\x00' or path.suffix == '.db' or '.sqlite' in path.name:
            assert path in expected, '发现非本轮来源或目标的 SQLite 文件'
            seen.add(path)
        if path.name.endswith(('-wal', '-shm', '-journal')):
            assert path.with_name(path.name.rsplit('-', 1)[0]) in expected
    assert seen == expected, '本轮 SQLite 证据文件缺失'
    return sorted(path.name for path in seen)


def assert_configuration(root, target):
    database = json.loads((root / 'config/config.json').read_text())['database']
    expected = target_payload(target)
    for field in ('type', 'localRef') if target.kind == 'sqlite' else ('type', 'host', 'port', 'username', 'name'):
        assert database.get(field) == expected[field], '活动配置没有指向本轮唯一目标'


def assert_active(root, source, target):
    assert_configuration(root, target)
    assert_files(root, source, target)


def descriptor_names(pid):
    assert pid.isdigit() and int(pid) > 0
    names = set()
    for descriptor in Path('/proc', pid, 'fd').iterdir():
        try:
            names.add(str(descriptor.readlink()))
        except FileNotFoundError:
            pass
    return names


def assert_connections(root, state, source, target):
    names = descriptor_names(state['MainPID'])
    database = target if target.kind != 'sqlite' else source
    count = connection_count(database)
    sqlite = source if source.kind == 'sqlite' else target
    prefix = '/opt/smail/data/' + sqlite.settings['name']
    opened = any(name == prefix or name.startswith(prefix + '-') or name.startswith(prefix + ' (deleted)')
                 for name in names)
    if source.kind == 'sqlite':
        assert not opened and count > 0, 'SQLite 源仍被打开或 SQL 目标无连接'
    else:
        assert prefix in names and count == 0, 'SQLite 目标未打开或 SQL 源仍有连接'
    assert_active(root, source, target)
    return {'sql_connections': count, 'sqlite_open': opened, 'source_inactive': True, 'target_active': True}


def source_fingerprint(database):
    return database_fingerprint(database.path) if database.kind == 'sqlite' else fingerprint(database)


@contextmanager
def unavailable_target(target):
    if target.kind == 'sqlite':
        path = target.path
        assert path.is_file() and stat.S_IMODE(path.stat().st_mode) == 0o600
        try:
            path.chmod(0)
            yield
        finally:
            path.chmod(0o600)
    else:
        try:
            target.lock()
            yield
        finally:
            target.unlock()


def migration_integrity(root, source, target):
    assert_active(root, source, target)
    sqlite = source if source.kind == 'sqlite' else target
    with read_database(sqlite.path) as database:
        assert database.execute('PRAGMA integrity_check').fetchone() == ('ok',)
    count = int(target.query('SELECT COUNT(*) FROM config_records'))
    assert count > 0
    return {'type': target.kind, 'active_database': target.settings['name'], 'config_records': count,
            'sqlite_files': 1, 'retained_offline_files': [source.settings['name']] if source.kind == 'sqlite' else [],
            'integrity': 'ok'}


def verify_migration(root, unit, password, result, source, target):
    assert root.parent.parent == Path('/var/tmp') and re.fullmatch(r'smail-review-runtime-\d{8}-\d{6}', root.parent.name)
    assert root.name == 'instance' and not root.is_symlink() and root.resolve(strict=True) == root
    assert source.kind != target.kind and 'sqlite' in (source.kind, target.kind)
    assert unit == 'smail-runtime-review-' + root.parent.name.removeprefix('smail-review-runtime-')
    details = result['migration'] = {'passed': False, 'database_type': source.kind, 'target_type': target.kind,
                                     'cross_engine': True, 'source_database': source.settings['name'],
                                     'target_database': target.settings['name']}
    settings = {'source': source.settings, 'target': target.settings}

    def probe(phase):
        return run_probe(root, unit, password, phase, settings, 'runtime_mixed_migration.py')

    before = service_state(unit)
    details['prepare'] = probe('prepare')
    if target.kind != 'sqlite':
        target.revoke_creation()
        details['temporary_creation_privilege_revoked'] = True
    internal = service_state(unit)
    assert before['MainPID'] == internal['MainPID'] and before['InvocationID'] == internal['InvocationID']
    details['internal_restart_same_process'] = True
    details['connections_after_internal'] = assert_connections(root, internal, source, target)
    digest = source_fingerprint(source)
    history = history_rows(source)
    assert history
    details['internal'] = probe('internal')
    subprocess.run(['systemctl', 'restart', unit], check=True, timeout=55)
    restarted = service_state(unit)
    assert restarted['MainPID'] != internal['MainPID'] and restarted['InvocationID'] != internal['InvocationID']
    details['process'] = probe('process')
    details['connections_after_process'] = assert_connections(root, restarted, source, target)
    details['process_states'] = {'before': before, 'internal': internal, 'restarted': restarted}
    with unavailable_target(target):
        subprocess.run(['systemctl', 'restart', unit], check=True, timeout=55)
        for _ in range(70):
            failed = service_state(unit)
            if failed['MainPID'] == '0' and failed['ActiveState'] == 'failed':
                break
            time.sleep(1)
        else:
            raise AssertionError('目标不可用时应用没有明确停止，不能接受静默回退')
        assert_configuration(root, target)
        assert source_fingerprint(source) == digest
        details['unavailable_target'] = {'failed_closed': True, 'source_unchanged': True,
                                         'configuration_preserved': True, 'state': failed}
    subprocess.run(['systemctl', 'start', unit], check=True, timeout=55)
    details['recovered'] = probe('recovered')
    final = service_state(unit)
    assert final['MainPID'] not in (before['MainPID'], restarted['MainPID'])
    details['connections_after_recovery'] = assert_connections(root, final, source, target)
    assert source_fingerprint(source) == digest
    current = history_rows(target)
    assert set(history).issubset(current)
    details['history'] = {'source_records': len(history), 'target_records': len(current), 'all_source_records_equal': True}
    details.update(source_unchanged=True, target_access_restored=True, final_state=final, passed=True)
    (root.parent / 'migration-result.json').write_text(json.dumps(details, ensure_ascii=False, indent=2), encoding='utf-8')


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('root', type=Path)
    parser.add_argument('phase', choices=('prepare', 'internal', 'process', 'recovered'))
    args = parser.parse_args()
    assert os.geteuid() != 0
    password = sys.stdin.readline().strip()
    assert len(password) == 12
    settings = json.loads(sys.stdin.readline())
    assert set(settings) == {'source', 'target'}
    source, target = (SQLiteDatabase(args.root, settings[key]) if settings[key]['kind'] == 'sqlite'
                      else SQLDatabase(settings[key]) for key in ('source', 'target'))
    assert source.kind != target.kind and 'sqlite' in (source.kind, target.kind)

    def active_check(root, database):
        assert_active(root, source, database)

    result = (prepare(args.root, password, source, target, active_check) if args.phase == 'prepare' else
              verify_mail(args.root, password, args.phase, source, target, active_check))
    result['passed'] = True
    (args.root.parent / ('migration-' + args.phase + '.json')).write_text(
        json.dumps(result, ensure_ascii=False, indent=2), encoding='utf-8')
    print(json.dumps(result, ensure_ascii=False, indent=2))


if __name__ == '__main__':
    main()
