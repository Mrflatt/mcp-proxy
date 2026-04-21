package config

import (
	"encoding/json"
	"testing"
)

func TestDirectToolsUnmarshal(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantAll bool
		wantNames []string
		wantErr bool
	}{
		{name: "true", input: `true`, wantAll: true},
		{name: "false", input: `false`, wantAll: false},
		{name: "list", input: `["topic_describe","topic_list"]`, wantNames: []string{"topic_describe", "topic_list"}},
		{name: "empty list", input: `[]`, wantNames: []string{}},
		{name: "invalid", input: `"string"`, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var d DirectTools
			err := json.Unmarshal([]byte(tt.input), &d)
			if tt.wantErr {
				if err == nil {
					t.Error("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if d.All != tt.wantAll {
				t.Errorf("All = %v, want %v", d.All, tt.wantAll)
			}
			if tt.wantNames != nil && len(d.Names) != len(tt.wantNames) {
				t.Errorf("Names = %v, want %v", d.Names, tt.wantNames)
			}
		})
	}
}

func TestDirectToolsEnabled(t *testing.T) {
	tests := []struct {
		name string
		d    DirectTools
		want bool
	}{
		{name: "zero value", d: DirectTools{}, want: false},
		{name: "all", d: DirectTools{All: true}, want: true},
		{name: "names", d: DirectTools{Names: []string{"foo"}}, want: true},
		{name: "empty names", d: DirectTools{Names: []string{}}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.d.Enabled(); got != tt.want {
				t.Errorf("Enabled() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestDirectToolsIncludes(t *testing.T) {
	tests := []struct {
		name     string
		d        DirectTools
		toolName string
		want     bool
	}{
		{name: "all includes anything", d: DirectTools{All: true}, toolName: "whatever", want: true},
		{name: "list includes match", d: DirectTools{Names: []string{"topic_describe", "topic_list"}}, toolName: "topic_describe", want: true},
		{name: "list excludes non-match", d: DirectTools{Names: []string{"topic_describe"}}, toolName: "topic_list", want: false},
		{name: "empty includes nothing", d: DirectTools{}, toolName: "anything", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.d.Includes(tt.toolName); got != tt.want {
				t.Errorf("Includes(%q) = %v, want %v", tt.toolName, got, tt.want)
			}
		})
	}
}
