package module

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestExpandGenericRepl(t *testing.T) {
	repl := func(key string) (string, bool) {
		switch key {
		case "raider":
			return "CoolStreamer", true
		case "viewers":
			return "42", true
		default:
			return "", false
		}
	}
	got := Expand(nil, "{raider} raided with {viewers}! {unknown}", repl)
	assert.Equal(t, "CoolStreamer raided with 42! {unknown}", string(got))
}

func TestExpandString(t *testing.T) {
	repl := func(key string) (string, bool) {
		switch key {
		case "raider":
			return "CoolStreamer", true
		case "viewers":
			return "42", true
		default:
			return "", false
		}
	}
	got := ExpandString("{raider} raided with {viewers}! {unknown}", repl)
	assert.Equal(t, "CoolStreamer raided with 42! {unknown}", got)
}
