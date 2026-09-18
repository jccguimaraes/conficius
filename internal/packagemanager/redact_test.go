package packagemanager

import (
	"os"
	"strings"
	"testing"
)

func TestRedactSensitive(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	input := "fetch https://user:pass@example.com/pkg token=abc123 password:hunter2 path=" + home + "/project"
	got := redactSensitive(input)
	for _, secret := range []string{"user:pass", "abc123", "hunter2", home} {
		if strings.Contains(got, secret) {
			t.Fatalf("redacted value contains %q: %q", secret, got)
		}
	}
	for _, want := range []string{"https://[REDACTED]@example.com/pkg", "token=[REDACTED]", "password:[REDACTED]", "~/project"} {
		if !strings.Contains(got, want) {
			t.Fatalf("redacted value %q missing %q", got, want)
		}
	}
}
