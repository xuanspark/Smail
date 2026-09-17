import paramiko
ssh = paramiko.SSHClient()
ssh.set_missing_host_key_policy(paramiko.RejectPolicy())
from remote_ssh import connect_remote
connect_remote(ssh)
_, out, _ = ssh.exec_command('cat /opt/smail/config/smtp.json; echo ===; cat /opt/smail/config/email.json')
print(out.read().decode('utf-8', errors='ignore'))
ssh.close()
