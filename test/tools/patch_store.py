p = 'backend/services/verification/store.go'
with open(p, 'rb') as f:
    data = f.read()

t1 = b'PurposePasswordRecover     Purpose = "password_recover"\n'
r1 = b'PurposePasswordRecover     Purpose = "password_recover"\n\tPurposePasswordChange      Purpose = "password_change"\n'
assert t1 in data
data = data.replace(t1, r1, 1)

t2 = b'case PurposeLogin, PurposePasswordRecover, PurposeExternalEmailBind, PurposeExternalEmailChange:'
r2 = b'case PurposeLogin, PurposePasswordRecover, PurposePasswordChange, PurposeExternalEmailBind, PurposeExternalEmailChange:'
assert t2 in data
data = data.replace(t2, r2, 1)

with open(p, 'wb') as f:
    f.write(data)
print('updated store.go')