package controllers

import (
	"encoding/json"
	"errors"
	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/access"
	"git.pcux.cn/xuans/Smail/services/auth"
	"git.pcux.cn/xuans/Smail/utils/array"
	"git.pcux.cn/xuans/Smail/utils/context"
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
	Id       int    `json:"id"`
	Account  string `json:"account"`
	Username string `json:"username"`
	Password string `json:"password"`
	Disabled int    `json:"disabled"`
	Role     string `json:"role"`
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
	if err := authorizeCapability(ctx, access.CapabilityUserManage, targetRole, false); err != nil {
		response.NewErrorResponse(response.NoAccessPrivileges, "无权访问", "").FPrint(w)
		return
	}
	if reqData.Username == "" || reqData.Password == "" || reqData.Account == "" {
		response.NewErrorResponse(response.ParamsError, "\u53c2\u6570\u9519\u8bef", "").FPrint(w)
		return
	}
	if reqData.Disabled != 0 && reqData.Disabled != 1 {
		response.NewErrorResponse(response.ParamsError, "\u53c2\u6570\u9519\u8bef", "").FPrint(w)
		return
	}

	account, err := validation.NormalizeAccount(reqData.Account)
	if err != nil {
		response.NewErrorResponse(response.ParamsError, "\u53c2\u6570\u9519\u8bef", "").FPrint(w)
		return
	}
	username, err := validation.NormalizeDisplayName(reqData.Username)
	if err != nil {
		response.NewErrorResponse(response.ParamsError, "\u53c2\u6570\u9519\u8bef", "").FPrint(w)
		return
	}
	user := models.User{Name: username, Account: account, Disabled: reqData.Disabled, SessionVersion: 1, SecurityVersion: 1}
	user.ApplyRole(targetRole)
	if err := auth.SetPassword(&user, reqData.Password); err != nil {
		if errors.Is(err, password.ErrInvalidLength) {
			response.NewErrorResponse(response.ParamsError, "\u5bc6\u7801\u957f\u5ea6\u5fc5\u987b\u4e3a 8-12 \u5b57\u8282", "").FPrint(w)
			return
		}
		log.WithContext(ctx).WithError(err).Error("password hash failed")
		response.NewErrorResponse(response.ServerError, "密码设置失败", "").FPrint(w)
		return
	}
	if _, err := db.Instance.Insert(&user); err != nil {
		log.WithContext(ctx).WithError(err).Error("create user failed")
		response.NewErrorResponse(response.ServerError, "\u521b\u5efa\u7528\u6237\u5931\u8d25", "").FPrint(w)
		return
	}
	response.NewSuccessResponse(user).FPrint(w)
}

type userListRequest struct {
	CurrentPage int `json:"current_page"`
	PageSize    int `json:"page_size"`
}

func UserList(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	if err := authorizeCapability(ctx, access.CapabilityUserRead, "", false); err != nil {
		response.NewErrorResponse(response.NoAccessPrivileges, "\u65e0\u6743\u8bbf\u95ee", "").FPrint(w)
		return
	}
	var reqData userListRequest
	if err := decodeUserRequest(w, req, &reqData); err != nil {
		response.NewErrorResponse(response.ParamsError, "\u53c2\u6570\u9519\u8bef", "").FPrint(w)
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
	totalNum, err := db.Instance.Table(&models.User{}).Limit(reqData.PageSize, offset).FindAndCount(&users)
	if err != nil {
		log.WithContext(ctx).Errorf("SQL Error: %+v", err)
		response.NewErrorResponse(response.ServerError, "查询失败", "").FPrint(w)
		return
	}
	response.NewSuccessResponse(map[string]any{
		"current_page": reqData.CurrentPage,
		"total_page":   cast.ToInt(math.Ceil(cast.ToFloat64(totalNum) / cast.ToFloat64(reqData.PageSize))),
		"list":         users,
	}).FPrint(w)
}

func Info(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	mailDomains := config.CurrentMailDomains()
	if mailDomains.Primary == "" {
		response.NewErrorResponse(response.ServerError, "服务配置不可用", "").FPrint(w)
		return
	}
	domains := array.Difference(mailDomains.Accepted, []string{mailDomains.Primary})
	domains = append([]string{mailDomains.Primary}, domains...)
	result := map[string]any{
		"account":                 ctx.UserAccount,
		"name":                    ctx.UserName,
		"role":                    ctx.EffectiveRole(),
		"capabilities":            access.Capabilities(ctx.EffectiveRole()),
		"is_admin":                ctx.IsAdmin,
		"must_change_password":    ctx.MustChangePassword,
		"showSecuritySetupPrompt": ctx.SecuritySetupAcknowledged == 0,
		"webAuthnEnabled":         webAuthnRuntime() != nil,
		"domains":                 domains,
	}
	if ctx.EffectiveRole() != access.RoleSuperAdmin {
		result["recoveryCodesConfigured"] = ctx.RecoveryCodeConfigured
		result["showRecoverySetupPrompt"] = !ctx.RecoveryCodeConfigured
	} else {
		result["recoveryCodesConfigured"] = false
		result["showRecoverySetupPrompt"] = false
	}
	response.NewSuccessResponse(result).FPrint(w)
}

func EditUser(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	var reqData userCreateRequest
	if err := decodeUserRequest(w, req, &reqData); err != nil {
		response.NewErrorResponse(response.ParamsError, "参数错误", "").FPrint(w)
		return
	}
	if reqData.Id == 0 && reqData.Account == "" {
		response.NewErrorResponse(response.ParamsError, "\u53c2\u6570\u9519\u8bef", "").FPrint(w)
		return
	}
	if reqData.Disabled != 0 && reqData.Disabled != 1 {
		response.NewErrorResponse(response.ParamsError, "\u53c2\u6570\u9519\u8bef", "").FPrint(w)
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
			response.NewErrorResponse(response.ParamsError, "\u53c2\u6570\u9519\u8bef", "").FPrint(w)
			return
		}
		found, err = db.Instance.Where("account=?", account).Get(&user)
	}
	if err != nil {
		log.WithContext(ctx).Errorf("SQL Error: %+v", err)
		response.NewErrorResponse(response.ServerError, "\u67e5\u8be2\u7528\u6237\u5931\u8d25", "").FPrint(w)
		return
	}
	if !found || user.ID == 0 {
		response.NewErrorResponse(response.ParamsError, "\u7528\u6237\u4e0d\u5b58\u5728", "").FPrint(w)
		return
	}
	targetRole := user.EffectiveRole()
	if err := authorizeCapability(ctx, access.CapabilityUserManage, targetRole, false); err != nil {
		response.NewErrorResponse(response.NoAccessPrivileges, "无权管理该用户", "").FPrint(w)
		return
	}
	if reqData.Password != "" {
		if err := authorizeCapability(ctx, access.CapabilityPasswordReset, targetRole, false); err != nil {
			response.NewErrorResponse(response.NoAccessPrivileges, "密码重置需要近期认证和有效离线恢复码", "").FPrint(w)
			return
		}
	}
	roleChanged := false
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
			response.NewErrorResponse(response.NoAccessPrivileges, "角色变更需要近期认证和恢复凭据", "").FPrint(w)
			return
		}
		user.ApplyRole(newRole)
		roleChanged = true
	}
	if user.IsAdmin == 1 && reqData.Disabled != 0 {
		response.NewErrorResponse(response.ParamsError, "\u4e0d\u5141\u8bb8\u7981\u7528\u7ba1\u7406\u5458\u8d26\u6237", "").FPrint(w)
		return
	}
	if user.ID == ctx.UserID && reqData.Password != "" {
		response.NewErrorResponse(response.ParamsError, "\u8bf7\u901a\u8fc7\u5b89\u5168\u8bbe\u7f6e\u4fee\u6539\u5f53\u524d\u8d26\u6237\u5bc6\u7801", "").FPrint(w)
		return
	}
	originalDisabled := user.Disabled
	originalRole := user.EffectiveRole()
	originalSessionVersion := user.SessionVersion
	originalSecurityVersion := user.SecurityVersion
	if reqData.Username != "" {
		username, validationErr := validation.NormalizeDisplayName(reqData.Username)
		if validationErr != nil {
			response.NewErrorResponse(response.ParamsError, "\u53c2\u6570\u9519\u8bef", "").FPrint(w)
			return
		}
		user.Name = username
	}
	user.Disabled = reqData.Disabled
	if reqData.Password != "" {
		if err := auth.SetPassword(&user, reqData.Password); err != nil {
			if errors.Is(err, password.ErrInvalidLength) {
				response.NewErrorResponse(response.ParamsError, "\u5bc6\u7801\u957f\u5ea6\u5fc5\u987b\u4e3a 8-12 \u5b57\u8282", "").FPrint(w)
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
	if reqData.Password != "" || user.Disabled != originalDisabled || user.EffectiveRole() != originalRole {
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
	if reqData.Password != "" {
		updateColumns = append(updateColumns, "password", "must_change_password", "password_changed_at")
	}
	if reqData.Password != "" || user.Disabled != originalDisabled || user.EffectiveRole() != originalRole {
		updateColumns = append(updateColumns, "session_version", "security_version")
	}
	if num, err := tx.ID(user.ID).Where("session_version = ?", originalSessionVersion).Cols(updateColumns...).Update(&user); err != nil {
		log.WithContext(ctx).WithError(err).Error("update user failed")
		response.NewErrorResponse(response.ServerError, "更新用户失败", "").FPrint(w)
		return
	} else if num == 0 {
		response.NewErrorResponse(response.ServerError, "没有可更新的数据", "").FPrint(w)
		return
	}
	if reqData.Password != "" {
		if err := credentialService.RevokeAllWithSession(tx, user.ID); err != nil {
			log.WithContext(ctx).WithError(err).Error("revoke target credentials failed")
			response.NewErrorResponse(response.ServerError, "撤销目标用户安全凭据失败", "").FPrint(w)
			return
		}
	}
	if err := tx.Commit(); err != nil {
		log.WithContext(ctx).WithError(err).Error("commit user update failed")
		response.NewErrorResponse(response.ServerError, "更新用户失败", "").FPrint(w)
		return
	}
	committed = true
	if reqData.Password != "" {
		InvalidateSecurityRuntimeUser(user.ID, user.Account)
	}
	_ = roleChanged
	response.NewSuccessResponse(user).FPrint(w)
}
