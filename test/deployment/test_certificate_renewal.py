"""证书更新脚本的隔离回归，不操作生产文件或服务。"""

from contextlib import nullcontext
import os
from pathlib import Path
import signal
import subprocess
import sys
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch


BASE = Path(__file__).resolve().parent
SCRIPTS = BASE if (BASE / 'deploy_certificate.py').exists() else BASE.parents[1] / 'deploy/nginx'
sys.path.insert(0, str(SCRIPTS))


with patch.dict(os.environ, {'SMAIL_MAIL_DOMAIN': 'cdis.cn', 'SMAIL_WEB_DOMAIN': 'web.cdis.cn'}):
    import deploy_certificate as deploy
    import renew_certificate as renew
import nginx_certificate_cutover as cutover


class DomainConfigurationTests(unittest.TestCase):
    def test_domain_requires_explicit_configuration(self):
        with patch.dict(os.environ, {}, clear=True), self.assertRaises(KeyError):
            deploy.configured_domain('SMAIL_MAIL_DOMAIN')

    def test_domain_rejects_invalid_input(self):
        for value in ('', ' ', 'localhost', '../example.test', 'https://example.test',
                      '*.example.test', 'example.test:443', 'example..test',
                      '-mail.example.test', 'mail-.example.test',
                      'a' * 64 + '.test', '.'.join(['a' * 63] * 4)):
            with self.subTest(value=value), \
                    patch.dict(os.environ, {'SMAIL_MAIL_DOMAIN': value}), \
                    self.assertRaises(ValueError):
                deploy.configured_domain('SMAIL_MAIL_DOMAIN')

    def test_domain_uses_user_configuration(self):
        with patch.dict(os.environ, {'SMAIL_MAIL_DOMAIN': ' New.Example.Test '}):
            self.assertEqual(deploy.configured_domain('SMAIL_MAIL_DOMAIN'), 'new.example.test')


class CutoverTests(unittest.TestCase):
    def test_atomic_config_preserves_mode_under_restrictive_umask(self):
        directory = Path(tempfile.mkdtemp(prefix='cutover-tests-', dir=Path.cwd()))
        path = directory / 'smail.conf'
        previous = os.umask(0o077)
        try:
            with patch.object(cutover, 'CONFIG', path):
                cutover.atomic_config(b'candidate\n', 0o644)
                self.assertEqual(path.read_bytes(), b'candidate\n')
                self.assertEqual(path.stat().st_mode & 0o777, 0o644)
                cutover.atomic_config(b'previous\n', 0o644)
                self.assertEqual(path.read_bytes(), b'previous\n')
                self.assertEqual(path.stat().st_mode & 0o777, 0o644)
        finally:
            os.umask(previous)


class RenewalTests(unittest.TestCase):
    def execute(self, callback, provider='alidns'):
        with patch.object(renew.os, 'geteuid', return_value=0), \
                patch.object(deploy, 'certificate_lock', return_value=nullcontext()), \
                patch.object(deploy, 'check_nginx_paths'), \
                patch.object(deploy, 'publish') as publish, \
                patch.dict(os.environ, {'SMAIL_ACME_DNS_PROVIDER': provider}), \
                patch.object(renew.subprocess, 'run', side_effect=callback):
            renew.main()
            return publish

    def test_not_due_keeps_nginx_running(self):
        calls = []

        def run(command, **kwargs):
            calls.append(command)
            return subprocess.CompletedProcess(command, 0)

        publish = self.execute(run, provider='')
        self.assertNotIn(['systemctl', 'stop', 'nginx'], calls)
        self.assertFalse(any(command[0].endswith('/lego') for command in calls))
        publish.assert_called_once()

    def test_due_failure_never_stops_nginx_or_publishes(self):
        for failure in (subprocess.CalledProcessError(1, 'lego'),
                        subprocess.TimeoutExpired('lego', renew.RENEW_TIMEOUT)):
            with self.subTest(failure=type(failure).__name__):
                calls = []

                def run(command, **kwargs):
                    calls.append(command)
                    if '-checkend' in command:
                        return subprocess.CompletedProcess(command, 1)
                    if command[0].endswith('/lego'):
                        raise failure
                    return subprocess.CompletedProcess(command, 0)

                with patch.object(deploy, 'certificate_lock', return_value=nullcontext()), \
                        patch.object(deploy, 'check_nginx_paths'), \
                        patch.object(deploy, 'publish') as publish, \
                        patch.dict(os.environ, {'SMAIL_ACME_DNS_PROVIDER': 'alidns'}), \
                        patch.object(renew.subprocess, 'run', side_effect=run), \
                        self.assertRaises(type(failure)):
                    renew.main()
                publish.assert_not_called()
                self.assertFalse(any(command[:2] in (['systemctl', 'stop'], ['systemctl', 'start'])
                                     for command in calls))

    def test_due_success_is_bounded_and_reuses_key(self):
        calls = []

        def run(command, **kwargs):
            calls.append(command)
            if '-checkend' in command:
                return subprocess.CompletedProcess(command, 1)
            if command[0].endswith('/lego'):
                self.assertEqual(kwargs['timeout'], 600)
                self.assertEqual(kwargs['umask'], 0o077)
                self.assertEqual(command[command.index('--dns') + 1], 'alidns')
                self.assertNotIn('--tls', command)
                self.assertIn('--server', command)
                self.assertIn('--reuse-key', command)
                self.assertIn('--no-random-sleep', command)
            return subprocess.CompletedProcess(command, 0)

        publish = self.execute(run)
        publish.assert_called_once()
        self.assertTrue(any(command[0].endswith('/lego') for command in calls))
        self.assertFalse(any(command[:2] in (['systemctl', 'stop'], ['systemctl', 'start'])
                             for command in calls))

    def test_due_uses_live_certificate_not_candidate(self):
        def run(command, **kwargs):
            if command[0] == 'openssl':
                self.assertEqual(command[command.index('-in') + 1], str(deploy.TARGET / 'custom.crt'))
            return subprocess.CompletedProcess(command, 0)

        self.execute(run)

    def test_missing_or_unsafe_provider_fails_before_lego(self):
        for provider in ('', '--tls', 'exec', 'alidns --tls', '../alidns'):
            calls = []

            def run(command, **kwargs):
                calls.append(command)
                return subprocess.CompletedProcess(command, 1 if '-checkend' in command else 0)

            with self.subTest(provider=provider), self.assertRaises(RuntimeError):
                self.execute(run, provider)
            self.assertFalse(any(command[0].endswith('/lego') for command in calls))

    def test_invalid_expiry_check_fails_closed(self):
        def run(command, **kwargs):
            return subprocess.CompletedProcess(command, 2 if '-checkend' in command else 0)

        with self.assertRaisesRegex(RuntimeError, '无法判断'):
            self.execute(run)

    def test_non_root_is_rejected(self):
        with patch.object(renew.os, 'geteuid', return_value=1000), \
                self.assertRaises(RuntimeError):
            renew.main()


class DeploymentTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        if os.geteuid() != 0:
            raise RuntimeError('本回归必须在远程隔离 root 单元中执行')
        cls.artifacts = Path(tempfile.mkdtemp(prefix='certificate-tests-', dir=Path.cwd()))
        cls.ca = cls.artifacts / 'ca.crt'
        cls.ca_key = cls.artifacts / 'ca.key'
        cls.key = cls.artifacts / 'server.key'
        cls.csr = cls.artifacts / 'server.csr'
        cls.old = cls.artifacts / 'old.crt'
        cls.new = cls.artifacts / 'new.crt'
        cls.real_run = staticmethod(deploy.run)
        cls.real_run('openssl', 'req', '-x509', '-newkey', 'rsa:2048', '-nodes',
                     '-keyout', str(cls.ca_key), '-out', str(cls.ca), '-days', '120',
                     '-subj', '/CN=Smail isolated test CA',
                     '-addext', 'keyUsage=critical,keyCertSign,cRLSign')
        cls.real_run('openssl', 'req', '-new', '-newkey', 'rsa:2048', '-nodes',
                     '-keyout', str(cls.key), '-out', str(cls.csr), '-subj', '/CN=cdis.cn',
                     '-addext', 'basicConstraints=critical,CA:FALSE',
                     '-addext', 'keyUsage=critical,digitalSignature,keyEncipherment',
                     '-addext', 'extendedKeyUsage=serverAuth',
                     '-addext', 'subjectAltName=' + ','.join('DNS:' + d for d in deploy.DOMAINS))
        for target, serial, days in ((cls.old, '1', '20'), (cls.new, '2', '90')):
            cls.real_run('openssl', 'x509', '-req', '-in', str(cls.csr), '-CA', str(cls.ca),
                         '-CAkey', str(cls.ca_key), '-set_serial', serial, '-days', days,
                         '-copy_extensions', 'copy', '-out', str(target))
        print('保留隔离证据目录=' + str(cls.artifacts), flush=True)

    def setUp(self):
        self.directory = self.artifacts / self._testMethodName
        self.directory.mkdir(mode=0o700)
        self.source = self.directory / 'candidate'
        self.target = self.directory / 'live'
        self.backups = self.directory / 'backups'
        for directory in (self.source, self.target, self.backups):
            directory.mkdir(mode=0o700)
        for path, value in ((self.source / 'cdis.cn.crt', self.new.read_bytes()),
                            (self.source / 'cdis.cn.key', self.key.read_bytes()),
                            (self.source / 'cdis.cn.issuer.crt', self.ca.read_bytes()),
                            (self.target / 'custom.crt', self.old.read_bytes()),
                            (self.target / 'custom.key', self.key.read_bytes())):
            path.write_bytes(value)
            path.chmod(0o600)
        self.calls = []
        self.failure = None
        self.failed_once = False
        self.config = ('ssl_certificate ' + str(self.target / 'custom.crt') + ';\n'
                       + 'ssl_certificate_key ' + str(self.target / 'custom.key') + ';\n')
        for name, value in (('LINEAGE', self.source), ('TARGET', self.target),
                            ('BACKUPS', self.backups), ('CA_FILE', str(self.ca)),
                            ('LOCK', self.directory / 'renew.lock')):
            patcher = patch.object(deploy, name, value)
            patcher.start()
            self.addCleanup(patcher.stop)
        identity = patch.object(deploy.pwd, 'getpwnam', return_value=SimpleNamespace(pw_uid=0, pw_gid=0))
        identity.start()
        self.addCleanup(identity.stop)
        commands = patch.object(deploy, 'run', side_effect=self.command)
        commands.start()
        self.addCleanup(commands.stop)
        live = patch.object(deploy, 'verify_live_certificate')
        self.real_verify = deploy.verify_live_certificate
        self.live = live.start()
        self.addCleanup(live.stop)

    def command(self, *command, **kwargs):
        self.calls.append(command)
        if command == self.failure and not self.failed_once:
            self.failed_once = True
            raise subprocess.CalledProcessError(1, command)
        if command == ('nginx', '-T'):
            return self.config.encode()
        if command[0] in ('nginx', 'systemctl'):
            return b''
        return self.real_run(*command, **kwargs)

    def assert_old_untouched(self):
        self.assertEqual((self.target / 'custom.crt').read_bytes(), self.old.read_bytes())
        self.assertEqual((self.target / 'custom.key').read_bytes(), self.key.read_bytes())

    def test_publish_valid_certificate_reloads_without_restart(self):
        deploy.main()
        self.assertEqual((self.target / 'custom.crt').read_bytes(), self.new.read_bytes())
        self.assertIn(('systemctl', 'reload', 'nginx'), self.calls)
        self.assertNotIn(('systemctl', 'stop', 'nginx'), self.calls)
        self.assertNotIn(('systemctl', 'start', 'nginx'), self.calls)
        self.live.assert_called_once_with(deploy.fingerprint(self.new))
        for path in self.backups.rglob('*'):
            self.assertEqual(path.stat().st_mode & 0o077, 0)

    def test_unchanged_certificate_checks_live_without_reload(self):
        (self.source / 'cdis.cn.crt').write_bytes(self.old.read_bytes())
        deploy.main()
        self.assertNotIn(('systemctl', 'reload', 'nginx'), self.calls)
        self.live.assert_called_once()

    def test_interrupted_publish_is_reconciled_when_files_already_match(self):
        (self.source / 'cdis.cn.crt').write_bytes(self.old.read_bytes())
        self.live.side_effect = [RuntimeError('上次重载前中断'), None]
        deploy.main()
        self.assertEqual(self.calls.count(('systemctl', 'reload', 'nginx')), 1)
        self.assertEqual(self.live.call_count, 2)

    def test_failed_reconciliation_does_not_report_success(self):
        (self.source / 'cdis.cn.crt').write_bytes(self.old.read_bytes())
        self.live.side_effect = RuntimeError('线上证书不匹配')
        with self.assertRaisesRegex(RuntimeError, '线上证书不匹配'):
            deploy.main()
        self.assertEqual(self.live.call_count, 2)
        self.assert_old_untouched()

    def test_untrusted_certificate_does_not_change_live_files(self):
        with patch.object(deploy, 'CA_FILE', '/etc/ssl/certs/ca-certificates.crt'), \
                self.assertRaises(subprocess.CalledProcessError):
            deploy.main()
        self.assert_old_untouched()
        self.assertNotIn(('systemctl', 'reload', 'nginx'), self.calls)

    def test_wrong_domain_does_not_change_live_files(self):
        with patch.object(deploy, 'DOMAINS', ('wrong.example',)), \
                self.assertRaisesRegex(RuntimeError, '根域名和泛域名'):
            deploy.main()
        self.assert_old_untouched()

    def test_short_lived_certificate_is_rejected(self):
        self.real_run('openssl', 'x509', '-req', '-in', str(self.csr), '-CA', str(self.ca),
                      '-CAkey', str(self.ca_key), '-set_serial', '3', '-days', '1',
                      '-copy_extensions', 'copy', '-out', str(self.source / 'cdis.cn.crt'))
        with self.assertRaises(subprocess.CalledProcessError):
            deploy.main()
        self.assert_old_untouched()

    def test_mismatched_key_is_rejected(self):
        (self.source / 'cdis.cn.key').write_bytes(self.ca_key.read_bytes())
        with self.assertRaisesRegex(RuntimeError, '不匹配'):
            deploy.main()
        self.assert_old_untouched()

    def test_key_rotation_is_rejected(self):
        (self.target / 'custom.key').write_bytes(self.ca_key.read_bytes())
        with self.assertRaisesRegex(RuntimeError, '复用现有密钥'):
            deploy.main()
        self.assertEqual((self.target / 'custom.crt').read_bytes(), self.old.read_bytes())

    def test_nginx_still_using_candidate_is_rejected(self):
        self.config += 'ssl_certificate ' + str(self.source / 'cdis.cn.crt') + ';\n'
        with self.assertRaisesRegex(RuntimeError, '签发目录'):
            deploy.main()
        self.assert_old_untouched()

    def test_missing_live_nginx_paths_is_rejected(self):
        self.config = 'ssl_certificate /some/other/site.crt;\n'
        with self.assertRaisesRegex(RuntimeError, '未找到'):
            deploy.main()
        self.assert_old_untouched()

    def test_nginx_test_failure_rolls_back_and_reloads_old(self):
        self.failure = ('nginx', '-t')
        with self.assertRaises(subprocess.CalledProcessError):
            deploy.main()
        self.assert_old_untouched()
        self.assertEqual(self.calls.count(('nginx', '-t')), 2)
        self.assertIn(('systemctl', 'reload', 'nginx'), self.calls)
        self.live.assert_called_once_with(deploy.fingerprint(self.old))

    def test_reload_failure_rolls_back_and_reloads_old(self):
        self.failure = ('systemctl', 'reload', 'nginx')
        with self.assertRaises(subprocess.CalledProcessError):
            deploy.main()
        self.assert_old_untouched()
        self.assertEqual(self.calls.count(('systemctl', 'reload', 'nginx')), 2)

    def test_live_probe_failure_rolls_back_and_verifies_old(self):
        self.live.side_effect = [RuntimeError('模拟线上证书不匹配'), None]
        with self.assertRaisesRegex(RuntimeError, '模拟'):
            deploy.main()
        self.assert_old_untouched()
        self.assertEqual(self.live.call_count, 2)
        self.live.assert_called_with(deploy.fingerprint(self.old))

    def test_unreadable_private_permissions_are_rejected(self):
        (self.source / 'cdis.cn.key').chmod(0o644)
        with self.assertRaisesRegex(RuntimeError, '权限'):
            deploy.main()
        self.assert_old_untouched()

    def test_symlink_source_is_rejected(self):
        link = self.directory / 'linked-source'
        link.symlink_to(self.source, target_is_directory=True)
        with patch.object(deploy, 'LINEAGE', link), self.assertRaisesRegex(RuntimeError, '符号链接'):
            deploy.main()
        self.assert_old_untouched()

    def test_concurrent_deployment_is_rejected(self):
        with deploy.certificate_lock(), self.assertRaises(BlockingIOError):
            deploy.main()
        self.assert_old_untouched()

    def test_unsafe_lock_permissions_are_rejected(self):
        deploy.LOCK.touch(mode=0o644)
        deploy.LOCK.chmod(0o644)
        with self.assertRaisesRegex(RuntimeError, '锁文件'):
            deploy.main()
        self.assert_old_untouched()

    def test_candidate_mutation_after_snapshot_cannot_change_published_bytes(self):
        validate = deploy.validate

        def mutate(snapshot):
            (self.source / 'cdis.cn.crt').write_bytes(b'changed-after-snapshot')
            validate(snapshot)

        with patch.object(deploy, 'validate', side_effect=mutate):
            deploy.main()
        self.assertEqual((self.target / 'custom.crt').read_bytes(), self.new.read_bytes())

    def test_real_isolated_nginx_reload_and_rollback(self):
        configuration = self.directory / 'nginx.conf'
        configuration.write_text(
            'worker_processes 1;\n'
            + 'pid ' + str(self.directory / 'nginx.pid') + ';\n'
            + 'error_log ' + str(self.directory / 'nginx-error.log') + ';\n'
            + 'events { worker_connections 32; }\n'
            + 'http {\naccess_log off;\n'
            + ''.join(name + '_temp_path ' + str(self.directory / name) + ';\n'
                      for name in ('client_body', 'proxy', 'fastcgi', 'uwsgi', 'scgi'))
            + 'server {\n'
            + 'listen 127.0.0.1:18443 ssl;\nlisten 127.0.0.1:18002 ssl;\n'
            + 'server_name mail.cdis.cn;\n' + self.config
            + 'location / { return 200 "ready"; }\n}\n}\n', encoding='utf-8')
        arguments = ('nginx', '-c', str(configuration), '-p', str(self.directory))

        def isolated(*command, **kwargs):
            self.calls.append(command)
            if command == ('nginx', '-T'):
                return self.real_run(*arguments, '-T')
            if command == ('nginx', '-t'):
                return self.real_run(*arguments, '-t')
            if command == ('systemctl', 'is-active', '--quiet', 'nginx'):
                if process.poll() is not None:
                    raise RuntimeError('隔离 Nginx 未运行')
                return b''
            if command == ('systemctl', 'reload', 'nginx'):
                return self.real_run(*arguments, '-s', 'reload')
            if command[0] == 'systemctl':
                raise AssertionError('隔离测试禁止操作主机服务')
            return self.real_run(*command, **kwargs)

        with (self.directory / 'nginx-start.log').open('wb') as log:
            process = subprocess.Popen((*arguments, '-g', 'daemon off;'), stdout=log, stderr=log)
            try:
                with patch.object(deploy, 'run', side_effect=isolated), \
                        patch.object(deploy, 'LIVE_PORTS', (18443, 18002)), \
                        patch.object(deploy, 'verify_live_certificate', side_effect=self.real_verify):
                    self.real_verify(deploy.fingerprint(self.old))
                    deploy.main()
                    self.real_verify(deploy.fingerprint(self.new))
                    self.assertIsNone(process.poll())
                    (self.source / 'cdis.cn.crt').write_bytes(self.old.read_bytes())
                    attempted = False

                    def fail_after_live_update(expected):
                        nonlocal attempted
                        self.real_verify(expected)
                        if not attempted:
                            attempted = True
                            raise RuntimeError('模拟新证书上线后健康检查失败')

                    with patch.object(deploy, 'verify_live_certificate', side_effect=fail_after_live_update), \
                            self.assertRaisesRegex(RuntimeError, '健康检查失败'):
                        deploy.main()
                    self.real_verify(deploy.fingerprint(self.new))
                    self.assertEqual((self.target / 'custom.crt').read_bytes(), self.new.read_bytes())
                    self.assertIsNone(process.poll())
            finally:
                if process.poll() is None:
                    process.send_signal(signal.SIGQUIT)
                    try:
                        process.wait(timeout=10)
                    except subprocess.TimeoutExpired:
                        process.kill()
                        process.wait(timeout=10)


if __name__ == '__main__':
    unittest.main(verbosity=2)
