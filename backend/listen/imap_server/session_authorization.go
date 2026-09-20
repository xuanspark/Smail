package imap_server

import (
	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
)

// authorizedSession 是网络会话的统一授权边界；业务实现不持有明文授权码。
type authorizedSession struct {
	*serverSession
}

var _ imapserver.Session = (*authorizedSession)(nil)

func protocolAuthorizationError() error {
	return &imap.Error{Type: imap.StatusResponseTypeNo, Code: imap.ResponseCodeAuthenticationFailed, Text: "Authentication credentials revoked"}
}

func (s *authorizedSession) check() error {
	if s.authorization == nil || s.authorization.Check() != nil {
		return protocolAuthorizationError()
	}
	return nil
}

func (s *authorizedSession) Select(mailbox string, options *imap.SelectOptions) (*imap.SelectData, error) {
	if err := s.check(); err != nil {
		return nil, err
	}
	return s.serverSession.Select(mailbox, options)
}

func (s *authorizedSession) Create(mailbox string, options *imap.CreateOptions) error {
	if err := s.check(); err != nil {
		return err
	}
	return s.serverSession.Create(mailbox, options)
}

func (s *authorizedSession) Delete(mailbox string) error {
	if err := s.check(); err != nil {
		return err
	}
	return s.serverSession.Delete(mailbox)
}

func (s *authorizedSession) Rename(mailbox, newName string) error {
	if err := s.check(); err != nil {
		return err
	}
	return s.serverSession.Rename(mailbox, newName)
}

func (s *authorizedSession) Subscribe(mailbox string) error {
	return s.check()
}

func (s *authorizedSession) Unsubscribe(mailbox string) error {
	return s.check()
}

func (s *authorizedSession) List(w *imapserver.ListWriter, ref string, patterns []string, options *imap.ListOptions) error {
	if err := s.check(); err != nil {
		return err
	}
	return s.serverSession.List(w, ref, patterns, options)
}

func (s *authorizedSession) Status(mailbox string, options *imap.StatusOptions) (*imap.StatusData, error) {
	if err := s.check(); err != nil {
		return nil, err
	}
	return s.serverSession.Status(mailbox, options)
}

func (s *authorizedSession) Append(mailbox string, r imap.LiteralReader, options *imap.AppendOptions) (*imap.AppendData, error) {
	if err := s.check(); err != nil {
		return nil, err
	}
	return s.serverSession.Append(mailbox, r, options)
}

func (s *authorizedSession) Expunge(w *imapserver.ExpungeWriter, uids *imap.UIDSet) error {
	if err := s.check(); err != nil {
		return err
	}
	return s.serverSession.Expunge(w, uids)
}

func (s *authorizedSession) Search(kind imapserver.NumKind, criteria *imap.SearchCriteria, options *imap.SearchOptions) (*imap.SearchData, error) {
	if err := s.check(); err != nil {
		return nil, err
	}
	return s.serverSession.Search(kind, criteria, options)
}

func (s *authorizedSession) Fetch(w *imapserver.FetchWriter, numbers imap.NumSet, options *imap.FetchOptions) error {
	if err := s.check(); err != nil {
		return err
	}
	return s.serverSession.Fetch(w, numbers, options)
}

func (s *authorizedSession) Store(w *imapserver.FetchWriter, numbers imap.NumSet, flags *imap.StoreFlags, options *imap.StoreOptions) error {
	if err := s.check(); err != nil {
		return err
	}
	return s.serverSession.Store(w, numbers, flags, options)
}

func (s *authorizedSession) Copy(numbers imap.NumSet, dest string) (*imap.CopyData, error) {
	if err := s.check(); err != nil {
		return nil, err
	}
	return s.serverSession.Copy(numbers, dest)
}

func (s *authorizedSession) Move(w *imapserver.MoveWriter, numbers imap.NumSet, dest string) error {
	if err := s.check(); err != nil {
		return err
	}
	return s.serverSession.Move(w, numbers, dest)
}

func (s *authorizedSession) Namespace() (*imap.NamespaceData, error) {
	if err := s.check(); err != nil {
		return nil, err
	}
	return s.serverSession.Namespace()
}

func (s *authorizedSession) Idle(w *imapserver.UpdateWriter, stop <-chan struct{}) error {
	if err := s.check(); err != nil {
		return err
	}
	return s.serverSession.Idle(w, stop)
}

func (s *authorizedSession) Unselect() error {
	if err := s.check(); err != nil {
		return err
	}
	return s.serverSession.Unselect()
}

func (s *authorizedSession) Poll(w *imapserver.UpdateWriter, allowExpunge bool) error {
	// 库在未登录状态也调用 Poll，此时不得触及邮箱数据。
	if s.authorization == nil {
		return nil
	}
	if err := s.check(); err != nil {
		return err
	}
	return s.serverSession.Poll(w, allowExpunge)
}
