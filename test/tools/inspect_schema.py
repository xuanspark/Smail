import paramiko
ssh = paramiko.SSHClient()
ssh.set_missing_host_key_policy(paramiko.RejectPolicy())
from remote_ssh import connect_remote
connect_remote(ssh)
_, stdout, _ = ssh.exec_command('sqlite3 /opt/smail/data/smail.db ".schema client_credential"')
print(stdout.read().decode())
_, stdout, _ = ssh.exec_command('sqlite3 /opt/smail/data/smail.db ".schema protocol_credential"')
print(stdout.read().decode())
ssh.close()
