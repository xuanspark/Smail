import paramiko
ssh = paramiko.SSHClient()
ssh.set_missing_host_key_policy(paramiko.RejectPolicy())
from remote_ssh import connect_remote
connect_remote(ssh)
stdin, stdout, stderr = ssh.exec_command("echo HELLO")
print('stdout:', stdout.read().decode('utf-8', errors='replace'))
print('stderr:', stderr.read().decode('utf-8', errors='replace'))
ssh.close()
