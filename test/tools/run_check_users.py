import sys, io
sys.stdout = io.TextIOWrapper(sys.stdout.buffer, encoding='utf-8')
import paramiko
ssh = paramiko.SSHClient()
ssh.set_missing_host_key_policy(paramiko.RejectPolicy())
from remote_ssh import connect_remote
connect_remote(ssh)

s = '''
import sqlite3
conn = sqlite3.connect('/opt/smail/data/smail.db')
c = conn.cursor()
c.execute('SELECT id, hex(account), hex(name), role FROM user')
for r in c.fetchall():
    print('User ID:', r[0], 'Role:', r[3])
    try:
        print('  account:', bytes.fromhex(r[1]).decode('utf-8'))
    except:
        pass
    try:
        print('  name:', bytes.fromhex(r[2]).decode('utf-8'))
    except:
        pass
'''
sftp = ssh.open_sftp()
with sftp.file('/tmp/check_users.py', 'w') as f:
    f.write(s)
sftp.close()

_, out, err = ssh.exec_command('python3 /tmp/check_users.py')
print('OUT:\n', out.read().decode('utf-8', errors='ignore'))
ssh.close()
