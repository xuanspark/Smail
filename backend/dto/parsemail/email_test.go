package parsemail

import (
	"bytes"
	"fmt"
	"io"
	stdmail "net/mail"
	"strings"
	"testing"
	"time"

	"git.pcux.cn/xuans/Smail/models"
	"github.com/emersion/go-message"
)

func mustParseEmail(t *testing.T, to []string, r io.Reader, size int) *Email {
	t.Helper()
	email, err := NewEmailFromReader(to, r, size)
	if err != nil {
		t.Fatalf("parse email: %v", err)
	}
	return email
}

func TestAttachmentFilenameFromMIME(t *testing.T) {
	cases := []struct {
		name        string
		contentType string
		disposition string
		want        string
	}{
		{"plain", "application/octet-stream", `attachment; filename="report.txt"`, "report.txt"},
		{"quoted_semicolon", "application/octet-stream", `attachment; filename="report; final.txt"`, "report; final.txt"},
		{"extended_utf8", "text/plain", "attachment; filename*=utf-8''%E4%B8%AD%E6%96%87.txt", "中文.txt"},
		{"continued_utf8", "text/html", "attachment; filename*0*=utf-8''%E4%B8%AD;\r\n filename*1*=%E6%96%87.txt", "中文.txt"},
		{"encoded_word", "application/octet-stream", `attachment; filename="=?UTF-8?B?5Lit5paHLnR4dA==?="`, "中文.txt"},
		{"content_type_name", `application/octet-stream; name="legacy.txt"`, "attachment", "legacy.txt"},
		{"preserve_name_priority", `application/octet-stream; name="legacy.txt"`, `attachment; filename="other.txt"`, "legacy.txt"},
		{"missing", "application/octet-stream", "attachment", "no_name_file"},
		{"invalid", "application/octet-stream", "attachment; filename", "no_name_file"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := []byte("From: sender@example.com\r\n" +
				"To: recipient@example.com\r\n" +
				"Subject: MIME attachment\r\n" +
				"MIME-Version: 1.0\r\n" +
				"Content-Type: multipart/mixed; boundary=attachment-test\r\n\r\n" +
				"--attachment-test\r\nContent-Type: text/plain\r\n\r\nbody\r\n" +
				"--attachment-test\r\nContent-Type: " + tc.contentType + "\r\n" +
				"Content-Disposition: " + tc.disposition + "\r\n\r\nattachment body\r\n" +
				"--attachment-test--\r\n")
			email := mustParseEmail(t, []string{"recipient@example.com"}, bytes.NewReader(raw), len(raw))
			if len(email.Attachments) != 1 {
				t.Fatalf("附件数量错误：%d", len(email.Attachments))
			}
			if got := email.Attachments[0].Filename; got != tc.want {
				t.Fatalf("附件名称错误：实际 %q，预期 %q", got, tc.want)
			}
			if got := string(email.Attachments[0].Content); got != "attachment body" {
				t.Fatalf("附件内容被改变：%q", got)
			}
		})
	}
}

func builtMessageDate(t *testing.T, raw []byte) time.Time {
	t.Helper()

	m, err := stdmail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("read built message: %v", err)
	}
	date, err := stdmail.ParseDate(m.Header.Get("Date"))
	if err != nil {
		t.Fatalf("parse built Date header %q: %v", m.Header.Get("Date"), err)
	}
	return date
}

func TestEmailDateFromReaderPreservesInstant(t *testing.T) {
	const dateHeader = "Sun, 09 Aug 2026 11:51:08 +0000"
	raw := []byte("From: sender@example.com\r\n" +
		"To: recipient@example.com\r\n" +
		"Subject: Date round trip\r\n" +
		"Date: " + dateHeader + "\r\n" +
		"Message-ID: <date-reader@example.com>\r\n" +
		"Content-Type: text/plain; charset=UTF-8\r\n\r\n" +
		"body")

	expected, err := stdmail.ParseDate(dateHeader)
	if err != nil {
		t.Fatalf("parse fixture date: %v", err)
	}

	email := mustParseEmail(t, []string{"recipient@example.com"}, bytes.NewReader(raw), len(raw))
	stored, err := time.Parse(time.RFC3339, email.Date)
	if err != nil {
		t.Fatalf("stored Date is not RFC3339: %q: %v", email.Date, err)
	}
	if !stored.Equal(expected) {
		t.Fatalf("stored Date changed instant: got %s, want %s", stored, expected)
	}

	built, err := email.BuildBytes(nil, false)
	if err != nil {
		t.Fatal(err)
	}
	got := builtMessageDate(t, built)
	if !got.Equal(expected) {
		t.Fatalf("rebuilt Date changed instant: got %s, want %s", got, expected)
	}
}

func TestEmailDateFromModelPreservesUTCInstant(t *testing.T) {
	expected := time.Date(2026, time.August, 9, 11, 51, 8, 0, time.UTC)
	email, err := NewEmailFromModel(models.Email{
		Id:          1,
		FromAddress: "sender@example.com",
		To:          `[{"EmailAddress":"recipient@example.com","Name":""}]`,
		Subject:     "Model date",
		SendDate:    expected,
		MsgID:       "date-model@example.com",
	})
	if err != nil {
		t.Fatal(err)
	}

	if email.Date != expected.Format(time.RFC3339) {
		t.Fatalf("model Date = %q, want %q", email.Date, expected.Format(time.RFC3339))
	}

	built, err := email.BuildBytes(nil, false)
	if err != nil {
		t.Fatal(err)
	}
	got := builtMessageDate(t, built)
	if !got.Equal(expected) {
		t.Fatalf("rebuilt model Date changed instant: got %s, want %s", got, expected)
	}
}

func TestEmailBuildBytesAcceptsLegacyDate(t *testing.T) {
	const legacyDate = "2026-08-09 11:51:08"
	expected, err := time.ParseInLocation(time.DateTime, legacyDate, time.Local)
	if err != nil {
		t.Fatalf("parse legacy fixture date: %v", err)
	}

	email := &Email{
		From:    buildUser("sender@example.com"),
		To:      buildUsers([]string{"recipient@example.com"}),
		Subject: "Legacy date",
		Text:    []byte("body"),
		Date:    legacyDate,
		MsgID:   "date-legacy@example.com",
	}

	built, err := email.BuildBytes(nil, false)
	if err != nil {
		t.Fatal(err)
	}
	got := builtMessageDate(t, built)
	if !got.Equal(expected) {
		t.Fatalf("legacy Date changed instant: got %s, want %s", got, expected)
	}
}

func TestHtmlTxtAttachment(t *testing.T) {
	emailBytes := `From: "=?utf-8?B?amlubnJyeQ==?=" <ok@xjiangwei.cn>
To: "=?utf-8?B?YWRtaW4=?=" <admin@jinnrry.com>
Subject: FileTest
Mime-Version: 1.0
Content-Type: multipart/mixed;
        boundary="----=_NextPart_6A102A6A_F16B7280_56878F2C"
Content-Transfer-Encoding: 8Bit
Date: Fri, 22 May 2026 18:05:30 +0800
Message-ID: <tencent_CB10C599AF69D91666D5A66E4505F633A705@qq.com>

This is a multi-part message in MIME format.

------=_NextPart_6A102A6A_F16B7280_56878F2C
Content-Type: multipart/alternative;
        boundary="----=_NextPart_6A102A6A_F16B7280_0A1A2221";

------=_NextPart_6A102A6A_F16B7280_0A1A2221
Content-Type: text/plain;
        charset="utf-8"
Content-Transfer-Encoding: base64

RW1haWwgQ29udGVudCE=

------=_NextPart_6A102A6A_F16B7280_0A1A2221
Content-Type: text/html;
        charset="utf-8"
Content-Transfer-Encoding: base64

PGRpdiBzdHlsZT0iZm9udC1mYW1pbHk6IC1hcHBsZS1zeXN0ZW0sIHN5c3RlbS11aTsgZm9u
dC1zaXplOiAxNHB4OyBjb2xvcjogcmdiKDAsIDAsIDApOyBsaW5lLWhlaWdodDogMS40Mzsi
PjxiPkVtYWlsIENvbnRlbnQhPC9iPjwvZGl2Pg==

------=_NextPart_6A102A6A_F16B7280_0A1A2221--

------=_NextPart_6A102A6A_F16B7280_56878F2C
Content-Type: application/octet-stream;
        charset="utf-8";
        name="html.html"
Content-Disposition: attachment; filename="html.html"
Content-Transfer-Encoding: base64

dGhpcyBpcyBodG1sIGZpbGUgY29udGV4dCE=

------=_NextPart_6A102A6A_F16B7280_56878F2C
Content-Type: application/octet-stream;
        charset="utf-8";
        name="txt.txt"
Content-Disposition: attachment; filename="txt.txt"
Content-Transfer-Encoding: base64

dGhpcyBpcyB0eHQgZmlsZSBjb250ZW50IQ==

------=_NextPart_6A102A6A_F16B7280_56878F2C--`

	email := mustParseEmail(t, []string{"admin@jinnrry.com"}, bytes.NewReader([]byte(emailBytes)), len(emailBytes))

	if strings.Contains(string(email.Text), "file") {
		t.Errorf("formatContentError:%s", "邮件内容解析错误，文件内容被解析到邮件中！")
	}

	if strings.Contains(string(email.HTML), "file") {
		t.Errorf("formatContentError:%s", "邮件内容解析错误，文件内容被解析到邮件中！")
	}

	emailBytes = `Content-Type: multipart/mixed;
 boundary=ee32d21f4234ae8884badf33c4eec57d61a93bc5048453d42dd9313d5974
Mime-Version: 1.0
Subject: TestHtmlTxtAttachment
To: <admin@jinnrry.com>
Sender: "jinnrry" <admin@jinnrry.com>
From: "jinnrry" <admin@jinnrry.com>
Message-Id: <0f376803f71ff716c49bdf0479c5568c.1779445057831143578@jinnrry.com>
Date: Fri, 22 May 2026 18:17:37 +0800

--ee32d21f4234ae8884badf33c4eec57d61a93bc5048453d42dd9313d5974
Content-Type: multipart/alternative;
 boundary=71b61b51b82589aba3a3315cc61b6406a790a8d6e50c34782a4f2149134d

--71b61b51b82589aba3a3315cc61b6406a790a8d6e50c34782a4f2149134d
Content-Disposition: inline
Content-Type: text/plain; charset=UTF-8
Content-Transfer-Encoding: base64

SGVyZSBpcyBlbWFpbCBjb250ZW50Lg==
--71b61b51b82589aba3a3315cc61b6406a790a8d6e50c34782a4f2149134d
Content-Disposition: inline
Content-Transfer-Encoding: base64
Content-Type: text/html; charset=UTF-8

PHA+PHN0cm9uZz5IZXJlIGlzIGVtYWlsIGNvbnRlbnQuPC9zdHJvbmc+PC9wPg==
--71b61b51b82589aba3a3315cc61b6406a790a8d6e50c34782a4f2149134d--

--ee32d21f4234ae8884badf33c4eec57d61a93bc5048453d42dd9313d5974
Content-Transfer-Encoding: base64
Content-Disposition: attachment; filename=html.html
Content-Type: text/html

dGhpcyBpcyBodG1sIGZpbGUgY29udGV4dCE=
--ee32d21f4234ae8884badf33c4eec57d61a93bc5048453d42dd9313d5974
Content-Transfer-Encoding: base64
Content-Disposition: attachment; filename=txt.txt
Content-Type: text/plain

dGhpcyBpcyB0eHQgZmlsZSBjb250ZW50IQ==
--ee32d21f4234ae8884badf33c4eec57d61a93bc5048453d42dd9313d5974--`

	email = mustParseEmail(t, []string{"admin@jinnrry.com"}, bytes.NewReader([]byte(emailBytes)), len(emailBytes))

	if strings.Contains(string(email.Text), "file") {
		t.Errorf("formatContentError:%s", "邮件内容解析错误，文件内容被解析到邮件中！")
	}

	if strings.Contains(string(email.HTML), "file") {
		t.Errorf("formatContentError:%s", "邮件内容解析错误，文件内容被解析到邮件中！")
	}

}

func Test_buildUser(t *testing.T) {
	u := buildUser("Jinnrry N <jiangwei1995910@gmail.com>")
	if u.EmailAddress != "jiangwei1995910@gmail.com" {
		t.Error("error")
	}
	if u.Name != "Jinnrry N" {
		t.Error("error")
	}

	u = buildUser("=?UTF-8?B?YWRtaW5AamlubnJyeS5jb20=?=<admin@jinnrry.com>")
	if u.EmailAddress != "admin@jinnrry.com" {
		t.Error("error")
	}
	if u.Name != "admin@jinnrry.com" {
		t.Error("error")
	}

	u = buildUser("\"admin@jinnrry.com\" <admin@jinnrry.com>")
	if u.EmailAddress != "admin@jinnrry.com" {
		t.Error("error")
	}
	if u.Name != "admin@jinnrry.com" {
		t.Error("error")
	}
}

func TestEmailBuidlers(t *testing.T) {
	var b bytes.Buffer

	var h message.Header
	h.SetContentType("multipart/alternative", nil)
	w, err := message.CreateWriter(&b, h)
	if err != nil {
	}

	var h1 message.Header
	h1.SetContentType("text/html", nil)
	w1, err := w.CreatePart(h1)
	if err != nil {
	}
	io.WriteString(w1, "<h1>Hello World!</h1><p>This is an HTML part.</p>")
	w1.Close()

	var h2 message.Header
	h2.SetContentType("text/plain", nil)
	w2, err := w.CreatePart(h2)
	if err != nil {
	}
	io.WriteString(w2, "Hello World!\n\nThis is a text part.")
	w2.Close()

	w.Close()

	fmt.Println(b.String())
}

func TestEmail_builder(t *testing.T) {
	e := Email{
		From:    buildUser("i@test.com"),
		To:      buildUsers([]string{"to@test.com"}),
		Subject: "Title中文",
		MsgID:   "builder@example.com",
		HTML:    []byte("Html"),
		Text:    []byte("Text"),
		Attachments: []*Attachment{
			{
				Filename:    "a.png",
				ContentType: "image/jpeg",
				Content:     []byte("aaa"),
				ContentID:   "1",
			},
		},
	}

	rest, err := e.BuildBytes(nil, false)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Println(string(rest))
}

func TestEmail_BuildPart(t *testing.T) {
	e := Email{
		Text: []byte("text"),
		HTML: []byte("html"),
	}
	res := e.BuildPart(nil, []int{1, 2})
	fmt.Println(string(res))

}

func TestNewEmailFromReaderRejectsMalformedSubject(t *testing.T) {
	raw := "From: sender@example.com\r\n" +
		"To: recipient@example.com\r\n" +
		"Subject: =?X-UNKNOWN?Q?broken?=\r\n" +
		"Content-Type: text/plain; charset=UTF-8\r\n\r\n" +
		"body"
	email, err := NewEmailFromReader([]string{"recipient@example.com"}, strings.NewReader(raw), len(raw))
	if err == nil {
		t.Fatal("malformed subject was accepted")
	}
	if email != nil {
		t.Fatal("malformed subject returned a partial email")
	}
}

func TestNewEmailFromReaderRejectsMalformedMessage(t *testing.T) {
	email, err := NewEmailFromReader(nil, strings.NewReader("invalid header\r\n\r\nbody"), 22)
	if err == nil {
		t.Fatal("malformed message was accepted")
	}
	if email != nil {
		t.Fatal("malformed message returned a partial email")
	}
}

func TestNewEmailFromModelRejectsCorruptedStoredJSON(t *testing.T) {
	tests := []struct {
		name  string
		apply func(*models.Email)
	}{
		{name: "to", apply: func(email *models.Email) { email.To = "{" }},
		{name: "reply-to", apply: func(email *models.Email) { email.ReplyTo = "{" }},
		{name: "sender", apply: func(email *models.Email) { email.Sender = "{" }},
		{name: "bcc", apply: func(email *models.Email) { email.Bcc = "{" }},
		{name: "cc", apply: func(email *models.Email) { email.Cc = "{" }},
		{name: "attachments", apply: func(email *models.Email) { email.Attachments = "{" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			model := models.Email{}
			tt.apply(&model)
			if _, err := NewEmailFromModel(model); err == nil {
				t.Fatal("corrupted stored JSON was accepted")
			}
		})
	}
}

func TestNewEmailFromModelAcceptsBlankStoredJSON(t *testing.T) {
	if _, err := NewEmailFromModel(models.Email{To: "  ", Attachments: "	"}); err != nil {
		t.Fatal(err)
	}
}
