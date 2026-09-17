"""证书更新脚本的隔离回归，不操作生产文件或服务。"""

import importlib.util
from pathlib import Path
import subprocess
import unittest
from unittest.mock import mock_open, patch


BASE = Path(__file__).resolve().parent


def load(name):
    path = BASE / (name + '.py')
    if not path.exists():
        path = BASE.parents[1] / 'deploy/nginx' / (name + '.py')
    spec = importlib.util.spec_from_file_location(name, path)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


renew = load('renew_certificate')
deploy = load('deploy_certificate')


class RenewalTests(unittest.TestCase):
    def execute(self, callback):
        with patch.object(renew.os, 'geteuid', return_value=0), \
                patch('builtins.open', mock_open()), \
                patch.object(renew.fcntl, 'flock'), \
                patch.object(renew.subprocess, 'run', side_effect=callback):
            renew.main()

    def test_not_due_keeps_nginx_running(self):
        calls = []

        def run(command, **kwargs):
            calls.append(command)
            return subprocess.CompletedProcess(command, 0)

        self.execute(run)
        self.assertNotIn(['systemctl', 'stop', 'nginx'], calls)
        self.assertIn(['/usr/local/libexec/smail-acme/deploy_certificate.py'], calls)

    def test_due_failure_restores_nginx(self):
        for failure in (subprocess.CalledProcessError(1, 'lego'),
                        subprocess.TimeoutExpired('lego', 150)):
            with self.subTest(failure=type(failure).__name__):
                calls = []

                def run(command, **kwargs):
                    calls.append(command)
                    if '-checkend' in command:
                        return subprocess.CompletedProcess(command, 1)
                    if command[0].endswith('/lego'):
                        raise failure
                    return subprocess.CompletedProcess(command, 0)

                with self.assertRaises(type(failure)):
                    self.execute(run)
                self.assertEqual(calls[-1], ['systemctl', 'start', 'nginx'])
                self.assertNotIn(['/usr/local/libexec/smail-acme/deploy_certificate.py'], calls)

    def test_due_success_is_bounded_and_reuses_key(self):
        calls = []

        def run(command, **kwargs):
            calls.append(command)
            if '-checkend' in command:
                return subprocess.CompletedProcess(command, 1)
            if command[0].endswith('/lego'):
                self.assertEqual(kwargs['timeout'], 150)
                self.assertIn('--reuse-key', command)
                self.assertIn('--no-random-sleep', command)
            return subprocess.CompletedProcess(command, 0)

        self.execute(run)
        self.assertLess(calls.index(['systemctl', 'start', 'nginx']),
                        calls.index(['/usr/local/libexec/smail-acme/deploy_certificate.py']))

    def test_non_root_is_rejected(self):
        with patch.object(renew.os, 'geteuid', return_value=1000), \
                self.assertRaises(RuntimeError):
            renew.main()

    def test_untrusted_certificate_never_replaces_files(self):
        with patch.object(deploy.os, 'geteuid', return_value=0), \
                patch.object(deploy, 'run', side_effect=subprocess.CalledProcessError(1, 'openssl')), \
                patch.object(deploy, 'replace_file') as replace, \
                self.assertRaises(subprocess.CalledProcessError):
            deploy.main()
        replace.assert_not_called()

    def test_mismatched_private_key_never_replaces_files(self):
        def run(*command, **kwargs):
            return b'private-public-key' if command[1] == 'pkey' else b'certificate-public-key'

        with patch.object(deploy.os, 'geteuid', return_value=0), \
                patch.object(deploy, 'run', side_effect=run), \
                patch.object(deploy, 'replace_file') as replace, \
                self.assertRaisesRegex(RuntimeError, '不匹配'):
            deploy.main()
        replace.assert_not_called()


if __name__ == '__main__':
    unittest.main(verbosity=2)
