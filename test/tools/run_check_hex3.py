import sys, io
sys.stdout = io.TextIOWrapper(sys.stdout.buffer, encoding='utf-8')
import paramiko
ssh = paramiko.SSHClient()
ssh.set_missing_host_key_policy(paramiko.AutoAddPolicy())
ssh.connect('47.108.78.12', port=22, username='root', password='10102442Lb!', timeout=10)

_, out, err = ssh.exec_command('python3 /tmp/check_hex2.py')
print('OUT:\n', out.read().decode('utf-8', errors='ignore'))
ssh.close()
