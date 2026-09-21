"""联合故障工具的范围、阶段与失败判定回归，不连接真实服务。"""

import http.client
import json
from pathlib import Path
import sys
import tempfile
import unittest
from unittest.mock import Mock, patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'tools'))
import runtime_sql_switch_fault as fault
from runtime_sql import SQLFixture


class RuntimeSQLSwitchFaultTest(unittest.TestCase):
    def test_every_thread_must_be_stopped(self):
        task = Mock()
        for state, expected in (('S', False), ('R', False), ('T', True), ('t', True)):
            task.__truediv__ = Mock(return_value=Mock(read_text=Mock(return_value='State:\t' + state + ' (state)\n')))
            with patch.object(Path, 'iterdir', return_value=iter([task])):
                self.assertEqual(fault.stopped_tasks('123'), expected)

    def test_formal_scope_rejected_before_process_or_database_access(self):
        with patch.object(fault.subprocess, 'Popen') as process, patch.object(fault, 'service_state') as state:
            with self.assertRaises(AssertionError):
                fault.verify_switch_fault(Path('/opt/smail'), 'smail.service', 'unused', {},
                                          SQLFixture('mysql'), SQLFixture('mysql'), 'before-root')
            process.assert_not_called()
            state.assert_not_called()

    def test_boundary_requires_active_directory_phase_and_expected_database(self):
        source, target = SQLFixture('mysql'), SQLFixture('mysql')
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            archive = root / 'data/transaction/one'
            archive.mkdir(parents=True)
            (root / 'config').mkdir()
            trace = root / 'trace.log'
            manifest = archive / 'manifest.json'
            for phase, name in (('before-root', source.settings['name']), ('after-root', target.settings['name'])):
                (root / 'config/config.json').write_text(json.dumps({'database': {'name': name}}))
                manifest.write_text(json.dumps({'phase': 'sections_committed'}))
                for path, expected in (('/opt/smail/data/transaction/one/candidate', False),
                                       ('/opt/smail/config', True), ('/opt/smail/config-other', False)):
                    trace.write_text('renameat(4<' + path + '>, ".config.json-a", 4<' + path + '>, "config.json"')
                    self.assertEqual(bool(fault.boundary(root, trace, phase, source, target)), expected)
                trace.write_text('renameat(4</opt/smail/config>, ".config.json-a", 4</opt/smail/config>, "config.json"')
                manifest.write_text(json.dumps({'phase': 'root_committed'}))
                self.assertIsNone(fault.boundary(root, trace, phase, source, target))

    def test_committed_requires_matching_nonempty_mail_and_preserved_history(self):
        source, target = Mock(), Mock()
        for mail, history in (([[], []], [['a'], ['a']]),
                              ([['a'], ['b']], [['a'], ['a']]),
                              ([['a'], ['a']], [[], []]),
                              ([['a'], ['a']], [['a'], ['b']])):
            with patch.object(fault, 'table_rows', side_effect=mail), \
                    patch.object(fault, 'history_rows', side_effect=history):
                with self.assertRaises(AssertionError):
                    fault.assert_committed(source, target)
        with patch.object(fault, 'table_rows', side_effect=[['mail'], ['mail']]), \
                patch.object(fault, 'history_rows', side_effect=[['old'], ['old', 'new']]):
            self.assertEqual(fault.assert_committed(source, target)['committed_history_rows'], 2)

    def test_timeout_and_regular_response_cannot_pass_as_killed_process(self):
        for outcome in (None, TimeoutError(), http.client.RemoteDisconnected(), ConnectionResetError()):
            with tempfile.TemporaryDirectory() as directory:
                root = Path(directory) / 'instance'
                client = Mock()
                client.success.return_value = {'migration': {'id': 'test-id'}, 'token': 'not-for-logs'}
                client.request.side_effect = outcome
                with patch.object(fault, 'login', return_value=(client, None)), \
                        patch.object(fault, 'clear_pending_restart', return_value=1), \
                        patch.object(fault, 'wait_until'):
                    if isinstance(outcome, (http.client.RemoteDisconnected, ConnectionResetError)):
                        self.assertTrue(fault.prepare(root, 'unused', SQLFixture('mysql'))['request_disconnected'])
                    else:
                        with self.assertRaises((AssertionError, TimeoutError)):
                            fault.prepare(root, 'unused', SQLFixture('mysql'))
                self.assertEqual((root.parent / 'switch-ready').read_bytes(), b'')


if __name__ == '__main__':
    unittest.main()
