import requests

from runtime_inputs import ADMIN_ACCOUNT, ADMIN_PASSWORD, MAIL_HOST, WEB_URL

base_url = WEB_URL.rstrip('/')
login_res = requests.post(
    base_url + '/api/login',
    json={'username': ADMIN_ACCOUNT, 'password': ADMIN_PASSWORD},
    timeout=15,
)
print('Login status:', login_res.status_code, login_res.text)

get_res = requests.get(base_url + '/api/v1/config/mail', timeout=15)
print('Config mail get:', get_res.status_code, get_res.text)

status_res = requests.get(base_url + '/api/v1/config/status', timeout=15)
print('Config status:', status_res.status_code, status_res.text)
