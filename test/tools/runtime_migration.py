"""通过隔离实机验证 SQLite 迁移、两种重启及目标不可读时的恢复。"""

import argparse
from contextlib import closing
from email.message import EmailMessage
import hashlib
import json
import os
from pathlib import Path
import re
import sqlite3
import ssl
import stat
import subprocess
import sys
import time

from runtime_probe import ACCOUNT, ADDRESS, Client, protocol_operation, smtp_connect, verify_protocol_read


def read_database(path):
    # 只读打开，缺失数据库必须失败，不能由验收器创建来掩盖问题。
    return closing(sqlite3.connect(path.resolve().as_uri() + '?mode=ro', uri=True, timeout=5))


def database_fingerprint(path):
    with read_database(path) as database:
        assert database.execute('PRAGMA integrity_check').fetchone() == ('ok',)
        rows = list(database.iterdump())
    return hashlib.sha256('\n'.join(rows).encode()).hexdigest()


def verify_history_preserved(root):
    query = 'SELECT kind, id, payload, digest FROM config_records'
    with read_database(root / 'data/smail.db') as source:
        old = set(source.execute(query))
    with read_database(root / 'data/migrated.db') as target:
        current = set(target.execute(query))
    assert old and old.issubset(current), '源库配置历史没有完整保留在目标库'
    return {'source_records': len(old), 'target_records': len(current), 'all_source_records_equal': True}


def root_database(root):
    data = json.loads((root / 'config/config.json').read_text())
    database = data['database']
    assert database['type'] == 'sqlite'
    assert database['localRef'] in ('smail.db', 'migrated.db')
    return database['localRef']


def migration_integrity(root, interrupted=False):
    active = root_database(root)
    assert active == 'migrated.db'
    names = sorted(path.name for path in (root / 'data').glob('*.db'))
    expected = ['migrated.db', 'occupied.db', 'smail.db']
    if interrupted:
        expected.append('interrupted.db')
    assert names == sorted(expected), '发现意外数据库文件'
    with read_database(root / 'data' / active) as database:
        assert database.execute('PRAGMA integrity_check').fetchone() == ('ok',)
        count = database.execute('SELECT COUNT(*) FROM config_records').fetchone()[0]
        assert count > 0
    return {'active_database': active, 'config_records': count,
            'retained_offline_files': sorted(set(expected) - {active}), 'integrity': 'ok'}


def wait_ready(client, generation=None):
    for _ in range(70):
        try:
            if generation is None:
                assert client.request('/api/ping', method='GET')[0] == 200
            else:
                status = client.success('/api/v1/config/status', method='GET')
                assert status['activeGeneration'] == generation and not status['pendingRestart']
            return
        except (OSError, AssertionError):
            time.sleep(1)
    raise AssertionError('迁移后的应用服务未就绪')


def login(root, password):
    context = ssl.create_default_context(cafile=str(root / 'ssl/runtime.crt'))
    assert context.check_hostname and context.verify_mode == ssl.CERT_REQUIRED
    client = Client(context)
    wait_ready(client)
    assert client.success('/api/v1/login', {'account': ACCOUNT, 'password': password})['account'] == ACCOUNT
    client.success('/api/v1/security/reauth', {'password': password})
    return client, context


def clear_pending_restart(client):
    status = client.success('/api/v1/config/status', method='GET')
    if status['pendingRestart']:
        assert client.success('/api/v1/system/restart')['status'] == 'accepted'
        wait_ready(client, status['activeGeneration'])
    return status['activeGeneration']


def prepare(root, password):
    client, _ = login(root, password)
    generation = clear_pending_restart(client)
    endpoint = '/api/v1/database/migrations'
    # 预览时目标不存在；执行前故意占用，只能拒绝，不能覆盖已有文件。
    preview = client.success(endpoint + '/preview', {
        'expectedGeneration': generation, 'target': {'type': 'sqlite', 'localRef': 'occupied.db'},
    })
    occupied = root / 'data/occupied.db'
    with occupied.open('xb') as stream:
        stream.write(b'smail-migration-occupied-target\n')
    occupied.chmod(0o600)
    original = occupied.read_bytes()
    status, rejected, _ = client.request(endpoint, {
        'id': preview['migration']['id'], 'token': preview['token'],
    })
    assert status == 409 and rejected is not None and rejected['errorNo'] != 0
    assert rejected['data']['code'] == 'DATABASE_MIGRATION_TARGET_EXISTS'
    assert occupied.read_bytes() == original and root_database(root) == 'smail.db'
    failed = client.success(endpoint + '/' + preview['migration']['id'], method='GET')
    assert failed['status'] == 'failed' and failed['errorCode'] == 'TARGET_EXISTS'
    assert client.success('/api/v1/user/info')['account'] == ACCOUNT
    # 写入实际配置，证明失败后没有卡在只读维护状态。
    from runtime_probe import verify_config_history
    verify_config_history(client)
    generation = clear_pending_restart(client)
    preview = client.success(endpoint + '/preview', {
        'expectedGeneration': generation, 'target': {'type': 'sqlite', 'localRef': 'migrated.db'},
    })
    record = client.success(endpoint, {'id': preview['migration']['id'], 'token': preview['token']})
    assert record['status'] == 'succeeded'
    assert root_database(root) == 'migrated.db'
    pending = client.success('/api/v1/config/status', method='GET')
    assert pending['activeGeneration'] > generation and pending['pendingRestart']
    # 成功切换到新配置但尚未重启时，不允许继续写业务。
    denied_status, denied, _ = client.request('/api/v1/security/credentials/protocols/generate', {'protocol': 'imap'})
    assert denied_status == 503 and denied is not None and denied['data'] == 'DATABASE_RESTART_REQUIRED'
    assert client.success('/api/v1/system/restart')['status'] == 'accepted'
    wait_ready(client, pending['activeGeneration'])
    assert client.success('/api/v1/user/info')['account'] == ACCOUNT
    return {'occupied_target_rejected': True, 'occupied_target_unchanged': True,
            'failed_record': failed['status'], 'failure_releases_maintenance': True,
            'switch_succeeded': True, 'pending_write_denied': True,
            'internal_restart_completed': True, 'existing_session_preserved': True,
            'generation': pending['activeGeneration'], 'rejection_http_status': status}


def verify_mail(root, password, phase):
    client, context = login(root, password)
    assert root_database(root) == 'migrated.db'
    assert client.success('/api/v1/user/info')['account'] == ACCOUNT
    status = client.success('/api/v1/config/status', method='GET')
    assert not status['pendingRestart']
    codes = {}
    for protocol in ('smtp', 'imap', 'pop3'):
        codes[protocol] = client.success('/api/v1/security/credentials/protocols/generate', {'protocol': protocol})['code']
        assert protocol_operation(protocol, context, codes[protocol])() == 'success'
    original = json.loads((root.parent / 'probe-result.json').read_text())['smtp']['message_id']
    body = '内部25端口真实收信，检查中文正文和附件。'
    attachment = '隔离验收附件内容'.encode('utf-8')
    historical = verify_protocol_read(context, codes, original, body, attachment)
    message_id = original + '.' + phase
    message = EmailMessage()
    message['From'], message['To'] = 'migration@sender.invalid', ADDRESS
    message['Subject'] = '切库重启后的中文邮件验收'
    message['Message-ID'] = '<' + message_id + '>'
    message.set_content(body)
    message.add_attachment(attachment, maintype='text', subtype='plain', filename='验收附件.txt')
    with smtp_connect(context) as smtp:
        assert not smtp.send_message(message)
    received = verify_protocol_read(context, codes, message_id, body, attachment)
    with read_database(root / 'data/migrated.db') as database:
        assert database.execute('SELECT COUNT(*) FROM email WHERE msg_id=?', (message_id,)).fetchone()[0] == 1
        assert database.execute('SELECT COUNT(*) FROM config_records').fetchone()[0] > 0
    with read_database(root / 'data/smail.db') as database:
        assert database.execute('SELECT COUNT(*) FROM email WHERE msg_id=?', (message_id,)).fetchone()[0] == 0
    return {'login': True, 'session': True, 'historical_mail': historical,
            'new_mail': received, 'new_mail_only_in_target': True, 'message_id': message_id}


def service_state(unit):
    assert re.fullmatch(r'smail-runtime-review-\d{8}-\d{6}', unit)
    output = subprocess.check_output(['systemctl', 'show', unit,
        '--property=MainPID,InvocationID,ActiveState,SubState,Result,ExecMainStatus'], text=True, timeout=15)
    return dict(line.split('=', 1) for line in output.splitlines())


def process_uses_target(root, state):
    pid = state['MainPID']
    assert pid.isdigit() and int(pid) > 0
    assert Path('/proc/' + pid + '/root/opt/smail/smail').stat().st_ino == (root / 'smail').stat().st_ino
    paths = []
    for item in Path('/proc/' + pid + '/fd').iterdir():
        try:
            paths.append(str(item.readlink()))
        except FileNotFoundError:
            pass
    assert '/opt/smail/data/migrated.db' in paths, '进程未持有迁移目标库'
    assert '/opt/smail/data/smail.db' not in paths, '进程仍然打开旧业务库'


def run_probe(root, unit, password, phase, sql_settings=None, probe_script=None):
    state = service_state(unit)
    pid = state['MainPID']
    assert pid.isdigit() and int(pid) > 0
    assert Path('/proc/' + pid + '/ns/net').readlink() != Path('/proc/self/ns/net').readlink()
    probe = 'smail-migration-probe-' + unit.removeprefix('smail-runtime-review-') + '-' + phase
    script = 'runtime_sql_migration.py' if sql_settings else 'runtime_migration.py'
    if probe_script is not None:
        assert ((probe_script == 'runtime_interruption.py' and sql_settings is None)
                or (probe_script in ('runtime_mixed_migration.py', 'runtime_sql_interruption.py',
                                     'runtime_sql_switch_fault.py')
                    and sql_settings is not None))
        script = probe_script
    command = ['systemd-run', '--wait', '--pipe', '--unit=' + probe,
        '--property=User=smail', '--property=Group=smail',
        '--property=NetworkNamespacePath=/proc/' + pid + '/ns/net',
        '--property=ProtectSystem=strict', '--property=ProtectHome=yes',
        '--property=NoNewPrivileges=yes', '--property=InaccessiblePaths=/opt/smail /run/smail',
        '--property=ReadWritePaths=' + str(root.parent), '--property=MemoryMax=48M',
        '--property=MemorySwapMax=0', '--property=CPUQuota=25%', '--property=TasksMax=32',
        '--property=RuntimeMaxSec=150s', '--property=KillMode=control-group',
        '--property=TimeoutStopSec=10s', 'python3', str(root.parent / script),
        str(root), phase]
    try:
        data = password + '\n' + (json.dumps(sql_settings) + '\n' if sql_settings else '')
        completed = subprocess.run(command, input=data, text=True,
            stdout=subprocess.PIPE, stderr=subprocess.STDOUT, timeout=175)
        (root.parent / ('migration-' + phase + '.log')).write_text(completed.stdout, encoding='utf-8')
        assert completed.returncode == 0, '迁移探针失败，请检查独立阶段日志'
        return json.loads((root.parent / ('migration-' + phase + '.json')).read_text())
    finally:
        # 瞬时单元可能已经回收；只有确认无进程且不活动才接受未加载返回值。
        stopped = subprocess.run(['systemctl', 'stop', probe], capture_output=True, text=True, timeout=25)
        output = subprocess.check_output(['systemctl', 'show', probe,
            '--property=MainPID,ActiveState,LoadState'], text=True, timeout=15)
        values = dict(line.split('=', 1) for line in output.splitlines())
        assert values['MainPID'] == '0' and values['ActiveState'] in ('inactive', 'failed')
        assert stopped.returncode == 0 or (stopped.returncode == 5 and values['LoadState'] == 'not-found')


def verify_migration(root, unit, password, result):
    assert root.parent.parent == Path('/var/tmp') and re.fullmatch(
        r'smail-review-runtime-\d{8}-\d{6}', root.parent.name)
    assert root.name == 'instance' and not root.is_symlink()
    assert root.resolve(strict=True) == root
    details = result['migration'] = {'passed': False}
    before = service_state(unit)
    details['prepare'] = run_probe(root, unit, password, 'prepare')
    internal = service_state(unit)
    assert before['MainPID'] == internal['MainPID'] and before['InvocationID'] == internal['InvocationID']
    process_uses_target(root, internal)
    details['internal_restart_same_process'] = True
    source_digest = database_fingerprint(root / 'data/smail.db')
    details['internal'] = run_probe(root, unit, password, 'internal')
    # 必须实际更换进程，不能把 API 的 accepted 或管理器 reload 当作系统重启。
    subprocess.run(['systemctl', 'restart', unit], check=True, timeout=55)
    restarted = service_state(unit)
    assert restarted['MainPID'] != internal['MainPID'] and restarted['InvocationID'] != internal['InvocationID']
    details['process'] = run_probe(root, unit, password, 'process')
    process_uses_target(root, restarted)
    details['process_states'] = {'before': before, 'internal': internal, 'restarted': restarted}
    target = root / 'data/migrated.db'
    assert target.is_file() and not target.is_symlink()
    mode = stat.S_IMODE(target.stat().st_mode)
    assert mode == 0o600
    try:
        # 只收紧本轮唯一目标文件权限，故障时绝不回写或清空正式库。
        target.chmod(0)
        subprocess.run(['systemctl', 'restart', unit], check=True, timeout=55)
        for _ in range(60):
            failed = service_state(unit)
            if failed['MainPID'] == '0' and failed['ActiveState'] == 'failed':
                break
            time.sleep(1)
        else:
            raise AssertionError('目标库不可读时没有明确停止，可能静默回退')
        assert root_database(root) == 'migrated.db'
        assert database_fingerprint(root / 'data/smail.db') == source_digest
        details['unavailable_target'] = {'failed_closed': True, 'source_unchanged': True,
                                         'configuration_preserved': True, 'state': failed}
    finally:
        target.chmod(mode)
    subprocess.run(['systemctl', 'start', unit], check=True, timeout=55)
    details['recovered'] = run_probe(root, unit, password, 'recovered')
    final = service_state(unit)
    process_uses_target(root, final)
    assert database_fingerprint(root / 'data/smail.db') == source_digest
    assert final['MainPID'] not in (before['MainPID'], restarted['MainPID'])
    details['history'] = verify_history_preserved(root)
    details.update(source_unchanged=True, target_permissions_restored=True, final_state=final, passed=True)
    (root.parent / 'migration-result.json').write_text(json.dumps(details, ensure_ascii=False, indent=2), encoding='utf-8')


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('root', type=Path)
    parser.add_argument('phase', choices=('prepare', 'internal', 'process', 'recovered'))
    args = parser.parse_args()
    assert os.geteuid() != 0, 'HTTP 与邮件探针必须使用非特权用户'
    password = sys.stdin.readline().strip()
    assert len(password) == 12
    result = prepare(args.root, password) if args.phase == 'prepare' else verify_mail(args.root, password, args.phase)
    result['passed'] = True
    (args.root.parent / ('migration-' + args.phase + '.json')).write_text(
        json.dumps(result, ensure_ascii=False, indent=2), encoding='utf-8')
    print(json.dumps(result, ensure_ascii=False, indent=2))


if __name__ == '__main__':
    main()
