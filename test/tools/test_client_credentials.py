# -*- coding: utf-8 -*-
import urllib.request, ssl, json, http.cookiejar

ctx = ssl.create_default_context()
ctx.check_hostname = False
ctx.verify_mode = ssl.CERT_NONE
cj = http.cookiejar.CookieJar()
opener = urllib.request.build_opener(urllib.request.HTTPSHandler(context=ctx), urllib.request.HTTPCookieProcessor(cj))

# 1. 登录 xuans
login_payload = json.dumps({'account': 'xuans', 'password': '12345678'}).encode('utf-8')
req = urllib.request.Request('https://47.108.78.12/api/v1/login', data=login_payload, headers={
    'Content-Type': 'application/json',
    'Host': 'mail.solit.cn',
    'Origin': 'https://mail.solit.cn'
})
resp = opener.open(req, timeout=5)
print('Login resp:', resp.read().decode())

csrf_token = ''
for c in cj:
    if c.name == 'smail_csrf':
        csrf_token = c.value

headers = {
    'Content-Type': 'application/json',
    'Host': 'mail.solit.cn',
    'Origin': 'https://mail.solit.cn',
    'Referer': 'https://mail.solit.cn/',
    'X-CSRF-Token': csrf_token
}

# 2. 读取客户端授权码列表
req_list = urllib.request.Request('https://47.108.78.12/api/v1/security/client-credentials/list', headers=headers)
try:
    r_list = opener.open(req_list, timeout=5)
    print('List client credentials:\n', r_list.read().decode())
except urllib.error.HTTPError as e:
    print('List error:', e.code, e.read().decode())

# 3. 输入密码验密 reveal 授权码明文
req_reveal = urllib.request.Request(
    'https://47.108.78.12/api/v1/security/client-credentials/reveal',
    data=json.dumps({'password': '12345678'}).encode('utf-8'),
    headers=headers
)
try:
    r_reveal = opener.open(req_reveal, timeout=5)
    print('Reveal client credentials:\n', r_reveal.read().decode())
except urllib.error.HTTPError as e:
    print('Reveal error:', e.code, e.read().decode())
