"""仅在隔离网络中通过正式接口验收首次安装和认证，不持久化测试密码。"""

import argparse
import base64
from concurrent.futures import ThreadPoolExecutor
from contextlib import ExitStack
from copy import deepcopy
from email.message import EmailMessage
from email.parser import BytesParser
from email.policy import default as email_policy
import http.client
from http.cookies import SimpleCookie
import imaplib
import json
from pathlib import Path
import poplib
import secrets
import smtplib
import socket
import sqlite3
import ssl
import sys
import threading
import time


HOST = 'mail.runtime.invalid'
ACCOUNT = 'reviewadmin'
ADDRESS = ACCOUNT + '@runtime.invalid'


class LocalHTTPS(http.client.HTTPSConnection):
    def connect(self):
        raw = socket.create_connection(('127.0.0.1', self.port), self.timeout, self.source_address)
        self.sock = self._context.wrap_socket(raw, server_hostname=self.host)


class Client:
    def __init__(self, context=None, port=None, source=None):
        self.context = context
        self.port = port or (443 if context else 80)
        self.source = (source, 0) if source else None
        self.cookies = SimpleCookie()

    def request(self, path, payload=None, method='POST', extra_headers=None):
        connection = (LocalHTTPS(HOST, self.port, timeout=30, context=self.context,
                                 source_address=self.source)
                      if self.context else http.client.HTTPConnection('127.0.0.1', self.port, timeout=30))
        origin = 'https://' + HOST if self.context else 'http://127.0.0.1'
        if self.port != (443 if self.context else 80):
            origin += ':' + str(self.port)
        headers = {'Origin': origin, 'Content-Type': 'application/json'}
        headers.update(extra_headers or {})
        if self.cookies:
            headers['Cookie'] = '; '.join(item.key + '=' + item.coded_value for item in self.cookies.values())
        if 'smail_csrf' in self.cookies:
            headers['X-CSRF-Token'] = self.cookies['smail_csrf'].value
        try:
            connection.request(method, path, json.dumps(payload) if payload is not None else None, headers)
            response = connection.getresponse()
            for name, value in response.getheaders():
                if name.lower() == 'set-cookie':
                    self.cookies.load(value)
            content = response.read()
            data = json.loads(content) if 'application/json' in response.getheader('Content-Type', '') else None
            return response.status, data, response.getheader('Retry-After')
        finally:
            connection.close()

    def success(self, path, payload=None, method='POST'):
        status, data, _ = self.request(path, payload, method)
        assert status == 200 and data is not None and data.get('errorNo') == 0, (
            path, status, data.get('errorNo') if data else None, data.get('errorMsg') if data else None)
        return data['data']


def copy_session(client):
    replay = Client(client.context, client.port)
    replay.cookies = deepcopy(client.cookies)
    return replay


def require_session_denied(client):
    status, data, _ = client.request('/api/v1/user/info')
    assert status == 200 and data is not None and data.get('errorNo') == 403, '旧会话未明确返回需要登录'


def verify_http_session_revocation(admin, context):
    # 普通验收账号与超管分离，不改变备份恢复所需的超管密码。
    account = 'sessionreview'
    password = secrets.token_urlsafe(9)
    new_password = secrets.token_urlsafe(9)
    admin.success('/api/v1/user/create', {
        'account': account, 'username': '隔离会话撤销验收', 'password': password,
        'disabled': 0, 'role': 'user',
    })

    def login(value):
        client = Client(context)
        identity = client.success('/api/v1/login', {'account': account, 'password': value})
        assert identity.get('account') == account and not identity.get('pending')
        assert client.success('/api/v1/user/info').get('account') == account
        return client

    first, independent = login(password), login(password)
    replay = copy_session(first)
    assert replay.success('/api/v1/user/info').get('account') == account
    first.success('/api/v1/logout')
    require_session_denied(replay)
    assert independent.success('/api/v1/user/info').get('account') == account
    result = {'logout_cookie_replay_denied': True, 'independent_session_preserved': True}

    # 此独立会话在禁用期间不发请求，避免只验证已被销毁的会话记录。
    replay_after_enable = login(password)
    admin.success('/api/v1/user/edit', {'account': account, 'disabled': 1})
    require_session_denied(independent)
    admin.success('/api/v1/user/edit', {'account': account, 'disabled': 0})
    require_session_denied(replay_after_enable)
    result.update(disabled_session_denied=True, reenabled_old_session_denied=True)

    current, other = login(password), login(password)
    replay = copy_session(current)
    current.success('/api/v1/settings/modify_password', {
        'current_password': password, 'password': new_password,
    })
    require_session_denied(replay)
    require_session_denied(other)
    status, data, _ = Client(context).request('/api/v1/login', {'account': account, 'password': password})
    assert status == 200 and data is not None and data.get('errorNo') == 100, '修改密码后旧密码未明确拒绝'
    login(new_password)
    assert admin.success('/api/v1/user/info').get('account') == ACCOUNT
    result.update(password_change_sessions_denied=True, old_password_denied=True,
                  new_password_accepted=True, admin_session_preserved=True)
    return result


def smtp_connect(context):
    client = smtplib.SMTP('127.0.0.1', 25, timeout=30)
    client.ehlo('runtime.invalid')
    assert client.has_extn('starttls') and not client.has_extn('auth')
    code, _ = client.docmd('AUTH', 'PLAIN')
    assert code in (502, 523, 538), '明文认证未被拒绝'
    client._host = HOST
    client.starttls(context=context)
    client.ehlo('runtime.invalid')
    return client


class LocalIMAP(imaplib.IMAP4_SSL):
    def _create_socket(self, timeout):
        raw = socket.create_connection(('127.0.0.1', self.port), timeout)
        return self.ssl_context.wrap_socket(raw, server_hostname=self.host)


class LocalPOP(poplib.POP3_SSL):
    def _create_socket(self, timeout):
        raw = socket.create_connection(('127.0.0.1', self.port), timeout)
        return self.context.wrap_socket(raw, server_hostname=self.host)


def verify_message_content(raw, message_id, body, attachment):
    message = BytesParser(policy=email_policy).parsebytes(raw)
    assert str(message['Message-ID']).strip('<>') == message_id
    text = message.get_body(preferencelist=('plain',))
    assert text is not None and body in text.get_content()
    files = list(message.iter_attachments())
    assert len(files) == 1 and files[0].get_filename() == '验收附件.txt'
    assert files[0].get_payload(decode=True) == attachment


def verify_config_history(client):
    # 首次安装不一定产生历史，必须通过正式变更接口写入并读回，不能只检查空表存在。
    status = client.success('/api/v1/config/status', method='GET')
    assert status['available']
    payload = client.success('/api/v1/config/base', method='GET')
    payload.pop('databasePasswordStatus', None)
    payload['logLevel'] = 'warn' if payload.get('logLevel') != 'warn' else 'info'
    request = {'expectedGeneration': status['activeGeneration'], 'payload': payload,
               'confirmation': {'confirmed': True, 'method': 'recent_auth'}}
    client.success('/api/v1/config/base/validate', request)
    candidate = client.success('/api/v1/config/base', request, 'PUT')
    client.success('/api/v1/config/base/apply', {
        'generation': candidate['generation'], 'candidateId': candidate['candidateId'],
    })
    active = client.success('/api/v1/config/base', method='GET')
    assert active['logLevel'] == payload['logLevel']
    assert active['generation'] > status['activeGeneration']
    return {'configuration_api': True, 'generation_advanced': True}


def verify_protocol_read(context, codes, message_id, body, attachment):
    # 通过协议取回真实正文，不以数据库存在或欢迎响应替代取信验收。
    client = LocalIMAP(HOST, 993, ssl_context=context, timeout=30)
    try:
        assert client.login(ADDRESS, codes['imap'])[0] == 'OK'
        assert client.select('INBOX', readonly=True)[0] == 'OK'
        status, matches = client.uid('SEARCH', None, 'HEADER', 'Message-ID', '"<' + message_id + '>"')
        assert status == 'OK' and matches[0].split(), 'IMAP 收件箱没有测试邮件'
        status, contents = client.uid('FETCH', matches[0].split()[-1], '(BODY.PEEK[])')
        assert status == 'OK'
        raw = next(item[1] for item in contents if isinstance(item, tuple))
        verify_message_content(raw, message_id, body, attachment)
    finally:
        client.logout()
    client = LocalPOP(HOST, 995, context=context, timeout=30)
    try:
        client.user(ADDRESS)
        client.pass_(codes['pop3'])
        found = False
        for item in client.list()[1]:
            raw = b'\r\n'.join(client.retr(int(item.split()[0]))[1])
            message = BytesParser(policy=email_policy).parsebytes(raw)
            if str(message['Message-ID']).strip('<>') == message_id:
                verify_message_content(raw, message_id, body, attachment)
                found = True
                break
        assert found, 'POP3 没有取到测试邮件'
    finally:
        client.quit()
    return {'imap_inbox': True, 'imap_body_attachment': True, 'pop3_body_attachment': True}


def protocol_operation(protocol, context, code):
    if protocol == 'smtp':
        client = smtp_connect(context)

        def invoke():
            try:
                status, _ = client.auth('PLAIN', lambda challenge=None: '\0' + ADDRESS + '\0' + code)
                assert status == 235
                return 'success'
            except smtplib.SMTPAuthenticationError as error:
                assert error.smtp_code == 454, ('smtp', error.smtp_code)
                assert b'Temporary authentication failure' in error.smtp_error
                return 'busy'
            finally:
                client.close()

    elif protocol == 'imap':
        client = LocalIMAP(HOST, 993, ssl_context=context, timeout=30)

        def invoke():
            try:
                status, _ = client.login(ADDRESS, code)
                assert status == 'OK'
                return 'success'
            except imaplib.IMAP4.error as error:
                assert 'UNAVAILABLE' in str(error), 'IMAP 未返回临时不可用'
                return 'busy'
            finally:
                client.shutdown()

    else:
        client = LocalPOP(HOST, 995, context=context, timeout=30)
        client.user(ADDRESS)

        def invoke():
            try:
                assert client.pass_(code).startswith(b'+OK')
                return 'success'
            except poplib.error_proto as error:
                assert '[SYS/TEMP]' in str(error), 'POP3 未返回临时不可用'
                return 'busy'
            finally:
                client.close()

    return invoke


def verify_imap_append(context, code, message_id, body, attachment):
    appended_id = 'append-' + message_id
    message = EmailMessage()
    message['From'] = ADDRESS
    message['To'] = ADDRESS
    message['Subject'] = '邮件协议追加和删除验收'
    message['Message-ID'] = '<' + appended_id + '>'
    message.set_content(body)
    message.add_attachment(attachment, maintype='text', subtype='plain', filename='验收附件.txt')
    client = LocalIMAP(HOST, 993, ssl_context=context, timeout=30)
    try:
        assert client.login(ADDRESS, code)[0] == 'OK'
        assert client.select('INBOX')[0] == 'OK'
        assert client.append('INBOX', None, None, message.as_bytes(policy=email_policy.clone(linesep='\r\n')))[0] == 'OK'
        status, values = client.response('APPENDUID')
        assert status == 'APPENDUID' and len(values) == 1 and values[0]
        validity, uid = map(int, values[0].split())
        assert 0 < validity <= 2**32 - 1 and 0 < uid <= 2**32 - 1
        assert client.noop()[0] == 'OK'
        status, contents = client.uid('FETCH', str(uid), '(BODY.PEEK[])')
        assert status == 'OK'
        raw = next(item[1] for item in contents if isinstance(item, tuple))
        verify_message_content(raw, appended_id, body, attachment)
        assert client.uid('STORE', str(uid), '+FLAGS.SILENT', r'(\Deleted)')[0] == 'OK'
        assert client.uid('EXPUNGE', str(uid))[0] == 'OK'
        status, matches = client.uid('SEARCH', None, 'HEADER', 'Message-ID', '"<' + appended_id + '>"')
        assert status == 'OK' and not matches[0].split()
    finally:
        client.logout()
    return {'append_uid': True, 'body_attachment': True, 'uid_expunge': True}


def verify_restored(root, context, password, message_id, port):
    client = Client(context, port=port)
    for _ in range(60):
        try:
            assert client.request('/api/ping', method='GET')[0] == 200
            break
        except (OSError, AssertionError):
            time.sleep(1)
    else:
        raise AssertionError('恢复实例未就绪')
    assert client.success('/api/v1/login', {'account': ACCOUNT, 'password': password})['account'] == ACCOUNT
    assert client.success('/api/v1/user/info')['account'] == ACCOUNT
    client.success('/api/v1/security/reauth', {'password': password})
    codes = {}
    for protocol in ('smtp', 'imap', 'pop3'):
        codes[protocol] = client.success('/api/v1/security/credentials/protocols/generate', {'protocol': protocol})['code']
        assert protocol_operation(protocol, context, codes[protocol])() == 'success'
    body = '内部25端口真实收信，检查中文正文和附件。'
    attachment = '隔离验收附件内容'.encode('utf-8')
    result = {'login': True, 'session': True, 'protocol_login': list(codes),
              'historical_mail': verify_protocol_read(context, codes, message_id, body, attachment)}
    message = EmailMessage()
    message['From'] = 'restore@sender.invalid'
    message['To'] = ADDRESS
    message['Subject'] = '备份恢复后中文收信验收'
    restored_id = message_id + '.restored'
    message['Message-ID'] = '<' + restored_id + '>'
    message.set_content(body)
    message.add_attachment(attachment, maintype='text', subtype='plain', filename='验收附件.txt')
    with smtp_connect(context) as smtp:
        assert not smtp.send_message(message)
    result['new_mail'] = verify_protocol_read(context, codes, restored_id, body, attachment)
    result['passed'] = True
    (root.parent / 'restore-probe-result.json').write_text(json.dumps(result, ensure_ascii=False, indent=2), encoding='utf-8')
    print(json.dumps(result, ensure_ascii=False, indent=2))


def verify_protocol_revocation(client, context, codes, revoke=None):
    # 必须保持已认证连接跨越撤销操作，重新连接失败不能代替旧连接失效验收。
    results = {}
    with ExitStack() as cleanup:
        smtp = smtp_connect(context)
        cleanup.callback(smtp.close)
        imap = LocalIMAP(HOST, 993, ssl_context=context, timeout=30)
        cleanup.callback(imap.shutdown)
        pop = LocalPOP(HOST, 995, context=context, timeout=30)
        cleanup.callback(pop.close)
        assert smtp.auth('PLAIN', lambda challenge=None: '\0' + ADDRESS + '\0' + codes['smtp'])[0] == 235
        assert imap.login(ADDRESS, codes['imap'])[0] == 'OK'
        assert imap.select('INBOX', readonly=True)[0] == 'OK'
        status, contents = imap.uid('FETCH', '1:*', '(BODY.PEEK[])')
        assert status == 'OK' and any(isinstance(item, tuple) and item[1] for item in contents)
        pop.user(ADDRESS)
        pop.pass_(codes['pop3'])
        messages = pop.list()[1]
        assert messages
        first = int(messages[0].split()[0])
        assert pop.retr(first)[0].startswith(b'+OK')
        # 不依赖外部 MX 或外发投递，直接验证旧认证连接能否开启新的邮件事务。
        assert smtp.mail(ADDRESS)[0] == 250
        assert smtp.rcpt(ADDRESS)[0] == 250
        assert smtp.rset()[0] == 250
        if revoke is None:
            for protocol in codes:
                client.success('/api/v1/security/credentials/protocols/revoke', {'protocol': protocol})
        else:
            revoke()
        try:
            code, _ = smtp.mail(ADDRESS)
            assert code in (250, 530, 535), 'SMTP 不是成功或明确的认证拒绝'
            results['smtp_existing_denied'] = code in (530, 535)
        except smtplib.SMTPServerDisconnected:
            results['smtp_existing_denied'] = True

        try:
            status, _ = imap.uid('FETCH', '1:*', '(BODY.PEEK[])')
            results['imap_existing_denied'] = status in ('NO', 'BAD')
        except imaplib.IMAP4.abort:
            results['imap_existing_denied'] = True
        try:
            pop.retr(first)
            results['pop3_existing_denied'] = False
        except poplib.error_proto as error:
            assert b'-ERR' in str(error).encode() or 'EOF' in str(error)
            results['pop3_existing_denied'] = True
        fresh = smtp_connect(context)
        try:
            try:
                fresh.auth('PLAIN', lambda challenge=None: '\0' + ADDRESS + '\0' + codes['smtp'])
                results['smtp_new_login_denied'] = False
            except smtplib.SMTPAuthenticationError as error:
                assert error.smtp_code == 535
                results['smtp_new_login_denied'] = True
        finally:
            fresh.close()
        fresh = LocalIMAP(HOST, 993, ssl_context=context, timeout=30)
        try:
            try:
                fresh.login(ADDRESS, codes['imap'])
                results['imap_new_login_denied'] = False
            except imaplib.IMAP4.error as error:
                assert 'AUTHENTICATIONFAILED' in str(error)
                results['imap_new_login_denied'] = True
        finally:
            fresh.shutdown()
        fresh = LocalPOP(HOST, 995, context=context, timeout=30)
        try:
            fresh.user(ADDRESS)
            try:
                fresh.pass_(codes['pop3'])
                results['pop3_new_login_denied'] = False
            except poplib.error_proto as error:
                assert '-ERR' in str(error)
                results['pop3_new_login_denied'] = True
        finally:
            fresh.close()
        # 拒绝不能由整个服务崩溃导致。
        assert client.success('/api/v1/user/info').get('account') == ACCOUNT
        return results


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('root', type=Path)
    parser.add_argument('--live-net', required=True)
    parser.add_argument('--proxy', action='store_true')
    parser.add_argument('--password-stdin', action='store_true')
    parser.add_argument('--restore-message-id')
    parser.add_argument('--sql-stdin', action='store_true')
    args = parser.parse_args()
    assert str(Path('/proc/self/ns/net').readlink()) != args.live_net, '禁止在宿主网络执行测试'
    root = args.root.resolve(strict=True)
    assert root.parent.name.startswith(('smail-review-runtime-', 'smail-review-restore-')) and root.name == 'instance'
    password = sys.stdin.readline().strip() if args.password_stdin else secrets.token_urlsafe(9)
    assert len(password) == 12
    sql_database = None
    if args.sql_stdin:
        assert args.password_stdin and not args.restore_message_id
        from runtime_sql import SQLDatabase
        sql_database = SQLDatabase(json.loads(sys.stdin.readline()))
    if args.restore_message_id:
        assert args.password_stdin
        context = ssl.create_default_context(cafile=str(root / 'ssl/runtime.crt'))
        verify_restored(root, context, password, args.restore_message_id, 8002 if args.proxy else 443)
        return
    result = {'stage': 'setup', 'passed': False}
    started = time.monotonic()
    try:
        setup_port = 8001 if args.proxy else 80
        client = Client(port=setup_port)
        for _ in range(60):
            try:
                assert client.request('/', method='GET')[0] == 200
                break
            except (OSError, AssertionError):
                time.sleep(1)
        else:
            raise AssertionError('初始化服务未就绪')
        assert 'smail_setup_token' in client.cookies, '初始化令牌未下发'
        assert client.cookies['smail_setup_token']['httponly']
        assert Client(port=setup_port).request('/api/v1/setup', {'step': 'database', 'action': 'get'})[0] == 403
        assert not (root / 'data/smail.db').exists(), '业务库必须从空数据开始'
        setup = '/api/v1/setup'
        if sql_database:
            client.success(setup, sql_database.setup_payload())
            sql_database.assert_empty_users()
            assert not (root / 'data/smail.db').exists()
        else:
            client.success(setup, {'step': 'database', 'action': 'set', 'db_type': 'sqlite', 'db_dsn': ''})
            with sqlite3.connect(f'file:{root / "data/smail.db"}?mode=ro', uri=True) as database:
                assert database.execute('SELECT COUNT(*) FROM user').fetchone()[0] == 0
        client.success(setup, {'step': 'password', 'action': 'set', 'account': ACCOUNT, 'password': password})
        client.success(setup, {'step': 'domain', 'action': 'set', 'smtp_domain': 'runtime.invalid',
                               'web_domain': HOST, 'multi_domain': ''})
        client.success(setup, {'step': 'ssl', 'action': 'set', 'ssl_type': '1',
                               'key_path': '/opt/smail/ssl/runtime.key', 'crt_path': '/opt/smail/ssl/runtime.crt'})
        context = ssl.create_default_context(cafile=str(root / 'ssl/runtime.crt'))
        assert context.check_hostname and context.verify_mode == ssl.CERT_REQUIRED
        client = Client(context)
        for _ in range(60):
            try:
                assert client.request('/api/ping', method='GET')[0] == 200
                break
            except (OSError, AssertionError):
                time.sleep(1)
        else:
            raise AssertionError('正式 HTTPS 服务未就绪')
        login = {'account': ACCOUNT, 'password': password}
        identity = client.success('/api/v1/login', login)
        assert identity.get('account') == ACCOUNT and not identity.get('pending')
        assert 'smail_csrf' in client.cookies
        assert client.success('/api/v1/user/info').get('account') == ACCOUNT
        result['setup'] = {'empty_database': True, 'token_required': True, 'login': True, 'session': True}
        # 首次登录先完成正式安全设置，不绕过恢复方式要求直接启用协议。
        client.success('/api/v1/security/reauth', {'password': password})
        result['stage'] = 'recovery_codes'
        recovery_started = time.monotonic()
        try:
            recovery = client.success('/api/v1/security/credentials/recovery-codes/generate', {'count': 10})
        finally:
            result['recovery_generation_seconds'] = round(time.monotonic() - recovery_started, 3)
        assert len(recovery['codes']) == 10
        recovery.clear()
        result['security_setup'] = {'recovery_codes_generated': 10, 'plaintext_logged': False}
        result['config_history'] = verify_config_history(client)
        if args.proxy:
            from runtime_proxy import configure_proxy
            result['proxy'] = configure_proxy(client, context, login, Client, LocalHTTPS)
        result['stage'] = 'protocols'
        codes = {}
        for protocol in ('smtp', 'imap', 'pop3'):
            base = '/api/v1/security/credentials/protocols/'
            client.success(base + 'enabled', {'protocol': protocol, 'enabled': True}, 'PUT')
            codes[protocol] = client.success(base + 'generate', {'protocol': protocol})['code']
            assert protocol_operation(protocol, context, codes[protocol])() == 'success'
        result['protocol_login'] = list(codes)
        with smtp_connect(context) as smtp:
            try:
                smtp.auth('PLAIN', lambda challenge=None: '\0' + ADDRESS + '\0invalid-code')
                raise AssertionError('SMTP 错误凭据未被拒绝')
            except smtplib.SMTPAuthenticationError as error:
                assert error.smtp_code == 535 and error.smtp_error.startswith(b'5.7.8 ')
        result['smtp_invalid_credentials'] = 535
        result['stage'] = 'smtp'
        message = EmailMessage()
        message['From'] = 'probe@sender.invalid'
        message['To'] = ADDRESS
        message['Subject'] = '隔离首次安装中文收信验收'
        message_id = 'runtime-' + root.parent.name + '@runtime.invalid'
        message['Message-ID'] = '<' + message_id + '>'
        body = '内部25端口真实收信，检查中文正文和附件。'
        attachment = '隔离验收附件内容'.encode('utf-8')
        message.set_content(body)
        message.add_attachment(attachment, maintype='text', subtype='plain', filename='验收附件.txt')
        with smtp_connect(context) as smtp:
            assert not smtp.send_message(message)
        if sql_database:
            sql_database.verify_message(message_id, ACCOUNT, body, attachment)
        else:
            with sqlite3.connect(f'file:{root / "data/smail.db"}?mode=ro', uri=True) as database:
                rows = database.execute('SELECT id,text,attachments FROM email WHERE msg_id=?', (message_id,)).fetchall()
                assert len(rows) == 1 and body in rows[0][1]
                attachments = json.loads(rows[0][2])
                assert len(attachments) == 1 and attachments[0]['Filename'] == '验收附件.txt'
                assert base64.b64decode(attachments[0]['Content']) == attachment
                owners = database.execute('SELECT u.account FROM user_email e JOIN user u ON u.id=e.user_id WHERE e.email_id=?',
                                          (rows[0][0],)).fetchall()
                assert owners == [(ACCOUNT,)]
        result['smtp'] = {'message_id': message_id, 'stored': True, 'owners': 1, 'attachment': True}
        result['stage'] = 'protocol_read'
        result['protocol_read'] = verify_protocol_read(context, codes, message_id, body, attachment)
        result['imap_append'] = verify_imap_append(context, codes['imap'], message_id, body, attachment)
        result['stage'] = 'overload'
        rounds = []
        result['overload'] = rounds
        for protocol in codes:
            round_result = {'protocol': protocol, 'responses': [], 'recovered': False}
            rounds.append(round_result)
            operations = [protocol_operation(protocol, context, codes[protocol]) for _ in range(4)]
            barrier = threading.Barrier(8, timeout=20)

            def http_operation():
                status, data, retry = Client(context).request('/api/v1/login', login)
                if status == 503:
                    assert retry == '1' and data.get('data', {}).get('code') == 'AUTH_BUSY'
                    return 'busy'
                assert status == 200 and data.get('errorNo') == 0 and data['data'].get('account') == ACCOUNT
                return 'success'

            def execute(operation):
                barrier.wait()
                began = time.monotonic()
                return {'outcome': operation(), 'seconds': round(time.monotonic() - began, 3)}

            with ThreadPoolExecutor(max_workers=8) as pool:
                for response in pool.map(execute, operations + [http_operation] * 4):
                    round_result['responses'].append(response)
            responses = round_result['responses']
            assert any(item['outcome'] == 'busy' for item in responses[:4]), protocol + ' 未观察到繁忙响应'
            assert any(item['outcome'] == 'busy' for item in responses[4:]), 'HTTP 未观察到繁忙响应'
            assert any(item['outcome'] == 'success' for item in responses), '所有请求均失败'
            assert protocol_operation(protocol, context, codes[protocol])() == 'success'
            assert client.success('/api/v1/login', login).get('account') == ACCOUNT
            round_result['recovered'] = True
            (root.parent / 'probe-result.json').write_text(json.dumps(result, ensure_ascii=False, indent=2), encoding='utf-8')
        if args.proxy:
            from runtime_proxy import verify_proxy_sources
            result['proxy']['sources'] = verify_proxy_sources(context, login, Client)
        result['stage'] = 'protocol_revocation'
        result['protocol_revocation'] = verify_protocol_revocation(client, context, codes)
        assert all(result['protocol_revocation'].values()), '撤销后旧协议连接仍可访问业务数据'
        result['stage'] = 'client_revocation'
        credential = client.success('/api/v1/security/credentials/clients/create', {'client_name': '隔离旧连接撤销验收'})
        client_codes = dict.fromkeys(('smtp', 'imap', 'pop3'), credential['code'])
        revoke = lambda: client.success('/api/v1/security/credentials/clients/revoke', {'client_id': credential['client']['id']})
        result['client_revocation'] = verify_protocol_revocation(client, context, client_codes, revoke)
        assert all(result['client_revocation'].values()), '撤销客户端授权码后旧协议连接仍可访问业务数据'
        result['stage'] = 'http_session_revocation'
        result['http_session_revocation'] = verify_http_session_revocation(client, context)
        result['stage'] = 'complete'
        result['passed'] = True
    finally:
        result['seconds'] = round(time.monotonic() - started, 2)
        (root.parent / 'probe-result.json').write_text(json.dumps(result, ensure_ascii=False, indent=2), encoding='utf-8')
    print(json.dumps(result, ensure_ascii=False, indent=2))


if __name__ == '__main__':
    main()
