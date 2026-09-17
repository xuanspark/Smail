#!/usr/bin/python3
"""仅在临近到期时占用 443 续期，任何正常异常路径均恢复 Nginx。"""

import fcntl
import os
from pathlib import Path
import subprocess


ROOT = Path('/var/lib/smail-acme-tls')
DOMAINS = ('solit.cn', 'mail.solit.cn', 'smtp.solit.cn', 'imap.solit.cn', 'pop.solit.cn')


def main():
    if os.geteuid() != 0:
        raise RuntimeError('必须使用 root 执行证书续期')
    with open('/run/smail-certificate-renew.lock', 'a') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        certificate = str(ROOT / 'certificates/solit.cn.crt')
        subprocess.run(['openssl', 'x509', '-in', certificate, '-noout'], check=True)
        due = subprocess.run(['openssl', 'x509', '-in', certificate, '-noout', '-checkend', '2592000'],
                             capture_output=True).returncode != 0
        if due:
            subprocess.run(['nginx', '-t'], check=True)
            subprocess.run(['systemctl', 'is-active', '--quiet', 'nginx'], check=True)
            command = ['/usr/local/libexec/smail-acme/lego', '--path', str(ROOT),
                       '--email', '', '--accept-tos', '--key-type', 'rsa2048', '--tls',
                       '--http-timeout', '20', '--cert.timeout', '30']
            for domain in DOMAINS:
                command.extend(['--domains', domain])
            command.extend(['renew', '--days', '30', '--reuse-key', '--no-random-sleep'])
            # systemd 的 ExecStopPost 另行覆盖进程被终止时的服务恢复。
            try:
                subprocess.run(['systemctl', 'stop', 'nginx'], check=True)
                subprocess.run(command, check=True, timeout=150,
                               env={**os.environ, 'GOMEMLIMIT': '64MiB'})
            finally:
                subprocess.run(['systemctl', 'start', 'nginx'], check=True)
        else:
            print('证书有效期大于 30 天，本次不暂停 Web 服务')
        subprocess.run(['/usr/local/libexec/smail-acme/deploy_certificate.py'], check=True)


if __name__ == '__main__':
    main()
