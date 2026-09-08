// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

// Package rpc holds the one refusal vocabulary every request/reply service in
// the fleet answers with: a short machine-readable Code beside the human
// Error sentence.
//
// Why it exists: forty-nine handlers across the seven db services answered
// `Reply{Error: err.Error()}` and nothing else, so the only thing a caller
// could branch on was the sentence. The console therefore string-matched
// error text ("already linked to another Twitch channel"), which breaks the
// moment anyone rewords a log line -- and it broke, twice. app/db/discord had
// already grown a private sentinel-to-code table to escape that; this package
// is that table promoted, so there is one vocabulary rather than one per
// service.
//
// The wire stays additive: `code` is a new omitempty field beside the
// unchanged `error`, so a reader that has never heard of it decodes exactly
// what it decoded before, and a reply from an older build simply arrives with
// an empty code. Nothing here ever changes the Error text; callers still on
// the sentence keep working through the transition.
//
// Rejected alternative: numeric codes (gRPC-style). They read as noise in a
// NATS JSON payload a human greps out of a log, and the console would have
// needed a lookup table to say anything useful. Strings cost a few bytes and
// are self-describing.
package rpc

import (
	"context"
	"errors"
)

// Code is the machine-readable half of a refusal. A defined type rather than
// a bare string so a handler cannot pass a message where a code belongs --
// the two are both strings and both plausible in either position.
type Code string

// The vocabulary. Deliberately small: these are the distinctions a caller
// acts on differently, not a catalogue of everything that can go wrong. The
// sentence in Error carries the detail. A service that needs a finer
// distinction declares its own Code constant next to its wire types (see
// discorddata.CodeBoundElsewhere) rather than widening this set for everyone.
const (
	// CodeOK is the zero value: the call did what it was asked to. Refusals
	// never carry it; it exists so `code == CodeOK` reads as "no refusal".
	CodeOK Code = ""
	// CodeInvalid: the request itself is malformed -- a missing id, an
	// unparseable number, a field outside its allowed set. Retrying the same
	// request cannot help.
	CodeInvalid Code = "invalid"
	// CodeNotFound: the request was well-formed and the addressed row does
	// not exist. Distinct from a read that legitimately found nothing, which
	// is a successful reply with found:false.
	CodeNotFound Code = "not_found"
	// CodeForbidden: the caller is known and is not allowed to do this.
	CodeForbidden Code = "forbidden"
	// CodeConflict: the caller's write raced another and lost -- a stale
	// version, a uniqueness violation. Re-read and re-apply, do not retry.
	CodeConflict Code = "conflict"
	// CodeUnavailable: a dependency this handler needs did not answer in
	// time. The same request may well succeed later, which is what separates
	// it from CodeInternal.
	CodeUnavailable Code = "unavailable"
	// CodeInternal: the handler failed and cannot say more usefully than
	// that. The Error field carries the detail for logs.
	CodeInternal Code = "internal"
)

// Codes lists the vocabulary, CodeOK excluded because it is the absence of a
// refusal. It exists so the console's known-code set can be checked against
// this file by a test instead of by hand.
func Codes() []Code {
	return []Code{CodeInvalid, CodeNotFound, CodeForbidden, CodeConflict, CodeUnavailable, CodeInternal}
}

// Refusal is the error half of a reply. Embedded in reply types so the JSON
// stays flat (`error` and `code` at the top level, `error` byte-identical to
// what the service sent before) while handlers build it in one place.
type Refusal struct {
	Error string `json:"error,omitempty"`
	Code  Code   `json:"code,omitempty"`
}

// Refused builds one refusal. A single constructor taking the code rather
// than one helper per code: six sibling one-liners with the same skeleton is
// the duplication this package exists to remove.
func Refused(code Code, message string) Refusal { return Refusal{Error: message, Code: code} }

// Failed satisfies bus.Failing, which is message-only and stays that way: a
// dozen replies outside this vocabulary still implement it. It deliberately
// leaves Code alone, so a caller that only knows the old contract produces
// exactly the reply it produced before rather than a wrongly-coded one.
func (r *Refusal) Failed(message string) { r.Error = message }

// Refuse stamps a whole refusal onto a reply that embeds one, which lets a
// generic helper build any such reply without knowing its type.
func (r *Refusal) Refuse(v Refusal) { *r = v }

// Refusing is any reply that carries a Refusal. Consumer-side interface (the
// helpers that need it declare it) so reply structs stay plain data.
type Refusing interface{ Refuse(Refusal) }

// Rule maps one error onto one code. Services hold their own small table of
// these for their own sentinels; Fail applies it before falling back to the
// generic cases, so a service can name a distinction the shared vocabulary
// does not without every other service having to know about it.
type Rule struct {
	Match func(error) bool
	Code  Code
}

// Is is the common rule: this sentinel (anywhere in the wrap chain).
func Is(sentinel error, code Code) Rule {
	return Rule{Match: func(err error) bool { return errors.Is(err, sentinel) }, Code: code}
}

// When is the rule for a predicate rather than a sentinel -- ent.IsNotFound
// and friends, which are functions, not comparable errors.
func When(match func(error) bool, code Code) Rule { return Rule{Match: match, Code: code} }

// Fail turns a handler's error into the reply's refusal. The message is
// always err.Error() verbatim: this changes what a caller can branch on, not
// what it reads.
func Fail(err error, rules ...Rule) Refusal {
	if err == nil {
		return Refusal{}
	}
	return Refusal{Error: err.Error(), Code: classify(err, rules)}
}

// classify runs the service's table first, then the two cases every service
// shares. An unrecognised error is CodeInternal rather than CodeOK so a
// caller can never read a failure as a success.
func classify(err error, rules []Rule) Code {
	for _, rule := range rules {
		if rule.Match(err) {
			return rule.Code
		}
	}
	if timedOut(err) {
		return CodeUnavailable
	}
	return CodeInternal
}

// timedOut names the "the dependency did not answer" predicate so classify's
// condition stays single-operand.
func timedOut(err error) bool {
	return errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)
}
