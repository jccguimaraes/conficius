package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"syscall"

	"golang.org/x/sys/unix"
)

const restartStateEnv = "CONFICIUS_TUI_RESTART_STATE"

type restartState struct {
	Profile   string            `json:"profile,omitempty"`
	Screen    string            `json:"screen"`
	ScopeKind string            `json:"scope_kind,omitempty"`
	ScopePath string            `json:"scope_path,omitempty"`
	Failed    map[string]string `json:"failed,omitempty"`
	Attempted map[string]string `json:"attempted,omitempty"`
}

func encodeRestartState(state restartState) (string, error) {
	contents, err := json.Marshal(state)
	if err != nil {
		return "", fmt.Errorf("encode restart state: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(contents), nil
}

func decodeRestartState(value string) (restartState, error) {
	contents, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return restartState{}, fmt.Errorf("decode restart state: %w", err)
	}
	var state restartState
	if err := json.Unmarshal(contents, &state); err != nil {
		return restartState{}, fmt.Errorf("parse restart state: %w", err)
	}
	return state, nil
}

func restoreRestartState(data *model, state restartState) string {
	if state.Profile != "" {
		data.profileOverride = state.Profile
		_ = reloadPackageConfig(data)
	}
	for i := range data.scopes {
		if data.scopes[i].Kind == state.ScopeKind && data.scopes[i].Path == state.ScopePath {
			data.active = &data.scopes[i]
			break
		}
	}
	for key, message := range state.Failed {
		data.failed[key] = message
	}
	for key, version := range state.Attempted {
		data.attempted[key] = version
	}
	if state.Screen == "results" {
		return state.Screen
	}
	return "scopes"
}

func restoreStdin(source, target int) error {
	if source == target {
		duplicate, err := syscall.Dup(source)
		if err != nil {
			return fmt.Errorf("duplicate terminal descriptor: %w", err)
		}
		defer func() { _ = syscall.Close(duplicate) }()
		source = duplicate
	}
	if err := unix.Dup2(source, target); err != nil {
		return fmt.Errorf("restore stdin: %w", err)
	}
	return nil
}

func execFreshTUI(tty *os.File, data *model) error {
	state := restartState{Profile: data.profileOverride, Screen: "results", Failed: data.failed, Attempted: data.attempted}
	if data.active != nil {
		state.ScopeKind, state.ScopePath = data.active.Kind, data.active.Path
	}
	encoded, err := encodeRestartState(state)
	if err != nil {
		return err
	}
	if err := os.Setenv(restartStateEnv, encoded); err != nil {
		return fmt.Errorf("set restart state: %w", err)
	}
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve TUI executable: %w", err)
	}
	if err := restoreStdin(int(tty.Fd()), syscall.Stdin); err != nil {
		return err
	}
	return syscall.Exec(executable, append([]string{executable}, os.Args[1:]...), os.Environ())
}
