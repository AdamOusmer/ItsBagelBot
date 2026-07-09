package worker

import (
	"testing"

	"ItsBagelBot/internal/domain/rpc/manage"
)

// The cooldown skip must only ever fire for a channel whose enrollment is
// verified healthy; every other state is a repair the enroll must run for.
func TestRedundantEnrollRequiresHealthyState(t *testing.T) {
	cases := []struct {
		name  string
		ch    manage.Channel
		found bool
		want  bool
	}{
		{"ok state", manage.Channel{SubState: "ok"}, true, true},
		{"failing state", manage.Channel{SubState: "failing"}, true, false},
		{"pending state", manage.Channel{SubState: "pending"}, true, false},
		{"cleared state", manage.Channel{}, true, false},
		{"unknown channel", manage.Channel{SubState: "ok"}, false, false},
	}
	for _, tc := range cases {
		if got := redundantEnroll(tc.ch, tc.found); got != tc.want {
			t.Errorf("%s: redundantEnroll = %v, want %v", tc.name, got, tc.want)
		}
	}
}
