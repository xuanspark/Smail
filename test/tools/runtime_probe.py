"""仅在隔离网络中通过正式接口验收首次安装和认证，不持久化测试密码。"""

import argparse
import base64
from concurrent.futures import ThreadPoolExecutor
from email.message import EmailMessage
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


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('root', type=Path)
    parser.add_argument('--live-net', required=True)
    parser.add_argument('--proxy', action='store_true')
    args = parser.parse_args()
    assert str(Path('/proc/self/ns/net').readlink()) != args.live_net, '禁止在宿主网络执行测试'
    root = args.root.resolve(strict=True)
    assert root.parent.name.startswith('smail-review-runtime-') and root.name == 'instance'
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
        client.success(setup, {'step': 'database', 'action': 'set', 'db_type': 'sqlite', 'db_dsn': ''})
        with sqlite3.connect(f'file:{root / "data/smail.db"}?mode=ro', uri=True) as database:
            assert database.execute('SELECT COUNT(*) FROM user').fetchone()[0] == 0
        password = secrets.token_urlsafe(9)
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
        message['From'] = 'probe@runtime.invalid'
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
        result['stage'] = 'complete'
        result['passed'] = True
    finally:
        result['seconds'] = round(time.monotonic() - started, 2)
        (root.parent / 'probe-result.json').write_text(json.dumps(result, ensure_ascii=False, indent=2), encoding='utf-8')
    print(json.dumps(result, ensure_ascii=False, indent=2))


if __name__ == '__main__':
    main()
