'''无停机 HTTP 验证选项回归，仅使用隔离目录和模拟续期命令。'''

from contextlib import nullcontext
import os
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
from unittest.mock import patch


BASE = Path(__file__).resolve().parent
SCRIPTS = BASE if (BASE / 'renew_certificate.py').exists() else BASE.parents[1] / 'deploy/nginx'
sys.path.insert(0, str(SCRIPTS))
if os.name == 'posix':
    with patch.dict(os.environ, {'SMAIL_MAIL_DOMAIN': 'example.com', 'SMAIL_WEB_DOMAIN': 'web.example.com'}):
        import renew_certificate as renew
else:
    renew = None


@unittest.skipUnless(os.name == 'posix', '证书部署脚本仅支持 Linux/Unix')
class WebrootTests(unittest.TestCase):
    def setUp(self):
        self.directory = Path(tempfile.mkdtemp(prefix='webroot-tests-', dir=Path.cwd()))
        self.webroot = self.directory / 'public'
        self.challenge = self.webroot / '.well-known/acme-challenge'
        self.challenge.mkdir(parents=True)
        for path in (self.webroot, self.challenge.parent, self.challenge):
            path.chmod(0o755)
        for context in (patch.object(renew, 'WEBROOT', self.webroot),
                        patch.object(renew, 'ROOT', self.directory),
                        patch.object(renew, 'DOMAINS', ('example.com', 'web.example.com')),
                        patch.dict(os.environ, {'SMAIL_ACME_CHALLENGE': 'http'}, clear=True)):
            context.start()
            self.addCleanup(context.stop)

    def execute(self, failure=None, due=True):
        calls = []

        def run(command, **kwargs):
            calls.append(command)
            if '-checkend' in command:
                return subprocess.CompletedProcess(command, 1 if due else 0)
            if command[0].endswith('/lego'):
                self.assertEqual(kwargs['timeout'], 600)
                self.assertEqual(kwargs['umask'], 0o022)
                self.assertEqual(command[command.index('--http.webroot') + 1], str(self.webroot))
                for required in ('--http', '--reuse-key', '--no-random-sleep'):
                    self.assertIn(required, command)
                for forbidden in ('--dns', '--tls', '--http.port'):
                    self.assertNotIn(forbidden, command)
                if failure:
                    raise failure
            return subprocess.CompletedProcess(command, 0)

        with patch.object(renew.os, 'geteuid', return_value=0), \
                patch.object(renew.deploy, 'certificate_lock', return_value=nullcontext()), \
                patch.object(renew.deploy, 'check_nginx_paths'), \
                patch.object(renew.deploy, 'publish') as publish, \
                patch.object(renew.subprocess, 'run', side_effect=run):
            if failure:
                with self.assertRaises(type(failure)):
                    renew.main()
                publish.assert_not_called()
            else:
                renew.main()
                publish.assert_called_once()
        self.assertFalse(any(command[:2] in (['systemctl', 'stop'], ['systemctl', 'start'])
                             for command in calls))
        return calls

    def test_http_explicitly_uses_existing_webroot(self):
        self.assertEqual(renew.challenge_arguments(), ['--http', '--http.webroot', str(self.webroot)])

    def test_wildcard_certificate_rejects_http_challenge(self):
        with patch.object(renew, 'DOMAINS', ('example.com', '*.example.com')):
            with self.assertRaisesRegex(RuntimeError, '泛域名证书必须使用 DNS 验证'):
                renew.challenge_arguments()

    def test_due_http_success_reuses_key_without_port_binding(self):
        self.assertTrue(any(command[0].endswith('/lego') for command in self.execute()))

    def test_due_http_failure_does_not_publish_or_stop(self):
        self.execute(subprocess.CalledProcessError(1, 'lego'))

    def test_due_http_timeout_does_not_publish_or_stop(self):
        self.execute(subprocess.TimeoutExpired('lego', 600))

    def test_not_due_does_not_request_new_certificate(self):
        self.assertFalse(any(command[0].endswith('/lego') for command in self.execute(due=False)))

    def test_invalid_method_does_not_fall_back(self):
        for method in ('', 'tls', 'HTTP', 'http dns'):
            with self.subTest(method=method), patch.dict(os.environ, {'SMAIL_ACME_CHALLENGE': method}):
                with self.assertRaises(RuntimeError):
                    renew.challenge_arguments()

    def test_dns_remains_explicit_and_requires_provider(self):
        with patch.dict(os.environ, {'SMAIL_ACME_CHALLENGE': 'dns'}):
            with self.assertRaises(RuntimeError):
                renew.challenge_arguments()
            with patch.dict(os.environ, {'SMAIL_ACME_DNS_PROVIDER': 'alidns'}):
                self.assertEqual(renew.challenge_arguments(), ['--dns', 'alidns'])

    def test_missing_directory_is_rejected(self):
        with patch.object(renew, 'WEBROOT', self.directory / 'missing'):
            with self.assertRaises(FileNotFoundError):
                renew.challenge_arguments()

    def test_public_account_directory_is_rejected(self):
        self.directory.chmod(0o755)
        try:
            with self.assertRaises(RuntimeError):
                renew.challenge_arguments()
        finally:
            self.directory.chmod(0o700)

    def test_child_umask_preserves_private_parent_and_readable_challenge(self):
        token = self.challenge / 'token'
        key = self.directory / 'key'
        previous = os.umask(0o077)
        try:
            subprocess.run([
                sys.executable, '-c',
                'import os,sys; fd=os.open(sys.argv[1],os.O_CREAT|os.O_EXCL|os.O_WRONLY,0o644); os.close(fd)',
                str(token),
            ], check=True, umask=0o022, timeout=10)
            key.write_bytes(b'private-test-data')
            self.assertEqual(token.stat().st_mode & 0o777, 0o644)
            self.assertEqual(key.stat().st_mode & 0o777, 0o600)
            self.assertEqual(self.directory.stat().st_mode & 0o777, 0o700)
        finally:
            os.umask(previous)

    def test_unsafe_permissions_on_each_directory_are_rejected(self):
        for directory in (self.webroot, self.challenge.parent, self.challenge):
            for mode in (0o775, 0o777, 0o750, 0o700):
                with self.subTest(directory=directory.name, mode=oct(mode)):
                    directory.chmod(mode)
                    try:
                        with self.assertRaises(RuntimeError):
                            renew.challenge_arguments()
                    finally:
                        directory.chmod(0o755)

    def test_symlink_directory_is_rejected(self):
        link = self.directory / 'linked'
        link.symlink_to(self.webroot, target_is_directory=True)
        with patch.object(renew, 'WEBROOT', link), self.assertRaises(RuntimeError):
            renew.challenge_arguments()

    def test_regular_file_is_not_webroot(self):
        path = self.directory / 'file'
        path.write_bytes(b'not-a-directory')
        path.chmod(0o755)
        with patch.object(renew, 'WEBROOT', path), self.assertRaises(RuntimeError):
            renew.challenge_arguments()


if __name__ == '__main__':
    unittest.main(verbosity=2)
