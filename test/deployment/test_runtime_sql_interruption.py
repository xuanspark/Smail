"""SQL 中断验收的范围、真实事务判定与清理回归，不连接真实数据库。"""

import http.client
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import Mock, patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'tools'))
import runtime_sql_interruption as interruption
from runtime_sql import SQLFixture
import runtime_review as review


class RuntimeSQLInterruptionTest(unittest.TestCase):
    def test_same_engine_interruption_only(self):
        for kind in ('sqlite', 'mysql', 'postgres'):
            self.assertEqual(review.migration_kind(kind, None, True, True), kind)
        with self.assertRaises(ValueError):
            review.migration_kind('mysql', 'postgres', True, True)

    def test_formal_instance_rejected_before_target_creation(self):
        with patch.object(interruption, 'MigrationTarget') as target:
            with self.assertRaises(AssertionError):
                interruption.verify_interruption(Path('/opt/smail'), 'smail.service', 'unused', {}, SQLFixture('mysql'))
            target.assert_not_called()

    def test_seed_does_not_copy_primary_key_or_change_existing_rows(self):
        for kind in ('mysql', 'postgres'):
            source = SQLFixture(kind)
            with patch.object(source, 'query', side_effect=['id\nmsg_id\ntext\nsubject', '1', '', '1025']) as query:
                self.assertEqual(interruption.seed_source(source)['added_rows'], 1024)
            sql = query.call_args_list[2].args[0]
            self.assertTrue(sql.startswith('INSERT INTO email'))
            self.assertNotIn('UPDATE ', sql)
            self.assertNotIn('DELETE ', sql)
            self.assertNotIn('DROP ', sql)
            self.assertIn('REPEAT(\'x\',16384)', sql)
            self.assertIn('SELECT 1023 AS i', sql)
            self.assertNotIn('id', sql.split(' SELECT ')[0].replace('msg_id', ''))

    def test_seed_rejects_unsafe_column_before_write(self):
        source = SQLFixture('mysql')
        with patch.object(source, 'query', return_value='id\nmsg_id\ntext\nbad;DROP') as query:
            with self.assertRaises(AssertionError):
                interruption.seed_source(source)
            self.assertEqual(query.call_count, 1)

    def test_mysql_observation_filters_exact_account_and_database(self):
        target = SQLFixture('mysql')
        with patch.object(target, 'admin', return_value='150') as admin:
            self.assertEqual(interruption.copy_observation(target),
                             {'open_copy_transactions': 1, 'modified_rows': 150})
        sql = admin.call_args.args[0]
        for key in ('name', 'user'):
            self.assertIn(target.settings[key], sql)
        self.assertNotIn(target.settings['password'], sql)
        self.assertIn('trx_rows_modified>128', sql)
        with patch.object(target, 'admin', return_value=''):
            self.assertEqual(interruption.copy_observation(target)['open_copy_transactions'], 0)

    def test_postgres_requires_database_pages_xid_and_write_lock(self):
        target = SQLFixture('postgres')
        with patch.object(target, 'admin', return_value='0') as admin:
            self.assertEqual(interruption.copy_observation(target)['open_copy_transactions'], 0)
            self.assertEqual(admin.call_count, 1)
        for size, expected in (('0', 0), ('32768', 0), ('65536', 1)):
            with patch.object(target, 'admin', side_effect=['1', '1', size]) as admin:
                self.assertEqual(interruption.copy_observation(target)['open_copy_transactions'], expected)
            sql = admin.call_args_list[1].args[0]
            for value in (target.settings['name'], target.settings['user'], 'backend_xid', 'RowExclusiveLock'):
                self.assertIn(value, sql)
            self.assertEqual(admin.call_args_list[1].args[1], target.settings['name'])

    def test_committed_or_missing_or_ambiguous_copy_cannot_pass(self):
        target = SQLFixture('mysql')
        for count, rows in ((0, ['0', '0']), (2, ['0', '0']),
                            (1, ['1', '0']), (1, ['0', '1'])):
            with patch.object(target, 'query', side_effect=rows):
                with self.assertRaises(AssertionError):
                    interruption.assert_uncommitted(target, {'open_copy_transactions': count})
        with patch.object(target, 'query', side_effect=['0', '0']):
            self.assertEqual(interruption.assert_uncommitted(target, {'open_copy_transactions': 1})['committed_mail_rows'], 0)

    def test_normal_response_is_not_interruption(self):
        client = Mock()
        client.success.return_value = {'migration': {'id': 'one'}, 'token': 'test-token'}
        with patch.object(interruption, 'login', return_value=(client, None)), \
                patch.object(interruption, 'clear_pending_restart', return_value=1):
            for response in ((200, {}, {}), (500, {}, {})):
                client.request.return_value = response
                with self.assertRaises(AssertionError):
                    interruption.prepare(Path('/unused'), 'unused', SQLFixture('postgres'))
            client.request.side_effect = http.client.RemoteDisconnected()
            self.assertEqual(interruption.prepare(Path('/unused'), 'unused', SQLFixture('postgres')),
                             {'request_interrupted': True})

    def test_partial_create_failure_still_locks_account_and_preserves_result(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory) / 'instance'
            source, target = SQLFixture('postgres'), Mock()
            target.settings = SQLFixture('postgres').settings
            target.create.side_effect = RuntimeError('controlled failure')
            result = {}
            with patch.object(interruption, 'assert_scope'), patch.object(interruption, 'MigrationTarget', return_value=target):
                with self.assertRaises(RuntimeError):
                    interruption.verify_interruption(root, 'unused', 'unused', result, source)
            target.lock.assert_called_once_with()
            self.assertFalse(result['interruption']['passed'])
            self.assertTrue((root.parent / 'interruption-result.json').exists())


if __name__ == '__main__':
    unittest.main()
