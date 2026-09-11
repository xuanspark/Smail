import paramiko

ssh = paramiko.SSHClient()
ssh.set_missing_host_key_policy(paramiko.AutoAddPolicy())
ssh.connect("47.108.78.12", port=22, username="root", password="10102442Lb!")

sftp = ssh.open_sftp()
with sftp.open("/tmp/test_api.py", "w") as f:
    f.write("""import requests
s = requests.Session()
r = s.post('http://127.0.0.1/api/login', json={'account': 'xuans', 'password': '12345678'}, headers={'Content-Type': 'application/json'})
print('LOGIN:', r.status_code, r.text)
print('COOKIES:', s.cookies.get_dict())

r2 = s.get('http://127.0.0.1/api/v1/config/status')
print('CONFIG_STATUS:', r2.status_code, r2.text)

r3 = s.get('http://127.0.0.1/api/v1/config/section/ssl')
print('SSL_SECTION:', r3.status_code, r3.text)
""")
sftp.close()

stdin, stdout, stderr = ssh.exec_command("python3 /tmp/test_api.py")
print("STDOUT:\n", stdout.read().decode('utf-8'))
print("STDERR:\n", stderr.read().decode('utf-8'))
ssh.close()
