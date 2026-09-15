p = 'backend/services/recovery/service.go'
with open(p, 'rb') as f:
    data = f.read()

t = b'func (s *Service) loadRecoverableUser(identifier string) (*models.User, error) {\n\taccount, err := auth.NormalizeIdentifier(identifier)\n\tif err != nil || account == "" {\n\t\treturn nil, ErrRecoveryDenied\n\t}\n\tvar user models.User\n\thas, err := s.engine.Where("account = ? AND disabled = 0", account).Get(&user)\n\tif err != nil || !has || user.EffectiveRole() == access.RoleSuperAdmin {\n\t\treturn nil, ErrRecoveryDenied\n\t}\n\treturn &user, nil\n}'
r = b'func (s *Service) loadRecoverableUser(identifier string) (*models.User, error) {\n\taccount, err := auth.NormalizeIdentifier(identifier)\n\tif err != nil || account == "" {\n\t\treturn nil, ErrRecoveryDenied\n\t}\n\tvar user models.User\n\thas, err := s.engine.Where("account = ? AND disabled = 0", account).Get(&user)\n\tif err != nil || !has {\n\t\treturn nil, ErrRecoveryDenied\n\t}\n\treturn &user, nil\n}'

assert t in data, 'loadRecoverableUser target not found'
data = data.replace(t, r, 1)

with open(p, 'wb') as f:
    f.write(data)
print('updated loadRecoverableUser in service.go')