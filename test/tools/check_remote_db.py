import paramiko
client = paramiko.SSHClient()
client.set_missing_host_key_policy(paramiko.RejectPolicy())
from remote_ssh import connect_remote
connect_remote(client)
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
