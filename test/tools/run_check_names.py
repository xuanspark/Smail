import paramiko
ssh = paramiko.SSHClient()
ssh.set_missing_host_key_policy(paramiko.RejectPolicy())
from remote_ssh import connect_remote
connect_remote(ssh)

s = '''
import sqlite3

conn = sqlite3.connect('/opt/smail/data/smail.db')
c = conn.cursor()

c.execute('SELECT id, user_id, client_name FROM client_credential')
for row in c.fetchall():
    print('Client credential:', row[0], row[1], repr(row[2]))

c.execute('SELECT id, account, name, role FROM user')
for row in c.fetchall():
    print('User:', row)
'''
sftp = ssh.open_sftp()
with sftp.file('/tmp/check_names.py', 'w') as f:
    f.write(s)
sftp.close()

_, out, err = ssh.exec_command('python3 /tmp/check_names.py')
print('OUT:\n', out.read().decode('utf-8', errors='ignore'))
ssh.close()
