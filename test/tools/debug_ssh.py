import paramiko

ssh = paramiko.SSHClient()
ssh.set_missing_host_key_policy(paramiko.RejectPolicy())
from remote_ssh import connect_remote
connect_remote(ssh)

def run(cmd):
    stdin, stdout, stderr = ssh.exec_command(cmd)
    return stdout.read().decode('utf-8', errors='replace'), stderr.read().decode('utf-8', errors='replace')

out, _ = run('ls -la /opt/smail/log')
print('=== /opt/smail/log ===\n' + out)

out, _ = run("grep -rn 'qq.com' /opt/smail/log/ 2>/dev/null")
print('=== qq.com grep in logs ===\n' + out)

out, _ = run("sqlite3 /opt/smail/data/smail.db '.schema email'")
print('=== schema email ===\n' + out)

out, _ = run("sqlite3 /opt/smail/data/smail.db '.schema user_email'")
print('=== schema user_email ===\n' + out)

out, _ = run("sqlite3 /opt/smail/data/smail.db 'SELECT id, subject, send_date FROM email ORDER BY id DESC LIMIT 5;'")
print('=== latest emails ===\n' + out)

out, _ = run("sqlite3 /opt/smail/data/smail.db 'SELECT id, user_id, email_id, is_read FROM user_email ORDER BY id DESC LIMIT 5;'")
print('=== latest user_emails ===\n' + out)

ssh.close()
