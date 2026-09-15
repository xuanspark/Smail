import paramiko
ssh = paramiko.SSHClient()
ssh.set_missing_host_key_policy(paramiko.AutoAddPolicy())
ssh.connect('47.108.78.12', port=22, username='root', password='10102442Lb!', timeout=10)

s = '''import smtplib, ssl
from email.mime.text import MIMEText

for target, subj in [
    ('gfxezc@163.com', 'Smail DirectMail Test - Admin'),
    ('xuanspark@163.com', 'Smail DirectMail Test - User'),
    ('likb_blue@163.com', 'Smail DirectMail Test - SuperAdmin')
]:
    msg = MIMEText('Smail DirectMail test outbound delivery', 'plain', 'utf-8')
    msg['From'] = 'xuans@solit.cn'
    msg['To'] = target
    msg['Subject'] = subj

    context = ssl.create_default_context()
    with smtplib.SMTP_SSL('smtpdm.aliyun.com', 465, context=context) as server:
        server.login('xuans@solit.cn', 'Solit12ABcd')
        res = server.sendmail('xuans@solit.cn', [target], msg.as_string())
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
