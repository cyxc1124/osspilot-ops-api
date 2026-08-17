package audit

import (
	"regexp"
)

var passwordValue = regexp.MustCompile(`(?i)(password|passwd|pwd)\s*[:=]\s*\S+`)

func sanitizeError(action string, msg *string) *string {
	if msg == nil {
		return nil
	}
	if action == "user_password_reset" || action == "password_change" {
		s := "[redacted]"
		return &s
	}
	out := passwordValue.ReplaceAllString(*msg, "${1}=[redacted]")
	return &out
}
