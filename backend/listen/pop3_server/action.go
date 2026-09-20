package pop3_server

import (
	errors2 "errors"
	"git.pcux.cn/xuans/Smail/consts"
	"git.pcux.cn/xuans/Smail/dto"
	"git.pcux.cn/xuans/Smail/dto/parsemail"
	"git.pcux.cn/xuans/Smail/dto/response"
	"git.pcux.cn/xuans/Smail/internal/gopop"
	"git.pcux.cn/xuans/Smail/services/access"
	"git.pcux.cn/xuans/Smail/services/auth"
	"git.pcux.cn/xuans/Smail/services/del_email"
	"git.pcux.cn/xuans/Smail/services/detail"
	"git.pcux.cn/xuans/Smail/services/list"
	"git.pcux.cn/xuans/Smail/utils/array"
	"git.pcux.cn/xuans/Smail/utils/context"
	"git.pcux.cn/xuans/Smail/utils/errors"
	"git.pcux.cn/xuans/Smail/utils/id"
	"git.pcux.cn/xuans/Smail/utils/password"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cast"
	"strconv"
	"strings"
)

type action struct {
}

// Custom 非标准命令
func (a action) Custom(session *gopop.Session, cmd string, args []string) ([]string, error) {
	if session.Ctx == nil {
		tc := &context.Context{}
		tc.SetValue(context.LogID, id.GenLogID())
		session.Ctx = tc
	}

	log.WithContext(session.Ctx).Debug("POP3 unsupported command")
	return nil, errors2.New("not supported cmd request")
}

// Capa 说明服务端支持的命令列表
func (a action) Capa(session *gopop.Session) ([]string, error) {
	if session.Ctx == nil {
		tc := &context.Context{}
		tc.SetValue(context.LogID, id.GenLogID())
		session.Ctx = tc
	}

	if session.InTls {
		log.WithContext(session.Ctx).Debugf("POP3 CMD: CAPA With Tls")
	} else {
		log.WithContext(session.Ctx).Debugf("POP3 CMD: CAPA Without Tls")
	}

	ret := []string{
		"USER",
		"PASS",
		"TOP",
		"STAT",
		"UIDL",
		"LIST",
		"RETR",
		"DELE",
		"RSET",
		"NOOP",
		"QUIT",
	}
	if !session.InTls {
		ret = append(ret, "STLS")
	}

	log.WithContext(session.Ctx).Debugf("CAPA \n %+v", ret)

	return ret, nil
}

// User 提交登陆的用户名
func (a action) User(session *gopop.Session, username string) error {
	if session.Ctx == nil {
		tc := &context.Context{}
		tc.SetValue(context.LogID, id.GenLogID())
		session.Ctx = tc
	}
	log.WithContext(session.Ctx).Debug("POP3 USER received")
	log.WithContext(session.Ctx).Debug("POP3 user name normalized")

	session.User = username
	return nil
}

// Pass 提交密码验证
func (a action) Pass(session *gopop.Session, pwd string) error {
	if session.Ctx == nil {
		tc := &context.Context{}
		tc.SetValue(context.LogID, id.GenLogID())
		session.Ctx = tc
	}
	if !session.InTls {
		log.WithContext(session.Ctx).Info("POP3 authentication rejected before TLS")
		return errors2.New("TLS required")
	}

	remoteAddress := ""
	if session.Conn != nil && session.Conn.RemoteAddr() != nil {
		remoteAddress = session.Conn.RemoteAddr().String()
	}
	user, authorization, err := auth.AuthenticateProtocolSession("pop3", remoteAddress, session.User, pwd)
	if err != nil {
		if errors2.Is(err, password.ErrBusy) {
			return gopop.ErrAuthenticationTemporary
		}
		log.WithContext(session.Ctx).WithFields(log.Fields{
			"action": "POP3 客户端登录",
			"result": "failure",
			"object": remoteAddress,
		}).Info("POP3 客户端认证失败")
		return errors2.New("password error")
	}

	ctx := session.Ctx.(*context.Context)
	session.Authorize = authorization.Check
	ctx.UserID = user.ID
	ctx.UserName = user.Name
	ctx.UserAccount = user.Account
	ctx.Role = user.EffectiveRole()
	ctx.IsAdmin = ctx.Role == access.RoleSuperAdmin || ctx.Role == access.RoleAdmin
	if _, err := a.ensureMessageMap(session); err != nil {
		ctx.UserID = 0
		return errors2.New("mailbox unavailable")
	}
	session.Status = gopop.TRANSACTION
	return nil
}

// Apop explicitly rejects the obsolete MD5-based authentication mechanism.
func (a action) Apop(session *gopop.Session, username, digest string) error {
	if session.Ctx == nil {
		tc := &context.Context{}
		tc.SetValue(context.LogID, id.GenLogID())
		session.Ctx = tc
	}
	log.WithContext(session.Ctx).Info("POP3 APOP is not supported")
	return errors2.New("APOP not supported")
}
func (a action) Stat(session *gopop.Session) (msgNum, msgSize int64, err error) {
	log.WithContext(session.Ctx).Debugf("POP3 CMD: STAT")
	emailList, err := a.visibleMessages(session)
	if err != nil {
		return 0, 0, err
	}
	for _, info := range emailList {
		msgNum++
		msgSize += int64(info.Size)
	}
	log.WithContext(session.Ctx).Debugf("POP3 STAT RETURT : %d,%d", msgNum, msgSize)
	return msgNum, msgSize, nil
}

// Uidl 查询某封邮件的唯一标志符
func (a action) Uidl(session *gopop.Session, msg string) ([]gopop.UidlItem, error) {
	log.WithContext(session.Ctx).Debugf("POP3 CMD: UIDL ,Args:%s", msg)

	if msg != "" {
		number, err := parseMessageNumber(msg)
		if err != nil {
			return nil, err
		}
		id, err := a.resolveMessageID(session, int64(number))
		if err != nil {
			return nil, err
		}
		return []gopop.UidlItem{{Id: int64(number), UnionId: cast.ToString(id)}}, nil
	}

	emailList, err := a.visibleMessages(session)
	if err != nil {
		return nil, err
	}
	ret := make([]gopop.UidlItem, 0, len(emailList))
	for _, info := range emailList {
		ret = append(ret, gopop.UidlItem{Id: int64(info.SerialNumber), UnionId: cast.ToString(info.Id)})
	}

	log.WithContext(session.Ctx).Debugf("Uidl \n %+v", ret)
	return ret, nil
}

type listItem struct {
	Id   int64 `json:"id"`
	Size int64 `json:"size"`
}

// List 邮件列表
func (a action) List(session *gopop.Session, msg string) ([]gopop.MailInfo, error) {
	log.WithContext(session.Ctx).Debugf("POP3 CMD: LIST ,Args:%s", msg)
	requestedNumber := 0
	if msg != "" {
		var err error
		requestedNumber, err = parseMessageNumber(msg)
		if err != nil {
			return nil, err
		}
	}
	emailList, err := a.visibleMessages(session)
	if err != nil {
		return nil, err
	}
	if requestedNumber > 0 {
		id, err := a.resolveMessageID(session, int64(requestedNumber))
		if err != nil {
			return nil, err
		}
		filtered := make([]*response.EmailResponseData, 0, 1)
		for _, info := range emailList {
			if cast.ToInt64(info.Id) == id {
				filtered = append(filtered, info)
				break
			}
		}
		emailList = filtered
	}
	ret := make([]gopop.MailInfo, 0, len(emailList))
	for _, info := range emailList {
		size := int64(info.Size)
		ret = append(ret, gopop.MailInfo{
			Id:   int64(info.SerialNumber),
			Size: size,
		})
	}

	log.WithContext(session.Ctx).Debugf("List \n %+v", ret)
	return ret, nil
}

// Retr 获取邮件详情
func (a action) Retr(session *gopop.Session, id int64) (string, int64, error) {
	log.WithContext(session.Ctx).Debugf("POP3 CMD: RETR ,Args:%d", id)
	actualID, err := a.resolveMessageID(session, id)
	if err != nil {
		return "", 0, err
	}
	email, err := detail.GetEmailDetail(session.Ctx.(*context.Context), cast.ToInt(actualID), false)
	if err != nil {
		log.WithContext(session.Ctx.(*context.Context)).Errorf("%+v", err)
		return "", 0, errors.New("server error")
	}

	parsedEmail, err := parsemail.NewEmailFromModel(email.Email)
	if err != nil {
		log.WithContext(session.Ctx.(*context.Context)).WithError(err).Error("decode POP3 RETR message failed")
		return "", 0, errors.New("server error")
	}
	ret, err := parsedEmail.BuildBytes(session.Ctx.(*context.Context), false)
	if err != nil {
		log.WithContext(session.Ctx.(*context.Context)).WithError(err).Error("build POP3 RETR message failed")
		return "", 0, errors.New("server error")
	}
	log.WithContext(session.Ctx).Debugf("POP3 RETR response_bytes=%d", len(ret))
	return string(ret), cast.ToInt64(len(ret)), nil

}

// Delete 删除邮件
func (a action) Delete(session *gopop.Session, id int64) error {
	log.WithContext(session.Ctx).Debugf("POP3 CMD: DELE ,Args:%d", id)
	actualID, err := a.resolveMessageID(session, id)
	if err != nil {
		return err
	}
	for _, deletedID := range session.DeleteIds {
		if deletedID == actualID {
			return errors2.New("message already marked for deletion")
		}
	}
	session.DeleteIds = append(session.DeleteIds, actualID)
	session.DeleteIds = array.Unique(session.DeleteIds)
	return nil
}

func (a action) Rest(session *gopop.Session) error {
	log.WithContext(session.Ctx).Debugf("POP3 CMD: RSET ")
	session.DeleteIds = []int64{}
	return nil
}

func (a action) Top(session *gopop.Session, id int64, n int) (string, error) {
	log.WithContext(session.Ctx).Debugf("POP3 CMD: TOP %d %d", id, n)
	if n < 0 {
		return "", errors2.New("invalid line count")
	}
	actualID, err := a.resolveMessageID(session, id)
	if err != nil {
		return "", err
	}
	email, err := detail.GetEmailDetail(session.Ctx.(*context.Context), cast.ToInt(actualID), false)
	if err != nil {
		log.WithContext(session.Ctx.(*context.Context)).Errorf("%+v", err)
		return "", errors2.New("password error")
	}

	parsedEmail, err := parsemail.NewEmailFromModel(email.Email)
	if err != nil {
		log.WithContext(session.Ctx.(*context.Context)).WithError(err).Error("decode POP3 TOP message failed")
		return "", errors2.New("server error")
	}
	ret, err := parsedEmail.BuildBytes(session.Ctx.(*context.Context), false)
	if err != nil {
		log.WithContext(session.Ctx.(*context.Context)).WithError(err).Error("build POP3 TOP message failed")
		return "", errors2.New("server error")
	}
	res := strings.Split(string(ret), "\n")
	headerEndLine := len(res) - 1
	for i, re := range res {
		if re == "\r" {
			headerEndLine = i
			break
		}
	}
	// 先比较剩余行数，避免极大的合法整数在加法中溢出。
	if n >= len(res)-headerEndLine-1 {
		return string(ret), nil
	}

	lines := array.Join(res[0:headerEndLine+n+1], "\n")
	log.WithContext(session.Ctx).Debugf("POP3 TOP response_bytes=%d", len(lines))
	return lines, nil

}

func parseMessageNumber(value string) (int, error) {
	number, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || number <= 0 {
		return 0, errors2.New("no such message")
	}
	return number, nil
}

func (a action) ensureMessageMap(session *gopop.Session) ([]*response.EmailResponseData, error) {
	if session.Authorize != nil {
		if err := session.Authorize(); err != nil {
			return nil, err
		}
	}
	if session.MessageMap != nil {
		return nil, nil
	}
	emailList, _, err := list.GetEmailList(session.Ctx.(*context.Context), dto.SearchTag{Type: consts.EmailTypeReceive, Status: -1, GroupId: -1}, "", true, 0, 99999)
	if err != nil {
		return nil, err
	}
	session.MessageMap = make(map[int64]int64, len(emailList))
	session.MessageSizes = make(map[int64]int, len(emailList))
	session.MessageIDs = make([]int64, 0, len(emailList))
	for number, info := range emailList {
		id := cast.ToInt64(info.Id)
		session.MessageMap[int64(number+1)] = id
		session.MessageSizes[id] = info.Size
		info.SerialNumber = number + 1
		session.MessageIDs = append(session.MessageIDs, id)
	}
	return emailList, nil
}

func (a action) resolveMessageID(session *gopop.Session, number int64) (int64, error) {
	if number <= 0 {
		return 0, errors2.New("no such message")
	}
	if _, err := a.ensureMessageMap(session); err != nil {
		return 0, err
	}
	id, ok := session.MessageMap[number]
	if !ok {
		return 0, errors2.New("no such message")
	}
	for _, deletedID := range session.DeleteIds {
		if deletedID == id {
			return 0, errors2.New("message already marked for deletion")
		}
	}
	return id, nil
}

func (a action) visibleMessages(session *gopop.Session) ([]*response.EmailResponseData, error) {
	if _, err := a.ensureMessageMap(session); err != nil {
		return nil, err
	}
	// 列表和单封命令使用同一会话快照，删除标记不能压缩消息编号。
	result := make([]*response.EmailResponseData, 0, len(session.MessageIDs))
	for number, id := range session.MessageIDs {
		marked := false
		for _, deletedID := range session.DeleteIds {
			if deletedID == id {
				marked = true
				break
			}
		}
		if !marked {
			info := &response.EmailResponseData{SerialNumber: number + 1}
			info.Id = cast.ToInt(id)
			info.Size = session.MessageSizes[id]
			result = append(result, info)
		}
	}
	return result, nil
}

func (a action) Noop(session *gopop.Session) error {
	log.WithContext(session.Ctx).Debugf("POP3 CMD: NOOP ")
	return nil
}

func (a action) Quit(session *gopop.Session) error {
	if session.Authorize != nil {
		if err := session.Authorize(); err != nil {
			return err
		}
	}
	log.WithContext(session.Ctx).Debugf("POP3 CMD: QUIT ")

	var DelIds []int

	if len(session.DeleteIds) > 0 {
		for _, delId := range session.DeleteIds {
			DelIds = append(DelIds, cast.ToInt(delId))
		}

		if err := del_email.DelEmail(session.Ctx.(*context.Context), DelIds, false); err != nil {
			log.WithContext(session.Ctx).WithError(err).Error("POP3 mailbox update failed")
			return errors2.New("mailbox update failed")
		}
	}

	return nil
}
