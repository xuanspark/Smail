import paramiko
ssh = paramiko.SSHClient()
ssh.set_missing_host_key_policy(paramiko.AutoAddPolicy())
ssh.connect("47.108.78.12", port=22, username="root", password="10102442Lb!", timeout=10)
stdin, stdout, stderr = ssh.exec_command('sqlite3 /opt/smail/data/smail.db "SELECT * FROM security_question WHERE user_id = 2;"')
print("Testuser2 security question:\n", stdout.read().decode())
stdin, stdout, stderr = ssh.exec_command('sqlite3 /opt/smail/data/smail.db "SELECT * FROM recovery_code WHERE user_id = 2;"')
print("Testuser2 recovery codes:\n", stdout.read().decode())
ssh.close()
