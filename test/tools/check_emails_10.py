import paramiko
ssh = paramiko.SSHClient()
ssh.set_missing_host_key_policy(paramiko.AutoAddPolicy())
ssh.connect("47.108.78.12", port=22, username="root", password="10102442Lb!", timeout=10)
stdin, stdout, stderr = ssh.exec_command('sqlite3 /opt/smail/data/smail.db "SELECT id, type, subject, from_address, [to], send_user_id, status FROM email WHERE id >= 10;"')
print("Emails >= 10:\n", stdout.read().decode())
stdin, stdout, stderr = ssh.exec_command('sqlite3 /opt/smail/data/smail.db "SELECT id, user_id, email_id, is_read, group_id, status FROM user_email WHERE email_id >= 10;"')
print("UserEmails >= 10:\n", stdout.read().decode())
stdin, stdout, stderr = ssh.exec_command('sqlite3 /opt/smail/data/smail.db "SELECT user_id, email, verified FROM external_email;"')
print("External emails:\n", stdout.read().decode())
ssh.close()

