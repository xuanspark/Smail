import paramiko
ssh = paramiko.SSHClient()
ssh.set_missing_host_key_policy(paramiko.AutoAddPolicy())
ssh.connect('47.108.78.12', port=22, username='root', password='10102442Lb!', timeout=10)
_, out, _ = ssh.exec_command('ss -tulpn; ps aux | grep smail')
print(out.read().decode('utf-8', errors='ignore'))
ssh.close()