'''使用全新 ACME 测试账号验证公网 HTTP 挑战，不停服，不发布测试证书。'''

import argparse
from datetime import datetime, timezone
import hashlib
import http.client
import json
import os
from pathlib import Path
import socket
import subprocess
import threading
import time

import nginx_certificate_cutover as monitor
import renew_certificate as renew


SERVER = 'https://acme-staging-v02.api.letsencrypt.org/directory'
CONFIG = Path('/etc/nginx/conf.d/smail.conf')
CONFIG_SHA = 'bf6816c38f9c639ab17fd7344fff4b06afc5a2388f74318e18c60b8b3311ebd8'


def run(*command):
    return subprocess.check_output(command, text=True, timeout=15).strip()


def file_state(path):
    info = path.stat()
    return {'sha256': hashlib.sha256(path.read_bytes()).hexdigest(),
            'uid': info.st_uid, 'gid': info.st_gid, 'mode': oct(info.st_mode & 0o777)}


def service_state():
    return {name: run('systemctl', 'show', name, '--property=ActiveState,MainPID,InvocationID,NRestarts')
            for name in ('smail.service', 'nginx.service', 'Scert.service', 'mysql.service')}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--confirm-staging-challenge', action='store_true')
    args = parser.parse_args()
    if not args.confirm_staging_challenge or os.geteuid() != 0 or run('hostname') != 'ikun2':
        parser.error('只允许在 47 服务器明确执行测试 CA 的全新 HTTP 挑战')
    if '*.solit.cn' in renew.DOMAINS:
        parser.error('当前泛域名证书必须使用 DNS-01，不能执行 HTTP-01 演练')
    if file_state(CONFIG)['sha256'] != CONFIG_SHA:
        parser.error('Nginx 配置与已审查模板不符，停止演练')
    memory = dict(line.split(':', 1) for line in Path('/proc/meminfo').read_text().splitlines())
    if int(memory['MemAvailable'].split()[0]) < 256 * 1024:
        parser.error('可用内存不足，停止演练')
    run('nginx', '-t')
    run('systemctl', 'is-active', 'nginx', 'smail', 'Scert', 'mysql')
    for domain in renew.DOMAINS:
        addresses = {entry[4][0] for entry in socket.getaddrinfo(domain, 80, type=socket.SOCK_STREAM)}
        if addresses != {'47.108.78.12'}:
            parser.error('域名解析与本次授权目标不符：' + domain)
        connection = http.client.HTTPConnection('127.0.0.1', 80, timeout=5)
        try:
            connection.request('GET', '/.well-known/acme-challenge/preflight-absent', headers={'Host': domain})
            response = connection.getresponse()
            response.read()
            if response.status != 404:
                parser.error('验证路径未返回预期状态：' + domain)
        finally:
            connection.close()
    os.umask(0o077)
    stamp = time.strftime('%Y%m%d-%H%M%S')
    evidence = Path('/var/tmp') / ('smail-review-http-challenge-' + stamp)
    state = Path('/var/lib') / ('smail-acme-http-review-' + stamp)
    evidence.mkdir(mode=0o700)
    state.mkdir(mode=0o700)
    assert not list(state.iterdir()), '必须使用全新账号目录，不能复用授权缓存'
    renew.ROOT = state
    os.environ['SMAIL_ACME_CHALLENGE'] = 'http'
    challenge = renew.challenge_arguments()
    protected = [Path('/opt/smail/smail'), Path('/opt/smail/smail-admin'),
                 Path('/opt/smail/ssl/custom.crt'), Path('/opt/smail/ssl/custom.key'),
                 Path('/usr/local/libexec/smail-acme/renew_certificate.py'),
                 Path('/usr/local/libexec/smail-acme/deploy_certificate.py'),
                 Path('/etc/systemd/system/smail-certificate-renew.service')]
    protected.extend(sorted(Path('/etc/nginx/conf.d').glob('*.conf')))
    protected.extend(sorted(Path('/var/lib/smail-acme-tls/certificates').glob('solit.cn.*')))
    before = {str(path): file_state(path) for path in protected}
    before_services = service_state()
    challenge_dir = renew.WEBROOT / '.well-known/acme-challenge'
    before_tokens = {path.name: file_state(path) for path in challenge_dir.iterdir()}
    report = {'status': 'running', 'startedUTC': datetime.now(timezone.utc).isoformat(),
              'stagingOnly': True, 'freshAccount': True, 'productionDeploymentExercised': False,
              'evidence': str(evidence), 'state': str(state), 'beforeServices': before_services,
              'protectedBefore': before, 'samples': []}
    stop = threading.Event()

    def sample():
        for host in ('mail.solit.cn', 'web.apmmk.cn'):
            value = {'host': host, 'timeUTC': datetime.now(timezone.utc).isoformat()}
            try:
                value['bytes'] = len(monitor.request(host, 443))
                value['ok'] = True
            except Exception as error:
                value.update(ok=False, error=type(error).__name__)
            report['samples'].append(value)

    def watch():
        while not stop.wait(0.5):
            sample()

    unit = 'smail-http-challenge-' + stamp
    command = [
        'systemd-run', '--wait', '--pipe', '--unit=' + unit,
        '--property=Type=exec', '--property=UMask=0022',
        '--property=ProtectSystem=strict', '--property=ProtectHome=yes',
        '--property=NoNewPrivileges=yes', '--property=PrivateTmp=yes',
        '--property=InaccessiblePaths=/opt/smail /run/smail /etc/nginx /var/lib/smail-acme-tls',
        '--property=ReadWritePaths=' + str(state) + ' ' + str(challenge_dir),
        '--property=CapabilityBoundingSet=', '--property=MemoryHigh=96M',
        '--property=MemoryMax=128M', '--property=MemorySwapMax=0', '--property=CPUQuota=100%',
        '--property=TasksMax=64', '--property=RuntimeMaxSec=600s',
        '--property=TimeoutStopSec=15s', '--property=KillMode=control-group',
        '--property=OOMPolicy=stop', '--setenv=GOMEMLIMIT=64MiB',
        '/usr/local/libexec/smail-acme/lego', '--path', str(state), '--server', SERVER,
        '--email', '', '--accept-tos', '--key-type', 'rsa2048',
        '--http-timeout', '20', '--cert.timeout', '30', *challenge,
    ]
    for domain in renew.DOMAINS:
        command.extend(['--domains', domain])
    command.append('run')
    thread = threading.Thread(target=watch, daemon=True)
    with renew.deploy.certificate_lock():
        try:
            sample()
            assert all(item['ok'] for item in report['samples']), '演练前业务探测失败'
            thread.start()
            with (evidence / 'acme.log').open('xb') as log:
                result = subprocess.run(command, stdout=log, stderr=subprocess.STDOUT, timeout=630)
            report['exitCode'] = result.returncode
            assert result.returncode == 0, '全新 HTTP 挑战失败，日志已保留'
            certificate = state / 'certificates/solit.cn.crt'
            key = state / 'certificates/solit.cn.key'
            public_cert = run('openssl', 'x509', '-in', str(certificate), '-pubkey', '-noout')
            public_key = run('openssl', 'pkey', '-in', str(key), '-pubout')
            assert public_cert == public_key
            for domain in renew.DOMAINS:
                run('openssl', 'x509', '-in', str(certificate), '-checkhost', domain, '-noout')
            report['certificate'] = file_state(certificate)
            report['domains'] = list(renew.DOMAINS)
            report['certificateMetadata'] = run('openssl', 'x509', '-in', str(certificate),
                                                '-noout', '-issuer', '-dates', '-ext', 'subjectAltName')
            report['status'] = 'passed'
        except BaseException as error:
            report.update(status='failed', error=type(error).__name__)
            raise
        finally:
            try:
                stopped = subprocess.run(['systemctl', 'stop', unit], capture_output=True, text=True, timeout=25)
                report['testUnitStopped'] = stopped.returncode == 0 or 'not loaded' in stopped.stderr
                sample()
            finally:
                stop.set()
                if thread.ident:
                    thread.join(timeout=10)
                report['afterServices'] = service_state()
                report['servicesUnchanged'] = report['afterServices'] == before_services
                report['protectedUnchanged'] = all(file_state(path) == before[str(path)] for path in protected)
                report['challengeFilesRestored'] = before_tokens == {
                    path.name: file_state(path) for path in challenge_dir.iterdir()}
                report['probesPassed'] = bool(report['samples']) and all(item['ok'] for item in report['samples'])
                if not all(report.get(key) for key in ('testUnitStopped', 'servicesUnchanged',
                                                       'protectedUnchanged', 'challengeFilesRestored', 'probesPassed')):
                    report['status'] = 'failed'
                report['finishedUTC'] = datetime.now(timezone.utc).isoformat()
                (evidence / 'result.json').write_text(json.dumps(report, ensure_ascii=False, indent=2), encoding='utf-8')
                print(json.dumps({key: value for key, value in report.items()
                                  if key not in ('samples', 'protectedBefore')}, ensure_ascii=False, indent=2))
    assert report['status'] == 'passed'


if __name__ == '__main__':
    main()
