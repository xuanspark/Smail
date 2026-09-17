import paramiko
ssh = paramiko.SSHClient()
ssh.set_missing_host_key_policy(paramiko.RejectPolicy())
from remote_ssh import connect_remote
connect_remote(ssh)
stdin, stdout, stderr = ssh.exec_command('tail -n 40 /opt/smail/log/runtime-2026-09-14.log')
print("Grep 6567567:\n", stdout.read().decode())
stdin, stdout, stderr = ssh.exec_command('tail -n 40 /opt/smail/log/stdout.log')
print("Grep QQ number:\n", stdout.read().decode())
stdin, stdout, stderr = ssh.exec_command('sqlite3 /opt/smail/data/smail.db "SELECT user_id, email, verified FROM external_email;"')
print("External emails:\n", stdout.read().decode())
ssh.close()


