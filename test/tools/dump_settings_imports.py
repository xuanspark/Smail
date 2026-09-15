p = 'backend/controllers/settings.go'
with open(p, 'rb') as f:
    data = f.read()

idx = data.find(b'import (')
idx_end = data.find(b')\n', idx)
print(data[idx:idx_end+2].decode('utf-8'))