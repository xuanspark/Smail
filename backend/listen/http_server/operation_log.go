package http_server

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/services/logstore"
	appcontext "git.pcux.cn/xuans/Smail/utils/context"
)

// 仅记录明确的变更接口，不将 POST 列表查询误认为操作，也不复制请求或响应载荷。
var loggedOperations = map[string]string{
	"/api/login": "处理账号登录", "/api/logout": "退出登录",
	"/api/security/login/verify":          "完成登录二次验证",
	"/api/security/webauthn/login/finish": "完成通行密钥登录验证",
	"/api/user/create":                    "创建用户", "/api/user/edit": "编辑用户",
	"/api/group/add": "创建分组", "/api/group/del": "删除分组",
	"/api/email/del": "删除邮件", "/api/email/move": "移动邮件", "/api/email/read": "修改已读状态",
	"/api/rule/add": "创建邮件规则", "/api/rule/update": "修改邮件规则", "/api/rule/del": "删除邮件规则",
	"/api/settings/modify_password":                     "修改账号密码",
	"/api/security/credentials/recovery-codes/generate": "生成安全码",
	"/api/security/credentials/recovery-codes/revoke":   "撤销安全码",
	"/api/security/external-email/bind":                 "申请绑定外部邮箱",
	"/api/security/external-email/verify":               "验证外部邮箱",
	"/api/security/external-email/change":               "申请修改外部邮箱",
	"/api/security/external-email/change/verify":        "验证外部邮箱修改",
	"/api/security/external-email/disable":              "解绑外部邮箱",
	"/api/security/credentials/protocols/enabled":       "设置邮件客户端授权",
	"/api/security/credentials/protocols/generate":      "生成邮件客户端安全码",
	"/api/security/credentials/protocols/revoke":        "撤销邮件客户端安全码",
	"/api/security/recovery/complete":                   "完成账号恢复",
	"/api/user/security/recovery-codes/generate":        "管理用户安全码",
	"/api/user/security/recovery-codes/revoke":          "撤销用户安全码",
	"/api/user/security/external-email/change":          "申请修改用户外部邮箱",
	"/api/user/security/external-email/verify":          "验证用户外部邮箱修改",
}

type operationResponse struct {
	http.ResponseWriter
	code    int
	known   bool
	pending bool
	status  int
}

func (w *operationResponse) Unwrap() http.ResponseWriter { return w.ResponseWriter }
func (w *operationResponse) ObserveResult(code int)      { w.code, w.known = code, true }
func (w *operationResponse) ObservePending(pending bool) { w.pending = pending }

func (w *operationResponse) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
	w.ResponseWriter.WriteHeader(status)
}

func (w *operationResponse) Write(data []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(data)
}

// 只从白名单路由识别对象类别，不解析或复制含密码、验证码的请求正文。
func operationObject(path string) string {
	for _, item := range []struct{ prefix, object string }{
		{"/api/email/", "邮件"}, {"/api/group/", "邮件分组"}, {"/api/rule/", "邮件规则"},
		{"/api/user/", "用户账号"}, {"/api/security/", "账号安全"},
		{"/api/settings/", "账号设置"},
	} {
		if strings.HasPrefix(path, item.prefix) {
			return item.object
		}
	}
	return "账号会话"
}

func operationOutcome(code int, result string) string {
	switch result {
	case "success":
		return "操作已完成"
	case "pending":
		return "等待后续验证，操作尚未完成"
	case "unknown":
		return "接口未返回可确认的业务结果"
	}
	switch code {
	case response.ParamsError:
		return "参数或身份验证未通过"
	case response.NeedLogin:
		return "需要重新登录"
	case response.NoAccessPrivileges:
		return "当前操作权限或安全条件不满足"
	case response.NeedSetup:
		return "系统尚未完成初始化"
	default:
		return "操作失败，请结合结果代码和运行日志排查"
	}
}

func operationLog(ctx *appcontext.Context, req *http.Request, w http.ResponseWriter) (http.ResponseWriter, func()) {
	path := normalizedAPIPath(req.URL.Path)
	action := loggedOperations[path]
	if action == "" || !stateChangingMethod(req.Method) {
		return w, func() {}
	}
	observed := &operationResponse{ResponseWriter: w}
	return observed, func() {
		result := "unknown"
		if observed.known {
			result = "success"
			if observed.code != 0 {
				result = "failure"
			}
		}
		if result == "success" && observed.pending {
			result = "pending"
		}
		if observed.status >= http.StatusBadRequest {
			result = "failure"
		}
		payload, _ := json.Marshal(struct {
			ActorName  string `json:"actorName"`
			ActorRole  string `json:"actorRole"`
			ErrorCode  int    `json:"errorCode"`
			Object     string `json:"object"`
			Trigger    string `json:"trigger"`
			Outcome    string `json:"outcome"`
			HTTPStatus int    `json:"httpStatus"`
		}{ctx.UserAccount, string(ctx.EffectiveRole()), observed.code,
			operationObject(path), req.Method + " " + path, operationOutcome(observed.code, result), observed.status})
		logstore.Record(context.Background(), logstore.Entry{
			Kind: "operation", CreatedAt: time.Now(), ActorID: strconv.Itoa(ctx.UserID), Action: action, Result: result, Payload: payload,
		})
	}
}
