package parsemail

import (
	"strings"

	"github.com/emersion/go-message/mail"
)

func ThreadMessageIDs(value string) []string {
	if value == "" || len(value) > 32768 || strings.ContainsAny(value, "\r\n\x00") {
		return nil
	}
	var header mail.Header
	header.Set("References", value)
	ids, err := header.MsgIDList("References")
	if err != nil {
		return nil
	}
	result := make([]string, 0, len(ids))
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if len(id) > 254 || !strings.Contains(id, "@") || strings.ContainsAny(id, "<>\r\n\x00\t ") {
			continue
		}
		if strings.IndexFunc(id, func(r rune) bool { return r < 33 || r == 127 }) >= 0 {
			continue
		}
		if !seen[id] {
			result = append(result, id)
			seen[id] = true
		}
	}
	if len(result) > 64 {
		result = append(result[:1:1], result[len(result)-63:]...)
	}
	return result
}

func NormalizeThreadHeader(value string) string {
	ids := ThreadMessageIDs(value)
	if len(ids) == 0 {
		return ""
	}
	return "<" + strings.Join(ids, "> <") + ">"
}

func ReplyThread(messageID, references, inReplyTo string) (string, string) {
	parent := NormalizeThreadHeader("<" + messageID + ">")
	if len(ThreadMessageIDs(parent)) != 1 {
		return "", ""
	}
	chain := NormalizeThreadHeader(references)
	if chain == "" && len(ThreadMessageIDs(inReplyTo)) == 1 {
		chain = NormalizeThreadHeader(inReplyTo)
	}
	return parent, NormalizeThreadHeader(strings.TrimSpace(chain + " " + parent))
}
