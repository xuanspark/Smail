package email

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/services/detail"
	"git.pcux.cn/xuans/Smail/services/translate"
	smailContext "git.pcux.cn/xuans/Smail/utils/context"
	log "github.com/sirupsen/logrus"
)

type emailTranslateRequest struct {
	ID         int    `json:"id"`
	Subject    string `json:"subject"`
	Text       string `json:"text"`
	TargetLang string `json:"target_lang"`
}

type emailTranslateResponse struct {
	SubjectTranslated string `json:"subject_translated"`
	ContentTranslated string `json:"content_translated"`
	DetectedLang      string `json:"detected_lang"`
	TargetLang        string `json:"target_lang"`
}

// TranslateEmail 邮件免配置一键翻译接口
func TranslateEmail(ctx *smailContext.Context, w http.ResponseWriter, req *http.Request) {
	var retData emailTranslateRequest
	if err := decodeEmailJSONRequest(w, req, &retData, maxEmailJSONRequestBytes); err != nil {
		log.WithContext(ctx).WithError(err).Info("invalid email translate request")
		response.NewErrorResponse(response.ParamsError, "参数错误", "").FPrint(w)
		return
	}

	targetLang := strings.TrimSpace(retData.TargetLang)
	if targetLang == "" {
		targetLang = "zh-CN"
	}

	var subjectToTranslate string
	var contentToTranslate string

	if retData.ID > 0 {
		emailData, err := detail.GetEmailDetail(ctx, retData.ID, false)
		if err != nil {
			log.WithContext(ctx).WithError(err).Warnf("translate email: load email detail failed for id=%d", retData.ID)
			response.NewErrorResponse(response.ServerError, "获取邮件失败", "").FPrint(w)
			return
		}
		subjectToTranslate = emailData.Subject
		if emailData.Text.Valid && strings.TrimSpace(emailData.Text.String) != "" {
			contentToTranslate = emailData.Text.String
		} else if emailData.Html.Valid {
			contentToTranslate = emailData.Html.String
		}
	} else {
		subjectToTranslate = retData.Subject
		contentToTranslate = retData.Text
	}

	if strings.TrimSpace(subjectToTranslate) == "" && strings.TrimSpace(contentToTranslate) == "" {
		response.NewSuccessResponse(emailTranslateResponse{
			SubjectTranslated: "",
			ContentTranslated: "",
			DetectedLang:      "auto",
			TargetLang:        targetLang,
		}).FPrint(w)
		return
	}

	callCtx := req.Context()
	if callCtx == nil {
		callCtx = context.Background()
	}

	var translatedSubject string
	var detectedLang = "auto"

	if strings.TrimSpace(subjectToTranslate) != "" {
		subRes, err := translate.TranslateText(callCtx, subjectToTranslate, targetLang)
		if err != nil {
			log.WithContext(ctx).WithError(err).Warn("translate subject failed")
			response.NewErrorResponse(response.ServerError, fmt.Sprintf("翻译主题失败: %v", err), "").FPrint(w)
			return
		}
		translatedSubject = subRes.TranslatedText
		if subRes.SourceLang != "" && subRes.SourceLang != "auto" {
			detectedLang = subRes.SourceLang
		}
	}

	var translatedContent string
	if strings.TrimSpace(contentToTranslate) != "" {
		contentRes, err := translate.TranslateText(callCtx, contentToTranslate, targetLang)
		if err != nil {
			log.WithContext(ctx).WithError(err).Warn("translate content failed")
			response.NewErrorResponse(response.ServerError, fmt.Sprintf("翻译内容失败: %v", err), "").FPrint(w)
			return
		}
		translatedContent = contentRes.TranslatedText
		if detectedLang == "auto" && contentRes.SourceLang != "" && contentRes.SourceLang != "auto" {
			detectedLang = contentRes.SourceLang
		}
	}

	response.NewSuccessResponse(emailTranslateResponse{
		SubjectTranslated: translatedSubject,
		ContentTranslated: translatedContent,
		DetectedLang:      detectedLang,
		TargetLang:        targetLang,
	}).FPrint(w)
}
