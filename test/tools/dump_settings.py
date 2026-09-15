p = 'backend/controllers/settings.go'
with open(p, 'rb') as f:
    data = f.read()

idx = data.find(b'type modifyPasswordRequest struct')
idx_end = data.find(b'if retData.CurrentPassword == "', idx)
print(data[idx:idx_end].decode('utf-8'))