import paramiko
client = paramiko.SSHClient()
client.set_missing_host_key_policy(paramiko.RejectPolicy())
from remote_ssh import connect_remote
connect_remote(client)
cmd = 'openssl rsa -in /opt/smail/ssl/dkim/dkim.priv -pubout -outform PEM'
stdin, stdout, stderr = client.exec_command(cmd)
lines = [l.strip() for l in stdout.read().decode('utf-8').splitlines() if '---' not in l and l.strip()]
print(''.join(lines))
client.close()
