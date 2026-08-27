package validation

import (
	"errors"
	"net/mail"
	"strings"

	"git.pcux.cn/xuans/Smail/dto/parsemail"
)

var (
	ErrInvalidEmail       = errors.New("invalid email")
	ErrTooManyRecipients  = errors.New("too many recipients")
	ErrTooManyAttachments = errors.New("too many attachments")
	ErrAttachmentTooLarge = errors.New("attachment too large")
	ErrEmailTooLarge      = errors.New("email too large")
)

type EmailLimits struct {
	MaxRecipients           int
	MaxAttachments          int
	MaxAttachmentBytes      int64
	MaxTotalAttachmentBytes int64
	MaxBodyBytes            int64
	MaxEstimatedBytes       int64
}

func ValidateEmail(email *parsemail.Email, limits EmailLimits) error {
	if email == nil || email.From == nil || email.Sender == nil {
		return ErrInvalidEmail
	}

	recipients := len(email.To) + len(email.Cc) + len(email.Bcc) + len(email.ReplyTo)
	if recipients == 0 {
		return ErrInvalidEmail
	}
	if limits.MaxRecipients > 0 && recipients > limits.MaxRecipients {
		return ErrTooManyRecipients
	}
	for _, list := range [][]*parsemail.User{email.To, email.Cc, email.Bcc, email.ReplyTo} {
		for _, user := range list {
			if user == nil || !IsMailboxAddress(user.EmailAddress) {
				return ErrInvalidEmail
			}
		}
	}

	bodyBytes := int64(len(email.Text)) + int64(len(email.HTML))
	if limits.MaxBodyBytes > 0 && bodyBytes > limits.MaxBodyBytes {
		return ErrEmailTooLarge
	}
	if limits.MaxAttachments > 0 && len(email.Attachments) > limits.MaxAttachments {
		return ErrTooManyAttachments
	}

	totalAttachmentBytes := int64(0)
	estimatedBytes := base64Size(int64(len(email.Text))) + base64Size(int64(len(email.HTML))) + 64<<10
	for _, attachment := range email.Attachments {
		if attachment == nil || strings.ContainsAny(attachment.ContentType, "\r\n") {
			return ErrInvalidEmail
		}
		size := int64(len(attachment.Content))
		if limits.MaxAttachmentBytes > 0 && size > limits.MaxAttachmentBytes {
			return ErrAttachmentTooLarge
		}
		if size > 0 && totalAttachmentBytes > int64(^uint64(0)>>1)-size {
			return ErrEmailTooLarge
		}
		totalAttachmentBytes += size
		if limits.MaxTotalAttachmentBytes > 0 && totalAttachmentBytes > limits.MaxTotalAttachmentBytes {
			return ErrEmailTooLarge
		}
		estimatedBytes += base64Size(size) + int64(len(attachment.Filename)) + int64(len(attachment.ContentType)) + 1024
		if limits.MaxEstimatedBytes > 0 && estimatedBytes > limits.MaxEstimatedBytes {
			return ErrEmailTooLarge
		}
	}
	if limits.MaxEstimatedBytes > 0 && estimatedBytes > limits.MaxEstimatedBytes {
		return ErrEmailTooLarge
	}
	return nil
}

func IsMailboxAddress(address string) bool {
	address = strings.TrimSpace(address)
	parsed, err := mail.ParseAddress(address)
	return err == nil && parsed.Address == address
}

func base64Size(size int64) int64 {
	if size <= 0 {
		return 0
	}
	return ((size + 2) / 3) * 4
}
