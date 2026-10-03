// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package engine

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

const campaignSimhash = 0x0123456789abcdef

func TestValkeyCampaignKeyScoping(t *testing.T) {
	cases := []struct {
		name          string
		broadcasterID uint64
		sender        string
	}{
		{name: "tenant 123", broadcasterID: 123, sender: "777"},
		{name: "tenant 987654321 with the same template stays isolated", broadcasterID: 987654321, sender: "888"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newFakeValkey(t)
			c := NewValkeyCampaign(f.client, zap.NewNop())

			c.Observe(context.Background(), tc.broadcasterID, campaignSimhash, tc.sender)

			tenant := strconv.FormatUint(tc.broadcasterID, 10)
			low, high := "am:tmpl:"+tenant+":1234567", "am:tmpl:"+tenant+":89abcdef"
			ttl := strconv.FormatInt(int64(campaignWindow.Seconds()), 10)
			assert.Equal(t, [][]string{
				{"PFADD", low, tc.sender}, {"PFADD", high, tc.sender},
				{"EXPIRE", low, ttl}, {"EXPIRE", high, ttl},
				{"PFCOUNT", low}, {"PFCOUNT", high},
			}, f.commands())
		})
	}
}

func TestValkeyCampaignTenantsShareNoKeys(t *testing.T) {
	f := newFakeValkey(t)
	c := NewValkeyCampaign(f.client, zap.NewNop())
	c.Observe(context.Background(), 123, campaignSimhash, "777")
	first := len(f.commands())
	c.Observe(context.Background(), 456, campaignSimhash, "888")

	keys := func(cmds [][]string) map[string]bool {
		out := map[string]bool{}
		for _, cmd := range cmds {
			out[cmd[1]] = true
		}
		return out
	}
	cmds := f.commands()
	tenantOne, tenantTwo := keys(cmds[:first]), keys(cmds[first:])

	for key := range tenantOne {
		assert.True(t, strings.HasPrefix(key, "am:tmpl:123:"), "key %q is not scoped to its tenant", key)
		assert.False(t, tenantTwo[key], "key %q is shared across tenants", key)
	}
	for key := range tenantTwo {
		assert.True(t, strings.HasPrefix(key, "am:tmpl:456:"), "key %q is not scoped to its tenant", key)
	}
}

func TestValkeyCampaignIgnoresUnusableInput(t *testing.T) {
	cases := []struct {
		name          string
		broadcasterID uint64
		simhash       uint64
		sender        string
	}{
		{"no broadcaster", 0, 0x1234, "777"},
		{"no simhash", 123, 0, "777"},
		{"no sender", 123, 0x1234, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := NewValkeyCampaign(nil, zap.NewNop())

			assert.Zero(t, c.Observe(context.Background(), tc.broadcasterID, tc.simhash, tc.sender))
		})
	}
}

func TestCampaignWriteErrorsVisibleOncePerInterval(t *testing.T) {
	core, logs := observer.New(zapcore.DebugLevel)
	f := newFakeValkey(t)
	c := NewValkeyCampaign(f.client, zap.New(core))
	observe := func() int { return c.Observe(context.Background(), 123, campaignSimhash, "777") }
	failures := func() []observer.LoggedEntry { return logs.FilterMessageSnippet("write failed").All() }
	f.goDown()

	assert.Zero(t, observe(), "a failing store never corroborates a campaign")
	assert.Len(t, failures(), 1, "first failure in an interval logs immediately")
	assert.Equal(t, int64(4), failures()[0].ContextMap()["suppressed"])
	assert.Zero(t, c.errPending.Load(), "counter resets after logging")

	observe()
	assert.Len(t, failures(), 1, "failures inside the interval are suppressed")
	assert.Equal(t, int64(4), c.errPending.Load())

	c.lastWriteLogNs.Add(-int64(2 * campaignErrLogInterval))
	observe()
	assert.Len(t, failures(), 2)
	assert.Equal(t, int64(8), failures()[1].ContextMap()["suppressed"])

	c.lastWriteLogNs.Add(-int64(2 * campaignErrLogInterval))
	f.comeBack()
	observe()
	assert.Len(t, failures(), 2, "a clean write logs nothing")
	assert.Zero(t, c.errPending.Load())
}
