import paramiko
ssh = paramiko.SSHClient()
ssh.set_missing_host_key_policy(paramiko.RejectPolicy())
from remote_ssh import connect_remote
connect_remote(ssh)

s = '''
import sqlite3, base64, glob
from cryptography.hazmat.primitives.ciphers.aead import AESGCM

key_files = glob.glob('/opt/smail/ssl/**/credential.key', recursive=True) + glob.glob('/opt/smail/config/**/credential.key', recursive=True)
print('Key files:', key_files)
if not key_files:
    exit(1)

with open(key_files[0], 'rb') as f:
    key = f.read()

aesgcm = AESGCM(key)

conn = sqlite3.connect('/opt/smail/data/smail.db')
c = conn.cursor()

print('=== CLIENT CREDENTIALS ===')
c.execute('SELECT id, user_id, client_name, code_ciphertext, revoked_at, last_used_at, created_at FROM client_credential')
for row in c.fetchall():
    cid, uid, cname, enc_code, revoked, last_used, created = row
    if not enc_code:
        print(f'ID {cid} (UID {uid}, {cname}): None (revoked={revoked})')
        continue
    parts = enc_code.split(':')
    b64 = parts[2]
    padded = b64 + '=' * (-len(b64) % 4)
    raw = base64.urlsafe_b64decode(padded)
    nonce = raw[:12]
    ct = raw[12:]
    try:
        pt = aesgcm.decrypt(nonce, ct, b'smail-config-v1')
        print(f'ID {cid} (UID {uid}, {cname}): {pt.decode()} | created={created} | revoked={revoked} | last_used={last_used}')
    except Exception as e:
        print(f'ID {cid} decrypt err: {e}')

print('=== PROTOCOL CREDENTIALS ===')
c.execute('SELECT id, user_id, protocol, enabled, code_ciphertext, last_used_at, created_at FROM protocol_credential')
for row in c.fetchall():
    pid, uid, proto, enabled, enc_code, last_used, created = row
    if not enc_code:
        print(f'Proto {proto} (UID {uid}, enabled={enabled}): None')
        continue
    parts = enc_code.split(':')
    b64 = parts[2]
    padded = b64 + '=' * (-len(b64) % 4)
    raw = base64.urlsafe_b64decode(padded)
    nonce = raw[:12]
    ct = raw[12:]
    try:
        pt = aesgcm.decrypt(nonce, ct, b'smail-config-v1')
        print(f'Proto {proto} (UID {uid}, enabled={enabled}): {pt.decode()} | created={created} | last_used={last_used}')
    except Exception as e:
        print(f'Proto {proto} decrypt err: {e}')
'''

sftp = ssh.open_sftp()
with sftp.file('/tmp/decrypt_creds.py', 'w') as f:
    f.write(s)
sftp.close()

_, out, err = ssh.exec_command('python3 /tmp/decrypt_creds.py')
print('OUT:\n', out.read().decode('utf-8', errors='ignore'))
print('ERR:\n', err.read().decode('utf-8', errors='ignore'))
ssh.close()
