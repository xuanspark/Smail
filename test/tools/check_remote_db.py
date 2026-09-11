import paramiko
client = paramiko.SSHClient()
client.set_missing_host_key_policy(paramiko.AutoAddPolicy())
client.connect('47.108.78.12', port=22, username='root', password='10102442Lb!', timeout=8)
cmd = 'systemctl list-unit-files | grep -E "(mysql|mariadb|postgresql)"'
stdin, stdout, stderr = client.exec_command(cmd)
print("DB units:\n", stdout.read().decode('utf-8'))

cmd2 = 'sqlite3 /opt/smail/data/smail.db "SELECT id, account, name, role, is_admin FROM user;"'
stdin, stdout, stderr = client.exec_command(cmd2)
print("User table:\n", stdout.read().decode('utf-8'))

cmd3 = 'sqlite3 /opt/smail/data/smail.db "SELECT count(*) FROM external_email;"'
stdin, stdout, stderr = client.exec_command(cmd3)
print("External email count:\n", stdout.read().decode('utf-8'))

client.close()
