"""SQL 切库的真实重启与账号不可用恢复验收，只操作随机隔离库。"""

import argparse
from email.message import EmailMessage
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess
import sys
import time

from runtime_migration import clear_pending_restart, login, run_probe, service_state, wait_ready
from runtime_probe import ACCOUNT, ADDRESS, protocol_operation, smtp_connect, verify_protocol_read
from runtime_sql import SQLDatabase, SQLFixture


def identifier(value):
    assert re.fullmatch(r'[A-Za-z_][A-Za-z0-9_]*', value), '验收数据表标识不符合安全范围'
    return value


class MigrationTarget(SQLFixture):
    def create(self):
        name, user, password = (self.settings[key] for key in ('name', 'user', 'password'))
        if self.kind == 'mysql':
            self.admin("CREATE USER '" + user + "'@'localhost' IDENTIFIED BY '" + password + "';")
            self.account_created = True
            self.admin('GRANT ALL ON `' + name.replace('_', r'\_') + "`.* TO '" + user + "'@'localhost';")
            count = self.admin("SELECT COUNT(*) FROM information_schema.schemata WHERE schema_name='" + name + "';")
        else:
            # PostgreSQL 创建目标库需要临时建库权限；复制结束立即撤销，绝不授予超级用户。
            self.admin('CREATE ROLE ' + user + " LOGIN CREATEDB CONNECTION LIMIT 24 PASSWORD '" + password + "';")
            self.account_created = True
            count = self.admin("SELECT COUNT(*) FROM pg_database WHERE datname='" + name + "';")
        assert count == '0', '迁移目标必须不存在，不能复用已有库'

    def revoke_creation(self):
        if self.account_created and self.kind == 'postgres':
            user = self.settings['user']
            self.admin('ALTER ROLE ' + user + ' NOCREATEDB;')
            assert self.admin("SELECT rolcreatedb FROM pg_roles WHERE rolname='" + user + "';") == 'f'

    def lock(self):
        try:
            self.revoke_creation()
        finally:
            super().lock()

    def unlock(self):
        assert self.account_created
        user = self.settings['user']
        if self.kind == 'mysql':
            self.admin("ALTER USER '" + user + "'@'localhost' ACCOUNT UNLOCK;")
            assert self.admin("SELECT account_locked FROM mysql.user WHERE User='" + user +
                              "' AND Host='localhost';") == 'N'
        else:
            self.admin('ALTER ROLE ' + user + ' LOGIN;')
            assert self.admin("SELECT rolcanlogin FROM pg_roles WHERE rolname='" + user + "';") == 't'


def target_payload(database):
    settings = database.settings
    return {'type': 'mysql' if database.kind == 'mysql' else 'postgresql',
            'host': '127.0.0.1', 'port': database.port, 'username': settings['user'],
            'name': settings['name'], 'password': {'plaintext': settings['password']}}


def assert_active(root, target):
    database = json.loads((root / 'config/config.json').read_text())['database']
    expected = target_payload(target)
    for field in ('type', 'host', 'port', 'username', 'name'):
        assert database[field] == expected[field], '活动配置没有指向本轮唯一 SQL 目标'
    assert not list((root / 'data').rglob('*.db'))
    assert not list((root / 'data').rglob('*.sqlite*'))


def table_rows(database, table):
    table = identifier(table)
    if database.kind == 'mysql':
        columns = database.query("SELECT COLUMN_NAME FROM information_schema.columns WHERE "
                                 "table_schema=DATABASE() AND table_name='" + table + "' ORDER BY ORDINAL_POSITION")
        # 对二进制和文本统一取十六进制，保留 NULL，避免 BLOB 不能进入 JSON 的问题。
        values = ','.join('HEX(`' + identifier(column) + '`)' for column in columns.splitlines())
        assert values
        output = database.query('SELECT JSON_ARRAY(' + values + ') FROM `' + table + '`')
    else:
        output = database.query('SELECT row_to_json(t) FROM "' + table + '" t')
    return sorted(json.dumps(json.loads(line), sort_keys=True, ensure_ascii=True) for line in output.splitlines())


def fingerprint(database):
    schema = 'DATABASE()' if database.kind == 'mysql' else "'public'"
    tables = database.query('SELECT table_name FROM information_schema.tables WHERE table_schema=' +
                            schema + " AND table_type='BASE TABLE' ORDER BY table_name").splitlines()
    assert 'config_records' in tables and 'email' in tables
    content = {table: table_rows(database, table) for table in tables}
    return hashlib.sha256(json.dumps(content, sort_keys=True).encode()).hexdigest()


def history_rows(database):
    # 两个引擎统一比较四列原始字节，不能直接比较各自的 BLOB JSON 表示。
    if database.kind == 'mysql':
        values = ','.join('LOWER(HEX(`' + column + '`))' for column in ('kind', 'id', 'payload', 'digest'))
        query = 'SELECT JSON_ARRAY(' + values + ') FROM config_records'
    else:
        values = ["encode(convert_to(" + column + ",'UTF8'),'hex')" for column in ('kind', 'id')]
        values += ["encode(payload,'hex')", "encode(convert_to(digest,'UTF8'),'hex')"]
        query = 'SELECT json_build_array(' + ','.join(values) + ') FROM config_records'
    rows = []
    for line in database.query(query).splitlines():
        values = json.loads(line)
        assert isinstance(values, list) and len(values) == 4
        assert all(isinstance(value, str) and re.fullmatch(r'(?:[0-9a-f]{2})+', value) for value in values)
        payload, digest = bytes.fromhex(values[2]), bytes.fromhex(values[3]).decode('ascii')
        assert hashlib.sha256(payload).hexdigest() == digest, '配置历史载荷与摘要不一致'
        rows.append(json.dumps(values, separators=(',', ':')))
    assert len(rows) == len(set(rows)), '配置历史出现重复完整行'
    return sorted(rows)


def assert_connections(source, target):
    counts = []
    for database in (source, target):
        name, user = database.settings['name'], database.settings['user']
        if database.kind == 'mysql':
            query = "SELECT COUNT(*) FROM information_schema.processlist WHERE USER='" + user + "' AND DB='" + name + "';"
        else:
            query = "SELECT COUNT(*) FROM pg_stat_activity WHERE usename='" + user + "' AND datname='" + name + "';"
        counts.append(int(database.admin(query)))
    assert counts[0] == 0 and counts[1] > 0, '源库仍有活动连接或目标库没有应用连接'
    return {'source_connections': counts[0], 'target_connections': counts[1]}


def prepare(root, password, source, target):
    client, _ = login(root, password)
    generation = clear_pending_restart(client)
    endpoint = '/api/v1/database/migrations'
    preview = client.success(endpoint + '/preview', {
        'expectedGeneration': generation, 'target': target_payload(target),
    })
    record = client.success(endpoint, {'id': preview['migration']['id'], 'token': preview['token']})
    assert record['status'] == 'succeeded'
    assert_active(root, target)
    pending = client.success('/api/v1/config/status', method='GET')
    assert pending['activeGeneration'] > generation and pending['pendingRestart']
    status, denied, _ = client.request('/api/v1/security/credentials/protocols/generate', {'protocol': 'imap'})
    assert status == 503 and denied is not None and denied['data'] == 'DATABASE_RESTART_REQUIRED'
    assert client.success('/api/v1/system/restart')['status'] == 'accepted'
    wait_ready(client, pending['activeGeneration'])
    assert client.success('/api/v1/user/info')['account'] == ACCOUNT
    return {'switch_succeeded': True, 'pending_write_denied': True, 'internal_restart_completed': True,
            'existing_session_preserved': True, 'generation': pending['activeGeneration']}


def verify_mail(root, password, phase, source, target):
    client, context = login(root, password)
    assert_active(root, target)
    assert not client.success('/api/v1/config/status', method='GET')['pendingRestart']
    codes = {}
    for protocol in ('smtp', 'imap', 'pop3'):
        codes[protocol] = client.success('/api/v1/security/credentials/protocols/generate', {'protocol': protocol})['code']
        assert protocol_operation(protocol, context, codes[protocol])() == 'success'
    original = json.loads((root.parent / 'probe-result.json').read_text())['smtp']['message_id']
    body = '内部25端口真实收信，检查中文正文和附件。'
    attachment = '隔离验收附件内容'.encode('utf-8')
    historical = verify_protocol_read(context, codes, original, body, attachment)
    message_id = original + '.' + phase
    assert re.fullmatch(r'[a-zA-Z0-9@._-]+', message_id)
    message = EmailMessage()
    message['From'], message['To'] = 'migration@sender.invalid', ADDRESS
    message['Subject'] = 'SQL 切库重启后的中文邮件验收'
    message['Message-ID'] = '<' + message_id + '>'
    message.set_content(body)
    message.add_attachment(attachment, maintype='text', subtype='plain', filename='验收附件.txt')
    with smtp_connect(context) as smtp:
        assert not smtp.send_message(message)
    received = verify_protocol_read(context, codes, message_id, body, attachment)
    target.verify_message(message_id, ACCOUNT, body, attachment)
    assert source.query("SELECT COUNT(*) FROM email WHERE msg_id='" + message_id + "'") == '0'
    return {'login': True, 'historical_mail': historical, 'new_mail': received,
            'new_mail_only_in_target': True, 'message_id': message_id}


def verify_migration(root, unit, password, result, source, target):
    assert root.parent.parent == Path('/var/tmp') and re.fullmatch(r'smail-review-runtime-\d{8}-\d{6}', root.parent.name)
    assert root.name == 'instance' and not root.is_symlink() and root.resolve(strict=True) == root
    details = result['migration'] = {'passed': False, 'database_type': source.kind,
                                     'target_type': target.kind, 'cross_engine': source.kind != target.kind,
                                     'source_database': source.settings['name'], 'target_database': target.settings['name']}
    settings = {'source': source.settings, 'target': target.settings}
    before = service_state(unit)
    details['prepare'] = run_probe(root, unit, password, 'prepare', settings)
    target.revoke_creation()
    details['temporary_creation_privilege_revoked'] = True
    internal = service_state(unit)
    assert before['MainPID'] == internal['MainPID'] and before['InvocationID'] == internal['InvocationID']
    details['internal_restart_same_process'] = True
    details['connections_after_internal'] = assert_connections(source, target)
    source_digest = fingerprint(source)
    history = history_rows(source)
    assert history
    details['internal'] = run_probe(root, unit, password, 'internal', settings)
    subprocess.run(['systemctl', 'restart', unit], check=True, timeout=55)
    restarted = service_state(unit)
    assert restarted['MainPID'] != internal['MainPID'] and restarted['InvocationID'] != internal['InvocationID']
    details['process'] = run_probe(root, unit, password, 'process', settings)
    details['connections_after_process'] = assert_connections(source, target)
    details['process_states'] = {'before': before, 'internal': internal, 'restarted': restarted}
    try:
        # 只锁定本轮目标账号，不停止共享数据库，不撤销任何正式账号权限。
        target.lock()
        subprocess.run(['systemctl', 'restart', unit], check=True, timeout=55)
        for _ in range(70):
            failed = service_state(unit)
            if failed['MainPID'] == '0' and failed['ActiveState'] == 'failed':
                break
            time.sleep(1)
        else:
            raise AssertionError('目标账号不可用时应用没有明确停止')
        assert_active(root, target)
        assert fingerprint(source) == source_digest
        details['unavailable_target'] = {'failed_closed': True, 'source_unchanged': True,
                                         'configuration_preserved': True, 'state': failed}
    finally:
        target.unlock()
    subprocess.run(['systemctl', 'start', unit], check=True, timeout=55)
    details['recovered'] = run_probe(root, unit, password, 'recovered', settings)
    final = service_state(unit)
    assert final['MainPID'] not in (before['MainPID'], restarted['MainPID'])
    details['connections_after_recovery'] = assert_connections(source, target)
    assert fingerprint(source) == source_digest
    current = history_rows(target)
    assert set(history).issubset(current)
    details['history'] = {'source_records': len(history), 'target_records': len(current), 'all_source_records_equal': True}
    details.update(source_unchanged=True, target_account_restored=True, final_state=final, passed=True)
    (root.parent / 'migration-result.json').write_text(json.dumps(details, ensure_ascii=False, indent=2), encoding='utf-8')


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('root', type=Path)
    parser.add_argument('phase', choices=('prepare', 'internal', 'process', 'recovered'))
    args = parser.parse_args()
    assert os.geteuid() != 0
    password = sys.stdin.readline().strip()
    assert len(password) == 12
    settings = json.loads(sys.stdin.readline())
    assert set(settings) == {'source', 'target'}
    source, target = (SQLDatabase(settings[key]) for key in ('source', 'target'))
    assert source.settings['name'] != target.settings['name']
    result = (prepare(args.root, password, source, target) if args.phase == 'prepare' else
              verify_mail(args.root, password, args.phase, source, target))
    result['passed'] = True
    (args.root.parent / ('migration-' + args.phase + '.json')).write_text(
        json.dumps(result, ensure_ascii=False, indent=2), encoding='utf-8')
    print(json.dumps(result, ensure_ascii=False, indent=2))


if __name__ == '__main__':
    main()
