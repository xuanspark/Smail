"""仅在指定远程部署机执行内部 SMTP 收信回归，不修改业务配置。"""

import base64
import hashlib
import json
import smtplib
import sqlite3
import ssl
import subprocess
import time
from email.message import EmailMessage
from email.policy import SMTP
from email.utils import formatdate
from pathlib import Path


ROOT = Path('/opt/smail')
RUN = time.strftime('%Y%m%d-%H%M%S')
DOMAIN = 'solit.cn'
BODY = '内部25端口中文正文验收：邮件必须真实入库，并且只属于指定收件人。'
ATTACHMENT = '中文附件内容，内部收信验收。'.encode('utf-8')


def connect():
    client = smtplib.SMTP('127.0.0.1', 25, timeout=30)
    client.ehlo('internal-check.invalid')
    assert client.has_extn('starttls'), 'STARTTLS not advertised'
    assert not client.has_extn('auth'), 'AUTH advertised before TLS'
    code, _ = client.docmd('AUTH', 'PLAIN')
    assert code in (502, 523, 538), f'plaintext AUTH not rejected: {code}'
    # 仅本次客户端信任部署证书，仍校验主机名和证书有效期。
    context = ssl.create_default_context(cafile=str(ROOT / 'ssl/custom.crt'))
    client._host = 'mail.solit.cn'
    client.starttls(context=context)
    client.ehlo('internal-check.invalid')
    assert client.sock.getpeername()[0] == '127.0.0.1'
    assert context.check_hostname and context.verify_mode == ssl.CERT_REQUIRED
    return client


def deliver(label, recipients, multipart=False):
    message = EmailMessage(policy=SMTP)
    message['From'] = '内部验收 <probe@internal-check.invalid>'
    message['To'] = 'remotecheck@solit.cn'
    message['Subject'] = f'内部25端口验收 {label} {RUN}'
    message['Message-ID'] = f'<internal-{RUN}-{label}@internal-check.invalid>'
    message['Date'] = formatdate(localtime=False, usegmt=True)
    message.set_content(BODY)
    if multipart:
        message.add_alternative('<p>内部25端口<strong>中文HTML正文</strong></p>', subtype='html')
        message.add_attachment(ATTACHMENT, maintype='text', subtype='plain', filename='中文验收附件.txt')
    with connect() as client:
        code, response = client.mail('probe@internal-check.invalid')
        assert code == 250, (code, response)
        for recipient in recipients:
            code, response = client.rcpt(recipient)
            assert code == 250, (code, response)
        code, response = client.data(message.as_bytes())
        assert code == 250, (code, response)
        return {'label': label, 'message_id': str(message['Message-ID']).strip('<>'), 'data_code': code,
                'tls': client.sock.version(), 'recipient_count': len(recipients)}


def main():
    assert ROOT.is_dir() and Path('/etc/systemd/system/smail.service').is_file()
    assert subprocess.check_output(['systemctl', 'is-active', 'smail.service'], text=True).strip() == 'active'
    # 只读连接真实业务库，备份两份数据库，不清除前一轮故障证据。
    backup = Path('/var/backups') / f'smail-internal-smtp-{RUN}'
    backup.mkdir(mode=0o700)
    for name in ('smail.db', 'smail-state.db'):
        with sqlite3.connect(f'file:{ROOT / "data" / name}?mode=ro', uri=True) as source:
            with sqlite3.connect(backup / name) as target:
                source.backup(target)
                assert target.execute('PRAGMA integrity_check').fetchone()[0] == 'ok'
        (backup / name).chmod(0o600)
    with sqlite3.connect(f'file:{ROOT / "data/smail.db"}?mode=ro', uri=True) as database:
        database.row_factory = sqlite3.Row
        policy = json.loads((ROOT / 'config/email.json').read_text(encoding='utf-8')).get('policy', {})
        assert policy.get('spamFilterLevel', 0) == 0, 'This fixture requires the current unfiltered policy'
        users = dict(database.execute("SELECT account,id FROM user WHERE account IN ('admin','remotecheck')"))
        assert set(users) == {'admin', 'remotecheck'}
        results = []
        cases = [
            ('text', ['remotecheck@solit.cn'], False, {users['remotecheck']}),
            ('multipart', ['remotecheck@solit.cn', 'admin@solit.cn'], True, set(users.values())),
            ('admin_only', ['admin@solit.cn'], True, {users['admin']}),
            ('unknown', [f'missing-{RUN}@solit.cn'], False, {users['admin']}),
            ('relay', ['nobody@external-check.invalid'], False, {users['admin']}),
        ]
        for label, recipients, multipart, expected_users in cases:
            result = deliver(label, recipients, multipart)
            emails = database.execute('SELECT * FROM email WHERE msg_id=?', (result['message_id'],)).fetchall()
            if expected_users:
                assert len(emails) == 1, f'{label}: expected one stored message, got {len(emails)}'
                email = emails[0]
                relations = database.execute('SELECT user_id,status FROM user_email WHERE email_id=?', (email['id'],)).fetchall()
                assert {row['user_id'] for row in relations} == expected_users
                assert BODY in email['text']
                assert email['type'] == 0
                if multipart:
                    assert '中文HTML正文' in email['html']
                    attachments = json.loads(email['attachments'])
                    assert len(attachments) == 1
                    assert attachments[0]['Filename'] == '中文验收附件.txt', repr(attachments[0]['Filename'])
                    assert base64.b64decode(attachments[0]['Content']) == ATTACHMENT
                result.update(email_id=email['id'], owners=sorted(expected_users),
                              mailbox_status=[row['status'] for row in relations], stored=True)
                if label in ('unknown', 'relay'):
                    result['outcome'] = 'administrator_fallback_no_outgoing_record'
            else:
                assert not emails, f'{label}: unexpected stored message'
                result.update(stored=False, outcome='accepted_then_discarded')
            results.append(result)
        summary = {'run': RUN, 'backup': str(backup), 'target': '127.0.0.1:25', 'spam_filter_level': 0,
                   'certificate_sha256': hashlib.sha256((ROOT / 'ssl/custom.crt').read_bytes()).hexdigest(),
                   'results': results}
        (backup / 'result.json').write_text(json.dumps(summary, ensure_ascii=False, indent=2), encoding='utf-8')
        print(json.dumps(summary, ensure_ascii=False, indent=2))


if __name__ == '__main__':
    main()
