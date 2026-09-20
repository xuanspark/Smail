"""HTTP 撤销探针必须重放原 Cookie，不能把客户端清空 Cookie 视为服务端失效。"""

from http.cookies import SimpleCookie
from pathlib import Path
import sys
import unittest
from unittest.mock import MagicMock, patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'tools'))
import runtime_probe


class RuntimeSessionTests(unittest.TestCase):
    def test_session_snapshot_preserves_cookie_and_does_not_follow_logout(self):
        original = runtime_probe.Client()
        original.cookies = SimpleCookie('session=test-session; smail_csrf=test-csrf')
        replay = runtime_probe.copy_session(original)
        original.cookies.clear()
        self.assertEqual(replay.cookies['session'].value, 'test-session')
        self.assertEqual(replay.cookies['smail_csrf'].value, 'test-csrf')

    def test_only_explicit_need_login_is_accepted(self):
        client = MagicMock()
        for response in [(200, {'errorNo': 0}, None), (500, None, None),
                         (200, {'errorNo': 500}, None), (403, None, None)]:
            with self.subTest(response=response):
                client.request.return_value = response
                with self.assertRaises(AssertionError):
                    runtime_probe.require_session_denied(client)
        client.request.return_value = (200, {'errorNo': 403}, None)
        runtime_probe.require_session_denied(client)

    def test_real_actions_are_required_and_passwords_are_not_in_result(self):
        clients = []

        def create_client(*args, **kwargs):
            client = MagicMock()
            client.cookies = SimpleCookie('session=test-session')
            client.success.return_value = {'account': 'sessionreview'}
            client.request.side_effect = lambda path, *args: (200, {'errorNo': 100 if path.endswith('/login') else 403}, None)
            clients.append(client)
            return client

        admin = MagicMock()
        admin.success.return_value = {'account': runtime_probe.ACCOUNT}
        with patch.object(runtime_probe, 'Client', side_effect=create_client), \
             patch.object(runtime_probe.secrets, 'token_urlsafe', side_effect=['TestPass1234', 'NewPass12345']):
            result = runtime_probe.verify_http_session_revocation(admin, None)
        self.assertTrue(all(result.values()))
        self.assertEqual(len(result), 8)
        edits = [call.args[1] for call in admin.success.call_args_list if call.args[0].endswith('/edit')]
        self.assertEqual([item['disabled'] for item in edits], [1, 0])
        paths = [call.args[0] for client in clients for call in client.success.call_args_list]
        self.assertIn('/api/v1/logout', paths)
        self.assertIn('/api/v1/settings/modify_password', paths)
        self.assertNotIn('TestPass1234', str(result))
        self.assertNotIn('NewPass12345', str(result))


if __name__ == '__main__':
    unittest.main()
