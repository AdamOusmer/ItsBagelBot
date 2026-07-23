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

func TestExpandKeyCaseInsensitive(t *testing.T) {
	repl := func(key string) (string, bool) {
		// Keys arrive with the name lowercased; the payload keeps its case.
		switch {
		case key == "user":
			return "sam", true
		case key == "choice:Hi,Yo":
			return "Hi", true
		default:
			return "", false
		}
	}
	got := ExpandString("{User} says {CHOICE:Hi,Yo} to {USER}", repl)
	assert.Equal(t, "sam says Hi to sam", got)
}

func TestParseDynamicCaseInsensitiveViaExpand(t *testing.T) {
	// {Random:5-5} normalizes to random:5-5 before ParseDynamic sees it.
	got := ExpandString("{Random:5-5}", func(key string) (string, bool) {
		return ParseDynamic(key)
	})
	assert.Equal(t, "5", got)
}
