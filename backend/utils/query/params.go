package query

import "strings"

func IntList(values []int, max int) (string, []any, bool) {
	if len(values) == 0 || max <= 0 || len(values) > max {
		return "", nil, false
	}
	args := make([]any, len(values))
	var placeholders strings.Builder
	placeholders.Grow(len(values)*2 - 1)
	for i, value := range values {
		if value <= 0 {
			return "", nil, false
		}
		if i > 0 {
			placeholders.WriteByte(',')
		}
		placeholders.WriteByte('?')
		args[i] = value
	}
	return placeholders.String(), args, true
}
