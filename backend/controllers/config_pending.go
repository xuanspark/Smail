package controllers

import (
	"errors"
	"net/http"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/services/access"
	appcontext "git.pcux.cn/xuans/Smail/utils/context"
)

// GetPendingConfig 返回候选的脱敏视图，用于刷新或重新登录后的恢复。
func GetPendingConfig(ctx *appcontext.Context, w http.ResponseWriter, req *http.Request) {
	manager, ok := configManagerOrError(w)
	if !ok {
		return
	}
	if err := authorizeCapability(ctx, access.CapabilityConfigRead, "", false); err != nil {
		writeConfigAccessError(w, err)
		return
	}
	pending := manager.Pending(configActor(ctx, req))
	if pending == nil {
		response.NewSuccessResponse(nil).FPrint(w)
		return
	}
	public, err := publicConfigSection(pending.Candidate, pending.Section)
	if err != nil {
		writeConfigError(w, err)
		return
	}
	response.NewSuccessResponse(map[string]any{"generation": pending.Generation, "candidateId": pending.ID, "section": pending.Section, "sameSession": pending.SameSession, "committed": pending.Committed, "payload": public}).FPrint(w)
}

func ManagePendingConfig(ctx *appcontext.Context, w http.ResponseWriter, req *http.Request) {
	manager, ok := configManagerOrError(w)
	if !ok {
		return
	}
	if err := authorizeCapability(ctx, access.CapabilityConfigWriteGlobal, "", false); err != nil {
		writeConfigAccessError(w, err)
		return
	}
	var input struct {
		Generation   uint64                    `json:"generation"`
		CandidateID  string                    `json:"candidateId"`
		Action       string                    `json:"action"`
		Confirmation config.StepUpConfirmation `json:"confirmation"`
	}
	if err := decodeJSONRequest(w, req, &input); err != nil || input.Generation == 0 || (input.Action != "cancel" && input.Action != "resume") {
		writeConfigError(w, errors.New("invalid configuration request"))
		return
	}
	if err := manager.ManagePending(req.Context(), input.Generation, input.CandidateID, configActor(ctx, req), input.Confirmation, input.Action == "cancel"); err != nil {
		writeConfigError(w, err)
		return
	}
	response.NewSuccessResponse(nil).FPrint(w)
}
