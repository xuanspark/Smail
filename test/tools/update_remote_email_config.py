import paramiko
ssh = paramiko.SSHClient()
ssh.set_missing_host_key_policy(paramiko.AutoAddPolicy())
ssh.connect("47.108.78.12", port=22, username="root", password="10102442Lb!", timeout=10)

cmd = """python3 -c '
import json
with open("/opt/smail/config/email.json", "r") as f:
    d = json.load(f)
d["policy"]["externalRecoveryEnabled"] = True
with open("/opt/smail/config/email.json", "w") as f:
    json.dump(d, f, indent=2)
'
"""
stdin, stdout, stderr = ssh.exec_command(cmd)
print("Stderr:", stderr.read().decode())

stdin, stdout, stderr = ssh.exec_command("cat /opt/smail/config/email.json")
print("email.json:\n", stdout.read().decode())

stdin, stdout, stderr = ssh.exec_command("killall -9 smail")
print("Killall:", stdout.read().decode())
ssh.close()
