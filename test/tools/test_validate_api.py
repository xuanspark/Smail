# -*- coding: utf-8 -*-
import requests, json
import urllib3
urllib3.disable_warnings()

s = requests.Session()
# 47.108.78.12 80 端口
r_login = s.post('http://47.108.78.12:80/api/login', json={'username': 'xuans', 'password': '12345678'})
print('Login:', r_login.status_code, r_login.text)

r_get = s.get('http://47.108.78.12:80/api/v1/config/mail')
print('Get mail:', r_get.status_code, r_get.text)

# 模拟前端发送的 validate 请求
data = r_get.json().get('data', {})
generation = data.get('meta', {}).get('generation', 2)

# 用户在前端填入的配置
payload = json.loads(json.dumps(data))
# 注意前端 parsePayload 会对 payload 做一些处理：
# delete payload.relayPasswordStatus
# if (payload.relay && 'password' in payload.relay) delete payload.relay.password
if 'relayPasswordStatus' in payload:
    del payload['relayPasswordStatus']
if 'relay' in payload and 'password' in payload['relay']:
    del payload['relay']['password']

payload['relay']['relayHost'] = 'smtpm.aliyun.com'
payload['relay']['relayPort'] = 587
payload['relay']['username'] = 'xuans'
payload['relay']['from'] = 'xuans@solit.cn'
payload['relay']['fromDomain'] = 'solit.cn'

secretChanges = {
    'relay.password': {'operation': 'replace', 'value': {'plaintext': 'Solit12ABcd'}}
}

req_body = {
    'expectedGeneration': generation,
    'payload': payload,
    'secretChanges': secretChanges,
    'confirmation': {'confirmed': True, 'method': 'recent_auth'}
}

print('Testing validate with smtpm.aliyun.com...')
r_val = s.post('http://47.108.78.12:80/api/v1/config/mail/validate', json=req_body)
print('Validate res:', r_val.status_code, r_val.text)

# 再测一次直接 PUT (UpdateConfig)
r_put = s.put('http://47.108.78.12:80/api/v1/config/mail', json=req_body)
print('PUT res:', r_put.status_code, r_put.text)