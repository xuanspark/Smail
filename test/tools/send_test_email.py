import paramiko
ssh = paramiko.SSHClient()
ssh.set_missing_host_key_policy(paramiko.RejectPolicy())
from remote_ssh import connect_remote
connect_remote(ssh)

s = '''import os
import smtplib, ssl
from email.mime.text import MIMEText

recipients = [item.strip() for item in os.environ['SMAIL_TEST_RECIPIENTS'].split(',') if item.strip()]
for target in recipients:
    msg = MIMEText('Smail DirectMail test outbound delivery', 'plain', 'utf-8')
    msg['From'] = os.environ['SMAIL_FROM_ADDRESS']
    msg['To'] = target
    msg['Subject'] = 'Smail DirectMail Test'

    context = ssl.create_default_context()
    with smtplib.SMTP_SSL(
        os.environ['SMAIL_RELAY_HOST'],
        int(os.environ['SMAIL_RELAY_PORT']),
        context=context,
    ) as server:
        username = os.environ['SMAIL_RELAY_USERNAME']
        server.login(username, os.environ['SMAIL_RELAY_PASSWORD'])
        res = server.sendmail(os.environ['SMAIL_FROM_ADDRESS'], [target], msg.as_string())
        print(f'Send to {target}: {res}')
'''
sftp = ssh.open_sftp()
with sftp.file('/tmp/test_send_all.py', 'w') as f:
    f.write(s)
sftp.close()
_, out, err = ssh.exec_command('python3 /tmp/test_send_all.py')
print('OUT:\n', out.read().decode())
print('ERR:\n', err.read().decode())
ssh.close()
