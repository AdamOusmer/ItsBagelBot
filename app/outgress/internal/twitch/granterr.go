package twitch

import (
	"errors"
	"fmt"
	"net/http"
)

// ErrNoRefreshToken marks a stored-token source with nothing to refresh from:
// the account never authorized, or its grant row is empty. Distinct from a
// grant Twitch actively rejects, though both mean the same thing to a caller.
var ErrNoRefreshToken = errors.New("no refresh token available")

// TokenError is a rejection from the OAuth token endpoint (id.twitch.tv), as
// opposed to StatusError which comes from the Helix API. The two are kept
// separate deliberately: worker.isPermanent switches on *StatusError and
// treats 401 as recoverable, because a Helix 401 usually clears after a token
// refresh. A token-endpoint 401 is the refresh itself failing, so folding
// these together would silently reclassify every grant failure.
type TokenError struct {
	Status int
	Body   string
}

func (e *TokenError) Error() string {
	return fmt.Sprintf("token request failed: %d %s", e.Status, e.Body)
}

// PermanentAuth reports whether Twitch rejected the grant itself, rather than
// failing transiently. A dead refresh token answers 400; 401 and 403 carry the
// same meaning for a grant. Everything else (429, 5xx, transport) may recover.
func (e *TokenError) PermanentAuth() bool {
	switch e.Status {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden:
		return true
	default:
		return false
	}
}

// GrantDead reports whether err means a broadcaster's stored grant can no
// longer produce a token, so no retry will help and only re-consent will.
//
// This gates a user-visible, publicly-posted chat line, so it is deliberately
// narrow: only a rejection from Twitch's own token endpoint counts. A 500, a
// timeout or a DNS blip must never tell a streamer their connection expired.
func GrantDead(err error) bool {
	if errors.Is(err, ErrNoUserToken) || errors.Is(err, ErrNoRefreshToken) {
		return true
	}

	var te *TokenError
	if errors.As(err, &te) {
		return te.PermanentAuth()
	}
	return false
}
