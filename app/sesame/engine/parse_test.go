package engine

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseCommand(t *testing.T) {
	tests := []struct {
		name     string
		text     string
		wantName string
		wantArgs string
		wantOk   bool
	}{
		{"plain text is not a command", "hello world", "", "", false},
		{"bare bang is not a command", "!", "", "", false},
		{"name only", "!ping", "ping", "", true},
		{"name and args", "!so @bob hi", "so", "@bob hi", true},
		{"name lowercased", "!PING", "ping", "", true},
		{"leading spaces tolerated", "   !ping", "ping", "", true},
		{"args trimmed", "!so    spaced   ", "so", "spaced", true},
		{"empty after trim still ok name", "!a b", "a", "b", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			name, args, ok := parseCommand(tt.text)
			assert.Equal(t, tt.wantOk, ok)
			assert.Equal(t, tt.wantName, name)
			assert.Equal(t, tt.wantArgs, args)
		})
	}
}
