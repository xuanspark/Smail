"""混合引擎验收器反例测试；本地临时库不连接云端。"""

import hashlib
from contextlib import closing
import json
from pathlib import Path
import shutil
import sqlite3
import stat
import sys
import tempfile
import unittest
from unittest.mock import Mock, PropertyMock, patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'tools'))
import runtime_mixed_migration as mixed
from runtime_sql import SQLFixture


class RuntimeMixedMigrationTest(unittest.TestCase):
    def setUp(self):
        self.directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.directory.cleanup)
        self.root = Path(self.directory.name).resolve()
        (self.root / 'data').mkdir()
        (self.root / 'config').mkdir()
        self.source = mixed.SQLiteDatabase(self.root, {'kind': 'sqlite', 'name': 'smail.db'})
        self.target = SQLFixture('mysql')
        with closing(sqlite3.connect(self.source.path)) as database:
            database.execute('CREATE TABLE config_records(kind TEXT,id TEXT,payload BLOB,digest TEXT)')
            payload = '中文\x00载荷'.encode()
            database.execute('INSERT INTO config_records VALUES (?,?,?,?)',
                             ('backup', 'item', payload, hashlib.sha256(payload).hexdigest()))
            database.commit()
        self.configure(self.target)

    def configure(self, target):
        (self.root / 'config/config.json').write_text(json.dumps({'database': mixed.target_payload(target)}))

    def test_only_fixed_local_names_are_allowed(self):
        for name in ('../smail.db', '/smail.db', 'nested/smail.db', 'state.db', ''):
            with self.assertRaises(AssertionError):
                mixed.SQLiteDatabase(self.root, {'kind': 'sqlite', 'name': name})
        with self.assertRaises(AssertionError):
            mixed.SQLiteDatabase(self.root, {'kind': 'sqlite', 'name': 'smail.db', 'password': 'extra'})

    def test_missing_database_is_not_created(self):
        missing = mixed.SQLiteDatabase(self.root, {'kind': 'sqlite', 'name': 'migrated.db'})
        with self.assertRaises(sqlite3.OperationalError):
            missing.query('SELECT 1')
        self.assertFalse(missing.path.exists())

    def test_read_only_query_cannot_change_database(self):
        with self.assertRaises(sqlite3.OperationalError):
            self.source.query('DELETE FROM config_records')
        self.assertEqual(self.source.query('SELECT COUNT(*) FROM config_records'), '1')

    def test_history_keeps_bytes_and_rejects_null_or_bad_digest(self):
        values = mixed.history_rows(self.source)
        self.assertEqual(bytes.fromhex(json.loads(values[0])[2]), '中文\x00载荷'.encode())
        for digest in (None, 'invalid'):
            with closing(sqlite3.connect(self.source.path)) as database:
                database.execute('UPDATE config_records SET digest=?', (digest,))
                database.commit()
            with self.assertRaises(AssertionError):
                mixed.history_rows(self.source)

    def test_expected_offline_source_and_wrong_configuration(self):
        mixed.assert_active(self.root, self.source, self.target)
        self.configure(self.source)
        with self.assertRaises(AssertionError):
            mixed.assert_active(self.root, self.source, self.target)

    def test_sql_to_sqlite_has_only_one_active_file(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory).resolve()
            (root / 'data').mkdir()
            (root / 'config').mkdir()
            target = mixed.SQLiteDatabase(root, {'kind': 'sqlite', 'name': 'migrated.db'})
            shutil.copyfile(self.source.path, target.path)
            (root / 'config/config.json').write_text(json.dumps({'database': mixed.target_payload(target)}))
            result = mixed.migration_integrity(root, self.target, target)
            self.assertEqual(result['active_database'], 'migrated.db')
            self.assertEqual(result['sqlite_files'], 1)
            self.assertEqual(result['retained_offline_files'], [])
            shutil.copyfile(target.path, root / 'data/smail.db')
            with self.assertRaises(AssertionError):
                mixed.migration_integrity(root, self.target, target)

    def test_hidden_nested_and_renamed_databases_rejected(self):
        for name in ('state.db', '.hidden.sqlite3', 'nested/smail.db', 'renamed.bin'):
            with self.subTest(name=name), tempfile.TemporaryDirectory() as directory:
                root = Path(directory).resolve()
                (root / 'data').mkdir()
                shutil.copyfile(self.source.path, root / 'data/smail.db')
                source = mixed.SQLiteDatabase(root, self.source.settings)
                mixed.assert_files(root, source, self.target)
                path = root / name
                path.parent.mkdir(parents=True, exist_ok=True)
                with closing(sqlite3.connect(path)) as database:
                    database.execute('CREATE TABLE marker(id INTEGER)')
                with self.assertRaises(AssertionError):
                    mixed.assert_files(root, source, self.target)

    def test_symlink_is_rejected_before_open(self):
        with patch.object(Path, 'is_symlink', return_value=True):
            with self.assertRaises(AssertionError):
                self.source.query('SELECT 1')
            with self.assertRaises(AssertionError):
                mixed.assert_files(self.root, self.source, self.target)

    def test_sql_target_requires_source_closed_and_target_connected(self):
        for names, count, accepted in ((set(), 3, True), ({'/opt/smail/data/smail.db'}, 3, False),
                                      ({'/opt/smail/data/smail.db-wal'}, 3, False),
                                      ({'/opt/smail/data/smail.db (deleted)'}, 3, False), (set(), 0, False)):
            with patch.object(mixed, 'descriptor_names', return_value=names), \
                    patch.object(mixed, 'connection_count', return_value=count):
                if accepted:
                    self.assertTrue(mixed.assert_connections(self.root, {'MainPID': '123'}, self.source, self.target)['source_inactive'])
                else:
                    with self.assertRaises(AssertionError):
                        mixed.assert_connections(self.root, {'MainPID': '123'}, self.source, self.target)

    def test_sql_source_requires_zero_connections_and_sqlite_open(self):
        with patch.object(mixed, 'assert_active'):
            for names, count, accepted in (({'/opt/smail/data/smail.db'}, 0, True),
                                          ({'/opt/smail/data/smail.db'}, 1, False), (set(), 0, False)):
                with patch.object(mixed, 'descriptor_names', return_value=names), \
                        patch.object(mixed, 'connection_count', return_value=count):
                    if accepted:
                        self.assertTrue(mixed.assert_connections(self.root, {'MainPID': '123'}, self.target, self.source)['target_active'])
                    else:
                        with self.assertRaises(AssertionError):
                            mixed.assert_connections(self.root, {'MainPID': '123'}, self.target, self.source)

    def test_sql_account_restored_even_when_lock_or_body_fails(self):
        for lock_failure in (False, True):
            target = Mock(kind='mysql')
            if lock_failure:
                target.lock.side_effect = RuntimeError('lock failure')
            with self.assertRaises(RuntimeError):
                with mixed.unavailable_target(target):
                    raise RuntimeError('probe failure')
            target.unlock.assert_called_once_with()

    def test_sqlite_permissions_restored_on_failure(self):
        with patch.object(Path, 'stat', return_value=Mock(st_mode=stat.S_IFREG | 0o600)), \
                patch.object(Path, 'is_file', return_value=True), \
                patch.object(mixed.SQLiteDatabase, 'path', new_callable=PropertyMock,
                             return_value=self.root / 'data/smail.db'), \
                patch.object(Path, 'chmod') as chmod:
            with self.assertRaises(RuntimeError):
                with mixed.unavailable_target(self.source):
                    raise RuntimeError('probe failure')
            self.assertEqual([call.args[0] for call in chmod.call_args_list], [0, 0o600])

    def test_formal_root_rejected_before_service_calls(self):
        with patch.object(mixed, 'service_state') as state:
            with self.assertRaises(AssertionError):
                mixed.verify_migration(Path('/opt/smail'), 'smail.service', '', {}, self.source, self.target)
            state.assert_not_called()


if __name__ == '__main__':
    unittest.main()
