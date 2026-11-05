// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"strings"
	"testing"

	"ItsBagelBot/internal/domain/automation"
	domainrpc "ItsBagelBot/internal/domain/rpc"
	loyaltyrpc "ItsBagelBot/internal/domain/rpc/loyalty"

	"github.com/stretchr/testify/assert"
)

func TestVariableVerbsRoundTrip(t *testing.T) {
	h := newHarness(t)
	channel := loyaltyrpc.Request{UserID: "2", Scope: automation.VariableScopeChannel, Name: "Deaths", Text: "{arg:1}"}
	assert.True(t, h.call(t, "variable.set", channel).Found)

	viewer := loyaltyrpc.Request{UserID: "2", ViewerID: "7", Scope: automation.VariableScopeViewer, Name: "mine", Value: 4}
	assert.True(t, h.call(t, "variable.add", viewer).Found)

	got := h.call(t, "variable.get", loyaltyrpc.Request{UserID: "2", Scope: automation.VariableScopeChannel, Names: []string{"deaths", "absent"}})
	assert.Equal(t, map[string]string{"deaths": "{arg:1}"}, got.Variables)

	got = h.call(t, "variable.get", loyaltyrpc.Request{UserID: "2", ViewerID: "7", Scope: automation.VariableScopeViewer, Names: []string{"mine"}})
	assert.Equal(t, map[string]string{"mine": "4"}, got.Variables)

	h.call(t, "variable.delete", loyaltyrpc.Request{UserID: "2", Scope: automation.VariableScopeChannel, Name: "deaths"})
	got = h.call(t, "variable.get", loyaltyrpc.Request{UserID: "2", Scope: automation.VariableScopeChannel, Names: []string{"deaths"}})
	assert.Empty(t, got.Variables)
}

func TestVariableVerbsReportLimitsAndBadInput(t *testing.T) {
	h := newHarness(t)
	over := loyaltyrpc.Request{UserID: "2", Scope: automation.VariableScopeChannel, Name: "x", Text: strings.Repeat("é", automation.MaxVariableValueRunes+1)}
	reply := h.call(t, "variable.set", over)
	assert.True(t, reply.LimitExceeded)
	assert.Empty(t, reply.Error)

	bad := h.call(t, "variable.set", loyaltyrpc.Request{UserID: "2", Scope: "global", Name: "x"})
	assert.Equal(t, domainrpc.CodeInvalid, bad.Code)
	bad = h.call(t, "variable.get", loyaltyrpc.Request{UserID: "nope", Scope: automation.VariableScopeChannel})
	assert.Equal(t, domainrpc.CodeInvalid, bad.Code)
}
