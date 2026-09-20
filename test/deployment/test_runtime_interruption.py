"""中断验收的范围、种子数据和误报防护测试，不终止任何真实服务。"""

from contextlib import closing
import http.client
from pathlib import Path
import sqlite3
import sys
import tempfile
import unittest
from unittest.mock import Mock, patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'tools'))
import runtime_interruption as interruption


class RuntimeInterruptionTest(unittest.TestCase):
    def test_occupied_target_requires_structured_error_contract(self):
        payload = {'errorNo': 409, 'data': {'code': 'DATABASE_MIGRATION_TARGET_EXISTS'}}
        self.assertEqual(interruption.assert_occupied_target(409, payload)['status'], 409)
        for status, invalid in ((200, payload), (409, None),
                                (409, {'errorNo': 409, 'data': 'DATABASE_MIGRATION_TARGET_EXISTS'}),
                                (409, {'errorNo': 409, 'data': {'code': 'OTHER'}}),
                                (409, {'errorNo': 0, 'data': payload['data']})):
            with self.assertRaises(AssertionError):
                interruption.assert_occupied_target(status, invalid)

    def test_formal_instance_rejected_before_service_command(self):
        with patch.object(interruption, 'service_state') as state, patch.object(interruption.subprocess, 'run') as run:
            with self.assertRaises(AssertionError):
                interruption.verify_interruption(Path('/opt/smail'), 'smail.service', 'not-a-secret', {})
            state.assert_not_called()
            run.assert_not_called()

    def test_seed_missing_database_is_not_created(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'data').mkdir()
            with self.assertRaises(FileNotFoundError):
                interruption.seed_source(root)
            self.assertFalse((root / 'data/smail.db').exists())

    def test_seed_preserves_existing_mail_and_schema(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'data').mkdir()
            path = root / 'data/smail.db'
            with closing(sqlite3.connect(path)) as database:
                database.execute('CREATE TABLE email (id INTEGER PRIMARY KEY, msg_id TEXT UNIQUE, text TEXT)')
                database.execute("INSERT INTO email VALUES (1, 'original', 'body')")
                database.commit()
            result = interruption.seed_source(root)
            self.assertEqual(result['added_rows'], 1024)
            with closing(sqlite3.connect(path)) as database:
                self.assertEqual(database.execute('SELECT COUNT(*) FROM email').fetchone()[0], 1025)
                self.assertEqual(database.execute('SELECT * FROM email WHERE id=1').fetchone(), (1, 'original', 'body'))
                self.assertEqual(database.execute('SELECT COUNT(*) FROM sqlite_master WHERE type=\'table\'').fetchone()[0], 1)

    def test_completed_request_cannot_count_as_interruption(self):
        client = Mock()
        client.success.return_value = {'migration': {'id': 'one'}, 'token': 'not-a-secret'}
        for response in ((200, {}, {}), (500, {}, {})):
            client.request.return_value = response
            with patch.object(interruption, 'login', return_value=(client, None)), \
                    patch.object(interruption, 'clear_pending_restart', return_value=1):
                with self.assertRaises(AssertionError):
                    interruption.prepare(Path('/unused'), 'not-a-secret')

    def test_disconnected_request_requires_controller_process_evidence(self):
        client = Mock()
        client.success.return_value = {'migration': {'id': 'one'}, 'token': 'not-a-secret'}
        client.request.side_effect = http.client.RemoteDisconnected()
        with patch.object(interruption, 'login', return_value=(client, None)), \
                patch.object(interruption, 'clear_pending_restart', return_value=1):
            self.assertEqual(interruption.prepare(Path('/unused'), 'not-a-secret'), {'request_interrupted': True})


if __name__ == '__main__':
    unittest.main()
