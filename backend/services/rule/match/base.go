package match

import (
	"git.pcux.cn/xuans/Smail/dto/parsemail"
	"git.pcux.cn/xuans/Smail/utils/context"
)

const (
	RuleTypeRegex     = "regex"
	RuleTypeContains  = "contains"
	RuleTypeEq        = "equal"
	maxRuleFieldBytes = 1 << 20
)

type Match interface {
	Match(ctx *context.Context, email *parsemail.Email) bool
}

func buildUsers(users []*parsemail.User) string {
	ret := ""
	for i, u := range users {
		if i != 0 {
			ret += ","
		}
		ret += u.EmailAddress
	}
	return ret
}

func limitedFieldString(value []byte) string {
	if len(value) > maxRuleFieldBytes {
		value = value[:maxRuleFieldBytes]
	}
	return string(value)
}

func limitedFieldJoin(first, second []byte) string {
	remaining := maxRuleFieldBytes
	capacity := len(first) + len(second)
	if capacity > remaining {
		capacity = remaining
	}
	result := make([]byte, 0, capacity)
	if len(first) > remaining {
		first = first[:remaining]
	}
	result = append(result, first...)
	remaining -= len(first)
	if len(second) > remaining {
		second = second[:remaining]
	}
	result = append(result, second...)
	return string(result)
}

func getFieldContent(field string, email *parsemail.Email) string {
	if email == nil {
		return ""
	}
	switch field {
	case "ReplyTo":
		return buildUsers(email.ReplyTo)
	case "From":
		if email.From != nil {
			return email.From.EmailAddress
		}
	case "Subject":
		return email.Subject
	case "To":
		return buildUsers(email.To)
	case "Bcc":
		return buildUsers(email.Bcc)
	case "Cc":
		return buildUsers(email.Cc)
	case "Text":
		return limitedFieldString(email.Text)
	case "Html":
		return limitedFieldString(email.HTML)
	case "Sender":
		if email.Sender != nil {
			return email.Sender.EmailAddress
		}
	case "Content":
		return limitedFieldJoin(email.HTML, email.Text)
	}
	return ""
}
