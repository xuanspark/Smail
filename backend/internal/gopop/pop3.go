package gopop

import (
	"bufio"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

const maxPOP3CommandLineBytes = 8 << 10

var errPOP3CommandLineTooLong = errors.New("POP3 command line exceeds the allowed size")

// ErrAuthenticationTemporary 仅用于映射固定的临时失败响应，不回显内部错误。
var ErrAuthenticationTemporary = errors.New("[SYS/TEMP] Authentication temporarily unavailable")

// Server Pop3服务实例
type Server struct {
	Domain           string        // 域名
	Port             int           // 端口
	TlsEnabled       bool          //是否启用Tls
	TlsConfig        *tls.Config   // tls配置
	ConnectAliveTime time.Duration // 连接存活时间，默认不超时
	Action           Action
	stop             chan bool
	close            bool
	lck              sync.Mutex
}

// NewPop3Server 新建一个服务实例
func NewPop3Server(port int, domain string, tlsEnabled bool, tlsConfig *tls.Config, action Action) *Server {
	return &Server{
		Domain:     domain,
		Port:       port,
		TlsEnabled: tlsEnabled,
		TlsConfig:  tlsConfig,
		Action:     action,
		stop:       make(chan bool, 1),
	}
}

// Start 启动服务
func (s *Server) Start() error {
	if !s.TlsEnabled {
		return s.startWithoutTLS()
	} else {
		return s.startWithTLS()
	}
}

func (s *Server) startWithTLS() error {
	if s.lck.TryLock() {
		listener, err := tls.Listen("tcp", fmt.Sprintf(":%d", s.Port), s.TlsConfig)
		if err != nil {
			return err
		}
		s.close = false
		defer func() {
			if err := listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
				slog.Error("pop3 listener close failed", "error", err)
			}
		}()

		go func() {
			for {
				conn, err := listener.Accept()
				if err != nil {
					if s.close {
						break
					} else {
						continue
					}
				}
				go s.handleClient(conn)
			}
		}()
		<-s.stop
	} else {
		return errors.New("Server Is Running")
	}

	return nil
}

func (s *Server) startWithoutTLS() error {
	if s.lck.TryLock() {
		listener, err := net.Listen("tcp", fmt.Sprintf(":%d", s.Port))
		if err != nil {
			return err
		}
		s.close = false
		defer func() {
			if err := listener.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
				slog.Error("pop3 listener close failed", "error", err)
			}
		}()

		go func() {
			for {
				conn, err := listener.Accept()
				if err != nil {
					if s.close {
						break
					} else {
						continue
					}
				}
				go s.handleClient(conn)
			}
		}()
		<-s.stop
	} else {
		return errors.New("Server Is Running")
	}

	return nil
}

// Stop 停止服务
func (s *Server) Stop() {
	s.close = true
	s.stop <- true
}

func (s *Server) handleClient(conn net.Conn) {
	slog.Debug("pop3 conn")

	defer conn.Close()

	session := &Session{
		Conn:      conn,
		AliveTime: time.Now(),
	}
	if s.TlsEnabled && s.TlsConfig != nil {
		session.InTls = true
	}

	// 检查连接是否超时
	if s.ConnectAliveTime != 0 {
		go func() {
			for {
				if time.Since(session.AliveTime) >= s.ConnectAliveTime {
					if err := session.Conn.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
						slog.Debug("pop3 timeout connection close failed", "error", err)
					}
					return
				}
				time.Sleep(3 * time.Second)
			}
		}()
	}

	var (
		eol = "\r\n"
	)
	reader := bufio.NewReaderSize(conn, maxPOP3CommandLineBytes+1)

	fmt.Fprintf(conn, "+OK %s POP3 Server powered by gopop"+eol, s.Domain)

	for {
		rawLine, err := readPOP3CommandLine(reader)
		if err != nil {
			if errors.Is(err, errPOP3CommandLineTooLong) {
				fmt.Fprint(conn, "-ERR command line too long"+eol)
			}
			return
		}
		session.AliveTime = time.Now()

		cmd, args := getCommand(rawLine)
		slog.Debug("pop3 command", "cmd", cmd)
		var cmdError error
		// 撤销后的事务连接立即终止，不执行 QUIT 的延迟删除或任何后续命令。
		if session.Status == TRANSACTION && session.Authorize != nil && session.Authorize() != nil {
			fmt.Fprint(conn, "-ERR authorization revoked"+eol)
			return
		}

		/*
			user<SP>username<CRLF>	      user 命令是POP3客户端程序与POP3邮件服务器建立连接后通常发送的第一条命令，参数 username 表示收件人的帐户名称。
			pass<SP>password<CRLF>	      pass 命令是在user命令成功通过后，POP3客户端程序接着发送的命令，它用于传递帐户的密码，参数 password 表示帐户的密码。
			apop<SP>name,digest<CRLF>	      apop 命令用于替代user和pass命令，它以MD5 数字摘要的形式向POP3邮件服务器提交帐户密码。
			stat<CRLF>	     stat 命令用于查询邮箱中的统计信息，例如：邮箱中的邮件数量和邮件占用的字节大小等。
			uidl<SP>msg#<CRLF>	     uidl 命令用于查询某封邮件的唯一标志符，参数msg#表示邮件的序号，是一个从1开始编号的数字。
			list<SP>[MSG#]<CRLF>	     list 命令用于列出邮箱中的邮件信息，参数 msg#是一个可选参数，表示邮件的序号。当不指定参数时，POP3服务器列出邮箱中所有的邮件信息；当指定参数msg#时，POP3服务器只返回序号对应的邮件信息。
			retr<SP>msg#<CRLF>	    retr 命令用于获取某封邮件的内容，参数 msg#表示邮件的序号。
			dele<SP>msg#<CRLF>	    dele 命令用于在某封邮件上设置删除标记，参数msg#表示邮件的序号。POP3服务器执行dele命令时，只是为邮件设置了删除标记，并没有真正把邮件删除掉，只有POP3客户端发出quit命令后，POP3服务器才会真正删除所有设置了删除标记的邮件。
			rest<CRLF>	    rest 命令用于清除所有邮件的删除标记。
			top<SP>msg#<SP>n<CRLF>	    top 命令用于获取某封邮件的邮件头和邮件体中的前n行内容，参数msg#表示邮件的序号，参数n表示要返回邮件的前几行内容。使用这条命令以提高 Web Mail系统（通过Web站点上收发邮件）中的邮件列表显示的处理效率，因为这种情况下不需要获取每封邮件的完整内容，而是仅仅需要获取每封邮件的邮件头信息。
			noop<CRLF>	    noop 命令用于检测POP3客户端与POP3服务器的连接情况。
			quit<CRLF>	    quit 命令表示要结束邮件接收过程，POP3服务器接收到此命令后，将删除所有设置了删除标记的邮件，并关闭与POP3客户端程序的网络连接。
			capa<CRLF>  capa命令返回服务器支持的命令列表
		*/

		switch cmd {
		case "CAPA":
			commands, err := s.Action.Capa(session)
			if err != nil {
				fmt.Fprintf(conn, "-ERR %s%s", err.Error(), eol)
			} else {
				ret := fmt.Sprintf("+OK Capability list follows%s", eol)

				for _, command := range commands {
					ret += fmt.Sprintf("%s%s", command, eol)
				}
				ret += fmt.Sprintf(".%s", eol)
				fmt.Fprint(conn, ret)
			}
		case "USER":
			userName := getSafeArg(args, 0)
			cmdError = s.Action.User(session, userName)
			if cmdError != nil {
				fmt.Fprintf(conn, "-ERR The user %s doesn't belong here!"+eol, userName)
			} else {
				fmt.Fprint(conn, "+OK"+eol)
			}
		case "PASS":
			password := getSafeArg(args, 0)
			cmdError = s.Action.Pass(session, password)
			if errors.Is(cmdError, ErrAuthenticationTemporary) {
				fmt.Fprint(conn, "-ERR [SYS/TEMP] Authentication temporarily unavailable"+eol)
			} else if cmdError != nil {
				fmt.Fprint(conn, "-ERR Password incorrect!"+eol)
			} else {
				fmt.Fprint(conn, "+OK User signed in"+eol)
			}
		case "STAT":
			if session.Status == TRANSACTION {
				num, size, err := s.Action.Stat(session)
				if err != nil {
					fmt.Fprintf(conn, "-ERR %s%s", err.Error(), eol)
				} else {
					fmt.Fprintf(conn, "+OK %d %d %s", num, size, eol)
				}
			}
		case "LIST":
			if session.Status == TRANSACTION {
				infos, err := s.Action.List(session, getSafeArg(args, 0))
				if err != nil {
					fmt.Fprintf(conn, "-ERR %s %s", err.Error(), eol)
				} else if len(args) > 0 && len(infos) == 1 {
					fmt.Fprintf(conn, "+OK %d %d%s", infos[0].Id, infos[0].Size, eol)
				} else {
					ret := "+OK" + eol
					for _, info := range infos {
						ret += fmt.Sprintf("%d %d%s", info.Id, info.Size, eol)
					}
					ret += "." + eol
					fmt.Fprint(conn, ret)
				}
			}
		case "UIDL":
			if session.Status == TRANSACTION {
				infos, err := s.Action.Uidl(session, getSafeArg(args, 0))
				if err != nil {
					fmt.Fprintf(conn, "-ERR %s %s", err.Error(), eol)
				} else if len(args) > 0 && len(infos) == 1 {
					fmt.Fprintf(conn, "+OK %d %s%s", infos[0].Id, infos[0].UnionId, eol)
				} else {
					ret := fmt.Sprintf("+OK%s", eol)
					for _, info := range infos {
						ret += fmt.Sprintf("%d %s%s", info.Id, info.UnionId, eol)
					}
					ret += "." + eol
					fmt.Fprint(conn, ret)
				}
			}
		case "TOP":
			if session.Status == TRANSACTION {
				id, err1 := strconv.ParseInt(getSafeArg(args, 0), 10, 64)
				line, err2 := strconv.Atoi(getSafeArg(args, 1))
				if err1 != nil || err2 != nil || line < 0 {
					fmt.Fprintf(conn, "-ERR %s %s", "params error", eol)
				} else {
					res, err := s.Action.Top(session, id, line)
					if err != nil {
						fmt.Fprintf(conn, "-ERR %s %s", err.Error(), eol)
					} else {
						ret := fmt.Sprintf("+OK%s", eol)
						ret += fmt.Sprintf("%s%s", res, eol)
						ret += "." + eol
						fmt.Fprint(conn, ret)
					}
				}
			}
		case "RETR":
			if session.Status == TRANSACTION {
				id, err := strconv.ParseInt(getSafeArg(args, 0), 10, 64)
				if err != nil {
					fmt.Fprintf(conn, "-ERR %s %s", "params error", eol)
				} else {
					res, size, err := s.Action.Retr(session, id)
					if err != nil {
						fmt.Fprintf(conn, "-ERR %s %s", err.Error(), eol)
					} else {
						ret := fmt.Sprintf("+OK %d%s", size, eol)
						ret += fmt.Sprintf("%s%s", res, eol)
						ret += "." + eol
						fmt.Fprint(conn, ret)
					}
				}
			}
		case "DELE":
			if session.Status == TRANSACTION {
				id, err := strconv.ParseInt(getSafeArg(args, 0), 10, 64)
				if err != nil {
					fmt.Fprintf(conn, "-ERR %s %s", "params error", eol)
				} else {
					err := s.Action.Delete(session, id)
					if err != nil {
						fmt.Fprintf(conn, "-ERR %s %s", err.Error(), eol)
					} else {
						fmt.Fprintf(conn, "+OK%s", eol)
					}
				}
			}
		case "RSET", "REST":
			if session.Status == TRANSACTION {
				err := s.Action.Rest(session)
				if err != nil {
					fmt.Fprintf(conn, "-ERR %s %s", err.Error(), eol)
				} else {
					fmt.Fprintf(conn, "+OK%s", eol)
				}

			}
		case "QUIT":
			if session.Status == TRANSACTION {
				if err := s.Action.Quit(session); err != nil {
					fmt.Fprintf(conn, "-ERR mailbox update failed %s", eol)
					return
				}
			}
			fmt.Fprintf(conn, "+OK Bye %s", eol)
			return
		case "NOOP":
			fmt.Fprintf(conn, "+OK%s", eol)
		case "STLS":
			if s.TlsConfig == nil || session.InTls || session.Status == TRANSACTION {
				fmt.Fprintf(conn, "-ERR %s %s", "unsupported", eol)
				break
			}
			if _, err := fmt.Fprint(conn, "+OK Begin TLS negotiation"+eol); err != nil {
				return
			}
			tlsConn := tls.Server(conn, s.TlsConfig)
			if err := tlsConn.Handshake(); err != nil {
				slog.Error("STLS handshake failed", "error", err)
				return
			}
			session.Status = UNAUTHORIZED
			session.User = ""
			session.Conn = tlsConn
			session.InTls = true
			conn = tlsConn
			reader = bufio.NewReaderSize(tlsConn, maxPOP3CommandLineBytes+1)
		default:
			rets, err := s.Action.Custom(session, cmd, args)
			if err != nil {
				fmt.Fprintf(conn, "-ERR %s %s", err.Error(), eol)
			} else {
				if len(rets) == 0 {
					fmt.Fprintf(conn, "+OK %s", eol)
				} else if len(rets) == 1 {
					fmt.Fprintf(conn, "+OK %s%s", rets[0], eol)
				} else {
					ret := fmt.Sprintf("+OK %s", eol)
					for _, re := range rets {
						ret += fmt.Sprintf("%s%s", re, eol)
					}
					ret += "." + eol
					fmt.Fprint(conn, ret)
				}
			}
		}

	}
}

func readPOP3CommandLine(reader *bufio.Reader) (string, error) {
	line, err := reader.ReadSlice('\n')
	if errors.Is(err, bufio.ErrBufferFull) || len(line) > maxPOP3CommandLineBytes {
		return "", errPOP3CommandLineTooLong
	}
	if err != nil {
		return "", err
	}
	return string(line), nil
}

// cuts the line into command and arguments
func getCommand(line string) (string, []string) {
	line = strings.Trim(line, "\r \n")
	cmd := strings.Split(line, " ")

	return strings.ToTitle(cmd[0]), cmd[1:]
}

func getSafeArg(args []string, nr int) string {
	if nr < len(args) {
		return args[nr]
	}
	return ""
}
