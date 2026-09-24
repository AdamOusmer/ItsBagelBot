// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package rpc

import (
	"context"
	"errors"
)

type Code string

const (
	CodeOK          Code = ""
	CodeInvalid     Code = "invalid"
	CodeNotFound    Code = "not_found"
	CodeForbidden   Code = "forbidden"
	CodeConflict    Code = "conflict"
	CodeUnavailable Code = "unavailable"
	CodeInternal    Code = "internal"
)

func Codes() []Code {
	return []Code{CodeInvalid, CodeNotFound, CodeForbidden, CodeConflict, CodeUnavailable, CodeInternal}
}

type Refusal struct {
	Error string `json:"error,omitempty"`
	Code  Code   `json:"code,omitempty"`
}

func Refused(code Code, message string) Refusal { return Refusal{Error: message, Code: code} }

func (r *Refusal) Failed(message string) { r.Error = message }

func (r *Refusal) Refuse(v Refusal) { *r = v }

type Refusing interface{ Refuse(Refusal) }

type Rule struct {
	Match func(error) bool
	Code  Code
}

func Is(sentinel error, code Code) Rule {
	return Rule{Match: func(err error) bool { return errors.Is(err, sentinel) }, Code: code}
}

func When(match func(error) bool, code Code) Rule { return Rule{Match: match, Code: code} }

func Fail(err error, rules ...Rule) Refusal {
	if err == nil {
		return Refusal{}
	}
	return Refusal{Error: err.Error(), Code: classify(err, rules)}
}

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

func timedOut(err error) bool {
	return errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled)
}
