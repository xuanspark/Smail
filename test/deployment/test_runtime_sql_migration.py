"""SQL 迁移验收器权限、摘要和边界测试，不连接真实数据库。"""

import hashlib
import json
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import Mock, patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'tools'))
import runtime_sql_migration as migration
import runtime_review as review


class RuntimeSQLMigrationTest(unittest.TestCase):
    def test_cross_engine_requires_explicit_sql_migration(self):
        for source, target in (('mysql', 'postgres'), ('postgres', 'mysql')):
            self.assertEqual(review.migration_kind(source, target, True, False), target)
        self.assertEqual(review.migration_kind('sqlite', None, True, False), 'sqlite')
        for arguments in (('sqlite', 'mysql', True, False), ('mysql', 'postgres', False, False),
                          ('mysql', 'postgres', True, True), ('mysql', 'sqlite', True, False)):
            with self.assertRaises(ValueError):
                review.migration_kind(*arguments)

    def test_all_database_bridges_closed_even_when_one_fails(self):
        first, second = Mock(), Mock()
        second.close.side_effect = RuntimeError('bridge failure')
        result = {}
        review.stop_database_bridges({3306: first, 5432: second}, result)
        first.close.assert_called_once_with()
        second.close.assert_called_once_with()
        self.assertFalse(result['database_bridge_stopped'])
        self.assertEqual(result['database_bridge_ports_stopped'], [3306])
        self.assertEqual(result['cleanup_errors'], [{'action': 'database_bridge', 'port': 5432, 'error': 'RuntimeError'}])

    def test_both_database_bridges_reported_stopped(self):
        result = {}
        review.stop_database_bridges({3306: Mock(), 5432: Mock()}, result)
        self.assertTrue(result['database_bridge_stopped'])
        self.assertEqual(result['database_bridge_ports_stopped'], [3306, 5432])
        self.assertFalse(result['cleanup_errors'])

    def test_history_compares_complete_raw_bytes_across_engines(self):
        payload = b'{"text":"\\u4e2d\\u6587","zero":"\\u0000"}'
        digest = hashlib.sha256(payload).hexdigest().encode()
        row = [value.hex() for value in (b'backup', b'identifier', payload, digest)]
        records = []
        for kind in ('mysql', 'postgres'):
            database = migration.SQLFixture(kind)
            with patch.object(database, 'query', return_value=json.dumps(row)) as query:
                records.append(migration.history_rows(database))
            for column in ('kind', 'id', 'payload', 'digest'):
                self.assertIn(column, query.call_args.args[0])
        self.assertEqual(records[0], records[1])

    def test_invalid_history_fails_instead_of_skipping(self):
        database = migration.SQLFixture('mysql')
        valid = [value.hex() for value in (b'backup', b'id', b'{}', hashlib.sha256(b'{}').hexdigest().encode())]
        invalid = [[], [None] * 4, ['xyz'] * 4, valid[:2] + [b'corrupt'.hex(), valid[3]]]
        for row in invalid:
            with patch.object(database, 'query', return_value=json.dumps(row)):
                with self.assertRaises(AssertionError):
                    migration.history_rows(database)
        with patch.object(database, 'query', return_value=json.dumps(valid) + '\n' + json.dumps(valid)):
            with self.assertRaises(AssertionError):
                migration.history_rows(database)

    def test_target_creation_does_not_precreate_database(self):
        for kind in ('mysql', 'postgres'):
            target = migration.MigrationTarget(kind)
            with patch.object(target, 'admin', return_value='0') as admin:
                target.create()
            statements = '\n'.join(call.args[0] for call in admin.call_args_list)
            self.assertNotIn('CREATE DATABASE ', statements)
            self.assertNotIn('DROP ', statements)
            self.assertNotIn('*.*', statements)
            self.assertNotIn('SUPERUSER', statements)
            if kind == 'mysql':
                self.assertIn(target.settings['name'].replace('_', r'\_'), statements)
            else:
                self.assertIn(' LOGIN CREATEDB CONNECTION LIMIT 24', statements)

    def test_existing_target_is_rejected(self):
        target = migration.MigrationTarget('mysql')
        with patch.object(target, 'admin', return_value='1'):
            with self.assertRaises(AssertionError):
                target.create()

    def test_cleanup_revokes_createdb_and_locks_login(self):
        target = migration.MigrationTarget('postgres')
        target.account_created = True
        with patch.object(target, 'admin', side_effect=['', 'f', '', 'f']) as admin:
            target.lock()
        statements = '\n'.join(call.args[0] for call in admin.call_args_list)
        self.assertIn(' NOCREATEDB;', statements)
        self.assertIn(' NOLOGIN;', statements)

    def test_revoke_failure_still_attempts_login_lock(self):
        target = migration.MigrationTarget('postgres')
        target.account_created = True
        with patch.object(target, 'admin', side_effect=[RuntimeError('denied'), '', 'f']) as admin:
            with self.assertRaises(RuntimeError):
                target.lock()
        self.assertIn(' NOLOGIN;', admin.call_args_list[1].args[0])

    def test_unlock_requires_confirmation(self):
        for kind in ('mysql', 'postgres'):
            target = migration.MigrationTarget(kind)
            target.account_created = True
            with patch.object(target, 'admin', return_value=''):
                with self.assertRaises(AssertionError):
                    target.unlock()

    def test_uncreated_account_is_not_changed(self):
        target = migration.MigrationTarget('postgres')
        with patch.object(target, 'admin') as admin:
            target.lock()
            admin.assert_not_called()
            with self.assertRaises(AssertionError):
                target.unlock()

    def test_target_uses_structured_secret(self):
        target = migration.MigrationTarget('postgres')
        payload = migration.target_payload(target)
        self.assertEqual(payload['type'], 'postgresql')
        self.assertEqual(payload['password'], {'plaintext': target.settings['password']})
        self.assertNotIn('dsn', payload)

    def test_untrusted_identifiers_rejected_before_query(self):
        target = migration.MigrationTarget('mysql')
        with patch.object(target, 'query') as query:
            for value in ('user; DROP TABLE email', 'email`', 'a.b', 'x y'):
                with self.assertRaises(AssertionError):
                    migration.table_rows(target, value)
            query.assert_not_called()

    def test_mysql_digest_encoding_keeps_binary_and_null(self):
        target = migration.MigrationTarget('mysql')
        with patch.object(target, 'query', side_effect=['payload\ndigest', '[null,"01"]\n["00FF","02"]']) as query:
            rows = migration.table_rows(target, 'config_records')
        self.assertEqual(len(rows), 2)
        self.assertIn('JSON_ARRAY(HEX(`payload`),HEX(`digest`))', query.call_args.args[0])
        self.assertIn('[null, "01"]', rows)

    def test_fingerprint_stable_when_row_order_changes(self):
        target = migration.MigrationTarget('postgres')
        def read(sql):
            if 'information_schema.tables' in sql:
                return 'config_records\nemail'
            return '{"a":2}\n{"a":1}'
        with patch.object(target, 'query', side_effect=read):
            first = migration.fingerprint(target)
        with patch.object(target, 'query', side_effect=lambda sql: read(sql) if 'information_schema' in sql else '{"a":1}\n{"a":2}'):
            self.assertEqual(first, migration.fingerprint(target))
        with patch.object(target, 'query', side_effect=lambda sql: read(sql) if 'information_schema' in sql else '{"a":3}'):
            self.assertNotEqual(first, migration.fingerprint(target))

    def test_connections_require_source_zero_and_target_positive(self):
        source, target = migration.SQLFixture('mysql'), migration.MigrationTarget('mysql')
        with patch.object(source, 'admin', return_value='0'), patch.object(target, 'admin', return_value='2'):
            self.assertEqual(migration.assert_connections(source, target)['source_connections'], 0)
        with patch.object(source, 'admin', return_value='1'), patch.object(target, 'admin', return_value='2'):
            with self.assertRaises(AssertionError):
                migration.assert_connections(source, target)
        with patch.object(source, 'admin', return_value='0'), patch.object(target, 'admin', return_value='0'):
            with self.assertRaises(AssertionError):
                migration.assert_connections(source, target)

    def test_active_configuration_and_no_hidden_sqlite(self):
        target = migration.MigrationTarget('mysql')
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'config').mkdir()
            (root / 'data').mkdir()
            (root / 'config/config.json').write_text(json.dumps({'database': migration.target_payload(target)}))
            migration.assert_active(root, target)
            (root / 'data/state.sqlite3').touch()
            with self.assertRaises(AssertionError):
                migration.assert_active(root, target)

    def test_formal_directory_rejected_before_any_service_operation(self):
        with patch.object(migration, 'service_state') as state:
            with self.assertRaises(AssertionError):
                migration.verify_migration(Path('/opt/smail'), 'smail.service', 'not-a-secret', {}, Mock(), Mock())
            state.assert_not_called()


if __name__ == '__main__':
    unittest.main()
