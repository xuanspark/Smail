#!/usr/bin/python3
"""校验证书并同步至 Smail，私钥与回退副本仅在服务器保存。"""

from datetime import datetime, timezone
import hashlib
import os
from pathlib import Path
import pwd
import shutil
import subprocess
import uuid


DOMAINS = ('solit.cn', 'mail.solit.cn', 'smtp.solit.cn', 'imap.solit.cn', 'pop.solit.cn')
LINEAGE = Path('/var/lib/smail-acme-tls/certificates')
TARGET = Path('/opt/smail/ssl')


def run(*args, **kwargs):
    return subprocess.run(args, check=True, capture_output=True, **kwargs).stdout


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


def main():
    if os.geteuid() != 0:
        raise RuntimeError('必须使用 root 执行证书同步')
    # 先验证信任链、服务域名、有效期和密钥匹配，再修改运行文件。
    for domain in DOMAINS:
        run('openssl', 'verify', '-CAfile', '/etc/ssl/certs/ca-certificates.crt',
            '-untrusted', str(LINEAGE / 'solit.cn.issuer.crt'), '-verify_hostname', domain,
            str(LINEAGE / 'solit.cn.crt'))
    run('openssl', 'x509', '-in', str(LINEAGE / 'solit.cn.crt'), '-checkend', '604800', '-noout')
    public_cert = run('openssl', 'x509', '-in', str(LINEAGE / 'solit.cn.crt'), '-pubkey', '-noout')
    public_key = run('openssl', 'pkey', '-in', str(LINEAGE / 'solit.cn.key'), '-pubout')
    if public_cert != public_key:
        raise RuntimeError('证书与私钥不匹配')
    if TARGET.is_symlink() or TARGET.resolve() != TARGET:
        raise RuntimeError('证书目标目录不能包含符号链接')
    pairs = [('solit.cn.key', 'custom.key'), ('solit.cn.crt', 'custom.crt')]
    for _, name in pairs:
        path = TARGET / name
        if path.is_symlink() or not path.is_file() or path.stat().st_nlink != 1:
            raise RuntimeError('现有证书文件类型不符合要求')
    if all((LINEAGE / source).read_bytes() == (TARGET / name).read_bytes() for source, name in pairs):
        print('证书校验通过，运行文件已同步')
        return
    identity = pwd.getpwnam('smail')
    stamp = datetime.now(timezone.utc).strftime('%Y%m%dT%H%M%S') + '-' + uuid.uuid4().hex[:8]
    backup = Path('/var/backups') / ('smail-certificate-' + stamp)
    backup.mkdir(mode=0o700)
    for _, name in pairs:
        path = TARGET / name
        if path.is_symlink() or not path.is_file():
            raise RuntimeError('现有证书文件类型不符合要求')
        shutil.copy2(path, backup / name)
        os.chmod(backup / name, 0o600)
    try:
        for source, name in pairs:
            replace_file(TARGET / name, (LINEAGE / source).read_bytes(), identity.pw_uid, identity.pw_gid)
        run('nginx', '-t')
        run('systemctl', 'reload', 'nginx')
    except Exception:
        for _, name in pairs:
            replace_file(TARGET / name, (backup / name).read_bytes(), identity.pw_uid, identity.pw_gid)
        raise
    fingerprint = hashlib.sha256(run('openssl', 'x509', '-in', str(TARGET / 'custom.crt'), '-outform', 'DER')).hexdigest()
    print('证书已同步，SHA256=' + fingerprint)
    print('回退副本=' + str(backup))


if __name__ == '__main__':
    main()
