p = 'backend/services/recovery/service.go'
with open(p, 'rb') as f:
    data = f.read()

# 1. In Begin (around line 199)
t1 = b'if !has || user.EffectiveRole() == access.RoleSuperAdmin {\n\t\treturn s.issueDecoyChallengeWithContext'
r1 = b'if !has {\n\t\treturn s.issueDecoyChallengeWithContext'
assert t1 in data, 't1 not found'
data = data.replace(t1, r1, 1)

# 2. In verifyEmailIdentity (around line 259)
t2 = b'if err != nil || !has || user.EffectiveRole() == access.RoleSuperAdmin || s.external == nil {\n\t\treturn nil, nil, ErrRecoveryDenied\n\t}'
r2 = b'if err != nil || !has || s.external == nil {\n\t\treturn nil, nil, ErrRecoveryDenied\n\t}'
assert t2 in data, 't2 not found'
data = data.replace(t2, r2, 1)

# 3. In CompleteWithContext (around line 349)
t3 = b'if err != nil || !has || user.Disabled != 0 || user.EffectiveRole() == access.RoleSuperAdmin || user.SessionVersion != item.SessionVersion {\n\t\treturn ErrRecoveryDenied\n\t}'
r3 = b'if err != nil || !has || user.Disabled != 0 || user.SessionVersion != item.SessionVersion {\n\t\treturn ErrRecoveryDenied\n\t}'
assert t3 in data, 't3 not found'
data = data.replace(t3, r3, 1)

with open(p, 'wb') as f:
    f.write(data)
print('updated service.go')