package packagemanager

import (
	"os"
	"regexp"
	"strings"
)

var (
	credentialURLPattern = regexp.MustCompile(`(?i)(https?://)[^\s/@:]+:[^\s/@]+@`)
	secretPattern        = regexp.MustCompile(`(?i)(token|password|passwd|secret|api[_-]?key)(=|:)[^\s]+`)
)

func redactSensitive(value string) string {
	value = credentialURLPattern.ReplaceAllString(value, `${1}[REDACTED]@`)
	value = secretPattern.ReplaceAllString(value, `${1}${2}[REDACTED]`)
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		value = strings.ReplaceAll(value, home, "~")
	}
	return value
}
