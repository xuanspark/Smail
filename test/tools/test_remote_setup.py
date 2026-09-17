import paramiko
client = paramiko.SSHClient()
client.set_missing_host_key_policy(paramiko.RejectPolicy())
from remote_ssh import connect_remote
connect_remote(client)
client.exec_command('systemctl stop smail-init; pkill -9 smail')

setup_script = '''#!/bin/bash
export SMAIL_SETUP_HOST=0.0.0.0
export SMAIL_SETUP_ALLOWED_CIDRS=0.0.0.0/0
export SMAIL_SETUP_TOKEN=setup-secret-token-1234567890123456
cd /opt/smail
exec ./smail -p 80
'''
sftp = client.open_sftp()
with sftp.file('/opt/smail/start-setup.sh', 'w') as f:
    f.write(setup_script)
sftp.close()

cmd2 = 'chmod +x /opt/smail/start-setup.sh; nohup /opt/smail/start-setup.sh > /opt/smail/log/init.log 2>&1 & sleep 2; ss -tulnp | grep 80'
stdin, stdout, stderr = client.exec_command(cmd2)
print(stdout.read().decode('utf-8'))
client.close()
