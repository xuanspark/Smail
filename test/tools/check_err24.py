import paramiko
ssh = paramiko.SSHClient()
ssh.set_missing_host_key_policy(paramiko.AutoAddPolicy())
ssh.connect("47.108.78.12", port=22, username="root", password="10102442Lb!", timeout=10)
stdin, stdout, stderr = ssh.exec_command('sqlite3 /opt/smail/data/smail.db "SELECT id, error FROM email WHERE id = 24;"')
print("Email 24 error:\n", stdout.read().decode())
stdin, stdout, stderr = ssh.exec_command('sqlite3 /opt/smail/data/smail.db "SELECT * FROM email WHERE id = 24;"')
print("Email 24 row:\n", stdout.read().decode())
stdin, stdout, stderr = ssh.exec_command('sqlite3 /opt/smail/data/smail.db "SELECT user_id, email, verified FROM external_email;"')
print("External emails:\n", stdout.read().decode())
ssh.close()

