"""SQL 实机验收器的范围限制、敏感信息和命名空间恢复回归。"""

from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import MagicMock, call, patch


sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'tools'))
import runtime_sql
import runtime_sql_bridge


def settings(kind='mysql'):
    return {'kind': kind, 'name': 'smail_runtime_test_123456abcdef',
            'user': 'smail_rt_123456abcdef', 'password': 'a' * 40}


class RuntimeSQLTest(unittest.TestCase):
    def test_rejects_other_projects_before_any_command(self):
        for key, value in [('name', 'smail'), ('name', 'production'),
                           ('name', "smail_runtime_test_123456abcdef'; DROP TABLE user"),
                           ('user', 'root'), ('kind', 'sqlite'), ('password', "a';SELECT 1")]:
            with self.subTest(key=key), patch.object(runtime_sql, 'command') as command:
                candidate = settings()
                candidate[key] = value
                with self.assertRaises(AssertionError):
                    runtime_sql.SQLDatabase(candidate)
                command.assert_not_called()

    def test_password_is_not_in_command_arguments(self):
        for kind, key in [('mysql', 'MYSQL_PWD'), ('postgres', 'PGPASSWORD')]:
            with self.subTest(kind=kind), patch.object(runtime_sql, 'command', return_value='0') as command:
                database = runtime_sql.SQLDatabase(settings(kind))
                database.assert_empty_users()
                args, sql, env = command.call_args.args
                self.assertNotIn(settings()['password'], ' '.join(args))
                self.assertEqual(env[key], settings()['password'])
                self.assertIn('COUNT(*)', sql)

    def test_driver_error_details_are_not_exposed(self):
        failure = subprocess.CompletedProcess([], 1, '', 'sensitive-test-value')
        with patch.object(runtime_sql.subprocess, 'run', return_value=failure):
            with self.assertRaises(RuntimeError) as caught:
                runtime_sql.command(['mysql'], 'SELECT 1')
            self.assertNotIn('sensitive-test-value', str(caught.exception))

    def test_fixture_grants_only_exact_database_and_locks_account(self):
        for kind in ('mysql', 'postgres'):
            with self.subTest(kind=kind):
                fixture = runtime_sql.SQLFixture(kind)
                with patch.object(fixture, 'admin', return_value='0') as admin:
                    fixture.create()
                    fixture.lock()
                statements = '\n'.join(item.args[0] for item in admin.call_args_list)
                self.assertNotIn('DROP ', statements)
                self.assertNotIn('*.*', statements)
                self.assertNotIn('CREATEDB', statements)
                if kind == 'mysql':
                    self.assertIn(fixture.settings['name'].replace('_', r'\_'), statements)
                    self.assertIn('ACCOUNT LOCK', statements)
                else:
                    self.assertIn('NOLOGIN', statements)

    def test_uncreated_account_does_not_trigger_admin_command(self):
        fixture = runtime_sql.SQLFixture('mysql')
        with patch.object(fixture, 'admin') as admin:
            fixture.lock()
            admin.assert_not_called()

    def test_nonempty_database_fails_first_install_check(self):
        fixture = runtime_sql.SQLFixture('postgres')
        with patch.object(fixture, 'admin', return_value='3'):
            with self.assertRaises(AssertionError):
                fixture.create()

    def test_sql_mode_rejects_hidden_second_sqlite_database(self):
        database = runtime_sql.SQLDatabase(settings())
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'data/nested').mkdir(parents=True)
            (root / 'data/nested/smail-state.db').touch()
            with patch.object(database, 'query') as query:
                with self.assertRaises(AssertionError):
                    database.integrity(root)
                query.assert_not_called()

    def test_sql_mode_requires_configuration_history_in_same_database(self):
        database = runtime_sql.SQLDatabase(settings())
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'data').mkdir()
            with patch.object(database, 'query', return_value='0'):
                with self.assertRaises(AssertionError):
                    database.integrity(root)
            with patch.object(database, 'query', return_value='5'):
                self.assertEqual(database.integrity(root)['sqlite_files'], 0)

    def test_bridge_rejects_non_database_port_before_opening_socket(self):
        with patch.object(runtime_sql_bridge.socket, 'socket') as socket:
            with self.assertRaises(AssertionError):
                runtime_sql_bridge.DatabaseBridge(1234, 22)
            socket.assert_not_called()

    def test_bridge_restores_host_namespace_when_bind_fails(self):
        handles = []
        for descriptor in (11, 12):
            handle = MagicMock()
            handle.__enter__.return_value = handle
            handle.fileno.return_value = descriptor
            handles.append(handle)
        with patch.object(runtime_sql_bridge.Path, 'readlink', side_effect=[Path('net2'), Path('net1')]), \
                patch('builtins.open', side_effect=handles), \
                patch.object(runtime_sql_bridge.os, 'setns', create=True) as setns, \
                patch.object(runtime_sql_bridge.socketserver.ThreadingTCPServer, '__init__', side_effect=OSError()), \
                patch.object(runtime_sql_bridge.threading, 'Thread') as thread:
            with self.assertRaises(OSError):
                runtime_sql_bridge.DatabaseBridge(1234, 3306)
            self.assertEqual(setns.call_args_list, [call(12, 0), call(11, 0)])
            thread.assert_not_called()


if __name__ == '__main__':
    unittest.main()
