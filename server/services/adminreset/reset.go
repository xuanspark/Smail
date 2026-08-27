package adminreset

import (
	"errors"
	"fmt"

	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/access"
	"git.pcux.cn/xuans/Smail/services/auth"
	"git.pcux.cn/xuans/Smail/services/credentials"
	"xorm.io/xorm"
)

var (
	ErrSuperAdminNotFound  = errors.New("super administrator not found")
	ErrSuperAdminAmbiguous = errors.New("multiple super administrators found")
)

// ResetSuperAdmin 在主程序内完成超级管理员密码重置事务。
// smail-admin 只负责通过本机 IPC 传递一次性请求，不直接访问数据库。
func ResetSuperAdmin(engine *xorm.Engine, newPassword string, invalidators ...func(userID int, account string)) error {
	if engine == nil || newPassword == "" {
		return ErrInvalidRequest
	}
	passwordHash, err := auth.HashPassword(newPassword)
	if err != nil {
		return fmt.Errorf("hash reset password: %w", err)
	}
	defer clearPassword(&passwordHash)
	credentialService, err := credentials.NewService(engine)
	if err != nil {
		return fmt.Errorf("create credential service: %w", err)
	}

	session := engine.NewSession()
	if session == nil {
		return errors.New("create reset transaction failed")
	}
	defer session.Close()
	if err := session.Begin(); err != nil {
		return fmt.Errorf("begin reset transaction: %w", err)
	}
	rollback := true
	defer func() {
		if rollback {
			_ = session.Rollback()
		}
	}()

	var users []models.User
	if err := session.Find(&users); err != nil {
		return fmt.Errorf("load administrator: %w", err)
	}
	var targets []models.User
	for _, user := range users {
		if user.EffectiveRole() == access.RoleSuperAdmin {
			targets = append(targets, user)
		}
	}
	if len(targets) == 0 {
		return ErrSuperAdminNotFound
	}
	if len(targets) != 1 {
		return ErrSuperAdminAmbiguous
	}
	target := targets[0]
	version := target.SessionVersion + 1
	if version <= 0 {
		version = 1
	}
	updated := &models.User{Password: passwordHash, SessionVersion: version}
	affected, err := session.ID(target.ID).Where("session_version = ?", target.SessionVersion).Cols("password", "session_version").Update(updated)
	if err != nil {
		return fmt.Errorf("update super administrator: %w", err)
	}
	if affected != 1 {
		return errors.New("super administrator changed during reset")
	}
	// session_version 只会使目标超级管理员的旧会话失效，不影响其他用户。
	// 密码重置必须在同一事务内撤销全部恢复凭据，避免任一环节失败后留下半完成状态。
	if err := credentialService.RevokeAllWithSession(session, target.ID); err != nil {
		return fmt.Errorf("revoke recovery credentials: %w", err)
	}
	if err := session.Commit(); err != nil {
		return fmt.Errorf("commit reset transaction: %w", err)
	}
	rollback = false
	for _, invalidate := range invalidators {
		if invalidate != nil {
			invalidate(target.ID, target.Account)
		}
	}
	return nil
}
