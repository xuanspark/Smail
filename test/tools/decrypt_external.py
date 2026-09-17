import paramiko
ssh = paramiko.SSHClient()
ssh.set_missing_host_key_policy(paramiko.RejectPolicy())
from remote_ssh import connect_remote
connect_remote(ssh)

s = '''import sqlite3, base64, glob
from cryptography.hazmat.primitives.ciphers.aead import AESGCM

conn = sqlite3.connect('/opt/smail/data/smail.db')
c = conn.cursor()
c.execute('SELECT id, user_id, email_ciphertext, status, email_type FROM external_email')
rows = c.fetchall()

key_files = glob.glob('/opt/smail/ssl/external-email-encryption/*.key')
print('Key files:', key_files)
if key_files:
    with open(key_files[0], 'rb') as f:
        key = f.read()
    aesgcm = AESGCM(key)
    for r in rows:
        enc_str = r[2]
        parts = enc_str.split(':')
        b64 = parts[3]
        padded = b64 + '=' * (-len(b64) % 4)
        raw = base64.urlsafe_b64decode(padded)
        nonce = raw[:12]
        ct = raw[12:]
        try:
            pt = aesgcm.decrypt(nonce, ct, b'smail-external-email-v1')
            print(f'User {r[1]} ({r[4]}, {r[3]}): {pt.decode()}')
        except Exception as e:
            print(f'User {r[1]} decrypt error: {e}')
'''
sftp = ssh.open_sftp()
with sftp.file('/tmp/decrypt_emails.py', 'w') as f:
    f.write(s)
sftp.close()
_, out, err = ssh.exec_command('python3 /tmp/decrypt_emails.py')
print('OUT:\n', out.read().decode())
print('ERR:\n', err.read().decode())
ssh.close()
