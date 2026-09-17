'''验证反代验收同时约束公网与上游跳转，不访问真实网络。'''

from pathlib import Path
import sys
import unittest
from unittest.mock import patch


sys.path.insert(0, str(Path(__file__).resolve().parent))
import verify_nginx_tls as verify


class RedirectAcceptanceTest(unittest.TestCase):
    expected = 'https://mail.solit.cn/acceptance?x=1'

    def check(self, first, second):
        results = {'checks': []}
        with patch.object(verify, 'request', side_effect=[first, second]) as request:
            failures = verify.check_redirects(results)
        self.assertEqual(request.call_count, 2)
        self.assertEqual([call.args[0] for call in request.call_args_list], [80, 8001])
        self.assertEqual(len(results['checks']), 2)
        return failures

    def response(self, location=None, status=308):
        return status, {'Location': location or self.expected}, b''

    def test_both_ports_use_public_address(self):
        self.assertEqual(self.check(self.response(), self.response()), [])

    def test_internal_https_port_is_failure(self):
        failures = self.check(self.response(), self.response('https://mail.solit.cn:8002/acceptance?x=1'))
        self.assertEqual(len(failures), 1)
        self.assertEqual(failures[0]['port'], 8001)

    def test_public_port_does_not_skip_validation(self):
        failures = self.check(self.response('http://mail.solit.cn/acceptance?x=1'), self.response())
        self.assertEqual([item['port'] for item in failures], [80])

    def test_missing_location_records_failure(self):
        failures = self.check(self.response(), (308, {}, b''))
        self.assertIsNone(failures[0]['actualLocation'])

    def test_wrong_status_is_failure(self):
        failures = self.check(self.response(), self.response(status=200))
        self.assertEqual(failures[0]['actualStatus'], 200)

    def test_path_and_query_must_be_preserved(self):
        for location in ('https://mail.solit.cn/', 'https://mail.solit.cn/acceptance',
                         'https://other.invalid/acceptance?x=1'):
            with self.subTest(location=location):
                self.assertTrue(self.check(self.response(), self.response(location)))


if __name__ == '__main__':
    unittest.main()
