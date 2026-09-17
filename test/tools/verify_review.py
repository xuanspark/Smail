"""只读核对远程分批测试覆盖及线上服务，不输出邮件、密码或会话内容。"""

import argparse
import hashlib
import json
import os
from pathlib import Path
import sqlite3
import stat
import subprocess


def run(*args):
    return subprocess.check_output(args, text=True).strip()


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--results', nargs='+', default=[])
    parser.add_argument('--main-sha')
    parser.add_argument('--admin-sha')
    parser.add_argument('--since', required=True)
    args = parser.parse_args()
    assert os.geteuid() == 0 and run('hostname') == 'ikun2'
    root = Path('/opt/smail')
    assert root.resolve() == root
    summary = {}
    if args.results:
        test_root = Path('/var/tmp/smail-review-full-20260917')
        expected = {item['directory'] for item in json.loads((test_root / 'manifest.json').read_text())}
        completed = {}
        for name in args.results:
            directory = (test_root / name).resolve(strict=True)
            assert directory.parent == test_root and directory.name.startswith('results-')
            for item in json.loads((directory / 'results.json').read_text()):
                assert item.get('code') == 0 and not item.get('failed'), '测试未通过或被中断'
                assert item['directory'] not in completed, '测试包结果重复'
                completed[item['directory']] = item
        assert set(completed) == expected, '测试包覆盖不完整'
        summary['tests'] = {
            'packages': len(completed),
            'passed': sum(item['passed'] for item in completed.values()),
            'skipped': {name: item['skipped'] for name, item in completed.items() if item['skipped']},
            'per_test': [name for name, item in completed.items() if item.get('per_test')],
            'evidence': args.results,
        }
    services = {}
    for name in ('smail.service', 'mysql.service', 'Scert.service'):
        properties = run('systemctl', 'show', name, '--property=ActiveState,SubState,MainPID,NRestarts,ExecMainStatus')
        services[name] = dict(line.split('=', 1) for line in properties.splitlines())
        assert services[name]['ActiveState'] == 'active'
    summary['services'] = services
    for name, checksum in (('smail', args.main_sha), ('smail-admin', args.admin_sha)):
        if checksum:
            with (root / name).open('rb') as stream:
                actual = hashlib.file_digest(stream, 'sha256').hexdigest()
            assert actual == checksum, '部署产物摘要不匹配'
            summary[name + '_sha256'] = actual
    summary['admin_version'] = run(str(root / 'smail-admin'), 'version')
    summary['database_integrity'] = {}
    for name in ('smail.db', 'smail-state.db'):
        with sqlite3.connect(f'file:{root / "data" / name}?mode=ro', uri=True) as database:
            integrity = database.execute('PRAGMA integrity_check').fetchall()
            assert integrity == [('ok',)]
            summary['database_integrity'][name] = 'ok'
    with sqlite3.connect(f'file:{root / "data/smail.db"}?mode=ro', uri=True) as database:
        summary['migration'] = database.execute('SELECT MAX(version) FROM schema_migration').fetchone()[0]
        summary['queue_states'] = dict(database.execute('SELECT state,COUNT(*) FROM outbound_delivery GROUP BY state'))
    control = Path('/run/smail/admin-control.sock').lstat()
    assert stat.S_ISSOCK(control.st_mode) and stat.S_IMODE(control.st_mode) == 0o600
    assert control.st_uid == (root / 'data/smail.db').stat().st_uid
    summary['control_socket_mode'] = oct(stat.S_IMODE(control.st_mode))
    curl = ['curl', '--silent', '--show-error', '--fail', '--max-time', '10', '--noproxy', '*',
            '--cacert', str(root / 'ssl/custom.crt'), '--resolve', 'mail.solit.cn:443:127.0.0.1']
    run(*curl, 'https://mail.solit.cn/api/ping')
    summary['unauthenticated'] = {}
    for endpoint in ('/api/v1/user/info', '/api/v1/email/list'):
        response = json.loads(run(*curl, '-X', 'POST', 'https://mail.solit.cn' + endpoint))
        assert response['errorNo'] == 403 and not response.get('data')
        summary['unauthenticated'][endpoint] = response['errorNo']
    entries = run('journalctl', '-u', 'smail.service', '--since', args.since, '--output=json', '--no-pager')
    levels = {}
    for line in entries.splitlines():
        entry = json.loads(line)
        priority = entry.get('PRIORITY', 'unknown')
        levels[priority] = levels.get(priority, 0) + 1
    summary['journal_priorities'] = levels
    kernel = run('journalctl', '-k', '--since', args.since, '--no-pager')
    summary['oom_events'] = sum('oom-kill:constraint=' in line for line in kernel.splitlines())
    summary['global_oom_events'] = sum('oom-kill:constraint=CONSTRAINT_NONE' in line for line in kernel.splitlines())
    print(json.dumps(summary, ensure_ascii=False, indent=2))


if __name__ == '__main__':
    main()
