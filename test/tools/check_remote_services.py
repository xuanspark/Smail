import paramiko
ssh = paramiko.SSHClient()
ssh.set_missing_host_key_policy(paramiko.RejectPolicy())
from remote_ssh import connect_remote
connect_remote(ssh)
_, out, _ = ssh.exec_command('ss -tulpn; ps aux | grep smail')
print(out.read().decode('utf-8', errors='ignore'))
ssh.close()