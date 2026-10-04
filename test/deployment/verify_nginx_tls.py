"""仅检查远程传输层，不登录、不发信、不读写业务数据库。"""

from datetime import datetime, timezone
import hashlib
import http.client
import json
import os
from pathlib import Path
import smtplib
import socket
import ssl
import subprocess


def required_domain(name):
    value = os.environ.get(name, '').strip().lower()
    if not value:
        raise RuntimeError(f'缺少运行时域名参数：{name}')
    return value


TEST_HOST = 'web.example.com'
CONTEXT = ssl.create_default_context()


def load_domains():
    return {
        'mail': required_domain('SMAIL_MAIL_DOMAIN'),
        'web': required_domain('SMAIL_WEB_DOMAIN'),
        'smtp': required_domain('SMAIL_SMTP_DOMAIN'),
        'imap': required_domain('SMAIL_IMAP_DOMAIN'),
        'pop': required_domain('SMAIL_POP_DOMAIN'),
    }


def request(port, path='/', host=TEST_HOST, tls=True):
    raw = socket.create_connection(('127.0.0.1', port), timeout=10)
    stream = CONTEXT.wrap_socket(raw, server_hostname=host) if tls else raw
    try:
        stream.sendall(f'GET {path} HTTP/1.1\r\nHost: {host}\r\nConnection: close\r\n\r\n'.encode())
        response = http.client.HTTPResponse(stream)
        response.begin()
        body = response.read()
        return response.status, dict(response.getheaders()), body
    finally:
        stream.close()


def check_redirects(results, host=TEST_HOST):
    expected = 'https://' + host + '/acceptance?x=1'
    failures = []
    for port in (80, 8001):
        status, headers, _ = request(port, '/acceptance?x=1', tls=False)
        location = headers.get('Location')
        results['checks'].append({'port': port, 'redirect': location, 'status': status})
        if status != 308 or location != expected:
            failures.append({'port': port, 'check': 'public_redirect',
                             'expectedStatus': 308, 'actualStatus': status,
                             'expectedLocation': expected, 'actualLocation': location})
    return failures


def main():
    domains = load_domains()
    host = domains['web']
    results = {'time': datetime.now(timezone.utc).isoformat(), 'checks': []}
    leaf = subprocess.run(['openssl', 'x509', '-in', '/opt/smail/ssl/custom.crt', '-outform', 'DER'],
                          check=True, capture_output=True).stdout
    expected = hashlib.sha256(leaf).hexdigest()
    for port, tls_host in [(443, host), (443, domains['mail']), (443, domains['smtp']),
                           (8002, host), (465, domains['smtp']),
                           (993, domains['imap']), (995, domains['pop'])]:
        with socket.create_connection(('127.0.0.1', port), timeout=10) as raw:
            with CONTEXT.wrap_socket(raw, server_hostname=tls_host) as stream:
                actual = hashlib.sha256(stream.getpeercert(binary_form=True)).hexdigest()
                assert actual == expected, (port, 'certificate mismatch')
                results['checks'].append({'port': port, 'hostname': tls_host, 'tls': stream.version(),
                                          'systemTrust': True, 'sameCertificate': True})
    for port in (25, 587):
        with smtplib.SMTP(timeout=10) as client:
            assert client.connect('127.0.0.1', port)[0] == 220
            client._host = domains['smtp']
            assert client.ehlo('localhost')[0] == 250
            assert not client.has_extn('auth'), 'plaintext AUTH advertised'
            assert client.has_extn('starttls')
            assert client.starttls(context=CONTEXT)[0] == 220
            assert hashlib.sha256(client.sock.getpeercert(binary_form=True)).hexdigest() == expected
            assert client.ehlo('localhost')[0] == 250
            results['checks'].append({'port': port, 'systemTrust': True, 'starttls': True,
                                      'plaintextAuthHidden': True})
    for port in (443, 8002):
        status, headers, body = request(port, host=host)
        assert status == 200 and b'<html' in body.lower(), (port, status)
        assert 'Content-Security-Policy' in headers
        results['checks'].append({'port': port, 'pageStatus': status, 'pageBytes': len(body)})
        status, _, body = request(port, '/api/ping', host=host)
        assert status == 200
    results['failures'] = check_redirects(results, host)
    assert request(80, host='untrusted.invalid', tls=False)[0] == 404
    results['checks'].append({'unknownHttpHost': 'rejected'})
    try:
        with socket.create_connection(('127.0.0.1', 443), timeout=10) as raw:
            CONTEXT.wrap_socket(raw, server_hostname='untrusted.invalid')
    except ssl.SSLError:
        results['checks'].append({'unknownSNI': 'rejected'})
    else:
        raise AssertionError('unknown SNI accepted')
    root = json.loads(Path('/opt/smail/config/config.json').read_text())
    assert root['http']['port'] == 8001 and root['http']['httpsPort'] == 8002
    assert root['security']['webAuthnRpOrigins'] == ['https://' + host]
    results['fingerprintSHA256'] = expected
    results['status'] = 'failed' if results['failures'] else 'passed'
    print(json.dumps(results, ensure_ascii=False, indent=2))
    return int(bool(results['failures']))


if __name__ == '__main__':
    raise SystemExit(main())
