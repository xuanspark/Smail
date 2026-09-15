p = 'backend/controllers/security.go'
with open(p, 'rb') as f:
    data = f.read()

idx = data.find(b'func RequestVerification')
idx_end = data.find(b'// VerifyVerification', idx)
print(data[idx:idx_end])