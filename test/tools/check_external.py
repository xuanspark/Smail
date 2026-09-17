import paramiko
ssh = paramiko.SSHClient()
ssh.set_missing_host_key_policy(paramiko.RejectPolicy())
from remote_ssh import connect_remote
connect_remote(ssh)
stdin, stdout, stderr = ssh.exec_command("sqlite3 /opt/smail/data/smail.db \"SELECT sql FROM sqlite_master WHERE name='external_email';\"")
print('SCHEMA:\n', stdout.read().decode())
stdin, stdout, stderr = ssh.exec_command("sqlite3 /opt/smail/data/smail.db \"SELECT * FROM external_email;\"")
print('DATA:\n', stdout.read().decode())
ssh.close()
