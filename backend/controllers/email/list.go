package email

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"git.pcux.cn/xuans/Smail/consts"
	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/dto"
	"git.pcux.cn/xuans/Smail/dto/parsemail"
	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/list"
	"git.pcux.cn/xuans/Smail/utils/context"
	log "github.com/sirupsen/logrus"
)

const (
	defaultEmailPageSize = 15
	maxEmailPageSize     = 100
	maxEmailPage         = 10000
	maxEmailKeywordBytes = 512
)

type emailListResponse struct {
	CurrentPage int         `json:"current_page"`
	TotalPage   int         `json:"total_page"`
	List        []*emilItem `json:"list"`
}

type emilItem struct {
	ID            int    `json:"id"`
	Title         string `json:"title"`
	Desc          string `json:"desc"`
	Datetime      string `json:"datetime"`
	IsRead        bool   `json:"is_read"`
	Sender        User   `json:"sender"`
	To            []User `json:"to"`
	Dangerous     bool   `json:"dangerous"`
	IsTrusted     bool   `json:"is_trusted"`
	IsBlacklisted bool   `json:"is_blacklisted"`
	Error         string `json:"error"`
	Status        int8   `json:"status"`
	EditableDraft bool   `json:"editable_draft"`
}

type User struct {
	Name         string `json:"Name"`
	EmailAddress string `json:"EmailAddress"`
}

type emailRequest struct {
	Keyword     string `json:"keyword"`
	Tag         string `json:"tag"`
	CurrentPage int    `json:"current_page"`
	PageSize    int    `json:"page_size"`
}

func EmailList(ctx *context.Context, w http.ResponseWriter, req *http.Request) {
	var retData emailRequest
	if err := decodeEmailJSONRequest(w, req, &retData, maxEmailJSONRequestBytes); err != nil {
		log.WithContext(ctx).Info("email list request validation failed")
		response.NewErrorResponse(response.ParamsError, "参数错误", "").FPrint(w)
		return
	}
	offset, err := normalizeEmailListRequest(&retData)
	if err != nil {
		response.NewErrorResponse(response.ParamsError, "分页参数超出限制", "").FPrint(w)
		return
	}

	tagInfo := dto.SearchTag{Type: -1, Status: -1, GroupId: -1}
	if strings.TrimSpace(retData.Tag) != "" {
		if err := json.Unmarshal([]byte(retData.Tag), &tagInfo); err != nil {
			response.NewErrorResponse(response.ParamsError, "邮件筛选参数错误", "").FPrint(w)
			return
		}
	}

	emailList, total, err := list.GetEmailList(ctx, tagInfo, retData.Keyword, false, offset, retData.PageSize)
	if err != nil {
		log.WithContext(ctx).WithError(err).Error("query email list failed")
		response.NewErrorResponse(response.ServerError, "读取邮件列表失败", "").FPrint(w)
		return
	}
	whitelistMap := make(map[string]bool)
	blacklistMap := make(map[string]bool)
	var senderFilters []models.UserTrustedSender
	if err := db.Instance.Table(&models.UserTrustedSender{}).Where("user_id = ?", ctx.UserID).Find(&senderFilters); err != nil {
		log.WithContext(ctx).WithError(err).Error("load sender filters failed")
		response.NewErrorResponse(response.ServerError, "读取发件人安全标记失败", "").FPrint(w)
		return
	} else {
		for _, item := range senderFilters {
			addr := strings.ToLower(strings.TrimSpace(item.Email))
			if item.FilterType == models.FilterTypeBlacklist {
				blacklistMap[addr] = true
			} else {
				whitelistMap[addr] = true
			}
		}
	}

	items := make([]*emilItem, 0, len(emailList))
	for _, email := range emailList {
		var sender User
		if err := decodeStoredJSON(email.Sender, &sender); err != nil {
			log.WithContext(ctx).WithField("email_id", email.Id).WithError(err).Error("decode stored sender failed")
			response.NewErrorResponse(response.ServerError, "读取邮件列表失败", "").FPrint(w)
			return
		}
		if sender.EmailAddress == "" {
			sender.EmailAddress = email.FromAddress
			sender.Name = email.FromName
		}

		var recipients []User
		if err := decodeStoredJSON(email.To, &recipients); err != nil {
			log.WithContext(ctx).WithField("email_id", email.Id).WithError(err).Error("decode stored recipients failed")
			response.NewErrorResponse(response.ServerError, "读取邮件列表失败", "").FPrint(w)
			return
		}
		authentication := parsemail.NewEmailAuthentication(email.SPFCheck == 1, email.DKIMCheck == 1)
		senderAddr := strings.ToLower(strings.TrimSpace(sender.EmailAddress))
		isTrusted := whitelistMap[senderAddr]
		isBlacklisted := blacklistMap[senderAddr] || blacklistMap[strings.ToLower(strings.TrimSpace(email.FromAddress))]
		dangerous := authentication.Dangerous
		if isBlacklisted {
			dangerous = true
		} else if isTrusted {
			dangerous = false
		}
		items = append(items, &emilItem{
			ID:            email.Id,
			Title:         email.Subject,
			Desc:          email.Text.String,
			Datetime:      email.SendDate.Format("2006-01-02 15:04:05"),
			IsRead:        email.IsRead == 1,
			Sender:        sender,
			To:            recipients,
			Dangerous:     dangerous,
			IsTrusted:     isTrusted,
			IsBlacklisted: isBlacklisted,
			Error:         email.Error.String,
			Status:        email.Status,
			EditableDraft: email.Type == consts.EmailTypeSend && email.Status == consts.EmailStatusDrafts &&
				email.SendUserID == ctx.UserID && email.UserStatus == consts.EmailStatusDrafts && email.IsDeleted == 0,
		})
	}

	totalPage := 0
	if total > 0 {
		totalPage = int((total + int64(retData.PageSize) - 1) / int64(retData.PageSize))
	}
	response.NewSuccessResponse(emailListResponse{
		CurrentPage: retData.CurrentPage,
		TotalPage:   totalPage,
		List:        items,
	}).FPrint(w)
}

func decodeStoredJSON(raw string, target any) error {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	if err := json.Unmarshal([]byte(raw), target); err != nil {
		return fmt.Errorf("decode stored JSON: %w", err)
	}
	return nil
}

func normalizeEmailListRequest(request *emailRequest) (int, error) {
	if request == nil {
		return 0, errors.New("request is nil")
	}
	if request.CurrentPage == 0 {
		request.CurrentPage = 1
	}
	if request.PageSize == 0 {
		request.PageSize = defaultEmailPageSize
	}
	if request.CurrentPage < 1 || request.CurrentPage > maxEmailPage || request.PageSize < 1 || request.PageSize > maxEmailPageSize {
		return 0, errors.New("pagination is out of range")
	}
	if len([]byte(request.Keyword)) > maxEmailKeywordBytes {
		return 0, errors.New("keyword is too long")
	}
	return (request.CurrentPage - 1) * request.PageSize, nil
}
