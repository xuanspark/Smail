# -*- coding: utf-8 -*-
import requests
import urllib3
urllib3.disable_warnings()

session = requests.Session()
login_res = session.post('https://mail.solit.cn/api/login', json={'username': 'xuans', 'password': '12345678'}, verify=False)
print('Login status:', login_res.status_code, login_res.text)

get_res = session.get('https://mail.solit.cn/api/v1/config/mail', verify=False)
print('Config mail get:', get_res.status_code, get_res.text)

status_res = session.get('https://mail.solit.cn/api/v1/config/status', verify=False)
print('Config status:', status_res.status_code, status_res.text)