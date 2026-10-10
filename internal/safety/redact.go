package safety

import (
	"regexp"
	"strings"
)

var signedQuery = regexp.MustCompile(`(?i)([?&]X-Amz-(?:Signature|Credential|Security-Token)=)[^&\s"'<>]*`)

func Redact(s string, secrets ...string) string {
	s = signedQuery.ReplaceAllString(s, "${1}[REDACTED]")
	for _, secret := range secrets {
		if secret != "" {
			s = strings.ReplaceAll(s, secret, "[REDACTED]")
		}
	}
	return s
}
