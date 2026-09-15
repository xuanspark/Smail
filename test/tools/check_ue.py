import paramiko
ssh = paramiko.SSHClient()
ssh.set_missing_host_key_policy(paramiko.AutoAddPolicy())
ssh.connect("47.108.78.12", port=22, username="root", password="10102442Lb!", timeout=10)
stdin, stdout, stderr = ssh.exec_command('sqlite3 /opt/smail/data/smail.db "SELECT user_id, email_id FROM user_email WHERE email_id = 22;"')
print("Security questions:\n", stdout.read().decode())
stdin, stdout, stderr = ssh.exec_command('sqlite3 /opt/smail/data/smail.db "SELECT user_id, email_id FROM user_email WHERE email_id = 23;"')
print("Recovery codes count:\n", stdout.read().decode())
stdin, stdout, stderr = ssh.exec_command('sqlite3 /opt/smail/data/smail.db "SELECT user_id, email_id FROM user_email WHERE email_id = 21;"')
print("External emails:\n", stdout.read().decode())
ssh.close()

