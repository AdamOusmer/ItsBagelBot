// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package scope

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUsesRender(t *testing.T) {
	tests := []struct {
		name     string
		chain    Chain
		template string
		want     string
	}{
		{"renders the command row count", Chain{Uses{Count: 41}}, "hugged {uses} times", "hugged 41 times"},
		{"renders zero rather than falling back", Chain{Uses{}}, "{uses|never}", "0"},
		{"answers the bare count alias", Chain{Uses{Count: 7}}, "{count}", "7"},
		{"rejects a payload", Chain{Uses{Count: 7}}, "{uses:hug}", "{uses:hug}"},
		{"rejects a payload with a fallback", Chain{Uses{Count: 7}}, "{uses:hug|0}", "{uses:hug|0}"},
		{"leaves a payloaded count to the counter store", Chain{Uses{Count: 7}}, "{count:deaths}", "{count:deaths}"},
		{"stays literal when not mounted", Chain{Message{User: "alice"}}, "{uses}", "{uses}"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, render(t, tt.template, tt.chain, nil))
		})
	}
}
