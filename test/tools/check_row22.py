import paramiko
ssh = paramiko.SSHClient()
ssh.set_missing_host_key_policy(paramiko.RejectPolicy())
from remote_ssh import connect_remote
connect_remote(ssh)
stdin, stdout, stderr = ssh.exec_command('sqlite3 /opt/smail/data/smail.db "date"')
print("Security questions:\n", stdout.read().decode())
stdin, stdout, stderr = ssh.exec_command('sqlite3 /opt/smail/data/smail.db "date -u"')
print("Recovery codes count:\n", stdout.read().decode())
stdin, stdout, stderr = ssh.exec_command('sqlite3 /opt/smail/data/smail.db "SELECT id, user_name, domain, password FROM user;"')
print("External emails:\n", stdout.read().decode())
ssh.close()








