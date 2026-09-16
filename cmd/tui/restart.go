package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"syscall"
)

const restartStateEnv = "CONFICIUS_TUI_RESTART_STATE"

type restartState struct {
	Screen    string            `json:"screen"`
	ScopeKind string            `json:"scope_kind,omitempty"`
	ScopePath string            `json:"scope_path,omitempty"`
	Failed    map[string]string `json:"failed,omitempty"`
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
	for i := range data.scopes {
		if data.scopes[i].Kind == state.ScopeKind && data.scopes[i].Path == state.ScopePath {
			data.active = &data.scopes[i]
			break
		}
	}
	for key, message := range state.Failed {
		data.failed[key] = message
	}
	if state.Screen == "results" {
		return state.Screen
	}
	return "scopes"
}

func execFreshTUI(tty *os.File, data *model) error {
	state := restartState{Screen: "results", Failed: data.failed}
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
	if err := syscall.Dup2(int(tty.Fd()), int(os.Stdin.Fd())); err != nil {
		return fmt.Errorf("restore stdin: %w", err)
	}
	return syscall.Exec(executable, append([]string{executable}, os.Args[1:]...), os.Environ())
}
