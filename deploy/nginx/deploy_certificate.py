#!/usr/bin/python3
"""校验证书并同步至 Smail，私钥与回退副本仅在服务器保存。"""

from datetime import datetime, timezone
from contextlib import contextmanager
import fcntl
import hashlib
import os
from pathlib import Path
import pwd
import shlex
import socket
import ssl
import stat
import subprocess
import time
import uuid


DOMAINS = ('solit.cn', 'mail.solit.cn', 'smtp.solit.cn', 'imap.solit.cn', 'pop.solit.cn')
LINEAGE = Path('/var/lib/smail-acme-tls/certificates')
TARGET = Path('/opt/smail/ssl')
BACKUPS = Path('/var/backups')
LOCK = Path('/run/smail-certificate-renew.lock')
CA_FILE = '/etc/ssl/certs/ca-certificates.crt'
LIVE_PORTS = (443, 8002)


def run(*args, **kwargs):
    kwargs.setdefault('timeout', 15)
    return subprocess.run(args, check=True, capture_output=True, **kwargs).stdout


@contextmanager
def certificate_lock():
    fd = os.open(LOCK, os.O_RDWR | os.O_CREAT | os.O_NOFOLLOW | os.O_CLOEXEC, 0o600)
    with os.fdopen(fd, 'a') as stream:
        info = os.fstat(stream.fileno())
        if (not stat.S_ISREG(info.st_mode) or info.st_uid != 0 or info.st_nlink != 1
                or info.st_mode & 0o077):
            raise RuntimeError('证书锁文件不安全')
        fcntl.flock(stream, fcntl.LOCK_EX | fcntl.LOCK_NB)
        yield


def read_file(path, uid):
    if path.parent.resolve(strict=True) != path.parent:
        raise RuntimeError('证书目录不能包含符号链接')
    fd = os.open(path, os.O_RDONLY | os.O_NOFOLLOW | os.O_CLOEXEC | os.O_NONBLOCK)
    with os.fdopen(fd, 'rb') as stream:
        info = os.fstat(stream.fileno())
        if (not stat.S_ISREG(info.st_mode) or info.st_nlink != 1 or info.st_uid != uid
                or info.st_mode & 0o077 or info.st_size > 1024 * 1024):
            raise RuntimeError('证书文件类型、属主、权限或大小不符合要求')
        data = stream.read(1024 * 1024 + 1)
        if len(data) != info.st_size:
            raise RuntimeError('读取期间证书发生变化')
        return data


def check_nginx_paths():
    configuration = run('nginx', '-T').decode('utf-8')
    found = set()
    expected = {'ssl_certificate': TARGET / 'custom.crt',
                'ssl_certificate_key': TARGET / 'custom.key'}
    for line in configuration.splitlines():
        if not line.lstrip().startswith(('ssl_certificate ', 'ssl_certificate\t',
                                        'ssl_certificate_key ', 'ssl_certificate_key\t')):
            continue
        tokens = shlex.split(line, comments=True)
        if len(tokens) < 2 or tokens[0] not in expected:
            continue
        value = tokens[1].rstrip(';')
        if value.startswith('/var/lib/smail-acme') or value.startswith(str(LINEAGE) + '/'):
            raise RuntimeError('Nginx 仍引用签发目录，必须先迁移到已校验的线上证书路径')
        if value == str(expected[tokens[0]]):
            found.add(tokens[0])
    if found != set(expected):
        raise RuntimeError('未找到 Nginx 的 Smail 线上证书路径，拒绝发布')


def replace_file(path, data, uid, gid):
    staged = path.with_name(path.name + '.new-' + uuid.uuid4().hex)
    fd = os.open(staged, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, 'wb') as stream:
        os.fchown(stream.fileno(), uid, gid)
        stream.write(data)
        stream.flush()
        os.fsync(stream.fileno())
    os.replace(staged, path)
    fd = os.open(path.parent, os.O_RDONLY | os.O_DIRECTORY)
    try:
        os.fsync(fd)
    finally:
        os.close(fd)


def validate(directory):
    for domain in DOMAINS:
        run('openssl', 'verify', '-CAfile', CA_FILE, '-purpose', 'sslserver',
            '-untrusted', str(directory / 'candidate.issuer.crt'), '-verify_hostname', domain,
            str(directory / 'candidate.crt'))
    run('openssl', 'x509', '-in', str(directory / 'candidate.crt'), '-checkend', '604800', '-noout')
    public_cert = run('openssl', 'x509', '-in', str(directory / 'candidate.crt'), '-pubkey', '-noout')
    public_key = run('openssl', 'pkey', '-in', str(directory / 'candidate.key'), '-pubout')
    if public_cert != public_key:
        raise RuntimeError('证书与私钥不匹配')
    # 普通续期只允许复用密钥，避免两个文件替换之间出现不匹配；密钥轮换另行维护。
    previous_key = run('openssl', 'pkey', '-in', str(directory / 'previous.key'), '-pubout')
    previous_cert = run('openssl', 'x509', '-in', str(directory / 'previous.crt'), '-pubkey', '-noout')
    if public_key != previous_key or previous_key != previous_cert:
        raise RuntimeError('无停机续期必须复用现有密钥，密钥轮换需要单独流程')


def verify_live_certificate(fingerprint):
    context = ssl.create_default_context(cafile=CA_FILE)
    for port in LIVE_PORTS:
        deadline = time.monotonic() + 12
        while True:
            try:
                with socket.create_connection(('127.0.0.1', port), timeout=2) as connection:
                    with context.wrap_socket(connection, server_hostname='mail.solit.cn') as secure:
                        actual = hashlib.sha256(secure.getpeercert(binary_form=True)).hexdigest()
                if actual == fingerprint:
                    break
                raise RuntimeError('线上入口未返回预期证书')
            except (OSError, RuntimeError):
                if time.monotonic() >= deadline:
                    raise RuntimeError('线上证书验证失败，端口=' + str(port)) from None
                time.sleep(0.25)


def fingerprint(path):
    return hashlib.sha256(run('openssl', 'x509', '-in', str(path), '-outform', 'DER')).hexdigest()


def publish():
    check_nginx_paths()
    run('systemctl', 'is-active', '--quiet', 'nginx')
    identity = pwd.getpwnam('smail')
    sources = {'candidate.crt': read_file(LINEAGE / 'solit.cn.crt', 0),
               'candidate.key': read_file(LINEAGE / 'solit.cn.key', 0),
               'candidate.issuer.crt': read_file(LINEAGE / 'solit.cn.issuer.crt', 0),
               'previous.crt': read_file(TARGET / 'custom.crt', identity.pw_uid),
               'previous.key': read_file(TARGET / 'custom.key', identity.pw_uid)}
    stamp = datetime.now(timezone.utc).strftime('%Y%m%dT%H%M%S') + '-' + uuid.uuid4().hex[:8]
    backup = BACKUPS / ('smail-certificate-' + stamp)
    backup.mkdir(mode=0o700)
    print('证书校验及回退快照=' + str(backup), flush=True)
    for name, data in sources.items():
        replace_file(backup / name, data, 0, 0)
    # 校验与发布使用同一份受保护快照，不重新读取可能变化的签发目录。
    validate(backup)
    expected = fingerprint(backup / 'candidate.crt')
    changed = [suffix for suffix in ('key', 'crt')
               if sources['candidate.' + suffix] != sources['previous.' + suffix]]
    if not changed:
        try:
            verify_live_certificate(expected)
        except RuntimeError:
            # 恢复上次在文件发布后、重载前中断的任务；不能仅因文件相同就宣告成功。
            run('nginx', '-t')
            run('systemctl', 'reload', 'nginx')
            verify_live_certificate(expected)
        print('证书校验通过，线上入口与运行文件已同步')
        return
    try:
        for suffix in changed:
            replace_file(TARGET / ('custom.' + suffix), sources['candidate.' + suffix],
                         identity.pw_uid, identity.pw_gid)
        run('nginx', '-t')
        run('systemctl', 'reload', 'nginx')
        verify_live_certificate(expected)
    except Exception:
        for suffix in changed:
            replace_file(TARGET / ('custom.' + suffix), sources['previous.' + suffix],
                         identity.pw_uid, identity.pw_gid)
        run('nginx', '-t')
        run('systemctl', 'reload', 'nginx')
        verify_live_certificate(fingerprint(backup / 'previous.crt'))
        raise
    print('证书已同步，SHA256=' + expected)


def main():
    if os.geteuid() != 0:
        raise RuntimeError('必须使用 root 执行证书同步')
    with certificate_lock():
        publish()


if __name__ == '__main__':
    main()
