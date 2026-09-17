import sys, io
sys.stdout = io.TextIOWrapper(sys.stdout.buffer, encoding='utf-8')
import paramiko
ssh = paramiko.SSHClient()
ssh.set_missing_host_key_policy(paramiko.RejectPolicy())
from remote_ssh import connect_remote
connect_remote(ssh)

_, out, err = ssh.exec_command('python3 /tmp/check_hex2.py')
print('OUT:\n', out.read().decode('utf-8', errors='ignore'))
ssh.close()
