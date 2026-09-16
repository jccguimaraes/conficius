package main

import (
	"testing"

	"github.com/jccguimaraes/conficius/internal/config"
)

func TestRestartStateRoundTrip(t *testing.T) {
	state := restartState{Screen: "results", ScopeKind: "project", ScopePath: "/repo", Failed: map[string]string{"mise\x00node": "failed"}, Attempted: map[string]string{"mise\x00node": "1.0"}}
	encoded, err := encodeRestartState(state)
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodeRestartState(encoded)
	if err != nil {
		t.Fatal(err)
	}
	if got.Screen != state.Screen || got.ScopePath != state.ScopePath || got.Failed["mise\x00node"] != "failed" || got.Attempted["mise\x00node"] != "1.0" {
		t.Fatalf("state = %#v", got)
	}
}

func TestRestoreRestartStateSelectsScope(t *testing.T) {
	data := &model{scopes: []config.Scope{{Kind: "global"}, {Kind: "project", Path: "/repo"}}, failed: map[string]string{}, attempted: map[string]string{}}
	start := restoreRestartState(data, restartState{Screen: "results", ScopeKind: "project", ScopePath: "/repo", Failed: map[string]string{"key": "error"}})
	if start != "results" || data.active == nil || data.active.Path != "/repo" || data.failed["key"] != "error" {
		t.Fatalf("start=%q data=%#v", start, data)
	}
}
