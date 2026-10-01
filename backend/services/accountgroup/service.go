package accountgroup

import (
	"errors"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/access"
	"xorm.io/xorm"
)

var (
	ErrDenied   = errors.New("account group scope denied")
	ErrInvalid  = errors.New("invalid account group")
	ErrConflict = errors.New("account group membership changed")
)

// ScopePredicate 的外层必须是用户表。使用实时子查询，不缓存管理员的分组范围。
// 未建立成员关系的旧账号以及新账号都属于默认分组，无需迁移或复制账号数据。
func ScopePredicate(actorID int) (string, []any) {
	return `((id IN (
		SELECT gm.user_id FROM account_group_member gm
		JOIN account_group_member am ON gm.group_id = am.group_id
		WHERE am.user_id = ?
	) OR ((id NOT IN (SELECT user_id FROM account_group_member)
		OR id IN (SELECT user_id FROM account_group_member WHERE group_id = 0))
		AND (NOT EXISTS (SELECT 1 FROM account_group_member WHERE user_id = ?)
		OR EXISTS (SELECT 1 FROM account_group_member WHERE user_id = ? AND group_id = 0))))
		AND NOT ` + models.SuperAdminPredicate + `)`,
		[]any{actorID, actorID, actorID}
}

func ApplyScope(query *xorm.Session, actorID int, role access.Role) {
	if actorID <= 0 {
		query.And("1 = 0")
		return
	}
	switch role {
	case access.RoleSuperAdmin:
		return
	case access.RoleAdmin:
		predicate, args := ScopePredicate(actorID)
		query.And(predicate, args...)
	default:
		query.And("id = ?", actorID)
	}
}

func ApplyGroup(query *xorm.Session, groupID int) {
	if groupID == 0 {
		query.And("(id NOT IN (SELECT user_id FROM account_group_member) OR id IN (SELECT user_id FROM account_group_member WHERE group_id = 0))")
	} else {
		query.And("id IN (SELECT user_id FROM account_group_member WHERE group_id = ?)", groupID)
	}
}

func CanAccess(engine *xorm.Engine, actorID int, role access.Role, targetID int) bool {
	if engine == nil || actorID <= 0 || targetID <= 0 {
		return false
	}
	query := engine.Table(&models.User{}).Where("id = ?", targetID)
	defer query.Close()
	ApplyScope(query, actorID, role)
	found, err := query.Exist(&models.User{})
	return err == nil && found
}

func Memberships(engine *xorm.Engine, userIDs []int) (map[int][]int, error) {
	result := make(map[int][]int, len(userIDs))
	if engine == nil {
		return nil, ErrDenied
	}
	if len(userIDs) == 0 {
		return result, nil
	}
	var rows []models.AccountGroupMember
	if err := engine.In("user_id", userIDs).Asc("group_id").Find(&rows); err != nil {
		return nil, err
	}
	for _, row := range rows {
		result[row.UserID] = append(result[row.UserID], row.GroupID)
	}
	for _, id := range userIDs {
		if len(result[id]) == 0 {
			result[id] = []int{0}
		}
	}
	return result, nil
}

func VisibleGroups(engine *xorm.Engine, actorID int, role access.Role) ([]models.AccountGroup, error) {
	if engine == nil || actorID <= 0 || (role != access.RoleAdmin && role != access.RoleSuperAdmin) {
		return nil, ErrDenied
	}
	query := engine.Table(&models.AccountGroup{})
	defer query.Close()
	showDefault := role == access.RoleSuperAdmin
	if role == access.RoleAdmin {
		members, err := Memberships(engine, []int{actorID})
		if err != nil {
			return nil, err
		}
		ids := members[actorID]
		query.In("id", ids)
		for _, id := range ids {
			if id == 0 {
				showDefault = true
			}
		}
	}
	groups := make([]models.AccountGroup, 0)
	if err := query.Asc("id").Find(&groups); err != nil {
		return nil, err
	}
	if showDefault {
		groups = append([]models.AccountGroup{{ID: 0, Name: "默认分组"}}, groups...)
	}
	return groups, nil
}

func NormalizeName(value string) (string, error) {
	name := strings.TrimSpace(value)
	if name == "" || name == "默认分组" || !utf8.ValidString(name) || utf8.RuneCountInString(name) > 32 {
		return "", ErrInvalid
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return "", ErrInvalid
		}
	}
	return name, nil
}

// ValidateMembership 必须在持有目标用户写锁的事务内调用，避免降级和分组变更并发突破单组约束。
func ValidateMembership(tx *xorm.Session, role access.Role, input []int) ([]int, error) {
	if len(input) > 100 {
		return nil, ErrInvalid
	}
	ids := make([]int, 0, len(input))
	seen := map[int]bool{}
	for _, id := range input {
		if id < 0 || seen[id] {
			return nil, ErrInvalid
		}
		seen[id] = true
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		ids = []int{0}
	}
	if role != access.RoleAdmin && len(ids) != 1 {
		return nil, ErrInvalid
	}
	for _, id := range ids {
		if id == 0 {
			continue
		}
		found, err := tx.ID(id).Exist(&models.AccountGroup{})
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, ErrInvalid
		}
	}
	sort.Ints(ids)
	return ids, nil
}

// MembershipScopeGuard 限制管理员只能把成员放入自己所属的分组。
func MembershipScopeGuard(actorID int, role access.Role, input []int) WriteGuard {
	return func(tx *xorm.Session) error {
		if tx == nil || actorID <= 0 || !role.Valid() {
			return ErrDenied
		}
		if role == access.RoleSuperAdmin {
			return nil
		}
		if role != access.RoleAdmin {
			return ErrDenied
		}
		allowedRows := make([]models.AccountGroupMember, 0)
		if err := tx.Where("user_id = ?", actorID).Find(&allowedRows); err != nil {
			return err
		}
		allowed := make(map[int]struct{}, len(allowedRows))
		for _, row := range allowedRows {
			allowed[row.GroupID] = struct{}{}
		}
		if len(allowed) == 0 {
			allowed[0] = struct{}{}
		}
		for _, groupID := range input {
			if _, ok := allowed[groupID]; !ok {
				return ErrDenied
			}
		}
		return nil
	}
}

func ReplaceMembership(tx *xorm.Session, userID int, ids []int) error {
	if _, err := tx.Where("user_id = ?", userID).Delete(&models.AccountGroupMember{}); err != nil {
		return err
	}
	if len(ids) == 1 && ids[0] == 0 {
		return nil
	}
	rows := make([]models.AccountGroupMember, 0, len(ids))
	for _, id := range ids {
		rows = append(rows, models.AccountGroupMember{UserID: userID, GroupID: id})
	}
	_, err := tx.Insert(&rows)
	return err
}

type WriteGuard func(*xorm.Session) error

// OwnGuard 只锁定并复核当前账号，不授予管理本人或其他账号的能力。
func OwnGuard(actorID int, role access.Role, sessionVersion, securityVersion int) WriteGuard {
	return func(tx *xorm.Session) error {
		if tx == nil || actorID <= 0 || !role.Valid() {
			return ErrDenied
		}
		if _, err := tx.ID(actorID).SetExpr("session_version", "session_version").Update(&models.User{}); err != nil {
			return err
		}
		var current models.User
		found, err := tx.ID(actorID).Get(&current)
		if err != nil {
			return err
		}
		if !found || current.Disabled != 0 || current.MustChangePassword != 0 || current.EffectiveRole() != role ||
			current.SessionVersion != sessionVersion || current.SecurityVersion != securityVersion {
			return ErrDenied
		}
		return nil
	}
}

func CheckWrite(tx *xorm.Session, guards ...WriteGuard) error {
	for _, guard := range guards {
		if guard == nil {
			return ErrDenied
		}
		if err := guard(tx); err != nil {
			return err
		}
	}
	return nil
}

func Guard(actorID int, role access.Role, sessionVersion, securityVersion int, target models.User) WriteGuard {
	return func(tx *xorm.Session) error {
		if tx == nil || actorID <= 0 || target.ID <= 0 || (role != access.RoleAdmin && role != access.RoleSuperAdmin) {
			return ErrDenied
		}
		ids := []int{actorID}
		if target.ID != actorID {
			ids = append(ids, target.ID)
		}
		sort.Ints(ids)
		for _, id := range ids {
			if _, err := tx.ID(id).SetExpr("session_version", "session_version").Update(&models.User{}); err != nil {
				return err
			}
		}
		var actor models.User
		found, err := tx.ID(actorID).Get(&actor)
		if err != nil {
			return err
		}
		if !found || actor.Disabled != 0 || actor.MustChangePassword != 0 || actor.EffectiveRole() != role ||
			actor.SessionVersion != sessionVersion || actor.SecurityVersion != securityVersion {
			return ErrDenied
		}
		var current models.User
		query := tx.ID(target.ID)
		ApplyScope(query, actorID, role)
		found, err = query.Get(&current)
		if err != nil {
			return err
		}
		if !found {
			return ErrDenied
		}
		if current.SessionVersion != target.SessionVersion || current.SecurityVersion != target.SecurityVersion ||
			current.EffectiveRole() != target.EffectiveRole() || current.Disabled != target.Disabled || current.MustChangePassword != target.MustChangePassword {
			return ErrConflict
		}
		return nil
	}
}

func SetMembership(engine *xorm.Engine, user models.User, input []int, guards ...WriteGuard) error {
	if engine == nil || user.ID <= 0 {
		return ErrDenied
	}
	tx := engine.NewSession()
	defer tx.Close()
	if err := tx.Begin(); err != nil {
		return err
	}
	defer tx.Rollback()
	if err := CheckWrite(tx, guards...); err != nil {
		return err
	}
	updated := user
	updated.SessionVersion++
	updated.SecurityVersion++
	n, err := tx.ID(user.ID).Where("session_version = ? AND security_version = ?", user.SessionVersion, user.SecurityVersion).
		Cols("session_version", "security_version").Update(&updated)
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrConflict
	}
	ids, err := ValidateMembership(tx, user.EffectiveRole(), input)
	if err != nil {
		return err
	}
	if err := ReplaceMembership(tx, user.ID, ids); err != nil {
		return err
	}
	return tx.Commit()
}
