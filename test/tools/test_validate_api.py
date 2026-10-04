import json

import requests

from runtime_inputs import (
    ADMIN_ACCOUNT,
    ADMIN_PASSWORD,
    FROM_ADDRESS,
    MAIL_DOMAIN,
    SMTP_HOST,
    SMTP_PASSWORD,
    SMTP_USERNAME,
    required,
)

base_url = required('SMAIL_HTTP_URL').rstrip('/')
session = requests.Session()
r_login = session.post(
    base_url + '/api/login',
    json={'username': ADMIN_ACCOUNT, 'password': ADMIN_PASSWORD},
    timeout=15,
)
print('Login:', r_login.status_code, r_login.text)

r_get = session.get(base_url + '/api/v1/config/mail', timeout=15)
print('Get mail:', r_get.status_code, r_get.text)

data = r_get.json().get('data', {})
generation = data.get('meta', {}).get('generation', 2)
payload = json.loads(json.dumps(data))
if 'relayPasswordStatus' in payload:
    del payload['relayPasswordStatus']
if 'relay' in payload and 'password' in payload['relay']:
    del payload['relay']['password']

payload['relay']['relayHost'] = SMTP_HOST
payload['relay']['relayPort'] = int(required('SMAIL_RELAY_PORT'))
payload['relay']['username'] = SMTP_USERNAME
payload['relay']['from'] = FROM_ADDRESS
payload['relay']['fromDomain'] = MAIL_DOMAIN

secret_changes = {
    'relay.password': {'operation': 'replace', 'value': {'plaintext': SMTP_PASSWORD}}
}
request_body = {
    'expectedGeneration': generation,
    'payload': payload,
    'secretChanges': secret_changes,
    'confirmation': {'confirmed': True, 'method': 'recent_auth'},
}

print('Testing validate with user-provided relay host...')
r_val = session.post(base_url + '/api/v1/config/mail/validate', json=request_body, timeout=15)
print('Validate res:', r_val.status_code, r_val.text)
r_put = session.put(base_url + '/api/v1/config/mail', json=request_body, timeout=15)
print('PUT res:', r_put.status_code, r_put.text)
