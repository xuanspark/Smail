p = 'backend/services/verification/delivery.go'
with open(p, 'rb') as f:
    data = f.read()

t = 'case PurposePasswordRecover:\n\t\tpurposeText = "完成密码恢复验证"'.encode('utf-8')
r = 'case PurposePasswordRecover:\n\t\tpurposeText = "完成密码恢复验证"\n\tcase PurposePasswordChange:\n\t\tpurposeText = "完成修改密码安全验证"'.encode('utf-8')

assert t in data, 'target not found in delivery.go'
data = data.replace(t, r, 1)

with open(p, 'wb') as f:
    f.write(data)
print('updated delivery.go')