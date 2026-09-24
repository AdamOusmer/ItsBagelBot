// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package bus

import (
	"context"
	"errors"
	"strconv"

	"ItsBagelBot/internal/domain/rpc"
)

const (
	refusalNoUserID      = "bad request"
	refusalInvalidUserID = "invalid user_id"
)

var (
	ErrNoUserID      = errors.New(refusalNoUserID)
	ErrInvalidUserID = errors.New(refusalInvalidUserID)
)

func UserID(raw string) (uint64, error) {
	if raw == "" {
		return 0, ErrNoUserID
	}
	id, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return 0, ErrInvalidUserID
	}
	return id, nil
}

type Requesting interface{ Requested() string }

type Failing interface{ Failed(message string) }

func ForUser[Req Requesting, Rep any, PR interface {
	*Rep
	Failing
}](load func(context.Context, Req, uint64) (Rep, error)) func(context.Context, Req) Rep {
	return func(ctx context.Context, req Req) Rep {
		id, err := UserID(req.Requested())
		if err != nil {
			return RefuseErr[Rep, PR](err)
		}
		reply, err := load(ctx, req, id)
		if err != nil {
			return RefuseErr[Rep, PR](err)
		}
		return reply
	}
}

func ServeForUser[Req Requesting, Rep any, PR interface {
	*Rep
	Failing
}](w RPCWiring, subject string, load func(context.Context, Req, uint64) (Rep, error)) error {
	return Serve(w, subject, ForUser[Req, Rep, PR](load))
}

func VerbForUser[Req Requesting, Rep any, PR interface {
	*Rep
	Failing
}](name string, load func(context.Context, Req, uint64) (Rep, error)) Verb[Req, Rep] {
	return Verb[Req, Rep]{Name: name, Handle: ForUser[Req, Rep, PR](load)}
}

func Refuse[Rep any, PR interface {
	*Rep
	Failing
}](message string) Rep {
	var zero Rep
	PR(&zero).Failed(message)
	return zero
}

var userIDRules = []rpc.Rule{
	rpc.Is(ErrNoUserID, rpc.CodeInvalid),
	rpc.Is(ErrInvalidUserID, rpc.CodeInvalid),
}

func Classify(err error, rules ...rpc.Rule) rpc.Refusal {
	return rpc.Fail(err, append(rules, userIDRules...)...)
}

func RefuseErr[Rep any, PR interface {
	*Rep
	Failing
}](err error) Rep {
	var zero Rep
	target := PR(&zero)
	refusal := Classify(err)
	if coded, ok := any(target).(rpc.Refusing); ok {
		coded.Refuse(refusal)
		return zero
	}
	target.Failed(refusal.Error)
	return zero
}
