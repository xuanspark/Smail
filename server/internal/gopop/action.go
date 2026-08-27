package gopop

type MailInfo struct {
	Id   int64
	Size int64
}

type UidlItem struct {
	Id      int64
	UnionId string
}

type Action interface {
	User(session *Session, username string) error
	Pass(session *Session, password string) error
	Apop(session *Session, username, digest string) error
	Stat(session *Session) (msgNum, msgSize int64, err error)
	Uidl(session *Session, msg string) ([]UidlItem, error)
	List(session *Session, msg string) ([]MailInfo, error)
	Retr(session *Session, id int64) (string, int64, error)
	Delete(session *Session, id int64) error
	Rest(session *Session) error
	Top(session *Session, id int64, n int) (string, error)
	Noop(session *Session) error
	Quit(session *Session) error
	Capa(session *Session) ([]string, error)
	Custom(session *Session, cmd string, args []string) ([]string, error)
}
