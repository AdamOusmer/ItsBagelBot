// Copyright (c) 2026 Adam Ousmer. All rights reserved.
// Proprietary. No license granted. See LICENSE.md.

package tts

import (
	"cmp"
	"context"
	"errors"

	gossiprpc "ItsBagelBot/internal/domain/rpc/gossip"
	"ItsBagelBot/internal/moderation"

	"github.com/newrelic/go-agent/v3/newrelic"
	"go.uber.org/zap"
)

type refusal struct {
	class gossiprpc.RefusalClass
	raw   string
}

func (r refusal) Error() string { return errRefused.Error() }

func (r refusal) reply() gossiprpc.TTSSynthesizeReply {
	if r.class == gossiprpc.RefusalEmpty {
		return gossiprpc.TTSSynthesizeReply{Error: errNoAudio.Error(), RefusalClass: r.class}
	}
	return gossiprpc.TTSSynthesizeReply{Error: errRefused.Error(), Reason: "refused", RefusalClass: r.class}
}

var blockReasonClasses = map[string]gossiprpc.RefusalClass{
	"SAFETY":                     gossiprpc.RefusalPolicy,
	"PROHIBITED_CONTENT":         gossiprpc.RefusalPolicy,
	"BLOCKLIST":                  gossiprpc.RefusalPolicy,
	"JAILBREAK":                  gossiprpc.RefusalPolicy,
	"MODEL_ARMOR":                gossiprpc.RefusalPolicy,
	"OTHER":                      gossiprpc.RefusalUnspecified,
	"BLOCKED_REASON_UNSPECIFIED": gossiprpc.RefusalUnspecified,
}

var finishReasonClasses = map[string]gossiprpc.RefusalClass{
	"SAFETY":                    gossiprpc.RefusalPolicy,
	"PROHIBITED_CONTENT":        gossiprpc.RefusalPolicy,
	"SPII":                      gossiprpc.RefusalPolicy,
	"BLOCKLIST":                 gossiprpc.RefusalPolicy,
	"MODEL_ARMOR":               gossiprpc.RefusalPolicy,
	"OTHER":                     gossiprpc.RefusalEmpty,
	"MAX_TOKENS":                gossiprpc.RefusalEmpty,
	"STOP":                      gossiprpc.RefusalEmpty,
	"MALFORMED_RESPONSE":        gossiprpc.RefusalEmpty,
	"FINISH_REASON_UNSPECIFIED": gossiprpc.RefusalEmpty,
	"":                          gossiprpc.RefusalEmpty,
	"RECITATION":                gossiprpc.RefusalInvalid,
}

func blockRefusal(raw string) refusal {
	return refusal{class: cmp.Or(blockReasonClasses[raw], gossiprpc.RefusalUnknown), raw: raw}
}

func finishRefusal(raw string) refusal {
	return refusal{class: cmp.Or(finishReasonClasses[raw], gossiprpc.RefusalUnknown), raw: raw}
}

func (s *synthesizer) refusalReply(ctx context.Context, err error) (gossiprpc.TTSSynthesizeReply, bool) {
	var ref refusal
	if !errors.As(err, &ref) {
		return gossiprpc.TTSSynthesizeReply{}, false
	}
	newrelic.FromContext(ctx).AddAttribute("tts.refusal", string(ref.class)+":"+ref.raw)
	if ref.class == gossiprpc.RefusalUnknown {
		s.log.Warn("unknown speech refusal reason", zap.String("raw", ref.raw))
	}
	return ref.reply(), true
}

func localRefusal(ctx context.Context, text string) (gossiprpc.TTSSynthesizeReply, bool) {
	reason, hit := moderation.CheckSpeechFloor(text)
	if !hit {
		return gossiprpc.TTSSynthesizeReply{}, false
	}
	newrelic.FromContext(ctx).AddAttribute("tts.gate", string(reason))
	reply := refusal{class: gossiprpc.RefusalLocal}.reply()
	reply.Gate = string(reason)
	return reply, true
}
