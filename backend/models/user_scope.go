package models

// OrdinaryUserPredicate 与 EffectiveRole 的兼容规则一致，旧管理员不能混入普通用户范围。
const OrdinaryUserPredicate = "(role = 'user' OR ((role IS NULL OR role NOT IN ('user', 'admin', 'super_admin')) AND is_admin <> 1))"
