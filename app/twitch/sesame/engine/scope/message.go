// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package scope

import "context"

// Message answers the tokens that are already on the chat line that triggered
// the command: who typed it, what they typed after the trigger, and where.
// Nothing here costs a lookup, so it is always mounted.
//
// The values arrive already sanitized (engine.sanitizeVar) — the viewer
// controls Args and Touser, and a leading slash run in either would otherwise
// become a moderation verb once the reply is split per line.
type Message struct {
	// User and Sender are the same chatter under two spellings; both are
	// kept because commands written against either must keep working.
	User   string
	Sender string
	// Args is the rest of the line after the trigger.
	Args string
	// Touser is the mentioned viewer, defaulting to the sender. {target} is
	// the dashboard-facing name for the same value.
	Touser string
	// Channel is the broadcaster's display name.
	Channel string
}

// Owns claims the fixed identity/argument palette.
func (Message) Owns(name string) bool {
	switch name {
	case "user", "sender", "args", "touser", "target", "channel":
		return true
	}
	return false
}

// Plan hands the struct back: every value is already in hand by the time a
// command runs, so there is nothing to batch and no ctx to spend.
func (m Message) Plan(context.Context, []Var) (Values, error) {
	return m, nil
}

// Get resolves one identity token.
//
// A payload is rejected rather than ignored: {user} is the token, {user:bob}
// is not one, and answering it as if the payload were absent would silently
// invent a grammar (and break the day a real {user:...} form ships). It stays
// literal, like every other name this palette does not have.
func (m Message) Get(v Var) (string, bool) {
	if v.HasPayload {
		return "", false
	}
	switch v.Name {
	case "user":
		return m.User, true
	case "sender":
		return m.Sender, true
	case "args":
		return m.Args, true
	case "touser", "target":
		return m.Touser, true
	case "channel":
		return m.Channel, true
	}
	return "", false
}
