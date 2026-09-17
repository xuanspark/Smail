package controllers

import (
	"net/http"
	"strings"

	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/services/auth"
	"git.pcux.cn/xuans/Smail/utils/context"
)

// ListClientCredentials 返回当前用户的客户端授权码列表
func ListClientCredentials(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	service, ok := requireProtocolCredentialContext(ctx, w)
	if !ok {
		return
	}
	items, err := service.ListClientCredentials(ctx.UserID)
	if err != nil {
		writeSecurityError(w, response.ParamsError, "读取客户端授权码失败")
		return
	}
	response.NewSuccessResponse(items).FPrint(w)
}

// CreateClientCredential 为当前用户创建客户端授权码
func CreateClientCredential(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	service, ok := requireProtocolCredentialContext(ctx, w)
	if !ok {
		return
	}
	var input struct {
		ClientName string `json:"client_name"`
	}
	if err := decodeJSONRequest(w, req, &input); err != nil || strings.TrimSpace(input.ClientName) == "" {
		writeSecurityError(w, response.ParamsError, "请输入客户端名称")
		return
	}
	code, item, err := service.CreateClientCredential(ctx.UserID, input.ClientName)
	if err != nil {
		if writePasswordBusy(w, err) {
			return
		}
		writeSecurityError(w, response.ParamsError, err.Error())
		return
	}
	response.NewSuccessResponse(map[string]any{"code": code, "client": item}).FPrint(w)
}

// RegenerateClientCredential 重新生成客户端专属授权码
func RegenerateClientCredential(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	service, ok := requireProtocolCredentialContext(ctx, w)
	if !ok {
		return
	}
	var input struct {
		ClientID int `json:"client_id"`
	}
	if err := decodeJSONRequest(w, req, &input); err != nil || input.ClientID <= 0 {
		writeSecurityError(w, response.ParamsError, "客户端标识无效")
		return
	}
	code, err := service.RegenerateClientCredential(ctx.UserID, input.ClientID)
	if err != nil {
		if writePasswordBusy(w, err) {
			return
		}
		writeSecurityError(w, response.ParamsError, err.Error())
		return
	}
	response.NewSuccessResponse(map[string]any{"code": code, "client_id": input.ClientID}).FPrint(w)
}

// RevokeClientCredential 撤销客户端授权码
func RevokeClientCredential(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	service, ok := requireProtocolCredentialContext(ctx, w)
	if !ok {
		return
	}
	var input struct {
		ClientID int `json:"client_id"`
	}
	if err := decodeJSONRequest(w, req, &input); err != nil || input.ClientID <= 0 {
		writeSecurityError(w, response.ParamsError, "客户端标识无效")
		return
	}
	if err := service.RevokeClientCredential(ctx.UserID, input.ClientID); err != nil {
		writeSecurityError(w, response.ParamsError, err.Error())
		return
	}
	response.NewSuccessResponse(map[string]any{"client_id": input.ClientID, "revoked": true}).FPrint(w)
}

// RevealClientCredentials 在安全身份验证成功后解密返回所有客户端授权码
func RevealClientCredentials(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	service, ok := requireProtocolCredentialContext(ctx, w)
	if !ok {
		return
	}
	var input struct {
		Password string `json:"password"`
	}
	if err := decodeJSONRequest(w, req, &input); err != nil || strings.TrimSpace(input.Password) == "" {
		writeSecurityError(w, response.ParamsError, "请输入当前账号密码进行安全验证")
		return
	}
	if _, err := auth.AuthenticateUserByID(ctx.UserID, input.Password); err != nil {
		if writePasswordBusy(w, err) {
			return
		}
		writeSecurityError(w, response.ParamsError, "密码验证失败")
		return
	}
	codes, err := service.RevealClientCredentials(ctx.UserID)
	if err != nil {
		writeSecurityError(w, response.ServerError, "解密授权码失败")
		return
	}
	response.NewSuccessResponse(codes).FPrint(w)
}
