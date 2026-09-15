p = 'backend/controllers/settings.go'
with open(p, 'rb') as f:
    data = f.read()

idx = data.find(b'if ctx.EffectiveRole() == access.RoleSuperAdmin {')
print(data[idx:idx+150])