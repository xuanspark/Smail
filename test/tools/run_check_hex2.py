import paramiko
ssh = paramiko.SSHClient()
ssh.set_missing_host_key_policy(paramiko.AutoAddPolicy())
ssh.connect('47.108.78.12', port=22, username='root', password='10102442Lb!', timeout=10)

s = '''
import sqlite3

conn = sqlite3.connect('/opt/smail/data/smail.db')
c = conn.cursor()

c.execute('SELECT id, user_id, hex(client_name) FROM client_credential')
for row in c.fetchall():
    h = row[2]
    raw_bytes = bytes.fromhex(h)
    print('Raw hex:', row[0], row[1], h)
    for enc in ['utf-8', 'gbk', 'gb2312', 'latin1']:
        try:
            print(f'  {enc}: {raw_bytes.decode(enc)}')
        except Exception as e:
            pass
'''
sftp = ssh.open_sftp()
with sftp.file('/tmp/check_hex2.py', 'w') as f:
    f.write(s)
sftp.close()

_, out, err = ssh.exec_command('python3 /tmp/check_hex2.py')
print('OUT:\n', out.read().decode('utf-8', errors='ignore'))
ssh.close()
