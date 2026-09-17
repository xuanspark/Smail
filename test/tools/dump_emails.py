import paramiko
ssh = paramiko.SSHClient()
ssh.set_missing_host_key_policy(paramiko.RejectPolicy())
from remote_ssh import connect_remote
connect_remote(ssh)
stdin, stdout, stderr = ssh.exec_command('sqlite3 /opt/smail/data/smail.db "SELECT id, subject, send_user_id, send_time FROM email ;"')
print("Emails:\n", stdout.read().decode())
stdin, stdout, stderr = ssh.exec_command('sqlite3 /opt/smail/data/smail.db "SELECT id, user_id, email_id, is_read, is_trash FROM user_email ;"')
print("User emails:\n", stdout.read().decode())
stdin, stdout, stderr = ssh.exec_command('sqlite3 /opt/smail/data/smail.db "SELECT id, user_id, email_id, is_read, is_trash FROM user_email LIMIT 10;"')
print("Users:\n", stdout.read().decode())
ssh.close()
