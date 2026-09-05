package session

import (
	"context"
	"encoding/json"
	"errors"

	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/access"
)

const userSessionKey = "user"

type UserIdentity struct {
	ID                 int         `json:"id"`
	Name               string      `json:"name"`
	Account            string      `json:"account"`
	Role               access.Role `json:"role"`
	IsAdmin            int         `json:"is_admin"`
	SessionVersion     int         `json:"session_version"`
	SecurityVersion    int         `json:"security_version"`
	MustChangePassword int         `json:"must_change_password"`
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
