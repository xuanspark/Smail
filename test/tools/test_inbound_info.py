import paramiko
ssh = paramiko.SSHClient()
ssh.set_missing_host_key_policy(paramiko.RejectPolicy())
from remote_ssh import connect_remote
connect_remote(ssh)
stdin, stdout, stderr = ssh.exec_command('python3 -c "import sqlite3; conn=sqlite3.connect(\x27/opt/smail/data/smail.db\x27); c=conn.cursor(); print(c.execute(\x27SELECT id, user_id, email_id, is_read, is_trash FROM user_email ORDER BY id DESC LIMIT 5\x27).fetchall())"')
print("User emails top 5:\n", stdout.read().decode())
stdin, stdout, stderr = ssh.exec_command('python3 -c "import sqlite3; conn=sqlite3.connect(\x27/opt/smail/data/smail.db\x27); c=conn.cursor(); print(c.execute(\x27SELECT id, subject, send_time FROM email ORDER BY id DESC LIMIT 5\x27).fetchall())"')
print("Emails top 5:\n", stdout.read().decode())
stdin, stdout, stderr = ssh.exec_command('python3 -c "import sqlite3; conn=sqlite3.connect(\x27/opt/smail/data/smail.db\x27); c=conn.cursor(); print(c.execute(\x27SELECT id, name, user_name FROM user\x27).fetchall())"')
print("Users:\n", stdout.read().decode())
ssh.close()






