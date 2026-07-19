package twitch

import (
	"errors"
	"fmt"
	"testing"
)

// incidentBody is the exact payload Twitch returned for the dead grant that
// this classification exists to catch.
const incidentBody = `{"status":400,"message":"Invalid refresh token"}`

func TestGrantDeadClassification(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"the incident: 400 invalid refresh token", &TokenError{Status: 400, Body: incidentBody}, true},
		{"401 unauthorized", &TokenError{Status: 401, Body: "unauthorized"}, true},
		{"403 forbidden", &TokenError{Status: 403, Body: "forbidden"}, true},
		{"no user token", ErrNoUserToken, true},
		{"no refresh token", ErrNoRefreshToken, true},

		{"429 rate limited", &TokenError{Status: 429, Body: "slow down"}, false},
		{"500 twitch broke", &TokenError{Status: 500, Body: "internal"}, false},
		{"503 unavailable", &TokenError{Status: 503, Body: "unavailable"}, false},
		{"transport failure", errors.New("dial tcp: i/o timeout"), false},
		{"nil", nil, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := GrantDead(tc.err); got != tc.want {
				t.Errorf("GrantDead(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// TestGrantDeadThroughWrapping matters because the incident's error reached the
// worker wrapped by client.do as "twitch token: %w".
func TestGrantDeadThroughWrapping(t *testing.T) {
	wrapped := fmt.Errorf("twitch token: %w", &TokenError{Status: 400, Body: incidentBody})
	if !GrantDead(wrapped) {
		t.Fatal("GrantDead did not see through the twitch token wrap")
	}
}

// TestStatusErrorIsNotGrantDead pins the separation from Helix errors. A Helix
// 401 is recoverable by refreshing, and worker.isPermanent depends on that, so
// it must never be mistaken for a dead grant.
func TestStatusErrorIsNotGrantDead(t *testing.T) {
	for _, status := range []int{400, 401, 403} {
		err := &StatusError{Status: status, Body: "helix says no"}
		if GrantDead(err) {
			t.Errorf("Helix StatusError %d was classified as a dead grant", status)
		}
	}
}

// TestTokenErrorMessageUnchanged keeps the rendered text byte-identical to the
// fmt.Errorf it replaced, so existing log greps and dashboards keep matching.
func TestTokenErrorMessageUnchanged(t *testing.T) {
	got := (&TokenError{Status: 400, Body: incidentBody}).Error()
	want := fmt.Sprintf("token request failed: %d %s", 400, incidentBody)
	if got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}
