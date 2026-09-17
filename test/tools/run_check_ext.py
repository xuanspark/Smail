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
c.execute("SELECT id, user_id, status, email_type, created_at, updated_at FROM external_email")
print("external_email rows:", c.fetchall())
'''
sftp = ssh.open_sftp()
with sftp.file('/tmp/check_external_email.py', 'w') as f:
    f.write(s)
sftp.close()

_, out, err = ssh.exec_command('python3 /tmp/check_external_email.py')
print('OUT:\n', out.read().decode('utf-8', errors='ignore'))
ssh.close()
