// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package scope

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
)

type fakePeeks struct {
	values map[string]string
	asked  []string
}

func (f *fakePeeks) Peek(_ context.Context, name string, addressed bool) string {
	read := name
	if addressed {
		read = "target:" + name
	}
	f.asked = append(f.asked, read)
	return f.values[name]
}

func TestStoreReads(t *testing.T) {
	counts := map[string]string{"deaths": "42", "hugs": "1", "shutups": "7"}
	tests := []struct {
		name      string
		template  string
		want      string
		wantAsked []string
	}{
		{"reads a count span after folding the payload", "{count:Deaths}", "42", []string{"deaths"}},
		{"treats counter and count as one read", "{counter:deaths} ({count:Deaths} too)", "42 (42 too)", []string{"deaths"}},
		{
			name:      "folds trim, bang and case spellings into one read",
			template:  "{counter:Deaths} {counter: deaths } {counter:!deaths}",
			want:      "42 42 42",
			wantAsked: []string{"deaths"},
		},
		{"reads a target-addressed count", "{count:target:shutups}", "7", []string{"target:shutups"}},
		{"reads a target-addressed counter", "{counter:target:shutups}", "7", []string{"target:shutups"}},
		{
			name:      "strips the addressing prefix before the store",
			template:  "{counter:hugs} {counter:target:shutups}",
			want:      "1 7",
			wantAsked: []string{"hugs", "target:shutups"},
		},
		{"renders a missing counter as empty", "{count:nothing}", "", []string{"nothing"}},
		{"renders the fallback for a missing counter", "{count:nothing|none yet}", "none yet", []string{"nothing"}},
		{"leaves an empty count span literal", "{count:}", "{count:}", nil},
		{"leaves an empty target count span literal", "{count:target:}", "{count:target:}", nil},
		{"leaves a bare counter literal", "{counter}", "{counter}", nil},
		{"leaves an empty counter literal", "{counter:}", "{counter:}", nil},
		{"leaves an empty target counter literal", "{counter:target:}", "{counter:target:}", nil},
		{"does not own a bare count", "{count}", "{count}", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			peeks := &fakePeeks{values: counts}

			assert.Equal(t, tt.want, render(t, tt.template, Chain{Store{Peeks: peeks}}, nil))
			assert.Equal(t, tt.wantAsked, peeks.asked)
		})
	}
}

func TestStoreUnmountedLeavesTokensLiteral(t *testing.T) {
	assert.Equal(t, "{counter:deaths} {count:deaths}", render(t, "{counter:deaths} {count:deaths}", Chain{Store{}}, nil))
}

func TestNormalizeName(t *testing.T) {
	tests := []struct{ in, want string }{
		{"  !Deaths  ", "deaths"},
		{"  ", ""},
		{"A.B", "a.b"},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			assert.Equal(t, tt.want, NormalizeName(tt.in))
		})
	}
}
