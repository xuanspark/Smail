import paramiko
ssh = paramiko.SSHClient()
ssh.set_missing_host_key_policy(paramiko.AutoAddPolicy())
ssh.connect('47.108.78.12', port=22, username='root', password='10102442Lb!', timeout=10)
sftp = ssh.open_sftp()
print('Uploading out/smail to /opt/smail/smail.new ...')
sftp.put('out/smail', '/opt/smail/smail.new')
sftp.close()

stdin, stdout, stderr = ssh.exec_command('chmod +x /opt/smail/smail.new && killall -9 smail; mv /opt/smail/smail.new /opt/smail/smail && cd /opt/smail && nohup ./smail -p 80 > log/runtime.log 2>&1 &')
print(stdout.read().decode())
print(stderr.read().decode())

import time
time.sleep(2)
stdin, stdout, stderr = ssh.exec_command('ps aux | grep smail')
print(stdout.read().decode())
ssh.close()
