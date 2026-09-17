#!/usr/bin/python3
"""使用 DNS 验证续期，不停止 Nginx，候选证书通过校验后才发布。"""

import os
from pathlib import Path
import re
import subprocess

import deploy_certificate as deploy


ROOT = Path('/var/lib/smail-acme-tls')
DOMAINS = ('solit.cn', 'mail.solit.cn', 'smtp.solit.cn', 'imap.solit.cn', 'pop.solit.cn')
RENEW_TIMEOUT = 600


def dns_provider():
    provider = os.environ.get('SMAIL_ACME_DNS_PROVIDER', '')
    if not re.fullmatch(r'[a-z][a-z0-9_]{0,63}', provider) or provider == 'exec':
        raise RuntimeError('未配置有效的 DNS 服务商，拒绝续期；不会回退到停机验证')
    return provider


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
            provider = dns_provider()
            deploy.check_nginx_paths()
            subprocess.run(['nginx', '-t'], check=True)
            subprocess.run(['systemctl', 'is-active', '--quiet', 'nginx'], check=True)
            command = ['/usr/local/libexec/smail-acme/lego', '--path', str(ROOT),
                       '--email', '', '--accept-tos', '--key-type', 'rsa2048', '--dns', provider,
                       '--server', 'https://acme-v02.api.letsencrypt.org/directory',
                       '--http-timeout', '20', '--cert.timeout', '30']
            for domain in DOMAINS:
                command.extend(['--domains', domain])
            command.extend(['renew', '--days', '30', '--reuse-key', '--no-random-sleep'])
            subprocess.run(command, check=True, timeout=RENEW_TIMEOUT,
                           env={**os.environ, 'GOMEMLIMIT': '64MiB'})
        else:
            print('线上证书有效期大于 30 天，本次不申请新证书')
        deploy.publish()


if __name__ == '__main__':
    main()
