"""读取由用户显式提供的远程验收参数。"""

import os


def required(name):
    value = os.environ.get(name, '').strip()
    if not value:
        raise RuntimeError(f'缺少运行时参数：{name}')
    return value


def optional(name, default=''):
    return os.environ.get(name, default).strip()


WEB_URL = required('SMAIL_WEB_URL')
MAIL_DOMAIN = required('SMAIL_MAIL_DOMAIN')
MAIL_HOST = required('SMAIL_MAIL_HOST')
ADMIN_ACCOUNT = required('SMAIL_ADMIN_ACCOUNT')
ADMIN_PASSWORD = required('SMAIL_ADMIN_PASSWORD')
SMTP_HOST = required('SMAIL_RELAY_HOST')
SMTP_PORT = int(required('SMAIL_RELAY_PORT'))
SMTP_USERNAME = required('SMAIL_RELAY_USERNAME')
SMTP_PASSWORD = required('SMAIL_RELAY_PASSWORD')
FROM_ADDRESS = required('SMAIL_FROM_ADDRESS')

