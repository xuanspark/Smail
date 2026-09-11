import paramiko
client = paramiko.SSHClient()
client.set_missing_host_key_policy(paramiko.AutoAddPolicy())
client.connect('47.108.78.12', port=22, username='root', password='10102442Lb!', timeout=8)
cmd = 'systemctl list-unit-files | grep -E (mysql|mariadb|postgresql)'
stdin, stdout, stderr = client.exec_command(cmd)
print(DB units:, stdout.read().decode('utf-8'))
client.close()
