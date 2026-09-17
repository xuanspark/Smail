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
c.execute("SELECT name FROM sqlite_master WHERE type='table';")
print('Tables:', [r[0] for r in c.fetchall()])

c.execute("SELECT * FROM operation_log ORDER BY id DESC LIMIT 5;")
print('Recent op logs:', c.fetchall())

c.execute("SELECT * FROM mail_log ORDER BY id DESC LIMIT 5;")
print('Recent mail logs:', c.fetchall())
'''
sftp = ssh.open_sftp()
with sftp.file('/tmp/check_logs.py', 'w') as f:
    f.write(s)
sftp.close()

_, out, err = ssh.exec_command('python3 /tmp/check_logs.py')
print('OUT:\n', out.read().decode('utf-8', errors='ignore'))
ssh.close()
