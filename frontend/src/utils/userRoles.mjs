const roleOptions = {
  super_admin: { label: '超级管理员', type: 'danger' },
  admin: { label: '管理员', type: 'primary' },
  user: { label: '普通用户', type: 'info' }
}

export function roleMeta(role) {
  return roleOptions[role] || { label: '未知权限', type: 'warning' }
}

export function assignableRoles(actorRole) {
  const roles = actorRole === 'super_admin'
    ? ['user', 'admin']
    : actorRole === 'admin' ? ['user'] : []
  return roles.map(value => ({ value, ...roleMeta(value) }))
}

export function canManageUser(actorRole, targetRole) {
  return assignableRoles(actorRole).some(option => option.value === targetRole)
}
