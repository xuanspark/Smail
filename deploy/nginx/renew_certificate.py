#!/usr/bin/python3
'''使用显式 DNS 或 Webroot 验证续期，不停止 Nginx，校验通过后才发布。'''

import os
from pathlib import Path
import re
import stat
import subprocess

import deploy_certificate as deploy


ROOT = Path('/var/lib/smail-acme-tls')
DOMAINS = ('solit.cn', 'mail.solit.cn', 'smtp.solit.cn', 'imap.solit.cn', 'pop.solit.cn')
RENEW_TIMEOUT = 600
WEBROOT = Path('/var/lib/smail-acme')


def dns_provider():
    provider = os.environ.get('SMAIL_ACME_DNS_PROVIDER', '')
    if not re.fullmatch(r'[a-z][a-z0-9_]{0,63}', provider) or provider == 'exec':
        raise RuntimeError('未配置有效的 DNS 服务商，拒绝续期；不会回退到停机验证')
    return provider


def challenge_arguments():
    method = os.environ.get('SMAIL_ACME_CHALLENGE', 'dns')
    if method == 'dns':
        return ['--dns', dns_provider()]
    if method != 'http':
        raise RuntimeError('验证方式必须为 dns 或 http，不允许自动回退到停机验证')
    private = ROOT.lstat()
    if (ROOT.resolve(strict=True) != ROOT or not stat.S_ISDIR(private.st_mode)
            or private.st_uid != 0 or private.st_mode & 0o077):
        raise RuntimeError('ACME 账号目录必须由 root 独占，禁止公开访问')
    for directory in (WEBROOT, WEBROOT / '.well-known', WEBROOT / '.well-known/acme-challenge'):
        info = directory.lstat()
        if (directory.resolve(strict=True) != directory or not stat.S_ISDIR(info.st_mode)
                or info.st_uid != 0 or info.st_mode & 0o022
                or info.st_mode & 0o005 != 0o005):
            raise RuntimeError('HTTP 验证目录必须由 root 管理、可读取且不能被其他用户写入')
    return ['--http', '--http.webroot', str(WEBROOT)]


def main():
    if os.geteuid() != 0:
        raise RuntimeError('必须使用 root 执行证书续期')
    with deploy.certificate_lock():
        certificate = str(deploy.TARGET / 'custom.crt')
        subprocess.run(['openssl', 'x509', '-in', certificate, '-noout'], check=True)
        result = subprocess.run(['openssl', 'x509', '-in', certificate, '-noout', '-checkend', '2592000'],
                                capture_output=True)
        if result.returncode not in (0, 1):
            raise RuntimeError('无法判断线上证书有效期，拒绝续期')
        due = result.returncode == 1
        if due:
            challenge = challenge_arguments()
            deploy.check_nginx_paths()
            subprocess.run(['nginx', '-t'], check=True)
            subprocess.run(['systemctl', 'is-active', '--quiet', 'nginx'], check=True)
            command = ['/usr/local/libexec/smail-acme/lego', '--path', str(ROOT),
                       '--email', '', '--accept-tos', '--key-type', 'rsa2048', *challenge,
                       '--server', 'https://acme-v02.api.letsencrypt.org/directory',
                       '--http-timeout', '20', '--cert.timeout', '30']
            for domain in DOMAINS:
                command.extend(['--domains', domain])
            command.extend(['renew', '--days', '30', '--reuse-key', '--no-random-sleep'])
            subprocess.run(command, check=True, timeout=RENEW_TIMEOUT,
                           env={**os.environ, 'GOMEMLIMIT': '64MiB'},
                           umask=0o022 if '--http' in challenge else 0o077)
        else:
            print('线上证书有效期大于 30 天，本次不申请新证书')
        deploy.publish()


if __name__ == '__main__':
    main()
