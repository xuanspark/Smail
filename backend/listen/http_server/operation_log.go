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

// loggedOperations 注册显式的静态状态变更路由，仅记录状态变更操作，不将列表查询误认为操作，也不复制请求或响应载荷。
var loggedOperations = map[string]string{
	"/api/system/notification/authorization": "修改系统通知发信权限",
	"/api/system/notification/credential": "生成系统通知发信凭据",
	"/api/email/draft/save":                             "保存邮件草稿",
	"/api/login":                                        "处理账号登录",
	"/api/logout":                                       "退出登录",
	"/api/security/reauth":                              "完成身份重新认证",
	"/api/security/verification/request":                "申请安全验证码",
	"/api/security/verification/verify":                 "完成安全验证",
	"/api/security/login/verify":                        "完成登录二次验证",
	"/api/security/login/resend":                        "重新发送登录验证码",
	"/api/security/webauthn/registration/begin":         "发起通行密钥注册",
	"/api/security/webauthn/registration/finish":        "完成通行密钥注册",
	"/api/security/webauthn/credentials/rename":         "重命名通行密钥",
	"/api/security/webauthn/credentials/revoke":         "撤销通行密钥",
	"/api/security/webauthn/login/begin":                "发起通行密钥登录",
	"/api/security/webauthn/login/finish":               "完成通行密钥登录验证",
	"/api/security/recovery/webauthn/begin":             "发起通行密钥恢复账号",
	"/api/security/recovery/webauthn/verify":            "通过通行密钥恢复账号",
	"/api/security/recovery/begin":                      "发起账号恢复",
	"/api/security/recovery/verify":                     "验证账号恢复凭据",
	"/api/security/recovery/complete":                   "完成账号恢复",
	"/api/security/setup/acknowledge":                   "确认安全初始化引导",
	"/api/security/credentials/question":                "设置密保问题",
	"/api/security/credentials/recovery-codes/generate": "生成安全码",
	"/api/security/credentials/recovery-codes/revoke":   "撤销安全码",
	"/api/security/credentials/protocols/enabled":       "设置邮件客户端授权",
	"/api/security/credentials/protocols/generate":      "生成邮件客户端安全码",
	"/api/security/credentials/protocols/revoke":        "撤销邮件客户端安全码",
	"/api/security/credentials/protocols/reveal":        "查看邮件客户端安全码",
	"/api/security/credentials/clients/create":          "创建客户端专属授权码",
	"/api/security/credentials/clients/regenerate":      "重新生成客户端授权码",
	"/api/security/credentials/clients/revoke":          "撤销客户端专属授权码",
	"/api/security/credentials/clients/reveal":          "查看客户端专属授权码",
	"/api/security/external-email/bind":                 "申请绑定外部邮箱",
	"/api/security/external-email/verify":               "验证外部邮箱",
	"/api/security/external-email/change":               "申请修改外部邮箱",
	"/api/security/external-email/change/verify":        "验证外部邮箱修改",
	"/api/security/external-email/disable":              "解绑外部邮箱",
	"/api/settings/modify_password":                     "修改账号密码",
	"/api/user/create":                                  "创建用户",
	"/api/user/edit":                                    "编辑用户",
	"/api/user/profile":                                 "修改个人资料",
	"/api/user/security/recovery-codes/generate":        "管理用户安全码",
	"/api/user/security/recovery-codes/revoke":          "撤销用户安全码",
	"/api/user/security/external-email/change":          "申请修改用户外部邮箱",
	"/api/user/security/external-email/verify":          "验证用户外部邮箱修改",
	"/api/group/add":                                    "创建分组",
	"/api/group/del":                                    "删除分组",
	"/api/email/del":                                    "删除邮件",
	"/api/email/read":                                   "修改已读状态",
	"/api/email/move":                                   "移动邮件",
	"/api/email/send":                                   "发送邮件",
	"/api/email/contacts/delete":                        "删除常用联系人",
	"/api/email/trust-sender":                           "设置信任发件人",
	"/api/rule/add":                                     "创建邮件规则",
	"/api/rule/update":                                  "修改邮件规则",
	"/api/rule/del":                                     "删除邮件规则",
	"/api/system/restart":                               "请求重启系统服务",
	"/api/system/restart/preview":                       "预览系统服务重启",
	"/api/database/migrations":                          "执行数据库迁移",
	"/api/database/migrations/preview":                  "预览数据库迁移",
	"/api/setup":                                        "执行系统初始化配置",
	"/api/plugin/settings/telegram_push/user":           "更新电报通知设置",
	"/api/plugin/settings/telegram_push/test":           "测试电报消息推送",
	"/api/plugin/secrets/telegram_push":                 "更新电报插件密钥",
	"/api/plugin/secrets/wechat_push":                   "更新微信插件密钥",
}

// readonlyPostEndpoints 明确排除属于查询性质的 POST 接口，防止误记操作记录。
var readonlyPostEndpoints = map[string]struct{}{
	"/api/email/draft/detail": {},
	"/api/user/info":          {},
	"/api/user/list":          {},
	"/api/email/list":         {},
	"/api/email/detail":       {},
	"/api/rule/get":           {},
}

var configSectionNames = map[string]string{
	"base":    "基础",
	"mail":    "邮件服务与策略",
	"smtp":    "SMTP服务",
	"email":   "邮件策略",
	"ssl":     "证书",
	"plugins": "插件",
}

func parseConfigAction(path, method string) string {
	cleanPath := strings.TrimSuffix(path, "/")
	if !strings.HasPrefix(cleanPath, "/api/config") {
		return ""
	}
	switch cleanPath {
	case "/api/config/pending":
		if method == http.MethodPost {
			return "管理候选配置"
		}
		return ""
	case "/api/config/rollback/preview":
		if method == http.MethodPost {
			return "预览配置回滚"
		}
		return ""
	case "/api/config/rollback/ticket":
		if method == http.MethodPost {
			return "签发配置回滚票据"
		}
		return ""
	case "/api/config/rollback":
		if method == http.MethodPost {
			return "回滚系统配置"
		}
		return ""
	}
	parts := strings.Split(strings.Trim(cleanPath, "/"), "/")
	if len(parts) == 3 && method == http.MethodPut {
		sectionKey := parts[2]
		sectionName := configSectionNames[sectionKey]
		if sectionName == "" {
			sectionName = sectionKey
		}
		return "保存" + sectionName + "配置"
	}
	if len(parts) == 4 && method == http.MethodPost {
		sectionKey := parts[2]
		operation := parts[3]
		sectionName := configSectionNames[sectionKey]
		if sectionName == "" {
			sectionName = sectionKey
		}
		switch operation {
		case "validate":
			return "校验" + sectionName + "配置"
		case "apply":
			return "应用" + sectionName + "配置"
		}
	}
	return ""
}

func parseOperationAction(path, method string) string {
	if _, readonly := readonlyPostEndpoints[path]; readonly {
		return ""
	}
	if action := loggedOperations[path]; action != "" {
		return action
	}
	return parseConfigAction(path, method)
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
		{"/api/config", "系统配置"},
		{"/api/system/", "系统服务"},
		{"/api/database/", "数据库运维"},
		{"/api/plugin/", "插件配置"},
		{"/api/setup", "系统服务"},
		{"/api/email/", "邮件"},
		{"/api/group/", "邮件分组"},
		{"/api/rule/", "邮件规则"},
		{"/api/user/security/", "账号安全"},
		{"/api/user/", "用户账号"},
		{"/api/security/", "账号安全"},
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
	if !stateChangingMethod(req.Method) {
		return w, func() {}
	}
	action := parseOperationAction(path, req.Method)
	if action == "" {
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
			Kind: "operation", CreatedAt: time.Now(), ActorID: strconv.Itoa(ctx.UserID), ActorRole: string(ctx.EffectiveRole()), Action: action, Result: result, Payload: payload,
		})
	}
}
