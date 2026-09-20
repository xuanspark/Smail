"""迁移验收工具的只读、范围和进程证据边界测试，不访问云服务器。"""

from contextlib import closing
import json
from pathlib import Path
import sqlite3
import sys
import tempfile
import unittest
from unittest.mock import Mock, patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'tools'))
import runtime_migration as migration


class RuntimeMigrationTest(unittest.TestCase):
    def test_pending_restart_is_completed_before_migration(self):
        client = Mock()
        client.success.side_effect = [
            {'activeGeneration': 5, 'pendingRestart': ['database']}, {'status': 'accepted'},
        ]
        with patch.object(migration, 'wait_ready') as wait:
            self.assertEqual(migration.clear_pending_restart(client), 5)
        wait.assert_called_once_with(client, 5)
        self.assertEqual(client.success.call_args_list[1].args, ('/api/v1/system/restart',))

    def test_no_pending_change_does_not_restart(self):
        client = Mock()
        client.success.return_value = {'activeGeneration': 5, 'pendingRestart': []}
        with patch.object(migration, 'wait_ready') as wait:
            self.assertEqual(migration.clear_pending_restart(client), 5)
        wait.assert_not_called()
        self.assertEqual(client.success.call_count, 1)

    def test_missing_database_is_not_created(self):
        with tempfile.TemporaryDirectory() as directory:
            missing = Path(directory) / 'missing.db'
            with self.assertRaises(sqlite3.OperationalError):
                migration.database_fingerprint(missing)
            self.assertFalse(missing.exists())

    def test_fingerprint_detects_changes(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'source.db'
            with closing(sqlite3.connect(path)) as database:
                database.execute('CREATE TABLE item (value TEXT)')
                database.commit()
            first = migration.database_fingerprint(path)
            self.assertEqual(first, migration.database_fingerprint(path))
            with closing(sqlite3.connect(path)) as database:
                database.execute("INSERT INTO item VALUES ('changed')")
                database.commit()
            self.assertNotEqual(first, migration.database_fingerprint(path))

    def test_history_requires_payload_and_digest_equality(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'data').mkdir()
            for name in ('smail.db', 'migrated.db'):
                with closing(sqlite3.connect(root / 'data' / name)) as database:
                    database.execute('CREATE TABLE config_records (kind TEXT, id TEXT, payload BLOB, digest TEXT)')
                    database.execute('INSERT INTO config_records VALUES (?, ?, ?, ?)', ('backup', 'one', b'payload', 'digest'))
                    database.commit()
            self.assertEqual(migration.verify_history_preserved(root)['source_records'], 1)
            with closing(sqlite3.connect(root / 'data/migrated.db')) as database:
                database.execute("UPDATE config_records SET payload = 'changed'")
                database.commit()
            with self.assertRaises(AssertionError):
                migration.verify_history_preserved(root)

    def test_integrity_distinguishes_active_and_offline_files(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'config').mkdir()
            (root / 'data').mkdir()
            (root / 'config/config.json').write_text(json.dumps({
                'database': {'type': 'sqlite', 'localRef': 'migrated.db'},
            }))
            with closing(sqlite3.connect(root / 'data/migrated.db')) as database:
                database.execute('CREATE TABLE config_records (id INTEGER)')
                database.execute('INSERT INTO config_records VALUES (1)')
                database.commit()
            (root / 'data/smail.db').touch()
            (root / 'data/occupied.db').touch()
            result = migration.migration_integrity(root)
            self.assertEqual(result['active_database'], 'migrated.db')
            self.assertEqual(result['config_records'], 1)
            (root / 'data/smail-state.db').touch()
            with self.assertRaises(AssertionError):
                migration.migration_integrity(root)

    def test_system_service_is_rejected_before_command(self):
        with patch.object(migration.subprocess, 'check_output') as command:
            for name in ('smail.service', 'mysql.service', 'smail-runtime-review-anything'):
                with self.assertRaises(AssertionError):
                    migration.service_state(name)
            command.assert_not_called()

    def test_formal_root_rejected_before_command(self):
        with patch.object(migration.subprocess, 'check_output') as command:
            with self.assertRaises(AssertionError):
                migration.verify_migration(Path('/opt/smail'), 'smail.service', 'not-a-secret', {})
            command.assert_not_called()

    def test_process_proof_rejects_zero_pid(self):
        with self.assertRaises(AssertionError):
            migration.process_uses_target(Path('/unused'), {'MainPID': '0'})


if __name__ == '__main__':
    unittest.main()
