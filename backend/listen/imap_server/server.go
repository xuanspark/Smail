package imap_server

import (
	"bytes"
	"database/sql"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/consts"
	"git.pcux.cn/xuans/Smail/db"
	"git.pcux.cn/xuans/Smail/dto/parsemail"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/services/group"
	"git.pcux.cn/xuans/Smail/utils/context"
	"git.pcux.cn/xuans/Smail/utils/id"
	"github.com/emersion/go-imap/v2"

	"github.com/emersion/go-imap/v2/imapserver"
	"xorm.io/xorm"
)

const maxIMAPAppendBytes = 30 << 20

// Server is a server instance.
//
// A server contains a list of users.
type Server struct {
	mutex sync.Mutex
}

// NewServer creates a new server.
func NewServer() *Server {
	return &Server{}
}

type Status int8

const (
	UNAUTHORIZED Status = 1
	AUTHORIZED   Status = 2
	SELECTED     Status = 3
	LOGOUT       Status = 4
)

type serverSession struct {
	server         *Server // immutable
	ctx            *context.Context
	status         Status
	currentMailbox string
	readOnly       bool
	connectTime    time.Time
	deleteUidList  []int
	remoteAddress  string
	mailboxMu      sync.Mutex
	mailboxUIDs    []int
}

// NewSession creates a new IMAP session.
func (s *Server) NewSession(remoteAddresses ...net.Addr) imapserver.Session {
	tc := &context.Context{}
	tc.SetValue(context.LogID, id.GenLogID())
	remoteAddress := ""
	if len(remoteAddresses) > 0 && remoteAddresses[0] != nil {
		remoteAddress = remoteAddresses[0].String()
	}

	return &serverSession{
		server:        s,
		ctx:           tc,
		connectTime:   time.Now(),
		remoteAddress: remoteAddress,
	}
}

func (s *serverSession) Close() error {
	return nil
}

func (s *serverSession) Subscribe(mailbox string) error {
	return nil
}

func (s *serverSession) Unsubscribe(mailbox string) error {
	return nil
}

func (s *serverSession) Append(mailbox string, r imap.LiteralReader, options *imap.AppendOptions) (*imap.AppendData, error) {
	if s == nil || s.ctx == nil || s.ctx.UserID <= 0 || s.status < AUTHORIZED {
		return nil, &imap.Error{
			Type: imap.StatusResponseTypeNo,
			Code: imap.ResponseCodeAuthenticationFailed,
			Text: "Authentication required",
		}
	}
	if r == nil {
		return nil, appendBadError("Message content is missing")
	}

	mailbox = strings.Trim(strings.TrimSpace(mailbox), `"`)
	if mailbox == "" {
		return nil, appendBadError("Mailbox is missing")
	}

	groupID, uidValidity, err := resolveAppendMailbox(s.ctx, mailbox)
	if err != nil {
		return nil, err
	}

	if r.Size() < 0 || r.Size() > maxIMAPAppendBytes {
		return nil, appendTooBigError()
	}
	rawMessage, err := io.ReadAll(io.LimitReader(r, maxIMAPAppendBytes+1))
	if err != nil {
		return nil, appendNoError("Read message failed")
	}
	if len(rawMessage) == 0 {
		return nil, appendBadError("Message content is empty")
	}
	if len(rawMessage) > maxIMAPAppendBytes {
		return nil, appendTooBigError()
	}

	parsed, err := parsemail.NewEmailFromReader(nil, bytes.NewReader(rawMessage), len(rawMessage))
	if err != nil {
		return nil, appendNoError("Invalid message content")
	}
	if parsed == nil || parsed.From == nil {
		return nil, appendNoError("Message sender is missing")
	}

	status, emailType := appendMailboxStatus(mailbox)
	if emailType == int8(consts.EmailTypeSend) && strings.TrimSpace(parsed.From.EmailAddress) == "" {
		return nil, appendNoError("Message sender is missing")
	}

	domains := config.CurrentMailDomains()
	if parsed.MsgID == "" {
		if domains.Primary == "" {
			return nil, appendNoError("Message ID domain is not configured")
		}
		parsed.MsgID, err = parsemail.GenerateMsgID(domains.Primary)
		if err != nil {
			return nil, appendNoError("Generate message ID failed")
		}
	}

	storedFields, err := parsed.EncodeStoredFields()
	if err != nil {
		return nil, appendNoError("Encode message failed")
	}

	appendTime := time.Now()
	if options != nil && !options.Time.IsZero() {
		appendTime = options.Time
	}
	modelEmail := models.Email{
		Type:         emailType,
		Subject:      parsed.Subject,
		ReplyTo:      storedFields.ReplyTo,
		FromName:     parsed.From.Name,
		FromAddress:  parsed.From.EmailAddress,
		To:           storedFields.To,
		Bcc:          storedFields.Bcc,
		Cc:           storedFields.Cc,
		Text:         sql.NullString{String: string(parsed.Text), Valid: true},
		Html:         sql.NullString{String: string(parsed.HTML), Valid: true},
		Sender:       storedFields.Sender,
		Attachments:  storedFields.Attachments,
		Size:         len(rawMessage),
		Status:       status,
		SendUserID:   s.ctx.UserID,
		SendDate:     appendTime,
		CreateTime:   appendTime,
		CronSendTime: appendTime,
		MsgID:        parsed.MsgID,
	}

	var savedUID int
	if err := db.Transaction(db.Instance, func(session *xorm.Session) error {
		candidate := modelEmail
		candidate.Id = 0
		if _, err := session.Insert(&candidate); err != nil {
			return fmt.Errorf("insert appended email: %w", err)
		}
		if candidate.Id <= 0 {
			return fmt.Errorf("insert appended email returned no id")
		}
		relation := models.UserEmail{
			UserID:  s.ctx.UserID,
			EmailID: candidate.Id,
			IsRead:  appendMessageRead(options),
			GroupId: groupID,
			Status:  status,
		}
		if _, err := session.Insert(&relation); err != nil {
			return fmt.Errorf("insert appended mailbox relation: %w", err)
		}
		savedUID = relation.ID
		return nil
	}); err != nil {
		return nil, appendNoError("Save message failed")
	}

	return &imap.AppendData{
		UID:         imap.UID(savedUID),
		UIDValidity: uint32(uidValidity),
	}, nil
}

func resolveAppendMailbox(ctx *context.Context, mailbox string) (int, int, error) {
	if group.IsDefaultBox(mailbox) {
		uidValidity, ok := models.GroupNameToCode[mailbox]
		if !ok {
			return 0, 0, appendNoError("Mailbox not found")
		}
		return 0, uidValidity, nil
	}
	mailboxGroup, err := group.GetGroupByFullPath(ctx, mailbox)
	if err != nil {
		return 0, 0, appendNoError("Mailbox lookup failed")
	}
	if mailboxGroup == nil || mailboxGroup.ID <= 0 || mailboxGroup.UserId != ctx.UserID {
		return 0, 0, &imap.Error{
			Type: imap.StatusResponseTypeNo,
			Code: imap.ResponseCodeTryCreate,
			Text: "No such mailbox",
		}
	}
	return mailboxGroup.ID, mailboxGroup.ID, nil
}

func appendMailboxStatus(mailbox string) (int8, int8) {
	switch mailbox {
	case "Sent Messages", "Drafts":
		if mailbox == "Drafts" {
			return consts.EmailStatusDrafts, consts.EmailTypeSend
		}
		return consts.EmailStatusSent, consts.EmailTypeSend
	case "Deleted Messages":
		return consts.EmailStatusDel, consts.EmailTypeReceive
	case "Junk":
		return consts.EmailStatusJunk, consts.EmailTypeReceive
	default:
		return consts.EmailStatusWait, consts.EmailTypeReceive
	}
}

func appendMessageRead(options *imap.AppendOptions) int8 {
	if options == nil {
		return 0
	}
	for _, flag := range options.Flags {
		if flag == imap.FlagSeen {
			return 1
		}
	}
	return 0
}

func appendBadError(text string) error {
	return &imap.Error{Type: imap.StatusResponseTypeBad, Text: text}
}

func appendNoError(text string) error {
	return &imap.Error{Type: imap.StatusResponseTypeNo, Text: text}
}

func appendTooBigError() error {
	return &imap.Error{
		Type: imap.StatusResponseTypeNo,
		Code: imap.ResponseCodeTooBig,
		Text: "Message exceeds the maximum size",
	}
}

func (s *serverSession) Unselect() error {
	s.mailboxMu.Lock()
	defer s.mailboxMu.Unlock()
	s.currentMailbox = ""
	s.readOnly = false
	s.mailboxUIDs = nil
	return nil
}
