package session

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/access"
)

const userSessionKey = "user"
const recentAuthenticationLifetime = 10 * time.Minute

type UserIdentity struct {
	ID                 int         `json:"id"`
	Name               string      `json:"name"`
	Account            string      `json:"account"`
	Role               access.Role `json:"role"`
	IsAdmin            int         `json:"is_admin"`
	SessionVersion     int         `json:"session_version"`
	SecurityVersion    int         `json:"security_version"`
	MustChangePassword int         `json:"must_change_password"`
	AuthenticatedAt    int64       `json:"authenticated_at,omitempty"`
}

// HasRecentAuthentication 只信任完成登录时写入的服务端时间，刷新不会续期。
// 旧会话缺少时间或时间来自未来时必须重新登录，不能自动获得高风险权限。
func (identity UserIdentity) HasRecentAuthentication(now time.Time) bool {
	if identity.ID <= 0 || identity.AuthenticatedAt <= 0 {
		return false
	}
	authenticated := time.Unix(identity.AuthenticatedAt, 0)
	return !now.Before(authenticated) && now.Before(authenticated.Add(recentAuthenticationLifetime))
}

func PutUser(ctx context.Context, user *models.User) error {
	if Instance == nil || user == nil || user.ID <= 0 {
		return errors.New("session user is invalid")
	}
	role := user.EffectiveRole()
	encoded, err := json.Marshal(UserIdentity{
		ID:                 user.ID,
		Name:               user.Name,
		Account:            user.Account,
		Role:               role,
		IsAdmin:            user.IsAdmin,
		SessionVersion:     user.SessionVersion,
		SecurityVersion:    user.SecurityVersion,
		MustChangePassword: user.MustChangePassword,
		AuthenticatedAt:    time.Now().UTC().Unix(),
	})
	if err != nil {
		return err
	}
	Instance.Put(ctx, userSessionKey, string(encoded))
	return nil
}

func GetUser(ctx context.Context) (UserIdentity, error) {
	var identity UserIdentity
	if Instance == nil {
		return identity, errors.New("session manager is unavailable")
	}
	encoded := Instance.GetString(ctx, userSessionKey)
	if encoded == "" {
		return identity, nil
	}
	if err := json.Unmarshal([]byte(encoded), &identity); err != nil {
		return UserIdentity{}, err
	}
	if !identity.Role.Valid() {
		if identity.IsAdmin == 1 {
			identity.Role = access.RoleSuperAdmin
		} else {
			identity.Role = access.RoleUser
		}
	}
	return identity, nil
}
