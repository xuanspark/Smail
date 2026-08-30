package pop3_server

import (
	errors2 "errors"
	"git.pcux.cn/xuans/Smail/consts"
	"git.pcux.cn/xuans/Smail/dto"
	"git.pcux.cn/xuans/Smail/dto/parsemail"
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
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cast"
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
		"REST",
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
	user, err := auth.AuthenticateProtocol("pop3", remoteAddress, session.User, pwd)
	if err != nil {
		log.WithContext(session.Ctx).Info("POP3 authentication failed")
		return errors2.New("password error")
	}

	session.Status = gopop.TRANSACTION
	ctx := session.Ctx.(*context.Context)
	ctx.UserID = user.ID
	ctx.UserName = user.Name
	ctx.UserAccount = user.Account
	ctx.Role = user.EffectiveRole()
	ctx.IsAdmin = ctx.Role == access.RoleSuperAdmin || ctx.Role == access.RoleAdmin
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

	num, size, err := list.Stat(session.Ctx.(*context.Context))
	if err != nil {
		return 0, 0, err
	}
	log.WithContext(session.Ctx).Debugf("POP3 STAT RETURT : %d,%d", num, size)
	return num, size, nil
}

// Uidl 查询某封邮件的唯一标志符
func (a action) Uidl(session *gopop.Session, msg string) ([]gopop.UidlItem, error) {
	log.WithContext(session.Ctx).Debugf("POP3 CMD: UIDL ,Args:%s", msg)

	reqId := cast.ToInt64(msg)
	if reqId > 0 {
		log.WithContext(session.Ctx).Debugf("Uidl \n %+v", reqId)
		return []gopop.UidlItem{
			{
				Id:      reqId,
				UnionId: msg,
			},
		}, nil
	}

	var res []listItem

	emailList, _, err := list.GetEmailList(session.Ctx.(*context.Context), dto.SearchTag{Type: consts.EmailTypeReceive, Status: -1, GroupId: -1}, "", true, 0, 99999)
	if err != nil {
		return nil, err
	}
	for _, info := range emailList {
		res = append(res, listItem{
			Id:   cast.ToInt64(info.Id),
			Size: cast.ToInt64(info.Size),
		})
	}
	ret := []gopop.UidlItem{}
	for _, re := range res {
		ret = append(ret, gopop.UidlItem{
			Id:      re.Id,
			UnionId: cast.ToString(re.Id),
		})
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
	var res []listItem
	var listId int
	if msg != "" {
		listId = cast.ToInt(msg)
		if listId == 0 {
			return nil, errors.New("params error")
		}
	}

	if listId != 0 {
		info, err := detail.GetEmailDetail(session.Ctx.(*context.Context), listId, false)
		if err != nil {
			return nil, err
		}
		item := listItem{
			Id:   cast.ToInt64(info.Id),
			Size: cast.ToInt64(info.Size),
		}
		if item.Size == 0 {
			item.Size = 9999
		}
		res = append(res, item)
	} else {
		emailList, _, err := list.GetEmailList(session.Ctx.(*context.Context), dto.SearchTag{Type: consts.EmailTypeReceive, Status: -1, GroupId: -1}, "", true, 0, 99999)
		if err != nil {
			return nil, err
		}
		for _, info := range emailList {
			item := listItem{
				Id:   cast.ToInt64(info.Id),
				Size: cast.ToInt64(info.Size),
			}
			if item.Size == 0 {
				item.Size = 9999
			}
			res = append(res, item)
		}
	}
	ret := []gopop.MailInfo{}
	for _, re := range res {
		ret = append(ret, gopop.MailInfo{
			Id:   re.Id,
			Size: re.Size,
		})
	}

	log.WithContext(session.Ctx).Debugf("List \n %+v", ret)
	return ret, nil
}

// Retr 获取邮件详情
func (a action) Retr(session *gopop.Session, id int64) (string, int64, error) {
	log.WithContext(session.Ctx).Debugf("POP3 CMD: RETR ,Args:%d", id)
	email, err := detail.GetEmailDetail(session.Ctx.(*context.Context), cast.ToInt(id), false)
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

	session.DeleteIds = append(session.DeleteIds, id)
	session.DeleteIds = array.Unique(session.DeleteIds)
	return nil
}

func (a action) Rest(session *gopop.Session) error {
	log.WithContext(session.Ctx).Debugf("POP3 CMD: REST ")
	session.DeleteIds = []int64{}
	return nil
}

func (a action) Top(session *gopop.Session, id int64, n int) (string, error) {
	log.WithContext(session.Ctx).Debugf("POP3 CMD: TOP %d %d", id, n)
	email, err := detail.GetEmailDetail(session.Ctx.(*context.Context), cast.ToInt(id), false)
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
	if len(res) <= headerEndLine+n+1 {
		return string(ret), nil
	}

	lines := array.Join(res[0:headerEndLine+n+1], "\n")
	log.WithContext(session.Ctx).Debugf("POP3 TOP response_bytes=%d", len(lines))
	return lines, nil

}

func (a action) Noop(session *gopop.Session) error {
	log.WithContext(session.Ctx).Debugf("POP3 CMD: NOOP ")
	return nil
}

func (a action) Quit(session *gopop.Session) error {
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
