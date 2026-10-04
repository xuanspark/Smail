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
        raise RuntimeError(f'?????????? {name}')
    return value


MAIL_DOMAIN = required_domain('SMAIL_MAIL_DOMAIN')
WEB_DOMAIN = required_domain('SMAIL_WEB_DOMAIN')
SMTP_DOMAIN = required_domain('SMAIL_SMTP_DOMAIN')
IMAP_DOMAIN = required_domain('SMAIL_IMAP_DOMAIN')
POP_DOMAIN = required_domain('SMAIL_POP_DOMAIN')
HOST = WEB_DOMAIN
CONTEXT = ssl.create_default_context()


def request(port, path='/', host=HOST, tls=True):
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


def check_redirects(results):
    expected = 'https://' + HOST + '/acceptance?x=1'
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
    results = {'time': datetime.now(timezone.utc).isoformat(), 'checks': []}
    leaf = subprocess.run(['openssl', 'x509', '-in', '/opt/smail/ssl/custom.crt', '-outform', 'DER'],
                          check=True, capture_output=True).stdout
    expected = hashlib.sha256(leaf).hexdigest()
    for port, host in [(443, HOST), (443, MAIL_DOMAIN), (443, SMTP_DOMAIN),
                       (8002, HOST), (465, SMTP_DOMAIN),
                       (993, IMAP_DOMAIN), (995, POP_DOMAIN)]:
        with socket.create_connection(('127.0.0.1', port), timeout=10) as raw:
            with CONTEXT.wrap_socket(raw, server_hostname=host) as stream:
                actual = hashlib.sha256(stream.getpeercert(binary_form=True)).hexdigest()
                assert actual == expected, (port, 'certificate mismatch')
                results['checks'].append({'port': port, 'hostname': host, 'tls': stream.version(),
                                          'systemTrust': True, 'sameCertificate': True})
    for port in (25, 587):
        with smtplib.SMTP(timeout=10) as client:
            assert client.connect('127.0.0.1', port)[0] == 220
            client._host = SMTP_DOMAIN
            assert client.ehlo('localhost')[0] == 250
            assert not client.has_extn('auth'), 'plaintext AUTH advertised'
            assert client.has_extn('starttls')
            assert client.starttls(context=CONTEXT)[0] == 220
            assert hashlib.sha256(client.sock.getpeercert(binary_form=True)).hexdigest() == expected
            assert client.ehlo('localhost')[0] == 250
            results['checks'].append({'port': port, 'systemTrust': True, 'starttls': True,
                                      'plaintextAuthHidden': True})
    for port in (443, 8002):
        status, headers, body = request(port)
        assert status == 200 and b'<html' in body.lower(), (port, status)
        assert 'Content-Security-Policy' in headers
        results['checks'].append({'port': port, 'pageStatus': status, 'pageBytes': len(body)})
        status, _, body = request(port, '/api/ping')
        assert status == 200
    results['failures'] = check_redirects(results)
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
    assert root['security']['webAuthnRpOrigins'] == ['https://' + WEB_DOMAIN]
    results['fingerprintSHA256'] = expected
    results['status'] = 'failed' if results['failures'] else 'passed'
    print(json.dumps(results, ensure_ascii=False, indent=2))
    return int(bool(results['failures']))


if __name__ == '__main__':
    raise SystemExit(main())
