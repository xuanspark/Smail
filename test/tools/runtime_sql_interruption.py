"""在随机 SQL 测试库中捕捉未提交复制，只终止本轮隔离应用。"""

import argparse
import hashlib
import http.client
import json
import os
from pathlib import Path
import subprocess
import sys
import threading
import time

from runtime_interruption import assert_occupied_target, assert_scope
from runtime_migration import clear_pending_restart, login, run_probe, service_state
from runtime_sql import SQLDatabase
from runtime_sql_migration import (
    MigrationTarget, assert_active, connection_count, fingerprint, identifier,
    target_payload, verify_mail,
)


def seed_source(source):
    schema = 'DATABASE()' if source.kind == 'mysql' else "'public'"
    columns = source.query('SELECT column_name FROM information_schema.columns WHERE table_schema=' +
                           schema + " AND table_name='email' ORDER BY ordinal_position").splitlines()
    assert {'id', 'msg_id', 'text'}.issubset(columns)
    quote = '`' if source.kind == 'mysql' else '"'
    columns = [identifier(column) for column in columns if column != 'id']
    names = ','.join(quote + column + quote for column in columns)
    values = []
    for column in columns:
        if column == 'msg_id':
            values.append("CONCAT('interruption-fill-',n.i,'@runtime.invalid.internal')")
        elif column == 'text':
            values.append("REPEAT('x',16384)")
        else:
            values.append('e.' + quote + column + quote)
    before = int(source.query('SELECT COUNT(*) FROM email'))
    assert before > 0
    numbers = ' UNION ALL '.join('SELECT ' + str(index) + ' AS i' for index in range(1024))
    source.query('INSERT INTO email (' + names + ') SELECT ' + ','.join(values) +
                 ' FROM (SELECT * FROM email ORDER BY id LIMIT 1) e CROSS JOIN (' + numbers + ') n;')
    assert int(source.query('SELECT COUNT(*) FROM email')) == before + 1024
    return {'added_rows': 1024, 'text_bytes_per_row': 16384}


def copy_observation(target):
    name, user = target.settings['name'], target.settings['user']
    if target.kind == 'mysql':
        raw = target.admin("SELECT t.trx_rows_modified FROM information_schema.innodb_trx t "
                           "JOIN information_schema.processlist p ON p.ID=t.trx_mysql_thread_id "
                           "WHERE p.USER='" + user + "' AND p.DB='" + name +
                           "' AND t.trx_rows_modified>128;")
        rows = [int(value) for value in raw.splitlines()]
        return {'open_copy_transactions': len(rows), 'modified_rows': max(rows, default=0)}
    exists = target.admin("SELECT COUNT(*) FROM pg_database WHERE datname='" + name + "';")
    if exists != '1':
        return {'open_copy_transactions': 0, 'relation_bytes': 0}
    raw = target.admin("SELECT COUNT(*) FROM pg_stat_activity a WHERE a.usename='" + user +
                       "' AND a.datname='" + name + "' AND a.xact_start IS NOT NULL "
                       "AND a.backend_xid IS NOT NULL AND EXISTS (SELECT 1 FROM pg_locks l "
                       "WHERE l.pid=a.pid AND l.locktype='relation' AND l.mode='RowExclusiveLock' "
                       "AND l.granted AND l.relation=to_regclass('public.email'));", name)
    size = target.admin("SELECT COALESCE(pg_relation_size(to_regclass('public.email')),0);", name)
    # 只接受真实分配了邮件页、持有写锁且分配了事务号的复制，不把空结构创建算成复制。
    count, size = int(raw), int(size)
    return {'open_copy_transactions': count if size > 32768 else 0,
            'relation_bytes': size}


def assert_uncommitted(target, observation):
    assert observation['open_copy_transactions'] == 1, '未捕获唯一未提交复制事务'
    assert target.query('SELECT COUNT(*) FROM email') == '0', '目标邮件已经提交，不能算未提交中断'
    assert target.query('SELECT COUNT(*) FROM config_records') == '0', '配置历史已经提交'
    return dict(observation, committed_mail_rows=0, committed_config_rows=0)


def prepare(root, password, target):
    client, _ = login(root, password)
    generation = clear_pending_restart(client)
    preview = client.success('/api/v1/database/migrations/preview', {
        'expectedGeneration': generation, 'target': target_payload(target),
    })
    try:
        client.request('/api/v1/database/migrations', {
            'id': preview['migration']['id'], 'token': preview['token'],
        })
    except (OSError, http.client.HTTPException):
        return {'request_interrupted': True}
    raise AssertionError('请求正常结束，不能当作进程中断验收')


def verify_recovery(root, password, source, target):
    client, _ = login(root, password)
    assert_active(root, source)
    status = client.success('/api/v1/config/status', method='GET')
    assert not status['pendingRestart']
    digest = fingerprint(target)
    denied, body, _ = client.request('/api/v1/database/migrations/preview', {
        'expectedGeneration': status['activeGeneration'], 'target': target_payload(target),
    })
    rejected = assert_occupied_target(denied, body)
    assert fingerprint(target) == digest
    # 中断后源库仍是唯一活动库；复用三协议验收，验证新邮件不写入失败目标。
    mail = verify_mail(root, password, 'interrupt-recovery', target, source)
    return {'occupied_target_rejected': rejected, 'retained_target_unchanged': True,
            'source_accepts_writes': True, 'mail': mail}


def verify_interruption(root, unit, password, result, source):
    assert_scope(root, unit)
    assert isinstance(source, SQLDatabase) and source.kind in ('mysql', 'postgres')
    target = MigrationTarget(source.kind)
    assert source.settings['name'] != target.settings['name']
    details = result['interruption'] = {'passed': False, 'database_type': source.kind,
                                       'retained_target_database': target.settings['name']}
    settings = {'source': source.settings, 'target': target.settings}
    frozen, thread, outcome = False, None, {}

    def probe(phase):
        return run_probe(root, unit, password, phase, settings,
                         probe_script='runtime_sql_interruption.py')

    def request():
        try:
            outcome['probe'] = probe('interrupt-copy')
        except Exception as error:
            outcome['error'] = type(error).__name__

    try:
        target.create()
        before = service_state(unit)
        details['seed'] = probe('interrupt-seed')
        active = service_state(unit)
        assert active['MainPID'] != '0' and active['MainPID'] == before['MainPID']
        assert Path('/proc/' + active['MainPID'] + '/root/opt/smail/smail').stat().st_ino == (root / 'smail').stat().st_ino
        thread = threading.Thread(target=request, daemon=True)
        thread.start()
        deadline = time.monotonic() + 90
        while time.monotonic() < deadline:
            observation = copy_observation(target)
            if observation['open_copy_transactions'] == 1:
                subprocess.run(['systemctl', 'kill', '--kill-whom=all', '--signal=SIGSTOP', unit], check=True, timeout=15)
                frozen = True
                break
            assert thread.is_alive(), '复制请求提前退出，未注入中断'
            time.sleep(0.02)
        assert frozen, '未捕获未提交事务，不能把普通重启算成中断'
        details['copy'] = assert_uncommitted(target, copy_observation(target))
        assert_active(root, source)
        source_digest = fingerprint(source)
        config_digest = hashlib.sha256((root / 'config/config.json').read_bytes()).hexdigest()
        subprocess.run(['systemctl', 'kill', '--kill-whom=all', '--signal=SIGKILL', unit], check=True, timeout=15)
        frozen = False
        for _ in range(100):
            failed = service_state(unit)
            if failed['MainPID'] == '0' and connection_count(target) == 0:
                break
            time.sleep(0.1)
        assert failed['MainPID'] == '0' and failed['ExecMainStatus'] == '9'
        assert connection_count(target) == 0
        thread.join(timeout=30)
        assert not thread.is_alive() and 'error' not in outcome and outcome['probe']['request_interrupted']
        assert fingerprint(source) == source_digest
        assert hashlib.sha256((root / 'config/config.json').read_bytes()).hexdigest() == config_digest
        assert target.query('SELECT COUNT(*) FROM email') == '0'
        assert target.query('SELECT COUNT(*) FROM config_records') == '0'
        target.revoke_creation()
        details.update(source_unchanged=True, configuration_unchanged=True,
                       target_rolled_back=True, killed_state=failed)
        subprocess.run(['systemctl', 'start', unit], check=True, timeout=45)
        details['recovery'] = probe('interrupt-recovery')
        restored = service_state(unit)
        assert restored['MainPID'] not in ('0', active['MainPID'])
        assert restored['InvocationID'] != active['InvocationID']
        assert connection_count(target) == 0
        details.update(before=active, recovered_state=restored, passed=True)
    finally:
        try:
            if frozen:
                subprocess.run(['systemctl', 'kill', '--kill-whom=all', '--signal=SIGCONT', unit], check=True, timeout=15)
            if thread:
                thread.join(timeout=90)
        finally:
            try:
                target.lock()
                details['retained_target_account_locked'] = True
            except Exception:
                details['passed'] = False
                raise
            finally:
                (root.parent / 'interruption-result.json').write_text(
                    json.dumps(details, ensure_ascii=False, indent=2), encoding='utf-8')


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('root', type=Path)
    parser.add_argument('phase', choices=('interrupt-seed', 'interrupt-copy', 'interrupt-recovery'))
    args = parser.parse_args()
    assert os.geteuid() != 0
    password = sys.stdin.readline().strip()
    assert len(password) == 12
    settings = json.loads(sys.stdin.readline())
    assert set(settings) == {'source', 'target'}
    source, target = (SQLDatabase(settings[key]) for key in ('source', 'target'))
    assert source.kind == target.kind and source.settings['name'] != target.settings['name']
    if args.phase == 'interrupt-seed':
        result = seed_source(source)
    elif args.phase == 'interrupt-copy':
        result = prepare(args.root, password, target)
    else:
        result = verify_recovery(args.root, password, source, target)
    result['passed'] = True
    (args.root.parent / ('migration-' + args.phase + '.json')).write_text(
        json.dumps(result, ensure_ascii=False, indent=2), encoding='utf-8')
    print(json.dumps(result, ensure_ascii=False))


if __name__ == '__main__':
    main()
