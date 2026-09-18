package packagemanager

import (
	"context"
	"errors"
	"os/exec"
	"testing"
)

func TestClassifyOperationError(t *testing.T) {
	tests := []struct {
		name     string
		ctx      context.Context
		err      error
		stderr   string
		category ErrorCategory
	}{
		{name: "unavailable", ctx: context.Background(), err: exec.ErrNotFound, category: ErrorUnavailable},
		{name: "authentication", ctx: context.Background(), err: errors.New("exit status 1"), stderr: "HTTP 401 Unauthorized", category: ErrorAuthentication},
		{name: "rate limit", ctx: context.Background(), err: errors.New("exit status 1"), stderr: "429 Too Many Requests", category: ErrorRateLimit},
		{name: "network", ctx: context.Background(), err: errors.New("exit status 1"), stderr: "connection refused", category: ErrorNetwork},
		{name: "registry", ctx: context.Background(), err: errors.New("exit status 1"), stderr: "registry unavailable", category: ErrorRegistry},
		{name: "command", ctx: context.Background(), err: errors.New("exit status 2"), category: ErrorCommand},
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	tests = append(tests, struct {
		name     string
		ctx      context.Context
		err      error
		stderr   string
		category ErrorCategory
	}{name: "cancelled", ctx: cancelled, err: errors.New("signal: killed"), category: ErrorCancelled})

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := classifyOperationError(test.ctx, test.err, test.stderr)
			var operationErr *OperationError
			if !errors.As(err, &operationErr) || operationErr.Category != test.category {
				t.Fatalf("error = %#v; want category %q", err, test.category)
			}
			if !errors.Is(err, test.err) {
				t.Fatalf("error does not wrap original: %v", err)
			}
		})
	}
}
