import paramiko
ssh = paramiko.SSHClient()
ssh.set_missing_host_key_policy(paramiko.RejectPolicy())
from remote_ssh import connect_remote
connect_remote(ssh)
stdin, stdout, stderr = ssh.exec_command("tail -n 25 /opt/smail/log/stdout.log")
print(stdout.read().decode("utf-8", errors="ignore"))
stdin, stdout, stderr = ssh.exec_command("ps aux | grep smail")
print(stdout.read().decode("utf-8", errors="ignore"))
ssh.close()