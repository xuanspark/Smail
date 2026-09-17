# -*- coding: utf-8 -*-
import paramiko, time

ssh = paramiko.SSHClient()
ssh.set_missing_host_key_policy(paramiko.RejectPolicy())
from remote_ssh import connect_remote
connect_remote(ssh)

cmd = """python3 -c '
import hashlib, json
with open("/opt/smail/config/email.json", "rb") as f:
    raw = f.read()
digest = hashlib.sha256(raw).hexdigest()

with open("/opt/smail/config/config.json", "r") as f:
    cfg = json.load(f)

cfg["sections"]["email"]["sha256"] = digest
cfg["email"]["externalRecoveryEnabled"] = True

with open("/opt/smail/config/config.json", "w") as f:
    json.dump(cfg, f, indent=2)
'
"""
stdin, stdout, stderr = ssh.exec_command(cmd)
print("Stderr:", stderr.read().decode())

ssh.exec_command("cd /opt/smail && nohup ./smail -p 80 > /opt/smail/log/stdout.log 2>&1 &")
time.sleep(2)

stdin, stdout, stderr = ssh.exec_command("ps -ef | grep smail")
print("Process:\n", stdout.read().decode())

stdin, stdout, stderr = ssh.exec_command("tail -n 15 /opt/smail/log/stdout.log")
print("Stdout tail:\n", stdout.read().decode())

ssh.close()