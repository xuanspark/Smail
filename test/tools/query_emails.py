import paramiko
ssh = paramiko.SSHClient()
ssh.set_missing_host_key_policy(paramiko.RejectPolicy())
from remote_ssh import connect_remote
connect_remote(ssh)
stdin, stdout, stderr = ssh.exec_command('sqlite3 /opt/smail/data/smail.db "SELECT id, user_id, email_id, is_read, group_id, status FROM user_email ORDER BY id DESC LIMIT 10;"')
print("Security questions:\n", stdout.read().decode("utf-8", errors="ignore").encode("utf-8"))
stdin, stdout, stderr = ssh.exec_command('sqlite3 /opt/smail/data/smail.db "SELECT id, type, subject, send_user_id, status FROM email ORDER BY id DESC LIMIT 10;"')
print("Recovery codes count:\n", stdout.read().decode("utf-8", errors="ignore").encode("utf-8"))
stdin, stdout, stderr = ssh.exec_command('sqlite3 /opt/smail/data/smail.db "SELECT user_id, email, verified FROM external_email;"')
print("External emails:\n", stdout.read().decode("utf-8", errors="ignore").encode("utf-8"))
ssh.close()





