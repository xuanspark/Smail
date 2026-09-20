"""在隔离 SQLite 实例复制事务未提交时终止进程，保留残留目标并验证恢复。"""

import argparse
from contextlib import closing
from email.message import EmailMessage
import hashlib
import http.client
import json
import os
from pathlib import Path
import re
import shutil
import sqlite3
import subprocess
import sys
import threading
import time

from runtime_migration import (
    clear_pending_restart, database_fingerprint, login, read_database,
    root_database, run_probe, service_state,
)
from runtime_probe import ACCOUNT, ADDRESS, smtp_connect, verify_protocol_read


def assert_scope(root, unit):
    assert root.parent.parent == Path('/var/tmp')
    assert re.fullmatch(r'smail-review-runtime-\d{8}-\d{6}', root.parent.name)
    assert root.name == 'instance' and root.resolve(strict=True) == root
    assert unit == root.parent.name.replace('smail-review-runtime-', 'smail-runtime-review-')
    assert (root / 'data').resolve(strict=True) == root / 'data'


def seed_source(root):
    path = root / 'data/smail.db'
    assert path.resolve(strict=True) == path and path.is_file()
    # 使用本轮服务身份和独占写事务扩充已有邮件表，不新增结构，不连接正式数据库。
    with closing(sqlite3.connect(path.as_uri() + '?mode=rw', uri=True)) as database:
        database.execute('BEGIN IMMEDIATE')
        columns = [row[1] for row in database.execute('PRAGMA table_info(email)')]
        assert all(re.fullmatch(r'[A-Za-z_][A-Za-z0-9_]*', name) for name in columns)
        sample = database.execute('SELECT * FROM email ORDER BY id LIMIT 1').fetchone()
        assert sample and {'id', 'msg_id', 'text'}.issubset(columns)
        before = database.execute('SELECT COUNT(*) FROM email').fetchone()[0]
        statement = ('INSERT INTO email (' + ','.join('"' + name + '"' for name in columns)
                     + ') VALUES (' + ','.join('?' for _ in columns) + ')')
        for index in range(1024):
            row = list(sample)
            row[columns.index('id')] = None
            row[columns.index('msg_id')] = 'interruption-fill-' + str(index) + '@runtime.invalid.internal'
            row[columns.index('text')] = 'x' * 16384
            database.execute(statement, row)
        database.commit()
        assert database.execute('SELECT COUNT(*) FROM email').fetchone()[0] == before + 1024
    return {'added_rows': 1024, 'text_bytes_per_row': 16384, 'database_bytes': path.stat().st_size}


def assert_copy_uncommitted(root):
    assert root_database(root) == 'smail.db'
    target = root / 'data/interrupted.db'
    journal = root / 'data/interrupted.db-journal'
    assert target.stat().st_size > 2 * 1024 * 1024 and journal.stat().st_size > 512
    assert target.stat().st_size < (root / 'data/smail.db').stat().st_size
    # 页面溢写可能取得排他锁；独立读取被锁定或只见零行，均不能看到已提交复制。
    committed_rows = None
    target_locked = False
    with closing(sqlite3.connect(target.as_uri() + '?mode=ro', uri=True, timeout=0)) as database:
        try:
            committed_rows = database.execute('SELECT COUNT(*) FROM email').fetchone()[0]
            assert committed_rows == 0
        except sqlite3.OperationalError as error:
            assert 'locked' in str(error).lower()
            target_locked = True
    with closing(sqlite3.connect((root / 'data/smail.db').as_uri() + '?mode=rw', uri=True, timeout=0)) as database:
        try:
            database.execute('BEGIN IMMEDIATE')
        except sqlite3.OperationalError as error:
            assert 'locked' in str(error).lower()
        else:
            database.rollback()
            raise AssertionError('复制期间源库没有写锁')
    return {'target_bytes': target.stat().st_size, 'journal_bytes': journal.stat().st_size,
            'committed_target_mail_rows': committed_rows, 'target_read_locked': target_locked,
            'source_write_locked': True}


def prepare(root, password):
    client, _ = login(root, password)
    generation = clear_pending_restart(client)
    preview = client.success('/api/v1/database/migrations/preview', {
        'expectedGeneration': generation, 'target': {'type': 'sqlite', 'localRef': 'interrupted.db'},
    })
    try:
        client.request('/api/v1/database/migrations', {
            'id': preview['migration']['id'], 'token': preview['token'],
        })
    except (OSError, http.client.HTTPException):
        return {'request_interrupted': True}
    raise AssertionError('请求没有被进程终止中断，不能算作中途故障验收')


def assert_occupied_target(status, payload):
    assert status == 409 and isinstance(payload, dict) and payload.get('errorNo') != 0
    data = payload.get('data')
    assert isinstance(data, dict) and data.get('code') == 'DATABASE_MIGRATION_TARGET_EXISTS'
    return {'status': status, 'code': data['code']}


def verify_recovery(root, password):
    client, context = login(root, password)
    assert root_database(root) == 'smail.db'
    status = client.success('/api/v1/config/status', method='GET')
    assert not status['pendingRestart']
    retained = {path.name: hashlib.sha256(path.read_bytes()).hexdigest()
                for path in (root / 'data').glob('interrupted.db*')}
    denied, body, _ = client.request('/api/v1/database/migrations/preview', {
        'expectedGeneration': status['activeGeneration'],
        'target': {'type': 'sqlite', 'localRef': 'interrupted.db'},
    })
    rejection = assert_occupied_target(denied, body)
    assert retained == {path.name: hashlib.sha256(path.read_bytes()).hexdigest()
                        for path in (root / 'data').glob('interrupted.db*')}
    codes = {protocol: client.success('/api/v1/security/credentials/protocols/generate',
             {'protocol': protocol})['code'] for protocol in ('smtp', 'imap', 'pop3')}
    original = json.loads((root.parent / 'probe-result.json').read_text())['smtp']['message_id']
    content = '内部25端口真实收信，检查中文正文和附件。'
    attachment = '隔离验收附件内容'.encode('utf-8')
    historical = verify_protocol_read(context, codes, original, content, attachment)
    message_id = original + '.interruption'
    message = EmailMessage()
    message['From'], message['To'] = 'interruption@sender.invalid', ADDRESS
    message['Subject'] = '迁移中断恢复后的中文邮件验收'
    message['Message-ID'] = '<' + message_id + '>'
    message.set_content(content)
    message.add_attachment(attachment, maintype='text', subtype='plain', filename='验收附件.txt')
    with smtp_connect(context) as smtp:
        assert not smtp.send_message(message)
    received = verify_protocol_read(context, codes, message_id, content, attachment)
    with read_database(root / 'data/smail.db') as database:
        assert database.execute('SELECT COUNT(*) FROM email WHERE msg_id=?', (message_id,)).fetchone()[0] == 1
        assert database.execute('SELECT COUNT(*) FROM config_records').fetchone()[0] > 0
    return {'login': True, 'occupied_target_rejected': rejection, 'retained_target_unchanged': True,
            'historical_mail': historical,
            'new_mail': received, 'source_accepts_writes': True}


def verify_interruption(root, unit, password, result):
    assert_scope(root, unit)
    details = result['interruption'] = {'passed': False}
    before = service_state(unit)
    details['seed'] = run_probe(root, unit, password, 'interrupt-seed', probe_script='runtime_interruption.py')
    active = service_state(unit)
    assert active['MainPID'] != '0' and active['MainPID'] == before['MainPID']
    assert Path('/proc/' + active['MainPID'] + '/root/opt/smail/smail').stat().st_ino == (root / 'smail').stat().st_ino
    outcome = {}

    def request():
        try:
            outcome['probe'] = run_probe(root, unit, password, 'interrupt-copy', probe_script='runtime_interruption.py')
        except Exception as error:
            outcome['error'] = type(error).__name__

    thread = threading.Thread(target=request, daemon=True)
    thread.start()
    frozen = False
    try:
        target = root / 'data/interrupted.db'
        journal = root / 'data/interrupted.db-journal'
        for _ in range(15000):
            if target.exists() and target.stat().st_size > 2 * 1024 * 1024 and journal.exists() and journal.stat().st_size > 512:
                subprocess.run(['systemctl', 'kill', '--kill-whom=all', '--signal=SIGSTOP', unit], check=True, timeout=15)
                frozen = True
                break
            assert thread.is_alive(), '复制请求提前退出，未注入中断'
            time.sleep(0.005)
        else:
            raise AssertionError('没有捕捉到未提交复制阶段，不能把普通重启算作中断')
        details['copy'] = assert_copy_uncommitted(root)
        digest = database_fingerprint(root / 'data/smail.db')
        config_digest = hashlib.sha256((root / 'config/config.json').read_bytes()).hexdigest()
        snapshot = root.parent / 'interruption-snapshot'
        snapshot.mkdir(mode=0o700)
        for path in (target, journal):
            shutil.copy2(path, snapshot / path.name)
        details['snapshot_sha256'] = {path.name: hashlib.sha256(path.read_bytes()).hexdigest() for path in snapshot.iterdir()}
        subprocess.run(['systemctl', 'kill', '--kill-whom=all', '--signal=SIGKILL', unit], check=True, timeout=15)
        frozen = False
        for _ in range(100):
            failed = service_state(unit)
            if failed['MainPID'] == '0':
                break
            time.sleep(0.1)
        assert failed['MainPID'] == '0' and failed['ExecMainStatus'] == '9'
        thread.join(timeout=30)
        assert not thread.is_alive() and 'error' not in outcome and outcome['probe']['request_interrupted']
        assert database_fingerprint(root / 'data/smail.db') == digest
        assert hashlib.sha256((root / 'config/config.json').read_bytes()).hexdigest() == config_digest
        details.update(source_unchanged=True, configuration_unchanged=True, killed_state=failed)
    finally:
        if frozen:
            subprocess.run(['systemctl', 'kill', '--kill-whom=all', '--signal=SIGCONT', unit], check=True, timeout=15)
        thread.join(timeout=90)
    subprocess.run(['systemctl', 'start', unit], check=True, timeout=45)
    details['recovery'] = run_probe(root, unit, password, 'interrupt-recovery', probe_script='runtime_interruption.py')
    restored = service_state(unit)
    assert restored['MainPID'] not in ('0', active['MainPID'])
    assert restored['InvocationID'] != active['InvocationID']
    details.update(before=active, recovered_state=restored, passed=True)
    (root.parent / 'interruption-result.json').write_text(json.dumps(details, ensure_ascii=False, indent=2), encoding='utf-8')


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('root', type=Path)
    parser.add_argument('phase', choices=('interrupt-seed', 'interrupt-copy', 'interrupt-recovery'))
    args = parser.parse_args()
    assert os.geteuid() != 0
    password = sys.stdin.readline().strip()
    assert len(password) == 12
    if args.phase == 'interrupt-seed':
        result = seed_source(args.root)
    elif args.phase == 'interrupt-copy':
        result = prepare(args.root, password)
    else:
        result = verify_recovery(args.root, password)
    result['passed'] = True
    (args.root.parent / ('migration-' + args.phase + '.json')).write_text(json.dumps(result, ensure_ascii=False, indent=2), encoding='utf-8')
    print(json.dumps(result, ensure_ascii=False))


if __name__ == '__main__':
    main()
