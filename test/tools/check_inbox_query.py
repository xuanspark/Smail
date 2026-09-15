import paramiko
ssh = paramiko.SSHClient()
ssh.set_missing_host_key_policy(paramiko.AutoAddPolicy())
ssh.connect("47.108.78.12", port=22, username="root", password="10102442Lb!", timeout=10)
stdin, stdout, stderr = ssh.exec_command('sqlite3 /opt/smail/data/smail.db "SELECT e.id, e.subject, e.type, ue.status, ue.group_id, ue.is_read FROM email e LEFT JOIN user_email ue ON e.id = ue.email_id WHERE ue.user_id = 1 AND ue.status = 0 AND e.type = 0 AND (ue.group_id = 0 OR ue.group_id = 1) ORDER BY e.id DESC;"')
print("Inbox query result:\n", stdout.read().decode())
stdin, stdout, stderr = ssh.exec_command('sqlite3 /opt/smail/data/smail.db "SELECT e.id, e.subject, e.type, e.status, e.error FROM email e WHERE e.send_user_id = 1 ORDER BY e.id DESC LIMIT 5;"')
print("Sent query result:\n", stdout.read().decode())
stdin, stdout, stderr = ssh.exec_command('sqlite3 /opt/smail/data/smail.db "SELECT user_id, email, verified FROM external_email;"')
print("External emails:\n", stdout.read().decode())
ssh.close()

