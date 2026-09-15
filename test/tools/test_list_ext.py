p = 'backend/controllers/security.go'
with open(p, 'rb') as f:
    data = f.read()

idx = data.find(b'external-email/list')
if idx == -1:
    idx = data.find(b'external-email')
print('idx:', idx)
print(data[idx-100:idx+400].decode('utf-8', errors='replace'))