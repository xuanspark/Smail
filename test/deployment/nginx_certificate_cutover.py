"""在远程主机迁移 Smail 的 Nginx 证书路径，保留回退和连续探测证据。"""

import argparse
from datetime import datetime, timezone
import hashlib
from html.parser import HTMLParser
import http.client
import json
import os
from pathlib import Path
import re
import socket
import ssl
import subprocess
import threading
import time
import uuid

import deploy_certificate as deploy


CONFIG = Path('/etc/nginx/conf.d/smail.conf')
ROOT = Path('/var/tmp')
LIMIT = 8 * 1024 * 1024


def run(*command):
    return subprocess.run(command, check=True, capture_output=True, timeout=15).stdout


def digest(data):
    return hashlib.sha256(data).hexdigest()


def state():
    return run('systemctl', 'show', 'nginx', 'smail', '-p', 'Id', '-p', 'MainPID',
               '-p', 'ActiveState', '-p', 'InvocationID').decode()


def request(host, port, path='/', slow=False):
    raw = socket.socket()
    raw.settimeout(15 if slow else 3)
    if slow:
        raw.setsockopt(socket.SOL_SOCKET, socket.SO_RCVBUF, 8192)
    try:
        raw.connect(('127.0.0.1', port))
        secure = ssl.create_default_context().wrap_socket(raw, server_hostname=host)
    except BaseException:
        raw.close()
        raise
    try:
        authority = host if port == 443 else host + ':' + str(port)
        secure.sendall(('GET ' + path + ' HTTP/1.1\r\nHost: ' + authority
                        + '\r\nAccept-Encoding: identity\r\nConnection: close\r\n\r\n').encode())
        response = http.client.HTTPResponse(secure)
        response.begin()
        if response.status != 200:
            raise RuntimeError('HTTPS 状态异常：' + str(response.status))
        if slow:
            return secure, response
        with response:
            data = response.read(LIMIT + 1)
            if len(data) > LIMIT:
                raise RuntimeError('响应超出本次探测上限')
        secure.close()
        return data
    except BaseException:
        secure.close()
        raise


class Scripts(HTMLParser):
    def __init__(self):
        super().__init__()
        self.paths = []

    def handle_starttag(self, tag, attrs):
        source = dict(attrs).get('src', '')
        if tag == 'script' and source.startswith('/assets/') and source.endswith('.js'):
            self.paths.append(source)


def atomic_config(data, mode):
    staged = CONFIG.with_name(CONFIG.name + '.candidate-' + uuid.uuid4().hex)
    fd = os.open(staged, os.O_WRONLY | os.O_CREAT | os.O_EXCL, mode)
    with os.fdopen(fd, 'wb') as stream:
        os.fchmod(stream.fileno(), mode)
        stream.write(data)
        stream.flush()
        os.fsync(stream.fileno())
    os.replace(staged, CONFIG)
    fd = os.open(CONFIG.parent, os.O_RDONLY | os.O_DIRECTORY)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--evidence-dir', required=True, type=Path)
    parser.add_argument('--expected-config-sha256', required=True)
    parser.add_argument('--asset-path', help='用于下载连续性检查的已部署大型脚本路径')
    args = parser.parse_args()
    evidence = args.evidence_dir.resolve(strict=True)
    if (os.geteuid() != 0 or evidence.parent != ROOT
            or not evidence.name.startswith('smail-review-cert-safe-')
            or evidence.stat().st_uid != 0 or evidence.stat().st_mode & 0o077):
        parser.error('必须在远程项目专用且受保护的证据目录中执行')
    report_path = evidence / 'cutover-result.json'
    if report_path.exists():
        parser.error('禁止覆盖已有迁移证据')
    if args.asset_path and not re.fullmatch(r'/assets/[a-zA-Z0-9_-]+\.js', args.asset_path):
        parser.error('下载探测仅允许项目静态脚本路径')
    previous = CONFIG.read_bytes()
    if digest(previous) != args.expected_config_sha256 or CONFIG.is_symlink():
        parser.error('线上配置与已审查版本不符')
    proposed = (evidence / 'smail.conf').read_bytes()
    certificate = f'/var/lib/smail-acme-tls/certificates/{deploy.MAIL_DOMAIN}.crt'.encode()
    private_key = f'/var/lib/smail-acme-tls/certificates/{deploy.MAIL_DOMAIN}.key'.encode()
    expected = previous.replace(certificate, b'/opt/smail/ssl/custom.crt').replace(
        private_key, b'/opt/smail/ssl/custom.key')
    if proposed != expected or proposed == previous:
        parser.error('候选配置必须且只能改变 Smail 证书读取路径')
    run('nginx', '-t')
    run('systemctl', 'is-active', '--quiet', 'nginx', 'smail')
    protected = [Path('/opt/smail/smail'), Path('/opt/smail/smail-admin'),
                 Path('/opt/smail/ssl/custom.crt'), Path('/opt/smail/ssl/custom.key'),
                 Path('/usr/local/libexec/smail-acme/renew_certificate.py'),
                 Path('/usr/local/libexec/smail-acme/deploy_certificate.py'),
                 Path('/etc/systemd/system/smail-certificate-renew.service')]
    protected.extend(path for path in Path('/etc/nginx/conf.d').glob('*.conf') if path != CONFIG)
    protected_before = {str(path): digest(path.read_bytes()) for path in protected}
    before_state = state()
    backup = evidence / 'smail.conf.before'
    with backup.open('xb') as stream:
        stream.write(previous)
    backup.chmod(0o600)
    report = {'startedUTC': datetime.now(timezone.utc).isoformat(), 'status': 'running',
              'beforeConfigSHA256': digest(previous), 'afterConfigSHA256': digest(proposed),
              'beforeState': before_state, 'productionCertificateChanged': False,
              'dnsRenewalActivated': False, 'protectedBefore': protected_before, 'samples': []}
    stop = threading.Event()
    threads = []
    pending = None
    changed = False
    mode = CONFIG.stat().st_mode & 0o777

    def monitor(host, port):
        while not stop.is_set():
            sample = {'utc': datetime.now(timezone.utc).isoformat(), 'host': host, 'port': port}
            try:
                request(host, port)
                sample['ok'] = True
            except Exception as error:
                sample.update(ok=False, error=type(error).__name__)
            report['samples'].append(sample)
            stop.wait(0.2)

    try:
        scripts = Scripts()
        scripts.feed(request(deploy.VERIFY_HOSTNAMES[0], 443).decode('utf-8'))
        if not scripts.paths:
            raise RuntimeError('未找到用于既有连接测试的真实静态资源')
        asset = args.asset_path or scripts.paths[0]
        expected_body = request(deploy.VERIFY_HOSTNAMES[0], 443, asset)
        if len(expected_body) < 65536:
            raise RuntimeError('静态资源过小，无法进行本次下载连续性检查')
        for host, port in ((deploy.VERIFY_HOSTNAMES[0], 443), (deploy.CERTIFICATE_SNI, 443)):
            thread = threading.Thread(target=monitor, args=(host, port))
            thread.start()
            threads.append(thread)
        time.sleep(2)
        if len(report['samples']) < 4 or any(not item['ok'] for item in report['samples']):
            raise RuntimeError('迁移前连续探测未通过')
        secure, response = request(deploy.VERIFY_HOSTNAMES[0], 443, asset, slow=True)
        pending = (secure, response)
        prefix = response.read(1024)
        report['connectionOpenedBeforeReload'] = True
        changed = True
        atomic_config(proposed, mode)
        run('nginx', '-t')
        run('systemctl', 'reload', 'nginx')
        report['reloadedUTC'] = datetime.now(timezone.utc).isoformat()
        body = prefix + response.read(LIMIT + 1)
        if body != expected_body:
            raise RuntimeError('重载前已建立的 HTTPS 下载未完整完成')
        report['download'] = {'path': asset, 'bytes': len(body), 'sha256': digest(body), 'completed': True}
        time.sleep(4)
        stop.set()
        for thread in threads:
            thread.join(timeout=5)
        if any(thread.is_alive() for thread in threads):
            raise RuntimeError('连续探测未在规定时间内结束')
        after_state = state()
        report['afterState'] = after_state
        if after_state != before_state:
            raise RuntimeError('迁移期间主服务进程或运行状态发生变化')
        if any(digest(path.read_bytes()) != protected_before[str(path)] for path in protected):
            raise RuntimeError('迁移期间保护范围内的文件发生变化')
        if any(not item['ok'] for item in report['samples']):
            raise RuntimeError('迁移期间连续 HTTPS 探测失败')
        report['protectedFilesUnchanged'] = True
        report['status'] = 'passed'
    except BaseException as error:
        report['status'] = 'failed'
        report['errorType'] = type(error).__name__
        if changed:
            atomic_config(previous, mode)
            run('nginx', '-t')
            run('systemctl', 'reload', 'nginx')
            report['configRolledBack'] = True
        raise
    finally:
        stop.set()
        for thread in threads:
            thread.join(timeout=5)
        if pending is not None:
            pending[1].close()
            pending[0].close()
        report['finishedUTC'] = datetime.now(timezone.utc).isoformat()
        report_path.write_text(json.dumps(report, indent=2) + '\n', encoding='utf-8')
        report_path.chmod(0o600)
    print(json.dumps({key: value for key, value in report.items() if key != 'samples'}, indent=2))
    print('HTTPS 探测次数=' + str(len(report['samples'])))


if __name__ == '__main__':
    with deploy.certificate_lock():
        main()
