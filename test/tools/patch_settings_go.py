p = 'backend/controllers/settings.go'
with open(p, 'rb') as f:
    data = f.read()

# 1. Add imports for verification & externalemail
t_import = b'\tgit.pcux.cn/xuans/Smail/services/auth\n'
r_import = b'\tgit.pcux.cn/xuans/Smail/services/auth\n\tgit.pcux.cn/xuans/Smail/services/externalemail\n\tgit.pcux.cn/xuans/Smail/services/verification\n'
assert t_import in data, 't_import not found'
data = data.replace(t_import, r_import, 1)

# 2. Update modifyPasswordRequest struct
idx_struct = data.find(b'type modifyPasswordRequest struct {')
idx_struct_end = data.find(b'}\n\nfunc ModifyPassword', idx_struct)
assert idx_struct != -1 and idx_struct_end != -1
new_struct = b'''type modifyPasswordRequest struct {
\tCurrentPassword string json:current_password
\tPassword        string json:password
\tCode            string json:code
\tChallengeID     string json:challenge_id
}'''
data = data[:idx_struct] + new_struct + data[idx_struct_end+1:]

# 3. Replace super admin block with comment
idx_super = data.find(b'if ctx.EffectiveRole() == access.RoleSuperAdmin {')
assert idx_super != -1, 'idx_super not found'
idx_super_end = data.find(b'var retData modifyPasswordRequest', idx_super)
assert idx_super_end != -1, 'idx_super_end not found'

r_super = '''// 无论是普通用户还是管理员、超级管理员均支持修改密码，超级管理员必须通过绑定的外部邮箱验证码进行核验
\t'''.encode('utf-8')
data = data[:idx_super] + r_super + data[idx_super_end:]

# 4. In ModifyPassword, after authenticating current password and before updating password, check code for super_admin
t_auth_ok = b'\tloginPairAttempts.clear(pairKey)\n\tloginAccountAttempts.clear(accountKey)\n'
r_auth_ok = '''\tloginPairAttempts.clear(pairKey)
\tloginAccountAttempts.clear(accountKey)

\tif ctx.EffectiveRole() == access.RoleSuperAdmin {
\t\truntime := currentSecurityRuntime()
\t\tif runtime.external == nil || runtime.verification == nil {
\t\t\tresponse.NewErrorResponse(response.ServerError, 安全验证服务不可用, ").FPrint(w)
\t\t\treturn
\t\t}
\t\tbinding, err := runtime.external.FindVerifiedPrimaryByUser(ctx.UserID)
\t\tif err != nil {
\t\t\tresponse.NewErrorResponse(response.ParamsError, 超级管理员必须先绑定外部安全邮箱才能修改密码, ).FPrint(w)
\t\t\treturn
\t\t}
\t\texternalEmail, err := runtime.external.Decrypt(*binding)
\t\tif err != nil {
\t\t\tresponse.NewErrorResponse(response.ServerError, 读取外部安全邮箱失败, ).FPrint(w)
\t\t\treturn
\t\t}
\t\tif strings.TrimSpace(retData.Code) ==  || strings.TrimSpace(retData.ChallengeID) ==  {
\t\t\tresponse.NewErrorResponse(response.ParamsError, 超级管理员修改密码必须提供邮箱验证码, ).FPrint(w)
\t\t\treturn
\t\t}
\t\tif err := runtime.verification.Verify(retData.ChallengeID, ctx.UserAccount, externalEmail, retData.Code, verification.PurposePasswordChange, remoteIP(req)); err != nil {
\t\t\tresponse.NewErrorResponse(response.ParamsError, 验证码错误或已过期, ).FPrint(w)
\t\t\treturn
\t\t}
\t}
'''.encode('utf-8')
assert t_auth_ok in data, 't_auth_ok not found'
data = data.replace(t_auth_ok, r_auth_ok, 1)

with open(p, 'wb') as f:
 f.write(data)
print('settings.go patched successfully')