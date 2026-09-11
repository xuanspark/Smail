package send

import (
	"crypto/tls"
	"errors"
	"fmt"
	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/dto/parsemail"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/logstore"
	"git.pcux.cn/xuans/Smail/utils/array"
	"git.pcux.cn/xuans/Smail/utils/async"
	"git.pcux.cn/xuans/Smail/utils/consts"
	"git.pcux.cn/xuans/Smail/utils/context"
	"git.pcux.cn/xuans/Smail/utils/smtp"
	log "github.com/sirupsen/logrus"
	"net"
	"net/mail"
	"net/textproto"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
)

type mxDomain struct {
	domain    string
	mxHost    string
	localTest bool
}

type temporaryMXFallbackError struct {
	lookupErr   error
	fallbackErr error
}

func (e *temporaryMXFallbackError) Error() string {
	return fmt.Sprintf("temporary MX lookup failed (%v); fallback delivery failed: %v", e.lookupErr, e.fallbackErr)
}

func (e *temporaryMXFallbackError) Unwrap() error {
	return e.lookupErr
}

// Forward 转发邮件
func Forward(ctx *context.Context, e *parsemail.Email, forwardAddress string, user *models.User) error {

	log.WithContext(ctx).Debugf("开始转发邮件")

	b, err := e.ForwardBuildBytes(ctx, user, forwardAddress)
	if err != nil {
		return fmt.Errorf("build forwarded message: %w", err)
	}

	log.WithContext(ctx).Debug("forward message prepared")

	domains := config.CurrentMailDomains()
	if len(domains.Accepted) == 0 {
		return errors.New("runtime configuration is unavailable")
	}
	from := user.Account + "@" + domains.Primary
	return forwardData(ctx, domains.Primary, b, forwardAddress, from)
}

func ForwardRaw(ctx *context.Context, e *parsemail.Email, rawEmailData []byte, forwardAddress string, user *models.User) error {
	log.WithContext(ctx).Debugf("开始原始邮件转发")

	domains := config.CurrentMailDomains()
	if len(domains.Accepted) == 0 {
		return errors.New("runtime configuration is unavailable")
	}
	from := user.Account + "@" + domains.Primary
	return forwardData(ctx, domains.Primary, rawEmailData, forwardAddress, from)
}

func forwardData(ctx *context.Context, fromDomain string, data []byte, forwardAddress string, from string) error {
	var to []*parsemail.User
	to = []*parsemail.User{
		{EmailAddress: forwardAddress},
	}

	err, _ := doSend(ctx, fromDomain, data, to, from, "邮件规则转发")
	return err
}

func Send(ctx *context.Context, e *parsemail.Email) (error, map[string]error) {
	return SendTriggered(ctx, e, "SMTP 客户端提交")
}

// SendTriggered 显式标记业务发起方，不从邮件主题或内容推断来源。
func SendTriggered(ctx *context.Context, e *parsemail.Email, trigger string) (error, map[string]error) {
	_, fromDomain := e.From.GetDomainAccount()

	b, err := e.BuildBytes(ctx, true)
	if err != nil {
		userID := 0
		if ctx != nil {
			userID = ctx.UserID
		}
		logstore.MailResult(userID, "outbound", e.From.EmailAddress, len(e.To)+len(e.Cc)+len(e.Bcc), 0, "failure",
			logstore.MailDetails{Trigger: trigger, Stage: "构建待发邮件", Outcome: "邮件编码或签名失败，未开始对外投递"})
		return fmt.Errorf("build message: %w", err), nil
	}

	var to []*parsemail.User
	to = append(append(append(to, e.To...), e.Cc...), e.Bcc...)

	return doSend(ctx, fromDomain, b, to, e.From.EmailAddress, trigger)

}

func parseRecipientAddress(user *parsemail.User) (string, string, error) {
	if user == nil {
		return "", "", errors.New("recipient is nil")
	}
	address := strings.TrimSpace(user.EmailAddress)
	if address == "" || address != user.EmailAddress {
		return address, "", errors.New("recipient address is empty or contains surrounding whitespace")
	}
	parsed, err := mail.ParseAddress(address)
	if err != nil || parsed.Address != address {
		return address, "", errors.New("recipient address is invalid")
	}
	at := strings.LastIndexByte(address, '@')
	if at <= 0 || at >= len(address)-1 {
		return address, "", errors.New("recipient address has no valid domain")
	}
	domain := strings.ToLower(address[at+1:])
	if strings.ContainsAny(domain, " \t\r\n") {
		return address, "", errors.New("recipient domain is invalid")
	}
	return address, domain, nil
}

func doSend(ctx *context.Context, fromDomain string, data []byte, to []*parsemail.User, from string, triggers ...string) (sendErr error, recipientErrors map[string]error) {
	var accepted atomic.Int64
	trigger := "邮件发送"
	if len(triggers) > 0 {
		trigger = triggers[0]
	}
	defer func() {
		result := deliveryLogResult(int(accepted.Load()), len(to), sendErr != nil || len(recipientErrors) > 0)
		userID := 0
		if ctx != nil {
			userID = ctx.UserID
		}
		details := logstore.MailDetails{Trigger: trigger, Stage: "对外投递", AcceptedCount: int(accepted.Load())}
		if result == "failure" || result == "partial" {
			details.Outcome = logstore.MailFailureReason(sendErr)
			// 多域名失败按类别去重并排序，避免随机挑选某一个错误误导排查。
			reasons := make(map[string]bool)
			for _, cause := range recipientErrors {
				if cause != nil {
					reasons[logstore.MailFailureReason(cause)] = true
				}
			}
			if len(reasons) > 0 {
				messages := make([]string, 0, len(reasons))
				for reason := range reasons {
					messages = append(messages, reason)
				}
				sort.Strings(messages)
				details.Outcome = strings.Join(messages, "；")
			}
			if result == "partial" {
				details.Outcome = "部分收件人已被对端接收；其余投递失败：" + details.Outcome
			}
		}
		logstore.MailResult(userID, "outbound", from, len(to), len(data), result, details)
	}()
	if len(to) == 0 {
		err := errors.New("no recipients")
		return err, map[string]error{"recipients": err}
	}
	invalidAddresses := make([]string, 0)
	invalidErrors := make(map[string]error)

	// 按域名整理
	toByDomain := map[mxDomain][]*parsemail.User{}
	mxLookupErrors := map[mxDomain]error{}
	for _, s := range to {
		address, domain, addressErr := parseRecipientAddress(s)
		if addressErr != nil {
			if address == "" {
				address = "<empty>"
			}
			invalidAddresses = append(invalidAddresses, address)
			invalidErrors[address] = addressErr
			log.WithContext(ctx).WithError(addressErr).Error("invalid recipient address")
			continue
		}
		args := []string{address, domain}
		if len(args) == 2 {
			if args[1] == consts.TEST_DOMAIN {
				// 测试使用
				address := mxDomain{
					domain:    "localhost",
					mxHost:    "127.0.0.1",
					localTest: true,
				}
				toByDomain[address] = append(toByDomain[address], s)
			} else {
				//查询dns mx记录
				mxInfo, lookupErr := net.LookupMX(args[1])
				address := mxDomain{
					domain: "smtp." + args[1],
					mxHost: "smtp." + args[1],
				}
				if lookupErr != nil {
					log.WithContext(ctx).Errorf("%s 域名mx记录查询失败，检查邮箱是否存在！", s.EmailAddress)
				}
				if len(mxInfo) > 0 {
					address = mxDomain{
						domain: args[1],
						mxHost: mxInfo[0].Host,
					}
				}
				if lookupErr != nil {
					mxLookupErrors[address] = lookupErr
				}
				toByDomain[address] = append(toByDomain[address], s)
			}
		} else {
			log.WithContext(ctx).Errorf("邮箱地址解析错误！ %s", s)
			continue
		}
	}

	errEmailAddress := append([]string(nil), invalidAddresses...)
	var errEmailAddressMu sync.Mutex

	errMap := sync.Map{}
	for address, err := range invalidErrors {
		errMap.Store(address, err)
	}

	as := async.New(ctx)
	for domain, tos := range toByDomain {
		domain := domain
		tos := tos
		mxLookupErr := mxLookupErrors[domain]
		as.WaitProcess(func(p any) {
			recordFailure := func(err error) {
				err = deliveryFailureCause(mxLookupErr, err)
				log.WithContext(ctx).Errorf("%v 邮件投递失败%+v", tos, err)

				errEmailAddressMu.Lock()
				for _, user := range tos {
					errEmailAddress = append(errEmailAddress, user.EmailAddress)
				}
				errEmailAddressMu.Unlock()

				errMap.Store(domain.domain, err)
			}

			if allowUnsafeSMTP(domain) {
				err := smtp.SendMailUnsafe("localhost", domain.mxHost+":25", nil, from, fromDomain, buildAddress(tos), data)
				if err != nil {
					recordFailure(err)
				} else {
					accepted.Add(int64(len(tos)))
				}
				return
			}

			// 优先尝试25端口，starttls方式投递
			err := smtp.SendMail("", domain.mxHost+":25", nil, from, fromDomain, buildAddress(tos), data)
			if err == nil {
				accepted.Add(int64(len(tos)))
				return
			}
			if isCertificateVerificationError(err) {
				recordFailure(err)
				return
			}
			if isPermanentSMTPResponse(err) {
				recordFailure(err)
				return
			}
			log.WithContext(ctx).WithFields(log.Fields{
				"action": "SMTP 邮件投递",
				"result": "failure",
				"object": domain.mxHost + ":25",
			}).Infof("SMTP 25 端口 STARTTLS 投递失败: %s", err.Error())

			// 再试用587投递
			err = smtp.SendMailWithTls(strings.TrimSuffix(domain.mxHost, "."), domain.mxHost+":587", nil, from, fromDomain, buildAddress(tos), data)
			if err == nil {
				accepted.Add(int64(len(tos)))
				return
			}
			if isCertificateVerificationError(err) {
				recordFailure(err)
				return
			}
			if isPermanentSMTPResponse(err) {
				recordFailure(err)
				return
			}
			log.WithContext(ctx).WithFields(log.Fields{
				"action": "SMTP 邮件投递",
				"result": "failure",
				"object": domain.mxHost + ":587",
			}).Infof("SMTP 587 端口投递失败: %s", err.Error())

			// 再次尝试465投递
			err = smtp.SendMailWithTls(strings.TrimSuffix(domain.mxHost, "."), domain.mxHost+":465", nil, from, fromDomain, buildAddress(tos), data)
			if err == nil {
				accepted.Add(int64(len(tos)))
				return
			}
			if isCertificateVerificationError(err) {
				recordFailure(err)
				return
			}
			if isPermanentSMTPResponse(err) {
				recordFailure(err)
				return
			}
			log.WithContext(ctx).WithFields(log.Fields{
				"action": "SMTP 邮件投递",
				"result": "failure",
				"object": domain.mxHost + ":465",
			}).Infof("SMTP 465 端口投递失败: %s", err.Error())

			// 所有安全投递方式均失败后记录错误，不允许回退到明文连接。
			recordFailure(err)
		}, nil)
	}
	as.Wait()

	orgMap := map[string]error{}
	errMap.Range(func(key, value any) bool {
		if value != nil {
			orgMap[key.(string)] = value.(error)
		} else {
			orgMap[key.(string)] = nil
		}

		return true
	})

	if len(errEmailAddress) > 0 {
		return errors.New("以下收件人投递失败：" + array.Join(errEmailAddress, ",")), orgMap
	}
	return nil, orgMap
}

// deliveryLogResult 以实际接收人数为依据，不把函数正常返回等同于全部投递成功。
func deliveryLogResult(accepted, total int, failed bool) string {
	if accepted > 0 {
		if accepted == total && !failed {
			return "accepted"
		}
		return "partial"
	}
	if failed {
		return "failure"
	}
	return "unknown"
}

func allowUnsafeSMTP(domain mxDomain) bool {
	return domain.localTest && domain.domain == "localhost" && domain.mxHost == "127.0.0.1"
}

func isCertificateVerificationError(err error) bool {
	var certificateErr *tls.CertificateVerificationError
	return errors.As(err, &certificateErr)
}

func isPermanentSMTPResponse(err error) bool {
	var protocolErr *textproto.Error
	return errors.As(err, &protocolErr) && protocolErr.Code >= 500 && protocolErr.Code <= 599
}

func deliveryFailureCause(mxLookupErr, fallbackErr error) error {
	if fallbackErr == nil || isPermanentSMTPResponse(fallbackErr) {
		return fallbackErr
	}

	var lookupDNSErr *net.DNSError
	if !errors.As(mxLookupErr, &lookupDNSErr) || (!lookupDNSErr.IsTimeout && !lookupDNSErr.IsTemporary) {
		return fallbackErr
	}

	var networkErr net.Error
	if !errors.As(fallbackErr, &networkErr) {
		return fallbackErr
	}

	return &temporaryMXFallbackError{
		lookupErr:   mxLookupErr,
		fallbackErr: fallbackErr,
	}
}

func buildAddress(u []*parsemail.User) []string {
	var ret []string

	for _, user := range u {
		ret = append(ret, user.EmailAddress)

	}

	return ret
}
