import paramiko
ssh = paramiko.SSHClient()
ssh.set_missing_host_key_policy(paramiko.AutoAddPolicy())
ssh.connect("47.108.78.12", port=22, username="root", password="10102442Lb!", timeout=10)
stdin, stdout, stderr = ssh.exec_command("echo HELLO")
print(stdout:, stdout.read().decode())
print(stderr:, stderr.read().decode())
ssh.close()
