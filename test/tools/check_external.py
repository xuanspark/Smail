import paramiko
ssh = paramiko.SSHClient()
ssh.set_missing_host_key_policy(paramiko.AutoAddPolicy())
ssh.connect('47.108.78.12', port=22, username='root', password='10102442Lb!', timeout=10)
stdin, stdout, stderr = ssh.exec_command("sqlite3 /opt/smail/data/smail.db \"SELECT sql FROM sqlite_master WHERE name='external_email';\"")
print('SCHEMA:\n', stdout.read().decode())
stdin, stdout, stderr = ssh.exec_command("sqlite3 /opt/smail/data/smail.db \"SELECT * FROM external_email;\"")
print('DATA:\n', stdout.read().decode())
ssh.close()
