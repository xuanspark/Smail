'''复用部署模板，在独立网络中验收 Nginx 与 8001/8002，不访问线上实例。'''

import http.client
import shutil
import time


HOST = 'mail.runtime.invalid'


def prepare_nginx(target, root, template):
    directory = target / 'nginx'
    directory.mkdir(mode=0o750)
    shutil.chown(directory, user='smail', group='smail')
    source = template.read_text(encoding='utf-8')
    assert 'proxy_pass https://127.0.0.1:8002;' in source
    assert 'proxy_ssl_verify on;' in source
    assert 'proxy_set_header X-Forwarded-For $remote_addr;' in source
    site = source.replace('example.com', 'runtime.invalid')
    site = site.replace('/opt/smail/ssl/custom.crt', str(root / 'ssl/runtime.crt'))
    site = site.replace('/opt/smail/ssl/custom.key', str(root / 'ssl/runtime.key'))
    site = site.replace('/etc/ssl/certs/ca-certificates.crt', str(root / 'ssl/runtime.crt'))
    site = site.replace('/var/lib/smail-acme', str(directory / 'acme'))
    (directory / 'site.conf').write_text(site, encoding='utf-8')
    config = '\n'.join([
        'worker_processes 1;',
        'pid ' + str(directory / 'nginx.pid') + ';',
        'error_log ' + str(directory / 'error.log') + ' warn;',
        'events { worker_connections 128; }',
        'http {',
        '    access_log off;',
        *('    ' + name + '_temp_path ' + str(directory / name) + ';'
          for name in ('client_body', 'proxy', 'fastcgi', 'uwsgi', 'scgi')),
        '    include ' + str(directory / 'site.conf') + ';',
        '}',
        '',
    ])
    (directory / 'nginx.conf').write_text(config, encoding='utf-8')
    for name in ('site.conf', 'nginx.conf'):
        path = directory / name
        path.chmod(0o640)
        shutil.chown(path, user='root', group='smail')


def configure_proxy(client, context, login, client_type, connection_type):
    status = client.success('/api/v1/config/status', method='GET')
    assert status['available']
    payload = client.success('/api/v1/config/base', method='GET')
    assert payload['http']['port'] == 8001 and payload['http']['httpsPort'] == 8002
    payload.pop('databasePasswordStatus', None)
    payload['http']['publicHttpsPort'] = 443
    payload['http']['trustedProxyCidrs'] = ['127.0.0.1/32']
    request = {'expectedGeneration': status['activeGeneration'], 'payload': payload,
               'confirmation': {'confirmed': True, 'method': 'recent_auth'}}
    client.success('/api/v1/config/base/validate', request)
    candidate = client.success('/api/v1/config/base', request, 'PUT')
    client.success('/api/v1/config/base/apply', {
        'generation': candidate['generation'], 'candidateId': candidate['candidateId'],
    })
    for _ in range(30):
        try:
            active = client.success('/api/v1/config/base', method='GET')
            assert active['http']['publicHttpsPort'] == 443
            assert active['http']['trustedProxyCidrs'] == ['127.0.0.1/32']
            break
        except (OSError, http.client.HTTPException, AssertionError):
            time.sleep(1)
    else:
        raise AssertionError('反代配置未通过正式接口生效')
    assert active['http']['port'] == 8001 and active['http']['httpsPort'] == 8002
    assert client_type(context, port=8002).success('/api/v1/login', login)['account'] == login['account']
    redirects = {}
    for port in (80, 8001):
        connection = http.client.HTTPConnection('127.0.0.1', port, timeout=10)
        try:
            connection.request('GET', '/login?review=port', headers={'Host': HOST})
            response = connection.getresponse()
            response.read()
            assert response.status in (301, 302, 307, 308), (port, response.status)
            location = response.getheader('Location')
            assert location == 'https://' + HOST + '/login?review=port', (port, location)
            redirects[str(port)] = location
        finally:
            connection.close()
    recovery = verify_recovery_page(context, connection_type)
    return {'configuration_api': True, 'internal_http': 8001, 'internal_https': 8002,
            'public_https': 443, 'upstream_tls_verified': True, 'redirects': redirects,
            'recovery': recovery}


def verify_recovery_page(context, connection_type):
    from html.parser import HTMLParser

    class PageParser(HTMLParser):
        def __init__(self):
            super().__init__()
            self.login_url = None
            self.scripts = []

        def handle_starttag(self, tag, attrs):
            attributes = dict(attrs)
            if tag == 'div' and attributes.get('id') == 'app':
                self.login_url = attributes.get('data-login-url')
            if tag == 'script' and attributes.get('src'):
                self.scripts.append(attributes['src'])

    def fetch(path):
        connection = connection_type(HOST, 443, timeout=10, context=context)
        try:
            connection.request('GET', path)
            response = connection.getresponse()
            content = response.read()
            return response.status, dict(response.getheaders()), content
        finally:
            connection.close()

    status, headers, _ = fetch('/public-password-recovery')
    assert status == 302 and headers.get('Location') == 'https://' + HOST + '/rest/'
    status, headers, content = fetch('/rest/')
    assert status == 200 and headers.get('Cache-Control') == 'no-store'
    page = PageParser()
    page.feed(content.decode('utf-8'))
    assert page.login_url == 'https://' + HOST + '/#/login'
    assert page.scripts, '恢复页面缺少生产构建入口'
    for script in page.scripts:
        assert script.startswith('/assets/') and '..' not in script
        status, headers, content = fetch(script)
        assert status == 200 and content and 'javascript' in headers.get('Content-Type', '')
    return {'entry_redirect': True, 'login_url': page.login_url,
            'no_store': True, 'scripts_loaded': len(page.scripts)}


def verify_proxy_sources(context, login, client_type):
    first = client_type(context, source='127.0.0.2')
    second = client_type(context, source='127.0.0.3')
    wrong = {**login, 'password': 'invalid-proxy-review-password'}
    for _ in range(10):
        status, data, _ = first.request('/api/v1/login', wrong)
        assert status == 200 and data is not None and data['errorNo'] != 0
    status, data, _ = first.request('/api/v1/login', login, extra_headers={
        'X-Forwarded-For': '127.0.0.3', 'X-Real-IP': '127.0.0.3',
    })
    assert status == 200 and data is not None and data['errorNo'] != 0, '伪造来源绕过登录限流'
    assert second.success('/api/v1/login', login)['account'] == login['account'], '不同来源被错误合并限流'
    return {'blocked_source': '127.0.0.2', 'unaffected_source': '127.0.0.3',
            'forged_headers_rejected': True, 'independent_login': True}
