package parsemail

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/textproto"
	"regexp"
	"strings"
	"time"

	"git.pcux.cn/xuans/Smail/config"
	"git.pcux.cn/xuans/Smail/models"
	"git.pcux.cn/xuans/Smail/utils/context"
	"github.com/emersion/go-message"
	_ "github.com/emersion/go-message/charset"
	"github.com/emersion/go-message/mail"
	"github.com/microcosm-cc/bluemonday"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cast"
	xhtml "golang.org/x/net/html"
	xhtmlatom "golang.org/x/net/html/atom"
)

type User struct {
	EmailAddress string `json:"EmailAddress"`
	Name         string `json:"Name"`
}

func (u User) Build() string {
	if u.Name != "" {
		return fmt.Sprintf("\"%s\" <%s>", mime.QEncoding.Encode("utf-8", u.Name), u.EmailAddress)
	}
	return fmt.Sprintf("<%s>", u.EmailAddress)
}

func (u User) GetDomainAccount() (string, string) {
	infos := strings.Split(u.EmailAddress, "@")
	if len(infos) >= 2 {
		return infos[0], infos[1]
	}

	return "", ""
}

type Attachment struct {
	Filename    string
	ContentType string
	Content     []byte
	ContentID   string
}

// EmailAuthentication 表示收信时的发件人身份认证结果。
type EmailAuthentication struct {
	SPFPassed  bool
	DKIMPassed bool
	Dangerous  bool
}

// NewEmailAuthentication 根据 SPF 和 DKIM 结果创建统一的邮件认证结论。
func NewEmailAuthentication(spfPassed, dkimPassed bool) *EmailAuthentication {
	return &EmailAuthentication{
		SPFPassed:  spfPassed,
		DKIMPassed: dkimPassed,
		Dangerous:  !spfPassed && !dkimPassed,
	}
}

// Email is the type used for email messages
type Email struct {
	InReplyTo        string
	ThreadReferences string
	EnvelopeFrom     string
	ReplyTo          []*User
	From             *User
	To               []*User
	Bcc              []*User
	Cc               []*User
	Subject          string
	Text             []byte // Plaintext message (optional)
	HTML             []byte // Html message (optional)
	Sender           *User  // override From as SMTP envelope sender (optional)
	Headers          textproto.MIMEHeader
	Attachments      []*Attachment
	ReadReceipt      []string
	Date             string
	Status           int // 0未发送，1已发送，2发送失败，3删除，5广告邮件
	MessageId        int64
	MsgID            string // RFC-compliant Message-ID, persisted in DB
	Size             int
	Authentication   *EmailAuthentication
}

func (e *Email) SenderFilterAddresses() []string {
	if e == nil {
		return nil
	}
	addresses := []string{e.EnvelopeFrom}
	if e.From != nil {
		addresses = append(addresses, e.From.EmailAddress)
	}
	if e.Sender != nil {
		addresses = append(addresses, e.Sender.EmailAddress)
	}
	return addresses
}

type StoredEmailFields struct {
	ReplyTo     string
	To          string
	Bcc         string
	Cc          string
	Sender      string
	Attachments string
}

func (e *Email) EncodeStoredFields() (StoredEmailFields, error) {
	if e == nil {
		return StoredEmailFields{}, errors.New("email is nil")
	}
	marshal := func(field string, value any) (string, error) {
		data, err := json.Marshal(value)
		if err != nil {
			return "", fmt.Errorf("encode %s: %w", field, err)
		}
		return string(data), nil
	}

	var fields StoredEmailFields
	var err error
	if fields.ReplyTo, err = marshal("reply-to", e.ReplyTo); err != nil {
		return StoredEmailFields{}, err
	}
	if fields.To, err = marshal("recipients", e.To); err != nil {
		return StoredEmailFields{}, err
	}
	if fields.Bcc, err = marshal("bcc", e.Bcc); err != nil {
		return StoredEmailFields{}, err
	}
	if fields.Cc, err = marshal("cc", e.Cc); err != nil {
		return StoredEmailFields{}, err
	}
	if fields.Sender, err = marshal("sender", e.Sender); err != nil {
		return StoredEmailFields{}, err
	}
	if fields.Attachments, err = marshal("attachments", e.Attachments); err != nil {
		return StoredEmailFields{}, err
	}
	return fields, nil
}

// GenerateMsgID creates an RFC-compliant Message-ID unique enough to avoid spam filters.
// It must be called once at email creation time and the result persisted to DB.
func GenerateMsgID(domain string) (string, error) {
	domain = strings.TrimSpace(domain)
	if domain == "" || len(domain) > 253 || strings.ContainsAny(domain, "\r\n\t <>@") {
		return "", errors.New("invalid message id domain")
	}
	randBytes := make([]byte, 16)
	if _, err := io.ReadFull(rand.Reader, randBytes); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x.%d@%s", randBytes, time.Now().UnixNano(), domain), nil
}

// Xss filter policy
var (
	strictPolicy  *bluemonday.Policy
	relaxedPolicy *bluemonday.Policy
)

func init() {
	strictPolicy = bluemonday.StrictPolicy()

	relaxedPolicy = bluemonday.NewPolicy()

	relaxedPolicy.AllowElements("p", "br", "strong", "em", "u", "b", "i", "h1", "h2", "h3", "h4", "h5", "h6")
	relaxedPolicy.AllowElements("div", "span", "center")
	relaxedPolicy.AllowElements("ul", "ol", "li")
	relaxedPolicy.AllowElements("blockquote", "cite")

	relaxedPolicy.AllowElements("table", "tbody", "thead", "tr", "td", "th")
	relaxedPolicy.AllowAttrs("width", "height", "border", "cellpadding", "cellspacing").OnElements("table")
	relaxedPolicy.AllowAttrs("align", "valign", "colspan", "rowspan").OnElements("td", "th")
	relaxedPolicy.AllowAttrs("align").OnElements("tr")

	relaxedPolicy.AllowAttrs("class", "id").Globally()

	relaxedPolicy.AllowAttrs("bgcolor", "color", "background").Globally()
	relaxedPolicy.AllowAttrs("align").OnElements("p", "div", "h1", "h2", "h3", "h4", "h5", "h6")

	relaxedPolicy.AllowElements("img")
	relaxedPolicy.AllowAttrs("src", "alt", "width", "height", "align").OnElements("img")

	relaxedPolicy.AllowElements("a")
	relaxedPolicy.AllowAttrs("href").OnElements("a")
	relaxedPolicy.RequireNoReferrerOnLinks(true)
	relaxedPolicy.AddTargetBlankToFullyQualifiedLinks(true)
	relaxedPolicy.RequireNoFollowOnLinks(true)

	relaxedPolicy.AllowElements("font")
	relaxedPolicy.AllowAttrs("size", "color", "face").OnElements("font")

	relaxedPolicy.AllowURLSchemes("http", "https", "mailto", "cid")
	relaxedPolicy.AllowRelativeURLs(true)

	relaxedPolicy.SkipElementsContent("script", "object", "embed", "iframe", "frame", "frameset")
}

func SanitizeHTML(htmlContent string) string {
	if htmlContent == "" {
		return ""
	}

	sanitized := relaxedPolicy.Sanitize(htmlContent)
	sanitized = sanitizeHTMLFragment(sanitized)

	dataUrlRegex := regexp.MustCompile(`href\s*=\s*["']data:[^"']*["']`)
	sanitized = dataUrlRegex.ReplaceAllString(sanitized, `rel="nofollow"`)

	jsUrlRegex := regexp.MustCompile(`href\s*=\s*["']javascript:[^"']*["']`)
	sanitized = jsUrlRegex.ReplaceAllString(sanitized, `rel="nofollow"`)

	expressionRegex := regexp.MustCompile(`(?i)expression\s*\(.*?\)`)
	sanitized = expressionRegex.ReplaceAllString(sanitized, "")

	styleExpressionRegex := regexp.MustCompile(`(?i)style\s*=\s*["'][^"']*expression[^"']*["']`)
	sanitized = styleExpressionRegex.ReplaceAllString(sanitized, "")

	cssJsRegex := regexp.MustCompile(`(?i)javascript\s*:`)
	sanitized = cssJsRegex.ReplaceAllString(sanitized, "")

	return sanitized
}

func sanitizeHTMLFragment(content string) string {
	container := &xhtml.Node{Type: xhtml.ElementNode, Data: "div", DataAtom: xhtmlatom.Div}
	nodes, err := xhtml.ParseFragment(strings.NewReader(content), container)
	if err != nil {
		return ""
	}
	var builder strings.Builder
	for _, node := range nodes {
		if sanitizeHTMLNode(node) {
			if err := xhtml.Render(&builder, node); err != nil {
				return ""
			}
		}
	}
	return builder.String()
}

func sanitizeHTMLNode(node *xhtml.Node) bool {
	if node.Type == xhtml.ElementNode && node.Data == "style" {
		return false
	}
	if node.Type == xhtml.ElementNode {
		filtered := node.Attr[:0]
		for _, attr := range node.Attr {
			if strings.EqualFold(attr.Key, "style") {
				continue
			}
			if node.Data == "img" && strings.EqualFold(attr.Key, "src") {
				value := strings.TrimSpace(strings.ToLower(attr.Val))
				if strings.HasPrefix(value, "http:") || strings.HasPrefix(value, "https:") || strings.HasPrefix(value, "//") {
					continue
				}
			}
			filtered = append(filtered, attr)
		}
		node.Attr = filtered
	}
	for child := node.FirstChild; child != nil; {
		next := child.NextSibling
		if !sanitizeHTMLNode(child) {
			node.RemoveChild(child)
		}
		child = next
	}
	return true
}

// Sanitize Text
func SanitizeText(text string) string {
	return strictPolicy.Sanitize(text)
}

func users2String(users []*User) string {
	ret := ""
	for _, user := range users {
		if ret != "" {
			ret += ", "
		}
		ret += user.Build()
	}
	return ret
}

func (e *Email) BuildTo2String() string {
	return users2String(e.To)
}

func (e *Email) BuildCc2String() string {
	return users2String(e.Cc)
}

func (e *Email) BuildBcc2String() string {
	return users2String(e.Bcc)
}

func NewEmailFromModel(d models.Email) (*Email, error) {

	var to []*User
	if err := decodeModelField(d.To, &to, "recipients"); err != nil {
		return nil, err
	}

	var replyTo []*User
	if err := decodeModelField(d.ReplyTo, &replyTo, "reply-to"); err != nil {
		return nil, err
	}

	var sender *User
	if err := decodeModelField(d.Sender, &sender, "sender"); err != nil {
		return nil, err
	}

	var bcc []*User
	if err := decodeModelField(d.Bcc, &bcc, "bcc"); err != nil {
		return nil, err
	}

	var cc []*User
	if err := decodeModelField(d.Cc, &cc, "cc"); err != nil {
		return nil, err
	}

	var attachments []*Attachment
	if err := decodeModelField(d.Attachments, &attachments, "attachments"); err != nil {
		return nil, err
	}

	ret := &Email{
		MessageId:        cast.ToInt64(d.Id),
		InReplyTo:        NormalizeThreadHeader(d.InReplyTo),
		ThreadReferences: NormalizeThreadHeader(d.ThreadReferences),
		MsgID:            d.MsgID,
		From: &User{
			Name:         d.FromName,
			EmailAddress: d.FromAddress,
		},
		To:          to,
		Subject:     d.Subject,
		Text:        []byte(d.Text.String),
		HTML:        []byte(d.Html.String),
		Sender:      sender,
		ReplyTo:     replyTo,
		Bcc:         bcc,
		Cc:          cc,
		Attachments: attachments,
		Date:        d.SendDate.Format(time.RFC3339),
	}
	if d.Type == 0 {
		ret.Authentication = NewEmailAuthentication(d.SPFCheck == 1, d.DKIMCheck == 1)
	}
	return ret, nil
}

func decodeModelField(raw string, target any, field string) error {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	if err := json.Unmarshal([]byte(raw), target); err != nil {
		return fmt.Errorf("decode stored %s: %w", field, err)
	}
	return nil
}
func NewEmailFromReader(to []string, r io.Reader, size int) (*Email, error) {
	ret := &Email{}
	m, err := message.Read(r)
	if err != nil {
		log.Errorf("email解析错误！ Error %+v", err)
	}
	if err != nil {
		return nil, fmt.Errorf("parse message: %w", err)
	}

	ret.Size = size
	ret.InReplyTo = NormalizeThreadHeader(m.Header.Get("In-Reply-To"))
	ret.ThreadReferences = NormalizeThreadHeader(m.Header.Get("References"))
	// Preserve the original Message-ID from the sender so it is stored and reused consistently.
	if mid := m.Header.Get("Message-Id"); mid != "" {
		ret.MsgID = strings.TrimPrefix(strings.TrimSuffix(strings.TrimSpace(mid), ">"), "<")
	}
	ret.From = buildUser(m.Header.Get("From"))

	smtpTo := buildUsers(to)

	ret.To = buildUsers(m.Header.Values("To"))
	ret.Cc = buildUsers(m.Header.Values("Cc"))

	ret.Bcc = []*User{}

	for _, user := range smtpTo {
		in := false
		for _, u := range ret.To {
			if strings.EqualFold(u.EmailAddress, user.EmailAddress) {
				in = true
				break
			}
		}
		for _, u := range ret.Cc {
			if strings.EqualFold(u.EmailAddress, user.EmailAddress) {
				in = true
				break
			}
		}
		if !in {
			ret.Bcc = append(ret.Bcc, user)
		}

	}

	ret.ReplyTo = buildUsers(m.Header.Values("Reply-To"))
	ret.Sender = buildUser(m.Header.Get("Sender"))
	if ret.Sender == nil || strings.TrimSpace(ret.Sender.EmailAddress) == "" {
		ret.Sender = ret.From
	}

	subject, err := m.Header.Text("Subject")
	if err != nil {
		return nil, fmt.Errorf("decode message subject: %w", err)
	}
	ret.Subject = strictPolicy.Sanitize(subject)

	sendTime, err := time.Parse(time.RFC1123Z, m.Header.Get("Date"))
	if err != nil {
		sendTime = time.Now()
	}
	ret.Date = sendTime.Format(time.RFC3339)
	if err = m.Walk(func(path []int, entity *message.Entity, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entity == nil {
			return fmt.Errorf("message part is missing")
		}
		return formatContent(entity, ret)
	}); err != nil {
		return nil, fmt.Errorf("read message content: %w", err)
	}

	if ret.From != nil {
		ret.From.Name = strictPolicy.Sanitize(ret.From.Name)
		ret.From.EmailAddress = strictPolicy.Sanitize(ret.From.EmailAddress)
	}

	return ret, nil
}

func formatContent(entity *message.Entity, ret *Email) error {
	if entity == nil {
		return fmt.Errorf("message part is missing")
	}
	contentType, p, err := entity.Header.ContentType()

	if err != nil {
		log.Errorf("email read error! %+v", err)
		return err
	}
	disp := strings.ToLower(entity.Header.Get("Content-Disposition"))
	isAttachment := strings.Contains(disp, "attachment")
	switch contentType {
	case "multipart/alternative":
	case "multipart/mixed":
	case "text/plain":
		body, err := io.ReadAll(entity.Body)
		if err != nil {
			return err
		}

		if isAttachment {
			fileName := getFileName(entity, p)

			ret.Attachments = append(ret.Attachments, &Attachment{
				Filename:    SanitizeText(fileName),
				ContentType: SanitizeText(strings.TrimSpace(contentType)),
				Content:     body,
				ContentID:   strings.TrimPrefix(strings.TrimSuffix(entity.Header.Get("Content-Id"), ">"), "<"),
			})

			return nil
		}

		ret.Text = []byte(strictPolicy.Sanitize(string(body)))
	case "text/html":
		body, err := io.ReadAll(entity.Body)
		if err != nil {
			return err
		}

		if isAttachment {
			fileName := getFileName(entity, p)

			ret.Attachments = append(ret.Attachments, &Attachment{
				Filename:    SanitizeText(fileName),
				ContentType: SanitizeText(strings.TrimSpace(contentType)),
				Content:     body,
				ContentID:   strings.TrimPrefix(strings.TrimSuffix(entity.Header.Get("Content-Id"), ">"), "<"),
			})

			return nil
		}

		ret.HTML = []byte(SanitizeHTML(string(body)))
	case "multipart/related":
	default:
		c, err := io.ReadAll(entity.Body)
		if err != nil {
			return err
		}
		fileName := getFileName(entity, p)

		ret.Attachments = append(ret.Attachments, &Attachment{
			Filename:    SanitizeText(fileName),
			ContentType: SanitizeText(strings.TrimSpace(contentType)),
			Content:     c,
			ContentID:   strings.TrimPrefix(strings.TrimSuffix(entity.Header.Get("Content-Id"), ">"), "<"),
		})
	}

	return nil
}
func getFileName(entity *message.Entity, p map[string]string) string {
	fileName := p["name"]

	if fileName != "" {
		return fileName
	}

	_, params, err := entity.Header.ContentDisposition()
	if err == nil && params["filename"] != "" {
		return params["filename"]
	}

	return "no_name_file"
}

func BuilderUser(str string) *User {
	return buildUser(str)
}

var emailAddressRe = regexp.MustCompile(`<(.*@.*)>`)

func buildUser(str string) *User {
	str = strings.TrimSpace(str)
	if str == "" {
		return &User{}
	}

	user := &User{}

	addr, err := mail.ParseAddress(str)
	if err == nil {
		user.EmailAddress = strings.TrimSpace(addr.Address)

		name := strings.TrimSpace(addr.Name)
		if name != "" {
			decoder := mime.WordDecoder{}
			if decoded, err := decoder.Decode(name); err == nil {
				name = decoded
			}
			user.Name = strictPolicy.Sanitize(name)
		}
		return user
	}

	matched := emailAddressRe.FindStringSubmatch(str)
	if len(matched) == 2 {
		user.EmailAddress = strings.TrimSpace(matched[1])
		namePart := strings.ReplaceAll(str, matched[0], "")
		namePart = strings.Trim(strings.TrimSpace(namePart), "\"")

		decoder := mime.WordDecoder{}
		if decoded, err := decoder.Decode(strings.ReplaceAll(namePart, "\"", "")); err == nil {
			user.Name = strictPolicy.Sanitize(strings.TrimSpace(decoded))
		} else {
			user.Name = strictPolicy.Sanitize(strings.TrimSpace(namePart))
		}
	} else {
		user.EmailAddress = strictPolicy.Sanitize(str)
	}

	return user
}

func buildUsers(strs []string) []*User {
	var ret []*User
	for _, line := range strs {
		if strings.TrimSpace(line) == "" {
			continue
		}
		addresses, err := mail.ParseAddressList(line)
		if err == nil {
			for _, address := range addresses {
				ret = append(ret, buildUser(address.String()))
			}
			continue
		}
		if user := buildUser(line); user != nil {
			ret = append(ret, user)
		}
	}
	return ret
}

func writeMessagePart(w io.WriteCloser, content []byte) error {
	n, writeErr := w.Write(content)
	closeErr := w.Close()
	if writeErr != nil {
		return errors.Join(writeErr, closeErr)
	}
	if n != len(content) {
		return errors.Join(io.ErrShortWrite, closeErr)
	}
	return closeErr
}

func closeMessageWriters(primary error, closers ...io.Closer) error {
	for _, closer := range closers {
		primary = errors.Join(primary, closer.Close())
	}
	return primary
}

func (e *Email) ForwardBuildBytes(ctx *context.Context, sender *models.User, forwardAddress string) ([]byte, error) {
	var b bytes.Buffer

	cfg := config.CurrentConfig()
	if e == nil || sender == nil || cfg == nil || len(cfg.Domains) == 0 {
		return nil, errors.New("invalid forwarding configuration")
	}
	forwardUser := buildUser(forwardAddress)
	if forwardUser == nil || forwardUser.EmailAddress == "" {
		return nil, errors.New("invalid forwarding address")
	}
	to := []*mail.Address{{Name: forwardUser.Name, Address: forwardUser.EmailAddress}}

	senderEmailAddress := fmt.Sprintf("%s@%s", sender.Account, cfg.Domains[0])
	senderAddress := []*mail.Address{{Name: sender.Name, Address: senderEmailAddress}}
	// Create our mail header
	var h mail.Header
	h.SetDate(time.Now())
	h.SetAddressList("From", senderAddress)
	h.SetAddressList("Sender", senderAddress)
	h.SetAddressList("To", to)
	if e.From != nil && e.From.EmailAddress != "" {
		h.SetAddressList("Reply-To", []*mail.Address{{Name: e.From.Name, Address: e.From.EmailAddress}})
		h.Set("X-Original-From", e.From.Build())
	}
	h.Set("X-Forwarded-By", senderEmailAddress)
	h.Set("X-Forwarded-To", forwardUser.EmailAddress)
	h.SetText("Subject", e.Subject)
	msgID, err := GenerateMsgID(cfg.Domain)
	if err != nil {
		return nil, fmt.Errorf("generate message id: %w", err)
	}
	h.SetMessageID(msgID)
	if e.MsgID != "" {
		h.Set("References", fmt.Sprintf("<%s>", e.MsgID))
		h.Set("In-Reply-To", fmt.Sprintf("<%s>", e.MsgID))
	}

	// Create a new mail writer
	mw, err := mail.CreateWriter(&b, h)
	if err != nil {
		return nil, fmt.Errorf("create mail writer: %w", err)
	}

	// Create a text part
	tw, err := mw.CreateInline()
	if err != nil {
		return nil, closeMessageWriters(fmt.Errorf("create inline writer: %w", err), mw)
	}
	var th mail.InlineHeader
	th.Set("Content-Type", "text/plain")
	w, err := tw.CreatePart(th)
	if err != nil {
		return nil, closeMessageWriters(fmt.Errorf("create text part: %w", err), tw, mw)
	}
	if err := writeMessagePart(w, e.Text); err != nil {
		return nil, closeMessageWriters(fmt.Errorf("write text part: %w", err), tw, mw)
	}

	var html mail.InlineHeader
	html.Set("Content-Type", "text/html")
	w, err = tw.CreatePart(html)
	if err != nil {
		return nil, closeMessageWriters(fmt.Errorf("create html part: %w", err), tw, mw)
	}
	if err := writeMessagePart(w, e.HTML); err != nil {
		return nil, closeMessageWriters(fmt.Errorf("write html part: %w", err), tw, mw)
	}

	if err := tw.Close(); err != nil {
		return nil, closeMessageWriters(fmt.Errorf("close inline writer: %w", err), mw)
	}

	// Create an attachment
	for _, attachment := range e.Attachments {
		if attachment == nil {
			return nil, closeMessageWriters(errors.New("nil attachment"), mw)
		}
		var ah mail.AttachmentHeader
		ah.Set("Content-Type", attachment.ContentType)
		if attachment.ContentID != "" {
			ah.Set("Content-ID", fmt.Sprintf("<%s>", attachment.ContentID))
		}
		ah.SetFilename(attachment.Filename)
		w, err = mw.CreateAttachment(ah)
		if err != nil {
			return nil, closeMessageWriters(fmt.Errorf("create attachment: %w", err), mw)
		}
		if err := writeMessagePart(w, attachment.Content); err != nil {
			return nil, closeMessageWriters(fmt.Errorf("write attachment: %w", err), mw)
		}
	}

	if err := mw.Close(); err != nil {
		return nil, fmt.Errorf("close mail writer: %w", err)
	}

	if instance == nil {
		return b.Bytes(), nil
	}

	// dkim 签名后返回
	return instance.Sign(b.String())
}

func (e *Email) BuildPart(ctx *context.Context, loc []int) []byte {
	if len(loc) == 0 {
		return nil
	}

	// 处理顶层 part (part 1 = alternative, part 2+ = attachments)
	if len(loc) == 1 {
		partIdx := loc[0]
		if partIdx == 1 {
			// Part 1 是 alternative，不能直接获取，需要获取子部分
			return nil
		}
		// Part 2, 3, ... 是附件
		attachIdx := partIdx - 2
		if attachIdx >= 0 && attachIdx < len(e.Attachments) {
			encoded := base64.StdEncoding.EncodeToString(e.Attachments[attachIdx].Content)
			encoded += "\r\n"
			return []byte(encoded)
		}
		return nil
	}

	// 处理 alternative 的子部分 (1.1, 1.2)
	if loc[0] == 1 && len(loc) >= 2 {
		subIdx := loc[1]

		// 根据 BODYSTRUCTURE 的构建顺序：先 text，后 html
		// 如果只有一个存在，那个就是 1.1
		hasText := len(e.Text) > 0
		hasHtml := len(e.HTML) > 0

		if hasText && hasHtml {
			// 两者都有: 1.1=text, 1.2=html
			if subIdx == 1 {
				encoded := base64.StdEncoding.EncodeToString(e.Text)
				encoded += "\r\n"
				return []byte(encoded)
			}
			if subIdx == 2 {
				encoded := base64.StdEncoding.EncodeToString(e.HTML)
				encoded += "\r\n"
				return []byte(encoded)
			}
		} else if hasText {
			// 只有 text: 1.1=text
			if subIdx == 1 {
				encoded := base64.StdEncoding.EncodeToString(e.Text)
				encoded += "\r\n"
				return []byte(encoded)
			}
		} else if hasHtml {
			// 只有 html: 1.1=html
			if subIdx == 1 {
				encoded := base64.StdEncoding.EncodeToString(e.HTML)
				encoded += "\r\n"
				return []byte(encoded)
			}
		}
	}

	return nil
}

func (e *Email) BuildBytes(ctx *context.Context, dkim bool) ([]byte, error) {
	var b bytes.Buffer

	if e == nil || e.From == nil || e.From.EmailAddress == "" {
		return nil, errors.New("invalid sender")
	}
	cfg := config.CurrentConfig()
	if e.MsgID == "" && (cfg == nil || cfg.Domain == "") {
		return nil, errors.New("message id domain is not configured")
	}
	from := []*mail.Address{{Name: e.From.Name, Address: e.From.EmailAddress}}
	to := []*mail.Address{}
	for _, user := range e.To {
		if user == nil || user.EmailAddress == "" {
			return nil, errors.New("invalid recipient")
		}
		to = append(to, &mail.Address{
			Name:    user.Name,
			Address: user.EmailAddress,
		})
	}

	// Create our mail header
	var h mail.Header
	if e.Date != "" {
		t, err := time.Parse(time.RFC3339, e.Date)
		if err != nil {
			// Keep compatibility with Date values stored before timezone offsets were preserved.
			t, err = time.ParseInLocation(time.DateTime, e.Date, time.Local)
		}
		if err != nil {
			log.WithContext(ctx).Errorf("Time Error ! Err:%+v", err)
			h.SetDate(time.Now())
		} else {
			h.SetDate(t)
		}
	} else {
		h.SetDate(time.Now())
	}
	if e.MsgID != "" {
		h.SetMessageID(e.MsgID)
	} else {
		h.SetMessageID(fmt.Sprintf("%d@%s", e.MessageId, cfg.Domain))
	}
	h.SetAddressList("From", from)
	h.SetMsgIDList("In-Reply-To", ThreadMessageIDs(e.InReplyTo))
	h.SetMsgIDList("References", ThreadMessageIDs(e.ThreadReferences))
	h.SetAddressList("Sender", from)
	h.SetAddressList("To", to)
	h.SetText("Subject", e.Subject)
	if len(e.ReplyTo) != 0 {
		replyTo := make([]*mail.Address, 0, len(e.ReplyTo))
		for _, user := range e.ReplyTo {
			if user == nil || user.EmailAddress == "" {
				return nil, errors.New("invalid reply-to recipient")
			}
			replyTo = append(replyTo, &mail.Address{Name: user.Name, Address: user.EmailAddress})
		}
		h.SetAddressList("Reply-To", replyTo)
	}
	if len(e.Cc) != 0 {
		cc := []*mail.Address{}
		for _, user := range e.Cc {
			if user == nil || user.EmailAddress == "" {
				return nil, errors.New("invalid cc recipient")
			}
			cc = append(cc, &mail.Address{
				Name:    user.Name,
				Address: user.EmailAddress,
			})
		}
		h.SetAddressList("Cc", cc)
	}

	// Create a new mail writer
	mw, err := mail.CreateWriter(&b, h)
	if err != nil {
		return nil, fmt.Errorf("create mail writer: %w", err)
	}

	// Create a text part
	tw, err := mw.CreateInline()
	if err != nil {
		return nil, closeMessageWriters(fmt.Errorf("create inline writer: %w", err), mw)
	}

	if len(e.Text) > 0 {
		var th mail.InlineHeader
		th.Header.Set("Content-Transfer-Encoding", "base64")
		th.SetContentType("text/plain", map[string]string{
			"charset": "UTF-8",
		})
		w, err := tw.CreatePart(th)
		if err != nil {
			return nil, closeMessageWriters(fmt.Errorf("create text part: %w", err), tw, mw)
		}
		if err := writeMessagePart(w, e.Text); err != nil {
			return nil, closeMessageWriters(fmt.Errorf("write text part: %w", err), tw, mw)
		}
	}

	var html mail.InlineHeader
	html.SetContentType("text/html", map[string]string{
		"charset": "UTF-8",
	})
	html.Header.Set("Content-Transfer-Encoding", "base64")
	w, err := tw.CreatePart(html)
	if err != nil {
		return nil, closeMessageWriters(fmt.Errorf("create html part: %w", err), tw, mw)
	}
	htmlContent := e.HTML
	if len(htmlContent) == 0 {
		htmlContent = e.Text
	}
	if err := writeMessagePart(w, htmlContent); err != nil {
		return nil, closeMessageWriters(fmt.Errorf("write html part: %w", err), tw, mw)
	}

	if err := tw.Close(); err != nil {
		return nil, closeMessageWriters(fmt.Errorf("close inline writer: %w", err), mw)
	}

	// Create an attachment
	for _, attachment := range e.Attachments {
		if attachment == nil {
			return nil, closeMessageWriters(errors.New("nil attachment"), mw)
		}
		var ah mail.AttachmentHeader
		ah.Set("Content-Type", attachment.ContentType)
		if attachment.ContentID != "" {
			ah.Set("Content-ID", fmt.Sprintf("<%s>", attachment.ContentID))
		}
		ah.SetFilename(attachment.Filename)
		w, err = mw.CreateAttachment(ah)
		if err != nil {
			return nil, closeMessageWriters(fmt.Errorf("create attachment: %w", err), mw)
		}
		if err := writeMessagePart(w, attachment.Content); err != nil {
			return nil, closeMessageWriters(fmt.Errorf("write attachment: %w", err), mw)
		}
	}

	if err := mw.Close(); err != nil {
		return nil, fmt.Errorf("close mail writer: %w", err)
	}

	if dkim {
		if instance == nil {
			return nil, errors.New("DKIM signer is not initialized")
		}
		// dkim 签名后返回
		return instance.Sign(b.String())
	}
	return b.Bytes(), nil
}
