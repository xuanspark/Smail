package match

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"git.pcux.cn/xuans/Smail/dto/parsemail"
	appcontext "git.pcux.cn/xuans/Smail/utils/context"
	log "github.com/sirupsen/logrus"
)

func TestRegexMatch(t *testing.T) {
	email := &parsemail.Email{Subject: "safe@example.com"}
	if !NewRegexMatch("Subject", `^(?!.*blocked\.example$).*`).Match(&appcontext.Context{}, email) {
		t.Fatal("valid regex did not match")
	}
}

func TestRegexMatchRejectsInvalidPatternWithoutPanic(t *testing.T) {
	email := &parsemail.Email{Subject: "subject"}
	if NewRegexMatch("Subject", "(").Match(&appcontext.Context{}, email) {
		t.Fatal("invalid regex matched")
	}
}

func TestRegexMatchTimesOutCatastrophicBacktracking(t *testing.T) {
	email := &parsemail.Email{Subject: strings.Repeat("a", 200000) + "!"}
	var output bytes.Buffer
	logger := log.StandardLogger()
	oldOutput := logger.Out
	logger.SetOutput(&output)
	t.Cleanup(func() { logger.SetOutput(oldOutput) })
	started := time.Now()
	if NewRegexMatch("Subject", `^(a+)+$`).Match(&appcontext.Context{}, email) {
		t.Fatal("catastrophic regex unexpectedly matched")
	}
	if elapsed := time.Since(started); elapsed > 2*time.Second {
		t.Fatalf("regex timeout took too long: %s", elapsed)
	}
	if strings.Contains(output.String(), email.Subject) || output.Len() > 4096 {
		t.Fatalf("regex failure leaked or amplified message content: log_bytes=%d", output.Len())
	}
}

func TestValidateRegexRejectsOversizedPattern(t *testing.T) {
	if err := ValidateRegex(strings.Repeat("a", maxRegexPatternBytes+1)); err == nil {
		t.Fatal("oversized regex was accepted")
	}
}

func TestRuleBodyMatchingIsSizeLimited(t *testing.T) {
	email := &parsemail.Email{Text: []byte(strings.Repeat("a", maxRuleFieldBytes) + "needle")}
	if NewContainsMatch("Text", "needle").Match(&appcontext.Context{}, email) {
		t.Fatal("rule matched content beyond the field inspection limit")
	}
	email.Text = []byte("needle" + strings.Repeat("a", maxRuleFieldBytes))
	if !NewContainsMatch("Text", "needle").Match(&appcontext.Context{}, email) {
		t.Fatal("rule did not match content within the field inspection limit")
	}
}

func TestGetFieldContentHandlesMissingAddresses(t *testing.T) {
	email := &parsemail.Email{}
	if got := getFieldContent("From", email); got != "" {
		t.Fatalf("missing From returned %q", got)
	}
	if got := getFieldContent("Sender", email); got != "" {
		t.Fatalf("missing Sender returned %q", got)
	}
}
