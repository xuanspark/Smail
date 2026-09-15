import paramiko
ssh = paramiko.SSHClient()
ssh.set_missing_host_key_policy(paramiko.AutoAddPolicy())
ssh.connect('47.108.78.12', port=22, username='root', password='10102442Lb!', timeout=10)
stdin, stdout, stderr = ssh.exec_command("sqlite3 /opt/smail/data/smail.db \"SELECT * FROM client_credential;\"")
print('client_credential:\n', stdout.read().decode())
stdin, stdout, stderr = ssh.exec_command("sqlite3 /opt/smail/data/smail.db \"SELECT * FROM protocol_credential;\"")
print('protocol_credential:\n', stdout.read().decode())
ssh.close()
