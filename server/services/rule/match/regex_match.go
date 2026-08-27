package match

import (
	"fmt"
	"time"

	"git.pcux.cn/xuans/Smail/dto/parsemail"
	"git.pcux.cn/xuans/Smail/utils/context"
	"github.com/dlclark/regexp2"
	log "github.com/sirupsen/logrus"
)

const (
	maxRegexPatternBytes = 2048
	regexMatchTimeout    = 100 * time.Millisecond
)

type RegexMatch struct {
	Rule  string
	Field string
}

func NewRegexMatch(field, rule string) *RegexMatch {
	return &RegexMatch{
		Rule:  rule,
		Field: field,
	}
}

func ValidateRegex(pattern string) error {
	_, err := compileRegex(pattern)
	return err
}

func compileRegex(pattern string) (*regexp2.Regexp, error) {
	if pattern == "" || len(pattern) > maxRegexPatternBytes {
		return nil, fmt.Errorf("regex pattern length is invalid")
	}
	re, err := regexp2.Compile(pattern, 0)
	if err != nil {
		return nil, fmt.Errorf("compile regex: %w", err)
	}
	re.MatchTimeout = regexMatchTimeout
	return re, nil
}

func (r *RegexMatch) Match(ctx *context.Context, email *parsemail.Email) bool {
	if r == nil || email == nil {
		return false
	}
	re, err := compileRegex(r.Rule)
	if err != nil {
		log.WithContext(ctx).Warn("rule regex rejected")
		return false
	}
	matched, err := re.MatchString(getFieldContent(r.Field, email))
	if err != nil {
		log.WithContext(ctx).Warn("rule regex match failed")
		return false
	}
	return matched
}
