"""仅在明确授权的维护窗口内执行远程测试环境续期演练。"""

import argparse
from datetime import datetime, timezone
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import time
from unittest.mock import patch


SERVER = 'https://acme-staging-v02.api.letsencrypt.org/directory'
SOURCE = Path('/usr/local/libexec/smail-acme/renew_certificate.py')
LEGACY_SOURCE_SHA256 = '6d0abbe9466f8becd61b1cb3c750bfffb938d16c5a9bcae0b96d1451227d4216'
DOMAINS = {'cdis.cn', 'mail.cdis.cn', 'smtp.cdis.cn', 'imap.cdis.cn', 'pop.cdis.cn'}
RUN = subprocess.run


def run(command, **kwargs):
    kwargs.setdefault('timeout', 15)
    return RUN(command, **kwargs)


def digest(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def public_key(path, certificate=False):
    command = ['openssl', 'x509', '-in', str(path), '-pubkey', '-noout'] if certificate else [
        'openssl', 'pkey', '-in', str(path), '-pubout']
    return run(command, check=True, capture_output=True).stdout


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--staging-dir', type=Path, required=True)
    parser.add_argument('--evidence-dir', type=Path, required=True)
    parser.add_argument('--allow-nginx-maintenance', action='store_true')
    parser.add_argument('--simulate-hang', action='store_true')
    args = parser.parse_args()
    if os.geteuid() != 0 or not args.allow_nginx_maintenance:
        parser.error('必须以 root 明确授权 Nginx 维护窗口')
    stage = args.staging_dir.resolve(strict=True)
    evidence = args.evidence_dir.resolve(strict=True)
    if stage.parent != Path('/var/lib') or not stage.name.startswith('smail-acme-drill-'):
        parser.error('测试 ACME 目录必须是独立的演练副本')
    if evidence.parent != Path('/var/tmp') or not evidence.name.startswith('smail-renewal-drill-'):
        parser.error('证据目录不属于本轮演练')
    for directory in (stage, evidence):
        if directory.stat().st_uid != 0 or directory.stat().st_mode & 0o077:
            parser.error('演练目录必须归 root 所有且禁止其他用户访问')
    if (evidence / 'result.json').exists():
        parser.error('禁止覆盖已有演练结果，请使用新的证据目录')
    if digest(SOURCE) != LEGACY_SOURCE_SHA256:
        parser.error('本工具仅用于已封存的停机续期实现，不能用于 DNS 无停机续期验收')
    run(['nginx', '-t'], check=True)
    run(['systemctl', 'is-active', '--quiet', 'nginx'], check=True)

    certificate = stage / 'certificates/cdis.cn.crt'
    key = stage / 'certificates/cdis.cn.key'
    old_certificate = digest(certificate)
    old_key = digest(key)
    protected = [Path('/opt/smail/smail'), Path('/opt/smail/smail-admin'),
                 Path('/opt/smail/ssl/custom.crt'), Path('/opt/smail/ssl/custom.key'),
                 Path('/var/lib/smail-acme-tls/certificates/cdis.cn.crt'),
                 Path('/var/lib/smail-acme-tls/certificates/cdis.cn.key')]
    protected.extend(sorted(Path('/etc/nginx/conf.d').glob('*.conf')))
    before = {path: digest(path) for path in protected}
    spec = importlib.util.spec_from_file_location('production_renew', SOURCE)
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    report = {'startedUTC': datetime.now(timezone.utc).isoformat(),
              'sourceSHA256': digest(SOURCE), 'stagingOnly': True,
              'simulatedDue': True, 'productionDeploymentExercised': False,
              'mode': 'hang' if args.simulate_hang else 'renew', 'status': 'running'}
    started = None

    def persist():
        (evidence / 'result.json').write_text(json.dumps(report, indent=2) + '\n', encoding='utf-8')

    def execute(command, **kwargs):
        nonlocal started
        command = list(command)
        if '-checkend' in command:
            # 只模拟临近到期，不更改系统时间或正式证书。
            return subprocess.CompletedProcess(command, 1)
        if command == ['systemctl', 'stop', 'nginx']:
            started = time.monotonic()
            report['stopRequestedUTC'] = datetime.now(timezone.utc).isoformat()
            persist()
        if command[0].endswith('/lego'):
            if args.simulate_hang:
                report['hangEnteredUTC'] = datetime.now(timezone.utc).isoformat()
                persist()
                time.sleep(300)
                raise RuntimeError('挂起演练未被外部超时终止')
            command[1:1] = ['--server', SERVER]
            command[command.index('--days') + 1] = '91'
            command.append('--ari-disable')
        if command == ['/usr/local/libexec/smail-acme/deploy_certificate.py']:
            # 测试环境证书不能进入正式部署钩子，只核验续期产物。
            run(['systemctl', 'is-active', '--quiet', 'nginx'], check=True)
            if digest(certificate) == old_certificate or digest(key) != old_key:
                raise RuntimeError('证书未更新或没有复用原密钥')
            if public_key(certificate, True) != public_key(key):
                raise RuntimeError('测试证书与私钥不匹配')
            text = run(['openssl', 'x509', '-in', str(certificate), '-noout', '-ext',
                        'subjectAltName'], check=True, capture_output=True, text=True).stdout
            names = {part.strip()[4:] for part in text.split('\n', 1)[1].split(',')
                     if part.strip().startswith('DNS:')}
            if names != DOMAINS:
                raise RuntimeError('续期证书域名集合不符合预期')
            report['renewedCertificateSHA256'] = digest(certificate)
            report['keyReused'] = True
            report['domains'] = sorted(names)
            report['productionDeployHookBlocked'] = True
            return subprocess.CompletedProcess(command, 0)
        result = run(command, **kwargs)
        if command == ['systemctl', 'start', 'nginx']:
            report['nginxRestoredUTC'] = datetime.now(timezone.utc).isoformat()
            report['stopToStartSeconds'] = time.monotonic() - started
            persist()
        return result

    try:
        with patch.object(module, 'ROOT', stage), patch.object(module.subprocess, 'run', execute):
            module.main()
        if any(digest(path) != value for path, value in before.items()):
            raise RuntimeError('正式文件在演练期间发生变化，需独立复核')
        report['productionFilesUnchanged'] = True
        report['status'] = 'passed'
    except BaseException as error:
        report['status'] = 'failed'
        report['errorType'] = type(error).__name__
        raise
    finally:
        report['finishedUTC'] = datetime.now(timezone.utc).isoformat()
        persist()
    print(json.dumps(report, indent=2))


if __name__ == '__main__':
    main()
