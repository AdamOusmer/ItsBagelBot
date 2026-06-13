package ui

import (
	"fmt"
	"time"
)

// relTime renders "12s ago" style ages for the snapshot tables. The fragment
// is re-rendered on every poll, so server-side text stays fresh enough.
func RelTime(t *time.Time, now time.Time) string {
	if t == nil || t.IsZero() {
		return "—"
	}
	d := now.Sub(*t)
	switch {
	case d < 0:
		return "just now"
	case d < time.Second:
		return "just now"
	case d < time.Minute:
		return fmt.Sprintf("%ds ago", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm %ds ago", int(d.Minutes()), int(d.Seconds())%60)
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh %dm ago", int(d.Hours()), int(d.Minutes())%60)
	default:
		return t.Format("2006-01-02 15:04 MST")
	}
}

// shortID keeps mono values readable inside the cards.
func ShortID(s string) string {
	if s == "" {
		return "—"
	}
	if len(s) > 18 {
		return s[:8] + "…" + s[len(s)-6:]
	}
	return s
}

// badgeClass maps a shard/manager state to one of the three badge tones.
func badgeClass(state string) string {
	switch state {
	case "connected", "running", "healthy":
		return "badge ok"
	case "migrating", "binding", "connecting", "degraded":
		return "badge warn"
	default: // backoff, unregistered, unresponsive, down
		return "badge down"
	}
}
