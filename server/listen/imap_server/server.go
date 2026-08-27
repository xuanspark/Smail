package imap_server

import (
	"git.pcux.cn/xuans/Smail/utils/context"
	"git.pcux.cn/xuans/Smail/utils/id"
	"github.com/emersion/go-imap/v2"
	log "github.com/sirupsen/logrus"
	"net"
	"sync"
	"time"

	"github.com/emersion/go-imap/v2/imapserver"
)

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
	connectTime    time.Time
	deleteUidList  []int
	remoteAddress  string
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
	log.WithContext(s.ctx).Errorf("Append Not Implemented")
	return nil, nil
}

func (s *serverSession) Unselect() error {
	s.currentMailbox = ""
	return nil
}
