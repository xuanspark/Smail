package rule

import (
	"fmt"
	"strings"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/dto"
	"git.pcux.cn/xuans/Smail/dto/parsemail"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/del_email"
	"git.pcux.cn/xuans/Smail/services/group"
	"git.pcux.cn/xuans/Smail/services/rule/match"
	"git.pcux.cn/xuans/Smail/utils/context"
	"git.pcux.cn/xuans/Smail/utils/send"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cast"
)

func GetAllRules(ctx *context.Context, userId int) ([]*dto.Rule, error) {
	if userId <= 0 {
		return nil, fmt.Errorf("invalid user id")
	}
	var storedRules []*models.Rule
	if err := db.Instance.Where("user_id=?", userId).Decr("sort").Find(&storedRules); err != nil {
		return nil, fmt.Errorf("load rules: %w", err)
	}
	ret := make([]*dto.Rule, 0, len(storedRules))
	for _, storedRule := range storedRules {
		decoded, err := (&dto.Rule{}).Decode(storedRule)
		if err != nil {
			return nil, fmt.Errorf("decode rule %d: %w", storedRule.Id, err)
		}
		ret = append(ret, decoded)
	}
	return ret, nil
}
func MatchRule(ctx *context.Context, rule *dto.Rule, email *parsemail.Email) bool {
	if rule == nil || email == nil || len(rule.Rules) == 0 || ruleContextDone(ctx) {
		return false
	}

	for _, r := range rule.Rules {
		if ruleContextDone(ctx) || r == nil {
			return false
		}
		var m match.Match

		switch r.Type {
		case match.RuleTypeRegex:
			m = match.NewRegexMatch(r.Field, r.Rule)
		case match.RuleTypeContains:
			m = match.NewContainsMatch(r.Field, r.Rule)
		case match.RuleTypeEq:
			m = match.NewEqualMatch(r.Field, r.Rule)
		}
		if m == nil {
			return false
		}

		if !m.Match(ctx, email) || ruleContextDone(ctx) {
			return false
		}
	}

	return true
}

func ruleContextDone(ctx *context.Context) bool {
	return ctx != nil && ctx.Context != nil && ctx.Err() != nil
}

func DoRule(ctx *context.Context, rule *dto.Rule, email *parsemail.Email, user *models.User, rawEmailData []byte) error {
	if ctx == nil || rule == nil || email == nil || user == nil || db.Instance == nil {
		return fmt.Errorf("invalid rule execution context")
	}
	if rule.UserId <= 0 || user.ID <= 0 || rule.UserId != user.ID {
		return fmt.Errorf("rule owner does not match execution user")
	}
	executionContext := *ctx
	executionContext.UserID = user.ID
	ctx = &executionContext
	log.WithContext(ctx).Debugf("执行规则:%s", rule.Name)

	switch rule.Action {
	case dto.READ:
		if email.MessageId <= 0 {
			return fmt.Errorf("email has not been saved")
		}
		var relation models.UserEmail
		found, err := db.Instance.Table(&models.UserEmail{}).Where("email_id=? and user_id=?", email.MessageId, user.ID).Get(&relation)
		if err != nil {
			return fmt.Errorf("load rule message relation: %w", err)
		}
		if !found {
			return fmt.Errorf("rule message relation not found")
		}
		if relation.IsRead == 1 {
			return nil
		}
		affected, err := db.Instance.Table(&models.UserEmail{}).Where("email_id=? and user_id=? and is_read=0", email.MessageId, user.ID).Cols("is_read").Update(map[string]interface{}{"is_read": 1})
		if err != nil {
			return fmt.Errorf("mark rule message as read: %w", err)
		}
		if affected != 1 {
			return fmt.Errorf("mark rule message as read affected %d rows", affected)
		}
	case dto.DELETE:
		if email.MessageId <= 0 {
			return fmt.Errorf("email has not been saved")
		}
		if err := del_email.DelEmail(ctx, []int{cast.ToInt(email.MessageId)}, false); err != nil {
			return fmt.Errorf("delete rule message: %w", err)
		}
	case dto.FORWARD:
		if err := doForward(ctx, email, rule.Params, user, rawEmailData); err != nil {
			return fmt.Errorf("forward rule message: %w", err)
		}
	case dto.MOVE:
		if err := doMove(ctx, rule, email); err != nil {
			return fmt.Errorf("move rule message: %w", err)
		}
	default:
		return fmt.Errorf("unsupported rule action")
	}
	return nil
}
func doForward(ctx *context.Context, email *parsemail.Email, forwardAddress string, user *models.User, rawEmailData []byte) error {
	forwardUser := parsemail.BuilderUser(forwardAddress)
	if forwardUser == nil || forwardUser.EmailAddress == "" {
		return fmt.Errorf("invalid forward address: %s", forwardAddress)
	}

	account, domain := forwardUser.GetDomainAccount()
	if account == "" || domain == "" {
		return fmt.Errorf("invalid forward address: %s", forwardAddress)
	}

	if isLocalDomain(domain) {
		if strings.EqualFold(account, user.Account) {
			return fmt.Errorf("loop forwarding to self: %s", forwardAddress)
		}
		return forwardToLocalUser(ctx, email, account, forwardAddress)
	}

	if len(rawEmailData) > 0 {
		err := send.ForwardRaw(ctx, email, rawEmailData, forwardAddress, user)
		if err == nil {
			return nil
		}
		log.WithContext(ctx).Warnf("Raw forward failed, retry remail forward:%v", err)
	}
	return send.Forward(ctx, email, forwardAddress, user)
}

func forwardToLocalUser(ctx *context.Context, email *parsemail.Email, account, forwardAddress string) error {
	if email.MessageId <= 0 {
		return fmt.Errorf("email has not been saved before local forwarding")
	}

	var user models.User
	has, err := db.Instance.Table(&models.User{}).
		Where("LOWER(account)=LOWER(?) and disabled=0", account).
		Get(&user)
	if err != nil {
		return err
	}
	if !has {
		return fmt.Errorf("local forward user not found: %s", forwardAddress)
	}
	session := db.Instance.NewSession()
	blocked, filterErr := models.BlacklistedRecipientIDs(session, []int{user.ID}, email.SenderFilterAddresses()...)
	session.Close()
	if filterErr != nil {
		return filterErr
	}
	if blocked[user.ID] {
		return fmt.Errorf("local forwarding rejected by recipient policy")
	}

	var ue models.UserEmail
	has, err = db.Instance.Table(&models.UserEmail{}).
		Where("email_id=? and user_id=?", email.MessageId, user.ID).
		Get(&ue)
	if err != nil {
		return err
	}
	if has {
		return nil
	}

	_, err = db.Instance.Insert(&models.UserEmail{
		UserID:  user.ID,
		EmailID: cast.ToInt(email.MessageId),
		Status:  cast.ToInt8(email.Status),
	})
	return err
}

func isLocalDomain(domain string) bool {
	domains := config.CurrentMailDomains()
	for _, localDomain := range domains.Accepted {
		if strings.EqualFold(domain, localDomain) {
			return true
		}
	}
	return strings.EqualFold(domain, domains.Primary)
}

func doMove(ctx *context.Context, rule *dto.Rule, email *parsemail.Email) error {
	if rule == nil || email == nil || email.MessageId <= 0 {
		return fmt.Errorf("invalid move rule context")
	}

	groupID := cast.ToInt(rule.Params)
	if name, ok := models.GroupCodeToName[groupID]; ok {
		return group.Move2DefaultBox(ctx, []int{cast.ToInt(email.MessageId)}, name)
	}
	return group.MoveMailToGroup(ctx, []int{cast.ToInt(email.MessageId)}, groupID)
}
