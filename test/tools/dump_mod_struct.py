p = 'backend/controllers/settings.go'
with open(p, 'rb') as f:
    data = f.read()

idx = data.find(b'type modifyPasswordRequest struct')
print(repr(data[idx:idx+200]))