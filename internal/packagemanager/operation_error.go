package packagemanager

import (
	"context"
	"errors"
	"os/exec"
	"strings"
)

type ErrorCategory string

const (
	ErrorUnavailable    ErrorCategory = "unavailable"
	ErrorAuthentication ErrorCategory = "authentication"
	ErrorNetwork        ErrorCategory = "network"
	ErrorRateLimit      ErrorCategory = "rate_limit"
	ErrorRegistry       ErrorCategory = "registry"
	ErrorParser         ErrorCategory = "parser"
	ErrorTimeout        ErrorCategory = "timeout"
	ErrorCancelled      ErrorCategory = "cancelled"
	ErrorCommand        ErrorCategory = "command"
)

type OperationError struct {
	Category ErrorCategory
	Err      error
}

func (e *OperationError) Error() string { return redactSensitive(e.Err.Error()) }
func (e *OperationError) Unwrap() error { return e.Err }

func parserError(err error) error {
	if err == nil {
		return nil
	}
	return &OperationError{Category: ErrorParser, Err: err}
}

func classifyOperationError(ctx context.Context, err error, stderr string) error {
	if err == nil {
		return nil
	}
	category := ErrorCommand
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded), errors.Is(err, context.DeadlineExceeded):
		category = ErrorTimeout
	case errors.Is(ctx.Err(), context.Canceled), errors.Is(err, context.Canceled):
		category = ErrorCancelled
	case errors.Is(err, exec.ErrNotFound):
		category = ErrorUnavailable
	default:
		message := strings.ToLower(stderr)
		switch {
		case strings.Contains(message, "too many requests"), strings.Contains(message, "rate limit"), strings.Contains(message, "http 429"):
			category = ErrorRateLimit
		case strings.Contains(message, "unauthorized"), strings.Contains(message, "forbidden"), strings.Contains(message, "authentication"), strings.Contains(message, "http 401"), strings.Contains(message, "http 403"):
			category = ErrorAuthentication
		case strings.Contains(message, "network"), strings.Contains(message, "connection refused"), strings.Contains(message, "connection reset"), strings.Contains(message, "timed out"), strings.Contains(message, "could not resolve"), strings.Contains(message, "no such host"):
			category = ErrorNetwork
		case strings.Contains(message, "registry"):
			category = ErrorRegistry
		}
	}
	return &OperationError{Category: category, Err: err}
}
