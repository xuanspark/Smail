import paramiko
ssh = paramiko.SSHClient()
ssh.set_missing_host_key_policy(paramiko.AutoAddPolicy())
ssh.connect('47.108.78.12', port=22, username='root', password='10102442Lb!', timeout=10)
_, stdout, _ = ssh.exec_command('sqlite3 /opt/smail/data/smail.db ".schema client_credential"')
print(stdout.read().decode())
_, stdout, _ = ssh.exec_command('sqlite3 /opt/smail/data/smail.db ".schema protocol_credential"')
print(stdout.read().decode())
ssh.close()
