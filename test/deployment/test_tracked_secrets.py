"""凭据扫描回归使用合成值，不复制真实凭据。"""

from pathlib import Path
import os
import runpy
import sys
import types
import unittest
from unittest.mock import Mock, patch

sys.path.insert(0, str(Path(__file__).resolve().parents[1] / 'tools'))
from check_tracked_secrets import scan_source


class TrackedSecretsTest(unittest.TestCase):
    def test_cloud_key_literal_is_rejected_without_value(self):
        secret = 'LTAI' + 'a' * 20
        result = scan_source('config.txt', 'key=' + secret)
        self.assertEqual(result, [('config.txt', 1, 'credential_literal')])
        self.assertNotIn(secret, repr(result))

    def test_python_cloud_secret_assignment_is_rejected(self):
        result = scan_source('tool.py', 'ak="sample"\nsk="sample-secret"\n')
        self.assertEqual(len(result), 2)
        self.assertNotIn('sample-secret', repr(result))

    def test_environment_lookup_is_allowed(self):
        self.assertEqual(scan_source('tool.py', 'import os\nak=os.environ["KEY"]\nsk=os.environ["SECRET"]'), [])

    def test_private_key_is_rejected(self):
        header = '-----BEGIN RSA PRIVATE' + ' KEY-----'
        self.assertEqual(scan_source('key.pem', header), [('key.pem', 1, 'credential_literal')])

    def test_python_parse_failure_is_not_silently_skipped(self):
        self.assertEqual(scan_source('tool.py', 'if:'), [('tool.py', 1, 'python_parse_failed')])

    def test_missing_cloud_environment_rejects_before_network(self):
        tools = Path(__file__).resolve().parents[1] / 'tools'
        ssh_module = types.ModuleType('paramiko')
        ssh_module.SSHClient = Mock(side_effect=AssertionError('unexpected SSH operation'))
        for name in ('add_dns.py', 'add_missing_dns.py', 'add_missing_dns_v2.py', 'check_alidns.py'):
            for environment, missing in (({}, 'SMAIL_ALIDNS_ACCESS_KEY_ID'),
                                         ({'SMAIL_ALIDNS_ACCESS_KEY_ID': 'sample'}, 'SMAIL_ALIDNS_ACCESS_KEY_SECRET')):
                with self.subTest(name=name, missing=missing), \
                     patch.dict(os.environ, environment, clear=True), \
                     patch.dict(sys.modules, {'paramiko': ssh_module}), \
                     patch('urllib.request.urlopen', side_effect=AssertionError('unexpected HTTP operation')), \
                     self.assertRaises(KeyError) as error:
                    runpy.run_path(str(tools / name), run_name='__main__')
                self.assertEqual(error.exception.args, (missing,))
        ssh_module.SSHClient.assert_not_called()


if __name__ == '__main__':
    unittest.main()
