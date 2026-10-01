package controllers

import (
	"encoding/json"
	"errors"
	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/access"
	"git.pcux.cn/xuans/Smail/services/accountgroup"
	"git.pcux.cn/xuans/Smail/services/auth"
	"git.pcux.cn/xuans/Smail/session"
	"git.pcux.cn/xuans/Smail/utils/array"
	"git.pcux.cn/xuans/Smail/utils/context"
	"git.pcux.cn/xuans/Smail/utils/csrf"
	"git.pcux.cn/xuans/Smail/utils/password"
	"git.pcux.cn/xuans/Smail/utils/validation"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cast"
	"io"
	"math"
	"net/http"
	"time"
)

const maxUserRequestBytes int64 = 64 << 10

func decodeUserRequest(w http.ResponseWriter, req *http.Request, dst any) error {
	req.Body = http.MaxBytesReader(w, req.Body, maxUserRequestBytes)
	decoder := json.NewDecoder(req.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		return err
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		if err == nil {
			return errors.New("multiple json values")
		}
		return err
	}
	return nil
}

type userCreateRequest struct {
	GroupIDs *[]int `json:"group_ids"`
	Id       int    `json:"id"`
	Account  string `json:"account"`
	Username string `json:"username"`
	Password string `json:"password"`
	Disabled int    `json:"disabled"`
	Role     string `json:"role"`
}

type userEditRequest struct {
	userCreateRequest
	GroupIDs          *[]int `json:"group_ids"`
	MembershipVersion int    `json:"membership_version"`
}

func CreateUser(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	targetRole := access.RoleUser
	var reqData userCreateRequest
	if err := decodeUserRequest(w, req, &reqData); err != nil {
		response.NewErrorResponse(response.ParamsError, "参数错误", "").FPrint(w)
		return
	}
	if reqData.Role != "" {
		targetRole = access.Role(reqData.Role)
	}
	if err := authorizeCapability(ctx, access.CapabilityUserCreate, targetRole, false); err != nil {
		response.NewErrorResponse(response.NoAccessPrivileges, "无权访问", "").FPrint(w)
		return
	}
	if reqData.Username == "" || reqData.Password == "" || reqData.Account == "" {
		response.NewErrorResponse(response.ParamsError, "参数错误", "").FPrint(w)
		return
	}
	if reqData.Disabled != 0 && reqData.Disabled != 1 {
		response.NewErrorResponse(response.ParamsError, "参数错误", "").FPrint(w)
		return
	}

	account, err := validation.NormalizeAccount(reqData.Account)
	if err != nil {
		response.NewErrorResponse(response.ParamsError, "参数错误", "").FPrint(w)
		return
	}
	username, err := validation.NormalizeDisplayName(reqData.Username)
	if err != nil {
		response.NewErrorResponse(response.ParamsError, "参数错误", "").FPrint(w)
		return
	}
	user := models.User{Name: username, Account: account, Disabled: reqData.Disabled, SessionVersion: 1, SecurityVersion: 1}
	user.ApplyRole(targetRole)
	groupIDs := []int{0}
	if reqData.GroupIDs != nil {
		groupIDs = *reqData.GroupIDs
	}
	if len(groupIDs) > 0 && (len(groupIDs) != 1 || groupIDs[0] != 0) {
		if !requireAccountGroupMembershipManagement(ctx, w) {
			return
		}
	}
	if err := auth.SetPassword(&user, reqData.Password); err != nil {
		if writePasswordBusy(w, err) {
			return
		}
		if errors.Is(err, password.ErrInvalidLength) {
			response.NewErrorResponse(response.ParamsError, "密码长度必须为 8-12 字节", "").FPrint(w)
			return
		}
		log.WithContext(ctx).WithError(err).Error("password hash failed")
		response.NewErrorResponse(response.ServerError, "密码设置失败", "").FPrint(w)
		return
	}
	if db.Instance == nil {
		writeAccountGroupError(w, accountgroup.ErrDenied)
		return
	}
	tx := db.Instance.NewSession()
	defer tx.Close()
	if err := tx.Begin(); err != nil {
		writeAccountGroupError(w, err)
		return
	}
	defer tx.Rollback()
	actor := models.User{ID: ctx.UserID, SessionVersion: ctx.SessionVersion, SecurityVersion: ctx.SecurityVersion}
	actor.ApplyRole(ctx.EffectiveRole())
	if err := accountgroup.CheckWrite(tx, accountWriteGuard(ctx, actor)); err != nil {
		writeAccountGroupError(w, err)
		return
	}
	groupIDs, err = accountgroup.ValidateMembership(tx, user.EffectiveRole(), groupIDs)
	if err != nil {
		writeAccountGroupError(w, err)
		return
	}
	if _, err := tx.Insert(&user); err != nil {
		log.WithContext(ctx).WithError(err).Error("create user failed")
		response.NewErrorResponse(response.ServerError, "创建用户失败", "").FPrint(w)
		return
	}
	if err := accountgroup.ReplaceMembership(tx, user.ID, groupIDs); err != nil {
		writeAccountGroupError(w, err)
		return
	}
	if err := tx.Commit(); err != nil {
		writeAccountGroupError(w, err)
		return
	}
	response.NewSuccessResponse(userListItem{
		ID: user.ID, Account: user.Account, Name: user.Name,
		Disabled: user.Disabled, Role: user.EffectiveRole(), IsAdmin: user.IsAdmin,
		GroupIDs: groupIDs, MembershipVersion: user.SecurityVersion,
		MustChangePassword:   user.MustChangePassword,
		ExternalEmailEnabled: user.ExternalEmailEnabled == 1,
	}).FPrint(w)
}

type userListRequest struct {
	UserID      int  `json:"user_id"`
	GroupID     *int `json:"group_id"`
	CurrentPage int  `json:"current_page"`
	PageSize    int  `json:"page_size"`
}

type userListItem struct {
	GroupIDs                []int       `json:"GroupIDs,omitempty"`
	MembershipVersion       int         `json:"MembershipVersion,omitempty"`
	ID                      int         `json:"ID"`
	Account                 string      `json:"Account"`
	Name                    string      `json:"Name"`
	Disabled                int         `json:"Disabled"`
	Role                    access.Role `json:"Role"`
	IsAdmin                 int         `json:"IsAdmin"`
	MustChangePassword      int         `json:"must_change_password"`
	RecoveryCodeConfigured  bool        `json:"RecoveryCodeConfigured"`
	ExternalEmailEnabled    bool        `json:"ExternalEmailEnabled"`
	ExternalEmailConfigured bool        `json:"ExternalEmailConfigured"`
}

func loadUserSecurityStatus(users []models.User) (map[int]bool, map[int]bool, error) {
	recoveryConfigured := make(map[int]bool, len(users))
	externalEmailConfigured := make(map[int]bool, len(users))
	if len(users) == 0 {
		return recoveryConfigured, externalEmailConfigured, nil
	}

	userIDs := make([]int, 0, len(users))
	for _, user := range users {
		userIDs = append(userIDs, user.ID)
	}

	var recoveryCodes []models.RecoveryCode
	if err := db.Instance.Cols("user_id").In("user_id", userIDs).Where("used_at IS NULL").Find(&recoveryCodes); err != nil {
		return nil, nil, err
	}
	for _, recoveryCode := range recoveryCodes {
		recoveryConfigured[recoveryCode.UserID] = true
	}

	var externalEmails []models.ExternalEmail
	if err := db.Instance.Cols("user_id").In("user_id", userIDs).Where("status = ?", models.ExternalEmailStatusVerified).Find(&externalEmails); err != nil {
		return nil, nil, err
	}
	for _, externalEmail := range externalEmails {
		externalEmailConfigured[externalEmail.UserID] = true
	}

	return recoveryConfigured, externalEmailConfigured, nil
}

func UserList(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	if err := authorizeCapability(ctx, access.CapabilityUserRead, "", false); err != nil {
		response.NewErrorResponse(response.NoAccessPrivileges, "无权访问", "").FPrint(w)
		return
	}
	var reqData userListRequest
	if err := decodeUserRequest(w, req, &reqData); err != nil {
		response.NewErrorResponse(response.ParamsError, "参数错误", "").FPrint(w)
		return
	}
	if reqData.PageSize <= 0 || reqData.PageSize > 100 {
		reqData.PageSize = 15
	}
	offset := 0
	if reqData.CurrentPage >= 1 {
		offset = (reqData.CurrentPage - 1) * reqData.PageSize
	}

	var users []models.User
	query := db.Instance.Table(&models.User{})
	defer query.Close()
	accountgroup.ApplyScope(query, ctx.UserID, ctx.EffectiveRole())
	if reqData.UserID != 0 {
		query.And("id = ?", reqData.UserID)
	}
	if reqData.GroupID != nil {
		groups, err := accountgroup.VisibleGroups(db.Instance, ctx.UserID, ctx.EffectiveRole())
		if err != nil {
			writeAccountGroupError(w, err)
			return
		}
		allowed := false
		for _, group := range groups {
			if group.ID == *reqData.GroupID {
				allowed = true
			}
		}
		if !allowed {
			writeAccountGroupError(w, accountgroup.ErrDenied)
			return
		}
		accountgroup.ApplyGroup(query, *reqData.GroupID)
	}
	totalNum, err := query.Asc("id").Limit(reqData.PageSize, offset).FindAndCount(&users)
	if err != nil {
		log.WithContext(ctx).Errorf("SQL Error: %+v", err)
		response.NewErrorResponse(response.ServerError, "查询失败", "").FPrint(w)
		return
	}
	recoveryConfigured, externalEmailConfigured, err := loadUserSecurityStatus(users)
	if err != nil {
		log.WithContext(ctx).WithError(err).Error("load user security status failed")
		response.NewErrorResponse(response.ServerError, "读取用户安全状态失败", "").FPrint(w)
		return
	}
	items := make([]userListItem, 0, len(users))
	memberships := map[int][]int{}
	if ctx.EffectiveRole() == access.RoleSuperAdmin {
		ids := make([]int, 0, len(users))
		for _, user := range users {
			ids = append(ids, user.ID)
		}
		memberships, err = accountgroup.Memberships(db.Instance, ids)
		if err != nil {
			writeAccountGroupError(w, err)
			return
		}
	}
	for _, user := range users {
		items = append(items, userListItem{
			GroupIDs:                memberships[user.ID],
			ID:                      user.ID,
			Account:                 user.Account,
			Name:                    user.Name,
			Disabled:                user.Disabled,
			Role:                    user.EffectiveRole(),
			IsAdmin:                 user.IsAdmin,
			MustChangePassword:      user.MustChangePassword,
			RecoveryCodeConfigured:  recoveryConfigured[user.ID],
			ExternalEmailEnabled:    user.ExternalEmailEnabled == 1,
			ExternalEmailConfigured: externalEmailConfigured[user.ID],
		})
		if ctx.EffectiveRole() == access.RoleSuperAdmin {
			items[len(items)-1].MembershipVersion = user.SecurityVersion
		}
	}
	response.NewSuccessResponse(map[string]any{
		"current_page": reqData.CurrentPage,
		"total_page":   cast.ToInt(math.Ceil(cast.ToFloat64(totalNum) / cast.ToFloat64(reqData.PageSize))),
		"list":         items,
	}).FPrint(w)
}

func Info(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	if _, err := csrf.Ensure(w, req, !config.DevelopmentMode); err != nil {
		log.WithContext(ctx).WithError(err).Error("restore csrf token failed")
		response.NewErrorResponse(response.ServerError, "登录状态恢复失败", "").FPrint(w)
		return
	}
	mailDomains := config.CurrentMailDomains()
	if mailDomains.Primary == "" {
		response.NewErrorResponse(response.ServerError, "服务配置不可用", "").FPrint(w)
		return
	}
	domains := array.Difference(mailDomains.Accepted, []string{mailDomains.Primary})
	domains = append([]string{mailDomains.Primary}, domains...)
	result := map[string]any{
		"id":                   ctx.UserID,
		"account":              ctx.UserAccount,
		"name":                 ctx.UserName,
		"role":                 ctx.EffectiveRole(),
		"capabilities":         access.Capabilities(ctx.EffectiveRole()),
		"is_admin":             ctx.IsAdmin,
		"must_change_password": ctx.MustChangePassword,
		"domains":              domains,
	}
	for key, value := range loginSecurityFields(&models.User{ID: ctx.UserID, Role: string(ctx.EffectiveRole())}) {
		result[key] = value
	}
	if result["securitySetupRequired"] != true && session.Instance != nil && session.Instance.GetBool(req.Context(), session.SecuritySetupDismissedKey) {
		result["showSecuritySetupPrompt"] = false
		result["showRecoverySetupPrompt"] = false
	}
	response.NewSuccessResponse(result).FPrint(w)
}

func EditUser(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	var reqData userEditRequest
	if err := decodeUserRequest(w, req, &reqData); err != nil {
		response.NewErrorResponse(response.ParamsError, "参数错误", "").FPrint(w)
		return
	}
	if reqData.Id == 0 && reqData.Account == "" {
		response.NewErrorResponse(response.ParamsError, "参数错误", "").FPrint(w)
		return
	}
	if reqData.Disabled != 0 && reqData.Disabled != 1 {
		response.NewErrorResponse(response.ParamsError, "参数错误", "").FPrint(w)
		return
	}

	var user models.User
	var found bool
	var err error
	if reqData.Id != 0 {
		found, err = db.Instance.Where("id=?", reqData.Id).Get(&user)
	} else {
		account, validationErr := validation.NormalizeAccount(reqData.Account)
		if validationErr != nil {
			response.NewErrorResponse(response.ParamsError, "参数错误", "").FPrint(w)
			return
		}
		found, err = db.Instance.Where("account=?", account).Get(&user)
	}
	if err != nil {
		log.WithContext(ctx).Errorf("SQL Error: %+v", err)
		response.NewErrorResponse(response.ServerError, "查询用户失败", "").FPrint(w)
		return
	}
	if !found || user.ID == 0 {
		response.NewErrorResponse(response.ParamsError, "用户不存在", "").FPrint(w)
		return
	}
	targetRole := user.EffectiveRole()
	writeGuard := accountWriteGuard(ctx, user)
	if reqData.GroupIDs != nil {
		if !requireAccountGroupMembershipManagement(ctx, w) {
			return
		}
		if reqData.MembershipVersion < 1 || reqData.MembershipVersion != user.SecurityVersion {
			writeAccountGroupError(w, accountgroup.ErrConflict)
			return
		}
	}
	if !requireAccountScope(ctx, w, user.ID) {
		return
	}
	if err := authorizeCapability(ctx, access.CapabilityUserManage, targetRole, false); err != nil {
		response.NewErrorResponse(response.NoAccessPrivileges, "无权管理该用户", "").FPrint(w)
		return
	}
	if reqData.Password != "" {
		if err := authorizeCapability(ctx, access.CapabilityPasswordReset, targetRole, false); err != nil {
			writeUserAuthorizationError(w, err, "密码重置需要为当前管理员配置有效安全码")
			return
		}
	}
	originalDisabled := user.Disabled
	originalRole := user.EffectiveRole()
	originalSessionVersion := user.SessionVersion
	originalSecurityVersion := user.SecurityVersion
	if reqData.Role != "" && access.Role(reqData.Role) != targetRole {
		newRole := access.Role(reqData.Role)
		if !newRole.Valid() {
			response.NewErrorResponse(response.ParamsError, "角色参数错误", "").FPrint(w)
			return
		}
		ctx.TargetRecoveryConfigured = false
		if credentialService := credentialRuntime(); credentialService != nil {
			configured, checkErr := credentialService.HasValidRecoveryCode(user.ID)
			if checkErr != nil {
				log.WithContext(ctx).WithError(checkErr).Error("load target recovery code status failed")
				response.NewErrorResponse(response.ServerError, "读取目标用户恢复码状态失败", "").FPrint(w)
				return
			}
			ctx.TargetRecoveryConfigured = configured
		}
		if err := authorizeCapabilityWithNewRole(ctx, access.CapabilityRoleManage, targetRole, newRole, false); err != nil {
			writeUserAuthorizationError(w, err, "更改权限级别前，请先为该用户配置有效安全码")
			return
		}
		user.ApplyRole(newRole)
	}
	if user.IsAdmin == 1 && reqData.Disabled != 0 {
		response.NewErrorResponse(response.ParamsError, "不允许禁用管理员账户", "").FPrint(w)
		return
	}
	if user.ID == ctx.UserID && reqData.Password != "" {
		response.NewErrorResponse(response.ParamsError, "请通过安全设置修改当前账户密码", "").FPrint(w)
		return
	}
	if reqData.Username != "" {
		username, validationErr := validation.NormalizeDisplayName(reqData.Username)
		if validationErr != nil {
			response.NewErrorResponse(response.ParamsError, "参数错误", "").FPrint(w)
			return
		}
		user.Name = username
	}
	user.Disabled = reqData.Disabled
	if reqData.Password != "" {
		if err := auth.SetPassword(&user, reqData.Password); err != nil {
			if writePasswordBusy(w, err) {
				return
			}
			if errors.Is(err, password.ErrInvalidLength) {
				response.NewErrorResponse(response.ParamsError, "密码长度必须为 8-12 字节", "").FPrint(w)
				return
			}
			log.WithContext(ctx).WithError(err).Error("password hash failed")
			response.NewErrorResponse(response.ServerError, "密码设置失败", "").FPrint(w)
			return
		}
		user.MustChangePassword = 1
		now := time.Now().UTC()
		user.PasswordChangedAt = &now
	}
	if reqData.Password != "" || user.Disabled != originalDisabled || user.EffectiveRole() != originalRole || reqData.GroupIDs != nil {
		user.SessionVersion = originalSessionVersion + 1
		user.SecurityVersion = originalSecurityVersion + 1
	}
	credentialService := credentialRuntime()
	if reqData.Password != "" && credentialService == nil {
		log.WithContext(ctx).Error("credential service unavailable during user password reset")
		response.NewErrorResponse(response.ServerError, "安全凭据服务不可用", "").FPrint(w)
		return
	}
	tx := db.Instance.NewSession()
	if tx == nil {
		response.NewErrorResponse(response.ServerError, "更新用户失败", "").FPrint(w)
		return
	}
	defer tx.Close()
	if err := tx.Begin(); err != nil {
		log.WithContext(ctx).WithError(err).Error("begin user update transaction failed")
		response.NewErrorResponse(response.ServerError, "更新用户失败", "").FPrint(w)
		return
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	updateColumns := []string{"name", "disabled", "role", "is_admin"}
	if err := accountgroup.CheckWrite(tx, writeGuard); err != nil {
		_ = tx.Rollback()
		writeAccountGroupError(w, err)
		return
	}
	if reqData.GroupIDs != nil {
		if err := accountgroup.CheckWrite(tx, accountgroup.MembershipScopeGuard(ctx.UserID, ctx.EffectiveRole(), *reqData.GroupIDs)); err != nil {
			_ = tx.Rollback()
			writeAccountGroupError(w, err)
			return
		}
	}
	if reqData.Password != "" {
		updateColumns = append(updateColumns, "password", "must_change_password", "password_changed_at")
	}
	if reqData.Password != "" || user.Disabled != originalDisabled || user.EffectiveRole() != originalRole || reqData.GroupIDs != nil {
		updateColumns = append(updateColumns, "session_version", "security_version")
	}
	update := tx.ID(user.ID).Where("session_version = ? AND security_version = ?", originalSessionVersion, originalSecurityVersion).Cols(updateColumns...)
	accountgroup.ApplyScope(update, ctx.UserID, ctx.EffectiveRole())
	if num, err := update.Update(&user); err != nil {
		_ = tx.Rollback()
		log.WithContext(ctx).WithError(err).Error("update user failed")
		response.NewErrorResponse(response.ServerError, "更新用户失败", "").FPrint(w)
		return
	} else if num == 0 {
		_ = tx.Rollback()
		response.NewErrorResponse(response.ServerError, "没有可更新的数据", "").FPrint(w)
		return
	}
	if reqData.Password != "" {
		if err := credentialService.RevokeAllWithSession(tx, user.ID); err != nil {
			_ = tx.Rollback()
			log.WithContext(ctx).WithError(err).Error("revoke target credentials failed")
			response.NewErrorResponse(response.ServerError, "撤销目标用户安全凭据失败", "").FPrint(w)
			return
		}
	}
	// 角色和分组在同一事务中校验与更新，任何失败均不能留下部分修改。
	if reqData.GroupIDs != nil {
		ids, err := accountgroup.ValidateMembership(tx, user.EffectiveRole(), *reqData.GroupIDs)
		if err == nil {
			err = accountgroup.ReplaceMembership(tx, user.ID, ids)
		}
		if err != nil {
			_ = tx.Rollback()
			writeAccountGroupError(w, err)
			return
		}
	}
	if user.EffectiveRole() != access.RoleAdmin {
		count, err := tx.Where("user_id = ?", user.ID).Count(&models.AccountGroupMember{})
		if err != nil {
			_ = tx.Rollback()
			writeAccountGroupError(w, err)
			return
		}
		if count > 1 {
			_ = tx.Rollback()
			writeSecurityError(w, response.ParamsError, "请先将账号调整为一个分组，再更改为普通用户")
			return
		}
	}
	if err := tx.Commit(); err != nil {
		_ = tx.Rollback()
		log.WithContext(ctx).WithError(err).Error("commit user update failed")
		response.NewErrorResponse(response.ServerError, "更新用户失败", "").FPrint(w)
		return
	}
	committed = true
	if user.SessionVersion != originalSessionVersion {
		InvalidateSecurityRuntimeUser(user.ID, user.Account)
	}
	response.NewSuccessResponse(user).FPrint(w)
}

// writeUserAuthorizationError 保留授权限制，并让界面按错误类型继续身份确认。
func writeUserAuthorizationError(w http.ResponseWriter, err error, recoveryMessage string) {
	if access.IsDenied(err, access.CodeRecentAuthRequired) {
		writeSecurityErrorWithCode(w, response.NoAccessPrivileges, "请确认当前密码后继续操作", string(access.CodeRecentAuthRequired))
		return
	}
	if access.IsDenied(err, access.CodeRecoveryCodeRequired) {
		writeSecurityErrorWithCode(w, response.NoAccessPrivileges, recoveryMessage, string(access.CodeRecoveryCodeRequired))
		return
	}
	writeSecurityError(w, response.NoAccessPrivileges, "无权执行该用户管理操作")
}
