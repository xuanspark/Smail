import paramiko
ssh = paramiko.SSHClient()
ssh.set_missing_host_key_policy(paramiko.AutoAddPolicy())
ssh.connect("47.108.78.12", port=22, username="root", password="10102442Lb!", timeout=10)
stdin, stdout, stderr = ssh.exec_command("tail -n 25 /opt/smail/log/stdout.log")
print(stdout.read().decode("utf-8", errors="ignore"))
stdin, stdout, stderr = ssh.exec_command("ps aux | grep smail")
print(stdout.read().decode("utf-8", errors="ignore"))
ssh.close()