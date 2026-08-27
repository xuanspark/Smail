package access

type Role string

const (
	RoleSuperAdmin Role = "super_admin"
	RoleAdmin      Role = "admin"
	RoleUser       Role = "user"
)

func (r Role) Valid() bool {
	switch r {
	case RoleSuperAdmin, RoleAdmin, RoleUser:
		return true
	default:
		return false
	}
}
