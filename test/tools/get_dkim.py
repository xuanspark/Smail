import paramiko
client = paramiko.SSHClient()
client.set_missing_host_key_policy(paramiko.AutoAddPolicy())
client.connect('47.108.78.12', port=22, username='root', password='10102442Lb!', timeout=10)
cmd = 'openssl rsa -in /opt/smail/ssl/dkim/dkim.priv -pubout -outform PEM'
stdin, stdout, stderr = client.exec_command(cmd)
lines = [l.strip() for l in stdout.read().decode('utf-8').splitlines() if '---' not in l and l.strip()]
print(''.join(lines))
client.close()
