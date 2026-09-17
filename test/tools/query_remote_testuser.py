import paramiko
ssh = paramiko.SSHClient()
ssh.set_missing_host_key_policy(paramiko.RejectPolicy())
from remote_ssh import connect_remote
connect_remote(ssh)
stdin, stdout, stderr = ssh.exec_command('sqlite3 /opt/smail/data/smail.db "SELECT * FROM security_question WHERE user_id = 2;"')
print("Testuser2 security question:\n", stdout.read().decode())
stdin, stdout, stderr = ssh.exec_command('sqlite3 /opt/smail/data/smail.db "SELECT * FROM recovery_code WHERE user_id = 2;"')
print("Testuser2 recovery codes:\n", stdout.read().decode())
ssh.close()
