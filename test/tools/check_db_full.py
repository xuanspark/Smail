import paramiko
ssh = paramiko.SSHClient()
ssh.set_missing_host_key_policy(paramiko.AutoAddPolicy())
ssh.connect("47.108.78.12", port=22, username="root", password="10102442Lb!", timeout=10)
stdin, stdout, stderr = ssh.exec_command('sqlite3 /opt/smail/data/smail.db "SELECT id, type, subject, from_address, send_user_id, status, error, create_time FROM email WHERE id >= 20;"')
print("Security questions:\n", stdout.read().decode())
stdin, stdout, stderr = ssh.exec_command('sqlite3 /opt/smail/data/smail.db "SELECT id, user_id, email_id, is_read, group_id, status, create FROM user_email WHERE email_id >= 20;"')
print("Recovery codes count:\n", stdout.read().decode())
stdin, stdout, stderr = ssh.exec_command('sqlite3 /opt/smail/data/smail.db "SELECT id, name, user_name, domain FROM user;"')
print("External emails:\n", stdout.read().decode())
ssh.close()

