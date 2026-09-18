// Copyright 2010 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

// Package smtp implements the Simple Mail Transfer Protocol as defined in RFC 5321.
// It also implements the following extensions:
//
//	8BITMIME  RFC 1652
//	AUTH      RFC 2554
//	STARTTLS  RFC 3207
//
// Additional extensions may be handled by clients.
//
// The smtp package is frozen and is not accepting new features.
// Some external packages provide more functionality. See:
//
//	https://godoc.org/?q=smtp
//
// 在go原始SMTP协议的基础上修复了TLS验证错误、支持了SMTPS协议、 支持自定义HELLO命令的域名信息
package smtp

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/utils/credential"
	log "github.com/sirupsen/logrus"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/smtp"
	"net/textproto"
	"net/url"
	"strings"
	"time"
)

var NoSupportSTARTTLSError = errors.New("smtp: server doesn't support STARTTLS")
var EOFError = errors.New("EOF")
var ErrUnsafeRemoteAddress = errors.New("smtp: remote address is not public")
var ErrUnsafePlaintextTarget = errors.New("smtp: plaintext delivery is restricted to localhost")

// DeliveryUncertainError 表示 DATA 已开始，但未取得明确接收或拒绝响应，禁止自动重投。
type DeliveryUncertainError struct {
	Cause error
}

func (e *DeliveryUncertainError) Error() string {
	return "投递结果不确定，请核对收件结果后再操作: " + e.Cause.Error()
}

func (e *DeliveryUncertainError) Unwrap() error { return e.Cause }

func IsDeliveryUncertain(err error) bool {
	var uncertain *DeliveryUncertainError
	return errors.As(err, &uncertain)
}

const (
	outboundSMTPDialTimeout         = 2 * time.Second
	outboundSMTPGreetingTimeout     = 5 * time.Second
	outboundSMTPTLSHandshakeTimeout = 2 * time.Second
	outboundSMTPTransactionTimeout  = 2 * time.Minute
	outboundSMTPQuitWriteTimeout    = 250 * time.Millisecond
	remoteAddressResponseLimit      = 64 * 1024
)

var benchmarkNetworkPrefix = netip.MustParsePrefix("198.18.0.0/15")

// A Client represents a client connection to an SMTP server.
type Client struct {
	// Text is the textproto.Conn used by the Client. It is exported to allow for
	// clients to add extensions.
	Text *textproto.Conn
	// keep a reference to the connection so it can be used to create a TLS
	// connection later
	conn net.Conn
	// whether the Client is using TLS
	tls        bool
	serverName string
	// map of supported extensions
	ext map[string]string
	// supported auth mechanisms
	auth       []string
	localName  string // the name to use in HELO/EHLO
	didHello   bool   // whether we've said HELO/EHLO
	helloError error  // the error from the hello
	ctx        context.Context
	stopCancel func() bool
}

// Dial returns a new Client connected to an SMTP server at addr.
// The addr must include a port, as in "mail.example.com:smtp".
func Dial(addr, fromDomain string) (*Client, error) {
	return dial(addr, fromDomain, true)
}

func dial(addr, fromDomain string, publicOnly bool) (*Client, error) {
	return dialContext(context.Background(), addr, fromDomain, publicOnly)
}

func dialContext(ctx context.Context, addr, fromDomain string, publicOnly bool) (*Client, error) {
	conn, err := dialTCPContext(ctx, addr, publicOnly)
	if err != nil {
		return nil, err
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return newClientWithContext(ctx, conn, host, fromDomain)
}

// with tls
func DialTls(addr, domain, fromDomain string) (*Client, error) {
	return dialTLS(addr, domain, fromDomain, true)
}

func dialTLS(addr, domain, fromDomain string, publicOnly bool) (*Client, error) {
	return dialTLSContext(context.Background(), addr, domain, fromDomain, publicOnly)
}

func dialTLSContext(ctx context.Context, addr, domain, fromDomain string, publicOnly bool) (*Client, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	if domain == "" {
		domain = strings.TrimSuffix(host, ".")
	}

	tlsconfig := &tls.Config{
		MinVersion: tls.VersionTLS12,
		ServerName: domain,
	}

	conn, err := dialTCPContext(ctx, addr, publicOnly)
	if err != nil {
		return nil, err
	}
	stopCancel := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stopCancel()
	tlsConn, err := handshakeTLS(conn, tlsconfig, outboundSMTPTLSHandshakeTimeout)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return newClientWithContext(ctx, tlsConn, host, fromDomain)
}

// newClientWithContext 将欢迎响应、命令和 TLS 升级限制在同一次投递时限内。
func newClientWithContext(ctx context.Context, conn net.Conn, host, fromDomain string) (*Client, error) {
	stopCancel := context.AfterFunc(ctx, func() { _ = conn.Close() })
	if err := conn.SetDeadline(smtpDeadline(ctx, outboundSMTPGreetingTimeout)); err != nil {
		stopCancel()
		_ = conn.Close()
		return nil, err
	}
	client, err := NewClient(conn, host, fromDomain)
	if err != nil {
		stopCancel()
		return nil, err
	}
	client.ctx = ctx
	client.stopCancel = stopCancel
	if err := conn.SetDeadline(smtpDeadline(ctx, outboundSMTPTransactionTimeout)); err != nil {
		_ = client.Close()
		return nil, err
	}
	return client, nil
}

func smtpDeadline(ctx context.Context, timeout time.Duration) time.Time {
	deadline := time.Now().Add(timeout)
	if ctx != nil {
		if limit, ok := ctx.Deadline(); ok && limit.Before(deadline) {
			return limit
		}
	}
	return deadline
}

func handshakeTLS(conn net.Conn, config *tls.Config, timeout time.Duration) (*tls.Conn, error) {
	tlsConn := tls.Client(conn, config)
	if err := tlsConn.SetDeadline(time.Now().Add(timeout)); err != nil {
		return nil, err
	}
	if err := tlsConn.Handshake(); err != nil {
		return nil, err
	}
	return tlsConn, nil
}

func newClientWithTimeout(conn net.Conn, host, fromDomain string, timeout time.Duration) (*Client, error) {
	if err := conn.SetDeadline(time.Now().Add(timeout)); err != nil {
		_ = conn.Close()
		return nil, err
	}
	return NewClient(conn, host, fromDomain)
}

func dialTCP(addr string, publicOnly bool) (net.Conn, error) {
	return dialTCPContext(context.Background(), addr, publicOnly)
}

func dialTCPContext(parent context.Context, addr string, publicOnly bool) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil || host == "" || port == "" {
		return nil, errors.New("smtp: invalid remote address")
	}

	ctx, cancel := context.WithTimeout(parent, outboundSMTPDialTimeout)
	defer cancel()
	addresses, err := resolveRemoteAddresses(ctx, host)
	if err != nil {
		return nil, err
	}
	for _, address := range addresses {
		if publicOnly && !credential.IsPublicRemoteIP(address) {
			return nil, fmt.Errorf("%w: %s", ErrUnsafeRemoteAddress, host)
		}
	}

	dialer := &net.Dialer{Timeout: outboundSMTPDialTimeout, KeepAlive: 30 * time.Second}
	var lastErr error
	for _, address := range addresses {
		conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(address.String(), port))
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		return nil, errors.New("smtp: remote address is unavailable")
	}
	return nil, fmt.Errorf("smtp: connect to remote address: %w", lastErr)
}

func resolveRemoteAddresses(ctx context.Context, host string) ([]netip.Addr, error) {
	if address, err := netip.ParseAddr(host); err == nil {
		return []netip.Addr{address.Unmap()}, nil
	}
	resolved, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, fmt.Errorf("smtp: resolve remote address: %w", err)
	}
	addresses := make([]netip.Addr, 0, len(resolved))
	for _, item := range resolved {
		address, ok := netip.AddrFromSlice(item.IP)
		if ok {
			addresses = append(addresses, address.Unmap())
		}
	}
	if len(addresses) == 0 {
		return nil, errors.New("smtp: remote address is unavailable")
	}
	if containsOnlyBenchmarkAddresses(addresses) {
		addresses, err = resolveRemoteAddressesOverHTTPS(ctx, host)
		if err != nil {
			return nil, fmt.Errorf("smtp: resolve real remote address: %w", err)
		}
	}
	return addresses, nil
}

func containsOnlyBenchmarkAddresses(addresses []netip.Addr) bool {
	if len(addresses) == 0 {
		return false
	}
	for _, address := range addresses {
		if !benchmarkNetworkPrefix.Contains(address.Unmap()) {
			return false
		}
	}
	return true
}

func resolveRemoteAddressesOverHTTPS(ctx context.Context, host string) ([]netip.Addr, error) {
	endpoint := "https://cloudflare-dns.com/dns-query?name=" + url.QueryEscape(host) + "&type=A"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/dns-json")

	client := &http.Client{
		Timeout: outboundSMTPDialTimeout,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected DNS response status: %d", response.StatusCode)
	}

	var payload struct {
		Status int `json:"Status"`
		Answer []struct {
			Type int    `json:"type"`
			Data string `json:"data"`
		} `json:"Answer"`
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, remoteAddressResponseLimit))
	if err = decoder.Decode(&payload); err != nil {
		return nil, err
	}
	if payload.Status != 0 {
		return nil, fmt.Errorf("DNS response code: %d", payload.Status)
	}

	addresses := make([]netip.Addr, 0, len(payload.Answer))
	for _, answer := range payload.Answer {
		if answer.Type != 1 {
			continue
		}
		address, parseErr := netip.ParseAddr(strings.TrimSpace(answer.Data))
		if parseErr == nil {
			addresses = append(addresses, address.Unmap())
		}
	}
	if len(addresses) == 0 {
		return nil, errors.New("remote address is unavailable")
	}
	return addresses, nil
}

func validateUnsafePlaintextTarget(domain, addr string) error {
	if strings.TrimSuffix(strings.ToLower(domain), ".") != "localhost" {
		return ErrUnsafePlaintextTarget
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return ErrUnsafePlaintextTarget
	}
	address, err := netip.ParseAddr(host)
	if err != nil || !address.Unmap().IsLoopback() {
		return ErrUnsafePlaintextTarget
	}
	return nil
}

// NewClient returns a new Client using an existing connection and host as a
// server name to be used when authenticating.
func NewClient(conn net.Conn, host, fromDomain string) (*Client, error) {
	text := textproto.NewConn(conn)
	_, _, err := text.ReadResponse(220)
	if err != nil {
		closeErr := text.Close()
		return nil, errors.Join(err, closeErr)
	}

	localName := "domain.com"

	if fromDomain != "" {
		localName = fromDomain
	} else if domains := config.CurrentMailDomains(); domains.Primary != "" {
		localName = domains.Primary
	}

	c := &Client{Text: text, conn: conn, serverName: host, localName: localName}
	_, c.tls = conn.(*tls.Conn)
	return c, nil
}

// Close closes the connection.
func (c *Client) Close() error {
	if c.stopCancel != nil {
		c.stopCancel()
	}
	return c.Text.Close()
}

// hello runs a hello exchange if needed.
func (c *Client) hello() error {
	if !c.didHello {
		c.didHello = true
		err := c.ehlo()
		if err != nil {
			c.helloError = c.helo()
		}
	}
	return c.helloError
}

// Hello sends a HELO or EHLO to the server as the given host name.
// Calling this method is only necessary if the client needs control
// over the host name used. The client will introduce itself as "localhost"
// automatically otherwise. If Hello is called, it must be called before
// any of the other methods.
func (c *Client) Hello(localName string) error {
	if err := validateLine(localName); err != nil {
		return err
	}
	if c.didHello {
		return errors.New("smtp: Hello called after other methods")
	}
	c.localName = localName
	return c.hello()
}

// cmd is a convenience function that sends a command and returns the response
func (c *Client) cmd(expectCode int, format string, args ...any) (int, string, error) {
	id, err := c.Text.Cmd(format, args...)
	if err != nil {
		return 0, "", err
	}
	c.Text.StartResponse(id)
	defer c.Text.EndResponse(id)
	code, msg, err := c.Text.ReadResponse(expectCode)
	return code, msg, err
}

// helo sends the HELO greeting to the server. It should be used only when the
// server does not support ehlo.
func (c *Client) helo() error {
	c.ext = nil
	_, _, err := c.cmd(250, "HELO %s", c.localName)
	return err
}

// ehlo sends the EHLO (extended hello) greeting to the server. It
// should be the preferred greeting for servers that support it.
func (c *Client) ehlo() error {
	_, msg, err := c.cmd(250, "EHLO %s", c.localName)
	if err != nil {
		return err
	}
	ext := make(map[string]string)
	extList := strings.Split(msg, "\n")
	if len(extList) > 1 {
		extList = extList[1:]
		for _, line := range extList {
			k, v, _ := strings.Cut(line, " ")
			ext[k] = v
		}
	}
	if mechs, ok := ext["AUTH"]; ok {
		c.auth = strings.Split(mechs, " ")
	}
	c.ext = ext
	return err
}

// StartTLS sends the STARTTLS command and encrypts all further communication.
// Only servers that advertise the STARTTLS extension support this function.
func (c *Client) StartTLS(config *tls.Config) error {
	if err := c.hello(); err != nil {
		return err
	}
	_, _, err := c.cmd(220, "STARTTLS")
	if err != nil {
		return err
	}
	if config == nil {
		config = &tls.Config{MinVersion: tls.VersionTLS12}
	}
	if config.ServerName == "" {
		// Make a copy to avoid polluting argument
		config = config.Clone()
		config.ServerName = c.serverName
	}
	if config.MinVersion < tls.VersionTLS12 {
		config = config.Clone()
		config.MinVersion = tls.VersionTLS12
	}
	tlsConn, err := handshakeTLS(c.conn, config, outboundSMTPTLSHandshakeTimeout)
	if err != nil {
		return err
	}
	if err = tlsConn.SetDeadline(smtpDeadline(c.ctx, outboundSMTPTransactionTimeout)); err != nil {
		return err
	}
	c.conn = tlsConn
	c.Text = textproto.NewConn(c.conn)
	c.tls = true
	return c.ehlo()
}

// TLSConnectionState returns the client's TLS connection state.
// The return values are their zero values if StartTLS did
// not succeed.
func (c *Client) TLSConnectionState() (state tls.ConnectionState, ok bool) {
	tc, ok := c.conn.(*tls.Conn)
	if !ok {
		return
	}
	return tc.ConnectionState(), true
}

// Verify checks the validity of an email address on the server.
// If Verify returns nil, the address is valid. A non-nil return
// does not necessarily indicate an invalid address. Many servers
// will not verify addresses for security reasons.
func (c *Client) Verify(addr string) error {
	if err := validateLine(addr); err != nil {
		return err
	}
	if err := c.hello(); err != nil {
		return err
	}
	_, _, err := c.cmd(250, "VRFY %s", addr)
	return err
}

// Auth authenticates a client using the provided authentication mechanism.
// A failed authentication closes the connection.
// Only servers that advertise the AUTH extension support this function.
func (c *Client) Auth(a smtp.Auth) error {
	if err := c.hello(); err != nil {
		return err
	}
	encoding := base64.StdEncoding
	mech, resp, err := a.Start(&smtp.ServerInfo{Name: c.serverName, TLS: c.tls, Auth: c.auth})
	if err != nil {
		quitErr := c.Quit()
		return errors.Join(err, quitErr)
	}
	resp64 := make([]byte, encoding.EncodedLen(len(resp)))
	encoding.Encode(resp64, resp)
	code, msg64, err := c.cmd(0, "AUTH %s %s", mech, resp64)
	for err == nil {
		var msg []byte
		switch code {
		case 334:
			msg, err = encoding.DecodeString(msg64)
		case 235:
			// the last message isn't base64 because it isn't a challenge
			msg = []byte(msg64)
		default:
			err = &textproto.Error{Code: code, Msg: msg64}
		}
		if err == nil {
			resp, err = a.Next(msg, code == 334)
		}
		if err != nil {
			// abort the AUTH
			_, _, abortErr := c.cmd(501, "*")
			err = errors.Join(err, abortErr, c.Quit())
			break
		}
		if resp == nil {
			break
		}
		resp64 = make([]byte, encoding.EncodedLen(len(resp)))
		encoding.Encode(resp64, resp)
		code, msg64, err = c.cmd(0, "%s", string(resp64))
	}
	return err
}

// Mail issues a MAIL command to the server using the provided email address.
// If the server supports the 8BITMIME extension, Mail adds the BODY=8BITMIME
// parameter. If the server supports the SMTPUTF8 extension, Mail adds the
// SMTPUTF8 parameter.
// This initiates a mail transaction and is followed by one or more Rcpt calls.
func (c *Client) Mail(from string) error {
	if err := validateLine(from); err != nil {
		return err
	}
	if err := c.hello(); err != nil {
		return err
	}
	cmdStr := "MAIL FROM:<%s>"
	if c.ext != nil {
		if _, ok := c.ext["8BITMIME"]; ok {
			cmdStr += " BODY=8BITMIME"
		}
		if _, ok := c.ext["SMTPUTF8"]; ok {
			cmdStr += " SMTPUTF8"
		}
	}
	_, _, err := c.cmd(250, cmdStr, from)
	return err
}

// Rcpt issues a RCPT command to the server using the provided email address.
// A call to Rcpt must be preceded by a call to Mail and may be followed by
// a Data call or another Rcpt call.
func (c *Client) Rcpt(to string) error {
	if err := validateLine(to); err != nil {
		return err
	}
	_, _, err := c.cmd(25, "RCPT TO:<%s>", to)
	return err
}

type dataCloser struct {
	c *Client
	io.WriteCloser
}

func (d *dataCloser) Close() error {
	if err := d.WriteCloser.Close(); err != nil {
		return err
	}
	_, _, err := d.c.Text.ReadResponse(250)
	return err
}

// Data issues a DATA command to the server and returns a writer that
// can be used to write the mail headers and body. The caller should
// close the writer before calling any more methods on c. A call to
// Data must be preceded by one or more calls to Rcpt.
func (c *Client) Data() (io.WriteCloser, error) {
	_, _, err := c.cmd(354, "DATA")
	if err != nil {
		return nil, err
	}
	return &dataCloser{c, c.Text.DotWriter()}, nil
}

func finishDelivery(c *Client, w io.WriteCloser, msg []byte) error {
	if _, err := w.Write(msg); err != nil {
		return &DeliveryUncertainError{Cause: err}
	}
	if err := w.Close(); err != nil {
		var rejection *textproto.Error
		if errors.As(err, &rejection) && rejection.Code >= 400 && rejection.Code <= 599 {
			return err
		}
		return &DeliveryUncertainError{Cause: err}
	}

	// The final 250 response to DATA transfers responsibility for the message.
	// Send QUIT with a bounded write, but do not wait for 221: a failed or
	// missing reply after acceptance must not block or trigger redelivery.
	if err := c.conn.SetWriteDeadline(time.Now().Add(outboundSMTPQuitWriteTimeout)); err != nil {
		log.Debugf("Could not bound SMTP QUIT write after message acceptance: %v", err)
	}
	if err := c.Text.PrintfLine("QUIT"); err != nil {
		log.Debugf("SMTP QUIT failed after message acceptance: %v", err)
	}
	return nil
}

func SendMailWithTls(domain string, addr string, a smtp.Auth, from string, fromDomain string, to []string, msg []byte) error {
	return SendMailWithTLSContext(context.Background(), domain, addr, a, from, fromDomain, to, msg)
}

func SendMailWithTLSContext(ctx context.Context, domain string, addr string, a smtp.Auth, from string, fromDomain string, to []string, msg []byte) error {
	if err := validateLine(from); err != nil {
		return err
	}
	for _, recp := range to {
		if err := validateLine(recp); err != nil {
			return err
		}
	}
	c, err := dialTLSContext(ctx, addr, domain, fromDomain, true)
	if err != nil {
		return err
	}
	defer c.Close()
	if err = c.hello(); err != nil {
		return err
	}
	if a != nil && c.ext != nil {
		if _, ok := c.ext["AUTH"]; !ok {
			return errors.New("smtp: server doesn't support AUTH")
		}
		if err = c.Auth(a); err != nil {
			return err
		}
	}
	if err = c.Mail(from); err != nil {
		return err
	}
	for _, addr := range to {
		if err = c.Rcpt(addr); err != nil {
			return err
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	return finishDelivery(c, w, msg)
}

// SendMail connects to the server at addr, switches to TLS if
// possible, authenticates with the optional mechanism a if possible,
// and then sends an email from address from, to addresses to, with
// message msg.
// The addr must include a port, as in "mail.example.com:smtp".
//
// The addresses in the to parameter are the SMTP RCPT addresses.
//
// The msg parameter should be an RFC 822-style email with headers
// first, a blank line, and then the message body. The lines of msg
// should be CRLF terminated. The msg headers should usually include
// fields such as "From", "To", "Subject", and "Cc".  Sending "Bcc"
// messages is accomplished by including an email address in the to
// parameter but not including it in the msg headers.
//
// The SendMail function and the net/smtp package are low-level
// mechanisms and provide no support for DKIM signing, MIME
// attachments (see the mime/multipart package), or other mail
// functionality. Higher-level packages exist outside of the standard
// library.
// 修复TSL验证问题
func SendMail(domain string, addr string, a smtp.Auth, from string, fromDomain string, to []string, msg []byte) error {
	return SendMailContext(context.Background(), domain, addr, a, from, fromDomain, to, msg)
}

func SendMailContext(ctx context.Context, domain string, addr string, a smtp.Auth, from string, fromDomain string, to []string, msg []byte) error {

	log.Debugf("SMTP delivery started: recipient_count=%d", len(to))

	if err := validateLine(from); err != nil {
		return err
	}
	for _, recp := range to {
		if err := validateLine(recp); err != nil {
			return err
		}
	}
	c, err := dialContext(ctx, addr, fromDomain, true)
	if err != nil {
		return err
	}
	defer c.Close()
	if err = c.hello(); err != nil {
		return err
	}
	if ok, _ := c.Extension("STARTTLS"); !ok {
		return NoSupportSTARTTLSError
	}

	var config *tls.Config
	if domain != "" {
		config = &tls.Config{
			ServerName: domain,
			MinVersion: tls.VersionTLS12,
		}
	}

	if err = c.StartTLS(config); err != nil {
		return err
	}
	if a != nil && c.ext != nil {
		if _, ok := c.ext["AUTH"]; !ok {
			return errors.New("smtp: server doesn't support AUTH")
		}
		if err = c.Auth(a); err != nil {
			return err
		}
	}
	if err = c.Mail(from); err != nil {
		return err
	}
	for _, addr := range to {
		if err = c.Rcpt(addr); err != nil {
			return err
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	return finishDelivery(c, w, msg)
}

// SendMailUnsafe 无TLS加密的邮件发送方式
func SendMailUnsafe(domain string, addr string, a smtp.Auth, from string, fromDomain string, to []string, msg []byte) error {
	return SendMailUnsafeContext(context.Background(), domain, addr, a, from, fromDomain, to, msg)
}

func SendMailUnsafeContext(ctx context.Context, domain string, addr string, a smtp.Auth, from string, fromDomain string, to []string, msg []byte) error {
	if err := validateUnsafePlaintextTarget(domain, addr); err != nil {
		return err
	}
	if err := validateLine(from); err != nil {
		return err
	}
	for _, recp := range to {
		if err := validateLine(recp); err != nil {
			return err
		}
	}
	c, err := dialContext(ctx, addr, fromDomain, false)
	if err != nil {
		return err
	}
	defer c.Close()
	if err = c.hello(); err != nil {
		return err
	}

	if a != nil && c.ext != nil {
		if _, ok := c.ext["AUTH"]; !ok {
			return errors.New("smtp: server doesn't support AUTH")
		}
		if err = c.Auth(a); err != nil {
			return err
		}
	}
	if err = c.Mail(from); err != nil {
		return err
	}
	for _, addr := range to {
		if err = c.Rcpt(addr); err != nil {
			return err
		}
	}
	w, err := c.Data()
	if err != nil {
		return err
	}
	return finishDelivery(c, w, msg)
}

// Extension reports whether an extension is support by the server.
// The extension name is case-insensitive. If the extension is supported,
// Extension also returns a string that contains any parameters the
// server specifies for the extension.
func (c *Client) Extension(ext string) (bool, string) {
	if err := c.hello(); err != nil {
		return false, ""
	}
	if c.ext == nil {
		return false, ""
	}
	ext = strings.ToUpper(ext)
	param, ok := c.ext[ext]
	return ok, param
}

// Reset sends the RSET command to the server, aborting the current mail
// transaction.
func (c *Client) Reset() error {
	if err := c.hello(); err != nil {
		return err
	}
	_, _, err := c.cmd(250, "RSET")
	return err
}

// Noop sends the NOOP command to the server. It does nothing but check
// that the connection to the server is okay.
func (c *Client) Noop() error {
	if err := c.hello(); err != nil {
		return err
	}
	_, _, err := c.cmd(250, "NOOP")
	return err
}

// Quit sends the QUIT command and closes the connection to the server.
func (c *Client) Quit() error {
	if err := c.hello(); err != nil {
		return err
	}
	_, _, err := c.cmd(221, "QUIT")
	if err != nil {
		return err
	}
	return c.Text.Close()
}

// validateLine checks to see if a line has CR or LF as per RFC 5321.
func validateLine(line string) error {
	if strings.ContainsAny(line, "\n\r") {
		return errors.New("smtp: A line must not contain CR or LF")
	}
	return nil
}
